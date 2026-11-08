//go:build windows

package retry

import "syscall"

// platformTransientErrors are the native Winsock errors for the transient
// network failures. On Windows, syscall.ECONNRESET and friends are distinct
// compatibility values that the network stack never returns.
var platformTransientErrors = []error{
	syscall.WSAECONNRESET,
	syscall.WSAECONNABORTED,
	syscall.Errno(10061), // WSAECONNREFUSED
	syscall.Errno(10051), // WSAENETUNREACH
	syscall.Errno(10052), // WSAENETRESET
	syscall.Errno(10065), // WSAEHOSTUNREACH
	syscall.Errno(10060), // WSAETIMEDOUT: Errno.Timeout only matches Go's invented ETIMEDOUT
}
