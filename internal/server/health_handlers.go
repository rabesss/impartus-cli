package server

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const (
	healthCommand        = "health"
	readinessCacheTTL    = 15 * time.Second
	upstreamProbeTimeout = 5 * time.Second
)

func (s *APIServer) livenessHandler(w http.ResponseWriter, _ *http.Request) {
	respondWithEnvelope(w, http.StatusOK, healthCommand, livenessResponse{Status: "ok"})
}

func (s *APIServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	response, ok := s.cachedReadiness(r.Context())
	if !ok {
		return
	}
	respondWithEnvelope(w, http.StatusOK, healthCommand, response)
}

func (s *APIServer) cachedReadiness(ctx context.Context) (healthResponse, bool) {
	for {
		now := s.healthNow()

		s.readinessCache.mu.Lock()
		if s.readinessCache.valid && now.Before(s.readinessCache.expiresAt) {
			response := s.readinessCache.response
			s.readinessCache.mu.Unlock()
			return response, true
		}
		if !s.readinessCache.refreshing {
			done := make(chan struct{})
			s.readinessCache.refreshing = true
			s.readinessCache.refreshDone = done
			go s.refreshReadiness(context.WithoutCancel(ctx), done)
		}
		done := s.readinessCache.refreshDone
		s.readinessCache.mu.Unlock()

		select {
		case <-done:
			continue
		case <-ctx.Done():
			return healthResponse{}, false
		}
	}
}

func (s *APIServer) refreshReadiness(parent context.Context, done chan struct{}) {
	var (
		response  healthResponse
		expiresAt time.Time
		completed bool
	)
	defer func() {
		s.readinessCache.mu.Lock()
		defer s.readinessCache.mu.Unlock()
		if completed {
			s.readinessCache.response = response
			s.readinessCache.expiresAt = expiresAt
			s.readinessCache.valid = true
		}
		s.readinessCache.refreshing = false
		close(done)
	}()
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("panic in readiness probe (%T); caching degraded result", recovered)
			response = readinessProbeFailedResponse()
			expiresAt = s.readinessExpiry()
			completed = true
		}
	}()

	ctx, cancel := context.WithTimeout(parent, upstreamProbeTimeout)
	defer cancel()
	response = s.collectReadiness(ctx)
	expiresAt = s.readinessExpiry()
	completed = true
}

func (s *APIServer) readinessExpiry() (expiresAt time.Time) {
	defer func() {
		if recover() != nil {
			expiresAt = time.Now().Add(readinessCacheTTL)
		}
	}()
	return s.healthNow().Add(readinessCacheTTL)
}

func readinessProbeFailedResponse() healthResponse {
	return healthResponse{
		Status:   "degraded",
		Config:   configCheckResult{Status: "unknown"},
		Upstream: statusCheckResult{Status: "unknown"},
		FFmpeg:   statusCheckResult{Status: "unknown"},
	}
}

func (s *APIServer) collectReadiness(ctx context.Context) healthResponse {
	if s.readinessProbe != nil {
		return s.readinessProbe(ctx)
	}

	configStatus := s.checkConfigStatus()
	upstreamStatus := s.checkUpstreamStatus(ctx)
	ffmpegStatus := s.checkFFmpegStatus()

	overallStatus := "ok"
	if configStatus.Status != "ok" || upstreamStatus.Status != "reachable" || ffmpegStatus.Status != "available" {
		overallStatus = "degraded"
	}

	return healthResponse{
		Status:   overallStatus,
		Config:   configStatus,
		Upstream: upstreamStatus,
		FFmpeg:   ffmpegStatus,
	}
}

func (s *APIServer) checkConfigStatus() configCheckResult {
	if s.cfg == nil {
		return configCheckResult{Status: "misconfigured"}
	}
	// Expose only an aggregate status: field-level "ok"/"missing" hints (and
	// even configured/missing counts) let an unauthenticated caller probe which
	// credentials are configured. Operators diagnose details via config/logs.
	for _, value := range []string{s.cfg.Username, s.cfg.Password, s.cfg.BaseURL} {
		if value == "" {
			return configCheckResult{Status: "misconfigured"}
		}
	}
	return configCheckResult{Status: "ok"}
}

func (s *APIServer) checkUpstreamStatus(ctx context.Context) statusCheckResult {
	if s.cfg == nil || s.cfg.BaseURL == "" {
		return statusCheckResult{Status: "not_configured"}
	}

	reachable, probed := s.probeUpstreamHTTP(ctx)
	if !probed {
		// No cached token to authenticate the HTTP probe; fall back to TCP.
		reachable = s.probeUpstreamTCP(ctx)
	}
	status := "unreachable"
	if reachable {
		status = "reachable"
	}
	return statusCheckResult{Status: status}
}

// probeUpstreamHTTP attempts an authenticated probe against the upstream. It
// returns (reachable, probed): probed is false only when there is no cached
// token to authenticate with (the caller may then fall back to a TCP probe).
// When probed is true, reachable reflects whether the upstream returned 2xx —
// an explicit non-2xx (e.g. 401/500) is honored as unreachable and must NOT be
// overridden by a TCP probe.
func (s *APIServer) probeUpstreamHTTP(parent context.Context) (reachable, probed bool) {
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	s.upstreamCacheMu.RUnlock()

	if cached == nil || cached.token == "" {
		return false, false
	}

	baseURL := s.ensureScheme(s.cfg.BaseURL)
	profileURL := strings.TrimSuffix(baseURL, "/") + "/user/profile"

	ctx, cancel := context.WithTimeout(parent, upstreamProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return false, true
	}
	req.Header.Set("Authorization", "Bearer "+cached.token)

	httpClient := &http.Client{Timeout: upstreamProbeTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, true
	}
	//nolint:errcheck
	_ = resp.Body.Close()
	// A non-2xx response means the upstream is misbehaving or rejecting the
	// cached token; treat it as not reachable. This was probed, so the caller
	// must NOT fall back to TCP and flip it back to reachable.
	return resp.StatusCode >= 200 && resp.StatusCode < 300, true
}

func (s *APIServer) probeUpstreamTCP(parent context.Context) bool {
	baseURL := s.ensureScheme(s.cfg.BaseURL)

	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(host, port)
	}

	ctx, cancel := context.WithTimeout(parent, upstreamProbeTimeout)
	defer cancel()

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return false
	}
	//nolint:errcheck
	_ = conn.Close()
	return true
}

func (s *APIServer) ensureScheme(rawURL string) string {
	if !strings.HasPrefix(rawURL, "http") {
		return "https://" + rawURL
	}
	return rawURL
}

func (s *APIServer) checkFFmpegStatus() statusCheckResult {
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return statusCheckResult{Status: "available"}
	}
	return statusCheckResult{Status: "not_found"}
}
