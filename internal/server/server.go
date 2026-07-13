// Package server implements the REST API and WebSocket server for managing download jobs.
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func defaultUpstreamLogin(ctx context.Context, cfg *config.Config) (*client.Client, *config.Config, error) {
	apiClient, err := client.NewLoggedIn(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("log in to upstream: %w", err)
	}
	return apiClient, cfg, nil
}

func newAPIServer(cfg *config.Config) *APIServer {
	return NewAPIServerWithLogin("8080", cfg, nil)
}

// NewAPIServerWithPersistence creates an APIServer with job persistence enabled.
// Jobs are persisted to the given file path (defaults to ".jobs.json" if empty).
func NewAPIServerWithPersistence(port string, cfg *config.Config, persistencePath string) *APIServer {
	return newAPIServerFull(port, cfg, nil, persistencePath, true)
}

// NewAPIServerWithLogin creates an APIServer with a custom upstream login function.
// If loginFn is nil, the default real upstream login is used.
func NewAPIServerWithLogin(port string, cfg *config.Config, loginFn UpstreamLoginFunc) *APIServer {
	return newAPIServerFull(port, cfg, loginFn, "", false)
}

// newAPIServerFull is the internal constructor with all options.
func newAPIServerFull(port string, cfg *config.Config, loginFn UpstreamLoginFunc, persistencePath string, persistenceEnabled bool) *APIServer {
	baseCfg := cloneConfig(cfg)
	if baseCfg == nil {
		baseCfg = &config.Config{}
	}
	baseCfg.ApplyDefaults()

	if loginFn == nil {
		loginFn = defaultUpstreamLogin
	}

	limiter := newLoginRateLimiter(5, 1*time.Minute)

	s := &APIServer{
		cfg:           baseCfg,
		wsHub:         NewWSHub(),
		tokenStore:    NewTokenStore(),
		router:        mux.NewRouter(),
		port:          port,
		upstreamLogin: loginFn,
		loginLimiter:  limiter,
		jobSem:        make(chan struct{}, 10), // limit concurrent running jobs
	}
	// loopback determines whether permissive CORS/WS origin checks are safe.
	// Binding a non-loopback address (e.g. 0.0.0.0) tightens them below.
	s.loopback = isLoopbackAddr(baseCfg.ListenAddr)
	s.upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return s.originAllowed(r)
	}}

	// Start rate limiter cleanup
	s.stopLoginLimiter = limiter.startCleanup()

	// Initialize job store (with persistence if enabled)
	if persistenceEnabled {
		s.jobStore = NewJobStoreWithPersistence(persistencePath)
	} else {
		s.jobStore = NewJobStore()
	}

	s.registerRoutes()
	return s
}

// Start starts the API server on the configured port. Accepts an optional context
// for graceful shutdown.
func (s *APIServer) Start(ctxs ...context.Context) error {
	if s.stopTokenCleanup == nil {
		s.stopTokenCleanup = StartTokenCleanup(s.tokenStore)
	}
	defer s.stopTokenCleanup()
	if s.stopLoginLimiter != nil {
		defer s.stopLoginLimiter()
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.jobStore.Close(shutdownCtx); err != nil {
			log.Printf("job persistence shutdown failed: %v", err)
		}
	}()

	// 0600: the log may capture redacted-but-sensitive upstream error context;
	// restrict to owner read/write only (no group/world access).
	// #nosec G302 -- intentionally restrictive mode for a credentials-adjacent log
	logFile, err := os.OpenFile("api.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err == nil {
		// OpenFile applies the mode only on creation; enforce owner-only on an
		// existing file too (e.g. one left world-readable by an older build).
		if chmodErr := os.Chmod("api.log", 0o600); chmodErr != nil {
			log.Printf("warning: failed to enforce api.log permissions: %v", chmodErr)
		}
		defer func() {
			_ = logFile.Close() //nolint:errcheck
		}()
		previousLogOutput := log.Writer()
		defer log.SetOutput(previousLogOutput)
		log.SetOutput(logFile)
	}

	addr := "127.0.0.1:" + s.port
	// Allow override via config field or env var (IMPARTUS_LISTEN_ADDR)
	if s.cfg != nil && s.cfg.ListenAddr != "" {
		addr = s.cfg.ListenAddr + ":" + s.port
	}
	// Refuse to bind a non-loopback address unless the operator explicitly
	// opts in via allowRemoteAccess / IMPARTUS_ALLOW_REMOTE_ACCESS. Binding
	// 0.0.0.0 exposes the API (same creds as Impartus config) to the network.
	// s.loopback is derived from ListenAddr in the constructor (IPv6-safe).
	if !s.loopback {
		allowRemote := s.cfg != nil && s.cfg.AllowRemoteAccess
		if err := nonLoopbackBindError(addr, allowRemote); err != nil {
			return err
		}
		log.Printf("WARNING: API server binding non-loopback address %s (allowRemoteAccess enabled)", addr)
	}
	server := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if len(ctxs) > 0 && ctxs[0] != nil {
		go func() {
			<-ctxs[0].Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("server shutdown failed: %v", err)
			}
		}()
	}

	log.Printf("Starting API server on %s", addr)
	return server.ListenAndServe()
}

func (s *APIServer) getOrRefreshUpstreamClient(ctx context.Context) (*client.Client, *config.Config, error) {
	// Fast path: check if we have a valid cached entry
	s.upstreamCacheMu.RLock()
	cached := s.upstreamCache
	if cached != nil {
		valid := cached.expiresAt.After(time.Now()) && cached.token != ""
		if valid {
			cfg := cloneConfig(s.cfg)
			cfg.Token = cached.token
			s.upstreamCacheMu.RUnlock()
			return cached.client, cfg, nil
		}
	}
	s.upstreamCacheMu.RUnlock()

	// Slow path: need to login and cache - acquire write lock
	s.upstreamCacheMu.Lock()
	defer s.upstreamCacheMu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have refreshed)
	cached = s.upstreamCache
	if cached != nil {
		valid := cached.expiresAt.After(time.Now()) && cached.token != ""
		if valid {
			cfg := cloneConfig(s.cfg)
			cfg.Token = cached.token
			return cached.client, cfg, nil
		}
	}

	// No cache or expired - do fresh login via injectable login function
	cfg := cloneConfig(s.cfg)
	apiClient, loginCfg, err := s.upstreamLogin(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	// Cache the result (token stored in cfg.Token by LoginAndSetToken)
	token := loginCfg.Token
	newEntry := &upstreamCacheEntry{
		client:    apiClient,
		cfg:       loginCfg,
		token:     token,
		expiresAt: time.Now().Add(23 * time.Hour), // Token typically valid for 24h
	}

	s.upstreamCache = newEntry

	return apiClient, loginCfg, nil
}
