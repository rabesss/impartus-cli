// Package playback owns shared error classifications for playback producers
// and consumers without coupling the downloader to the mpv implementation.
package playback

import "errors"

// ErrAuthorization classifies an upstream media authorization failure.
var ErrAuthorization = errors.New("upstream authorization failed")
