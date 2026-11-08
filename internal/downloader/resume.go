package downloader

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/lockfile"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

// A failed download keeps its completed chunks in a per-lecture workspace
// under TempDirLocation, so the next attempt fetches only the missing ones.
const (
	resumeFormatVersion = 1
	resumeMetaName      = "resume.json"
	resumeLockName      = ".lock"
	resumeTTL           = 7 * 24 * time.Hour
	maxResumeMetaSize   = 4 << 10
)

var (
	resumeDirPattern = regexp.MustCompile(`^lecture-(\d+)-resume-[0-9a-f]{16}$`)
	trashDirPattern  = regexp.MustCompile(`^\.lecture-\d+-trash-[0-9a-f]{16}$`)
)

// resumeMeta is resume.json. Fingerprint identifies the playlist the chunks
// belong to; KeyHash identifies the decryption key they were decrypted with.
type resumeMeta struct {
	Version     int    `json:"version"`
	Fingerprint string `json:"fingerprint"`
	KeyHash     string `json:"keyHash,omitempty"`
}

// resumeState is the resumable workspace a download writes into.
type resumeState struct {
	dir  string
	meta resumeMeta
}

// lectureWorkspace is where one DownloadAndJoinPlaylist call keeps its
// chunks. A resumable workspace has a lock and resume state; a one-off
// fallback has neither and is always removed.
type lectureWorkspace struct {
	dir       string
	lectureID int
	lock      *lockfile.Lock
	resume    *resumeState
}

// resumeFingerprint identifies the chunks a playlist produces: lecture,
// download mode (serial and pipelined downloads name views differently), key
// URI and every segment URI, in a length-prefixed encoding.
func (d *Downloader) resumeFingerprint(playlist client.ParsedPlaylist) string {
	h := sha256.New()
	mode := "serial"
	if d.config.EnablePipeline {
		mode = "pipeline"
	}
	writeFingerprintField(h, "impartus-resume-v"+strconv.Itoa(resumeFormatVersion))
	writeFingerprintField(h, strconv.Itoa(playlist.ID))
	writeFingerprintField(h, mode)
	writeFingerprintField(h, playlist.KeyURL)
	for _, urls := range [][]string{playlist.FirstViewURLs, playlist.SecondViewURLs} {
		writeFingerprintField(h, strconv.Itoa(len(urls)))
		for _, u := range urls {
			writeFingerprintField(h, u)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeFingerprintField(h hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])         //nolint:errcheck // hash writes never fail
	_, _ = io.WriteString(h, value) //nolint:errcheck // hash writes never fail
}

// openLectureWorkspace returns the resumable workspace for playlist, or a
// one-off workspace when it cannot be used (another download of the same
// lecture holds it, or the path is not a real directory).
func (d *Downloader) openLectureWorkspace(playlist client.ParsedPlaylist) (*lectureWorkspace, error) {
	base := d.config.TempDirLocation
	if err := os.MkdirAll(base, 0o755); err != nil { // #nosec G301
		return nil, err
	}
	fingerprint := d.resumeFingerprint(playlist)
	name := fmt.Sprintf("lecture-%d-resume-%s", playlist.ID, fingerprint[:16])
	d.sweepResumeWorkspaces(base, playlist.ID, fingerprint, name, time.Now())
	workspace, reason := d.tryResumeWorkspace(filepath.Join(base, name), playlist.ID, fingerprint)
	if workspace != nil {
		return workspace, nil
	}
	d.logDiagnostic("lecture %d: not resuming earlier chunks (%s); using a one-off workspace", playlist.ID, reason)
	dir, err := os.MkdirTemp(base, fmt.Sprintf("lecture-%d-", playlist.ID))
	if err != nil {
		return nil, err
	}
	return &lectureWorkspace{dir: dir, lectureID: playlist.ID}, nil
}

func (d *Downloader) tryResumeWorkspace(dir string, lectureID int, fingerprint string) (*lectureWorkspace, string) {
	info, err := os.Lstat(dir)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, "workspace path is not a real directory"
		}
	case errors.Is(err, os.ErrNotExist):
		if mkErr := os.Mkdir(dir, 0o700); mkErr != nil && !errors.Is(mkErr, os.ErrExist) {
			return nil, secrets.ScrubError(mkErr)
		}
		if info, err = os.Lstat(dir); err != nil {
			return nil, secrets.ScrubError(err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, "workspace path is not a real directory"
		}
	default:
		return nil, secrets.ScrubError(err)
	}
	if err := validatePrivateWorkspace(info); err != nil {
		return nil, err.Error()
	}
	lockPath := filepath.Join(dir, resumeLockName)
	lock, err := lockfile.TryAcquire(lockPath)
	if errors.Is(err, lockfile.ErrLocked) {
		return nil, "another download of this lecture is using its workspace"
	}
	if err != nil {
		return nil, secrets.ScrubError(err)
	}
	// The lock may belong to a workspace another process discarded (renamed
	// away) between our open and our lock.
	if !lock.HoldsPath(lockPath) {
		_ = lock.Close() //nolint:errcheck // nothing was written under it
		return nil, "workspace was removed while opening it"
	}
	state, err := loadResumeState(dir, fingerprint)
	if err != nil {
		_ = lock.Close() //nolint:errcheck // the load error is the one to report
		return nil, secrets.ScrubError(err)
	}
	return &lectureWorkspace{dir: dir, lectureID: lectureID, lock: lock, resume: state}, ""
}

// loadResumeState reuses a workspace whose resume.json matches fingerprint
// and resets any other to empty. Writing resume.json also refreshes its
// mtime, which the TTL sweep counts from.
func loadResumeState(dir, fingerprint string) (*resumeState, error) {
	meta, _, err := readResumeMeta(dir)
	if err != nil || meta.Version != resumeFormatVersion || meta.Fingerprint != fingerprint {
		if clearErr := clearWorkspace(dir, false); clearErr != nil {
			return nil, clearErr
		}
		meta = resumeMeta{Version: resumeFormatVersion, Fingerprint: fingerprint}
	}
	state := &resumeState{dir: dir, meta: meta}
	return state, state.save()
}

func readResumeMeta(dir string) (resumeMeta, os.FileInfo, error) {
	path := filepath.Join(dir, resumeMetaName)
	info, err := os.Lstat(path)
	if err != nil {
		return resumeMeta{}, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxResumeMetaSize {
		return resumeMeta{}, nil, errors.New("resume metadata is not a small regular file")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed name inside a validated workspace
	if err != nil {
		return resumeMeta{}, nil, err
	}
	var meta resumeMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return resumeMeta{}, nil, err
	}
	return meta, info, nil
}

func (r *resumeState) save() error {
	data, err := json.Marshal(r.meta)
	if err != nil {
		return err
	}
	return writeChunkAtomically(filepath.Join(r.dir, resumeMetaName), data, true)
}

// clearWorkspace removes everything in dir except the lock and, when
// keepMeta is set, resume.json.
func clearWorkspace(dir string, keepMeta bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if name == resumeLockName || (keepMeta && name == resumeMetaName) {
			continue
		}
		errs = append(errs, os.RemoveAll(filepath.Join(dir, name)))
	}
	return errors.Join(errs...)
}

// verifyKey runs after the key fetch and before any chunk is reused. Chunks
// decrypted with a different key are discarded.
func (r *resumeState) verifyKey(key []byte) (discarded bool, err error) {
	h := sha256.New()
	_, _ = io.WriteString(h, "impartus-resume-key-v1\x00") //nolint:errcheck // hash writes never fail
	_, _ = h.Write(key)                                    //nolint:errcheck // hash writes never fail
	keyHash := hex.EncodeToString(h.Sum(nil))
	if r.meta.KeyHash == keyHash {
		return false, nil
	}
	discarded = r.meta.KeyHash != ""
	if err := clearWorkspace(r.dir, true); err != nil {
		return discarded, err
	}
	r.meta.KeyHash = keyHash
	return discarded, r.save()
}

// completedChunk returns a chunk an earlier attempt finished in this
// resumable workspace. Final chunk files are only ever created by rename
// once complete, so a non-empty regular file is a finished chunk.
func (d *Downloader) completedChunk(id int, view string, chunk int) (string, int64, bool) {
	if d.resume == nil {
		return "", 0, false
	}
	path := d.chunkPath(id, view, chunk)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", 0, false
	}
	return path, info.Size(), true
}

// close keeps a resumable workspace after a failure and removes it after
// success. A one-off workspace is always removed.
func (w *lectureWorkspace) close(d *Downloader, succeeded bool) {
	if w.lock == nil {
		if err := os.RemoveAll(w.dir); err != nil {
			d.logDiagnostic("warning: failed to remove temporary workspace for lecture %d: %s", w.lectureID, secrets.ScrubError(err))
		}
		return
	}
	if !succeeded {
		if err := w.lock.Close(); err != nil {
			d.logDiagnostic("warning: failed to release workspace lock for lecture %d: %s", w.lectureID, secrets.ScrubError(err))
		}
		return
	}
	d.discardWorkspace(w.dir, w.lectureID, w.lock)
}

// discardWorkspace renames a locked workspace out of the way before
// releasing the lock, so it never deletes a workspace another download has
// just created at the same path, then removes it.
func (d *Downloader) discardWorkspace(dir string, lectureID int, lock *lockfile.Lock) {
	trash, err := trashPath(filepath.Dir(dir), lectureID)
	if err != nil {
		_ = lock.Close() //nolint:errcheck // leaving the workspace for the sweep
		d.logDiagnostic("warning: failed to remove workspace for lecture %d: %s", lectureID, secrets.ScrubError(err))
		return
	}
	renameErr := os.Rename(dir, trash)
	if renameErr != nil {
		// Windows refuses to rename a directory holding an open file. Empty
		// it while still holding the lock instead; the small remainder is
		// swept once it has been unused for resumeTTL.
		if clearErr := clearWorkspace(dir, true); clearErr != nil {
			d.logDiagnostic("warning: failed to remove workspace for lecture %d: %s", lectureID, secrets.ScrubError(clearErr))
		}
	}
	if closeErr := lock.Close(); closeErr != nil {
		d.logDiagnostic("warning: failed to release workspace lock for lecture %d: %s", lectureID, secrets.ScrubError(closeErr))
	}
	if renameErr != nil {
		return
	}
	if err := os.RemoveAll(trash); err != nil {
		d.logDiagnostic("warning: failed to remove workspace for lecture %d: %s", lectureID, secrets.ScrubError(err))
	}
}

func trashPath(base string, lectureID int) (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return filepath.Join(base, fmt.Sprintf(".lecture-%d-trash-%s", lectureID, hex.EncodeToString(suffix[:]))), nil
}

// sweepResumeWorkspaces removes leftover trash, workspaces unused for
// resumeTTL and this lecture's workspaces for an older playlist. It skips the
// current workspace, symlinks, directories without valid resume metadata and
// workspaces another download holds. Failures are diagnostics only.
func (d *Downloader) sweepResumeWorkspaces(base string, lectureID int, fingerprint, current string, now time.Time) {
	entries, err := os.ReadDir(base)
	if err != nil {
		d.logDiagnostic("warning: failed to scan workspaces: %s", secrets.ScrubError(err))
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == current { // IsDir is false for symlinks
			continue
		}
		dir := filepath.Join(base, name)
		info, err := entry.Info()
		if err != nil || validatePrivateWorkspace(info) != nil {
			continue // not ours to remove
		}
		if trashDirPattern.MatchString(name) {
			if err := os.RemoveAll(dir); err != nil {
				d.logDiagnostic("warning: failed to remove discarded workspace: %s", secrets.ScrubError(err))
			}
			continue
		}
		match := resumeDirPattern.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		removable := func() bool {
			meta, metaInfo, err := readResumeMeta(dir)
			if err != nil {
				return false
			}
			stale := now.Sub(metaInfo.ModTime()) > resumeTTL
			replaced := match[1] == strconv.Itoa(lectureID) && meta.Fingerprint != fingerprint
			return stale || replaced
		}
		if !removable() {
			continue
		}
		lockPath := filepath.Join(dir, resumeLockName)
		lock, err := lockfile.TryAcquire(lockPath)
		if err != nil {
			continue // in use, or not lockable: leave it
		}
		// Recheck under the lock: another download may have moved the
		// workspace away, or used and refreshed it, since the first check.
		if !lock.HoldsPath(lockPath) || !removable() {
			_ = lock.Close() //nolint:errcheck // nothing was changed under it
			continue
		}
		owner, _ := strconv.Atoi(match[1]) //nolint:errcheck // the pattern guarantees digits
		d.discardWorkspace(dir, owner, lock)
	}
}
