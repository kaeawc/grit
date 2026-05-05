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
		r.Calls = append(r.Calls, cloneDependencies(*deps))
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
	calls := make([]modulebuild.Dependencies, len(r.Calls))
	for i, call := range r.Calls {
		calls[i] = cloneDependencies(call)
	}
	return calls
}

func cloneDependencies(deps modulebuild.Dependencies) modulebuild.Dependencies {
	deps.Main = append([]modulebuild.Ref(nil), deps.Main...)
	deps.Debug = append([]modulebuild.Ref(nil), deps.Debug...)
	deps.Test = append([]modulebuild.Ref(nil), deps.Test...)
	deps.AndroidTest = append([]modulebuild.Ref(nil), deps.AndroidTest...)
	deps.CompileOnly = append([]modulebuild.Ref(nil), deps.CompileOnly...)
	deps.RuntimeOnly = append([]modulebuild.Ref(nil), deps.RuntimeOnly...)
	deps.TestCompileOnly = append([]modulebuild.Ref(nil), deps.TestCompileOnly...)
	deps.TestRuntimeOnly = append([]modulebuild.Ref(nil), deps.TestRuntimeOnly...)
	deps.AndroidTestCompileOnly = append([]modulebuild.Ref(nil), deps.AndroidTestCompileOnly...)
	deps.AndroidTestRuntimeOnly = append([]modulebuild.Ref(nil), deps.AndroidTestRuntimeOnly...)
	deps.CoreLibraryDesugaring = append([]modulebuild.Ref(nil), deps.CoreLibraryDesugaring...)
	if deps.Scoped != nil {
		scoped := make(map[string][]modulebuild.Ref, len(deps.Scoped))
		for scope, refs := range deps.Scoped {
			scoped[scope] = append([]modulebuild.Ref(nil), refs...)
		}
		deps.Scoped = scoped
	}
	return deps
}
