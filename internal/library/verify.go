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
)

// VerifyOptions controls expensive verification work.
type VerifyOptions struct {
	Hash bool
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
}

// VerifyArtifact checks every known path and updates presence/hash metadata.
// It never deletes an artifact or file row.
func (store *Store) VerifyArtifact(ctx context.Context, artifactID string, options VerifyOptions) (Verification, error) {
	record, err := store.GetArtifact(ctx, artifactID)
	if err != nil {
		return Verification{}, err
	}
	checkedAt := time.Now().UTC()
	result := Verification{
		ArtifactID: record.Manifest.ArtifactID,
		OK:         true,
		CheckedAt:  checkedAt,
		Files:      make([]FileVerification, 0, len(record.Files)),
	}
	for _, file := range record.Files {
		verification := verifyArtifactFile(file, options)
		if verification.Status != FilePresent {
			result.OK = false
		}
		result.Files = append(result.Files, verification)
	}
	if err := store.recordVerification(ctx, result); err != nil {
		return Verification{}, err
	}
	return result, nil
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
	if options.Hash {
		actual, hashErr := hashFile(opened)
		if hashErr != nil {
			result.Status = FileUnreadable
			result.Error = hashErr.Error()
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

func hashFile(file io.Reader) (string, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
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
