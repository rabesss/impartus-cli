package library_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/library"
)

func TestOpenCreatesPrivateWALDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "state", "library.db")
	store, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})

	parentInfo, err := os.Stat(filepath.Dir(databasePath))
	if err != nil {
		t.Fatal(err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("state directory mode = %04o, want 0700", got)
	}
	databaseInfo, err := os.Stat(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := databaseInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("database mode = %04o, want 0600", got)
	}

	settings, err := store.Settings(context.Background())
	if err != nil {
		t.Fatalf("Settings() error = %v", err)
	}
	if settings.UserVersion != 1 || settings.JournalMode != "wal" || !settings.ForeignKeys || settings.Synchronous != 2 || settings.BusyTimeout <= 0 {
		t.Fatalf("Settings() = %+v", settings)
	}
}

func TestOpenRejectsNonPrivateExistingParentWithoutChangingItsMode(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "shared-parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(parent, "library.db")
	store, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if store != nil {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close unexpectedly opened store: %v", closeErr)
		}
	}
	if err == nil {
		t.Fatal("Open() error = nil, want non-private parent rejection")
	}
	info, statErr := os.Stat(parent)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("Open() changed parent mode to %04o, want 0755", got)
	}
	if _, statErr := os.Stat(databasePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Open() created database before rejecting parent: %v", statErr)
	}
}

func TestOpenRejectsNonPrivateExistingDatabaseWithoutChangingItsMode(t *testing.T) {
	databasePath := privateDatabasePath(t)
	if err := os.WriteFile(databasePath, []byte("not a private database"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if store != nil {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close unexpectedly opened store: %v", closeErr)
		}
	}
	if err == nil {
		t.Fatal("Open() error = nil, want non-private database rejection")
	}
	info, statErr := os.Stat(databasePath)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("Open() changed database mode to %04o, want 0644", got)
	}
}

func TestOpenMigrationIsIdempotent(t *testing.T) {
	databasePath := privateDatabasePath(t)
	for attempt := range 2 {
		store, err := library.Open(context.Background(), library.Options{Path: databasePath})
		if err != nil {
			t.Fatalf("Open() attempt %d error = %v", attempt+1, err)
		}
		settings, err := store.Settings(context.Background())
		if err != nil {
			t.Fatalf("Settings() attempt %d error = %v", attempt+1, err)
		}
		if settings.UserVersion != 1 {
			t.Fatalf("schema version after attempt %d = %d, want 1", attempt+1, settings.UserVersion)
		}
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close() attempt %d error = %v", attempt+1, closeErr)
		}
	}
}

func TestConcurrentFirstOpenConvergesOnOnePrivateDatabase(t *testing.T) {
	databasePath := privateDatabasePath(t)
	start := make(chan struct{})
	errorsFound := make(chan error, 12)
	var wait sync.WaitGroup
	for range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			store, err := library.Open(context.Background(), library.Options{Path: databasePath})
			if err != nil {
				errorsFound <- err
				return
			}
			if closeErr := store.Close(); closeErr != nil {
				errorsFound <- closeErr
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent first Open() error = %v", err)
	}
	store, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close converged store: %v", closeErr)
		}
	})
	settings, err := store.Settings(context.Background())
	if err != nil || settings.UserVersion != 1 || settings.JournalMode != "wal" {
		t.Fatalf("converged settings = %+v, %v", settings, err)
	}
}

func TestOpenRejectsNewerSchemaWithoutChangingIt(t *testing.T) {
	ctx := context.Background()
	databasePath := privateDatabasePath(t)
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, execErr := database.ExecContext(ctx, "CREATE TABLE sentinel (value TEXT NOT NULL); INSERT INTO sentinel(value) VALUES ('keep'); PRAGMA user_version = 99"); execErr != nil {
		t.Fatal(execErr)
	}
	if chmodErr := os.Chmod(databasePath, 0o600); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	var originalJournal string
	if queryErr := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&originalJournal); queryErr != nil {
		t.Fatal(queryErr)
	}
	if closeErr := database.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	store, err := library.Open(ctx, library.Options{Path: databasePath})
	if store != nil {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close rejected newer store: %v", closeErr)
		}
	}
	if !errors.Is(err, library.ErrNewerSchema) {
		t.Fatalf("Open() error = %v, want ErrNewerSchema", err)
	}

	database, err = sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(); closeErr != nil {
			t.Errorf("close inspection database: %v", closeErr)
		}
	})
	var version int
	var value string
	var journal string
	if queryErr := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); queryErr != nil {
		t.Fatal(queryErr)
	}
	if queryErr := database.QueryRowContext(ctx, "SELECT value FROM sentinel").Scan(&value); queryErr != nil {
		t.Fatal(queryErr)
	}
	if queryErr := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); queryErr != nil {
		t.Fatal(queryErr)
	}
	if version != 99 || value != "keep" || journal != originalJournal {
		t.Fatalf("newer database changed: version=%d sentinel=%q journal=%q want journal=%q", version, value, journal, originalJournal)
	}
	var artifactsTable int
	if queryErr := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='artifacts'").Scan(&artifactsTable); queryErr != nil {
		t.Fatal(queryErr)
	}
	if artifactsTable != 0 {
		t.Fatal("newer database received current schema tables")
	}
}

func TestFailedMigrationRollsBackEverySchemaChange(t *testing.T) {
	ctx := context.Background()
	databasePath := privateDatabasePath(t)
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, execErr := database.ExecContext(ctx, "CREATE TABLE artifact_files (sentinel TEXT NOT NULL); PRAGMA user_version = 0"); execErr != nil {
		t.Fatal(execErr)
	}
	if chmodErr := os.Chmod(databasePath, 0o600); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	if closeErr := database.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	if store, openErr := library.Open(ctx, library.Options{Path: databasePath}); openErr == nil {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close unexpectedly opened store: %v", closeErr)
		}
		t.Fatal("Open() error = nil, want migration conflict")
	}
	database, err = sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(); closeErr != nil {
			t.Errorf("close migration inspection database: %v", closeErr)
		}
	})
	var version, artifacts, sentinelColumns int
	if queryErr := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); queryErr != nil {
		t.Fatal(queryErr)
	}
	if queryErr := database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='artifacts'").Scan(&artifacts); queryErr != nil {
		t.Fatal(queryErr)
	}
	if queryErr := database.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('artifact_files') WHERE name='sentinel'").Scan(&sentinelColumns); queryErr != nil {
		t.Fatal(queryErr)
	}
	if version != 0 || artifacts != 0 || sentinelColumns != 1 {
		t.Fatalf("failed migration was not atomic: version=%d artifacts=%d sentinelColumns=%d", version, artifacts, sentinelColumns)
	}
}

func TestReadOnlyStoreSurfacesWriteErrors(t *testing.T) {
	databasePath := privateDatabasePath(t)
	store, err := library.Open(context.Background(), library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	manifest := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "readonly")
	if recordErr := store.RecordManifest(context.Background(), manifest); recordErr != nil {
		t.Fatal(recordErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	readOnly, err := library.Open(context.Background(), library.Options{Path: databasePath, ReadOnly: true})
	if err != nil {
		t.Fatalf("Open(read-only) error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := readOnly.Close(); closeErr != nil {
			t.Errorf("close read-only store: %v", closeErr)
		}
	})
	if _, readErr := readOnly.GetArtifact(context.Background(), manifest.ArtifactID); readErr != nil {
		t.Fatalf("read-only GetArtifact() error = %v", readErr)
	}
	if writeErr := readOnly.RecordManifest(context.Background(), manifest); writeErr == nil {
		t.Fatal("read-only RecordManifest() error = nil, want write failure")
	}
}

func TestBusyWriterReturnsErrorThenSucceedsAfterLockRelease(t *testing.T) {
	ctx := context.Background()
	databasePath := privateDatabasePath(t)
	store, err := library.Open(ctx, library.Options{Path: databasePath, BusyTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close store: %v", closeErr)
		}
	})
	manifest := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "locked")

	locker, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := locker.Close(); closeErr != nil {
			t.Errorf("close locker: %v", closeErr)
		}
	})
	connection, err := locker.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := connection.Close(); closeErr != nil {
			t.Errorf("close lock connection: %v", closeErr)
		}
	})
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordManifest(ctx, manifest); err == nil {
		t.Fatal("RecordManifest() under writer lock error = nil")
	}
	if _, err := connection.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordManifest(ctx, manifest); err != nil {
		t.Fatalf("RecordManifest() after lock release error = %v", err)
	}
}

func TestCheckReportsCorruptArtifactMetadataWithoutRepairingIt(t *testing.T) {
	ctx := context.Background()
	databasePath := privateDatabasePath(t)
	store, err := library.Open(ctx, library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	manifest := buildTestManifest(t, filepath.Join(t.TempDir(), "lecture.mp4"), "corrupt")
	recordErr := store.RecordManifest(ctx, manifest)
	if recordErr != nil {
		t.Fatal(recordErr)
	}
	closeErr := store.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := database.ExecContext(ctx, "UPDATE artifacts SET manifest_json = '{broken' WHERE artifact_id = ?", manifest.ArtifactID)
	if execErr != nil {
		t.Fatal(execErr)
	}
	closeErr = database.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	store, err = library.Open(ctx, library.Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close corrupt store: %v", closeErr)
		}
	})
	checkErr := store.Check(ctx)
	if checkErr == nil {
		t.Fatal("Check() error = nil, want corrupt metadata failure")
	}
	readerDB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := readerDB.Close(); closeErr != nil {
			t.Errorf("close metadata reader: %v", closeErr)
		}
	})
	var encoded string
	queryErr := readerDB.QueryRowContext(ctx, "SELECT manifest_json FROM artifacts WHERE artifact_id = ?", manifest.ArtifactID).Scan(&encoded)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if encoded != "{broken" {
		t.Fatalf("Check() repaired or rewrote corrupt data: %q", encoded)
	}
}
