package testsupport

import (
	"context"
	"sync"

	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/integration"
)

// HookRecorder implements integration.Hook and records every
// BeforePlan / AfterPlan invocation for later inspection.
type HookRecorder struct {
	mu       sync.Mutex
	Before   []integration.PlanRequest
	After    []integration.PlanResult
	BeforeFn func(context.Context, integration.PlanRequest, integration.ReadOnlyModel) error
	AfterFn  func(context.Context, integration.PlanResult, integration.ReadOnlyModel) error
}

func (h *HookRecorder) BeforePlan(ctx context.Context, req integration.PlanRequest, model integration.ReadOnlyModel) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Before = append(h.Before, req)
	if h.BeforeFn != nil {
		return h.BeforeFn(ctx, req, model)
	}
	return nil
}

func (h *HookRecorder) AfterPlan(ctx context.Context, result integration.PlanResult, model integration.ReadOnlyModel) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.After = append(h.After, clonePlanResult(result))
	if h.AfterFn != nil {
		return h.AfterFn(ctx, result, model)
	}
	return nil
}

// BeforeSnapshot returns a copy of recorded BeforePlan requests.
func (h *HookRecorder) BeforeSnapshot() []integration.PlanRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]integration.PlanRequest(nil), h.Before...)
}

// AfterSnapshot returns a copy of recorded AfterPlan results.
func (h *HookRecorder) AfterSnapshot() []integration.PlanResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	results := make([]integration.PlanResult, len(h.After))
	for i, result := range h.After {
		results[i] = clonePlanResult(result)
	}
	return results
}

func clonePlanResult(result integration.PlanResult) integration.PlanResult {
	result.Variants = append([]string(nil), result.Variants...)
	result.Actions = append([]graph.Action(nil), result.Actions...)
	for i, action := range result.Actions {
		result.Actions[i] = cloneAction(action)
	}
	return result
}

func cloneAction(action graph.Action) graph.Action {
	action.Inputs = append([]graph.ArtifactID(nil), action.Inputs...)
	action.Outputs = append([]graph.ArtifactID(nil), action.Outputs...)
	if action.Attributes != nil {
		attributes := make(map[string]string, len(action.Attributes))
		for key, value := range action.Attributes {
			attributes[key] = value
		}
		action.Attributes = attributes
	}
	return action
}
