// Package testdeadline bounds test waits by the test binary's own deadline
// instead of a fixed wall-clock budget, so slow or contended machines only fail
// on a real hang.
package testdeadline

import "time"

// maxMargin caps how long before the binary deadline the guard fires. The
// margin leaves time for the test to report a clear failure and clean up
// before `go test -timeout` panics.
const maxMargin = 5 * time.Second

// T is the part of *testing.T that Guard needs.
type T interface {
	Helper()
	Deadline() (time.Time, bool)
	Cleanup(func())
}

// Guard returns a channel that fires shortly before t's deadline. It returns
// nil, which blocks forever in a select, when the test binary has no deadline
// (`-timeout 0`), because then only the caller can decide what a hang is.
func Guard(t T) <-chan time.Time {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		return nil
	}
	timer := time.NewTimer(guardDelay(time.Until(deadline)))
	t.Cleanup(func() { timer.Stop() })
	return timer.C
}

// Budget returns how long a non-semantic upper bound (one that only expires
// on failure, such as waiting for a killed child to be reaped) may last: the
// time left before t's deadline less the Guard margin, or fallback when the
// binary has no deadline.
func Budget(t T, fallback time.Duration) time.Duration {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		return fallback
	}
	return guardDelay(time.Until(deadline))
}

// guardDelay returns how long to wait when remaining time is left before the
// deadline: all of it except a margin of 10%, capped at maxMargin.
func guardDelay(remaining time.Duration) time.Duration {
	margin := min(remaining/10, maxMargin)
	return max(remaining-margin, 0)
}
