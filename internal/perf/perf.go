package perf

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/kaeawc/grit/internal/explain"
)

type TimingEntry struct {
	Name        string          `json:"name"`
	DurationMs  int64           `json:"durationMs"`
	Children    *TimingData     `json:"children,omitempty"`
	Explanation *explain.Timing `json:"explanation,omitempty"`
}

type timingShape uint8

const (
	timingShapeList timingShape = iota
	timingShapeMap
)

type TimingData struct {
	shape   timingShape
	entries []TimingEntry
}

func (d *TimingData) Entries() []TimingEntry {
	if d == nil || len(d.entries) == 0 {
		return nil
	}
	out := make([]TimingEntry, len(d.entries))
	copy(out, d.entries)
	return out
}

func newTimingList(entries []TimingEntry) *TimingData {
	return &TimingData{shape: timingShapeList, entries: entries}
}

func newTimingMap(entries []TimingEntry) *TimingData {
	return &TimingData{shape: timingShapeMap, entries: entries}
}

func List(entries []TimingEntry) *TimingData {
	return newTimingList(cloneTimingEntries(entries))
}

func Map(entries []TimingEntry) *TimingData {
	return newTimingMap(cloneTimingEntries(entries))
}

func cloneTimingEntries(entries []TimingEntry) []TimingEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]TimingEntry, len(entries))
	copy(out, entries)
	return out
}

func (d *TimingData) MarshalJSON() ([]byte, error) {
	if d == nil {
		return []byte("null"), nil
	}
	switch d.shape {
	case timingShapeMap:
		out := make(map[string]TimingEntry, len(d.entries))
		for _, entry := range d.entries {
			out[entry.Name] = entry
		}
		return json.Marshal(out)
	default:
		return json.Marshal(d.entries)
	}
}

func (d *TimingData) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = TimingData{}
		return nil
	}
	var list []TimingEntry
	if err := json.Unmarshal(data, &list); err == nil {
		d.shape = timingShapeList
		d.entries = list
		return nil
	}
	var m map[string]TimingEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	entries := make([]TimingEntry, 0, len(m))
	for _, entry := range m {
		entries = append(entries, entry)
	}
	d.shape = timingShapeMap
	d.entries = entries
	return nil
}

type Tracker interface {
	Serial(name string) Tracker
	Parallel(name string) Tracker
	Track(name string, fn func() error) error
	Record(entry TimingEntry)
	End() Tracker
	GetTimings() *TimingData
	IsEnabled() bool
}

type blockType string

const (
	serialBlock   blockType = "serial"
	parallelBlock blockType = "parallel"
)

type timingBlock struct {
	name    string
	kind    blockType
	start   time.Time
	entries []TimingEntry
	parent  *timingBlock
}

type DefaultTracker struct {
	mu      sync.Mutex
	root    *timingBlock
	current *timingBlock
	enabled bool
}

type DisabledTracker struct{}

func New(enabled bool) Tracker {
	if !enabled {
		return DisabledTracker{}
	}
	root := &timingBlock{
		name:  "root",
		kind:  serialBlock,
		start: time.Now(),
	}
	return &DefaultTracker{
		root:    root,
		current: root,
		enabled: true,
	}
}

func (t *DefaultTracker) Serial(name string) Tracker {
	block := &timingBlock{
		name:   name,
		kind:   serialBlock,
		start:  time.Now(),
		parent: t.current,
	}
	t.current = block
	return t
}

func (t *DefaultTracker) Parallel(name string) Tracker {
	block := &timingBlock{
		name:   name,
		kind:   parallelBlock,
		start:  time.Now(),
		parent: t.current,
	}
	t.current = block
	return t
}

func (t *DefaultTracker) Track(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	duration := time.Since(start).Milliseconds()
	t.mu.Lock()
	t.current.entries = append(t.current.entries, TimingEntry{
		Name:        name,
		DurationMs:  duration,
		Explanation: explain.InferTiming(name, duration, err),
	})
	t.mu.Unlock()
	return err
}

func (t *DefaultTracker) Record(entry TimingEntry) {
	if t.current == nil {
		return
	}
	t.mu.Lock()
	t.current.entries = append(t.current.entries, entry)
	t.mu.Unlock()
}

func (t *DefaultTracker) End() Tracker {
	if t.current == nil || t.current.parent == nil {
		return t
	}
	entry := TimingEntry{
		Name:       t.current.name,
		DurationMs: time.Since(t.current.start).Milliseconds(),
		Children:   t.entriesData(t.current),
	}
	t.current = t.current.parent
	t.current.entries = append(t.current.entries, entry)
	return t
}

func (t *DefaultTracker) GetTimings() *TimingData {
	for t.current != nil && t.current.parent != nil {
		t.End()
	}
	if t.root == nil {
		return nil
	}
	return t.entriesData(t.root)
}

func (t *DefaultTracker) IsEnabled() bool {
	return t.enabled
}

func (t *DefaultTracker) entriesData(block *timingBlock) *TimingData {
	t.mu.Lock()
	out := make([]TimingEntry, len(block.entries))
	copy(out, block.entries)
	kind := block.kind
	t.mu.Unlock()
	if kind == parallelBlock {
		return newTimingMap(out)
	}
	return newTimingList(out)
}

func (DisabledTracker) Serial(string) Tracker                 { return DisabledTracker{} }
func (DisabledTracker) Parallel(string) Tracker               { return DisabledTracker{} }
func (DisabledTracker) Track(_ string, fn func() error) error { return fn() }
func (DisabledTracker) Record(TimingEntry)                    {}
func (DisabledTracker) End() Tracker                          { return DisabledTracker{} }
func (DisabledTracker) GetTimings() *TimingData               { return nil }
func (DisabledTracker) IsEnabled() bool                       { return false }
