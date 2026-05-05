package perf

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kaeawc/grit/internal/explain"
)

func TestTimingDataMarshalSerial(t *testing.T) {
	t.Parallel()

	data := newTimingList([]TimingEntry{
		{Name: "load", DurationMs: 12},
		{Name: "run", DurationMs: 34},
	})
	got, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `[{"name":"load","durationMs":12},{"name":"run","durationMs":34}]`
	if string(got) != want {
		t.Fatalf("marshal mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestTimingDataMarshalParallel(t *testing.T) {
	t.Parallel()

	data := newTimingMap([]TimingEntry{
		{Name: "load", DurationMs: 12},
		{Name: "run", DurationMs: 34},
	})
	got, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]TimingEntry
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("parallel timing should marshal as map, got %d entries", len(decoded))
	}
	if decoded["load"].DurationMs != 12 || decoded["run"].DurationMs != 34 {
		t.Fatalf("unexpected decoded values: %#v", decoded)
	}
}

func TestListCopiesInputEntries(t *testing.T) {
	t.Parallel()

	entries := []TimingEntry{{Name: "load", DurationMs: 12}}
	data := List(entries)
	entries[0] = TimingEntry{Name: "mutated", DurationMs: 99}

	got := data.Entries()
	if len(got) != 1 {
		t.Fatalf("entries len = %d, want 1", len(got))
	}
	if got[0].Name != "load" || got[0].DurationMs != 12 {
		t.Fatalf("List aliased caller entries: %#v", got[0])
	}
}

func TestListCopiesNestedEntryMetadata(t *testing.T) {
	t.Parallel()

	explanation := &explain.Timing{State: explain.StateReused, Basis: "test"}
	children := List([]TimingEntry{{Name: "child", DurationMs: 1}})
	entries := []TimingEntry{{
		Name:        "phase",
		DurationMs:  12,
		Children:    children,
		Explanation: explanation,
	}}

	data := List(entries)
	explanation.State = explain.StateRebuilt
	if err := children.UnmarshalJSON([]byte(`[{"name":"mutated","durationMs":99}]`)); err != nil {
		t.Fatalf("mutate children: %v", err)
	}

	got := data.Entries()
	if got[0].Explanation == nil || got[0].Explanation.State != explain.StateReused {
		t.Fatalf("List aliased timing explanation: %#v", got[0].Explanation)
	}
	childEntries := got[0].Children.Entries()
	if len(childEntries) != 1 || childEntries[0].Name != "child" {
		t.Fatalf("List aliased child timings: %#v", childEntries)
	}
}

func TestTimingDataEntriesReturnDeepCopies(t *testing.T) {
	t.Parallel()

	data := List([]TimingEntry{{
		Name:        "phase",
		DurationMs:  12,
		Children:    List([]TimingEntry{{Name: "child", DurationMs: 1}}),
		Explanation: &explain.Timing{State: explain.StateReused, Basis: "test"},
	}})

	entries := data.Entries()
	entries[0].Explanation.State = explain.StateRebuilt
	if err := entries[0].Children.UnmarshalJSON([]byte(`[{"name":"mutated","durationMs":99}]`)); err != nil {
		t.Fatalf("mutate returned children: %v", err)
	}

	fresh := data.Entries()
	if fresh[0].Explanation == nil || fresh[0].Explanation.State != explain.StateReused {
		t.Fatalf("Entries aliased timing explanation: %#v", fresh[0].Explanation)
	}
	childEntries := fresh[0].Children.Entries()
	if len(childEntries) != 1 || childEntries[0].Name != "child" {
		t.Fatalf("Entries aliased child timings: %#v", childEntries)
	}
}

func TestMapCopiesInputEntries(t *testing.T) {
	t.Parallel()

	entries := []TimingEntry{{Name: "compile", DurationMs: 34}}
	data := Map(entries)
	entries[0] = TimingEntry{Name: "mutated", DurationMs: 99}

	got, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]TimingEntry
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["mutated"]; ok {
		t.Fatalf("Map aliased caller entries: %#v", decoded)
	}
	if decoded["compile"].DurationMs != 34 {
		t.Fatalf("unexpected copied map entry: %#v", decoded)
	}
}

func TestTrackerNestedTimings(t *testing.T) {
	t.Parallel()

	tracker := New(true)
	if err := tracker.Track("root", func() error { return nil }); err != nil {
		t.Fatalf("track root: %v", err)
	}
	tracker = tracker.Serial("phase")
	if err := tracker.Track("child", func() error { return errors.New("boom") }); err == nil {
		t.Fatal("expected error from child track")
	}
	tracker = tracker.End()

	timings := tracker.GetTimings()
	if timings == nil {
		t.Fatal("expected timings")
	}
	got, err := json.Marshal(timings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected marshaled json")
	}
}

func TestTrackerRecordAppendsSyntheticEntries(t *testing.T) {
	t.Parallel()

	explanation := &explain.Timing{State: explain.StateReused, Basis: "test"}
	children := Map([]TimingEntry{
		{Name: "compile", DurationMs: 5},
		{Name: "assemble", DurationMs: 7},
	})
	tracker := New(true)
	tracker.Record(TimingEntry{
		Name:        "batch",
		DurationMs:  12,
		Children:    children,
		Explanation: explanation,
	})
	explanation.State = explain.StateRebuilt
	if err := children.UnmarshalJSON([]byte(`[{"name":"mutated","durationMs":99}]`)); err != nil {
		t.Fatalf("mutate children: %v", err)
	}

	timings := tracker.GetTimings()
	if timings == nil {
		t.Fatal("expected timings")
	}
	got, err := json.Marshal(timings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded []TimingEntry
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Name != "batch" || decoded[0].Children == nil {
		t.Fatalf("unexpected decoded timings: %#v", decoded)
	}
	if decoded[0].Explanation == nil || decoded[0].Explanation.State != explain.StateReused {
		t.Fatalf("tracker record aliased explanation: %#v", decoded[0].Explanation)
	}
	if childEntries := decoded[0].Children.Entries(); len(childEntries) != 2 {
		t.Fatalf("tracker record aliased child timings: %#v", childEntries)
	}
}
