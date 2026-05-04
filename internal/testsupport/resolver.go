package testsupport

import (
	"sync"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/perf"
)

// WiringResolverRecorder implements the dependencywiring.DependencyResolver
// interface for use in integration-level tests that need a controllable
// resolver without real filesystem or network access.
type WiringResolverRecorder struct {
	mu       sync.Mutex
	Calls    []modulebuild.Dependencies
	Result   *m2local.Resolved
	Err      error
	topology m2local.CacheTopology
	tracker  perf.Tracker
}

// NewWiringResolverRecorder returns a recorder that returns an empty
// Resolved by default.  Set Result and/or Err to control behaviour.
func NewWiringResolverRecorder() *WiringResolverRecorder {
	return &WiringResolverRecorder{
		Result: &m2local.Resolved{},
	}
}

func (r *WiringResolverRecorder) Resolve(deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if deps != nil {
		r.Calls = append(r.Calls, *deps)
	}
	return r.Result, r.Err
}

func (r *WiringResolverRecorder) SetTracker(t perf.Tracker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tracker = t
}

func (r *WiringResolverRecorder) Topology() m2local.CacheTopology {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.topology
}

// SetTopology configures the topology returned by Topology().
func (r *WiringResolverRecorder) SetTopology(t m2local.CacheTopology) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topology = t
}

// CallsSnapshot returns a copy of recorded Resolve calls.
func (r *WiringResolverRecorder) CallsSnapshot() []modulebuild.Dependencies {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]modulebuild.Dependencies(nil), r.Calls...)
}
