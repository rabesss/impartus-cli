package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/buildinfo"
	"github.com/rabesss/impartus-cli/internal/config"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/tuihost"
	"github.com/rabesss/impartus-cli/internal/tuisession"
)

type exitCodeError struct {
	code int
	err  error
}

var (
	openTUILibraryFn       = library.Open
	getTUIDoctorReportFn   = getDoctorReport
	resolveTUIExecutableFn = tuihost.ResolveExecutable
	startTUISessionFn      = tuisession.Start
	runTUIHostFn           = tuihost.Run
)

func (err *exitCodeError) Error() string { return err.err.Error() }
func (err *exitCodeError) Unwrap() error { return err.err }
func (err *exitCodeError) ExitCode() int { return err.code }

// ExitCode maps explicitly coded errors to their requested process status;
// uncoded command failures use status 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func runTUI() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := openTUILibraryFn(ctx, library.Options{})
	if err != nil {
		return fmt.Errorf("open local lecture library: %w", err)
	}
	report, reportErr := getTUIDoctorReportFn(nil)
	if reportErr != nil {
		return errors.Join(reportErr, store.Close())
	}
	diagnostics := make([]tuisession.Diagnostic, 0, len(report.Checks))
	for _, check := range report.Checks {
		diagnostics = append(diagnostics, tuisession.Diagnostic{Name: check.Name, Status: check.Status, Detail: check.Detail})
	}
	frontend, err := resolveTUIExecutableFn(os.Getenv("IMPARTUS_UI_BINARY"))
	if err != nil {
		return errors.Join(err, store.Close())
	}
	authentication := newTUIAuthenticationCoordinator(store, loadTUIConfig, func(ctx context.Context, cfg *config.Config) (tuiRemoteService, error) {
		apiClient, loginErr := newLoggedInFn(ctx, cfg)
		if loginErr != nil {
			return nil, loginErr
		}
		return app.NewWithLibraryAndDiagnosticWriter(cfg, apiClient, store, io.Discard), nil
	})
	if authErr := authentication.Retry(ctx); authErr != nil && !isRecoverableTUIAuthenticationError(authErr) {
		return errors.Join(authErr, store.Close())
	}
	session, err := startTUISessionFn(ctx, tuisession.Options{
		Actions:        authentication,
		Artifacts:      authentication,
		Authentication: authentication,
		Catalog:        authentication,
		Diagnostics:    diagnostics,
		Lectures:       authentication,
		Version:        buildinfo.Version,
	})
	if err != nil {
		return errors.Join(err, store.Close())
	}
	runErr := runTUIHostFn(ctx, tuihost.Options{Session: session, Executable: frontend})
	return errors.Join(runErr, session.Close(), store.Close())
}
