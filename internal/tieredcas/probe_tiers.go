package tieredcas

// Scheduler-facing probe tier names. These string values are persisted in
// action schedules and run summaries, so callers outside this package need a
// shared source of truth when deciding whether a probe is local-only or may
// hit the network.
const (
	ProbeTierLocalOverlay  = "local-overlay"
	ProbeTierSharedMachine = "shared-machine"
)

// IsLocalProbeTier reports whether a scheduler probe tier is one of the tiers
// considered local by tieredcas. Any other non-empty tier name is treated as
// remote/constrained so new upstream tiers do not silently bypass admission.
func IsLocalProbeTier(tier string) bool {
	switch tier {
	case ProbeTierLocalOverlay, ProbeTierSharedMachine:
		return true
	default:
		return false
	}
}

// HasRemoteProbeTier reports whether the probe order includes any non-local
// tier and therefore may need bandwidth-aware admission.
func HasRemoteProbeTier(probeOrder []string) bool {
	for _, tier := range probeOrder {
		if tier != "" && !IsLocalProbeTier(tier) {
			return true
		}
	}
	return false
}
