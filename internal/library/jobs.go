package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

// JobStatus is the durable lifecycle state for a local download job.
type JobStatus string

const (
	// JobPending has durable metadata but has not begun an attempt.
	JobPending JobStatus = "pending"
	// JobRunning is actively producing its expected final outputs.
	JobRunning JobStatus = "running"
	// JobRecoverable was interrupted and can be inspected or retried.
	JobRecoverable JobStatus = "recoverable"
	// JobCompleted has atomically committed a verified artifact.
	JobCompleted JobStatus = "completed"
	// JobFailed ended with a sanitized failure summary.
	JobFailed JobStatus = "failed"
	// JobCanceled was explicitly canceled before completion.
	JobCanceled JobStatus = "canceled"
)

var (
	// ErrJobNotFound reports an unknown local-library job ID.
	ErrJobNotFound = errors.New("library job not found")
	// ErrJobTerminal reports a requested transition from a terminal state.
	ErrJobTerminal = errors.New("library job is already terminal")
	// ErrJobTransition reports a lifecycle operation invalid for the current state.
	ErrJobTransition = errors.New("invalid library job transition")
)

// ExpectedFile records a final output before media creation starts. Partial
// workspace paths are deliberately outside this contract.
type ExpectedFile struct {
	Path      string `json:"path"`
	Role      string `json:"role"`
	View      string `json:"view"`
	Container string `json:"container"`
	SHA256    string `json:"sha256,omitempty"`
}

// ExpectedArtifact is sufficient to rebuild and validate a completed manifest
// after a process crash, without another Impartus request.
type ExpectedArtifact struct {
	Lecture    artifact.Lecture   `json:"lecture"`
	Selection  artifact.Selection `json:"selection"`
	Files      []ExpectedFile     `json:"files"`
	ProducedAt time.Time          `json:"producedAt"`
	Producer   artifact.Producer  `json:"producer"`
}

// JobSpec creates one durable job before final-output work begins.
type JobSpec struct {
	ID       string
	Kind     string
	Expected ExpectedArtifact
}

// Job is the public durable representation of a local download job.
type Job struct {
	ID                  string           `json:"id"`
	Kind                string           `json:"kind"`
	Status              JobStatus        `json:"status"`
	LogicalArtifactID   string           `json:"logicalArtifactId"`
	CompletedArtifactID string           `json:"completedArtifactId,omitempty"`
	Expected            ExpectedArtifact `json:"expected"`
	Attempts            int              `json:"attempts"`
	ErrorSummary        string           `json:"errorSummary,omitempty"`
	CreatedAt           time.Time        `json:"createdAt"`
	StartedAt           *time.Time       `json:"startedAt,omitempty"`
	FinishedAt          *time.Time       `json:"finishedAt,omitempty"`
	UpdatedAt           time.Time        `json:"updatedAt"`
}

// RecoveryResult separates complete output sets from jobs that still require
// a future retry. Recovery performs no network work.
type RecoveryResult struct {
	Recovered []string `json:"recovered"`
	Pending   []string `json:"pending"`
	Skipped   []string `json:"skipped,omitempty"`
}

// CreateJob durably records expected final paths before download work starts.
func (store *Store) CreateJob(ctx context.Context, spec JobSpec) error {
	if store == nil || store.database == nil {
		return errors.New("library store is closed")
	}
	parsedID, err := uuid.Parse(strings.TrimSpace(spec.ID))
	if err != nil || parsedID.Version() != 4 {
		return errors.New("job ID must be a UUIDv4")
	}
	spec.ID = parsedID.String()
	spec.Kind = strings.ToLower(strings.TrimSpace(spec.Kind))
	if spec.Kind != "download" && spec.Kind != "watch" {
		return errors.New("job kind must be download or watch")
	}
	expected, artifactID, err := normalizeExpectedArtifact(spec.Expected)
	if err != nil {
		return err
	}
	encodedExpected, err := json.Marshal(expected)
	if err != nil {
		return fmt.Errorf("encode expected artifact: %w", err)
	}
	now := formatDatabaseTime(time.Now())
	_, err = store.database.ExecContext(ctx, `
		INSERT INTO jobs (
			job_id, kind, status, logical_artifact_id, expected_artifact_json,
			attempts, error_summary, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 0, '', ?, ?)`,
		spec.ID,
		spec.Kind,
		JobPending,
		artifactID,
		string(encodedExpected),
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("create library job %s: %w", spec.ID, err)
	}
	return nil
}

// StartJob transitions pending/recoverable work to running and counts an attempt.
func (store *Store) StartJob(ctx context.Context, jobID string) error {
	if store == nil || store.database == nil {
		return errors.New("library store is closed")
	}
	now := formatDatabaseTime(time.Now())
	result, err := store.database.ExecContext(ctx, `
		UPDATE jobs SET status = ?, attempts = attempts + 1, started_at = ?,
			finished_at = NULL, error_summary = '', updated_at = ?
		WHERE job_id = ? AND status IN (?, ?)`,
		JobRunning,
		now,
		now,
		strings.TrimSpace(jobID),
		JobPending,
		JobRecoverable,
	)
	if err != nil {
		return fmt.Errorf("start library job %s: %w", jobID, err)
	}
	return requireJobTransition(ctx, store, jobID, result)
}

// FailJob records a sanitized terminal failure.
func (store *Store) FailJob(ctx context.Context, jobID string, cause error) error {
	if cause == nil {
		return errors.New("job failure cause is required")
	}
	return store.finishJob(ctx, jobID, JobFailed, sanitizedJobSummary(secrets.ScrubError(cause)))
}

// CancelJob records an explicit terminal cancellation.
func (store *Store) CancelJob(ctx context.Context, jobID string) error {
	return store.finishJob(ctx, jobID, JobCanceled, "")
}

func (store *Store) finishJob(ctx context.Context, jobID string, status JobStatus, summary string) error {
	if store == nil || store.database == nil {
		return errors.New("library store is closed")
	}
	now := formatDatabaseTime(time.Now())
	result, err := store.database.ExecContext(ctx, `
		UPDATE jobs SET status = ?, error_summary = ?, finished_at = ?, updated_at = ?
		WHERE job_id = ? AND status IN (?, ?, ?)`,
		status,
		summary,
		now,
		now,
		strings.TrimSpace(jobID),
		JobPending,
		JobRunning,
		JobRecoverable,
	)
	if err != nil {
		return fmt.Errorf("finish library job %s: %w", jobID, err)
	}
	return requireJobTransition(ctx, store, jobID, result)
}

// CompleteJob atomically records a verified artifact and completes its job.
func (store *Store) CompleteJob(ctx context.Context, jobID string, manifest artifact.Manifest) error {
	if store == nil || store.database == nil {
		return errors.New("library store is closed")
	}
	validated, validationErr := validateCompletedManifest(manifest)
	if validationErr != nil {
		return validationErr
	}
	tx, beginErr := store.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return fmt.Errorf("begin job completion: %w", beginErr)
	}
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)
	expectation, loadErr := loadJobCompletionExpectation(ctx, tx, jobID)
	if loadErr != nil {
		return loadErr
	}
	alreadyCompleted, expectationErr := expectation.validateCompletion(validated)
	if expectationErr != nil {
		return expectationErr
	}
	if alreadyCompleted {
		if commitErr := tx.Commit(); commitErr != nil {
			return commitErr
		}
		committed = true
		return nil
	}
	if recordErr := recordManifestTx(ctx, tx, validated); recordErr != nil {
		return recordErr
	}
	now := formatDatabaseTime(time.Now())
	transition, transitionErr := tx.ExecContext(ctx, `
		UPDATE jobs SET status = ?, completed_artifact_id = ?, error_summary = '',
			finished_at = ?, updated_at = ?
		WHERE job_id = ? AND status IN (?, ?)`,
		JobCompleted,
		validated.ArtifactID,
		now,
		now,
		strings.TrimSpace(jobID),
		JobRunning,
		JobRecoverable,
	)
	if transitionErr != nil {
		return fmt.Errorf("complete library job: %w", transitionErr)
	}
	rowsAffected, rowsErr := transition.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("confirm library job completion: %w", rowsErr)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("%w: job changed while completion was committing", ErrJobTransition)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("commit job completion: %w", commitErr)
	}
	committed = true
	return nil
}

type jobCompletionExpectation struct {
	logicalArtifactID   string
	status              JobStatus
	completedArtifactID string
	expected            ExpectedArtifact
}

func loadJobCompletionExpectation(ctx context.Context, tx *sql.Tx, jobID string) (jobCompletionExpectation, error) {
	var result jobCompletionExpectation
	var completed sql.NullString
	var encodedExpected string
	err := tx.QueryRowContext(ctx, `
		SELECT logical_artifact_id, status, completed_artifact_id, expected_artifact_json
		FROM jobs WHERE job_id = ?`, strings.TrimSpace(jobID)).Scan(
		&result.logicalArtifactID,
		&result.status,
		&completed,
		&encodedExpected,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return jobCompletionExpectation{}, ErrJobNotFound
	}
	if err != nil {
		return jobCompletionExpectation{}, fmt.Errorf("read job before completion: %w", err)
	}
	result.completedArtifactID = completed.String
	if err := json.Unmarshal([]byte(encodedExpected), &result.expected); err != nil {
		return jobCompletionExpectation{}, fmt.Errorf("decode expected job artifact: %w", err)
	}
	return result, nil
}

func (expected jobCompletionExpectation) validateLifecycle() error {
	if isTerminalJobStatus(expected.status) {
		return fmt.Errorf("%w: %s", ErrJobTerminal, expected.status)
	}
	if expected.status != JobRunning && expected.status != JobRecoverable {
		return fmt.Errorf("%w: cannot complete from %s", ErrJobTransition, expected.status)
	}
	return nil
}

func (expected jobCompletionExpectation) validateCompletion(manifest artifact.Manifest) (bool, error) {
	if err := expected.matchesManifest(manifest); err != nil {
		return false, err
	}
	if expected.status == JobCompleted && expected.completedArtifactID == manifest.ArtifactID {
		return true, nil
	}
	return false, expected.validateLifecycle()
}

func (expected jobCompletionExpectation) matchesManifest(manifest artifact.Manifest) error {
	if expected.logicalArtifactID != manifest.ArtifactID {
		return errors.New("completed manifest does not match job artifact identity")
	}
	return expected.expected.matchesManifest(manifest)
}

// RecoverInterruptedJobs marks orphaned running work recoverable, then commits
// only output sets that already pass artifact validation. The caller must hold
// the process-wide watcher lock and invoke recovery at startup before launching
// workers; this method deliberately does not guess whether another owner lives.
func (store *Store) RecoverInterruptedJobs(ctx context.Context) (RecoveryResult, error) {
	if store == nil || store.database == nil {
		return RecoveryResult{}, errors.New("library store is closed")
	}
	now := formatDatabaseTime(time.Now())
	if _, err := store.database.ExecContext(ctx, `
		UPDATE jobs SET status = ?, error_summary = ?, updated_at = ? WHERE status = ?`,
		JobRecoverable,
		"interrupted before durable completion",
		now,
		JobRunning,
	); err != nil {
		return RecoveryResult{}, fmt.Errorf("mark interrupted jobs recoverable: %w", err)
	}
	jobs, err := store.jobsByStatus(ctx, JobRecoverable)
	if err != nil {
		return RecoveryResult{}, err
	}
	result := RecoveryResult{Recovered: make([]string, 0), Pending: make([]string, 0), Skipped: make([]string, 0)}
	for _, job := range jobs {
		manifest, buildErr := job.Expected.buildManifest()
		if buildErr != nil {
			result.Pending = append(result.Pending, job.ID)
			if err := store.setRecoverableSummary(ctx, job.ID, "expected outputs are not yet valid"); err != nil {
				return RecoveryResult{}, err
			}
			continue
		}
		if err := store.CompleteJob(ctx, job.ID, manifest); err != nil {
			if errors.Is(err, ErrJobTerminal) || errors.Is(err, ErrJobTransition) || errors.Is(err, ErrJobNotFound) {
				result.Skipped = append(result.Skipped, job.ID)
				continue
			}
			return RecoveryResult{}, err
		}
		result.Recovered = append(result.Recovered, job.ID)
	}
	return result, nil
}

// Job returns one durable job.
func (store *Store) Job(ctx context.Context, jobID string) (Job, error) {
	if store == nil || store.database == nil {
		return Job{}, errors.New("library store is closed")
	}
	row := store.database.QueryRowContext(ctx, `
		SELECT job_id, kind, status, logical_artifact_id, completed_artifact_id,
			expected_artifact_json, attempts, error_summary, created_at, started_at,
			finished_at, updated_at
		FROM jobs WHERE job_id = ?`, strings.TrimSpace(jobID))
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("read library job %s: %w", jobID, err)
	}
	return job, nil
}

// ListJobs returns durable jobs newest first.
func (store *Store) ListJobs(ctx context.Context) ([]Job, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("library store is closed")
	}
	rows, err := store.database.QueryContext(ctx, `
		SELECT job_id, kind, status, logical_artifact_id, completed_artifact_id,
			expected_artifact_json, attempts, error_summary, created_at, started_at,
			finished_at, updated_at
		FROM jobs ORDER BY created_at DESC, job_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list library jobs: %w", err)
	}
	defer closeRows(rows)
	jobs := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (store *Store) jobsByStatus(ctx context.Context, status JobStatus) ([]Job, error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT job_id, kind, status, logical_artifact_id, completed_artifact_id,
			expected_artifact_json, attempts, error_summary, created_at, started_at,
			finished_at, updated_at
		FROM jobs WHERE status = ? ORDER BY created_at ASC, job_id ASC`, status)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	jobs := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var completed sql.NullString
	var encodedExpected string
	var createdAt, updatedAt string
	var startedAt, finishedAt sql.NullString
	if err := row.Scan(
		&job.ID,
		&job.Kind,
		&job.Status,
		&job.LogicalArtifactID,
		&completed,
		&encodedExpected,
		&job.Attempts,
		&job.ErrorSummary,
		&createdAt,
		&startedAt,
		&finishedAt,
		&updatedAt,
	); err != nil {
		return Job{}, err
	}
	job.CompletedArtifactID = completed.String
	if err := json.Unmarshal([]byte(encodedExpected), &job.Expected); err != nil {
		return Job{}, err
	}
	normalizedExpected, artifactID, normalizeErr := normalizeExpectedArtifact(job.Expected)
	if normalizeErr != nil {
		return Job{}, normalizeErr
	}
	if artifactID != job.LogicalArtifactID {
		return Job{}, errors.New("stored job artifact identity is inconsistent")
	}
	job.Expected = normalizedExpected
	var err error
	job.CreatedAt, err = parseDatabaseTime(createdAt)
	if err != nil {
		return Job{}, err
	}
	job.UpdatedAt, err = parseDatabaseTime(updatedAt)
	if err != nil {
		return Job{}, err
	}
	job.StartedAt, err = parseOptionalTime(startedAt)
	if err != nil {
		return Job{}, err
	}
	job.FinishedAt, err = parseOptionalTime(finishedAt)
	if err != nil {
		return Job{}, err
	}
	return job, nil
}

func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseDatabaseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func normalizeExpectedArtifact(expected ExpectedArtifact) (ExpectedArtifact, string, error) {
	expected.Selection.Views = strings.ToLower(strings.TrimSpace(expected.Selection.Views))
	switch expected.Selection.Views {
	case "first":
		expected.Selection.Views = "left"
	case "second":
		expected.Selection.Views = "right"
	}
	expected.Selection.Quality = strings.ToLower(strings.TrimSpace(expected.Selection.Quality))
	expected.Selection.AudioFormat = strings.ToLower(strings.TrimSpace(expected.Selection.AudioFormat))
	artifactID, err := artifact.NewID(artifact.Identity{
		InstituteID: expected.Lecture.InstituteID,
		SubjectID:   expected.Lecture.SubjectID,
		SessionID:   expected.Lecture.SessionID,
		TTID:        expected.Lecture.TTID,
		AudioOnly:   expected.Selection.AudioOnly,
		Views:       expected.Selection.Views,
		Quality:     expected.Selection.Quality,
		AudioFormat: expected.Selection.AudioFormat,
	})
	if err != nil {
		return ExpectedArtifact{}, "", err
	}
	if expected.ProducedAt.IsZero() {
		return ExpectedArtifact{}, "", errors.New("expected artifact producedAt is required")
	}
	expected.ProducedAt = expected.ProducedAt.UTC()
	expected.Producer.Name = strings.TrimSpace(expected.Producer.Name)
	expected.Producer.Version = strings.TrimSpace(expected.Producer.Version)
	if expected.Producer.Name == "" || expected.Producer.Version == "" {
		return ExpectedArtifact{}, "", errors.New("expected artifact producer is required")
	}
	if len(expected.Files) == 0 {
		return ExpectedArtifact{}, "", errors.New("expected artifact files are required")
	}
	seen := make(map[string]struct{}, len(expected.Files))
	for index := range expected.Files {
		file := &expected.Files[index]
		rawPath := strings.TrimSpace(file.Path)
		if rawPath == "" {
			return ExpectedArtifact{}, "", errors.New("expected output path is required")
		}
		absolute, err := filepath.Abs(filepath.Clean(rawPath))
		if err != nil {
			return ExpectedArtifact{}, "", err
		}
		if strings.ContainsRune(absolute, '\x00') {
			return ExpectedArtifact{}, "", errors.New("expected output path contains a null byte")
		}
		if strings.HasSuffix(strings.ToLower(absolute), ".part") {
			return ExpectedArtifact{}, "", errors.New("expected final output must not use a .part path")
		}
		if _, exists := seen[absolute]; exists {
			return ExpectedArtifact{}, "", errors.New("expected output paths must be unique")
		}
		seen[absolute] = struct{}{}
		file.Path = absolute
		file.Role = strings.ToLower(strings.TrimSpace(file.Role))
		file.View = strings.ToLower(strings.TrimSpace(file.View))
		file.Container = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(file.Container)), ".")
		file.SHA256 = strings.ToLower(strings.TrimSpace(file.SHA256))
		if err := validateExpectedFile(expected.Selection, *file); err != nil {
			return ExpectedArtifact{}, "", err
		}
	}
	return expected, artifactID, nil
}

func validateExpectedFile(selection artifact.Selection, file ExpectedFile) error {
	wantRole := "video"
	allowedContainers := map[string]bool{"mp4": true, "mkv": true}
	if selection.AudioOnly {
		wantRole = "audio"
		allowedContainers = map[string]bool{"mp3": true, "m4a": true, "aac": true, "opus": true}
	}
	if file.Role != wantRole {
		return fmt.Errorf("expected output role %q does not match selection role %q", file.Role, wantRole)
	}
	if file.View != "left" && file.View != "right" && file.View != "both" {
		return fmt.Errorf("unsupported expected output view %q", file.View)
	}
	if !config.IncludesOutputView(selection.Views, file.View) {
		return fmt.Errorf("expected output view %q is outside selected views %q", file.View, selection.Views)
	}
	if !allowedContainers[file.Container] {
		return fmt.Errorf("unsupported expected output container %q", file.Container)
	}
	if file.SHA256 != "" {
		decoded, err := hex.DecodeString(file.SHA256)
		if err != nil || len(decoded) != sha256.Size {
			return errors.New("expected output sha256 must be 64 hexadecimal characters")
		}
	}
	return nil
}

func (expected ExpectedArtifact) buildManifest() (artifact.Manifest, error) {
	files := make([]artifact.FileSpec, 0, len(expected.Files))
	for _, file := range expected.Files {
		files = append(files, artifact.FileSpec{
			Path:      file.Path,
			Role:      file.Role,
			View:      file.View,
			Container: file.Container,
			SHA256:    file.SHA256,
		})
	}
	return artifact.Build(artifact.BuildInput{
		Lecture:    expected.Lecture,
		Selection:  expected.Selection,
		Files:      files,
		ProducedAt: expected.ProducedAt,
		Producer:   expected.Producer,
	})
}

func (expected ExpectedArtifact) matchesManifest(manifest artifact.Manifest) error {
	if !reflect.DeepEqual(expected.Lecture, manifest.Lecture) ||
		!reflect.DeepEqual(expected.Selection, manifest.Selection) ||
		!expected.ProducedAt.Equal(manifest.ProducedAt) ||
		!reflect.DeepEqual(expected.Producer, manifest.Producer) {
		return errors.New("completed manifest metadata does not match the durable job expectation")
	}
	if len(expected.Files) != len(manifest.Files) {
		return errors.New("completed manifest file count does not match the durable job expectation")
	}
	byPath := make(map[string]artifact.File, len(manifest.Files))
	for _, file := range manifest.Files {
		byPath[file.Path] = file
	}
	for _, file := range expected.Files {
		completed, exists := byPath[file.Path]
		if !exists || completed.Role != file.Role || completed.View != file.View || completed.Container != file.Container {
			return errors.New("completed manifest files do not match the durable job expectation")
		}
		if file.SHA256 != "" && completed.SHA256 != file.SHA256 {
			return errors.New("completed manifest sha256 does not match the durable job expectation")
		}
	}
	return nil
}

func requireJobTransition(ctx context.Context, store *Store, jobID string, result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 1 {
		return nil
	}
	job, err := store.Job(ctx, jobID)
	if errors.Is(err, ErrJobNotFound) {
		return err
	}
	if err != nil {
		return err
	}
	if isTerminalJobStatus(job.Status) {
		return fmt.Errorf("%w: %s", ErrJobTerminal, job.Status)
	}
	return fmt.Errorf("%w: %s", ErrJobTransition, job.Status)
}

func (store *Store) setRecoverableSummary(ctx context.Context, jobID, summary string) error {
	summary = sanitizedJobSummary(secrets.Scrub(summary))
	_, err := store.database.ExecContext(ctx, "UPDATE jobs SET error_summary = ?, updated_at = ? WHERE job_id = ? AND status = ?", summary, formatDatabaseTime(time.Now()), jobID, JobRecoverable)
	return err
}

func sanitizedJobSummary(summary string) string {
	characters := []rune(summary)
	if len(characters) > 256 {
		return string(characters[:256])
	}
	return summary
}

func isTerminalJobStatus(status JobStatus) bool {
	return status == JobCompleted || status == JobFailed || status == JobCanceled
}
