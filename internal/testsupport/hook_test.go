package testsupport

import (
	"context"
	"errors"
	"testing"

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
