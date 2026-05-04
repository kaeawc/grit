package tieredcas

import "context"

type ctxKey struct{}

// maxTierFromContext returns the MaxTier value carried on the context, or 0
// (meaning "no limit") if none was set. This allows callers that operate
// through the generic cas.Store interface — where ProbeOptions cannot be
// threaded — to still benefit from bandwidth-aware tier limiting.
func maxTierFromContext(ctx context.Context) int {
	v, _ := ctx.Value(ctxKey{}).(int)
	return v
}

// WithMaxTier returns a derived context that constrains tieredcas probe
// operations to tiers with index < maxTier. For example, WithMaxTier(ctx, 1)
// limits probes to the primary (local) tier only, which is the expected
// behaviour when the admission controller sets DeferRemote.
//
// A maxTier of 0 or negative means no limit — all tiers are probed.
func WithMaxTier(ctx context.Context, maxTier int) context.Context {
	if maxTier <= 0 {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, maxTier)
}

// LocalTierCount is the number of tiers considered local (worktree overlay
// + shared-local CAS). Remote tiers have indices >= LocalTierCount.
// This constant matches the tier architecture documented in the package
// header: tier 0 = worktree overlay, tier 1 = shared-local, tier 2+ = remote.
const LocalTierCount = 2

// WithLocalOnly returns a derived context that constrains tieredcas probe
// operations to local tiers only (indices 0 and 1), skipping any remote
// tiers. This is the convenience wrapper callers should use when the
// admission controller's DeferRemote flag is set.
func WithLocalOnly(ctx context.Context) context.Context {
	return WithMaxTier(ctx, LocalTierCount)
}
