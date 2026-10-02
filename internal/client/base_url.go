package client

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// errInvalidBaseURL intentionally contains no user-supplied text. Base URLs
// are accepted by a few direct client entry points that do not pass through
// config.Config.Validate, so construction errors must not echo a credential
// carried in query, userinfo, or fragment data.
var errInvalidBaseURL = errors.New("baseUrl must be a valid HTTP(S) URL without credentials, query, or fragment")

// canonicalBaseURL validates the URL used as the upstream API base and
// returns a stable spelling suitable for appending endpoint paths. Keeping
// this check at the client boundary protects callers that use the package
// directly rather than loading a validated config file.
func canonicalBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("baseUrl is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !validBaseURL(parsed) {
		return "", errInvalidBaseURL
	}

	// Scheme and host names are case-insensitive. Keep a valid RawPath so an
	// escaped slash in an API prefix retains its request semantics; JoinPath
	// below performs the endpoint join without string concatenation.
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}

func validBaseURL(parsed *url.URL) bool {
	if !validBaseURLShape(parsed) || !validBaseURLScheme(parsed) || !validBaseURLAuthority(parsed) {
		return false
	}
	// Dot segments make endpoint joining ambiguous and can turn a configured
	// API prefix into a different path after URL normalization.
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validBaseURLShape(parsed *url.URL) bool {
	return parsed != nil && parsed.Opaque == "" && parsed.User == nil && parsed.Host != "" && parsed.Hostname() != "" &&
		parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == ""
}

func validBaseURLScheme(parsed *url.URL) bool {
	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}

func validBaseURLAuthority(parsed *url.URL) bool {
	if strings.ContainsAny(parsed.Host, "\r\n") || strings.HasSuffix(parsed.Host, ":") || strings.Contains(parsed.Hostname(), "%") {
		return false
	}
	port := parsed.Port()
	if port == "" {
		return true
	}
	portNumber, err := strconv.Atoi(port)
	return err == nil && portNumber >= 1 && portNumber <= 65535
}

// baseURLPath appends an internal endpoint to a validated API base without
// allowing the base URL's query, fragment, or userinfo to become part of the
// request URL.
func baseURLPath(rawBaseURL, endpoint string) (string, error) {
	canonical, err := canonicalBaseURL(rawBaseURL)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(canonical)
	if err != nil {
		// canonicalBaseURL already parsed this value; keep this defensive branch
		// fixed-text so a future parser change cannot echo the input.
		return "", errInvalidBaseURL
	}
	endpoint = strings.TrimPrefix(endpoint, "/")
	if endpoint == "" || strings.ContainsAny(endpoint, "?#\r\n") {
		return "", errInvalidBaseURL
	}
	parsed = parsed.JoinPath(endpoint)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// BaseURLPath appends an internal endpoint to a validated API base. It is the
// shared boundary for sibling internal packages that construct authenticated
// upstream paths, so trailing slashes, API path prefixes, and malformed
// credential-bearing bases are handled consistently.
func BaseURLPath(rawBaseURL, endpoint string) (string, error) {
	return baseURLPath(rawBaseURL, endpoint)
}
