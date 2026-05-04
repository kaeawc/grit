package testsupport

import (
	"testing"
)

func TestTimingProbeExposesRecorder(t *testing.T) {
	t.Parallel()

	probe := NewTimingProbe(0, 11)
	tracker := probe.Tracker()
	if tracker == nil || !tracker.IsEnabled() {
		t.Fatal("expected enabled tracker")
	}
	if err := tracker.Track("compileKotlin", func() error { return nil }); err != nil {
		t.Fatalf("track compile: %v", err)
	}
	if err := tracker.Track("runD8", func() error { return nil }); err != nil {
		t.Fatalf("track dex: %v", err)
	}
	entries := probe.Entries()
	if got, want := len(entries), 2; got != want {
		t.Fatalf("unexpected entries: got %d want %d", got, want)
	}
	if entries[0].Explanation == nil || entries[0].Explanation.State != "reused" {
		t.Fatalf("expected reused cache probe on first entry, got %#v", entries[0])
	}
	if entries[1].Explanation == nil || entries[1].Explanation.State != "rebuilt" {
		t.Fatalf("expected rebuilt cache probe on second entry, got %#v", entries[1])
	}
}
