package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

type requestIDKey struct{}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		w.Header().Set("X-Request-ID", requestID)

		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFrom(r *http.Request) string {
	if id, ok := r.Context().Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// isLoopbackAddr reports whether addr binds only the loopback interface. An
// empty addr is treated as loopback (the 127.0.0.1 default applies). It
// accepts any 127.0.0.0/8 IPv4 address and IPv6 loopback (incl. "[::1]").
func isLoopbackAddr(addr string) bool {
	if addr == "" || strings.EqualFold(addr, "localhost") {
		return true
	}
	host := addr
	// Accept bracketed IPv6 forms (e.g. "[::1]") as produced by net.JoinHostPort.
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	// Treat the entire 127.0.0.0/8 range and IPv6 loopback as loopback.
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// nonLoopbackBindError returns an error explaining why a non-loopback bind is
// refused, or nil if the operator has explicitly opted into remote access. It
// is the testable core of Start()'s network-exposure gate: binding 0.0.0.0
// exposes the API (which reuses the shared Impartus credentials) to the network.
func nonLoopbackBindError(addr string, allowRemote bool) error {
	if allowRemote {
		return nil
	}
	return errors.New("refusing to bind non-loopback address " + addr +
		": set allowRemoteAccess=true (IMPARTUS_ALLOW_REMOTE_ACCESS=1) to opt in")
}

// sameHost reports whether the Origin and Host headers reference the same host.
// Hostnames compare case-insensitively and default ports (80/http, 443/https)
// are normalized, so a browser that omits the default port still matches.
//
// Threat model: both Origin and Host are client-supplied, so this guards against
// accidental cross-origin browser traffic (a stray bookmark/extension), NOT a
// determined attacker. DNS rebinding can make both headers agree. That is
// acceptable here because the API authenticates every request with a Bearer
// token and never emits Access-Control-Allow-Credentials, so a spoofed
// same-origin match alone grants no access. If cookie-based auth is ever added,
// replace this with a server-known allow-list or the configured bind address.
func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	oHost, oPort := normalizeHostPort(u.Host, u.Scheme)
	hHost, hPort := normalizeHostPort(host, u.Scheme)
	return strings.EqualFold(oHost, hHost) && oPort == hPort
}

// normalizeHostPort splits a "host:port" authority into its parts, applying the
// scheme's default port when none is present and trimming IPv6 brackets.
func normalizeHostPort(hostPort, scheme string) (string, string) {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return strings.Trim(hostPort, "[]"), defaultPort(scheme)
	}
	return strings.Trim(host, "[]"), port
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// corsMiddleware sets permissive CORS headers when bound to loopback, and a
// tightened same-origin policy when exposed on a non-loopback address.
func (s *APIServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.loopback {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			// Exposed server: the ACAO decision depends on Origin, so always
			// Vary on it (prevents a shared cache from reusing a CORS response
			// across origins). Reflect Origin only for same-origin requests.
			w.Header().Add("Vary", "Origin")
			origin := r.Header.Get("Origin")
			if origin != "" && sameHost(origin, r.Host) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed is the WebSocket CheckOrigin policy. Loopback accepts any
// origin; an exposed server accepts only non-browser clients (no Origin) or
// same-origin browser clients.
func (s *APIServer) originAllowed(r *http.Request) bool {
	if s.loopback {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return sameHost(origin, r.Host)
}
