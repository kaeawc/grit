package admission

// BandwidthSummary aggregates bandwidth-aware admission statistics for a
// completed build. It combines per-action deferral counts (supplied by the
// caller from ActionExecution records) with the network budget's token-bucket
// state at the end of the build.
type BandwidthSummary struct {
	// DeferredActions is the number of actions whose remote cache probes were
	// skipped because the network budget was exhausted.
	DeferredActions int `json:"deferredActions,omitempty"`

	// TotalCacheableActions is the number of cacheable actions that were
	// candidates for remote probing (regardless of whether they were deferred).
	TotalCacheableActions int `json:"totalCacheableActions,omitempty"`

	// EstimatedBytesSaved is the sum of EstimatedBytes across deferred actions.
	// This approximates the network traffic avoided by falling back to local
	// resolution.
	EstimatedBytesSaved int64 `json:"estimatedBytesSaved,omitempty"`

	// BudgetCapacityBytes is the token-bucket capacity, copied from the
	// network budget configuration.
	BudgetCapacityBytes int64 `json:"budgetCapacityBytes,omitempty"`

	// BudgetRemainingBytes is the number of byte-tokens still available when
	// the summary was captured.
	BudgetRemainingBytes int64 `json:"budgetRemainingBytes,omitempty"`

	// BudgetRefillRate is the steady-state refill rate in bytes per second.
	BudgetRefillRate int64 `json:"budgetRefillRate,omitempty"`

	// TotalAdmittedBytes is the cumulative bytes admitted by the budget over
	// the build lifetime.
	TotalAdmittedBytes int64 `json:"totalAdmittedBytes,omitempty"`

	// TotalDeniedBytes is the cumulative bytes denied by the budget over the
	// build lifetime.
	TotalDeniedBytes int64 `json:"totalDeniedBytes,omitempty"`
}

// BandwidthSummary returns a BandwidthSummary from the controller's network
// budget. Returns nil if no network budget is attached.
func (c *Controller) BandwidthSummary() *BandwidthSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.networkBudget == nil {
		return nil
	}
	snap := c.networkBudget.Snapshot()
	return &BandwidthSummary{
		BudgetCapacityBytes:  snap.Capacity,
		BudgetRemainingBytes: snap.Available,
		BudgetRefillRate:     snap.RefillRate,
		TotalAdmittedBytes:   snap.TotalAdmitted,
		TotalDeniedBytes:     snap.TotalDenied,
	}
}
