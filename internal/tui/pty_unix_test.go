//go:build !windows

package tui_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/tui"
)

const (
	enterAltScreen = "\x1b[?1049h"
	exitAltScreen  = "\x1b[?1049l"
)

func TestPTYRestoresAlternateScreenOnEveryTerminalPath(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Run("normal quit", func(t *testing.T) {
		output, err := runPTY(t, &fakeBackend{courses: client.Courses{{SubjectName: "Course"}}}, func(master *os.File, _ context.CancelFunc) {
			if _, writeErr := master.Write([]byte("q")); writeErr != nil {
				t.Fatalf("write quit: %v", writeErr)
			}
		})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		assertAlternateScreenRestored(t, output)
	})

	t.Run("rendered application error", func(t *testing.T) {
		output, err := runPTY(t, &fakeBackend{coursesErr: errors.New("catalog unavailable")}, func(master *os.File, _ context.CancelFunc) {
			if _, writeErr := master.Write([]byte("q")); writeErr != nil {
				t.Fatalf("write quit: %v", writeErr)
			}
		})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		assertAlternateScreenRestored(t, output)
	})

	t.Run("context cancellation", func(t *testing.T) {
		output, err := runPTY(t, &fakeBackend{courses: client.Courses{{SubjectName: "Course"}}}, func(_ *os.File, cancel context.CancelFunc) {
			cancel()
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context cancellation", err)
		}
		assertAlternateScreenRestored(t, output)
	})

	t.Run("recovered panic", func(t *testing.T) {
		output, err := runPTY(t, &fakeBackend{
			courses:       client.Courses{{SubjectName: "Course", SubjectID: 1, SessionID: 2}},
			panicLectures: true,
		}, func(master *os.File, _ context.CancelFunc) {
			if _, writeErr := master.Write([]byte("\r")); writeErr != nil {
				t.Fatalf("write enter: %v", writeErr)
			}
		})
		if err == nil {
			t.Fatal("Run() recovered panic without an error")
		}
		assertAlternateScreenRestored(t, output)
	})
}

func runPTY(t *testing.T, backend *fakeBackend, afterEnter func(*os.File, context.CancelFunc)) (string, error) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("open PTY: %v", err)
	}
	defer closePTY(t, master)
	defer closePTY(t, slave)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- tui.Run(ctx, backend, slave, slave) }()

	chunks := make(chan []byte, 32)
	go func() {
		buffer := make([]byte, 4096)
		for {
			count, readErr := master.Read(buffer)
			if count > 0 {
				copyOfChunk := append([]byte(nil), buffer[:count]...)
				chunks <- copyOfChunk
			}
			if readErr != nil {
				close(chunks)
				return
			}
		}
	}()

	var output strings.Builder
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	triggered := false
	for !triggered {
		select {
		case chunk, open := <-chunks:
			if !open {
				chunks = nil
				continue
			}
			output.Write(chunk)
			if strings.Contains(output.String(), enterAltScreen) {
				triggered = true
			}
		case runErr := <-result:
			triggered = true
			result <- runErr
		case <-deadline.C:
			t.Fatalf("TUI did not enter alternate screen; output=%q", output.String())
		}
	}
	if afterEnter != nil {
		afterEnter(master, cancel)
	}

	var runErr error
	for {
		select {
		case chunk, open := <-chunks:
			if open {
				output.Write(chunk)
			}
		case runErr = <-result:
			if closeErr := slave.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
				t.Errorf("close PTY slave after run: %v", closeErr)
			}
			for chunk := range chunks {
				output.Write(chunk)
			}
			return output.String(), runErr
		case <-deadline.C:
			t.Fatalf("TUI did not terminate; output=%q", output.String())
		}
	}
}

func closePTY(t *testing.T, file *os.File) {
	t.Helper()
	if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Errorf("close PTY %s: %v", file.Name(), err)
	}
}

func assertAlternateScreenRestored(t *testing.T, output string) {
	t.Helper()
	entered := strings.Index(output, enterAltScreen)
	exited := strings.LastIndex(output, exitAltScreen)
	if entered < 0 || exited <= entered {
		t.Fatalf("alternate screen was not restored: entered=%d exited=%d output=%q", entered, exited, output)
	}
}
