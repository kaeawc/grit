package cachepolicy

import "testing"

func TestRecommendTierWallTimeProgression(t *testing.T) {
	t.Parallel()

	thresholds := DefaultPersistenceThresholds()

	tests := []struct {
		name     string
		signals  CostSignals
		expected PersistenceTier
	}{
		{
			name:     "zero cost yields none",
			signals:  CostSignals{},
			expected: PersistenceNone,
		},
		{
			name:     "below local threshold yields none",
			signals:  CostSignals{WallTimeMs: 200},
			expected: PersistenceNone,
		},
		{
			name:     "at local threshold yields local",
			signals:  CostSignals{WallTimeMs: 500},
			expected: PersistenceLocal,
		},
		{
			name:     "between local and shared yields local",
			signals:  CostSignals{WallTimeMs: 1500},
			expected: PersistenceLocal,
		},
		{
			name:     "at shared threshold yields shared",
			signals:  CostSignals{WallTimeMs: 2000},
			expected: PersistenceShared,
		},
		{
			name:     "between shared and remote yields shared",
			signals:  CostSignals{WallTimeMs: 5000},
			expected: PersistenceShared,
		},
		{
			name:     "at remote threshold yields remote",
			signals:  CostSignals{WallTimeMs: 10000},
			expected: PersistenceRemote,
		},
		{
			name:     "well above remote threshold yields remote",
			signals:  CostSignals{WallTimeMs: 60000},
			expected: PersistenceRemote,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RecommendTier(tt.signals, thresholds)
			if got != tt.expected {
				t.Errorf("RecommendTier(%+v) = %q, want %q", tt.signals, got, tt.expected)
			}
		})
	}
}

func TestRecommendTierLargeOutputPromotesToShared(t *testing.T) {
	t.Parallel()

	thresholds := DefaultPersistenceThresholds()

	// Fast action but large output should be promoted to shared.
	signals := CostSignals{
		WallTimeMs:  100,
		OutputBytes: 60 << 20, // 60 MiB — above MinOutputBytesForShared
	}
	got := RecommendTier(signals, thresholds)
	if got != PersistenceShared {
		t.Errorf("expected large output to promote to shared, got %q", got)
	}
}

func TestRecommendTierLargeOutputCapsRemote(t *testing.T) {
	t.Parallel()

	thresholds := DefaultPersistenceThresholds()

	// Expensive action with huge output should be capped at shared.
	signals := CostSignals{
		WallTimeMs:  30000,
		OutputBytes: 600 << 20, // 600 MiB — above MaxOutputBytesForRemote
	}
	got := RecommendTier(signals, thresholds)
	if got != PersistenceShared {
		t.Errorf("expected oversized output to cap at shared, got %q", got)
	}
}

func TestRecommendTierRemoteWithinOutputCap(t *testing.T) {
	t.Parallel()

	thresholds := DefaultPersistenceThresholds()

	// Expensive action with reasonable output should be remote.
	signals := CostSignals{
		WallTimeMs:  30000,
		OutputBytes: 100 << 20, // 100 MiB — within cap
	}
	got := RecommendTier(signals, thresholds)
	if got != PersistenceRemote {
		t.Errorf("expected remote tier, got %q", got)
	}
}

func TestRecommendTierZeroThresholdsYieldsNone(t *testing.T) {
	t.Parallel()

	// All zero thresholds means "always none" — zero MinWallTimeMsForLocal
	// disables the local tier because the condition requires > 0.
	thresholds := PersistenceThresholds{}
	signals := CostSignals{WallTimeMs: 50000, OutputBytes: 1 << 30}

	got := RecommendTier(signals, thresholds)
	if got != PersistenceNone {
		t.Errorf("expected none with zero thresholds, got %q", got)
	}
}

func TestRecommendTierCustomThresholds(t *testing.T) {
	t.Parallel()

	thresholds := PersistenceThresholds{
		MinWallTimeMsForLocal:  100,
		MinWallTimeMsForShared: 500,
		MinWallTimeMsForRemote: 1000,
	}

	tests := []struct {
		name     string
		wallMs   int64
		expected PersistenceTier
	}{
		{"below local", 50, PersistenceNone},
		{"at local", 100, PersistenceLocal},
		{"at shared", 500, PersistenceShared},
		{"at remote", 1000, PersistenceRemote},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RecommendTier(CostSignals{WallTimeMs: tt.wallMs}, thresholds)
			if got != tt.expected {
				t.Errorf("RecommendTier(wallMs=%d) = %q, want %q", tt.wallMs, got, tt.expected)
			}
		})
	}
}

func TestTierShareability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tier     PersistenceTier
		expected Shareability
	}{
		{PersistenceNone, ShareabilityWorktreeOnly},
		{PersistenceLocal, ShareabilityWorktreeOnly},
		{PersistenceShared, ShareabilityMachine},
		{PersistenceRemote, ShareabilityRemote},
	}

	for _, tt := range tests {
		t.Run(tt.tier.String(), func(t *testing.T) {
			t.Parallel()
			got := TierShareability(tt.tier)
			if got != tt.expected {
				t.Errorf("TierShareability(%s) = %q, want %q", tt.tier, got, tt.expected)
			}
		})
	}
}

func TestDefaultPersistenceThresholdsAreConsistent(t *testing.T) {
	t.Parallel()

	thresholds := DefaultPersistenceThresholds()

	if thresholds.MinWallTimeMsForLocal <= 0 {
		t.Fatal("local threshold must be positive")
	}
	if thresholds.MinWallTimeMsForShared <= thresholds.MinWallTimeMsForLocal {
		t.Fatal("shared threshold must exceed local")
	}
	if thresholds.MinWallTimeMsForRemote <= thresholds.MinWallTimeMsForShared {
		t.Fatal("remote threshold must exceed shared")
	}
	if thresholds.MaxOutputBytesForRemote <= 0 {
		t.Fatal("remote output cap must be positive")
	}
	if thresholds.MinOutputBytesForShared <= 0 {
		t.Fatal("shared output floor must be positive")
	}
	if thresholds.MinOutputBytesForShared >= thresholds.MaxOutputBytesForRemote {
		t.Fatal("shared output floor must be below remote output cap")
	}
}

func TestPersistenceTierOrdering(t *testing.T) {
	t.Parallel()

	if !(PersistenceNone < PersistenceLocal) {
		t.Fatal("none should be less than local")
	}
	if !(PersistenceLocal < PersistenceShared) {
		t.Fatal("local should be less than shared")
	}
	if !(PersistenceShared < PersistenceRemote) {
		t.Fatal("shared should be less than remote")
	}
}

func TestPersistenceTierString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tier PersistenceTier
		want string
	}{
		{PersistenceNone, "none"},
		{PersistenceLocal, "local"},
		{PersistenceShared, "shared"},
		{PersistenceRemote, "remote"},
		{PersistenceTier(99), "unknown"},
	}
	for _, tt := range tests {
		got := tt.tier.String()
		if got != tt.want {
			t.Errorf("PersistenceTier(%d).String() = %q, want %q", int(tt.tier), got, tt.want)
		}
	}
}
