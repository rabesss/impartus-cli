package library

import (
	"context"
	"errors"
	"fmt"
)

// Check validates SQLite pages, foreign keys, schema settings, and durable JSON
// metadata without repairing or deleting anything.
func (store *Store) Check(ctx context.Context) error {
	if store == nil || store.database == nil {
		return errors.New("library store is closed")
	}
	if err := store.checkPages(ctx); err != nil {
		return err
	}
	if err := store.checkForeignKeys(ctx); err != nil {
		return err
	}
	if err := store.checkSettings(ctx); err != nil {
		return err
	}
	if _, err := store.ListArtifacts(ctx); err != nil {
		return err
	}
	if _, err := store.ListJobs(ctx); err != nil {
		return err
	}
	return nil
}

func (store *Store) checkPages(ctx context.Context) error {
	rows, err := store.database.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return fmt.Errorf("check library pages: %w", err)
	}
	defer closeRows(rows)
	for rows.Next() {
		var message string
		if scanErr := rows.Scan(&message); scanErr != nil {
			return fmt.Errorf("read library page check: %w", scanErr)
		}
		if message != "ok" {
			return fmt.Errorf("library page check failed: %s", message)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("iterate library page check: %w", rowsErr)
	}
	return nil
}

func (store *Store) checkForeignKeys(ctx context.Context) error {
	var foreignKeyFailures int
	if queryErr := store.database.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_check").Scan(&foreignKeyFailures); queryErr != nil {
		return fmt.Errorf("check library foreign keys: %w", queryErr)
	}
	if foreignKeyFailures != 0 {
		return fmt.Errorf("library has %d foreign-key violation(s)", foreignKeyFailures)
	}
	return nil
}

func (store *Store) checkSettings(ctx context.Context) error {
	settings, err := store.Settings(ctx)
	if err != nil {
		return err
	}
	if settings.UserVersion != currentSchemaVersion || settings.JournalMode != "wal" || !settings.ForeignKeys || settings.Synchronous != 2 || settings.BusyTimeout <= 0 {
		return fmt.Errorf("library settings are unsafe: %+v", settings)
	}
	return nil
}
