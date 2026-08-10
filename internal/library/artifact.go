package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
)

// ErrArtifactNotFound reports an unknown logical artifact ID.
var ErrArtifactNotFound = errors.New("library artifact not found")

// ArtifactFile is one persisted materialization of a logical artifact.
type ArtifactFile struct {
	Path           string     `json:"path"`
	Role           string     `json:"role"`
	View           string     `json:"view"`
	Container      string     `json:"container"`
	Bytes          int64      `json:"bytes"`
	SHA256         string     `json:"sha256,omitempty"`
	Present        bool       `json:"present"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`
}

// ArtifactRecord combines the latest manifest metadata with every path ever
// materialized for that logical artifact.
type ArtifactRecord struct {
	Manifest artifact.Manifest `json:"manifest"`
	Files    []ArtifactFile    `json:"files"`
}

// RecordManifest idempotently commits one completed manifest.
func (store *Store) RecordManifest(ctx context.Context, manifest artifact.Manifest) error {
	return store.RecordManifests(ctx, []artifact.Manifest{manifest})
}

// RecordManifests commits a completed manifest batch atomically.
func (store *Store) RecordManifests(ctx context.Context, manifests []artifact.Manifest) error {
	if store == nil || store.database == nil {
		return errors.New("library store is closed")
	}
	validated := make([]artifact.Manifest, len(manifests))
	for index, manifest := range manifests {
		rebuilt, err := validateCompletedManifest(manifest)
		if err != nil {
			return fmt.Errorf("validate artifact %d: %w", index+1, err)
		}
		validated[index] = rebuilt
	}
	if len(validated) == 0 {
		return nil
	}
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin artifact commit: %w", err)
	}
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)
	for _, manifest := range validated {
		if err := recordManifestTx(ctx, tx, manifest); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit artifacts: %w", err)
	}
	committed = true
	return nil
}

func validateCompletedManifest(manifest artifact.Manifest) (artifact.Manifest, error) {
	if manifest.SchemaVersion != artifact.SchemaVersionV1 {
		return artifact.Manifest{}, fmt.Errorf("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	files := make([]artifact.FileSpec, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		files = append(files, artifact.FileSpec{
			Path:            file.Path,
			Role:            file.Role,
			View:            file.View,
			Container:       file.Container,
			SHA256:          file.SHA256,
			VerifyContainer: true,
		})
	}
	rebuilt, err := artifact.Build(artifact.BuildInput{
		Lecture:    manifest.Lecture,
		Selection:  manifest.Selection,
		Files:      files,
		ProducedAt: manifest.ProducedAt,
		Producer:   manifest.Producer,
	})
	if err != nil {
		return artifact.Manifest{}, err
	}
	if manifest.ArtifactID != rebuilt.ArtifactID {
		return artifact.Manifest{}, errors.New("manifest artifactId does not match its canonical identity")
	}
	return rebuilt, nil
}

func recordManifestTx(ctx context.Context, tx *sql.Tx, manifest artifact.Manifest) error {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode artifact manifest: %w", err)
	}
	now := formatDatabaseTime(time.Now())
	result, err := tx.ExecContext(ctx, `
		INSERT INTO artifacts (
			artifact_id, schema_version, institute_id, subject_id, session_id, ttid,
			views, quality, audio_only, audio_format, manifest_json, produced_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(artifact_id) DO UPDATE SET
			manifest_json = excluded.manifest_json,
			produced_at = excluded.produced_at,
			updated_at = excluded.updated_at
		WHERE artifacts.schema_version = excluded.schema_version
			AND artifacts.institute_id = excluded.institute_id
			AND artifacts.subject_id = excluded.subject_id
			AND artifacts.session_id = excluded.session_id
			AND artifacts.ttid = excluded.ttid
			AND artifacts.views = excluded.views
			AND artifacts.quality = excluded.quality
			AND artifacts.audio_only = excluded.audio_only
			AND artifacts.audio_format = excluded.audio_format`,
		manifest.ArtifactID,
		manifest.SchemaVersion,
		manifest.Lecture.InstituteID,
		manifest.Lecture.SubjectID,
		manifest.Lecture.SessionID,
		manifest.Lecture.TTID,
		manifest.Selection.Views,
		manifest.Selection.Quality,
		manifest.Selection.AudioOnly,
		manifest.Selection.AudioFormat,
		string(encoded),
		formatDatabaseTime(manifest.ProducedAt),
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("record logical artifact %s: %w", manifest.ArtifactID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm logical artifact %s: %w", manifest.ArtifactID, err)
	}
	if rowsAffected != 1 {
		return errors.New("stored artifact identity conflicts with the immutable manifest identity")
	}
	for _, file := range manifest.Files {
		absolute := filepath.Clean(file.Path)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO artifact_files (
				artifact_id, path, role, view, container, bytes, sha256, present,
				last_verified_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
			ON CONFLICT(artifact_id, path) DO UPDATE SET
				role = excluded.role,
				view = excluded.view,
				container = excluded.container,
				bytes = excluded.bytes,
				sha256 = CASE
					WHEN excluded.sha256 <> '' THEN excluded.sha256
					ELSE artifact_files.sha256
				END,
				present = 1,
				last_verified_at = excluded.last_verified_at,
				updated_at = excluded.updated_at`,
			manifest.ArtifactID,
			absolute,
			file.Role,
			file.View,
			file.Container,
			file.Bytes,
			file.SHA256,
			now,
			now,
			now,
		)
		if err != nil {
			return fmt.Errorf("record artifact file %q: %w", file.Path, err)
		}
	}
	return nil
}

// GetArtifact returns one logical artifact and all known materialized paths.
func (store *Store) GetArtifact(ctx context.Context, artifactID string) (ArtifactRecord, error) {
	if store == nil || store.database == nil {
		return ArtifactRecord{}, errors.New("library store is closed")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return ArtifactRecord{}, errors.New("artifact ID is required")
	}
	var encoded string
	if err := store.database.QueryRowContext(ctx, "SELECT manifest_json FROM artifacts WHERE artifact_id = ?", artifactID).Scan(&encoded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ArtifactRecord{}, ErrArtifactNotFound
		}
		return ArtifactRecord{}, fmt.Errorf("read artifact %s: %w", artifactID, err)
	}
	manifest, err := decodeStoredManifest(artifactID, encoded)
	if err != nil {
		return ArtifactRecord{}, err
	}
	files, err := store.artifactFiles(ctx, artifactID)
	if err != nil {
		return ArtifactRecord{}, err
	}
	return ArtifactRecord{Manifest: manifest, Files: files}, nil
}

// ListArtifacts returns all logical artifacts, newest manifest first.
func (store *Store) ListArtifacts(ctx context.Context) ([]ArtifactRecord, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("library store is closed")
	}
	stored, err := store.storedManifests(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ArtifactRecord, 0, len(stored))
	for _, item := range stored {
		manifest, err := decodeStoredManifest(item.id, item.encoded)
		if err != nil {
			return nil, err
		}
		files, err := store.artifactFiles(ctx, item.id)
		if err != nil {
			return nil, err
		}
		result = append(result, ArtifactRecord{Manifest: manifest, Files: files})
	}
	return result, nil
}

func decodeStoredManifest(artifactID, encoded string) (artifact.Manifest, error) {
	var manifest artifact.Manifest
	if err := json.Unmarshal([]byte(encoded), &manifest); err != nil {
		return artifact.Manifest{}, fmt.Errorf("decode stored artifact %s: %w", artifactID, err)
	}
	if manifest.SchemaVersion != artifact.SchemaVersionV1 || manifest.ArtifactID != artifactID {
		return artifact.Manifest{}, fmt.Errorf("stored artifact %s has inconsistent manifest metadata", artifactID)
	}
	canonicalID, err := artifact.NewID(manifest.Identity())
	if err != nil || canonicalID != artifactID {
		return artifact.Manifest{}, fmt.Errorf("stored artifact %s has invalid canonical identity", artifactID)
	}
	return manifest, nil
}

type storedManifest struct {
	id      string
	encoded string
}

func (store *Store) storedManifests(ctx context.Context) ([]storedManifest, error) {
	rows, err := store.database.QueryContext(ctx, "SELECT artifact_id, manifest_json FROM artifacts ORDER BY produced_at DESC, artifact_id ASC")
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer closeRows(rows)
	stored := make([]storedManifest, 0)
	for rows.Next() {
		var item storedManifest
		if err := rows.Scan(&item.id, &item.encoded); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		stored = append(stored, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifacts: %w", err)
	}
	return stored, nil
}

func (store *Store) artifactFiles(ctx context.Context, artifactID string) ([]ArtifactFile, error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT path, role, view, container, bytes, sha256, present, last_verified_at
		FROM artifact_files WHERE artifact_id = ? ORDER BY path ASC`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("list files for artifact %s: %w", artifactID, err)
	}
	defer closeRows(rows)
	files := make([]ArtifactFile, 0)
	for rows.Next() {
		var file ArtifactFile
		var present int
		var verified sql.NullString
		if err := rows.Scan(&file.Path, &file.Role, &file.View, &file.Container, &file.Bytes, &file.SHA256, &present, &verified); err != nil {
			return nil, fmt.Errorf("scan artifact file: %w", err)
		}
		file.Present = present == 1
		if verified.Valid {
			parsed, err := parseDatabaseTime(verified.String)
			if err != nil {
				return nil, fmt.Errorf("decode artifact verification time: %w", err)
			}
			file.LastVerifiedAt = &parsed
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact files: %w", err)
	}
	return files, nil
}
