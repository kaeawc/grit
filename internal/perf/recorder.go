package perf

import (
	"sync"
	"time"

	"github.com/kaeawc/grit/internal/explain"
)

type RecordedCall struct {
	Kind        string
	Name        string
	DurationMs  int64
	Depth       int
	Explanation *explain.Timing
}

type RecorderTracker struct {
	mu            sync.Mutex
	root          *recordingBlock
	current       *recordingBlock
	enabled       bool
	nextDurations []int64
	calls         []RecordedCall
}

type recordingBlock struct {
	name    string
	kind    blockType
	start   time.Time
	entries []TimingEntry
	parent  *recordingBlock
}

func NewRecorder() *RecorderTracker {
	root := &recordingBlock{
		name:  "root",
		kind:  serialBlock,
		start: time.Now(),
	}
	return &RecorderTracker{
		root:    root,
		current: root,
		enabled: true,
	}
}

func (t *RecorderTracker) QueueDurations(durations ...int64) *RecorderTracker {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextDurations = append(t.nextDurations, durations...)
	return t
}

func (t *RecorderTracker) Serial(name string) Tracker {
	t.mu.Lock()
	defer t.mu.Unlock()
	block := &recordingBlock{
		name:   name,
		kind:   serialBlock,
		start:  time.Now(),
		parent: t.current,
	}
	t.current = block
	t.calls = append(t.calls, RecordedCall{Kind: "serial", Name: name, Depth: t.depthLocked()})
	return t
}

func (t *RecorderTracker) Parallel(name string) Tracker {
	t.mu.Lock()
	defer t.mu.Unlock()
	block := &recordingBlock{
		name:   name,
		kind:   parallelBlock,
		start:  time.Now(),
		parent: t.current,
	}
	t.current = block
	t.calls = append(t.calls, RecordedCall{Kind: "parallel", Name: name, Depth: t.depthLocked()})
	return t
}

func (t *RecorderTracker) Track(name string, fn func() error) error {
	t.mu.Lock()
	duration := int64(0)
	if len(t.nextDurations) > 0 {
		duration = t.nextDurations[0]
		t.nextDurations = t.nextDurations[1:]
	}
	current := t.current
	depth := t.depthLocked()
	t.mu.Unlock()

	err := fn()
	entry := TimingEntry{
		Name:        name,
		DurationMs:  duration,
		Explanation: explain.InferTiming(name, duration, err),
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if current == nil {
		current = t.root
	}
	current.entries = append(current.entries, entry)
	t.calls = append(t.calls, RecordedCall{
		Kind:        "track",
		Name:        name,
		DurationMs:  duration,
		Depth:       depth,
		Explanation: entry.Explanation,
	})
	return err
}

func (t *RecorderTracker) Record(entry TimingEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current == nil {
		t.current = t.root
	}
	t.current.entries = append(t.current.entries, cloneTimingEntry(entry))
	t.calls = append(t.calls, RecordedCall{
		Kind:        "record",
		Name:        entry.Name,
		DurationMs:  entry.DurationMs,
		Depth:       t.depthLocked(),
		Explanation: entry.Explanation,
	})
}

func (t *RecorderTracker) End() Tracker {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current == nil || t.current.parent == nil {
		return t
	}
	entry := TimingEntry{
		Name:       t.current.name,
		DurationMs: 0,
		Children:   t.entriesData(t.current),
	}
	t.current = t.current.parent
	t.current.entries = append(t.current.entries, entry)
	t.calls = append(t.calls, RecordedCall{Kind: "end", Name: entry.Name, Depth: t.depthLocked()})
	return t
}

func (t *RecorderTracker) GetTimings() *TimingData {
	t.mu.Lock()
	defer t.mu.Unlock()
	for t.current != nil && t.current.parent != nil {
		entry := TimingEntry{
			Name:       t.current.name,
			DurationMs: 0,
			Children:   t.entriesData(t.current),
		}
		t.current = t.current.parent
		t.current.entries = append(t.current.entries, entry)
	}
	if t.root == nil {
		return nil
	}
	return t.entriesData(t.root)
}

func (t *RecorderTracker) IsEnabled() bool {
	return t != nil && t.enabled
}

func (t *RecorderTracker) Entries() []TimingEntry {
	timings := t.GetTimings()
	if timings == nil {
		return nil
	}
	return timings.Entries()
}

func (t *RecorderTracker) Calls() []RecordedCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneRecordedCalls(t.calls)
}

func (t *RecorderTracker) depthLocked() int {
	depth := 0
	for current := t.current; current != nil && current.parent != nil; current = current.parent {
		depth++
	}
	return depth
}

func (t *RecorderTracker) entriesData(block *recordingBlock) *TimingData {
	if block == nil {
		return nil
	}
	if block.kind == parallelBlock {
		return newTimingMap(cloneTimingEntries(block.entries))
	}
	return newTimingList(cloneTimingEntries(block.entries))
}

func cloneRecordedCalls(calls []RecordedCall) []RecordedCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]RecordedCall, len(calls))
	for i, call := range calls {
		out[i] = call
		if call.Explanation != nil {
			explanation := *call.Explanation
			out[i].Explanation = &explanation
		}
	}
	return out
}
