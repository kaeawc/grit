package explain

import "testing"

func TestInferCacheProbeMapsToExplicitStates(t *testing.T) {
	t.Parallel()

	got := InferCacheProbe("compileKotlin", 0, nil)
	if got.State != CacheProbeHit {
		t.Fatalf("expected cache hit, got %#v", got)
	}
	if got.AsTiming().State != StateReused {
		t.Fatalf("expected reused timing, got %#v", got.AsTiming())
	}

	got = InferCacheProbe("compileKotlin", 7, nil)
	if got.State != CacheProbeRebuilt {
		t.Fatalf("expected rebuilt cache probe, got %#v", got)
	}
	if got.AsTiming().State != StateRebuilt {
		t.Fatalf("expected rebuilt timing, got %#v", got.AsTiming())
	}

	got = InferCacheProbe("loadProject", 0, nil)
	if got.State != CacheProbeUnknown {
		t.Fatalf("expected unknown probe for non-cacheable action, got %#v", got)
	}
}
