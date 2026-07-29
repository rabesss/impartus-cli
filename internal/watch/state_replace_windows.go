//go:build windows

package watch

import "golang.org/x/sys/windows"

func replaceStateFile(from, to string) error {
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

// MoveFileEx with WRITE_THROUGH commits the replacement metadata on Windows.
func syncStateDirectory(string) error {
	return nil
}
