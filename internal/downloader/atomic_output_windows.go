//go:build windows

package downloader

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
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
	partialPointer, err := windows.UTF16PtrFromString(partial)
	if err != nil {
		return outputPublication{}, fmt.Errorf("encode partial output path: %w", err)
	}
	finalPointer, err := windows.UTF16PtrFromString(final)
	if err != nil {
		return outputPublication{}, fmt.Errorf("encode final output path: %w", err)
	}
	if err := windows.MoveFileEx(partialPointer, finalPointer, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return outputPublication{}, fmt.Errorf("publish final output atomically: %w", err)
	}
	return outputPublication{}, nil
}
