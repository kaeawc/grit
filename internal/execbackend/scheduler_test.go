package execbackend

import (
	"testing"

	"github.com/kaeawc/grit/internal/admission"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
)

func schedulerStep(id string, deps ...graph.ActionID) configmodel.ActionScheduleStep {
	return configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:         graph.ActionID(id),
			Name:       id,
			Attributes: map[string]string{"operation": "compile"},
		},
		Dependencies:   append([]graph.ActionID(nil), deps...),
		WorkerClass:    "kotlin-compile",
		ResourceClass:  "cpu",
		ResourceCost:   1,
		MaxParallelism: 2,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote"},
		ExecuteOnMiss:  true,
		EstimatedBytes: 80,
	}
}

func TestSchedulerReadyAndCompleteUnlocksDependents(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		Steps: []configmodel.ActionScheduleStep{
			schedulerStep("action:first"),
			schedulerStep("action:second", "action:first"),
			schedulerStep("action:third", "action:second"),
		},
		Dependencies: map[graph.ActionID][]graph.ActionID{
			"action:second": {"action:first"},
			"action:third":  {"action:second"},
		},
		Dependents: map[graph.ActionID][]graph.ActionID{
			"action:first":  {"action:second"},
			"action:second": {"action:third"},
		},
	}

	scheduler := NewSchedulerFromSchedule(schedule)
	ready := scheduler.Ready()
	if len(ready) != 1 || ready[0].Action.ID != "action:first" {
		t.Fatalf("expected only first action ready initially, got %#v", ready)
	}
	if err := scheduler.Complete("action:first"); err != nil {
		t.Fatalf("complete first action: %v", err)
	}
	ready = scheduler.Ready()
	if len(ready) != 1 || ready[0].Action.ID != "action:second" {
		t.Fatalf("expected second action ready after completing first, got %#v", ready)
	}
	if err := scheduler.Complete("action:second"); err != nil {
		t.Fatalf("complete second action: %v", err)
	}
	ready = scheduler.Ready()
	if len(ready) != 1 || ready[0].Action.ID != "action:third" {
		t.Fatalf("expected third action ready after completing second, got %#v", ready)
	}
}

func TestSchedulerPredictRemoteProbeDeferralsUsesSchedulerOrder(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 2}},
		Steps: []configmodel.ActionScheduleStep{
			schedulerStep("action:first"),
			schedulerStep("action:second"),
			schedulerStep("action:third", "action:first", "action:second"),
		},
		Dependencies: map[graph.ActionID][]graph.ActionID{
			"action:third": {"action:first", "action:second"},
		},
		Dependents: map[graph.ActionID][]graph.ActionID{
			"action:first":  {"action:third"},
			"action:second": {"action:third"},
		},
	}

	scheduler := NewSchedulerFromSchedule(schedule)
	scheduler.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	}))

	decisions := scheduler.PredictRemoteProbeDeferrals()
	if len(decisions) != 3 {
		t.Fatalf("expected predictions for all actions, got %#v", decisions)
	}
	if decisions["action:first"] {
		t.Fatalf("expected first action to keep remote probe enabled, got %#v", decisions)
	}
	if !decisions["action:second"] {
		t.Fatalf("expected second action to defer remote probe once budget is mostly consumed, got %#v", decisions)
	}
	if !decisions["action:third"] {
		t.Fatalf("expected dependent action to inherit exhausted budget after roots complete, got %#v", decisions)
	}
}

func TestSchedulerSetNetworkBudgetOverridesDefaultControllerBudget(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 1}},
		Steps: []configmodel.ActionScheduleStep{
			schedulerStep("action:first"),
			schedulerStep("action:second"),
		},
	}

	scheduler := NewScheduler(schedule, nil)
	scheduler.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     80,
		RefillBytesPerSec: 0,
	}))

	decisions := scheduler.PredictRemoteProbeDeferrals()
	if decisions["action:first"] {
		t.Fatalf("expected first action to fit exactly in injected budget, got %#v", decisions)
	}
	if !decisions["action:second"] {
		t.Fatalf("expected second action to defer after injected budget is exhausted, got %#v", decisions)
	}
}
