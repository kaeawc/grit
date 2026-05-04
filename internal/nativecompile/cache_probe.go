package nativecompile

import (
	"strings"

	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/perf"
)

func recordCacheProbe(tracker perf.Tracker, stepName string, reused bool, basis string, detail string) {
	if tracker == nil || !tracker.IsEnabled() {
		return
	}
	state := explain.StateRebuilt
	if reused {
		state = explain.StateReused
	}
	name := "cacheProbe"
	if stepName = strings.TrimSpace(stepName); stepName != "" {
		name += ":" + stepName
	}
	tracker.Record(perf.TimingEntry{
		Name:       name,
		DurationMs: 0,
		Explanation: &explain.Timing{
			State:  state,
			Basis:  basis,
			Detail: detail,
		},
	})
}
