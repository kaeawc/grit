package remotecache

import (
	"context"
	"sync/atomic"
)

// ReadCounter tracks remote-cache response bytes observed through a context.
// It is intended for admission reconciliation: schedulers reserve estimated
// bandwidth up front, then release the unused portion after the action
// completes based on the bytes actually read from the remote cache.
type ReadCounter struct {
	bytes atomic.Int64
}

type readCounterKey struct{}

// WithReadCounter returns a derived context that accumulates remote-cache read
// bytes performed through this package, plus the associated counter.
func WithReadCounter(ctx context.Context) (context.Context, *ReadCounter) {
	counter := &ReadCounter{}
	return context.WithValue(ctx, readCounterKey{}, counter), counter
}

func readCounterFromContext(ctx context.Context) *ReadCounter {
	counter, _ := ctx.Value(readCounterKey{}).(*ReadCounter)
	return counter
}

func observeReadBytes(ctx context.Context, bytes int64) {
	if bytes <= 0 {
		return
	}
	if counter := readCounterFromContext(ctx); counter != nil {
		counter.Add(bytes)
	}
}

// Add records bytes read from the remote cache.
func (c *ReadCounter) Add(bytes int64) {
	if c == nil || bytes <= 0 {
		return
	}
	c.bytes.Add(bytes)
}

// Bytes returns the cumulative remote-cache bytes observed on the counter.
func (c *ReadCounter) Bytes() int64 {
	if c == nil {
		return 0
	}
	return c.bytes.Load()
}
