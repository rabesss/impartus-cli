package server

import (
	"context"
	"net/http"
	"net/url"

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
// empty addr is treated as loopback (the 127.0.0.1 default applies).
func isLoopbackAddr(addr string) bool {
	switch addr {
	case "", "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// sameHost reports whether the Origin and Host headers reference the same host.
func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == host
}

// corsMiddleware sets permissive CORS headers when bound to loopback, and a
// tightened same-origin policy when exposed on a non-loopback address.
func (s *APIServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.loopback {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			// Exposed server: reflect the Origin only for same-origin requests.
			origin := r.Header.Get("Origin")
			if origin != "" && sameHost(origin, r.Host) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
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
