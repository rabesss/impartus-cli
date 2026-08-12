//go:build !windows

package library

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

type ownerOverrideFileInfo struct {
	os.FileInfo
	system any
}

func (info ownerOverrideFileInfo) Sys() any { return info.system }

func TestPrivatePermissionsRejectForeignOwner(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("os.Stat().Sys() = %T, want *syscall.Stat_t", info.Sys())
	}
	foreign := *stat
	foreign.Uid = uint32(os.Geteuid()) + 1
	info = ownerOverrideFileInfo{FileInfo: info, system: &foreign}

	if err := validatePrivateDirectoryPermissions(path, info); err == nil || !strings.Contains(err.Error(), "owned by the current user") {
		t.Fatalf("validatePrivateDirectoryPermissions() error = %v, want owner rejection", err)
	}
}
