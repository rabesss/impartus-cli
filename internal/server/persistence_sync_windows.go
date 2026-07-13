//go:build windows

package server

// Windows does not expose a portable directory fsync through os.File. File
// contents are still flushed before ReplaceFile-style rename semantics.
func syncPersistenceDirectory(string) error {
	return nil
}
