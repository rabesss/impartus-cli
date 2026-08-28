package watch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func TestWatcherDryRunAppliesGlobalBudgetBeforePlaylistFetch(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	first := config.WatchTarget{SubjectID: 10, SessionID: 11, Label: "first"}
	second := config.WatchTarget{SubjectID: 20, SessionID: 21, Label: "second"}
	cfg.Watch.Targets = []config.WatchTarget{first, second}
	firstLecture := watchLecture(first, 91, 1, "First")
	secondLecture := watchLecture(second, 92, 1, "Second")
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{
			{first.SubjectID, first.SessionID}:   {firstLecture},
			{second.SubjectID, second.SessionID}: {secondLecture},
		},
		errors: map[[2]int]error{},
	}
	producer := &fakeProducer{
		cfg: cfg,
		fetchErrors: map[int]error{
			secondLecture.TTID: errors.New("over-budget dry-run playlist must not be fetched"),
		},
		downloadErrors: map[int]error{},
	}

	cycle, err := New(cfg, source, producer, store, Options{
		Once: true, DryRun: true, MaxLecturesPerCycle: 1,
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cycle.New != 1 || cycle.Downloaded != 0 || cycle.Skipped != 1 || !cycle.DryRun {
		t.Fatalf("cycle = %+v", cycle)
	}
}

func TestWatcherFailedPlaylistResolveConsumesGlobalBudget(t *testing.T) {
	t.Parallel()

	store := openWatchStore(t)
	cfg := watchTestConfig(t)
	first := config.WatchTarget{SubjectID: 10, SessionID: 11, Label: "first"}
	second := config.WatchTarget{SubjectID: 20, SessionID: 21, Label: "second"}
	cfg.Watch.Targets = []config.WatchTarget{first, second}
	firstLecture := watchLecture(first, 91, 1, "First")
	secondLecture := watchLecture(second, 92, 1, "Second")
	source := &fakeSource{
		lectures: map[[2]int]client.Lectures{
			{first.SubjectID, first.SessionID}:   {firstLecture},
			{second.SubjectID, second.SessionID}: {secondLecture},
		},
		errors: map[[2]int]error{},
	}
	producer := &fakeProducer{
		cfg: cfg,
		fetchErrors: map[int]error{
			firstLecture.TTID:  errors.New("first playlist failed"),
			secondLecture.TTID: errors.New("over-budget second playlist must not be fetched"),
		},
		downloadErrors: map[int]error{},
	}

	cycle, err := New(cfg, source, producer, store, Options{
		Once: true, MaxLecturesPerCycle: 1,
	}).Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "first playlist failed") || strings.Contains(err.Error(), "second playlist") {
		t.Fatalf("Run() error = %v", err)
	}
	if cycle.New != 1 || cycle.Downloaded != 0 || cycle.Skipped != 1 || cycle.Failed != 1 {
		t.Fatalf("cycle = %+v", cycle)
	}
}
