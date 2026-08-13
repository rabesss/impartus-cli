//go:build !windows

package tuihost_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/rabesss/impartus-cli/internal/tuihost"
	"github.com/rabesss/impartus-cli/internal/tuisession"
)

const (
	openTUIEnterAlternateScreen = "\x1b[?1049h"
	openTUIExitAlternateScreen  = "\x1b[?1049l"
)

func TestCompiledOpenTUIRestoresResponsivePTY(t *testing.T) {
	executable := os.Getenv("IMPARTUS_UI_TEST_BINARY")
	if executable == "" {
		t.Skip("set IMPARTUS_UI_TEST_BINARY to the compiled OpenTUI sidecar")
	}
	t.Setenv("TERM", "xterm-256color")
	for _, size := range []struct {
		name          string
		columns, rows uint16
	}{
		{name: "narrow", columns: 40, rows: 10},
		{name: "medium", columns: 80, rows: 24},
		{name: "wide", columns: 140, rows: 40},
	} {
		t.Run(size.name, func(t *testing.T) {
			output, runErr := runCompiledOpenTUIPTY(t, executable, size.columns, size.rows)
			if runErr != nil {
				t.Fatalf("run OpenTUI: %v\noutput: %q", runErr, output)
			}
			entered := strings.Index(output, openTUIEnterAlternateScreen)
			exited := strings.LastIndex(output, openTUIExitAlternateScreen)
			if entered < 0 || exited <= entered {
				t.Fatalf("alternate screen was not restored: entered=%d exited=%d output=%q", entered, exited, output)
			}
			if !strings.Contains(output, "IMPARTUS") {
				t.Fatalf("responsive shell did not render its header: %q", output)
			}
		})
	}
}

func runCompiledOpenTUIPTY(t *testing.T, executable string, columns, rows uint16) (string, error) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("open PTY: %v", err)
	}
	defer closeOpenTUIPTY(t, master)
	defer closeOpenTUIPTY(t, slave)
	if sizeErr := pty.Setsize(master, &pty.Winsize{Cols: columns, Rows: rows}); sizeErr != nil {
		t.Fatalf("size PTY: %v", sizeErr)
	}

	session, err := tuisession.Start(t.Context(), tuisession.Options{
		Catalog: integrationCatalog{},
		Version: "pty-integration-test",
	})
	if err != nil {
		t.Fatalf("start private TUI session: %v", err)
	}
	cleanupIntegrationSession(t, session)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stderr bytes.Buffer
	result := make(chan error, 1)
	go func() {
		result <- tuihost.Run(ctx, tuihost.Options{
			Session:    session,
			Executable: executable,
			Stdin:      slave,
			Stdout:     slave,
			Stderr:     &stderr,
		})
	}()

	readChunks := make(chan []byte, 32)
	go func() {
		buffer := make([]byte, 16<<10)
		for {
			count, readErr := master.Read(buffer)
			if count > 0 {
				readChunks <- append([]byte(nil), buffer[:count]...)
			}
			if readErr != nil {
				close(readChunks)
				return
			}
		}
	}()

	var output strings.Builder
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !strings.Contains(output.String(), "IMPARTUS") {
		select {
		case chunk, open := <-readChunks:
			if !open {
				return output.String(), errors.New("OpenTUI PTY closed before rendering")
			}
			output.Write(chunk)
		case runErr := <-result:
			return output.String(), errors.Join(runErr, errors.New("OpenTUI exited before rendering"))
		case <-deadline.C:
			cancel()
			return output.String(), errors.New("OpenTUI did not render before the deadline")
		}
	}
	if _, err := master.Write([]byte("q")); err != nil {
		return output.String(), err
	}
	for {
		select {
		case chunk, open := <-readChunks:
			if open {
				output.Write(chunk)
			}
		case runErr := <-result:
			if closeErr := slave.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
				t.Errorf("close PTY slave after run: %v", closeErr)
			}
			for chunk := range readChunks {
				output.Write(chunk)
			}
			if stderr.Len() > 0 {
				runErr = errors.Join(runErr, errors.New(stderr.String()))
			}
			return output.String(), runErr
		case <-deadline.C:
			cancel()
			return output.String(), errors.New("OpenTUI did not exit before the deadline")
		}
	}
}

func closeOpenTUIPTY(t *testing.T, file *os.File) {
	t.Helper()
	if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Errorf("close PTY %s: %v", file.Name(), err)
	}
}
