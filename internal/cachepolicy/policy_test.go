package cachepolicy

import "testing"

func TestDefaultPolicyHasExpectedRetentionClasses(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()
	if policy.CleanupMode != CleanupModeBackground {
		t.Fatalf("cleanup mode = %q", policy.CleanupMode)
	}
	if policy.SharedTarget == 0 || policy.SharedHard == 0 || policy.SharedHard < policy.SharedTarget {
		t.Fatalf("unexpected shared size policy: %#v", policy)
	}
	if _, ok := policy.ClassPolicies[RetentionClassWorktreeEphemeral]; !ok {
		t.Fatalf("missing worktree retention class: %#v", policy.ClassPolicies)
	}
	if got := policy.ClassPolicies[RetentionClassPinned].RequiresReachableRoot; !got {
		t.Fatalf("expected pinned class to require reachable roots")
	}
	if got := policy.ClassPolicies[RetentionClassRemoteShareable].Shareability; got != ShareabilityRemote {
		t.Fatalf("unexpected remote shareability: %q", got)
	}
}

