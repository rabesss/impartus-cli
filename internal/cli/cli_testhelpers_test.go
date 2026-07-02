package cli

import (
	"io"
	"os"
	"testing"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	//nolint:errcheck // closing write end of pipe in test helper
	w.Close()
	os.Stdout = oldStdout

	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("ReadAll: %v", readErr)
	}
	return string(out), runErr
}

func restoreCLIState(t *testing.T) {
	t.Helper()
	oldArgs := os.Args
	oldInteractive := runInteractiveFn
	oldCourses := runCoursesFn
	oldLectures := runLecturesFn
	oldDownload := runDownloadFn
	oldServe := runServeFn
	oldPlay := runPlayFn
	t.Cleanup(func() {
		os.Args = oldArgs
		runInteractiveFn = oldInteractive
		runCoursesFn = oldCourses
		runLecturesFn = oldLectures
		runDownloadFn = oldDownload
		runServeFn = oldServe
		runPlayFn = oldPlay
	})
}
