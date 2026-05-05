package testsupport

import (
	"context"
	"errors"
	"testing"

	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/integration"
)

func TestHookRecorderCapturesBeforeAndAfter(t *testing.T) {
	hook := &HookRecorder{}

	req := integration.PlanRequest{
		Command:          "assemble",
		ModulePath:       ":app",
		RequestedVariant: "debug",
	}
	if err := hook.BeforePlan(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}

	result := integration.PlanResult{
		Command:       "assemble",
		ModulePath:    ":app",
		TargetVariant: "debug",
	}
	if err := hook.AfterPlan(context.Background(), result, nil); err != nil {
		t.Fatal(err)
	}

	before := hook.BeforeSnapshot()
	after := hook.AfterSnapshot()
	if len(before) != 1 || before[0].Command != "assemble" || before[0].ModulePath != ":app" {
		t.Fatalf("before = %#v", before)
	}
	if len(after) != 1 || after[0].Command != "assemble" || after[0].TargetVariant != "debug" {
		t.Fatalf("after = %#v", after)
	}
}

func TestHookRecorderDelegatesCustomFunctions(t *testing.T) {
	sentinel := errors.New("hook error")
	hook := &HookRecorder{
		BeforeFn: func(_ context.Context, _ integration.PlanRequest, _ integration.ReadOnlyModel) error {
			return sentinel
		},
	}

	err := hook.BeforePlan(context.Background(), integration.PlanRequest{}, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if len(hook.Before) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(hook.Before))
	}
}

func TestHookRecorderAfterSnapshotReturnsCopies(t *testing.T) {
	hook := &HookRecorder{}
	result := integration.PlanResult{
		Command:  "assemble",
		Variants: []string{"debug"},
		Actions: []graph.Action{
			{
				ID:         "action.compile",
				Kind:       graph.ActionKindCompile,
				Inputs:     []graph.ArtifactID{"artifact.source"},
				Outputs:    []graph.ArtifactID{"artifact.classes"},
				Attributes: map[string]string{"owner": "test"},
			},
		},
	}

	if err := hook.AfterPlan(context.Background(), result, nil); err != nil {
		t.Fatal(err)
	}
	result.Variants[0] = "mutated"
	result.Actions[0].Inputs[0] = "artifact.mutated"
	result.Actions[0].Outputs[0] = "artifact.mutated"
	result.Actions[0].Attributes["owner"] = "mutated"

	after := hook.AfterSnapshot()
	after[0].Variants[0] = "mutated-again"
	after[0].Actions[0].Inputs[0] = "artifact.mutated-again"
	after[0].Actions[0].Outputs[0] = "artifact.mutated-again"
	after[0].Actions[0].Attributes["owner"] = "mutated-again"

	fresh := hook.AfterSnapshot()
	if got := fresh[0].Variants[0]; got != "debug" {
		t.Fatalf("Variants[0] = %q", got)
	}
	if got := fresh[0].Actions[0].Inputs[0]; got != "artifact.source" {
		t.Fatalf("Actions[0].Inputs[0] = %q", got)
	}
	if got := fresh[0].Actions[0].Outputs[0]; got != "artifact.classes" {
		t.Fatalf("Actions[0].Outputs[0] = %q", got)
	}
	if got := fresh[0].Actions[0].Attributes["owner"]; got != "test" {
		t.Fatalf("Actions[0].Attributes[owner] = %q", got)
	}
}
