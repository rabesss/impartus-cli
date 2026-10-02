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

// UnconfiguredMediaOriginError reports a 401 or 403 to a media request that
// started at an origin outside mediaOrigins. The request was sent without the
// login, so signing in again cannot fix it. Origin is where the request
// started, the origin to configure. RespondingOrigin is set only when a
// redirect ended at a different origin, which then returned StatusCode. Both
// are scheme://host[:port] only and never carry a path or query.
type UnconfiguredMediaOriginError struct {
	Origin           string
	RespondingOrigin string
	StatusCode       int
}

func (e *UnconfiguredMediaOriginError) Error() string {
	if e.RespondingOrigin != "" && e.RespondingOrigin != e.Origin {
		return fmt.Sprintf(
			"media origin %s is not in mediaOrigins (IMPARTUS_MEDIA_ORIGINS), so the request was sent without the login; it ended in HTTP %d from %s",
			e.Origin, e.StatusCode, e.RespondingOrigin,
		)
	}
	return fmt.Sprintf(
		"media origin %s returned HTTP %d; it is not in mediaOrigins (IMPARTUS_MEDIA_ORIGINS), so the request was sent without the login",
		e.Origin, e.StatusCode,
	)
}

func (e *UnconfiguredMediaOriginError) Unwrap() error {
	return ErrUnconfiguredMediaOrigin
}
