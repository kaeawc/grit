package testsupport

import "github.com/kaeawc/grit/internal/perf"

// TimingProbe is a small test harness around the scripted perf tracker.
// It makes cache and scheduling tests easier to write because they can
// force deterministic durations and inspect the recorded timing tree.
type TimingProbe struct {
	tracker *perf.RecorderTracker
}

func NewTimingProbe(durations ...int64) *TimingProbe {
	return &TimingProbe{
		tracker: perf.NewRecorder().QueueDurations(durations...),
	}
}

func (p *TimingProbe) Tracker() perf.Tracker {
	if p == nil || p.tracker == nil {
		return perf.New(false)
	}
	return p.tracker
}

func (p *TimingProbe) Entries() []perf.TimingEntry {
	if p == nil || p.tracker == nil {
		return nil
	}
	return p.tracker.Entries()
}

func (p *TimingProbe) Calls() []perf.RecordedCall {
	if p == nil || p.tracker == nil {
		return nil
	}
	return p.tracker.Calls()
}
