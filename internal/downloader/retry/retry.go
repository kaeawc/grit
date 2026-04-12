// Package retry wraps a downloader.Downloader with configurable retry
// and backoff behavior.
//
// The wrapper is intentionally small: it does not change downloader
// contracts, it simply repeats Fetch calls after retryable failures.
// Callers can tune the attempt count, backoff schedule, retry predicate,
// and sleep function without changing the wrapped downloader.
package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
)

// Backoff returns the delay before the next retry attempt. The attempt
// number starts at 1 for the first retry after an initial failure.
type Backoff func(attempt int) time.Duration

// Sleeper waits for the requested duration or returns early if ctx is
// canceled.
type Sleeper func(ctx context.Context, d time.Duration) error

// ShouldRetry decides whether a failed Fetch call should be retried.
type ShouldRetry func(error) bool

// Option configures a retry Downloader.
type Option func(*Downloader)

// Downloader wraps a downstream downloader.Downloader with retry logic.
type Downloader struct {
	inner       downloader.Downloader
	attempts    int
	backoff     Backoff
	sleeper     Sleeper
	shouldRetry ShouldRetry
}

// New returns a retry wrapper around inner.
func New(inner downloader.Downloader, opts ...Option) (*Downloader, error) {
	if inner == nil {
		return nil, errors.New("retry: inner downloader is required")
	}
	d := &Downloader{
		inner:    inner,
		attempts: 1,
		backoff:  func(int) time.Duration { return 0 },
		sleeper:  sleepContext,
		shouldRetry: func(err error) bool {
			return !errors.Is(err, downloader.ErrNotFound)
		},
	}
	for _, opt := range opts {
		opt(d)
	}
	if d.attempts < 1 {
		return nil, fmt.Errorf("retry: attempts must be >= 1, got %d", d.attempts)
	}
	if d.backoff == nil {
		d.backoff = func(int) time.Duration { return 0 }
	}
	if d.sleeper == nil {
		d.sleeper = sleepContext
	}
	if d.shouldRetry == nil {
		d.shouldRetry = func(err error) bool {
			return !errors.Is(err, downloader.ErrNotFound)
		}
	}
	return d, nil
}

// WithAttempts sets the maximum number of Fetch calls per invocation.
// attempts counts the initial call, so attempts=3 allows two retries.
func WithAttempts(attempts int) Option {
	return func(d *Downloader) {
		d.attempts = attempts
	}
}

// WithBackoff overrides the delay schedule used between retries.
func WithBackoff(backoff Backoff) Option {
	return func(d *Downloader) {
		d.backoff = backoff
	}
}

// WithSleeper overrides the wait function used between retries.
func WithSleeper(sleeper Sleeper) Option {
	return func(d *Downloader) {
		d.sleeper = sleeper
	}
}

// WithRetryable overrides the predicate used to decide whether an error
// should be retried.
func WithRetryable(shouldRetry ShouldRetry) Option {
	return func(d *Downloader) {
		d.shouldRetry = shouldRetry
	}
}

// ID implements downloader.Downloader.
func (d *Downloader) ID() string {
	return d.inner.ID()
}

// Inner returns the wrapped downloader.
func (d *Downloader) Inner() downloader.Downloader {
	return d.inner
}

// Fetch retries the wrapped downloader according to the configured
// policy. Not-found errors are returned immediately so callers can
// preserve ordinary fall-through behavior in chain compositions.
func (d *Downloader) Fetch(ctx context.Context, pin lockfile.Pin, store cas.Store) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= d.attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := d.inner.Fetch(ctx, pin, store)
		if err == nil {
			return nil
		}
		if errors.Is(err, downloader.ErrNotFound) {
			return err
		}
		lastErr = err
		if !d.shouldRetry(err) {
			return err
		}
		if attempt == d.attempts {
			return fmt.Errorf("retry: exhausted after %d attempt(s): %w", d.attempts, err)
		}
		delay := d.backoff(attempt)
		if delay <= 0 {
			continue
		}
		if err := d.sleeper(ctx, delay); err != nil {
			return err
		}
	}
	return fmt.Errorf("retry: exhausted after %d attempt(s): %w", d.attempts, lastErr)
}

// Compile-time assertion that *Downloader satisfies downloader.Downloader.
var _ downloader.Downloader = (*Downloader)(nil)

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
