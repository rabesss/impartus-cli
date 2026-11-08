package retry_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/rabesss/impartus-cli/internal/retry"
	"github.com/rabesss/impartus-cli/internal/retry/retrytest"
	"github.com/rabesss/impartus-cli/internal/secrets"
)

func testPolicy(clock *retrytest.Clock, budget time.Duration) retry.Policy {
	policy := retry.Default(budget)
	policy.Clock = clock
	policy.Jitter = retrytest.MaxJitter
	return policy
}

func status(code int) error {
	return &retry.StatusError{StatusCode: code, Err: fmt.Errorf("request failed with status %d", code)}
}

func TestDoSucceedsAfterTransientFailures(t *testing.T) {
	clock := retrytest.NewClock()
	calls := 0
	attempts, err := testPolicy(clock, time.Minute).Do(context.Background(), func(context.Context) error {
		calls++
		if calls <= 4 {
			return status(516)
		}
		return nil
	})
	if err != nil || attempts != 5 || calls != 5 {
		t.Fatalf("Do() = (%d, %v), calls %d; want 5 attempts and success", attempts, err, calls)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if got := clock.Sleeps(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
}

func TestDoStopsOnPermanentError(t *testing.T) {
	for _, err := range []error{status(http.StatusUnauthorized), status(http.StatusNotFound), errors.New("policy refusal"), context.Canceled} {
		clock := retrytest.NewClock()
		calls := 0
		attempts, got := testPolicy(clock, time.Minute).Do(context.Background(), func(context.Context) error {
			calls++
			return err
		})
		if attempts != 1 || calls != 1 || !errors.Is(got, err) {
			t.Fatalf("Do(%v) = (%d, %v) after %d calls; want one attempt", err, attempts, got, calls)
		}
		if len(clock.Sleeps()) != 0 {
			t.Fatalf("Do(%v) slept %v", err, clock.Sleeps())
		}
	}
}

func TestDoStopsWhenBudgetExhausted(t *testing.T) {
	clock := retrytest.NewClock()
	budget := 3 * time.Minute
	calls := 0
	attempts, err := testPolicy(clock, budget).Do(context.Background(), func(context.Context) error {
		calls++
		clock.Advance(100 * time.Millisecond) // each request takes some time
		return status(http.StatusServiceUnavailable)
	})
	var statusErr *retry.StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("Do() error = %v, want the last 503", err)
	}
	if attempts != calls || attempts < 5 {
		t.Fatalf("attempts = %d (calls %d), want several", attempts, calls)
	}
	total := clock.Now().Sub(retrytest.NewClock().Now())
	if total > budget+time.Second {
		t.Fatalf("total time %v exceeds budget %v", total, budget)
	}
	for _, d := range clock.Sleeps() {
		if d > retry.DefaultMaxDelay {
			t.Fatalf("sleep %v exceeds max step %v", d, retry.DefaultMaxDelay)
		}
	}
}

func TestDoHonorsRetryAfterWithinBudget(t *testing.T) {
	clock := retrytest.NewClock()
	calls := 0
	_, err := testPolicy(clock, time.Minute).Do(context.Background(), func(context.Context) error {
		calls++
		if calls == 1 {
			return &retry.StatusError{StatusCode: http.StatusServiceUnavailable, RetryAfter: 5 * time.Second, Err: errors.New("busy")}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if got := clock.Sleeps(); len(got) != 1 || got[0] != 5*time.Second {
		t.Fatalf("sleeps = %v, want [5s]", got)
	}
}

func TestDoGivesUpWhenRetryAfterExceedsBudget(t *testing.T) {
	clock := retrytest.NewClock()
	calls := 0
	attempts, err := testPolicy(clock, time.Minute).Do(context.Background(), func(context.Context) error {
		calls++
		return &retry.StatusError{StatusCode: http.StatusTooManyRequests, RetryAfter: 2 * time.Minute, Err: errors.New("slow down")}
	})
	if err == nil || attempts != 1 || len(clock.Sleeps()) != 0 {
		t.Fatalf("Do() = (%d, %v), sleeps %v; want immediate give-up", attempts, err, clock.Sleeps())
	}
}

func TestDoZeroBudgetMakesOneAttempt(t *testing.T) {
	attempts, err := retry.Policy{}.Do(context.Background(), func(context.Context) error { return status(502) })
	if attempts != 1 || err == nil {
		t.Fatalf("zero policy = (%d, %v), want 1 attempt", attempts, err)
	}

	attempts, err = retry.Default(0).Do(context.Background(), func(context.Context) error { return status(502) })
	if attempts != 1 || err == nil {
		t.Fatalf("Default(0) = (%d, %v), want 1 attempt", attempts, err)
	}
}

// A slow attempt (here, one that hits a 10m HTTP timeout) must not use up the
// budget: only time spent waiting between attempts counts against it.
func TestDoChargesOnlyWaitingAgainstBudget(t *testing.T) {
	clock := retrytest.NewClock()
	calls := 0
	attempts, err := testPolicy(clock, 3*time.Minute).Do(context.Background(), func(context.Context) error {
		calls++
		clock.Advance(10 * time.Minute)
		if calls < 3 {
			return timeoutError{}
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("Do() = (%d, %v), want success on attempt 3 despite slow attempts", attempts, err)
	}
	if got := clock.Sleeps(); !slices.Equal(got, []time.Duration{time.Second, 2 * time.Second}) {
		t.Fatalf("sleeps = %v, want [1s 2s]", got)
	}
}

func TestDoRetriesEOFBeforeResponse(t *testing.T) {
	if !retry.Retryable(fmt.Errorf("Get chunk: %w", io.EOF)) {
		t.Fatal("Retryable(wrapped io.EOF) = false, want true")
	}
}

func TestDoStopsWhenContextCanceledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := retrytest.NewClock()
	policy := testPolicy(clock, time.Minute)
	policy.OnRetry = func(int, time.Duration, error) { cancel() }
	attempts, err := policy.Do(ctx, func(context.Context) error { return status(503) })
	if attempts != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() = (%d, %v), want cancellation after one attempt", attempts, err)
	}
}

func TestDoUsesFullJitterByDefault(t *testing.T) {
	clock := retrytest.NewClock()
	policy := retry.Default(time.Hour)
	policy.Clock = clock
	if _, err := policy.Do(context.Background(), func(context.Context) error { return status(503) }); err == nil {
		t.Fatal("Do() error = nil, want 503 after the budget")
	}
	for i, d := range clock.Sleeps() {
		if ceiling := retry.Backoff(retry.DefaultBaseDelay, retry.DefaultMaxDelay, i); d < 0 || d > ceiling {
			t.Fatalf("sleep %d = %v outside [0, %v]", i, d, ceiling)
		}
	}
}

func TestBackoffCapsAndNeverOverflows(t *testing.T) {
	if got := retry.Backoff(time.Second, 30*time.Second, 10); got != 30*time.Second {
		t.Fatalf("Backoff capped = %v", got)
	}
	if got := retry.Backoff(time.Second, 0, 200); got <= 0 {
		t.Fatalf("Backoff uncapped overflowed to %v", got)
	}
	if got := retry.Backoff(time.Second, 30*time.Second, 2); got != 4*time.Second {
		t.Fatalf("Backoff(2) = %v, want 4s", got)
	}
	for _, base := range []time.Duration{1 << 40, 1 << 61, 1 << 62} {
		if got := retry.Backoff(base, 0, 4); got <= 0 {
			t.Fatalf("Backoff(base=%d) overflowed to %v", base, got)
		}
	}
}

func TestDoNeverRetriesWithoutBackoff(t *testing.T) {
	clock := retrytest.NewClock()
	// No BaseDelay or MaxDelay: the defaults apply. 1s+2s fits 3s; 4s does not.
	policy := retry.Policy{Budget: 3 * time.Second, Clock: clock, Jitter: retrytest.MaxJitter}
	if _, err := policy.Do(context.Background(), func(context.Context) error { return status(503) }); err == nil {
		t.Fatal("Do() error = nil, want 503 after the budget")
	}
	want := []time.Duration{retry.DefaultBaseDelay, 2 * retry.DefaultBaseDelay}
	if got := clock.Sleeps(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sleeps without BaseDelay = %v, want %v", got, want)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestRetryableClassification(t *testing.T) {
	urlErr := func(inner error) error {
		return &netURLError{Op: "Get", URL: "https://example.test/x?token=secret", Err: inner}
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"516", status(516), true},
		{"500", status(500), true},
		{"502", status(502), true},
		{"429", status(429), true},
		{"408", status(408), true},
		{"401", status(401), false},
		{"403", status(403), false},
		{"404", status(404), false},
		{"wrapped 503", fmt.Errorf("chunk: %w", status(503)), true},
		{"timeout", timeoutError{}, true},
		{"sanitized timeout", secrets.SanitizeErrorWithToken(urlErr(timeoutError{}), "secret"), true},
		{"sanitized reset", secrets.SanitizeErrorWithToken(urlErr(&net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ECONNRESET)}), "secret"), true},
		{"sanitized unexpected EOF", fmt.Errorf("could not write chunk: %w", secrets.SanitizeErrorWithToken(io.ErrUnexpectedEOF, "secret")), true},
		{"refused", &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, true},
		{"canceled", context.Canceled, false},
		{"plain", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		if got := retry.Retryable(tt.err); got != tt.want {
			t.Errorf("Retryable(%s: %v) = %v, want %v", tt.name, tt.err, got, tt.want)
		}
	}
}

// netURLError mirrors *url.Error without importing net/url in the table.
type netURLError struct {
	Op, URL string
	Err     error
}

func (e *netURLError) Error() string { return e.Op + " " + e.URL + ": " + e.Err.Error() }
func (e *netURLError) Unwrap() error { return e.Err }
func (e *netURLError) Timeout() bool {
	var t interface{ Timeout() bool }
	return errors.As(e.Err, &t) && t.Timeout()
}
func (e *netURLError) Temporary() bool { return false }

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := map[string]time.Duration{
		"5":    5 * time.Second,
		" 30 ": 30 * time.Second,
		"0":    0,
		"-3":   0,
		"soon": 0,
		"":     0,
		now.Add(10 * time.Second).Format(http.TimeFormat): 10 * time.Second,
		now.Add(-time.Minute).Format(http.TimeFormat):     0,
		"99999999999": 24 * time.Hour,
	}
	for value, want := range tests {
		if got := retry.ParseRetryAfter(value, now); got != want {
			t.Errorf("ParseRetryAfter(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestNewStatusErrorKeepsMessageAndRetryAfter(t *testing.T) {
	resp := &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": []string{"7"}}}
	err := retry.NewStatusError(resp, time.Now(), errors.New("stream info request failed with status 503: busy"))
	if err.Error() != "stream info request failed with status 503: busy" {
		t.Fatalf("message = %q", err.Error())
	}
	if retry.HintedDelay(err) != 7*time.Second || !retry.Retryable(err) {
		t.Fatalf("HintedDelay = %v, Retryable = %v", retry.HintedDelay(err), retry.Retryable(err))
	}
}
