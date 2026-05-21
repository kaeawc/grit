package nativecompile

import (
	"context"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestKSPModeDefaultsToTwo(t *testing.T) {
	t.Setenv("GRIT_KSP_MODE", "")
	if got := kspMode(); got != 2 {
		t.Fatalf("default mode: got %d want 2", got)
	}
}

func TestKSPModeHonorsExplicitOnes(t *testing.T) {
	for value, want := range map[string]int{"1": 1, "2": 2} {
		t.Setenv("GRIT_KSP_MODE", value)
		if got := kspMode(); got != want {
			t.Errorf("GRIT_KSP_MODE=%s: got %d want %d", value, got, want)
		}
	}
}

func TestKSPModeUnknownFallsBackToTwo(t *testing.T) {
	for _, v := range []string{"3", "garbage", "auto", "true"} {
		t.Setenv("GRIT_KSP_MODE", v)
		if got := kspMode(); got != 2 {
			t.Errorf("GRIT_KSP_MODE=%q: got %d, want default 2", v, got)
		}
	}
}

func TestRunKSP1ForModuleReturnsStructuredError(t *testing.T) {
	c := &Compiler{}
	mod := &project.Module{Path: ":app"}
	_, err := c.runKSP1ForModule(context.Background(), nil, nil, mod, "debug", "", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error from stub")
	}
	msg := err.Error()
	if !strings.Contains(msg, ":app") {
		t.Fatalf("error should name the module: %q", msg)
	}
	if !strings.Contains(msg, "GRIT_KSP_MODE") {
		t.Fatalf("error should reference the env var so users know how to fall back: %q", msg)
	}
}
