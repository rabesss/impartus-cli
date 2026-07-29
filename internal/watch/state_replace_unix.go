//go:build !windows

package watch

import "os"

func replaceStateFile(from, to string) error {
	return os.Rename(from, to)
}

func syncStateDirectory(path string) error {
	directory, err := os.Open(path) // #nosec G304 -- parent of operator-configured state path
	if err != nil {
		return err
	}
	defer directory.Close() //nolint:errcheck
	return directory.Sync()
}
