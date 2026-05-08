package downloader

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

//nolint:misspell // Prefer UK English in user-facing error text.
var errPipelineCancelled = errors.New("pipeline cancelled")

// PipelineStats holds aggregate statistics for a completed pipeline run.
type PipelineStats struct {
	TotalChunks      int
	FirstViewChunks  int
	SecondViewChunks int
	FailedChunks     int
	ElapsedTime      time.Duration
	DownloadWorkers  int
	DecryptWorkers   int
}

// ChunkTask represents a single chunk to be downloaded and decrypted by the pipeline.
type ChunkTask struct {
	ChunkID      int
	URL          string
	View         string
	LectureID    int
	LectureSeqNo int
}

// DownloadedChunk holds the result of downloading a single chunk for decryption.
type DownloadedChunk struct {
	ChunkID        int
	View           string
	EncryptedPath  string
	EncryptedBytes []byte
	LectureID      int
	DownloadTime   time.Duration
	Err            error
}

// DecryptedChunk holds the result of decrypting a downloaded chunk.
type DecryptedChunk struct {
	ChunkID       int
	View          string
	DecryptedPath string
	DecryptTime   time.Duration
	Err           error
}

// PipelineConfig configures the concurrency and context for a lecture download pipeline.
type PipelineConfig struct {
	Context         context.Context
	DownloadWorkers int
	DecryptWorkers  int
	DecryptionKey   []byte
	LectureID       int
	LectureSeqNo    int
	ProgressTracker *ProgressTracker
}

// PipelineResult contains the ordered chunk paths and any failures from a completed pipeline.
type PipelineResult struct {
	FirstViewChunks  []string
	SecondViewChunks []string
	TotalTime        time.Duration
	FailedChunks     []int
}

// LecturePipeline manages concurrent download and decrypt workers for a single lecture.
type LecturePipeline struct {
	config PipelineConfig

	downloader *Downloader

	downloadQueue    chan ChunkTask
	downloadedChunks chan DownloadedChunk
	decryptedChunks  chan DecryptedChunk

	ctx    context.Context
	cancel context.CancelFunc

	firstViewMap  map[int]string
	secondViewMap map[int]string
	totalChunks   int
	failedChunks  []int
	mu            sync.Mutex

	startTime time.Time

	downloadWg sync.WaitGroup
	decryptWg  sync.WaitGroup

	submissionsClosed atomic.Bool
}

// NewLecturePipeline creates a new pipeline with the given config and downloader.
func NewLecturePipeline(config PipelineConfig, downloader *Downloader) *LecturePipeline {
	baseCtx := config.Context
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(baseCtx)
	downloadBufSize := config.DownloadWorkers * 2
	decryptBufSize := config.DecryptWorkers * 2
	if downloadBufSize < 1 {
		downloadBufSize = 1
	}
	if decryptBufSize < 1 {
		decryptBufSize = 1
	}

	pipeline := &LecturePipeline{
		config:           config,
		downloader:       downloader,
		downloadQueue:    make(chan ChunkTask, downloadBufSize),
		downloadedChunks: make(chan DownloadedChunk, downloadBufSize),
		decryptedChunks:  make(chan DecryptedChunk, decryptBufSize),
		ctx:              ctx,
		cancel:           cancel,
		firstViewMap:     make(map[int]string),
		secondViewMap:    make(map[int]string),
		failedChunks:     make([]int, 0),
		startTime:        time.Now(),
	}
	go func() {
		<-ctx.Done()
		pipeline.submissionsClosed.Store(true)
	}()
	return pipeline
}

// Start launches the download and decrypt worker goroutines.
func (p *LecturePipeline) Start() {
	for i := 0; i < p.config.DownloadWorkers; i++ {
		p.downloadWg.Add(1)
		go p.downloadWorker()
	}

	for i := 0; i < p.config.DecryptWorkers; i++ {
		p.decryptWg.Add(1)
		go p.decryptWorker()
	}

	go func() {
		p.downloadWg.Wait()
		close(p.downloadedChunks)
		p.decryptWg.Wait()
		close(p.decryptedChunks)
	}()
}

func (p *LecturePipeline) downloadWorker() {
	defer p.downloadWg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case task, ok := <-p.downloadQueue:
			if !ok {
				return
			}

			encryptedPath, encryptedBytes, err := p.downloader.downloadBytesWithRetry(p.ctx, task.URL, task.LectureID, task.ChunkID, task.View, 3, p.config.ProgressTracker)
			result := DownloadedChunk{
				ChunkID:        task.ChunkID,
				View:           task.View,
				EncryptedPath:  encryptedPath,
				EncryptedBytes: encryptedBytes,
				LectureID:      task.LectureID,
				Err:            err,
			}

			select {
			case <-p.ctx.Done():
				return
			case p.downloadedChunks <- result:
			}
		}
	}
}

func (p *LecturePipeline) decryptWorker() {
	defer p.decryptWg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case downloaded, ok := <-p.downloadedChunks:
			if !ok {
				return
			}

			if downloaded.Err != nil {
				select {
				case <-p.ctx.Done():
					return
				case p.decryptedChunks <- DecryptedChunk{ChunkID: downloaded.ChunkID, View: downloaded.View, Err: downloaded.Err}:
				}
				continue
			}

			decryptedPath, err := p.downloader.decryptChunkBytes(downloaded.EncryptedPath, downloaded.EncryptedBytes, p.config.DecryptionKey)
			result := DecryptedChunk{
				ChunkID:       downloaded.ChunkID,
				View:          downloaded.View,
				DecryptedPath: decryptedPath,
				Err:           err,
			}

			select {
			case <-p.ctx.Done():
				return
			case p.decryptedChunks <- result:
			}
		}
	}
}

// SubmitDownload enqueues a chunk download task. Returns an error if the pipeline is canceled.
func (p *LecturePipeline) SubmitDownload(task ChunkTask) error {
	if p.submissionsClosed.Load() || p.ctx.Err() != nil {
		return errPipelineCancelled
	}
	select {
	case <-p.ctx.Done():
		return errPipelineCancelled
	case p.downloadQueue <- task:
		return nil
	}
}

// FinishSubmission marks submission as complete and records the total expected chunk count.
func (p *LecturePipeline) FinishSubmission(totalChunks int) {
	p.mu.Lock()
	p.totalChunks = totalChunks
	p.mu.Unlock()
	p.submissionsClosed.Store(true)
	close(p.downloadQueue)
}

// Collect waits for all workers to finish and returns the ordered pipeline result.
func (p *LecturePipeline) Collect() PipelineResult {
	for decrypted := range p.decryptedChunks {
		p.mu.Lock()
		if decrypted.Err != nil {
			p.failedChunks = append(p.failedChunks, decrypted.ChunkID)
		} else if decrypted.View == "first" {
			p.firstViewMap[decrypted.ChunkID] = decrypted.DecryptedPath
		} else if decrypted.View == "second" {
			p.secondViewMap[decrypted.ChunkID] = decrypted.DecryptedPath
		}
		p.mu.Unlock()
	}

	return PipelineResult{
		FirstViewChunks:  p.buildOrderedList(p.firstViewMap),
		SecondViewChunks: p.buildOrderedList(p.secondViewMap),
		TotalTime:        time.Since(p.startTime),
		FailedChunks:     p.failedChunks,
	}
}

func (p *LecturePipeline) buildOrderedList(chunkMap map[int]string) []string {
	if len(chunkMap) == 0 {
		return []string{}
	}

	maxID := 0
	for id := range chunkMap {
		if id > maxID {
			maxID = id
		}
	}

	orderedList := make([]string, 0, len(chunkMap))
	for i := 0; i <= maxID; i++ {
		if path, ok := chunkMap[i]; ok {
			orderedList = append(orderedList, path)
		}
	}

	return orderedList
}

func (p *LecturePipeline) cancelPipeline() {
	p.submissionsClosed.Store(true)
	p.cancel()
}

// GetStats returns a snapshot of the current pipeline progress.
func (p *LecturePipeline) GetStats() PipelineStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	return PipelineStats{
		TotalChunks:      p.totalChunks,
		FirstViewChunks:  len(p.firstViewMap),
		SecondViewChunks: len(p.secondViewMap),
		FailedChunks:     len(p.failedChunks),
		ElapsedTime:      time.Since(p.startTime),
		DownloadWorkers:  p.config.DownloadWorkers,
		DecryptWorkers:   p.config.DecryptWorkers,
	}
}
