//go:build windows

package client

import "os"

// Windows does not expose O_NOFOLLOW through the standard x/sys contract used
// by this package. readTokenCache performs an Lstat/type check immediately
// before this open, and the cache writer never follows the destination during
// atomic replacement.
func readTokenCacheFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
