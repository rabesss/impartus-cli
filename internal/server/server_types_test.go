package server

import (
	"errors"
	"testing"
)

func TestTerminalStatusError(t *testing.T) {
	err := &TerminalStatusError{Status: StatusCompleted}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
	if !errors.Is(err, ErrJobTerminated) {
		t.Error("TerminalStatusError should unwrap to ErrJobTerminated")
	}
}

func TestJobStatusValues(t *testing.T) {
	cases := map[JobStatus]string{
		StatusPending:   "pending",
		StatusRunning:   "running",
		StatusCompleted: "completed",
		StatusFailed:    "failed",
		StatusCanceled:  "canceled",
	}
	for status, want := range cases {
		if string(status) != want {
			t.Errorf("JobStatus = %q, want %q", string(status), want)
		}
	}
}

func TestJobCopyIsIndependent(t *testing.T) {
	original := &Job{ID: "job-1", Status: StatusRunning, Outputs: []string{"a.mp4"}}
	clone := original.copy()

	clone.Status = StatusCompleted
	clone.Outputs[0] = "b.mp4"

	if original.Status != StatusRunning {
		t.Error("mutating the copy changed the original Status")
	}
	if original.Outputs[0] != "a.mp4" {
		t.Error("copy aliases the Outputs slice instead of cloning it")
	}
}
