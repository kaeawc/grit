package testsupport

import (
	"context"
	"sync"

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
	h.After = append(h.After, result)
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
	return append([]integration.PlanResult(nil), h.After...)
}
