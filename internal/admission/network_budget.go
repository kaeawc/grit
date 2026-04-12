package admission

import (
	"sync"
	"time"
)

// NetworkBudget implements a token-bucket bandwidth limiter for remote cache
// probes. Each token represents one byte of network capacity. Tokens refill at
// a steady rate up to the bucket capacity, and callers consume tokens before
// issuing remote operations. When the bucket is empty, remote probes should be
// deferred in favour of local-only resolution.
type NetworkBudget struct {
	mu sync.Mutex

	// capacity is the maximum number of byte-tokens the bucket can hold.
	capacity int64

	// available is the current number of byte-tokens. It may go negative when
	// execution consumes more bytes than the scheduler reserved up front; that
	// negative balance represents bandwidth debt that future refills must repay
	// before new remote probes can be admitted.
	available int64

	// refillRate is the number of byte-tokens added per second.
	refillRate int64

	// lastRefill is the last time tokens were added.
	lastRefill time.Time

	// now is a hook for testing; defaults to time.Now.
	now func() time.Time

	// stats
	totalAdmitted int64
	totalDenied   int64
}

// NetworkBudgetAdmission captures the result of a single token-bucket
// admission attempt.
type NetworkBudgetAdmission struct {
	RequestedBytes  int64
	Admitted        bool
	AvailableBefore int64
	AvailableAfter  int64
}

// NetworkBudgetConfig holds the parameters for creating a NetworkBudget.
type NetworkBudgetConfig struct {
	// CapacityBytes is the maximum burst size in bytes.
	CapacityBytes int64

	// RefillBytesPerSec is the steady-state bandwidth allowance.
	RefillBytesPerSec int64
}

// NewNetworkBudget creates a NetworkBudget that starts full.
func NewNetworkBudget(cfg NetworkBudgetConfig) *NetworkBudget {
	cap := cfg.CapacityBytes
	if cap <= 0 {
		cap = 1
	}
	rate := cfg.RefillBytesPerSec
	if rate < 0 {
		rate = 0
	}
	return &NetworkBudget{
		capacity:   cap,
		available:  cap,
		refillRate: rate,
		lastRefill: time.Now(),
		now:        time.Now,
	}
}

// Admit checks whether estimatedBytes can be consumed from the budget. If so
// it deducts the tokens and returns true. Otherwise it returns false without
// modifying the budget — the caller should fall back to local-only resolution.
func (nb *NetworkBudget) Admit(estimatedBytes int64) bool {
	return nb.AdmitDetailed(estimatedBytes).Admitted
}

// CanAdmit reports whether estimatedBytes would fit in the budget after any
// time-based refill, without consuming tokens or updating admission stats.
func (nb *NetworkBudget) CanAdmit(estimatedBytes int64) bool {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	nb.refillLocked()
	if estimatedBytes <= 0 {
		return true
	}
	return nb.available >= estimatedBytes
}

// AdmitDetailed performs the same token-bucket check as Admit while also
// returning the budget state before and after the attempt.
func (nb *NetworkBudget) AdmitDetailed(estimatedBytes int64) NetworkBudgetAdmission {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	nb.refillLocked()

	admission := NetworkBudgetAdmission{
		RequestedBytes:  estimatedBytes,
		AvailableBefore: nb.available,
		AvailableAfter:  nb.available,
	}
	if estimatedBytes <= 0 {
		admission.Admitted = true
		return admission
	}
	if nb.available >= estimatedBytes {
		nb.available -= estimatedBytes
		nb.totalAdmitted += estimatedBytes
		admission.Admitted = true
		admission.AvailableAfter = nb.available
		return admission
	}
	nb.totalDenied += estimatedBytes
	return admission
}

// Return gives back unused byte-tokens (e.g. when a cache probe turns out
// smaller than estimated). Tokens are capped at capacity.
func (nb *NetworkBudget) Return(bytes int64) {
	if bytes <= 0 {
		return
	}
	nb.mu.Lock()
	defer nb.mu.Unlock()
	nb.available += bytes
	if nb.available > nb.capacity {
		nb.available = nb.capacity
	}
}

// Reconcile adjusts the budget after execution reports actual network usage for
// a previously reserved probe. Surplus reserved bytes are returned to the
// bucket; overages consume additional tokens and may drive the budget negative
// so future probes defer until refill catches up.
func (nb *NetworkBudget) Reconcile(reservedBytes, actualBytes int64) {
	if nb == nil || reservedBytes <= 0 || actualBytes < 0 {
		return
	}

	nb.mu.Lock()
	defer nb.mu.Unlock()
	nb.refillLocked()

	delta := actualBytes - reservedBytes
	switch {
	case delta < 0:
		nb.available += -delta
		if nb.available > nb.capacity {
			nb.available = nb.capacity
		}
	case delta > 0:
		nb.available -= delta
		nb.totalAdmitted += delta
	}
}

// NetworkBudgetSnapshot captures the state of the budget at a point in time.
type NetworkBudgetSnapshot struct {
	Capacity      int64
	Available     int64
	RefillRate    int64
	TotalAdmitted int64
	TotalDenied   int64
}

// Snapshot returns the current state of the budget.
func (nb *NetworkBudget) Snapshot() NetworkBudgetSnapshot {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	nb.refillLocked()
	return NetworkBudgetSnapshot{
		Capacity:      nb.capacity,
		Available:     nb.available,
		RefillRate:    nb.refillRate,
		TotalAdmitted: nb.totalAdmitted,
		TotalDenied:   nb.totalDenied,
	}
}

// Clone returns a copy of the budget with the same token state, refill clock,
// testing hook, and aggregate stats.
func (nb *NetworkBudget) Clone() *NetworkBudget {
	if nb == nil {
		return nil
	}
	nb.mu.Lock()
	defer nb.mu.Unlock()
	nb.refillLocked()
	return &NetworkBudget{
		capacity:      nb.capacity,
		available:     nb.available,
		refillRate:    nb.refillRate,
		lastRefill:    nb.lastRefill,
		now:           nb.now,
		totalAdmitted: nb.totalAdmitted,
		totalDenied:   nb.totalDenied,
	}
}

// refillLocked adds tokens based on elapsed time. Must be called with mu held.
func (nb *NetworkBudget) refillLocked() {
	now := nb.now()
	elapsed := now.Sub(nb.lastRefill)
	if elapsed <= 0 {
		return
	}
	nb.lastRefill = now

	refill := int64(elapsed.Seconds() * float64(nb.refillRate))
	if refill <= 0 {
		return
	}
	nb.available += refill
	if nb.available > nb.capacity {
		nb.available = nb.capacity
	}
}
