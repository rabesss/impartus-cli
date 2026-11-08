//go:build windows

package downloader

import "os"

// validatePrivateWorkspace accepts any real directory on Windows, where the
// temporary directory is per-user by default and creating the symlinks a
// planted workspace would need takes a privilege.
func validatePrivateWorkspace(os.FileInfo) error { return nil }
