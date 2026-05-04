package cachepolicy

// PersistenceTier describes how aggressively an action's outputs should
// be persisted after execution.  The tiers form a promotion ladder:
// outputs start at the lowest justified tier and may be promoted later.
// Integer values preserve ordering so tiers can be compared with < / >.
type PersistenceTier int

const (
	// PersistenceNone means the action's outputs should not be persisted.
	PersistenceNone PersistenceTier = iota

	// PersistenceLocal means outputs are worth caching in the worktree
	// overlay but not promoted to shared storage.
	PersistenceLocal

	// PersistenceShared means outputs should be promoted to machine-level
	// shared storage so other worktrees can reuse them.
	PersistenceShared

	// PersistenceRemote means outputs are expensive enough that they
	// should be uploaded to a remote cache for cross-machine reuse.
	PersistenceRemote
)

var persistenceTierNames = map[PersistenceTier]string{
	PersistenceNone:   "none",
	PersistenceLocal:  "local",
	PersistenceShared: "shared",
	PersistenceRemote: "remote",
}

func (t PersistenceTier) String() string {
	if name, ok := persistenceTierNames[t]; ok {
		return name
	}
	return "unknown"
}

// CostSignals captures the execution-time cost observations that inform
// persistence tier decisions.  All fields are optional; zero values mean
// "not measured."
type CostSignals struct {
	// WallTimeMs is the wall-clock duration of the action in milliseconds.
	WallTimeMs int64 `json:"wallTimeMs,omitempty"`

	// CPUTimeMs is the CPU time consumed, if measurable.
	CPUTimeMs int64 `json:"cpuTimeMs,omitempty"`

	// OutputBytes is the total size of the action's output artifacts.
	OutputBytes int64 `json:"outputBytes,omitempty"`

	// InputBytes is the total size of the action's input artifacts,
	// useful for estimating materialization cost on cache miss.
	InputBytes int64 `json:"inputBytes,omitempty"`
}

// PersistenceThresholds controls the tier boundaries for cost-aware
// persistence.  Each threshold is the minimum cost signal value at which
// persistence becomes worthwhile at that tier.  Zero means "always
// persist at this tier" — set a threshold to disable a tier for cheap
// actions.
type PersistenceThresholds struct {
	// MinWallTimeMsForLocal is the minimum wall-clock time before local
	// persistence is worthwhile.  Actions faster than this are not
	// persisted at all.
	MinWallTimeMsForLocal int64 `json:"minWallTimeMsForLocal,omitempty"`

	// MinWallTimeMsForShared is the minimum wall-clock time before
	// promotion to shared storage is justified.
	MinWallTimeMsForShared int64 `json:"minWallTimeMsForShared,omitempty"`

	// MinWallTimeMsForRemote is the minimum wall-clock time before
	// remote upload is justified.
	MinWallTimeMsForRemote int64 `json:"minWallTimeMsForRemote,omitempty"`

	// MaxOutputBytesForRemote caps the output size that is eligible for
	// remote upload.  Zero means no cap.
	MaxOutputBytesForRemote int64 `json:"maxOutputBytesForRemote,omitempty"`

	// MinOutputBytesForShared is the minimum output size at which shared
	// persistence makes sense even for fast actions — large outputs are
	// expensive to regenerate even if wall time is low.
	MinOutputBytesForShared int64 `json:"minOutputBytesForShared,omitempty"`
}

// DefaultPersistenceThresholds returns thresholds tuned for a typical
// Android/JVM build.  Actions under 500 ms are not persisted locally;
// actions under 2 s are not promoted to shared; actions under 10 s are
// not uploaded remotely.
func DefaultPersistenceThresholds() PersistenceThresholds {
	return PersistenceThresholds{
		MinWallTimeMsForLocal:   500,
		MinWallTimeMsForShared:  2000,
		MinWallTimeMsForRemote:  10000,
		MaxOutputBytesForRemote: 500 << 20, // 500 MiB
		MinOutputBytesForShared: 50 << 20,  // 50 MiB
	}
}

// RecommendTier returns the highest persistence tier justified by the
// given cost signals and thresholds.  It does not override the caller's
// existing shareability or retention-class policy — it advises what tier
// the cost profile alone supports.
//
// If the action is not cacheable at all the caller should not consult
// this function.
func RecommendTier(signals CostSignals, thresholds PersistenceThresholds) PersistenceTier {
	tier := PersistenceNone

	// Wall time is the primary cost axis.
	if signals.WallTimeMs >= thresholds.MinWallTimeMsForLocal && thresholds.MinWallTimeMsForLocal > 0 {
		tier = PersistenceLocal
	}
	if signals.WallTimeMs >= thresholds.MinWallTimeMsForShared && thresholds.MinWallTimeMsForShared > 0 {
		tier = PersistenceShared
	}
	if signals.WallTimeMs >= thresholds.MinWallTimeMsForRemote && thresholds.MinWallTimeMsForRemote > 0 {
		tier = PersistenceRemote
	}

	// Large outputs deserve shared storage even when wall time is low,
	// because regenerating them on a different worktree is expensive.
	if tier < PersistenceShared &&
		thresholds.MinOutputBytesForShared > 0 &&
		signals.OutputBytes >= thresholds.MinOutputBytesForShared {
		tier = PersistenceShared
	}

	// Cap remote tier when output exceeds the upload budget.
	if tier == PersistenceRemote &&
		thresholds.MaxOutputBytesForRemote > 0 &&
		signals.OutputBytes > thresholds.MaxOutputBytesForRemote {
		tier = PersistenceShared
	}

	return tier
}

// TierShareability returns the maximum shareability implied by a
// persistence tier.  This bridges cost-aware persistence into the
// existing retention/shareability model.
func TierShareability(tier PersistenceTier) Shareability {
	switch tier {
	case PersistenceRemote:
		return ShareabilityRemote
	case PersistenceShared:
		return ShareabilityMachine
	case PersistenceLocal:
		return ShareabilityWorktreeOnly
	default:
		return ShareabilityWorktreeOnly
	}
}
