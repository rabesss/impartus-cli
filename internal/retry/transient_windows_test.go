//go:build windows

package retry_test

import (
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/rabesss/impartus-cli/internal/retry"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

func TestRetryableNativeWinsockErrors(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.WSAECONNRESET, syscall.WSAECONNABORTED, 10061, 10051, 10052, 10065} {
		err := secrets.SanitizeErrorWithToken(&net.OpError{Op: "read", Err: os.NewSyscallError("wsarecv", errno)}, "secret")
		if !retry.Retryable(err) {
			t.Errorf("Retryable(Winsock %d) = false, want true", uintptr(errno))
		}
	}
}
