package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/rabesss/impartus-cli/internal/app"
	"github.com/rabesss/impartus-cli/internal/library"
	"github.com/rabesss/impartus-cli/internal/tui"
)

type exitCodeError struct {
	code int
	err  error
}

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
	ctx := context.Background()
	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return err
	}
	store, err := library.Open(ctx, library.Options{})
	if err != nil {
		return fmt.Errorf("open local lecture library: %w", err)
	}
	service := app.NewWithLibraryAndDiagnosticWriter(cfg, apiClient, store, io.Discard)
	report, reportErr := getDoctorReport(nil)
	if reportErr != nil {
		return errors.Join(reportErr, store.Close())
	}
	diagnostics := make([]tui.Diagnostic, 0, len(report.Checks))
	for _, check := range report.Checks {
		diagnostics = append(diagnostics, tui.Diagnostic{Name: check.Name, Status: check.Status, Detail: check.Detail})
	}
	runErr := tui.Run(ctx, service, os.Stdin, os.Stdout, tui.Options{Diagnostics: diagnostics})
	return errors.Join(runErr, store.Close())
}
