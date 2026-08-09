//go:build !windows

package downloader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func syncAndReplaceOutput(partial, final string) (outputPublication, error) {
	file, err := os.OpenFile(partial, os.O_RDWR, 0) // #nosec G304 -- validated same-directory output path
	if err != nil {
		return outputPublication{}, fmt.Errorf("open partial output for sync: %w", err)
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil || closeErr != nil {
		if syncErr != nil {
			syncErr = fmt.Errorf("sync partial output: %w", syncErr)
		}
		return outputPublication{}, errors.Join(syncErr, closeErr)
	}
	return syncAndReplaceOutputWith(partial, final, (*os.File).Sync)
}

func syncAndReplaceOutputWith(partial, final string, syncDirectory func(*os.File) error) (outputPublication, error) {
	directory, err := os.Open(filepath.Dir(final)) // #nosec G304 -- parent of validated local output
	if err != nil {
		return outputPublication{}, fmt.Errorf("open output directory before publication: %w", err)
	}
	if syncErr := syncDirectory(directory); syncErr != nil {
		return outputPublication{}, errors.Join(fmt.Errorf("preflight output directory durability: %w", syncErr), directory.Close())
	}
	if renameErr := os.Rename(partial, final); renameErr != nil {
		return outputPublication{}, errors.Join(fmt.Errorf("publish final output atomically: %w", renameErr), directory.Close())
	}
	syncErr := syncDirectory(directory)
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return outputPublication{Warning: fmt.Errorf("final output %q was published but directory durability could not be confirmed: %w", final, errors.Join(syncErr, closeErr))}, nil
	}
	return outputPublication{}, nil
}
