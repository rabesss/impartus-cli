//go:build !windows

package server

import "os"

func syncPersistenceDirectory(path string) error {
	directory, err := os.Open(path) // #nosec G304 -- parent of operator-configured persistence path
	if err != nil {
		return err
	}
	defer directory.Close() //nolint:errcheck
	return directory.Sync()
}
