package tieredcas

import "testing"

func TestIsLocalProbeTier(t *testing.T) {
	t.Run("known local tiers", func(t *testing.T) {
		for _, tier := range []string{ProbeTierLocalOverlay, ProbeTierSharedMachine} {
			if !IsLocalProbeTier(tier) {
				t.Fatalf("expected %q to be treated as local", tier)
			}
		}
	})

	t.Run("remote tier names stay non-local", func(t *testing.T) {
		for _, tier := range []string{"remote", "remote-tier1", "http-cache"} {
			if IsLocalProbeTier(tier) {
				t.Fatalf("expected %q to be treated as non-local", tier)
			}
		}
	})
}

func TestHasRemoteProbeTier(t *testing.T) {
	if HasRemoteProbeTier([]string{ProbeTierLocalOverlay, ProbeTierSharedMachine}) {
		t.Fatal("expected local-only probe order to skip remote admission")
	}
	if !HasRemoteProbeTier([]string{ProbeTierLocalOverlay, ProbeTierSharedMachine, "remote"}) {
		t.Fatal("expected probe order with remote tier to require remote admission")
	}
	if HasRemoteProbeTier(nil) {
		t.Fatal("expected nil probe order to have no remote tiers")
	}
}
