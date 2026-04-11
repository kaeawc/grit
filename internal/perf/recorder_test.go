package perf

import (
	"encoding/json"
	"testing"
)

func TestRecorderTrackerUsesScriptedDurations(t *testing.T) {
	t.Parallel()

	tracker := NewRecorder().QueueDurations(0, 9)
	tracker.Serial("phase")
	if err := tracker.Track("compileKotlin", func() error { return nil }); err != nil {
		t.Fatalf("track compile: %v", err)
	}
	if err := tracker.Track("runD8", func() error { return nil }); err != nil {
		t.Fatalf("track dex: %v", err)
	}
	tracker.End()

	timings := tracker.GetTimings()
	if timings == nil {
		t.Fatal("expected timings")
	}
	got, err := json.Marshal(timings)
	if err != nil {
		t.Fatalf("marshal timings: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected marshaled timings")
	}
	entries := timings.Entries()
	if got, want := len(entries), 1; got != want {
		t.Fatalf("unexpected top-level entries: got %d want %d", got, want)
	}
	if entries[0].Name != "phase" || entries[0].Children == nil {
		t.Fatalf("expected nested phase entry, got %#v", entries[0])
	}
	children := entries[0].Children.Entries()
	if got, want := len(children), 2; got != want {
		t.Fatalf("unexpected child entry count: got %d want %d", got, want)
	}
	if children[0].Explanation == nil || children[0].Explanation.State != "reused" {
		t.Fatalf("expected reused explanation for first child, got %#v", children[0])
	}
	if children[1].Explanation == nil || children[1].Explanation.State != "rebuilt" {
		t.Fatalf("expected rebuilt explanation for second child, got %#v", children[1])
	}
}

func TestRecorderTrackerExposesCallLog(t *testing.T) {
	t.Parallel()

	tracker := NewRecorder().QueueDurations(0)
	tracker.Parallel("batch")
	_ = tracker.Track("compileTests", func() error { return nil })
	tracker.End()

	calls := tracker.Calls()
	if got, want := len(calls), 3; got != want {
		t.Fatalf("unexpected call count: got %d want %d", got, want)
	}
	if calls[0].Kind != "parallel" || calls[1].Kind != "track" || calls[2].Kind != "end" {
		t.Fatalf("unexpected call log: %#v", calls)
	}
	if calls[1].Explanation == nil || calls[1].Explanation.State != "reused" {
		t.Fatalf("expected probe explanation on tracked call, got %#v", calls[1])
	}
}
