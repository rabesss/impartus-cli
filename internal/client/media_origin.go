package client

import (
	"errors"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

var (
	// ErrMediaOrigin identifies an authenticated media URL that is not safe to
	// use for bearer-token requests.
	ErrMediaOrigin = errors.New("authenticated media origin is not authorized")
	// ErrInsecureMediaURL identifies a remote media URL that is not HTTPS.
	ErrInsecureMediaURL = errors.New("remote authenticated media URLs must use HTTPS")
	// ErrMediaURLUserinfo identifies a URL carrying credentials in its userinfo
	// component. Credentials must never be accepted as part of media URLs.
	ErrMediaURLUserinfo = errors.New("media URLs must not contain userinfo")
	// ErrInvalidMediaURL identifies malformed, unsupported, or relative media
	// URLs. It is deliberately fixed text so malformed URLs cannot echo secrets.
	ErrInvalidMediaURL = errors.New("media URL must be an absolute HTTP(S) URL with a host")
)

// MediaOriginError reports a safe, typed media-origin policy failure without
// retaining the rejected URL (which may contain upstream query credentials).
type MediaOriginError struct {
	reason error
}

func (e *MediaOriginError) Error() string {
	if e == nil || e.reason == nil {
		return ErrMediaOrigin.Error()
	}
	return e.reason.Error()
}

func (e *MediaOriginError) Unwrap() error {
	if e == nil || e.reason == nil {
		return ErrMediaOrigin
	}
	return errors.Join(ErrMediaOrigin, e.reason)
}

func newMediaOriginError(reason error) error {
	return &MediaOriginError{reason: reason}
}

// mediaOriginPolicy is immutable after construction. Sharing it between
// concurrent requests is safe because no request mutates the sorted origin
// slice.
type mediaOriginPolicy struct {
	origins []string
}

func newMediaOriginPolicy(origins ...string) (mediaOriginPolicy, error) {
	validated := make(map[string]struct{}, len(origins))
	for _, rawOrigin := range origins {
		rawOrigin = strings.TrimSpace(rawOrigin)
		if rawOrigin == "" {
			continue
		}
		parsed, err := url.Parse(rawOrigin)
		if err != nil {
			return mediaOriginPolicy{}, newMediaOriginError(ErrInvalidMediaURL)
		}
		if validateErr := validateMediaURL(parsed, true); validateErr != nil {
			return mediaOriginPolicy{}, validateErr
		}
		origin, err := mediaOriginKey(parsed)
		if err != nil {
			return mediaOriginPolicy{}, err
		}
		validated[origin] = struct{}{}
	}
	canonical := make([]string, 0, len(validated))
	for origin := range validated {
		canonical = append(canonical, origin)
	}
	sort.Strings(canonical)
	return mediaOriginPolicy{origins: canonical}, nil
}

// ValidateMediaOrigins checks an exact-origin allowlist without retaining
// mutable state on the Client. Request callers should use
// GetAuthorizedWithTokenForOrigins to apply the validated policy per call.
func ValidateMediaOrigins(origins ...string) error {
	_, err := newMediaOriginPolicy(origins...)
	return err
}

func (p mediaOriginPolicy) allows(rawURL *url.URL) bool {
	if rawURL == nil {
		return false
	}
	origin, err := mediaOriginKey(rawURL)
	if err != nil {
		return false
	}
	index := sort.SearchStrings(p.origins, origin)
	return index < len(p.origins) && p.origins[index] == origin
}

func (p mediaOriginPolicy) allowsRedirect(initial, destination *url.URL) bool {
	if initial == nil || destination == nil {
		return false
	}
	initialOrigin, initialErr := mediaOriginKey(initial)
	destinationOrigin, destinationErr := mediaOriginKey(destination)
	if initialErr != nil || destinationErr != nil {
		return false
	}
	// Same-origin redirects preserve the authorization decision made for the
	// initial request. Cross-origin redirects need an explicit configured
	// destination in the policy.
	return initialOrigin == destinationOrigin || p.allows(destination)
}

func parseRequestURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, newMediaOriginError(ErrInvalidMediaURL)
	}
	if err := validateMediaURL(parsed, false); err != nil {
		return nil, err
	}
	// URL validation is deliberately case-insensitive for compatibility with
	// config and playlist inputs, but net/http transports expect the canonical
	// lower-case protocol name.
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	return parsed, nil
}

func validateMediaURL(parsed *url.URL, requireHTTPS bool) error {
	if parsed != nil && parsed.User != nil {
		return newMediaOriginError(ErrMediaURLUserinfo)
	}
	if parsed == nil || !validHTTPURL(parsed) {
		return newMediaOriginError(ErrInvalidMediaURL)
	}
	if requireHTTPS && strings.EqualFold(parsed.Scheme, "http") && !isLoopbackMediaHost(parsed.Hostname()) {
		return newMediaOriginError(ErrInsecureMediaURL)
	}
	return nil
}

func mediaOriginKey(parsed *url.URL) (string, error) {
	if err := validateMediaURL(parsed, true); err != nil {
		return "", err
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" || strings.Contains(hostname, "%") {
		return "", newMediaOriginError(ErrInvalidMediaURL)
	}
	if ip := net.ParseIP(hostname); ip != nil {
		// ParseIP.String() normalizes equivalent IPv4/IPv6 spellings and maps
		// IPv4-mapped IPv6 addresses to their canonical IPv4 representation.
		hostname = strings.ToLower(ip.String())
	}
	port := parsed.Port()
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 0 || portNumber > 65535 {
			return "", newMediaOriginError(ErrInvalidMediaURL)
		}
		port = strconv.Itoa(portNumber)
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + net.JoinHostPort(hostname, port), nil
	}
	if strings.Contains(hostname, ":") {
		return scheme + "://[" + hostname + "]", nil
	}
	return scheme + "://" + hostname, nil
}

func isLoopbackMediaHost(hostname string) bool {
	hostname = strings.TrimSuffix(strings.TrimSpace(hostname), ".")
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

var credentialQueryKeys = map[string]struct{}{
	"auth":          {},
	"authorization": {},
	"token":         {},
	"access_token":  {},
	"sig":           {},
	"signature":     {},
	"secret":        {},
	"key":           {},
	"api_key":       {},
	"cookie":        {},
	"set-cookie":    {},
	"x-api-key":     {},
	"x_api_key":     {},
	"refresh_token": {},
	"client_secret": {},
	"password":      {},
}

func hasCredentialQuery(parsed *url.URL) bool {
	if parsed == nil || parsed.RawQuery == "" {
		return false
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return true
	}
	for key := range query {
		if _, ok := credentialQueryKeys[strings.ToLower(key)]; ok {
			return true
		}
	}
	return false
}

func normalizeBearerToken(token string) string {
	trimmed := strings.TrimSpace(token)
	fields := strings.Fields(trimmed)
	if len(fields) >= 2 && strings.EqualFold(fields[0], "bearer") {
		return strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
	}
	return trimmed
}

func queryCredentialContainsBearer(value, token string) bool {
	actual := normalizeBearerToken(token)
	if actual == "" {
		return false
	}
	for depth := 0; depth < 3; depth++ {
		if strings.Contains(value, actual) {
			return true
		}
		lowerValue := strings.ToLower(value)
		if strings.Contains(lowerValue, strings.ToLower("Bearer "+actual)) {
			return true
		}
		decoded, err := url.QueryUnescape(value)
		if err != nil || decoded == value {
			break
		}
		value = decoded
	}
	return false
}

// stripBearerTokenQuery removes known credential query aliases and any other
// query value carrying the actual bearer. ParseQuery is used instead of
// URL.Query so malformed escapes fail closed rather than being silently
// discarded.
func stripBearerTokenQuery(rawURL, token string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", newMediaOriginError(ErrInvalidMediaURL)
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", newMediaOriginError(ErrInvalidMediaURL)
	}
	changed := false
	for key, values := range query {
		_, knownCredentialKey := credentialQueryKeys[strings.ToLower(key)]
		remove := knownCredentialKey
		for _, value := range values {
			if queryCredentialContainsBearer(value, token) {
				remove = true
				break
			}
		}
		if remove {
			delete(query, key)
			changed = true
		}
	}
	if !changed {
		return rawURL, nil
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func stripBearerTokenFromURL(rawURL *url.URL, token string) error {
	if rawURL == nil {
		return newMediaOriginError(ErrInvalidMediaURL)
	}
	stripped, err := stripBearerTokenQuery(rawURL.String(), token)
	if err != nil {
		return err
	}
	updated, err := url.Parse(stripped)
	if err != nil {
		return newMediaOriginError(ErrInvalidMediaURL)
	}
	*rawURL = *updated
	return nil
}
