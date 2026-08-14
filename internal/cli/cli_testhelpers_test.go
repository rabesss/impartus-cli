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
	oldTUI := runTUIFn
	oldInteractiveTerminal := isInteractiveTerminalFn
	oldCourses := runCoursesFn
	oldLectures := runLecturesFn
	oldDownload := runDownloadFn
	oldDownloadJSON := runDownloadJSONFn
	oldServe := runServeFn
	oldPlay := runPlayFn
	oldDoctor := runDoctorFn
	oldLibrary := runLibraryFn
	oldWatch := runWatchFn
	oldWatchJSON := runWatchJSONFn
	oldLoadResolved := loadResolvedFn
	oldNewLoggedIn := newLoggedInFn
	oldStartAPIServer := startAPIServerFn
	t.Cleanup(func() {
		os.Args = oldArgs
		runTUIFn = oldTUI
		isInteractiveTerminalFn = oldInteractiveTerminal
		runCoursesFn = oldCourses
		runLecturesFn = oldLectures
		runDownloadFn = oldDownload
		runDownloadJSONFn = oldDownloadJSON
		runServeFn = oldServe
		runPlayFn = oldPlay
		runDoctorFn = oldDoctor
		runLibraryFn = oldLibrary
		runWatchFn = oldWatch
		runWatchJSONFn = oldWatchJSON
		loadResolvedFn = oldLoadResolved
		newLoggedInFn = oldNewLoggedIn
		startAPIServerFn = oldStartAPIServer
	})
}
