//go:build windows

package watch

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func openAndTryLock(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode watch lock path: %w", err)
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open watch lock: %w", err)
	}
	var information windows.ByHandleFileInformation
	if infoErr := windows.GetFileInformationByHandle(handle, &information); infoErr != nil {
		return nil, errors.Join(fmt.Errorf("inspect opened watch lock: %w", infoErr), windows.CloseHandle(handle))
	}
	if information.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return nil, errors.Join(errors.New("watch lock path must be a regular non-reparse file"), windows.CloseHandle(handle))
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		return nil, errors.Join(errors.New("wrap watch lock handle"), windows.CloseHandle(handle))
	}
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped,
	)
	if err != nil {
		closeErr := file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errors.Join(ErrWatcherRunning, closeErr)
		}
		return nil, errors.Join(fmt.Errorf("acquire watch lock: %w", err), closeErr)
	}
	return file, nil
}

func unlockFile(file *os.File) error {
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped)); err != nil {
		return fmt.Errorf("release watch lock: %w", err)
	}
	return nil
}
