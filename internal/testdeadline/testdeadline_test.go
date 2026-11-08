package testdeadline

import (
	"testing"
	"time"
)

type deadlineT struct {
	*testing.T
	deadline time.Time
	ok       bool
}

func (tb deadlineT) Deadline() (time.Time, bool) { return tb.deadline, tb.ok }

func TestGuardBlocksForeverWithoutDeadline(t *testing.T) {
	if guard := Guard(deadlineT{T: t}); guard != nil {
		t.Fatal("Guard() without a deadline returned a channel, want nil")
	}
}

func TestGuardFiresPastDeadline(t *testing.T) {
	guard := Guard(deadlineT{T: t, deadline: time.Now().Add(-time.Second), ok: true})
	if guard == nil {
		t.Fatal("Guard() with a deadline returned nil")
	}
	<-guard
}

func TestGuardDelayLeavesMarginBeforeDeadline(t *testing.T) {
	for _, tc := range []struct {
		remaining, want time.Duration
	}{
		{remaining: 10 * time.Minute, want: 10*time.Minute - maxMargin},
		{remaining: 10 * time.Second, want: 9 * time.Second},
		{remaining: 0, want: 0},
		{remaining: -time.Second, want: 0},
	} {
		if got := guardDelay(tc.remaining); got != tc.want {
			t.Errorf("guardDelay(%v) = %v, want %v", tc.remaining, got, tc.want)
		}
	}
}

func TestBudgetUsesFallbackWithoutDeadline(t *testing.T) {
	if got := Budget(deadlineT{T: t}, time.Minute); got != time.Minute {
		t.Fatalf("Budget() without a deadline = %v, want fallback", got)
	}
}

func TestBudgetLeavesMarginBeforeDeadline(t *testing.T) {
	got := Budget(deadlineT{T: t, deadline: time.Now().Add(time.Hour), ok: true}, time.Second)
	if got <= time.Hour-2*maxMargin || got > time.Hour-maxMargin {
		t.Fatalf("Budget() = %v, want just under %v", got, time.Hour-maxMargin)
	}
}
