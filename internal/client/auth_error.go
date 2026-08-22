package client

import "errors"

// ErrAuthentication identifies an upstream authentication failure.
var ErrAuthentication = errors.New("upstream authentication failed")

// AuthenticationError carries only safe, fixed metadata about an upstream
// authentication failure.
type AuthenticationError struct {
	Operation  string
	StatusCode int
}

func (e *AuthenticationError) Error() string {
	return "wrong credentials please retry"
}

func (e *AuthenticationError) Unwrap() error {
	return ErrAuthentication
}
