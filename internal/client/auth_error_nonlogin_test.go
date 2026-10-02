package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/config"
)

func assertAuthenticationError(t *testing.T, err error, operation, wantMessage, secretMarker string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want typed authentication error")
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("error = %T %q, want ErrAuthentication identity", err, err)
	}
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T %q, want *AuthenticationError", err, err)
	}
	if authErr.Operation != operation || authErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("authentication metadata = (%q, %d), want (%q, 401)", authErr.Operation, authErr.StatusCode, operation)
	}
	if err.Error() != wantMessage {
		t.Fatalf("error = %q, want %q", err, wantMessage)
	}
	if strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("error leaked response body marker: %q", err)
	}
}

func writeAuthTestResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write authentication test response: %v", err)
	}
}

func TestGetCoursesUnauthorizedWrapsTypedAuthenticationError(t *testing.T) {
	const secretMarker = "subjects-response-token-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subjects" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		writeAuthTestResponse(t, w, secretMarker)
	}))
	defer server.Close()

	apiClient := New(server.Client(), nil)
	_, err := apiClient.GetCourses(context.Background(), &config.Config{BaseURL: server.URL, Token: "request-token"})
	assertAuthenticationError(
		t,
		err,
		"subjects",
		"subjects request failed with status 401: wrong credentials please retry",
		secretMarker,
	)
}

func TestGetLecturesUnauthorizedWrapsTypedAuthenticationError(t *testing.T) {
	const secretMarker = "lectures-response-password-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subjects/7/lectures/9" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		writeAuthTestResponse(t, w, secretMarker)
	}))
	defer server.Close()

	apiClient := New(server.Client(), nil)
	_, err := apiClient.GetLectures(
		context.Background(),
		&config.Config{BaseURL: server.URL, Token: "request-token"},
		Course{SubjectID: 7, SessionID: 9},
	)
	assertAuthenticationError(
		t,
		err,
		"lectures",
		"lectures request failed with status 401: wrong credentials please retry",
		secretMarker,
	)
}

func TestGetStreamInfosUnauthorizedWrapsTypedAuthenticationError(t *testing.T) {
	const secretMarker = "stream-response-username-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fetchvideo" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		writeAuthTestResponse(t, w, secretMarker)
	}))
	defer server.Close()

	apiClient := New(server.Client(), nil)
	_, err := apiClient.getStreamInfos(context.Background(), server.URL, "request-token", Lecture{TTID: 42})
	assertAuthenticationError(
		t,
		err,
		"stream info",
		"stream info request failed with status 401: wrong credentials please retry",
		secretMarker,
	)
}

func TestGetPlaylistsUnauthorizedWrapsTypedAuthenticationError(t *testing.T) {
	const secretMarker = "playlist-response-token-secret"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fetchvideo":
			writeAuthTestResponse(t, w, server.URL+"/1280x144/master.m3u8\n")
		case "/1280x144/master.m3u8":
			w.WriteHeader(http.StatusUnauthorized)
			writeAuthTestResponse(t, w, secretMarker)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	apiClient := New(server.Client(), nil)
	_, err := apiClient.GetPlaylists(
		context.Background(),
		&config.Config{BaseURL: server.URL, Token: "request-token", Quality: "144"},
		Lectures{{TTID: 42, Topic: "Auth failure", SeqNo: 1}},
	)
	assertAuthenticationError(
		t,
		err,
		"playlist",
		"playlist request failed with status 401: wrong credentials please retry",
		secretMarker,
	)
}

func TestMediaRefusalFromUnconfiguredOriginIsNotAuthentication(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			const bodyMarker = "refusal-body-marker"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				writeAuthTestResponse(t, w, bodyMarker)
			}))
			defer server.Close()

			// No origin is configured, so the request is sent without the bearer
			// and signing in again cannot fix the refusal.
			apiClient := New(server.Client(), nil)
			_, err := apiClient.getPlaylistWithPolicy(
				context.Background(),
				server.URL+"/cdn-path/master.m3u8?Signature=fake-signature",
				"request-token",
				Lecture{TTID: 42},
				mediaOriginPolicy{},
			)
			assertUnconfiguredOriginRefusal(t, err, server.URL, status, "/cdn-path", "Signature", bodyMarker)
		})
	}
}

func TestMediaRefusalAfterRedirectNamesTheRespondingOrigin(t *testing.T) {
	const bodyMarker = "redirect-refusal-body-marker"
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				writeAuthTestResponse(t, w, bodyMarker)
			}))
			defer cdn.Close()
			start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/same-origin" {
					http.Redirect(w, r, "/cdn-path/refused.ts?Signature=fake-signature", http.StatusFound)
					return
				}
				if r.URL.Path == "/cdn-path/refused.ts" {
					w.WriteHeader(status)
					writeAuthTestResponse(t, w, bodyMarker)
					return
				}
				http.Redirect(w, r, cdn.URL+"/cdn-path/refused.ts?Signature=fake-signature", http.StatusFound)
			}))
			defer start.Close()
			apiClient := New(start.Client(), nil)

			// The chain starts at an unconfigured origin, so the bearer is withheld
			// even though the redirect ends at a configured one.
			resp, err := apiClient.GetAuthorizedWithTokenForOrigins(
				context.Background(), start.URL+"/start/master.m3u8?Expires=1", "request-token", cdn.URL,
			)
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			assertUnconfiguredOriginRefusal(t, err, start.URL, status, "/start", "/cdn-path", "Expires", "Signature", bodyMarker)
			var originErr *UnconfiguredMediaOriginError
			if !errors.As(err, &originErr) || originErr.RespondingOrigin != cdn.URL {
				t.Fatalf("refusal = %#v, want responding origin %q", originErr, cdn.URL)
			}
			want := fmt.Sprintf("it ended in HTTP %d from %s", status, cdn.URL)
			if !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), start.URL+" returned") {
				t.Fatalf("error = %q, want it to name %s as the responder (%q), not %s", err, cdn.URL, want, start.URL)
			}

			// A same-origin redirect names a single origin.
			resp, err = apiClient.GetAuthorizedWithTokenForOrigins(
				context.Background(), start.URL+"/same-origin", "request-token", cdn.URL,
			)
			if resp != nil {
				_ = resp.Body.Close() //nolint:errcheck
			}
			assertUnconfiguredOriginRefusal(t, err, start.URL, status, "/same-origin", "/cdn-path", "Signature", bodyMarker)
			if !errors.As(err, &originErr) || originErr.RespondingOrigin != "" || strings.Contains(err.Error(), cdn.URL) {
				t.Fatalf("same-origin refusal = %q (%#v), want no responding origin", err, originErr)
			}
		})
	}
}

func assertUnconfiguredOriginRefusal(t *testing.T, err error, origin string, status int, absentMarkers ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want unconfigured media origin refusal")
	}
	if errors.Is(err, ErrAuthentication) {
		t.Fatalf("error = %q, want a non-authentication error", err)
	}
	var originErr *UnconfiguredMediaOriginError
	if !errors.As(err, &originErr) || !errors.Is(err, ErrUnconfiguredMediaOrigin) {
		t.Fatalf("error = %T %q, want *UnconfiguredMediaOriginError", err, err)
	}
	if originErr.Origin != origin || originErr.StatusCode != status {
		t.Fatalf("refusal metadata = (%q, %d), want (%q, %d)", originErr.Origin, originErr.StatusCode, origin, status)
	}
	for _, want := range []string{origin, "mediaOrigins", "IMPARTUS_MEDIA_ORIGINS", "without the login"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q", err, want)
		}
	}
	for _, marker := range absentMarkers {
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("error = %q, want no %q", err, marker)
		}
	}
}
