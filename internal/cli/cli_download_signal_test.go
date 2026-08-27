package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/artifact"
	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestDownloadSIGINTCleansWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt is not supported on Windows")
	}

	for _, mode := range []string{"human", "json"} {
		t.Run(mode, func(t *testing.T) {
			testDownloadSIGINTCleansWorkspace(t, mode)
		})
	}
}

func testDownloadSIGINTCleansWorkspace(t *testing.T, mode string) {
	t.Helper()
	tempBase := t.TempDir()
	readyPath := filepath.Join(tempBase, "chunk-requested")
	commandCtx, cancelCommand := context.WithCancel(context.Background())
	defer cancelCommand()
	cmd := exec.CommandContext(commandCtx, os.Args[0], "-test.run=^TestDownloadSIGINTProcessHelper$")
	cmd.Env = append(os.Environ(),
		"IMPARTUS_DOWNLOAD_SIGINT_HELPER=1",
		"IMPARTUS_DOWNLOAD_SIGINT_MODE="+mode,
		"IMPARTUS_DOWNLOAD_SIGINT_TEMP_BASE="+tempBase,
		"IMPARTUS_DOWNLOAD_SIGINT_READY="+readyPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		workspaces, globErr := filepath.Glob(filepath.Join(tempBase, "lecture-*-*"))
		if globErr != nil {
			cancelCommand()
			if waitErr := cmd.Wait(); waitErr != nil {
				t.Logf("helper exit after glob failure: %v", waitErr)
			}
			t.Fatalf("glob helper workspaces: %v", globErr)
		}
		if _, statErr := os.Stat(readyPath); statErr == nil && len(workspaces) == 1 {
			break
		}
		if time.Now().After(deadline) {
			cancelCommand()
			if waitErr := cmd.Wait(); waitErr != nil {
				t.Logf("helper exit after readiness timeout: %v", waitErr)
			}
			t.Fatalf("%s helper did not begin a real chunk download; workspaces=%v", mode, workspaces)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		cancelCommand()
		if waitErr := cmd.Wait(); waitErr != nil {
			t.Logf("helper exit after interrupt failure: %v", waitErr)
		}
		t.Fatalf("interrupt helper process: %v", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case err := <-wait:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
			t.Fatalf("%s helper exit = %v, want status 130", mode, err)
		}
	case <-time.After(10 * time.Second):
		cancelCommand()
		if waitErr := <-wait; waitErr != nil {
			t.Logf("helper exit after shutdown timeout: %v", waitErr)
		}
		t.Fatal("helper did not exit after SIGINT")
	}

	workspaces, err := filepath.Glob(filepath.Join(tempBase, "lecture-*-*"))
	if err != nil {
		t.Fatalf("glob helper workspaces after exit: %v", err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("SIGINT left temporary lecture workspaces behind: %v", workspaces)
	}
}

func TestDownloadSIGINTProcessHelper(t *testing.T) {
	if os.Getenv("IMPARTUS_DOWNLOAD_SIGINT_HELPER") != "1" {
		return
	}

	tempBase := os.Getenv("IMPARTUS_DOWNLOAD_SIGINT_TEMP_BASE")
	readyPath := os.Getenv("IMPARTUS_DOWNLOAD_SIGINT_READY")
	key := []byte("1234567890123456")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/subjects/1/lectures/2":
			if err := json.NewEncoder(w).Encode(client.Lectures{{
				InstituteID: 9,
				SubjectID:   1,
				SessionID:   2,
				TTID:        7,
				Topic:       "Interrupted Lecture",
				SeqNo:       1,
			}}); err != nil {
				return
			}
		case "/fetchvideo":
			if _, err := fmt.Fprintln(w, server.URL+"/stream-1280x720.m3u8"); err != nil {
				return
			}
		case "/stream-1280x720.m3u8":
			if _, err := fmt.Fprintf(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=%q\n#EXTINF:1,\n%s/chunk0.ts\n", server.URL+"/key", server.URL); err != nil {
				return
			}
		case "/key":
			if _, err := w.Write(append([]byte{0, 0}, reverseTestBytes(key)...)); err != nil {
				return
			}
		case "/chunk0.ts":
			if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
				return
			}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))

	cfg := &config.Config{
		BaseURL:                   server.URL,
		Token:                     "test-token",
		Quality:                   "720",
		Views:                     "left",
		DownloadLocation:          filepath.Join(tempBase, "downloads"),
		TempDirLocation:           tempBase,
		NumWorkers:                1,
		RateLimit:                 100,
		APIRateLimit:              100,
		EnablePipeline:            true,
		DownloadWorkersPerLecture: 1,
		DecryptWorkersPerLecture:  1,
	}
	deps := downloadExecutionDependencies{
		ensureFFmpeg: func() error { return nil },
		loadConfig:   func() (*config.Config, error) { return cfg, nil },
		login: func(context.Context, *config.Config) (*client.Client, error) {
			return client.New(server.Client(), nil), nil
		},
		downloadLectures: downloadLectures,
		recordArtifacts:  func(context.Context, []artifact.Manifest) error { return nil },
	}
	args := []string{"-s", "1", "-S", "2"}
	var err error
	if os.Getenv("IMPARTUS_DOWNLOAD_SIGINT_MODE") == "json" {
		runDownloadJSONFn = func(args []string) (downloadResult, error) {
			return runDownloadJSONWithSignalDependencies(args, deps)
		}
		os.Args = append([]string{"impartus", "--json", "download"}, args...)
		err = Execute("test", "test")
	} else {
		err = runDownloadWithDependencies(args, deps)
	}
	server.Close()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(ExitCode(err))
}
