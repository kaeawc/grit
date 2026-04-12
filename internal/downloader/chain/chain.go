// Package chain composes multiple downloader.Downloaders into a
// priority-ordered fall-through aggregator.
//
// For each pin passed to Fetch, the chain tries its sub-downloaders in
// order. The first source whose Fetch returns nil wins and the chain
// returns immediately. A source that returns an error wrapping
// downloader.ErrNotFound is treated as "this source does not have the
// file" and the chain falls through to the next source. Any other
// error is a hard stop: it short-circuits the chain and is returned
// to the caller.
//
// The chain is a Layer 2 composition utility. It sits alongside the
// concrete adapters (gradlecache, mavenlocal, mavenremote) rather than
// above them, so callers can build any ordering they like —
// (worktree-local → machine-shared → team-remote → Maven Central →
// Google Maven) is one common shape for mixed Android + JVM builds.
//
// See roadmap/planning/dependency-cache-architecture.md for the role
// this package plays in the overall layer contract.
package chain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
)

// DefaultID is the downloader identifier returned by Downloader.ID when
// the caller does not override it via WithID.
const DefaultID = "chain"

// Downloader is a priority-ordered aggregator over one or more
// downloader.Downloaders.
type Downloader struct {
	sources []downloader.Downloader
	id      string
}

// Option configures a Downloader at construction.
type Option func(*Downloader)

// FetchOutcome describes the result of one source attempt in a chain fetch.
type FetchOutcome string

const (
	FetchOutcomeSuccess  FetchOutcome = "success"
	FetchOutcomeNotFound FetchOutcome = "not_found"
	FetchOutcomeError    FetchOutcome = "error"
)

// FetchRecord captures one source attempt in a chain fetch.
type FetchRecord struct {
	Order      int          `json:"order"`
	SourceID   string       `json:"sourceId"`
	Outcome    FetchOutcome `json:"outcome"`
	DurationMs int64        `json:"durationMs,omitempty"`
	Error      string       `json:"error,omitempty"`
}

// WithID overrides the chain identifier recorded in log messages and
// downstream consumers. The default is DefaultID.
func WithID(id string) Option {
	return func(d *Downloader) {
		if id != "" {
			d.id = id
		}
	}
}

// New returns a Downloader that tries sources in the given order on
// every Fetch call. At least one source is required.
func New(sources []downloader.Downloader, opts ...Option) (*Downloader, error) {
	if len(sources) == 0 {
		return nil, errors.New("chain: at least one source required")
	}
	c := &Downloader{
		sources: append([]downloader.Downloader(nil), sources...),
		id:      DefaultID,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// ID implements downloader.Downloader.
func (c *Downloader) ID() string { return c.id }

// Sources returns a defensive copy of the source chain in priority
// order. Callers may not mutate the chain by modifying the returned
// slice.
func (c *Downloader) Sources() []downloader.Downloader {
	return append([]downloader.Downloader(nil), c.sources...)
}

// FetchWithRecords behaves like Fetch but also returns a structured trace
// of the source attempts made in priority order.
func (c *Downloader) FetchWithRecords(ctx context.Context, pin lockfile.Pin, store cas.Store) ([]FetchRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records := make([]FetchRecord, 0, len(c.sources))
	var lastNotFound error
	for i, src := range c.sources {
		start := time.Now()
		err := src.Fetch(ctx, pin, store)
		record := FetchRecord{
			Order:      i,
			SourceID:   src.ID(),
			DurationMs: time.Since(start).Milliseconds(),
		}
		if err == nil {
			record.Outcome = FetchOutcomeSuccess
			records = append(records, record)
			return records, nil
		}
		if errors.Is(err, downloader.ErrNotFound) {
			record.Outcome = FetchOutcomeNotFound
			record.Error = err.Error()
			records = append(records, record)
			lastNotFound = err
			continue
		}
		record.Outcome = FetchOutcomeError
		record.Error = err.Error()
		records = append(records, record)
		return records, fmt.Errorf("chain: source %s: %w", src.ID(), err)
	}
	if lastNotFound != nil {
		return records, fmt.Errorf("chain: no source provided %s: %w", pin.Coordinate, lastNotFound)
	}
	// Unreachable: New guarantees len(sources) >= 1 and every source
	// either succeeds, returns ErrNotFound, or returns a hard error.
	return records, fmt.Errorf("chain: no sources available for %s", pin.Coordinate)
}

// Fetch tries each source in order. For each source:
//
//   - nil error → the chain is done, return nil
//   - error wrapping downloader.ErrNotFound → fall through to the
//     next source
//   - any other error → short-circuit and return it
//
// If every source returns downloader.ErrNotFound, Fetch returns a
// fmt.Errorf wrapping the last ErrNotFound so errors.Is(err,
// downloader.ErrNotFound) remains true. Callers can therefore chain
// aggregators transparently.
//
// Because downloader.Fetch takes an entire pin, fall-through operates
// at pin granularity, not file granularity. A source that partially
// populates a pin before returning ErrNotFound leaves the already-
// stored blobs in the CAS; the next source sees them as present via
// store.Has and skips them. This is safe because content addressing
// guarantees the already-stored blobs are the same bytes the next
// source would have fetched anyway.
func (c *Downloader) Fetch(ctx context.Context, pin lockfile.Pin, store cas.Store) error {
	_, err := c.FetchWithRecords(ctx, pin, store)
	return err
}

// Compile-time assertion that *Downloader satisfies downloader.Downloader.
var _ downloader.Downloader = (*Downloader)(nil)
