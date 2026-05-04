package explain

type CacheProbeState string

const (
	CacheProbeUnknown     CacheProbeState = "unknown"
	CacheProbeHit         CacheProbeState = "hit"
	CacheProbeMiss        CacheProbeState = "miss"
	CacheProbeRebuilt     CacheProbeState = "rebuilt"
	CacheProbeInvalidated CacheProbeState = "invalidated"
)

type CacheProbe struct {
	State  CacheProbeState `json:"state"`
	Basis  string          `json:"basis,omitempty"`
	Detail string          `json:"detail,omitempty"`
}

func CacheHit(basis, detail string) CacheProbe {
	return CacheProbe{State: CacheProbeHit, Basis: basis, Detail: detail}
}

func CacheMiss(basis, detail string) CacheProbe {
	return CacheProbe{State: CacheProbeMiss, Basis: basis, Detail: detail}
}

func CacheRebuilt(basis, detail string) CacheProbe {
	return CacheProbe{State: CacheProbeRebuilt, Basis: basis, Detail: detail}
}

func CacheInvalidated(basis, detail string) CacheProbe {
	return CacheProbe{State: CacheProbeInvalidated, Basis: basis, Detail: detail}
}

func CacheUnknown(basis, detail string) CacheProbe {
	return CacheProbe{State: CacheProbeUnknown, Basis: basis, Detail: detail}
}

func InferCacheProbe(actionName string, durationMs int64, err error) CacheProbe {
	timing := InferTiming(actionName, durationMs, err)
	if timing == nil {
		return CacheUnknown("not-cacheable", "action is not cacheable")
	}
	switch timing.State {
	case StateReused:
		return CacheHit(timing.Basis, timing.Detail)
	case StateRebuilt:
		return CacheRebuilt(timing.Basis, timing.Detail)
	default:
		return CacheUnknown(timing.Basis, timing.Detail)
	}
}

func (p CacheProbe) AsTiming() *Timing {
	switch p.State {
	case CacheProbeHit:
		return &Timing{State: StateReused, Basis: p.Basis, Detail: p.Detail}
	case CacheProbeMiss, CacheProbeRebuilt, CacheProbeInvalidated:
		return &Timing{State: StateRebuilt, Basis: p.Basis, Detail: p.Detail}
	default:
		return &Timing{State: StateUnknown, Basis: p.Basis, Detail: p.Detail}
	}
}
