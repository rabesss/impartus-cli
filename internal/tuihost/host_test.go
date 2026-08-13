package tuihost_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/tuihost"
	"github.com/rabesss/impartus-cli/internal/tuiproto"
)

const helperCapability = "bootstrap-capability-must-stay-private"

type sessionStub struct{}

func (sessionStub) BaseURL() string    { return "http://127.0.0.1:43123/tui/v1" }
func (sessionStub) Capability() string { return helperCapability }
func (sessionStub) ID() string         { return "session-test-id" }

func TestRunHandsPrivateBootstrapToChildAndCleansItUp(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	t.Setenv("IMPARTUS_PASSWORD", "parent-secret-must-not-reach-ui")
	var output synchronizedBuffer
	err = tuihost.Run(t.Context(), tuihost.Options{
		Session:    sessionStub{},
		Executable: executable,
		Arguments:  []string{"-test.run=^TestTUIHostHelperProcess$", "--"},
		Stdout:     &output,
		Stderr:     &output,
	})
	if err != nil {
		t.Fatalf("Run() error = %v\nchild output:\n%s", err, output.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) < 4 {
		t.Fatalf("child output = %q, want at least four non-secret lines", output.String())
	}
	if lines[0] != (sessionStub{}).BaseURL() || lines[1] != tuiproto.ProtocolVersion || lines[2] != (sessionStub{}).ID() {
		t.Fatalf("child projection = %q", output.String())
	}
	if strings.Contains(output.String(), helperCapability) || strings.Contains(output.String(), "parent-secret") {
		t.Fatal("child output disclosed a credential")
	}
	if _, statErr := os.Stat(lines[3]); !os.IsNotExist(statErr) {
		t.Fatalf("bootstrap directory remained after Run: %v", statErr)
	}
}

func TestRunRemovesBootstrapWhenChildNeverConsumesIt(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	var output bytes.Buffer
	err = tuihost.Run(t.Context(), tuihost.Options{
		Session:    sessionStub{},
		Executable: executable,
		Arguments:  []string{"-test.run=^TestTUIHostHelperProcess$", "--", "--skip-consume"},
		Stdout:     &output,
		Stderr:     &output,
	})
	if err == nil || !strings.Contains(err.Error(), "before consuming") {
		t.Fatalf("Run() error = %v, want unconsumed-bootstrap failure", err)
	}
	if strings.Contains(err.Error(), helperCapability) || strings.Contains(output.String(), helperCapability) {
		t.Fatal("unconsumed-bootstrap failure disclosed the capability")
	}
	directory := strings.Split(strings.TrimSpace(output.String()), "\n")[0]
	if _, statErr := os.Stat(directory); !os.IsNotExist(statErr) {
		t.Fatalf("unconsumed bootstrap directory remained: %v", statErr)
	}
}

func TestRunCancellationKillsChildAndCleansBootstrap(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var output synchronizedBuffer
	done := make(chan error, 1)
	go func() {
		done <- tuihost.Run(ctx, tuihost.Options{
			Session:    sessionStub{},
			Executable: executable,
			Arguments:  []string{"-test.run=^TestTUIHostHelperProcess$", "--", "--wait-for-cancel"},
			Stdout:     &output,
			Stderr:     &output,
		})
	}()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), "impartus-tui-session-") {
		if time.Now().After(deadline) {
			t.Fatal("child did not consume bootstrap before cancellation")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("Run() error = %v, want context cancellation", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop the child after cancellation")
	}
	directory := strings.Split(strings.TrimSpace(output.String()), "\n")[0]
	if _, statErr := os.Stat(directory); !os.IsNotExist(statErr) {
		t.Fatalf("canceled bootstrap directory remained: %v", statErr)
	}
}

func TestTUIHostHelperProcess(t *testing.T) {
	bootstrapPath := argumentValue(os.Args, "--bootstrap")
	if bootstrapPath == "" {
		return
	}
	if hasArgument(os.Args, "--skip-consume") {
		if _, err := fmt.Fprintln(os.Stdout, filepath.Dir(bootstrapPath)); err != nil {
			t.Fatalf("write helper output: %v", err)
		}
		return
	}
	content, err := os.ReadFile(bootstrapPath) // #nosec G304 -- helper reads the exact parent-provided test bootstrap
	if err != nil {
		t.Fatalf("read bootstrap: %v", err)
	}
	var bootstrap tuiproto.Bootstrap
	if err := json.Unmarshal(content, &bootstrap); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if bootstrap.Capability != helperCapability {
		t.Fatal("child received the wrong capability")
	}
	for _, argument := range os.Args {
		if strings.Contains(argument, bootstrap.Capability) {
			t.Fatal("capability appeared in child argv")
		}
	}
	for _, variable := range os.Environ() {
		if strings.Contains(variable, bootstrap.Capability) || strings.HasPrefix(variable, "IMPARTUS_PASSWORD=") {
			t.Fatal("credential appeared in child environment")
		}
	}
	if runtime.GOOS != "windows" {
		fileInfo, statErr := os.Stat(bootstrapPath)
		if statErr != nil {
			t.Fatalf("stat bootstrap: %v", statErr)
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("bootstrap mode = %04o, want 0600", fileInfo.Mode().Perm())
		}
		directoryInfo, statErr := os.Stat(filepath.Dir(bootstrapPath))
		if statErr != nil {
			t.Fatalf("stat bootstrap directory: %v", statErr)
		}
		if directoryInfo.Mode().Perm() != 0o700 {
			t.Fatalf("bootstrap directory mode = %04o, want 0700", directoryInfo.Mode().Perm())
		}
	}
	if err := os.Remove(bootstrapPath); err != nil {
		t.Fatalf("consume bootstrap: %v", err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "%s\n%s\n%s\n%s\n",
		bootstrap.BaseURL,
		bootstrap.Protocol,
		bootstrap.SessionID,
		filepath.Dir(bootstrapPath),
	); err != nil {
		t.Fatalf("write helper output: %v", err)
	}
	if hasArgument(os.Args, "--wait-for-cancel") {
		time.Sleep(time.Hour)
	}
}

func argumentValue(arguments []string, name string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == name {
			return arguments[index+1]
		}
	}
	return ""
}

func hasArgument(arguments []string, name string) bool {
	for _, argument := range arguments {
		if argument == name {
			return true
		}
	}
	return false
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(content []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(content)
}

func (buffer *synchronizedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}
