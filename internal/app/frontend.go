package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/buildinfo"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/downloader"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/player"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

// PlaybackSession is the transport-neutral playback surface consumed by the
// terminal UI. The concrete implementation continues to own mpv and its
// loopback stream capability.
type PlaybackSession interface {
	Events() <-chan player.Event
	WaitForEnd(context.Context) error
	Pause(context.Context, bool) error
	SeekRelative(context.Context, float64) error
	SeekAbsolute(context.Context, float64) error
	SetVolume(context.Context, float64) error
	SetMute(context.Context, bool) error
	SetSpeed(context.Context, float64) error
	CycleVideo(context.Context) error
	Close(context.Context) error
}

// DownloadResult is the application-level result of one interactive lecture
// download. LibraryRecorded is false only when media completed but the local
// library commit failed.
type DownloadResult struct {
	Manifest        artifact.Manifest
	LibraryRecorded bool
	Warning         string
}

// StartLecture resolves one live lecture and starts supervised playback. A
// positive resume position is applied only after the capability URL is loaded.
func (service *Service) StartLecture(ctx context.Context, lecture client.Lecture, resumeSeconds float64) (PlaybackSession, error) {
	if service == nil || service.streams == nil {
		return nil, errors.New("application playback service is not configured")
	}
	if resumeSeconds < 0 || math.IsNaN(resumeSeconds) || math.IsInf(resumeSeconds, 0) {
		return nil, errors.New("resume position must be finite and non-negative")
	}
	playlists, err := service.streams.FetchLecturePlaylists(ctx, client.Lectures{lecture})
	if err != nil {
		return nil, fmt.Errorf("resolve lecture playlist: %w", err)
	}
	if len(playlists) != 1 {
		return nil, fmt.Errorf("expected one playable lecture, got %d", len(playlists))
	}
	playback, err := service.StartPlayback(ctx, playlists[0])
	if err != nil {
		return nil, err
	}
	if resumeSeconds > 0 {
		if err := waitForPlaybackReady(ctx, playback); err != nil {
			closeErr := playback.Close(context.Background())
			return nil, errors.Join(fmt.Errorf("resume lecture playback: wait for media readiness: %w", err), closeErr)
		}
		if err := playback.SeekAbsolute(ctx, resumeSeconds); err != nil {
			closeErr := playback.Close(context.Background())
			return nil, errors.Join(fmt.Errorf("resume lecture playback: %w", err), closeErr)
		}
	}
	return playback, nil
}

func waitForPlaybackReady(ctx context.Context, playback PlaybackSession) error {
	for {
		select {
		case event, open := <-playback.Events():
			if !open {
				if waitErr := playback.WaitForEnd(ctx); waitErr != nil {
					return waitErr
				}
				return errors.New("playback ended before media became ready")
			}
			ready, terminal, eventErr := playbackReadiness(event)
			if eventErr != nil {
				return eventErr
			}
			if ready {
				return nil
			}
			if terminal {
				if waitErr := playback.WaitForEnd(ctx); waitErr != nil {
					return waitErr
				}
				return errors.New("playback ended before media became ready")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func playbackReadiness(event player.Event) (ready, terminal bool, err error) {
	if event.Name == "end-file" {
		return false, event.Reason != "redirect", nil
	}
	if event.Name != "property-change" {
		return false, false, nil
	}
	switch event.Property {
	case "duration":
		if len(event.Data) == 0 || string(event.Data) == "null" {
			return false, false, nil
		}
		var duration float64
		if err := json.Unmarshal(event.Data, &duration); err != nil {
			return false, false, fmt.Errorf("decode playback duration: %w", err)
		}
		return duration > 0, false, nil
	case "eof-reached":
		if len(event.Data) == 0 || string(event.Data) == "null" {
			return false, false, nil
		}
		var reached bool
		if err := json.Unmarshal(event.Data, &reached); err != nil {
			return false, false, fmt.Errorf("decode playback eof state: %w", err)
		}
		return false, reached, nil
	default:
		return false, false, nil
	}
}

// ResumeLecture resolves the canonical artifact identity for the configured
// media selection and returns its durable checkpoint when that artifact has
// already been recorded locally.
func (service *Service) ResumeLecture(ctx context.Context, lecture client.Lecture) (library.PlaybackState, bool, error) {
	if service == nil || service.config == nil || service.history == nil {
		return library.PlaybackState{}, false, nil
	}
	artifactID, err := artifact.NewID(artifact.Identity{
		InstituteID: lecture.InstituteID,
		SubjectID:   lecture.SubjectID,
		SessionID:   lecture.SessionID,
		TTID:        lecture.TTID,
		AudioOnly:   service.config.AudioOnly,
		Views:       service.config.Views,
		Quality:     service.config.Quality,
		AudioFormat: service.config.AudioFormat,
	})
	if err != nil {
		return library.PlaybackState{}, false, fmt.Errorf("resolve lecture artifact identity: %w", err)
	}
	state, found, err := service.Resume(ctx, artifactID)
	if err != nil || found {
		return state, found, err
	}
	if service.library == nil {
		return library.PlaybackState{}, false, nil
	}
	if _, err := service.library.GetArtifact(ctx, artifactID); err != nil {
		if errors.Is(err, library.ErrArtifactNotFound) {
			return library.PlaybackState{}, false, nil
		}
		return library.PlaybackState{}, false, fmt.Errorf("check lecture artifact: %w", err)
	}
	return library.PlaybackState{ArtifactID: artifactID}, false, nil
}

// DownloadLecture downloads, decrypts, and joins exactly one selected lecture,
// then best-effort commits its completed artifact to the local library.
func (service *Service) DownloadLecture(ctx context.Context, lecture client.Lecture) (DownloadResult, error) {
	if service == nil || service.config == nil || service.downloads == nil {
		return DownloadResult{}, errors.New("application download service is not configured")
	}
	// Download output is user-owned rather than secret state.
	if err := os.MkdirAll(service.config.DownloadLocation, 0o755); err != nil { // #nosec G301
		return DownloadResult{}, fmt.Errorf("prepare download directory: %w", err)
	}
	playlists, err := service.downloads.FetchLecturePlaylists(ctx, client.Lectures{lecture})
	if err != nil {
		return DownloadResult{}, fmt.Errorf("resolve lecture playlist: %w", err)
	}
	if len(playlists) != 1 {
		return DownloadResult{}, fmt.Errorf("expected one downloadable lecture, got %d", len(playlists))
	}
	producedAt := time.Now().UTC()
	jobID := ""
	if service.library != nil {
		plan, planErr := downloader.PlanJoinResult(service.config, playlists[0])
		if planErr != nil {
			return DownloadResult{}, fmt.Errorf("plan downloaded lecture artifact: %w", planErr)
		}
		jobID = uuid.NewString()
		expected := buildExpectedDownloadArtifact(lecture, service.config, plan, producedAt)
		if createErr := service.library.CreateJob(context.WithoutCancel(ctx), library.JobSpec{ID: jobID, Kind: "download", Expected: expected}); createErr != nil {
			return DownloadResult{}, fmt.Errorf("create durable download job: %w", createErr)
		}
		if startErr := service.library.StartJob(context.WithoutCancel(ctx), jobID); startErr != nil {
			failErr := service.library.FailJob(context.WithoutCancel(ctx), jobID, startErr)
			return DownloadResult{}, errors.Join(fmt.Errorf("start durable download job: %w", startErr), failErr)
		}
	}
	joined, err := service.downloads.DownloadAndJoin(ctx, playlists[0])
	if err != nil {
		return DownloadResult{}, errors.Join(err, service.finishDownloadJob(ctx, jobID, err))
	}
	manifest, err := BuildDownloadArtifact(lecture, service.config, joined, producedAt)
	if err != nil {
		buildErr := fmt.Errorf("build downloaded lecture artifact: %w", err)
		return DownloadResult{}, errors.Join(buildErr, service.finishDownloadJob(ctx, jobID, buildErr))
	}
	result := DownloadResult{Manifest: manifest}
	if service.library == nil {
		result.Warning = "download completed but the local library is unavailable"
		return result, nil
	}
	// Once final media is published, retain its artifact record even if the UI
	// is closing. The TUI waits for this application command before closing the
	// store, so this uncancelable commit cannot race library shutdown.
	if err := service.library.CompleteJob(context.WithoutCancel(ctx), jobID, manifest); err != nil {
		result.Warning = "download completed but the local library was not updated: " + secrets.ScrubError(err)
		return result, nil
	}
	result.LibraryRecorded = true
	return result, nil
}

func (service *Service) finishDownloadJob(ctx context.Context, jobID string, cause error) error {
	if service.library == nil || jobID == "" || cause == nil {
		return nil
	}
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return service.library.CancelJob(context.WithoutCancel(ctx), jobID)
	}
	return service.library.FailJob(context.WithoutCancel(ctx), jobID, cause)
}

// Artifacts returns the current durable local library for the TUI.
func (service *Service) Artifacts(ctx context.Context) ([]library.ArtifactRecord, error) {
	if service == nil || service.library == nil {
		return nil, errors.New("application artifact library is not configured")
	}
	return service.library.ListArtifacts(ctx)
}

// BuildDownloadArtifact maps typed joined outputs to artifact manifest v1. It
// is shared by the CLI and TUI so both frontends produce identical contracts.
func BuildDownloadArtifact(lecture client.Lecture, cfg *config.Config, result downloader.JoinResult, producedAt time.Time) (artifact.Manifest, error) {
	if cfg == nil {
		return artifact.Manifest{}, errors.New("download configuration is required")
	}
	role := "video"
	if cfg.AudioOnly {
		role = "audio"
	}
	fileSpecs := make([]artifact.FileSpec, 0, 3)
	for _, output := range result.Outputs() {
		fileSpecs = append(fileSpecs, artifact.FileSpec{Path: output.Path, Role: role, View: output.View, Container: output.Container})
	}
	return artifact.Build(artifact.BuildInput{
		Lecture:   downloadArtifactLecture(lecture),
		Selection: downloadArtifactSelection(cfg),
		Files:     fileSpecs, ProducedAt: producedAt,
		Producer: artifact.Producer{Name: "impartus", Version: buildinfo.Version},
	})
}

func buildExpectedDownloadArtifact(lecture client.Lecture, cfg *config.Config, result downloader.JoinResult, producedAt time.Time) library.ExpectedArtifact {
	role := "video"
	if cfg.AudioOnly {
		role = "audio"
	}
	files := make([]library.ExpectedFile, 0, len(result.Outputs()))
	for _, output := range result.Outputs() {
		files = append(files, library.ExpectedFile{Path: output.Path, Role: role, View: output.View, Container: output.Container})
	}
	return library.ExpectedArtifact{
		Lecture: downloadArtifactLecture(lecture), Selection: downloadArtifactSelection(cfg), Files: files,
		ProducedAt: producedAt, Producer: artifact.Producer{Name: "impartus", Version: buildinfo.Version},
	}
}

func downloadArtifactLecture(lecture client.Lecture) artifact.Lecture {
	return artifact.Lecture{
		TTID: lecture.TTID, InstituteID: lecture.InstituteID, SubjectID: lecture.SubjectID,
		SessionID: lecture.SessionID, SeqNo: lecture.SeqNo, Topic: lecture.Topic,
		StartTime: lecture.StartTime, DurationSeconds: lecture.ActualDuration,
		Professor: lecture.ProfessorName, Institute: lecture.Institute, NoAudio: lecture.NoAudio == 1,
	}
}

func downloadArtifactSelection(cfg *config.Config) artifact.Selection {
	return artifact.Selection{Views: cfg.Views, Quality: cfg.Quality, AudioOnly: cfg.AudioOnly, AudioFormat: cfg.AudioFormat}
}
