package execbackend

import (
	"testing"

	"github.com/kaeawc/grit/internal/admission"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/responsepayload"
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

func TestSchedulerCopiesScheduleStepMetadata(t *testing.T) {
	step := schedulerStep("action:first")
	step.Action.Inputs = []graph.ArtifactID{graph.ArtifactID("artifact:input")}
	step.Action.Outputs = []graph.ArtifactID{graph.ArtifactID("artifact:output")}
	step.ProbeHint = &responsepayload.CacheProbe{ActionID: "action:first", State: "reused"}
	schedule := configmodel.ActionSchedule{
		Steps: []configmodel.ActionScheduleStep{step},
	}

	scheduler := NewSchedulerFromSchedule(schedule)
	schedule.Steps[0].Action.Inputs[0] = graph.ArtifactID("artifact:mutated-input")
	schedule.Steps[0].Action.Outputs[0] = graph.ArtifactID("artifact:mutated-output")
	schedule.Steps[0].Action.Attributes["operation"] = "mutated"
	schedule.Steps[0].Dependencies = append(schedule.Steps[0].Dependencies, graph.ActionID("action:mutated-dependency"))
	schedule.Steps[0].ProbeOrder[0] = "mutated-tier"
	schedule.Steps[0].ProbeHint.State = "mutated"

	ready := scheduler.Ready()
	if len(ready) != 1 {
		t.Fatalf("expected one ready action, got %#v", ready)
	}
	if got, want := ready[0].Action.Inputs[0], graph.ArtifactID("artifact:input"); got != want {
		t.Fatalf("ready action input = %q, want %q", got, want)
	}
	if got, want := ready[0].Action.Outputs[0], graph.ArtifactID("artifact:output"); got != want {
		t.Fatalf("ready action output = %q, want %q", got, want)
	}
	if got, want := ready[0].Action.Attributes["operation"], "compile"; got != want {
		t.Fatalf("ready action operation = %q, want %q", got, want)
	}
	if len(ready[0].Dependencies) != 0 {
		t.Fatalf("ready action dependencies = %#v, want none", ready[0].Dependencies)
	}
	if got, want := ready[0].ProbeOrder[0], "local-overlay"; got != want {
		t.Fatalf("ready action probe order = %q, want %q", got, want)
	}
	if ready[0].ProbeHint == nil || ready[0].ProbeHint.State != "reused" {
		t.Fatalf("ready action probe hint = %#v, want reused", ready[0].ProbeHint)
	}
}

func TestSchedulerReadyAccessorsReturnCopies(t *testing.T) {
	step := schedulerStep("action:first")
	step.Action.Inputs = []graph.ArtifactID{graph.ArtifactID("artifact:input")}
	step.Action.Outputs = []graph.ArtifactID{graph.ArtifactID("artifact:output")}
	step.ProbeHint = &responsepayload.CacheProbe{ActionID: "action:first", State: "reused"}
	scheduler := NewSchedulerFromSchedule(configmodel.ActionSchedule{
		Batches: [][]configmodel.ActionScheduleStep{{step}},
	})

	ready := scheduler.Ready()
	ready[0].Action.Inputs[0] = graph.ArtifactID("artifact:mutated-input")
	ready[0].Action.Outputs[0] = graph.ArtifactID("artifact:mutated-output")
	ready[0].Action.Attributes["operation"] = "mutated"
	ready[0].Dependencies = append(ready[0].Dependencies, graph.ActionID("action:mutated-dependency"))
	ready[0].ProbeOrder[0] = "mutated-tier"
	ready[0].ProbeHint.State = "mutated"

	readyWithDecisions := scheduler.ReadyWithRemoteProbeDecisions()
	readyWithDecisions[0].Step.Action.Inputs[0] = graph.ArtifactID("artifact:decision-mutated-input")
	readyWithDecisions[0].Step.Action.Outputs[0] = graph.ArtifactID("artifact:decision-mutated-output")
	readyWithDecisions[0].Step.Action.Attributes["operation"] = "decision-mutated"
	readyWithDecisions[0].Step.Dependencies = append(readyWithDecisions[0].Step.Dependencies, graph.ActionID("action:decision-mutated-dependency"))
	readyWithDecisions[0].Step.ProbeOrder[0] = "decision-mutated-tier"
	readyWithDecisions[0].Step.ProbeHint.State = "decision-mutated"

	fresh := scheduler.Ready()
	if len(fresh) != 1 {
		t.Fatalf("expected one fresh ready action, got %#v", fresh)
	}
	if got, want := fresh[0].Action.Inputs[0], graph.ArtifactID("artifact:input"); got != want {
		t.Fatalf("fresh ready action input = %q, want %q", got, want)
	}
	if got, want := fresh[0].Action.Outputs[0], graph.ArtifactID("artifact:output"); got != want {
		t.Fatalf("fresh ready action output = %q, want %q", got, want)
	}
	if got, want := fresh[0].Action.Attributes["operation"], "compile"; got != want {
		t.Fatalf("fresh ready action operation = %q, want %q", got, want)
	}
	if len(fresh[0].Dependencies) != 0 {
		t.Fatalf("fresh ready action dependencies = %#v, want none", fresh[0].Dependencies)
	}
	if got, want := fresh[0].ProbeOrder[0], "local-overlay"; got != want {
		t.Fatalf("fresh ready action probe order = %q, want %q", got, want)
	}
	if fresh[0].ProbeHint == nil || fresh[0].ProbeHint.State != "reused" {
		t.Fatalf("fresh ready action probe hint = %#v, want reused", fresh[0].ProbeHint)
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

func TestSchedulerReadyWithRemoteProbeDecisionsCachesBudgetConsumption(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 3}},
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
		CapacityBytes:     240,
		RefillBytesPerSec: 0,
	}))

	firstReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(firstReady) != 2 {
		t.Fatalf("expected two root actions ready, got %#v", firstReady)
	}
	if firstReady[0].RemoteProbeDecision.DeferRemote || firstReady[1].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected both root actions to fit in the initial budget, got %#v", firstReady)
	}

	secondReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(secondReady) != 2 {
		t.Fatalf("expected repeated ready read to return same actions, got %#v", secondReady)
	}
	if secondReady[0].RemoteProbeDecision.DeferRemote || secondReady[1].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected cached ready decisions to avoid double-spending the budget, got %#v", secondReady)
	}

	if err := scheduler.Complete("action:first"); err != nil {
		t.Fatalf("complete first action: %v", err)
	}
	if err := scheduler.Complete("action:second"); err != nil {
		t.Fatalf("complete second action: %v", err)
	}

	ready := scheduler.ReadyWithRemoteProbeDecisions()
	if len(ready) != 1 || ready[0].Step.Action.ID != "action:third" {
		t.Fatalf("expected dependent action to become ready, got %#v", ready)
	}
	if ready[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected remaining budget to cover the dependent probe, got %#v", ready[0])
	}
}

func TestSchedulerReadyWithRemoteProbeDecisionsRetriesDeferredActionsAfterBudgetRecovery(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 2}},
		Steps: []configmodel.ActionScheduleStep{
			schedulerStep("action:first"),
			schedulerStep("action:second"),
		},
	}

	scheduler := NewSchedulerFromSchedule(schedule)
	nb := admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     80,
		RefillBytesPerSec: 0,
	})
	scheduler.SetNetworkBudget(nb)

	firstReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(firstReady) != 2 {
		t.Fatalf("expected two ready actions, got %#v", firstReady)
	}
	if firstReady[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected first action to consume the available budget, got %#v", firstReady[0])
	}
	if !firstReady[0].RemoteProbeDecision.Eligible || firstReady[0].RemoteProbeDecision.BudgetBeforeBytes != 80 || firstReady[0].RemoteProbeDecision.BudgetAfterBytes != 0 {
		t.Fatalf("expected scheduler to surface admitted probe budget details, got %#v", firstReady[0].RemoteProbeDecision)
	}
	if !firstReady[1].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected second action to defer after budget exhaustion, got %#v", firstReady[1])
	}
	if !firstReady[1].RemoteProbeDecision.Eligible || firstReady[1].RemoteProbeDecision.BudgetBeforeBytes != 0 || firstReady[1].RemoteProbeDecision.BudgetAfterBytes != 0 {
		t.Fatalf("expected scheduler to surface denied probe budget details, got %#v", firstReady[1].RemoteProbeDecision)
	}
	if snap := nb.Snapshot(); snap.TotalDenied != 80 {
		t.Fatalf("expected one denied remote probe attempt, got %+v", snap)
	}

	nb.Return(40)

	partiallyRecoveredReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(partiallyRecoveredReady) != 2 {
		t.Fatalf("expected ready actions after partial recovery, got %#v", partiallyRecoveredReady)
	}
	if !partiallyRecoveredReady[1].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected deferred decision to remain cached until enough bytes return, got %#v", partiallyRecoveredReady[1])
	}
	if snap := nb.Snapshot(); snap.Available != 40 || snap.TotalDenied != 80 {
		t.Fatalf("expected partial recovery to avoid re-denying the probe, got %+v", snap)
	}

	secondReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(secondReady) != 2 {
		t.Fatalf("expected repeated ready read to return same actions, got %#v", secondReady)
	}
	if !secondReady[1].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected deferred decision to stay cached before recovery, got %#v", secondReady[1])
	}
	if snap := nb.Snapshot(); snap.TotalDenied != 80 {
		t.Fatalf("expected cached deferred decision to avoid double-denial, got %+v", snap)
	}

	nb.Return(40)

	recoveredReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(recoveredReady) != 2 {
		t.Fatalf("expected ready actions after budget recovery, got %#v", recoveredReady)
	}
	if recoveredReady[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected first action decision to remain cached after recovery, got %#v", recoveredReady[0])
	}
	if recoveredReady[1].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected second action to retry remote probe after recovery, got %#v", recoveredReady[1])
	}
	if snap := nb.Snapshot(); snap.TotalAdmitted != 160 || snap.TotalDenied != 80 {
		t.Fatalf("expected recovered action to consume the returned budget once, got %+v", snap)
	}
}

func TestSchedulerCompleteWithActualRemoteBytesRestoresBudgetBeforeUnlockingDependents(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 1}},
		Steps: []configmodel.ActionScheduleStep{
			schedulerStep("action:first"),
			schedulerStep("action:second", "action:first"),
		},
		Dependencies: map[graph.ActionID][]graph.ActionID{
			"action:second": {"action:first"},
		},
		Dependents: map[graph.ActionID][]graph.ActionID{
			"action:first": {"action:second"},
		},
	}

	scheduler := NewSchedulerFromSchedule(schedule)
	nb := admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     80,
		RefillBytesPerSec: 0,
	})
	scheduler.SetNetworkBudget(nb)

	firstReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(firstReady) != 1 || firstReady[0].Step.Action.ID != "action:first" {
		t.Fatalf("expected only first action ready, got %#v", firstReady)
	}
	if firstReady[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected first action to consume initial budget, got %#v", firstReady[0])
	}
	if snap := nb.Snapshot(); snap.Available != 0 {
		t.Fatalf("expected first action to consume full budget, got %+v", snap)
	}

	if err := scheduler.CompleteWithActualRemoteBytes("action:first", 0); err != nil {
		t.Fatalf("complete with actual bytes: %v", err)
	}

	secondReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(secondReady) != 1 || secondReady[0].Step.Action.ID != "action:second" {
		t.Fatalf("expected dependent action ready after completion, got %#v", secondReady)
	}
	if secondReady[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected returned budget to admit dependent remote probe, got %#v", secondReady[0])
	}
	if secondReady[0].RemoteProbeDecision.BudgetBeforeBytes != 80 || secondReady[0].RemoteProbeDecision.BudgetAfterBytes != 0 {
		t.Fatalf("expected dependent action to observe restored budget, got %#v", secondReady[0].RemoteProbeDecision)
	}
}

func TestSchedulerCompleteWithActualRemoteBytesChargesOverrunBeforeUnlockingDependents(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 1}},
		Steps: []configmodel.ActionScheduleStep{
			schedulerStep("action:first"),
			schedulerStep("action:second", "action:first"),
		},
		Dependencies: map[graph.ActionID][]graph.ActionID{
			"action:second": {"action:first"},
		},
		Dependents: map[graph.ActionID][]graph.ActionID{
			"action:first": {"action:second"},
		},
	}

	scheduler := NewSchedulerFromSchedule(schedule)
	nb := admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	})
	scheduler.SetNetworkBudget(nb)

	firstReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(firstReady) != 1 || firstReady[0].Step.Action.ID != "action:first" {
		t.Fatalf("expected only first action ready, got %#v", firstReady)
	}
	if firstReady[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected first action to consume initial budget, got %#v", firstReady[0])
	}

	if err := scheduler.CompleteWithActualRemoteBytes("action:first", 95); err != nil {
		t.Fatalf("complete with actual bytes: %v", err)
	}

	secondReady := scheduler.ReadyWithRemoteProbeDecisions()
	if len(secondReady) != 1 || secondReady[0].Step.Action.ID != "action:second" {
		t.Fatalf("expected dependent action ready after completion, got %#v", secondReady)
	}
	if !secondReady[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected overrun to force dependent action into local-only mode, got %#v", secondReady[0])
	}
	if secondReady[0].RemoteProbeDecision.BudgetBeforeBytes != 5 || secondReady[0].RemoteProbeDecision.BudgetAfterBytes != 5 {
		t.Fatalf("expected dependent action to observe only 5 bytes remaining, got %#v", secondReady[0].RemoteProbeDecision)
	}
}

func TestSchedulerClonesControllerNetworkBudget(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 2}},
		Steps: []configmodel.ActionScheduleStep{
			schedulerStep("action:first"),
			schedulerStep("action:second"),
		},
	}

	controller := admission.NewController(schedule.ResourceBudgets)
	controller.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     80,
		RefillBytesPerSec: 0,
	}))

	scheduler := NewScheduler(schedule, controller)
	ready := scheduler.ReadyWithRemoteProbeDecisions()
	if len(ready) != 2 {
		t.Fatalf("expected two ready actions, got %#v", ready)
	}
	if ready[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected first action to consume scheduler-owned budget, got %#v", ready[0])
	}
	if !ready[1].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected second action to defer after scheduler-owned budget is exhausted, got %#v", ready[1])
	}
	if snap := controller.NetworkBudgetSnapshot(); snap == nil || snap.Available != 80 || snap.TotalAdmitted != 0 || snap.TotalDenied != 0 {
		t.Fatalf("expected controller budget to remain untouched during scheduler prediction, got %+v", snap)
	}
	if snap := scheduler.NetworkBudgetSnapshot(); snap == nil || snap.Available != 0 || snap.TotalAdmitted != 80 || snap.TotalDenied != 80 {
		t.Fatalf("expected scheduler to track its own budget accounting, got %+v", snap)
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

func TestSchedulerNilControllerUsesScheduleNetworkBudgetConfig(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 2}},
		NetworkBudgetConfig: &configmodel.ScheduleNetworkBudget{
			CapacityBytes:     80,
			RefillBytesPerSec: 0,
		},
		Steps: []configmodel.ActionScheduleStep{
			schedulerStep("action:first"),
			schedulerStep("action:second"),
		},
	}

	scheduler := NewScheduler(schedule, nil)
	ready := scheduler.ReadyWithRemoteProbeDecisions()
	if len(ready) != 2 {
		t.Fatalf("expected two ready actions, got %#v", ready)
	}
	if ready[0].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected first action to consume schedule-provided budget, got %#v", ready[0])
	}
	if !ready[1].RemoteProbeDecision.DeferRemote {
		t.Fatalf("expected second action to defer after schedule-provided budget is exhausted, got %#v", ready[1])
	}
}
