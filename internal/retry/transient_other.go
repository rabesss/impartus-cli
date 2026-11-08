//go:build !windows

package retry

// platformTransientErrors needs nothing beyond the POSIX errors outside
// Windows.
var platformTransientErrors []error
