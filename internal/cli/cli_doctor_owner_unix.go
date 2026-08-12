//go:build !windows

package cli

import (
	"fmt"
	"os"
	"syscall"
)

func validateDoctorStateDirectoryOwner(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot verify owner of state directory %s", path)
	}
	effectiveUID := os.Geteuid()
	if effectiveUID < 0 {
		return fmt.Errorf("cannot determine current owner for state directory %s", path)
	}
	// #nosec G115 -- effectiveUID is checked non-negative and Unix UIDs are uint32 values.
	if stat.Uid != uint32(effectiveUID) {
		return fmt.Errorf("state directory %s must be owned by the current user", path)
	}
	return nil
}
