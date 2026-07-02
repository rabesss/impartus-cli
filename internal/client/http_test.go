package client

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestDoRequestWithToken_NeverLeaksToken is the regression guard for the P0
// secret-leak fix: http.Client.Do returns a *url.Error whose Error() embeds the
// full request URL (including a query token). The token must not survive into
// the returned error, neither in the wrapping message nor via %w unwrapping.
func TestDoRequestWithToken_NeverLeaksToken(t *testing.T) {
	const secret = "do-leak-secret-token"
	c := New(nil, nil) // safe zero-value client
	// A closed port on a routable loopback host triggers a *url.Error from Do.
	resp, err := c.doRequestWithToken(context.TODO(), http.MethodGet,
		"https://127.0.0.1:1/fetchvideo?ttid=1&token="+secret, nil, secret)
	if resp != nil {
		closeErr := resp.Body.Close()
		_ = closeErr
	}
	if err == nil {
		t.Skip("no error produced; cannot assert redaction")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("doRequestWithToken error leaked token: %v", err)
	}
}

// TestDoRequestWithToken_MalformedURLNoLeak exercises the parse-failure branch:
// an invalid percent escape makes http.NewRequest fail with a *url.Error whose
// URL field is the raw tokenized URL. Neither the explicit %s nor the wrapped
// error must leak the token.
func TestDoRequestWithToken_MalformedURLNoLeak(t *testing.T) {
	const secret = "malformed-secret"
	// "%zz" is an invalid percent-escape that url.Parse rejects.
	malformed := "https://127.0.0.1:1/fetchvideo/%zz?token=" + secret
	c := New(nil, nil)
	resp, err := c.doRequestWithToken(context.TODO(), http.MethodGet, malformed, nil, "")
	if resp != nil {
		closeErr := resp.Body.Close()
		_ = closeErr
	}
	if err == nil {
		t.Skip("no error produced; cannot assert redaction")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("doRequestWithToken leaked token via malformed URL: %v", err)
	}
}
