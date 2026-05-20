package perf

import (
	"sync"
	"time"
)

// Span is a single in-progress timing. End it with Stop — typically via
// defer:
//
//	defer perf.NewSpan(tracker, "request").Stop()
//
// or, to attach attributes or metrics during the span:
//
//	span := perf.NewSpan(tracker, "download")
//	defer span.Stop()
//	span.SetAttr("url", u)
//	span.AddMetric("bytes", n)
//
// Spans complement Tracker.Track: Track is for cacheable actions where
// explanation inference matters and the timed body must return an
// error; Span is general-purpose timing with deferred Stop and
// out-of-band attribute/metric attachment.
//
// Spans against a disabled tracker are zero-allocation no-ops.
type Span interface {
	// SetAttr — last write wins; calls after Stop are dropped.
	SetAttr(key, value string)
	// AddMetric — last write wins; calls after Stop are dropped.
	AddMetric(key string, value int64)
	// Stop is safe to call multiple times; subsequent calls are no-ops.
	Stop()
}

// NewSpan returns a Span on tracker. Always pair with Stop, ideally
// via defer. If tracker is nil or disabled the returned Span is a
// no-op — no allocation, no Record call on Stop.
func NewSpan(tracker Tracker, name string) Span {
	if tracker == nil || !tracker.IsEnabled() {
		return noopSpan{}
	}
	return &activeSpan{
		tracker: tracker,
		name:    name,
		start:   time.Now(),
	}
}

type noopSpan struct{}

func (noopSpan) SetAttr(string, string)  {}
func (noopSpan) AddMetric(string, int64) {}
func (noopSpan) Stop()                   {}

type activeSpan struct {
	mu      sync.Mutex
	tracker Tracker
	name    string
	start   time.Time
	attrs   map[string]string
	metrics map[string]int64
	stopped bool
}

func (s *activeSpan) SetAttr(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	if s.attrs == nil {
		s.attrs = make(map[string]string)
	}
	s.attrs[key] = value
}

func (s *activeSpan) AddMetric(key string, value int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	if s.metrics == nil {
		s.metrics = make(map[string]int64)
	}
	s.metrics[key] = value
}

func (s *activeSpan) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	duration := time.Since(s.start).Milliseconds()
	attrs := s.attrs
	metrics := s.metrics
	name := s.name
	tracker := s.tracker
	// Drop our references so post-Stop calls cannot reach the maps
	// the recorded entry now owns.
	s.attrs = nil
	s.metrics = nil
	s.mu.Unlock()

	tracker.Record(TimingEntry{
		Name:       name,
		DurationMs: duration,
		Attributes: attrs,
		Metrics:    metrics,
	})
}
