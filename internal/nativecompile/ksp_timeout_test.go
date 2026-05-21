package nativecompile

import (
	"testing"
	"time"
)

func TestKSPTimeoutDefaults(t *testing.T) {
	t.Setenv("GRIT_KSP_TIMEOUT", "")
	if got := kspTimeout(); got != defaultKSPTimeout {
		t.Fatalf("default: got %v want %v", got, defaultKSPTimeout)
	}
}

func TestKSPTimeoutHonorsEnvOverride(t *testing.T) {
	t.Setenv("GRIT_KSP_TIMEOUT", "30m")
	if got := kspTimeout(); got != 30*time.Minute {
		t.Fatalf("override: got %v want 30m", got)
	}
	t.Setenv("GRIT_KSP_TIMEOUT", "2h")
	if got := kspTimeout(); got != 2*time.Hour {
		t.Fatalf("hour override: got %v want 2h", got)
	}
}

func TestKSPTimeoutZeroDisablesCap(t *testing.T) {
	t.Setenv("GRIT_KSP_TIMEOUT", "0")
	if got := kspTimeout(); got != 0 {
		t.Fatalf("zero should mean disabled, got %v", got)
	}
}

func TestKSPTimeoutRejectsGarbageAndNegative(t *testing.T) {
	for _, v := range []string{"garbage", "-5m"} {
		t.Setenv("GRIT_KSP_TIMEOUT", v)
		if got := kspTimeout(); got != defaultKSPTimeout {
			t.Errorf("invalid input %q: got %v, want default %v", v, got, defaultKSPTimeout)
		}
	}
}
