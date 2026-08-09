package library

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
)

const currentSchemaVersion = 1

// ErrNewerSchema reports a database created by a newer Impartus binary.
var ErrNewerSchema = errors.New("library schema is newer than this Impartus build")

//go:embed migrations/001_initial.sql
var initialMigration string

func checkSchemaCompatibility(ctx context.Context, store *Store) error {
	var version int
	if err := store.database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read library schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("%w: database=%d supported=%d", ErrNewerSchema, version, currentSchemaVersion)
	}
	return nil
}

func migrate(ctx context.Context, store *Store) error {
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin library migration: %w", err)
	}
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read library schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("%w: database=%d supported=%d", ErrNewerSchema, version, currentSchemaVersion)
	}
	if version == 0 {
		if _, err := tx.ExecContext(ctx, initialMigration); err != nil {
			return fmt.Errorf("apply library schema version 1: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
			return fmt.Errorf("record library schema version 1: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit library migration: %w", err)
	}
	committed = true
	return nil
}
