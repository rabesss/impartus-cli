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
//
// Strict is set when strictMediaOrigins refused the request (or a redirect
// hop) before it was sent. Origin is then the refused destination and
// StatusCode and RespondingOrigin are unset. Origin is empty when the refused
// origin carries the bearer (for example in a hostname label) and so cannot
// be shown. A strict refusal also matches ErrMediaOrigin, since it is a
// media-origin policy decision.
type UnconfiguredMediaOriginError struct {
	Origin           string
	RespondingOrigin string
	StatusCode       int
	Strict           bool
}

func (e *UnconfiguredMediaOriginError) Error() string {
	if e.Strict {
		if e.Origin == "" {
			return "media origin (withheld: it contains the login token) is not in mediaOrigins (IMPARTUS_MEDIA_ORIGINS); strictMediaOrigins refused the request before it was sent"
		}
		return fmt.Sprintf(
			"media origin %s is not in mediaOrigins (IMPARTUS_MEDIA_ORIGINS); strictMediaOrigins refused the request before it was sent",
			e.Origin,
		)
	}
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

// Is reports a strict refusal as ErrMediaOrigin too, keeping the single-error
// Unwrap contract for errors.Unwrap callers.
func (e *UnconfiguredMediaOriginError) Is(target error) bool {
	return e != nil && e.Strict && target == ErrMediaOrigin
}
