package execbackend

import (
	"fmt"
	"sort"

	"github.com/kaeawc/grit/internal/admission"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
)

// Scheduler is a minimal execution-graph scheduler stub. It tracks DAG
// readiness from an ActionSchedule and owns the admission controller used to
// preview or enforce runtime policy such as bandwidth-aware remote probe
// deferral.
type Scheduler struct {
	steps         map[graph.ActionID]configmodel.ActionScheduleStep
	dependents    map[graph.ActionID][]graph.ActionID
	remainingDeps map[graph.ActionID]int
	stepOrder     map[graph.ActionID]int
	ready         []graph.ActionID
	completed     map[graph.ActionID]bool
	admission     *admission.Controller
}

// NewSchedulerFromSchedule constructs a scheduler stub directly from the
// action schedule. It wires the schedule's resource and network budgets into
// the underlying admission controller.
func NewSchedulerFromSchedule(schedule configmodel.ActionSchedule) *Scheduler {
	return NewScheduler(schedule, admission.NewControllerFromSchedule(schedule))
}

// NewScheduler constructs a scheduler stub over a schedule using the provided
// admission controller. When controller is nil, a fresh controller is created
// from the schedule's resource budgets.
func NewScheduler(schedule configmodel.ActionSchedule, controller *admission.Controller) *Scheduler {
	steps := scheduledSteps(schedule)
	if controller == nil {
		controller = admission.NewController(schedule.ResourceBudgets)
	}
	s := &Scheduler{
		steps:         make(map[graph.ActionID]configmodel.ActionScheduleStep, len(steps)),
		dependents:    make(map[graph.ActionID][]graph.ActionID, len(schedule.Dependents)),
		remainingDeps: make(map[graph.ActionID]int, len(steps)),
		stepOrder:     make(map[graph.ActionID]int, len(steps)),
		completed:     make(map[graph.ActionID]bool, len(steps)),
		admission:     controller,
	}
	for actionID, ids := range schedule.Dependents {
		s.dependents[actionID] = append([]graph.ActionID(nil), ids...)
	}
	for i, step := range steps {
		id := step.Action.ID
		s.steps[id] = step
		s.stepOrder[id] = i
		deps := schedule.Dependencies[id]
		if len(deps) == 0 {
			deps = step.Dependencies
		}
		s.remainingDeps[id] = len(deps)
		for _, dep := range step.Dependencies {
			s.dependents[dep] = appendIfMissingActionID(s.dependents[dep], id)
		}
	}
	for _, step := range steps {
		if s.remainingDeps[step.Action.ID] == 0 {
			s.ready = append(s.ready, step.Action.ID)
		}
	}
	s.sortReady()
	return s
}

// SetNetworkBudget attaches or replaces the scheduler's bandwidth-aware
// admission constraint.
func (s *Scheduler) SetNetworkBudget(nb *admission.NetworkBudget) {
	if s == nil || s.admission == nil {
		return
	}
	s.admission.SetNetworkBudget(nb)
}

// Ready returns the currently ready actions in deterministic scheduler order.
func (s *Scheduler) Ready() []configmodel.ActionScheduleStep {
	if s == nil || len(s.ready) == 0 {
		return nil
	}
	out := make([]configmodel.ActionScheduleStep, 0, len(s.ready))
	for _, id := range s.ready {
		out = append(out, s.steps[id])
	}
	return out
}

// Complete marks an action complete and promotes newly-unblocked dependents
// into the ready set.
func (s *Scheduler) Complete(actionID graph.ActionID) error {
	if s == nil {
		return fmt.Errorf("scheduler is nil")
	}
	if _, ok := s.steps[actionID]; !ok {
		return fmt.Errorf("scheduler: unknown action %s", actionID)
	}
	if s.completed[actionID] {
		return fmt.Errorf("scheduler: action %s already completed", actionID)
	}
	s.completed[actionID] = true
	s.ready = removeActionID(s.ready, actionID)
	for _, dependent := range s.dependents[actionID] {
		if s.completed[dependent] {
			continue
		}
		if s.remainingDeps[dependent] > 0 {
			s.remainingDeps[dependent]--
		}
		if s.remainingDeps[dependent] == 0 {
			s.ready = appendIfMissingActionID(s.ready, dependent)
		}
	}
	s.sortReady()
	return nil
}

// PredictRemoteProbeDeferrals walks the scheduler in readiness order and asks
// the admission controller whether each action's remote probe should be
// deferred. It advances the scheduler to completion; callers should create a
// fresh scheduler when they need an independent prediction pass.
func (s *Scheduler) PredictRemoteProbeDeferrals() map[string]bool {
	if s == nil || s.admission == nil {
		return nil
	}
	decisions := make(map[string]bool, len(s.steps))
	for len(s.ready) > 0 {
		ready := append([]graph.ActionID(nil), s.ready...)
		for _, id := range ready {
			decision := s.admission.AdmitRemoteProbe(s.steps[id])
			decisions[id.String()] = decision.DeferRemote
		}
		for _, id := range ready {
			if err := s.Complete(id); err != nil {
				return decisions
			}
		}
	}
	return decisions
}

func (s *Scheduler) sortReady() {
	sort.Slice(s.ready, func(i, j int) bool {
		left := s.ready[i]
		right := s.ready[j]
		if s.stepOrder[left] != s.stepOrder[right] {
			return s.stepOrder[left] < s.stepOrder[right]
		}
		return left < right
	})
}

func appendIfMissingActionID(ids []graph.ActionID, target graph.ActionID) []graph.ActionID {
	for _, id := range ids {
		if id == target {
			return ids
		}
	}
	return append(ids, target)
}

func removeActionID(ids []graph.ActionID, target graph.ActionID) []graph.ActionID {
	out := ids[:0]
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return out
}

func scheduledSteps(schedule configmodel.ActionSchedule) []configmodel.ActionScheduleStep {
	if len(schedule.Steps) != 0 {
		return schedule.Steps
	}
	if len(schedule.Batches) == 0 {
		return nil
	}
	var steps []configmodel.ActionScheduleStep
	for _, batch := range schedule.Batches {
		steps = append(steps, batch...)
	}
	return steps
}
