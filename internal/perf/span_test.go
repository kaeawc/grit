package perf

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSpanWithoutAttributesOrMetricsLeavesMapsNil(t *testing.T) {
	tracker := NewRecorder()

	NewSpan(tracker, "bare").Stop()

	entries := tracker.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Attributes != nil {
		t.Fatalf("expected nil Attributes, got %v", entry.Attributes)
	}
	if entry.Metrics != nil {
		t.Fatalf("expected nil Metrics, got %v", entry.Metrics)
	}
}

// A Span built against a disabled tracker must not record anything,
// must not panic, and must allocate nothing on the heap.
func TestSpanDisabledTrackerIsNoop(t *testing.T) {
	tracker := New(false)

	span := NewSpan(tracker, "noop")
	span.SetAttr("k", "v")
	span.AddMetric("m", 7)
	span.Stop()
	// Second Stop must not panic.
	span.Stop()

	if got := tracker.GetTimings(); got != nil {
		t.Fatalf("disabled tracker recorded entries: %+v", got)
	}
}

// A nil tracker must yield a no-op span. Defends against call sites
// that conditionally build a tracker.
func TestSpanNilTrackerIsNoop(t *testing.T) {
	span := NewSpan(nil, "no-tracker")
	// Calls must be safe.
	span.SetAttr("k", "v")
	span.AddMetric("m", 1)
	span.Stop()
	span.Stop()
}

// Calling Stop twice must record exactly one entry.
func TestSpanStopIsIdempotent(t *testing.T) {
	tracker := NewRecorder()
	span := NewSpan(tracker, "once")
	span.Stop()
	span.Stop()
	span.Stop()

	if got := len(tracker.Entries()); got != 1 {
		t.Fatalf("expected 1 entry after repeated Stop, got %d", got)
	}
}

// SetAttr / AddMetric calls after Stop must be silently ignored and
// must not mutate the recorded snapshot.
func TestSpanMutationAfterStopIgnored(t *testing.T) {
	tracker := NewRecorder()
	span := NewSpan(tracker, "stopped")
	span.SetAttr("k", "before")
	span.Stop()
	span.SetAttr("k", "after")
	span.AddMetric("late", 99)

	entries := tracker.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if got, want := entry.Attributes["k"], "before"; got != want {
		t.Fatalf("entry.Attributes[k] = %q, want %q (post-Stop mutation leaked)", got, want)
	}
	if _, ok := entry.Metrics["late"]; ok {
		t.Fatalf("post-Stop AddMetric leaked into recorded entry: %v", entry.Metrics)
	}
}

// Span must nest under whatever block is current on the tracker.
// Closing a Serial block after Stop should fold the span's entry into
// the block's children.
func TestSpanRecordsIntoCurrentBlock(t *testing.T) {
	tracker := NewRecorder()

	tracker.Serial("outer")
	NewSpan(tracker, "inner").Stop()
	tracker.End()

	entries := tracker.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 top-level entry, got %d", len(entries))
	}
	outer := entries[0]
	if outer.Name != "outer" {
		t.Fatalf("top-level name = %q, want %q", outer.Name, "outer")
	}
	children := outer.Children.Entries()
	if len(children) != 1 || children[0].Name != "inner" {
		t.Fatalf("expected one inner child, got %+v", children)
	}
}

// Concurrent SetAttr / AddMetric calls on the same active span must
// race-clean (verified via -race) and must land in the recorded entry.
func TestSpanConcurrentMutation(t *testing.T) {
	tracker := NewRecorder()
	span := NewSpan(tracker, "concurrent")

	const writers = 16
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			span.SetAttr("worker", "active")
			span.AddMetric("calls", int64(i))
		}(i)
	}
	wg.Wait()
	span.Stop()

	entries := tracker.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Attributes["worker"] != "active" {
		t.Fatalf("attribute write did not survive: %+v", entries[0].Attributes)
	}
	calls := entries[0].Metrics["calls"]
	if calls < 0 || calls >= writers {
		t.Fatalf("metric out of range [0, %d): got %d", writers, calls)
	}
}

// A span recorded into a Parallel block must land in the block's
// map-shaped TimingData (companion to TestSpanRecordsIntoCurrentBlock).
func TestSpanRecordsIntoParallelBlock(t *testing.T) {
	tracker := NewRecorder()
	tracker.Parallel("outer")
	NewSpan(tracker, "a").Stop()
	NewSpan(tracker, "b").Stop()
	tracker.End()

	entries := tracker.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 top-level entry, got %d", len(entries))
	}
	children := entries[0].Children
	if children == nil {
		t.Fatalf("parallel block lost its children")
	}
	if got := children.shape; got != timingShapeMap {
		t.Fatalf("expected parallel block children to use map shape, got %v", got)
	}
	names := map[string]bool{}
	for _, child := range children.entries {
		names[child.Name] = true
	}
	if !names["a"] || !names["b"] {
		t.Fatalf("expected child names {a, b}, got %v", names)
	}
}

// Active span allocates; disabled span does not. Guards the
// "zero-allocation no-op" claim in NewSpan's godoc.
func TestSpanDisabledIsZeroAlloc(t *testing.T) {
	tracker := New(false)
	var calls atomic.Int64
	allocs := testing.AllocsPerRun(100, func() {
		span := NewSpan(tracker, "noop")
		span.SetAttr("k", "v")
		span.AddMetric("m", 1)
		span.Stop()
		calls.Add(1)
	})
	if allocs != 0 {
		t.Fatalf("disabled span should be zero-alloc, got %v allocs/op", allocs)
	}
}

// A span carries its name, attributes, and metrics into the recorded
// entry, and the entry's maps must be independent from the caller's
// view — mutating one read must not affect the next.
func TestSpanRecordsAttributesAndMetricsAsCopies(t *testing.T) {
	tracker := NewRecorder()

	span := NewSpan(tracker, "request")
	span.SetAttr("url", "https://example.com")
	span.AddMetric("bytes", 1024)
	span.Stop()

	entries := tracker.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Name != "request" {
		t.Fatalf("entry.Name = %q, want %q", entry.Name, "request")
	}
	if got, want := entry.Attributes["url"], "https://example.com"; got != want {
		t.Fatalf("entry.Attributes[url] = %q, want %q", got, want)
	}
	if got, want := entry.Metrics["bytes"], int64(1024); got != want {
		t.Fatalf("entry.Metrics[bytes] = %d, want %d", got, want)
	}

	// Mutate the returned copy; subsequent reads should be unaffected.
	entry.Attributes["url"] = "mutated"
	entry.Metrics["bytes"] = 999

	fresh := tracker.Entries()
	if fresh[0].Attributes["url"] != "https://example.com" {
		t.Fatalf("caller mutation leaked back into recorder: %+v", fresh[0].Attributes)
	}
	if fresh[0].Metrics["bytes"] != 1024 {
		t.Fatalf("caller mutation leaked back into recorder: %+v", fresh[0].Metrics)
	}
}

// Duration must be at least the wall-clock time the span was open.
// We sleep for a small but measurable window so the >0 guard is real
// rather than relying on millisecond rounding luck.
func TestSpanCapturesDuration(t *testing.T) {
	tracker := NewRecorder()
	span := NewSpan(tracker, "elapsed")
	time.Sleep(5 * time.Millisecond)
	span.Stop()

	entries := tracker.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].DurationMs < 1 {
		t.Fatalf("expected DurationMs >= 1 after 5ms sleep, got %d", entries[0].DurationMs)
	}
}
