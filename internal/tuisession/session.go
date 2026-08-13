// Package tuisession implements the private single-session transport that
// hosts the experimental OpenTUI frontend. It is deliberately separate from
// the general API server in internal/server: it binds one ephemeral loopback
// port per launch, authenticates with a single per-launch capability, speaks
// one versioned protocol, and exposes only presentation-shaped projections of
// state that the Go parent continues to own.
package tuisession

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

const (
	// capabilityBytes is the per-launch capability size. 256 bits keeps the
	// secret far beyond brute force for a process-lifetime loopback socket.
	capabilityBytes = 32

	// maxRequestBodyBytes bounds every request body. The session accepts only
	// tiny control documents, so a small cap is a real limit rather than a
	// formality.
	maxRequestBodyBytes = 4 << 10

	// maxHeaderBytes bounds request headers.
	maxHeaderBytes = 8 << 10

	// readHeaderTimeout bounds how long a client may take to send headers.
	readHeaderTimeout = 5 * time.Second

	// readTimeout also bounds the tiny JSON request bodies. Event streams are
	// response-only after their header-only GET request has been read.
	readTimeout = 10 * time.Second

	// idleTimeout bounds keep-alive connections with no in-flight request.
	idleTimeout = 30 * time.Second

	// shutdownTimeout bounds graceful shutdown before connections are forced
	// closed. A wedged frontend cannot hold the parent open.
	shutdownTimeout = 2 * time.Second
)

// Catalog is the read-only application seam projected by the session. The
// production implementation is *app.Service, so the frontend reaches live
// state through the same boundary the Bubble Tea frontend uses.
type Catalog interface {
	Courses(context.Context) (client.Courses, error)
}

// LectureCatalog resolves the live lectures for one selected course.
type LectureCatalog interface {
	Lectures(context.Context, client.Course) (client.Lectures, error)
}

// ArtifactCatalog projects the durable local lecture library.
type ArtifactCatalog interface {
	Artifacts(context.Context) ([]library.ArtifactRecord, error)
}

// Actions owns mutable lecture work. Production uses *app.Service.
type Actions interface {
	DownloadLecture(context.Context, client.Lecture) (app.DownloadResult, error)
	RecordPlayback(context.Context, library.PlaybackState) error
	ResumeLecture(context.Context, client.Lecture) (library.PlaybackState, bool, error)
	StartLecture(context.Context, client.Lecture, float64) (app.PlaybackStart, error)
}

// Diagnostic is one presentation-only startup preflight result.
type Diagnostic struct {
	Name   string
	Status string
	Detail string
}

// SelfTestOptions tunes the cancellable operation seam used to prove
// operation lifecycle semantics without coupling the transport to the
// downloader.
type SelfTestOptions struct {
	// Steps is the number of progress ticks before the operation completes.
	Steps int
	// Interval is the delay between progress ticks. A non-positive interval
	// runs the operation as fast as the scheduler allows.
	Interval time.Duration
}

// Options configure one session.
type Options struct {
	// Catalog supplies read-only catalog projections. Required.
	Catalog Catalog
	// Lectures supplies live lecture projections. Optional during transport-only tests.
	Lectures LectureCatalog
	// Artifacts supplies local-library projections. Optional during transport-only tests.
	Artifacts ArtifactCatalog
	// Actions supplies mutable lecture operations. Optional during read-only tests.
	Actions Actions
	// Diagnostics are copied and scrubbed before the session begins serving.
	Diagnostics []Diagnostic
	// Version is the parent build version reported by the health probe.
	Version string
	// EventQueueDepth bounds per-client event delivery. Zero uses the default.
	EventQueueDepth int
	// SelfTest tunes the cancellable operation seam.
	SelfTest SelfTestOptions
}

// Session is one private loopback transport instance.
type Session struct {
	id         string
	capability string
	address    string
	version    string

	listener    net.Listener
	server      *http.Server
	catalog     Catalog
	lectures    LectureCatalog
	artifacts   ArtifactCatalog
	diagnostics []tuiproto.Diagnostic
	events      *hub
	operations  *operationRegistry

	ctx    context.Context
	cancel context.CancelFunc

	serveOnce sync.Once
	serveErr  chan error
	closeOnce sync.Once
	closeErr  error
}

// Start pre-binds an ephemeral loopback port, mints a fresh capability, and
// begins serving the versioned session contract. The bound address and
// capability are available before any child process is launched.
func Start(ctx context.Context, options Options) (*Session, error) {
	if options.Catalog == nil {
		return nil, errors.New("tui session requires a catalog source")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind private tui session port: %w", err)
	}
	session, err := newSession(ctx, listener, options)
	if err != nil {
		return nil, errors.Join(err, listener.Close())
	}
	session.serve()
	go session.closeOnContext()
	return session, nil
}

func newSession(ctx context.Context, listener net.Listener, options Options) (*Session, error) {
	capability, err := randomToken(capabilityBytes)
	if err != nil {
		return nil, fmt.Errorf("generate tui session capability: %w", err)
	}
	identity, err := randomToken(16)
	if err != nil {
		return nil, fmt.Errorf("generate tui session identity: %w", err)
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &Session{
		id:          identity,
		capability:  capability,
		address:     listener.Addr().String(),
		version:     options.Version,
		listener:    listener,
		catalog:     options.Catalog,
		lectures:    options.Lectures,
		artifacts:   options.Artifacts,
		diagnostics: projectDiagnostics(options.Diagnostics),
		events:      newHub(options.EventQueueDepth),
		ctx:         sessionCtx,
		cancel:      cancel,
		serveErr:    make(chan error, 1),
	}
	session.id = identity
	session.operations = newOperationRegistry(sessionCtx, session.events, options.SelfTest, options.Actions)
	session.server = &http.Server{
		Handler:           session.guard(session.routes()),
		MaxHeaderBytes:    maxHeaderBytes,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		BaseContext:       func(net.Listener) context.Context { return sessionCtx },
	}
	return session, nil
}

func (session *Session) serve() {
	session.serveOnce.Do(func() {
		go func() {
			err := session.server.Serve(session.listener)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			session.serveErr <- err
		}()
	})
}

func (session *Session) closeOnContext() {
	<-session.ctx.Done()
	if closeErr := session.Close(); closeErr != nil {
		return
	}
}

// Address returns the bound loopback address, for example "127.0.0.1:53219".
func (session *Session) Address() string { return session.address }

// BaseURL returns the versioned protocol root the frontend must call.
func (session *Session) BaseURL() string {
	return "http://" + session.address + tuiproto.ProtocolBasePath
}

// Capability returns the per-launch capability required on every request. It
// must never reach argv, the child environment, or any log.
func (session *Session) Capability() string { return session.capability }

// ID returns the opaque per-launch session identity. It is not a credential.
func (session *Session) ID() string { return session.id }

// Close cancels in-flight operations, terminates every event stream, and
// shuts the transport down. It is safe to call more than once.
func (session *Session) Close() error {
	session.closeOnce.Do(func() {
		session.cancel()
		session.operations.stopAndWait()
		session.events.close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErr := session.server.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, session.server.Close())
		}
		session.closeErr = errors.Join(shutdownErr, <-session.serveErr)
	})
	return session.closeErr
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
