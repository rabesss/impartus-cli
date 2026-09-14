package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
)

var validateStableArtifactFile = artifact.ValidateStableCompletedFile

// FileStatus describes the on-disk validity of one materialized path.
type FileStatus string

const (
	// FilePresent matches its persisted size and optional hash.
	FilePresent FileStatus = "present"
	// FileMissing no longer exists at its persisted path.
	FileMissing FileStatus = "missing"
	// FileNotRegular exists but is not a regular file.
	FileNotRegular FileStatus = "not_regular"
	// FileSizeMismatch differs from its recorded byte count.
	FileSizeMismatch FileStatus = "size_mismatch"
	// FileHashMismatch differs from its recorded SHA-256.
	FileHashMismatch FileStatus = "hash_mismatch"
	// FileUnreadable could not be inspected or hashed.
	FileUnreadable FileStatus = "unreadable"
	// FileContainerMismatch exists but does not match its recorded container.
	FileContainerMismatch FileStatus = "container_mismatch"
)

// VerifyOptions controls expensive verification work.
type VerifyOptions struct {
	Hash                  bool
	Container             bool
	LatestMaterialization bool
}

// FileVerification is the result for one persisted materialization.
type FileVerification struct {
	Path          string     `json:"path"`
	Status        FileStatus `json:"status"`
	ExpectedBytes int64      `json:"expectedBytes"`
	ActualBytes   int64      `json:"actualBytes,omitempty"`
	SHA256        string     `json:"sha256,omitempty"`
	Error         string     `json:"error,omitempty"`
}

// Verification reports one non-destructive artifact verification pass.
type Verification struct {
	ArtifactID string             `json:"artifactId"`
	OK         bool               `json:"ok"`
	CheckedAt  time.Time          `json:"checkedAt"`
	Files      []FileVerification `json:"files"`
	Manifest   artifact.Manifest  `json:"-"`
}

// VerifyArtifact checks persisted paths and updates presence/hash metadata.
// LatestMaterialization limits the check to files named in the current manifest.
// It never deletes an artifact or file row.
func (store *Store) VerifyArtifact(ctx context.Context, artifactID string, options VerifyOptions) (Verification, error) {
	record, err := store.GetArtifact(ctx, artifactID)
	if err != nil {
		return Verification{}, err
	}
	files, persisted := filesForVerification(record, options)
	checkedAt := time.Now().UTC()
	result := Verification{
		ArtifactID: record.Manifest.ArtifactID,
		OK:         true,
		CheckedAt:  checkedAt,
		Files:      make([]FileVerification, 0, len(files)),
		Manifest:   record.Manifest,
	}
	recorded := Verification{
		ArtifactID: record.Manifest.ArtifactID,
		CheckedAt:  checkedAt,
		Files:      make([]FileVerification, 0, len(persisted)),
	}
	for index, file := range files {
		verification := verifyArtifactFile(file, options)
		if verification.Status != FilePresent {
			result.OK = false
		}
		result.Files = append(result.Files, verification)
		if persisted[index] {
			recorded.Files = append(recorded.Files, verification)
		}
	}
	if err := store.recordVerification(ctx, recorded); err != nil {
		return Verification{}, err
	}
	return result, nil
}

func filesForVerification(record ArtifactRecord, options VerifyOptions) ([]ArtifactFile, []bool) {
	if !options.LatestMaterialization {
		persisted := make([]bool, len(record.Files))
		for index := range persisted {
			persisted[index] = true
		}
		return record.Files, persisted
	}
	byPath := make(map[string]ArtifactFile, len(record.Files))
	for _, file := range record.Files {
		byPath[file.Path] = file
	}
	files := make([]ArtifactFile, 0, len(record.Manifest.Files))
	persisted := make([]bool, 0, len(record.Manifest.Files))
	for _, spec := range record.Manifest.Files {
		file, ok := byPath[spec.Path]
		if !ok {
			files = append(files, ArtifactFile{
				Path:      spec.Path,
				Role:      spec.Role,
				View:      spec.View,
				Container: spec.Container,
				Bytes:     spec.Bytes,
				SHA256:    spec.SHA256,
			})
			persisted = append(persisted, false)
			continue
		}
		files = append(files, file)
		persisted = append(persisted, true)
	}
	return files, persisted
}

// VerifyAll checks every artifact without deleting missing materializations.
func (store *Store) VerifyAll(ctx context.Context, options VerifyOptions) ([]Verification, error) {
	records, err := store.ListArtifacts(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]Verification, 0, len(records))
	for _, record := range records {
		verified, err := store.VerifyArtifact(ctx, record.Manifest.ArtifactID, options)
		if err != nil {
			return nil, err
		}
		results = append(results, verified)
	}
	return results, nil
}

func verifyArtifactFile(file ArtifactFile, options VerifyOptions) FileVerification {
	result := FileVerification{Path: file.Path, ExpectedBytes: file.Bytes, SHA256: file.SHA256}
	pathInfo, err := os.Lstat(file.Path)
	if errors.Is(err, os.ErrNotExist) {
		result.Status = FileMissing
		return result
	}
	if err != nil {
		result.Status = FileUnreadable
		result.Error = err.Error()
		return result
	}
	result.ActualBytes = pathInfo.Size()
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		result.Status = FileNotRegular
		return result
	}
	opened, openErr := artifact.OpenCompletedFileDescriptor(file.Path)
	if openErr != nil {
		result.Status = FileUnreadable
		result.Error = openErr.Error()
		return result
	}
	defer closeFile(opened)
	openedInfo, statErr := opened.Stat()
	if statErr != nil {
		result.Status = FileUnreadable
		result.Error = statErr.Error()
		return result
	}
	result.ActualBytes = openedInfo.Size()
	if !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		result.Status = FileNotRegular
		return result
	}
	if openedInfo.Size() != file.Bytes {
		result.Status = FileSizeMismatch
		return result
	}
	return finishArtifactFileVerification(result, file, opened, pathInfo, options)
}

func finishArtifactFileVerification(result FileVerification, file ArtifactFile, opened *os.File, pathInfo os.FileInfo, options VerifyOptions) FileVerification {
	if options.Container {
		if containerErr := artifact.VerifyContainerSignature(opened, file.Path, file.Container); containerErr != nil {
			result.Status = FileContainerMismatch
			result.Error = containerErr.Error()
			return result
		}
	}
	if options.Hash {
		actual, read, hashErr := hashFile(opened)
		if hashErr != nil {
			result.Status = FileUnreadable
			result.Error = hashErr.Error()
			return result
		}
		if read != file.Bytes {
			// The file changed size mid-read; the digest never described a
			// stable state, so it must not be published or filled.
			result.Status = FileNotRegular
			result.Error = fmt.Sprintf("hashed %d bytes, want %d: size changed while hashing", read, file.Bytes)
			return result
		}
		if file.SHA256 != "" && actual != file.SHA256 {
			result.Status = FileHashMismatch
			result.SHA256 = actual
			return result
		}
		result.SHA256 = actual
	}
	if stableErr := validateStableArtifactFile(file.Path, opened, pathInfo); stableErr != nil {
		result.Status = FileNotRegular
		result.SHA256 = file.SHA256
		result.Error = stableErr.Error()
		return result
	}
	result.Status = FilePresent
	return result
}

// hashFile digests the stream and reports how many bytes the digest covers.
func hashFile(file io.Reader) (string, int64, error) {
	hasher := sha256.New()
	read, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), read, nil
}

// digestManifestFiles fills each file's empty SHA-256 from the bytes on disk
// so the recorded manifest carries a reference digest from creation. Digests
// the caller already pinned were verified by artifact.Build and are left
// untouched.
func digestManifestFiles(manifest artifact.Manifest) (artifact.Manifest, error) {
	files := append([]artifact.File(nil), manifest.Files...)
	for index, file := range files {
		if file.SHA256 != "" {
			continue
		}
		digest, err := hashStoredFile(file.Path, file.Bytes)
		if err != nil {
			return artifact.Manifest{}, fmt.Errorf("hash artifact file %q: %w", file.Path, err)
		}
		files[index].SHA256 = digest
	}
	manifest.Files = files
	return manifest, nil
}

// hashStoredFile hashes one completed file under the same rules verification
// applies: the path must still name the same regular file, the size must
// match what the validated manifest recorded, and the file must stay stable
// across the read. A swapped or growing file fails the record instead of
// persisting a digest that never described a durable state.
func hashStoredFile(path string, expectedBytes int64) (string, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	opened, err := artifact.OpenCompletedFileDescriptor(path)
	if err != nil {
		return "", err
	}
	defer closeFile(opened)
	openedInfo, err := opened.Stat()
	if err != nil {
		return "", err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		return "", errors.New("path changed during validation or is not a regular file")
	}
	if openedInfo.Size() != expectedBytes {
		return "", fmt.Errorf("size changed between validation and recording: got %d bytes, want %d", openedInfo.Size(), expectedBytes)
	}
	digest, read, err := hashFile(opened)
	if err != nil {
		return "", err
	}
	if read != expectedBytes {
		return "", fmt.Errorf("hashed %d bytes, want %d: size changed while hashing", read, expectedBytes)
	}
	if err := validateStableArtifactFile(path, opened, pathInfo); err != nil {
		return "", err
	}
	return digest, nil
}

func (store *Store) recordVerification(ctx context.Context, result Verification) error {
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin library verification update: %w", err)
	}
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)
	checkedAt := formatDatabaseTime(result.CheckedAt)
	for _, file := range result.Files {
		present := file.Status == FilePresent
		updated, err := tx.ExecContext(ctx, `
			UPDATE artifact_files
			SET present = ?,
				sha256 = CASE WHEN ? AND sha256 = '' AND ? <> '' THEN ? ELSE sha256 END,
				last_verified_at = ?,
				updated_at = ?
			WHERE artifact_id = ? AND path = ?`,
			present,
			present,
			file.SHA256,
			file.SHA256,
			checkedAt,
			checkedAt,
			result.ArtifactID,
			file.Path,
		)
		if err != nil {
			return fmt.Errorf("record artifact verification for %q: %w", file.Path, err)
		}
		rowsAffected, rowsErr := updated.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("confirm artifact verification for %q: %w", file.Path, rowsErr)
		}
		if rowsAffected != 1 {
			return fmt.Errorf("record artifact verification for %q: stored materialization changed", file.Path)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library verification: %w", err)
	}
	committed = true
	return nil
}
