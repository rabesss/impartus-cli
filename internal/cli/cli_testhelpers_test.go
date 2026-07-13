package cli

import (
	"os"
	"testing"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	stdout, _, err := captureOutputStreams(t, fn)
	return stdout, err
}

func restoreCLIState(t *testing.T) {
	t.Helper()
	oldArgs := os.Args
	oldInteractive := runInteractiveFn
	oldCourses := runCoursesFn
	oldLectures := runLecturesFn
	oldDownload := runDownloadFn
	oldDownloadJSON := runDownloadJSONFn
	oldServe := runServeFn
	oldPlay := runPlayFn
	oldLoadResolved := loadResolvedFn
	oldNewLoggedIn := newLoggedInFn
	oldStartAPIServer := startAPIServerFn
	t.Cleanup(func() {
		os.Args = oldArgs
		runInteractiveFn = oldInteractive
		runCoursesFn = oldCourses
		runLecturesFn = oldLectures
		runDownloadFn = oldDownload
		runDownloadJSONFn = oldDownloadJSON
		runServeFn = oldServe
		runPlayFn = oldPlay
		loadResolvedFn = oldLoadResolved
		newLoggedInFn = oldNewLoggedIn
		startAPIServerFn = oldStartAPIServer
	})
}
