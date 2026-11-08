// Package retry provides a bounded exponential-backoff policy for idempotent
// upstream requests and a classifier that retries only transient failures.
package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// DefaultBaseDelay is the backoff ceiling before the first retry.
	DefaultBaseDelay = time.Second
	// DefaultMaxDelay caps a single backoff step.
	DefaultMaxDelay = 30 * time.Second
)

// Clock abstracts time so tests can run without real sleeps.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// RealClock returns the wall clock.
func RealClock() Clock { return realClock{} }

// Policy describes how an operation is retried. The zero value makes exactly
// one attempt.
type Policy struct {
	// Budget bounds the total time spent waiting between attempts. Time
	// spent inside an attempt is not charged, so a slow or timed-out request
	// still leaves room to retry. Zero disables retries.
	Budget    time.Duration
	BaseDelay time.Duration
	MaxDelay  time.Duration
	Clock     Clock
	// Jitter maps a backoff ceiling to the delay actually waited. Nil uses
	// full jitter: a uniform delay in [0, ceiling].
	Jitter func(ceiling time.Duration) time.Duration
	// OnRetry, when set, is called before each backoff sleep.
	OnRetry func(attempt int, delay time.Duration, err error)
}

// Default returns the production policy for the given total budget. A
// budget of zero or less disables retries.
func Default(budget time.Duration) Policy {
	if budget < 0 {
		budget = 0
	}
	return Policy{
		Budget:    budget,
		BaseDelay: DefaultBaseDelay,
		MaxDelay:  DefaultMaxDelay,
		Clock:     RealClock(),
	}
}

// FullJitter returns a uniformly random duration in [0, ceiling].
func FullJitter(ceiling time.Duration) time.Duration {
	if ceiling <= 0 {
		return 0
	}
	// Backoff jitter only spreads load; it is not security-sensitive.
	return time.Duration(rand.Int64N(int64(ceiling) + 1)) // #nosec G404
}

// Do runs op until it succeeds, fails permanently, or the policy is
// exhausted. It returns the number of attempts made and the last error. A
// canceled ctx stops retrying and returns ctx.Err() if the cancellation
// happened while waiting.
func (p Policy) Do(ctx context.Context, op func(ctx context.Context) error) (int, error) {
	clock := p.Clock
	if clock == nil {
		clock = RealClock()
	}
	var waited time.Duration
	attempts := 0
	for {
		attempts++
		err := op(ctx)
		if err == nil {
			return attempts, nil
		}
		if ctx.Err() != nil || !Retryable(err) || p.Budget <= 0 {
			return attempts, err
		}
		delay := p.delay(attempts)
		if hinted := HintedDelay(err); hinted > delay {
			delay = hinted
		}
		if delay > p.Budget-waited {
			return attempts, err
		}
		if p.OnRetry != nil {
			p.OnRetry(attempts, delay, err)
		}
		if sleepErr := clock.Sleep(ctx, delay); sleepErr != nil {
			return attempts, sleepErr
		}
		waited += delay
	}
}

// delay returns the jittered backoff before attempt number attempts+1.
func (p Policy) delay(attempts int) time.Duration {
	base, maxDelay := p.BaseDelay, p.MaxDelay
	if base <= 0 {
		base = DefaultBaseDelay // never retry without backoff
	}
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}
	ceiling := Backoff(base, maxDelay, attempts-1)
	jitter := p.Jitter
	if jitter == nil {
		jitter = FullJitter
	}
	delay := jitter(ceiling)
	if delay < 0 {
		return 0
	}
	return delay
}

// Backoff returns min(maxDelay, base*2^step) without overflowing. A
// non-positive maxDelay leaves the value uncapped.
func Backoff(base, maxDelay time.Duration, step int) time.Duration {
	if base <= 0 {
		return 0
	}
	d := base
	for i := 0; i < step; i++ {
		if maxDelay > 0 && d >= maxDelay {
			break
		}
		if d > math.MaxInt64/2 {
			d = math.MaxInt64
			break
		}
		d <<= 1
	}
	if maxDelay > 0 && d > maxDelay {
		return maxDelay
	}
	return d
}

// StatusError marks a non-success HTTP response. It wraps the caller's
// already-sanitized error so the message is unchanged.
type StatusError struct {
	StatusCode int
	RetryAfter time.Duration
	Err        error
}

func (e *StatusError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("upstream request failed with status %d", e.StatusCode)
	}
	return e.Err.Error()
}

func (e *StatusError) Unwrap() error { return e.Err }

// NewStatusError wraps err with the response status and its Retry-After
// header, parsed relative to now.
func NewStatusError(resp *http.Response, now time.Time, err error) error {
	statusErr := &StatusError{Err: err}
	if resp != nil {
		statusErr.StatusCode = resp.StatusCode
		statusErr.RetryAfter = ParseRetryAfter(resp.Header.Get("Retry-After"), now)
	}
	return statusErr
}

// RetryableStatus reports whether an HTTP status is worth retrying: any 5xx
// (including Impartus's 516), 429 and 408.
func RetryableStatus(code int) bool {
	return code >= 500 && code <= 599 || code == http.StatusTooManyRequests || code == http.StatusRequestTimeout
}

// Retryable reports whether err is a transient upstream failure. It is an
// allowlist: authentication, policy, size and parse errors are permanent.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return RetryableStatus(statusErr.StatusCode)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	for _, transient := range []error{
		syscall.ECONNRESET,
		syscall.ECONNABORTED,
		syscall.ECONNREFUSED,
		syscall.EPIPE,
		io.EOF, // connection closed before any response on these idempotent requests
		io.ErrUnexpectedEOF,
	} {
		if errors.Is(err, transient) {
			return true
		}
	}
	return false
}

// HintedDelay returns the server-requested (Retry-After) delay carried by
// err, if any.
func HintedDelay(err error) time.Duration {
	var statusErr *StatusError
	if errors.As(err, &statusErr) && statusErr.RetryAfter > 0 {
		return statusErr.RetryAfter
	}
	return 0
}

// maxRetryAfter keeps a hostile or broken header from overflowing.
const maxRetryAfter = 24 * time.Hour

// ParseRetryAfter parses a Retry-After value in delta-seconds or HTTP-date
// form. Invalid, negative and past values return zero.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > int64(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	delay := when.Sub(now)
	if delay <= 0 {
		return 0
	}
	if delay > maxRetryAfter {
		return maxRetryAfter
	}
	return delay
}
