// Package admission implements resource-aware runtime admission control for
// action execution. It is the second level of the two-level scheduler described
// in the execution-graph-and-scheduler roadmap: level one determines what CAN
// run (DAG readiness), and level two — this package — determines what SHOULD
// run given current CPU, memory, IO, and tool contention.
//
// The controller tracks live resource usage across all resource classes and
// makes dynamic admit/release decisions as actions start and complete. Unlike
// the static batch planner in configmodel.ScheduleActions, this operates at
// runtime and can react to cache hits freeing resources early.
package admission

import (
	"fmt"
	"sort"
	"sync"

	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/tieredcas"
)

// Decision describes the outcome of an admission attempt.
type Decision struct {
	Admitted    bool
	ActionID    string
	Reason      string
	WaitClass   string // resource class that blocked admission, if any
	WaitCost    int    // cost that exceeded capacity, if any
	PoolUsage   []PoolSnapshot
	WorkerUsage []WorkerSnapshot

	// DeferRemote is true when the action was admitted but the network budget
	// denied the remote probe. The executor should skip the remote cache tier
	// and resolve locally.
	DeferRemote bool

	// RemoteProbe captures the bandwidth-aware probe decision taken during
	// admission.
	RemoteProbe RemoteProbeDecision
}

// RemoteProbeDecision describes whether a cacheable action should skip remote
// cache probes because the network budget is exhausted.
type RemoteProbeDecision struct {
	ActionID          string
	Eligible          bool
	DeferRemote       bool
	EstimatedBytes    int64
	BudgetBeforeBytes int64
	BudgetAfterBytes  int64
}

// PoolSnapshot captures the state of a single resource pool at a point in time.
type PoolSnapshot struct {
	ResourceClass string
	Capacity      int
	Used          int
	Remaining     int
}

// WorkerSnapshot captures the state of a single worker class at a point in time.
type WorkerSnapshot struct {
	WorkerClass string
	Limit       int
	Active      int
	Remaining   int
}

// Controller tracks live resource pool capacities and worker class limits,
// making dynamic admission decisions for action schedule steps.
type Controller struct {
	mu sync.Mutex

	// Resource pools keyed by resource class.
	pools map[string]*pool

	// Worker class limits keyed by worker class name.
	workers map[string]*workerSlot

	// Actions currently admitted, keyed by action ID.
	admitted map[string]admittedAction

	// networkBudget is an optional bandwidth-aware admission constraint.
	// When non-nil, cacheable actions with remote probe tiers are checked
	// against this budget before admission. Denied actions have their
	// Decision.DeferRemote flag set, signalling the executor to skip the
	// remote probe and resolve locally.
	networkBudget *NetworkBudget
}

type pool struct {
	capacity int
	used     int
}

type workerSlot struct {
	limit  int
	active int
}

type admittedAction struct {
	resourceClass          string
	resourceCost           int
	workerClass            string
	estimatedBytes         int64
	deferRemote            bool
	remoteProbePrecomputed bool
}

// NewController creates a Controller pre-loaded with the given resource budgets.
// Worker class limits are registered lazily on first admission attempt, or can
// be pre-registered with RegisterWorkerClass.
func NewController(budgets []configmodel.ResourceBudget) *Controller {
	c := &Controller{
		pools:    make(map[string]*pool, len(budgets)),
		workers:  make(map[string]*workerSlot),
		admitted: make(map[string]admittedAction),
	}
	for _, b := range budgets {
		c.pools[b.ResourceClass] = &pool{capacity: b.Capacity}
	}
	return c
}

// NewControllerFromSchedule creates a Controller from an ActionSchedule,
// pre-loading resource budgets and attaching a NetworkBudget when the schedule
// includes one. This is the primary entry point for the Build flow: the
// service layer calls it once per build to wire admission control from the
// static schedule configuration.
func NewControllerFromSchedule(schedule configmodel.ActionSchedule) *Controller {
	c := NewController(schedule.ResourceBudgets)
	if schedule.NetworkBudgetConfig != nil {
		nb := NewNetworkBudget(NetworkBudgetConfig{
			CapacityBytes:     schedule.NetworkBudgetConfig.CapacityBytes,
			RefillBytesPerSec: schedule.NetworkBudgetConfig.RefillBytesPerSec,
		})
		c.SetNetworkBudget(nb)
	}
	return c
}

// SetNetworkBudget attaches a bandwidth-aware admission constraint. When set,
// cacheable actions that include remote probe tiers are checked against the
// budget before admission. If the budget denies the request, the action is
// still admitted for local execution but the Decision.DeferRemote flag is set.
func (c *Controller) SetNetworkBudget(nb *NetworkBudget) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.networkBudget = nb
}

// RegisterWorkerClass sets the maximum parallelism for a worker class. If the
// class is already registered, the limit is updated.
func (c *Controller) RegisterWorkerClass(workerClass string, maxParallelism int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workers[workerClass] = &workerSlot{limit: maxParallelism}
}

// TryAdmit attempts to admit a single action step. It returns a Decision
// indicating whether the action was admitted and, if not, which constraint
// blocked it.
//
// On success the action's resource cost and worker slot are reserved until
// Release is called with the same action ID.
func (c *Controller) TryAdmit(step configmodel.ActionScheduleStep) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tryAdmitLocked(step, nil)
}

// TryAdmitWithRemoteProbeDecision reserves worker and resource capacity using
// a remote-probe decision that was already computed by the scheduler. This
// avoids double-consuming network budget when the scheduler admits or defers
// remote probes ahead of execution. The scheduler still owns later byte
// reconciliation for that reservation once execution reports the actual
// remote-cache traffic.
func (c *Controller) TryAdmitWithRemoteProbeDecision(step configmodel.ActionScheduleStep, remoteProbe RemoteProbeDecision) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tryAdmitLocked(step, &remoteProbe)
}

func (c *Controller) tryAdmitLocked(step configmodel.ActionScheduleStep, remoteProbeOverride *RemoteProbeDecision) Decision {
	actionID := step.Action.ID.String()
	if _, ok := c.admitted[actionID]; ok {
		return Decision{
			Admitted: false,
			ActionID: actionID,
			Reason:   "already-admitted",
		}
	}

	// Check resource pool capacity.
	p := c.ensurePool(step.ResourceClass)
	if p.used+step.ResourceCost > p.capacity {
		return Decision{
			Admitted:    false,
			ActionID:    actionID,
			Reason:      "resource-exhausted",
			WaitClass:   step.ResourceClass,
			WaitCost:    step.ResourceCost,
			PoolUsage:   c.snapshotPoolsLocked(),
			WorkerUsage: c.snapshotWorkersLocked(),
		}
	}

	// Check worker class limit.
	w := c.ensureWorker(step.WorkerClass, step.MaxParallelism)
	if w.active >= w.limit {
		return Decision{
			Admitted:    false,
			ActionID:    actionID,
			Reason:      "worker-saturated",
			WaitClass:   step.WorkerClass,
			PoolUsage:   c.snapshotPoolsLocked(),
			WorkerUsage: c.snapshotWorkersLocked(),
		}
	}

	// Admit the action.
	p.used += step.ResourceCost
	w.active++

	// Check network budget for cacheable actions with remote probe tiers.
	remoteProbe := c.remoteProbeDecisionLocked(step, remoteProbeOverride)

	c.admitted[actionID] = admittedAction{
		resourceClass:          step.ResourceClass,
		resourceCost:           step.ResourceCost,
		workerClass:            step.WorkerClass,
		estimatedBytes:         remoteProbe.EstimatedBytes,
		deferRemote:            remoteProbe.DeferRemote,
		remoteProbePrecomputed: remoteProbeOverride != nil,
	}

	return Decision{
		Admitted:    true,
		ActionID:    actionID,
		Reason:      "admitted",
		DeferRemote: remoteProbe.DeferRemote,
		RemoteProbe: remoteProbe,
		PoolUsage:   c.snapshotPoolsLocked(),
		WorkerUsage: c.snapshotWorkersLocked(),
	}
}

// AdmitRemoteProbe consults only the bandwidth-aware admission path for a
// step's remote cache probe. Unlike TryAdmit, it does not reserve worker or
// resource capacity; it only consumes network budget for eligible remote
// probes so the scheduler can decide whether to force local-only resolution.
func (c *Controller) AdmitRemoteProbe(step configmodel.ActionScheduleStep) RemoteProbeDecision {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.admitRemoteProbeLocked(step)
}

// ReconcileRemoteProbe returns unused network-budget capacity from a prior
// remote-probe decision. Callers should pass the scheduler's cached decision
// together with the measured remote bytes once execution completes.
//
// Deferred or ineligible probes are a no-op because they never consumed
// bandwidth budget in the first place.
func (c *Controller) ReconcileRemoteProbe(decision RemoteProbeDecision, actualBytes int64) {
	if actualBytes < 0 || !decision.Eligible || decision.DeferRemote || decision.EstimatedBytes <= 0 {
		return
	}

	c.mu.Lock()
	nb := c.networkBudget
	c.mu.Unlock()
	if nb == nil || actualBytes >= decision.EstimatedBytes {
		return
	}
	nb.Return(decision.EstimatedBytes - actualBytes)
}

// Release frees the resources held by a previously admitted action. It returns
// an error if the action was not currently admitted.
//
// When the action was admitted with DeferRemote=true, the estimated bytes are
// returned to the network budget because the remote tier was never used.
func (c *Controller) Release(actionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.releaseLocked(actionID, -1)
}

// ReleaseWithActual frees resources and reconciles the network budget against
// the actual bytes transferred. If the action used fewer bytes than estimated,
// the surplus is returned to the budget. If actualBytes is zero and the action
// was deferred (DeferRemote=true), the full estimate is returned.
//
// When the action was admitted with a scheduler-precomputed remote-probe
// decision, this only frees worker/resource slots. The scheduler remains
// responsible for reconciling the reserved bytes once it completes the action.
func (c *Controller) ReleaseWithActual(actionID string, actualBytes int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.releaseLocked(actionID, actualBytes)
}

// releaseLocked is the shared implementation for Release and ReleaseWithActual.
// An actualBytes value of -1 signals the basic Release path (auto-return for
// deferred actions only).
func (c *Controller) releaseLocked(actionID string, actualBytes int64) error {
	entry, ok := c.admitted[actionID]
	if !ok {
		return fmt.Errorf("action %s not admitted", actionID)
	}
	delete(c.admitted, actionID)

	if p, ok := c.pools[entry.resourceClass]; ok {
		p.used -= entry.resourceCost
		if p.used < 0 {
			p.used = 0
		}
	}
	if w, ok := c.workers[entry.workerClass]; ok {
		w.active--
		if w.active < 0 {
			w.active = 0
		}
	}

	// Return unused bandwidth to the network budget.
	if c.networkBudget != nil && entry.estimatedBytes > 0 {
		if entry.deferRemote {
			// Action was deferred — no remote bytes consumed at all. The
			// budget already denied the request so no tokens were taken,
			// nothing to return.
		} else if entry.remoteProbePrecomputed {
			// The scheduler pre-reserved bandwidth for this remote probe, so it
			// also owns the later actual-byte reconciliation.
		} else if actualBytes >= 0 && actualBytes < entry.estimatedBytes {
			// Action used fewer bytes than estimated; return the surplus.
			c.networkBudget.Return(entry.estimatedBytes - actualBytes)
		}
	}

	return nil
}

// AdmitBatch takes a set of ready steps (DAG-ready actions) and returns two
// slices: those that were admitted and those that must wait. Steps are
// considered in order, so callers should sort by priority before calling.
func (c *Controller) AdmitBatch(ready []configmodel.ActionScheduleStep) (admitted, waiting []configmodel.ActionScheduleStep) {
	for _, step := range ready {
		decision := c.TryAdmit(step)
		if decision.Admitted {
			admitted = append(admitted, step)
		} else {
			waiting = append(waiting, step)
		}
	}
	return admitted, waiting
}

// BatchEntry pairs an admitted action step with its admission decision, giving
// the caller access to per-action flags like DeferRemote.
type BatchEntry struct {
	Step     configmodel.ActionScheduleStep
	Decision Decision
}

// AdmitBatchWithDecisions takes a set of ready steps and returns admitted
// entries (each paired with their Decision) and the steps that must wait.
// Unlike AdmitBatch, this preserves per-action admission metadata such as
// DeferRemote, which the executor needs to decide whether to skip remote
// cache probes.
func (c *Controller) AdmitBatchWithDecisions(ready []configmodel.ActionScheduleStep) (admitted []BatchEntry, waiting []configmodel.ActionScheduleStep) {
	for _, step := range ready {
		decision := c.TryAdmit(step)
		if decision.Admitted {
			admitted = append(admitted, BatchEntry{Step: step, Decision: decision})
		} else {
			waiting = append(waiting, step)
		}
	}
	return admitted, waiting
}

// Snapshot returns the current state of all resource pools and worker slots.
func (c *Controller) Snapshot() ([]PoolSnapshot, []WorkerSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotPoolsLocked(), c.snapshotWorkersLocked()
}

// FullSnapshot returns the current state of all resource pools, worker slots,
// and the network budget (if attached). This gives callers a single consistent
// view of every admission constraint.
func (c *Controller) FullSnapshot() ControllerSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap := ControllerSnapshot{
		Pools:   c.snapshotPoolsLocked(),
		Workers: c.snapshotWorkersLocked(),
		Active:  len(c.admitted),
	}
	if c.networkBudget != nil {
		nbs := c.networkBudget.Snapshot()
		snap.NetworkBudget = &nbs
	}
	return snap
}

// NetworkBudgetSnapshot returns the current bandwidth budget state, if one is
// attached. Callers use this to detect when previously deferred remote probes
// can be retried after bytes are returned or refilled.
func (c *Controller) NetworkBudgetSnapshot() *NetworkBudgetSnapshot {
	c.mu.Lock()
	nb := c.networkBudget
	c.mu.Unlock()
	if nb == nil {
		return nil
	}
	snap := nb.Snapshot()
	return &snap
}

// CanAdmitRemoteProbeEstimate reports whether the attached network budget can
// currently satisfy the given estimated remote-probe size without consuming any
// bytes. When no budget is attached, remote probing is unconstrained.
func (c *Controller) CanAdmitRemoteProbeEstimate(estimatedBytes int64) bool {
	c.mu.Lock()
	nb := c.networkBudget
	c.mu.Unlock()
	if nb == nil {
		return true
	}
	return nb.CanAdmit(estimatedBytes)
}

// ControllerSnapshot captures the full state of the admission controller at a
// point in time, including resource pools, worker slots, and the network budget.
type ControllerSnapshot struct {
	Pools         []PoolSnapshot         `json:"pools"`
	Workers       []WorkerSnapshot       `json:"workers"`
	Active        int                    `json:"active"`
	NetworkBudget *NetworkBudgetSnapshot `json:"networkBudget,omitempty"`
}

// ActiveCount returns the number of currently admitted actions.
func (c *Controller) ActiveCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.admitted)
}

func (c *Controller) ensurePool(resourceClass string) *pool {
	p, ok := c.pools[resourceClass]
	if !ok {
		p = &pool{capacity: 1}
		c.pools[resourceClass] = p
	}
	return p
}

func (c *Controller) admitRemoteProbeLocked(step configmodel.ActionScheduleStep) RemoteProbeDecision {
	decision := RemoteProbeDecision{ActionID: step.Action.ID.String()}
	if c.networkBudget == nil || !step.Cacheable || !tieredcas.HasRemoteProbeTier(step.ProbeOrder) {
		return decision
	}
	admission := c.networkBudget.AdmitDetailed(step.EstimatedBytes)
	decision.Eligible = true
	decision.EstimatedBytes = step.EstimatedBytes
	decision.BudgetBeforeBytes = admission.AvailableBefore
	decision.BudgetAfterBytes = admission.AvailableAfter
	decision.DeferRemote = !admission.Admitted
	return decision
}

func (c *Controller) remoteProbeDecisionLocked(step configmodel.ActionScheduleStep, override *RemoteProbeDecision) RemoteProbeDecision {
	if override == nil {
		return c.admitRemoteProbeLocked(step)
	}
	decision := *override
	decision.ActionID = step.Action.ID.String()
	if decision.Eligible && decision.EstimatedBytes <= 0 {
		decision.EstimatedBytes = step.EstimatedBytes
	}
	return decision
}

func (c *Controller) ensureWorker(workerClass string, maxParallelism int) *workerSlot {
	w, ok := c.workers[workerClass]
	if !ok {
		limit := maxParallelism
		if limit <= 0 {
			limit = 1
		}
		w = &workerSlot{limit: limit}
		c.workers[workerClass] = w
	}
	return w
}

func (c *Controller) snapshotPoolsLocked() []PoolSnapshot {
	out := make([]PoolSnapshot, 0, len(c.pools))
	for class, p := range c.pools {
		out = append(out, PoolSnapshot{
			ResourceClass: class,
			Capacity:      p.capacity,
			Used:          p.used,
			Remaining:     p.capacity - p.used,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ResourceClass < out[j].ResourceClass })
	return out
}

func (c *Controller) snapshotWorkersLocked() []WorkerSnapshot {
	out := make([]WorkerSnapshot, 0, len(c.workers))
	for class, w := range c.workers {
		out = append(out, WorkerSnapshot{
			WorkerClass: class,
			Limit:       w.limit,
			Active:      w.active,
			Remaining:   w.limit - w.active,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkerClass < out[j].WorkerClass })
	return out
}
