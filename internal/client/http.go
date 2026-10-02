package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rabesss/impartus-cli/internal/secrets"
)

const defaultHTTPTimeout = 10 * time.Minute

// net/http applies this limit when CheckRedirect is nil. Media requests wrap
// CheckRedirect to enforce origin policy, so retain the same bound explicitly.
const defaultMaxRedirects = 10

// NewHTTPClient creates a new HTTP client with sensible defaults and the given timeout.
func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
		},
	}
}

func (c *Client) doRequestWithToken(ctx context.Context, method, url string, body io.Reader, token string) (*http.Response, error) {
	c.initialize()
	return c.doRequestWithTokenClient(ctx, method, url, body, token, c.httpClient)
}

func (c *Client) doRequestWithTokenClient(ctx context.Context, method, rawURL string, body io.Reader, token string, httpClient *http.Client, redactionTokens ...string) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	redactionToken := token
	if len(redactionTokens) > 0 {
		redactionToken = redactionTokens[0]
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		// Redact the URL and sanitize the error: malformed/tokenized URLs can
		// surface in the parse error, which may carry query tokens.
		return nil, fmt.Errorf("failed to create http request for %s %s: %w", method, secrets.RedactURLWithToken(rawURL, redactionToken), secrets.SanitizeErrorWithToken(err, redactionToken))
	}

	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", normalizeBearerToken(token)))
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if body != nil {
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	}

	if httpClient == nil {
		httpClient = c.httpClient
	}
	response, err := httpClient.Do(req)
	if err != nil {
		var originErr *MediaOriginError
		if errors.As(err, &originErr) {
			return nil, originErr
		}
		// http.Client.Do returns a *url.Error whose Error() embeds the full
		// request URL (including query tokens). Sanitize it before wrapping so
		// the token can never reach logs via %w/%v on this error.
		return nil, fmt.Errorf("request failed with error %w for %s %s", secrets.SanitizeErrorWithToken(err, redactionToken), method, secrets.RedactURLWithToken(rawURL, redactionToken))
	}

	return response, nil
}

func (c *Client) httpClientForMediaRequest(initialURL *url.URL, redactionToken string, policy mediaOriginPolicy) *http.Client {
	client := *c.httpClient
	client.Jar = nil
	previousCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		return c.handleMediaRedirect(initialURL, redactionToken, policy, previousCheckRedirect, next, via)
	}
	return &client
}

func (c *Client) handleMediaRedirect(initialURL *url.URL, redactionToken string, policy mediaOriginPolicy, previousCheckRedirect func(*http.Request, []*http.Request) error, next *http.Request, via []*http.Request) error {
	if len(via) >= defaultMaxRedirects {
		return errors.New("stopped after 10 redirects")
	}
	canonicalizeMediaRedirectScheme(next)
	if err := validateMediaRedirect(initialURL, next.URL, policy); err != nil {
		return err
	}

	// Strip the bearer from the query before invoking a caller-supplied hook,
	// which additionally only sees a sanitized view. Only parameters carrying
	// the bearer are removed: unrelated parameters such as CDN signing
	// parameters on the Location are kept, as net/http would. A configured,
	// token-bearing request may retain its query form on same-origin
	// redirects because the upstream may require it in addition to the
	// Authorization header.
	initialAuthorized := policy.allows(initialURL)
	sameOrigin := sameMediaOrigin(initialURL, next.URL)
	stripRedirectCredentials := redactionToken == "" || !initialAuthorized || !sameOrigin
	if err := maybeStripRedirectURL(next.URL, redactionToken, stripRedirectCredentials); err != nil {
		return err
	}
	removeMediaRedirectHeaders(next.Header, stripRedirectCredentials)
	if previousCheckRedirect != nil {
		if err := callMediaRedirectHook(previousCheckRedirect, next, via, redactionToken); err != nil {
			return err
		}
	}
	// A custom hook may rewrite the URL or add sensitive headers. Recheck
	// the policy and enforce the cross-origin header boundary after it runs.
	canonicalizeMediaRedirectScheme(next)
	if err := validateMediaRedirect(initialURL, next.URL, policy); err != nil {
		return err
	}
	sameOrigin = sameMediaOrigin(initialURL, next.URL)
	stripRedirectCredentials = redactionToken == "" || !initialAuthorized || !sameOrigin
	if err := maybeStripRedirectURL(next.URL, redactionToken, stripRedirectCredentials); err != nil {
		return err
	}
	removeMediaRedirectHeaders(next.Header, stripRedirectCredentials)
	return validateRedirectRequestCredentialBoundary(next, redactionToken, !stripRedirectCredentials && initialAuthorized && sameOrigin)
}

func canonicalizeMediaRedirectScheme(request *http.Request) {
	if request == nil || request.URL == nil {
		return
	}
	if strings.EqualFold(request.URL.Scheme, "http") || strings.EqualFold(request.URL.Scheme, "https") {
		request.URL.Scheme = strings.ToLower(request.URL.Scheme)
	}
}

func maybeStripRedirectURL(rawURL *url.URL, token string, strip bool) error {
	if !strip {
		return nil
	}
	return stripBearerTokenFromURL(rawURL, token)
}

func removeMediaRedirectHeaders(header http.Header, removeAuth bool) {
	deleteHeaderCaseInsensitive(header, "Referer")
	if removeAuth {
		deleteHeaderCaseInsensitive(header, "Authorization", "Proxy-Authorization", "Cookie", "Cookie2")
	}
}

// validateRedirectCredentialBoundary rejects a URL carrying the bearer outside
// its query, or in its query unless allowBearerQuery is set. The query is
// checked for the bearer alone: the generic credential scrub would also match
// unrelated parameters such as CDN signing parameters.
func validateRedirectCredentialBoundary(rawURL *url.URL, token string, allowBearerQuery bool) error {
	if rawURL == nil {
		return newMediaOriginError(ErrInvalidMediaURL)
	}
	parts := []string{
		rawURL.Scheme,
		rawURL.Host,
		rawURL.Path,
		rawURL.RawPath,
		rawURL.Fragment,
		rawURL.RawFragment,
		rawURL.Opaque,
	}
	if rawURL.User != nil {
		parts = append(parts, rawURL.User.String())
	}
	for _, part := range parts {
		if containsTokenRepresentation(part, token) {
			return newMediaOriginError(ErrMediaOrigin)
		}
	}
	if !allowBearerQuery && queryCarriesBearer(rawURL.RawQuery, token) {
		return newMediaOriginError(ErrMediaOrigin)
	}
	return nil
}

// validateRedirectRequestCredentialBoundary extends the URL credential
// boundary to request fields that are not represented by URL: Host controls
// the outgoing Host header and RequestURI can be inspected by a custom
// RoundTripper even when URL itself is clean. The bearer remains allowed in
// the query only for an explicitly configured same-origin request.
func validateRedirectRequestCredentialBoundary(request *http.Request, token string, allowBearerQuery bool) error {
	if request == nil {
		return newMediaOriginError(ErrInvalidMediaURL)
	}
	if err := validateRedirectCredentialBoundary(request.URL, token, allowBearerQuery); err != nil {
		return err
	}
	if err := validateRedirectRequestHost(request, token); err != nil {
		return err
	}
	if request.RequestURI == "" {
		return nil
	}
	requestURI, err := url.ParseRequestURI(request.RequestURI)
	if err != nil {
		if containsTokenRepresentation(request.RequestURI, token) {
			return newMediaOriginError(ErrMediaOrigin)
		}
		return nil
	}
	return validateRedirectCredentialBoundary(requestURI, token, allowBearerQuery)
}

func containsTokenRepresentation(value, token string) bool {
	if value == "" {
		return false
	}
	for depth := 0; depth < 3; depth++ {
		if secrets.RedactWithToken(value, token) != value {
			return true
		}
		decoded, err := url.PathUnescape(value)
		if err != nil || decoded == value {
			break
		}
		value = decoded
	}
	return false
}

func deleteHeaderCaseInsensitive(header http.Header, names ...string) {
	for key := range header {
		for _, name := range names {
			if strings.EqualFold(key, name) {
				delete(header, key)
				break
			}
		}
	}
}

func validateMediaRedirect(initial, destination *url.URL, policy mediaOriginPolicy) error {
	// A tokenless compatibility request may legitimately begin on a public
	// HTTP origin. Preserve that public/loopback behavior for its redirects;
	// authenticated requests have already been rejected before this hook when
	// their initial URL is a remote HTTP origin, and a secure initial request
	// still rejects HTTPS downgrades here.
	requireHTTPS := initial == nil || !strings.EqualFold(initial.Scheme, "http")
	if err := validateMediaURL(destination, requireHTTPS); err != nil {
		return err
	}
	if !policy.allowsRedirect(initial, destination) {
		return newMediaOriginError(ErrMediaOrigin)
	}
	return nil
}

func callMediaRedirectHook(hook func(*http.Request, []*http.Request) error, next *http.Request, via []*http.Request, token string) error {
	safeNext := sanitizedRedirectRequest(next, token, true)
	baseline := safeNext.Clone(safeNext.Context())
	safeVia := make([]*http.Request, len(via))
	for index, request := range via {
		safeVia[index] = sanitizedRedirectRequest(request, token, true)
	}
	if err := hook(safeNext, safeVia); err != nil {
		return err
	}
	return applyRedirectHookChanges(next, baseline, safeNext, token)
}

func sanitizedRedirectRequest(request *http.Request, token string, includeResponse bool) *http.Request {
	if request == nil {
		return nil
	}
	safe := request.Clone(request.Context())
	safe.URL = sanitizedRedirectURL(request.URL, token)
	if safe.URL == nil {
		safe.RequestURI = ""
	} else {
		safe.RequestURI = safe.URL.RequestURI()
	}
	safe.Host = sanitizeRedirectValue(request.Host, token)
	safe.Header = sanitizedRedirectHeader(request.Header, token)
	safe.Trailer = sanitizedRedirectHeader(request.Trailer, token)
	safe.Body = nil
	safe.GetBody = nil
	safe.Form = nil
	safe.PostForm = nil
	safe.MultipartForm = nil
	safe.Response = nil
	if includeResponse {
		safe.Response = sanitizedRedirectResponse(request.Response, token) //nolint:bodyclose // sanitized clone owns an inert replacement body
	}
	return safe
}

func sanitizedRedirectResponse(response *http.Response, token string) *http.Response {
	if response == nil {
		return nil
	}
	safe := *response
	safe.Status = fmt.Sprintf("%d", response.StatusCode)
	safe.Header = sanitizedRedirectHeader(response.Header, token)
	safe.Trailer = sanitizedRedirectHeader(response.Trailer, token)
	safe.Body = io.NopCloser(strings.NewReader(""))
	safe.ContentLength = 0
	safe.Request = sanitizedRedirectRequest(response.Request, token, false)
	return &safe
}

func sanitizedRedirectURL(rawURL *url.URL, token string) *url.URL {
	if rawURL == nil {
		return nil
	}
	stripped, err := stripBearerTokenQuery(rawURL.String(), token)
	if err != nil {
		stripped = secrets.RedactURLWithToken(rawURL.String(), token)
	}
	stripped = secrets.RedactURLWithToken(stripped, token)
	safe, err := url.Parse(stripped)
	if err != nil {
		safe = &url.URL{Scheme: rawURL.Scheme, Host: rawURL.Host, Path: "/"}
	}
	safe.Scheme = sanitizeRedirectValue(safe.Scheme, token)
	safe.Host = sanitizeRedirectValue(safe.Host, token)
	safe.Path = sanitizeRedirectValue(safe.Path, token)
	safe.RawPath = sanitizeRedirectValue(safe.RawPath, token)
	safe.RawQuery = sanitizeRedirectValue(safe.RawQuery, token)
	safe.Opaque = sanitizeRedirectValue(safe.Opaque, token)
	safe.User = nil
	safe.Fragment = sanitizeRedirectValue(safe.Fragment, token)
	safe.RawFragment = sanitizeRedirectValue(safe.RawFragment, token)
	return safe
}

func sanitizedRedirectHeader(header http.Header, token string) http.Header {
	safe := make(http.Header, len(header))
	for key, values := range header {
		// Header names are observable metadata too. Do not expose a raw or
		// bounded URL-encoded bearer through a custom redirect hook even when
		// the value itself is absent or innocuous.
		if isSensitiveRedirectHeader(key) || containsTokenRepresentation(key, token) {
			continue
		}
		redacted := make([]string, len(values))
		for index, value := range values {
			redacted[index] = sanitizeRedirectValue(value, token)
		}
		safe[key] = redacted
	}
	return safe
}

func isSensitiveRedirectHeader(key string) bool {
	switch {
	case strings.EqualFold(key, "Authorization"),
		strings.EqualFold(key, "Proxy-Authorization"),
		strings.EqualFold(key, "Cookie"),
		strings.EqualFold(key, "Set-Cookie"),
		strings.EqualFold(key, "Cookie2"),
		strings.EqualFold(key, "Referer"):
		return true
	default:
		return false
	}
}

func applyRedirectHookChanges(next, baseline, modified *http.Request, token string) error {
	if err := validateRedirectHeaderCredentialBoundary(modified.Header, token); err != nil {
		return err
	}
	if err := validateRedirectRequestHost(modified, token); err != nil {
		return err
	}
	if !sameRedirectURL(baseline.URL, modified.URL) {
		next.URL = cloneRedirectURL(modified.URL)
	}
	if baseline.RequestURI != modified.RequestURI {
		if containsTokenRepresentation(modified.RequestURI, token) {
			return newMediaOriginError(ErrMediaOrigin)
		}
		next.RequestURI = modified.RequestURI
	}
	if baseline.Method != modified.Method {
		// Authenticated media requests are GETs. A redirect hook cannot turn
		// one into a mutating or body-bearing method.
		return newMediaOriginError(ErrMediaOrigin)
	}
	if baseline.Host != modified.Host {
		next.Host = modified.Host
	}
	applyRedirectHeaderChanges(next.Header, baseline.Header, modified.Header, token)
	return nil
}

// validateRedirectHeaderCredentialBoundary prevents a custom redirect hook
// from smuggling the bearer into an otherwise innocuous header name. Named
// authentication/cookie headers are handled separately by the origin policy:
// they are removed across origins and preserved only for an authorized
// same-origin redirect.
func validateRedirectHeaderCredentialBoundary(header http.Header, token string) error {
	for key, values := range header {
		if containsTokenRepresentation(key, token) {
			return newMediaOriginError(ErrMediaOrigin)
		}
		if isSensitiveRedirectHeader(key) {
			continue
		}
		for _, value := range values {
			if containsTokenRepresentation(value, token) {
				return newMediaOriginError(ErrMediaOrigin)
			}
		}
	}
	return nil
}

func validateRedirectRequestHost(request *http.Request, token string) error {
	if request == nil || request.URL == nil {
		return newMediaOriginError(ErrInvalidMediaURL)
	}
	if containsTokenRepresentation(request.Host, token) {
		return newMediaOriginError(ErrMediaOrigin)
	}
	if request.Host == "" {
		return nil
	}
	parsedHost, err := url.Parse("//" + request.Host)
	if err != nil || parsedHost.User != nil || parsedHost.Path != "" || parsedHost.RawQuery != "" || parsedHost.Fragment != "" || parsedHost.Host == "" {
		return newMediaOriginError(ErrMediaOrigin)
	}
	expectedOrigin, expectedErr := requestOriginKey(request.URL)
	actualOrigin, actualErr := requestOriginKey(&url.URL{Scheme: request.URL.Scheme, Host: parsedHost.Host})
	if expectedErr != nil || actualErr != nil || expectedOrigin != actualOrigin {
		return newMediaOriginError(ErrMediaOrigin)
	}
	return nil
}

// sanitizeRedirectValue redacts the token and bounded recursive URL-encoded
// representations. The fallback marker is intentionally opaque: a hook must
// never receive a partially decoded bearer value through a copied field.
func sanitizeRedirectValue(value, token string) string {
	redacted := secrets.RedactWithToken(value, token)
	if containsTokenRepresentation(redacted, token) {
		return "REDACTED"
	}
	return redacted
}

func sameRedirectURL(left, right *url.URL) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.String() == right.String()
}

func cloneRedirectURL(rawURL *url.URL) *url.URL {
	if rawURL == nil {
		return nil
	}
	clone := *rawURL
	return &clone
}

func applyRedirectHeaderChanges(actual, baseline, modified http.Header, token string) {
	names := make(map[string]string, len(baseline)+len(modified))
	for key := range baseline {
		names[strings.ToLower(key)] = key
	}
	for key := range modified {
		names[strings.ToLower(key)] = key
	}
	for _, key := range names {
		if isSensitiveRedirectHeader(key) {
			continue
		}
		deleteHeaderCaseInsensitive(actual, key)
		values, ok := redirectHeaderValues(modified, key)
		if !ok {
			continue
		}
		redacted := make([]string, len(values))
		for index, value := range values {
			redacted[index] = sanitizeRedirectValue(value, token)
		}
		actual[key] = redacted
	}
}

func redirectHeaderValues(header http.Header, name string) ([]string, bool) {
	for key, values := range header {
		if strings.EqualFold(key, name) {
			return values, true
		}
	}
	return nil, false
}

func sameMediaOrigin(left, right *url.URL) bool {
	leftOrigin, leftErr := requestOriginKey(left)
	rightOrigin, rightErr := requestOriginKey(right)
	return leftErr == nil && rightErr == nil && leftOrigin == rightOrigin
}
