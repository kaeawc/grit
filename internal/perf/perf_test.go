package perf

import (
	"encoding/json"
	"errors"
	"testing"
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

	tracker := New(true)
	tracker.Record(TimingEntry{
		Name:       "batch",
		DurationMs: 12,
		Children: Map([]TimingEntry{
			{Name: "compile", DurationMs: 5},
			{Name: "assemble", DurationMs: 7},
		}),
	})

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
}
