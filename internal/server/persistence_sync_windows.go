//go:build windows

package server

import "golang.org/x/sys/windows"

// Windows commits the replacement metadata with MOVEFILE_WRITE_THROUGH, so a
// separate directory handle flush is neither necessary nor reliably portable.
func syncPersistenceDirectory(string) error {
	return nil
}

func replacePersistenceFile(from, to string) error {
	fromPtr, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPtr, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		fromPtr,
		toPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
