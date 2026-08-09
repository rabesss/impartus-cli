// Package library owns the private local SQLite catalog for completed lecture
// artifacts, playback history, and resumable download jobs.
package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	moderncsqlite "modernc.org/sqlite"
)

const defaultBusyTimeout = 5 * time.Second

// Options controls the location and lock wait used by Open.
type Options struct {
	Path        string
	BusyTimeout time.Duration
	ReadOnly    bool
}

// Settings exposes the durability and compatibility pragmas used by a store.
type Settings struct {
	UserVersion int
	JournalMode string
	ForeignKeys bool
	Synchronous int
	BusyTimeout int
}

// Store is a concurrency-safe handle to one local library database.
type Store struct {
	database *sql.DB
	path     string
}

// DefaultPath returns the XDG state location for the local library.
func DefaultPath() (string, error) {
	stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for library: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "impartus", "library.db"), nil
}

// Open creates, migrates, and verifies a private SQLite library.
func Open(ctx context.Context, options Options) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("library context is required")
	}
	absolute, err := resolveDatabasePath(options.Path)
	if err != nil {
		return nil, err
	}
	if prepareErr := preparePrivateDatabasePath(absolute, options.ReadOnly); prepareErr != nil {
		return nil, prepareErr
	}
	timeout := normalizedBusyTimeout(options.BusyTimeout)
	database, err := sql.Open("sqlite", sqliteDSN(absolute, timeout, options.ReadOnly))
	if err != nil {
		return nil, fmt.Errorf("open library database: %w", err)
	}
	store := &Store{database: database, path: absolute}
	if err := configureDatabase(ctx, store, options.ReadOnly, timeout); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(4)
	return store, nil
}

func resolveDatabasePath(optionPath string) (string, error) {
	path := strings.TrimSpace(optionPath)
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve library path: %w", err)
	}
	if strings.ContainsRune(absolute, '\x00') {
		return "", errors.New("library path contains a null byte")
	}
	return absolute, nil
}

func normalizedBusyTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultBusyTimeout
	}
	return timeout
}

func configureDatabase(ctx context.Context, store *Store, readOnly bool, busyTimeout time.Duration) error {
	if err := store.database.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to library database: %w", err)
	}
	// Read compatibility before changing persistent pragmas. A newer database
	// must fail closed without even switching its journal mode.
	if err := checkSchemaCompatibility(ctx, store); err != nil {
		return err
	}
	journalMode, err := ensureJournalMode(ctx, store.database, readOnly, busyTimeout)
	if err != nil {
		return err
	}
	if strings.ToLower(journalMode) != "wal" {
		return fmt.Errorf("enable library WAL: got journal mode %q", journalMode)
	}
	if readOnly {
		return verifyReadOnlySchema(ctx, store)
	}
	if err := migrate(ctx, store); err != nil {
		return err
	}
	return nil
}

func ensureJournalMode(ctx context.Context, database *sql.DB, readOnly bool, timeout time.Duration) (string, error) {
	query := "PRAGMA journal_mode = WAL"
	if readOnly {
		query = "PRAGMA journal_mode"
	}
	deadline := time.Now().Add(timeout)
	for {
		var mode string
		err := database.QueryRowContext(ctx, query).Scan(&mode)
		if err == nil {
			return mode, nil
		}
		if !retryableSQLiteLock(err) || !time.Now().Before(deadline) {
			return "", fmt.Errorf("enable library WAL: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", fmt.Errorf("enable library WAL: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func retryableSQLiteLock(err error) bool {
	var sqliteErr *moderncsqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	primaryCode := sqliteErr.Code() & 0xff
	return primaryCode == 5 || primaryCode == 6
}

func verifyReadOnlySchema(ctx context.Context, store *Store) error {
	var version int
	if err := store.database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read library schema version: %w", err)
	}
	if version != currentSchemaVersion {
		return fmt.Errorf("read-only library requires schema version %d, got %d", currentSchemaVersion, version)
	}
	return nil
}

func preparePrivateDatabasePath(path string, readOnly bool) error {
	if err := prepareStateDirectory(filepath.Dir(path), readOnly); err != nil {
		return err
	}
	return prepareDatabaseFile(path, readOnly)
}

func prepareStateDirectory(parent string, readOnly bool) error {
	created := false
	if !readOnly {
		if _, err := os.Lstat(parent); errors.Is(err, os.ErrNotExist) {
			created = true
		} else if err != nil {
			return fmt.Errorf("inspect library state directory: %w", err)
		}
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return fmt.Errorf("create library state directory: %w", err)
		}
		if created {
			if err := secureNewStateDirectory(parent); err != nil {
				return fmt.Errorf("secure new library state directory: %w", err)
			}
		}
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect library state directory: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return errors.New("library state path must end in a real directory")
	}
	if permissionErr := validatePrivateDirectoryPermissions(parent, parentInfo); permissionErr != nil {
		return permissionErr
	}
	return nil
}

func prepareDatabaseFile(path string, readOnly bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if readOnly {
			return errors.New("read-only library database does not exist")
		}
		return createPrivateDatabaseFile(path)
	}
	if err != nil {
		return fmt.Errorf("inspect library database: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("library database path must be a regular file, not a symlink")
	}
	if permissionErr := validatePrivateDatabasePermissions(path, info); permissionErr != nil {
		return permissionErr
	}
	return nil
}

func createPrivateDatabaseFile(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- resolved library path in a verified private directory
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return waitForConcurrentDatabaseCreation(path)
		}
		return fmt.Errorf("create private library database: %w", err)
	}
	chmodErr := secureNewDatabaseFile(file)
	closeErr := file.Close()
	if chmodErr != nil || closeErr != nil {
		return fmt.Errorf("secure new library database: %w", errors.Join(chmodErr, closeErr))
	}
	return nil
}

func waitForConcurrentDatabaseCreation(path string) error {
	deadline := time.Now().Add(250 * time.Millisecond)
	var lastErr error
	for {
		info, err := os.Lstat(path)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return errors.New("library database path must be a regular file, not a symlink")
			}
			lastErr = validatePrivateDatabasePermissions(path, info)
			if lastErr == nil {
				return nil
			}
		} else {
			lastErr = fmt.Errorf("inspect concurrently created library database: %w", err)
		}
		if !time.Now().Before(deadline) {
			return lastErr
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func sqliteDSN(path string, busyTimeout time.Duration, readOnly bool) string {
	uri := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := url.Values{}
	mode := "rwc"
	if readOnly {
		mode = "ro"
	}
	query.Set("mode", mode)
	// Serialize writer transactions before they take a read snapshot. This
	// avoids SQLITE_BUSY_SNAPSHOT during concurrent first-open migrations and
	// makes lifecycle transitions observe the latest committed job status.
	query.Set("_txlock", "immediate")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	query.Add("_pragma", "synchronous(FULL)")
	uri.RawQuery = query.Encode()
	return uri.String()
}

// Path returns the absolute database path without opening another connection.
func (store *Store) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

// Settings reads the active SQLite compatibility and durability settings.
func (store *Store) Settings(ctx context.Context) (Settings, error) {
	if store == nil || store.database == nil {
		return Settings{}, errors.New("library store is closed")
	}
	connection, err := store.database.Conn(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("acquire library connection: %w", err)
	}
	defer closeConnection(connection)
	var result Settings
	var foreignKeys int
	if err := connection.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.UserVersion); err != nil {
		return Settings{}, err
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&result.JournalMode); err != nil {
		return Settings{}, err
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return Settings{}, err
	}
	result.ForeignKeys = foreignKeys == 1
	if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&result.Synchronous); err != nil {
		return Settings{}, err
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&result.BusyTimeout); err != nil {
		return Settings{}, err
	}
	return result, nil
}

// Close releases the SQLite connection pool.
func (store *Store) Close() error {
	if store == nil || store.database == nil {
		return nil
	}
	return store.database.Close()
}
