//go:build windows

package downloader

import "os"

// validatePrivateWorkspace accepts any real directory on Windows, where the
// temporary directory is per-user by default and creating the symlinks a
// planted workspace would need takes a privilege.
func validatePrivateWorkspace(os.FileInfo) error { return nil }

// syncWorkspaceDir is a no-op on Windows, which cannot fsync a directory;
// NTFS journals the metadata updates in order.
func syncWorkspaceDir(string) error { return nil }

// lockDeleteFailsWhileHeld reports whether deleting a held lock file fails.
// Windows opens the lock without FILE_SHARE_DELETE, so it does.
const lockDeleteFailsWhileHeld = true
