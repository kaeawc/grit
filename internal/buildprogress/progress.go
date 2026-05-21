// Package buildprogress emits human-readable phase markers and
// per-item progress updates to stderr during long-running grit
// operations. The package is a no-op unless GRIT_PROGRESS is set to a
// truthy value at process start, so production output is unchanged
// and callers can drop calls into hot paths without overhead.
//
// Markers are designed to be greppable from terminal output:
//
//	[12.3s] phase: materialize-pins  count=862
//	[14.7s]   item materialize-pins 1/862 androidx.core:core:1.13.1
//	[14.8s]   item materialize-pins 2/862 androidx.activity:activity:1.10.1
//	...
//	[5m12s] phase done: materialize-pins  elapsed=4m58s items=862
//
// Item updates are rate-limited (default: one per 200ms, or every
// 50 items, or the final item) so a fast phase doesn't drown the
// stderr stream. The default reporter is process-wide; tests can
// construct their own with NewReporter.
package buildprogress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	itemEmitInterval = 200 * time.Millisecond
	itemEmitStride   = 50
)

// Reporter writes progress events to a sink. The zero value is a
// disabled reporter that no-ops on every method.
type Reporter struct {
	enabled bool
	sink    io.Writer
	start   time.Time
	mu      sync.Mutex
	phases  map[string]*phaseState
}

type phaseState struct {
	startedAt time.Time
	total     int
	count     int64
	lastEmit  time.Time
}

// NewReporter returns a Reporter writing to sink. enabled gates
// every method; a disabled reporter is functionally identical to a
// nil receiver.
func NewReporter(sink io.Writer, enabled bool) *Reporter {
	return &Reporter{
		enabled: enabled,
		sink:    sink,
		start:   time.Now(),
		phases:  map[string]*phaseState{},
	}
}

var defaultReporter = NewReporter(os.Stderr, envEnabled())

// Default returns the process-wide reporter. Honor GRIT_PROGRESS=1
// (or true/yes/on) to enable; any other value leaves it disabled.
func Default() *Reporter { return defaultReporter }

func envEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GRIT_PROGRESS"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Phase emits a phase-start marker. Optional total is the expected
// item count for the phase; zero means "unknown".
func (r *Reporter) Phase(name string, total int) {
	if r == nil || !r.enabled {
		return
	}
	r.mu.Lock()
	r.phases[name] = &phaseState{startedAt: time.Now(), total: total}
	r.emitLocked("phase: %s  count=%d", name, total)
	r.mu.Unlock()
}

// Item emits a per-item update for the named phase. label is a short
// description (e.g. coordinate string). Updates are rate-limited so
// emitting them in a tight loop is cheap.
func (r *Reporter) Item(phase, label string) {
	if r == nil || !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.phases[phase]
	if !ok {
		state = &phaseState{startedAt: time.Now()}
		r.phases[phase] = state
	}
	state.count++
	now := time.Now()
	final := state.total > 0 && state.count == int64(state.total)
	if !final && now.Sub(state.lastEmit) < itemEmitInterval && state.count%itemEmitStride != 0 {
		return
	}
	state.lastEmit = now
	if state.total > 0 {
		r.emitLocked("  item %s %d/%d %s", phase, state.count, state.total, label)
	} else {
		r.emitLocked("  item %s %d %s", phase, state.count, label)
	}
}

// PhaseDone emits a phase-completion marker. If the phase was never
// started via Phase, the marker is still emitted with the items count
// observed via Item.
func (r *Reporter) PhaseDone(name string) {
	if r == nil || !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.phases[name]
	if !ok {
		state = &phaseState{startedAt: r.start}
	}
	delete(r.phases, name)
	elapsed := time.Since(state.startedAt).Truncate(time.Millisecond)
	r.emitLocked("phase done: %s  elapsed=%s items=%d", name, elapsed, state.count)
}

// Enabled reports whether the reporter is actively emitting. Callers
// that want to guard expensive label construction can check this.
func (r *Reporter) Enabled() bool {
	return r != nil && r.enabled
}

func (r *Reporter) emitLocked(format string, args ...any) {
	elapsed := time.Since(r.start).Truncate(time.Millisecond)
	_, _ = fmt.Fprintf(r.sink, "[%s] "+format+"\n", append([]any{elapsed}, args...)...)
}
