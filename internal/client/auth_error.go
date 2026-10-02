package client

import (
	"errors"
	"fmt"
)

// ErrAuthentication identifies an upstream authentication failure.
var ErrAuthentication = errors.New("upstream authentication failed")

// ErrUnconfiguredMediaOrigin identifies a media request refused by an origin
// outside mediaOrigins. It is deliberately not an authentication failure.
var ErrUnconfiguredMediaOrigin = errors.New("media origin is not in mediaOrigins")

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

// UnconfiguredMediaOriginError reports a 401 or 403 from a media origin that
// is not in mediaOrigins. The request was sent without the login, so signing
// in again cannot fix it. Origin is scheme://host[:port] only and never
// carries a path or query.
type UnconfiguredMediaOriginError struct {
	Origin     string
	StatusCode int
}

func (e *UnconfiguredMediaOriginError) Error() string {
	return fmt.Sprintf(
		"media origin %s returned HTTP %d; it is not in mediaOrigins (IMPARTUS_MEDIA_ORIGINS), so the request was sent without the login",
		e.Origin, e.StatusCode,
	)
}

func (e *UnconfiguredMediaOriginError) Unwrap() error {
	return ErrUnconfiguredMediaOrigin
}
