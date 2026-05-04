package cachepolicy

import (
	"testing"
	"time"
)

func TestManifestRootSetProtectsAcrossWorktrees(t *testing.T) {
	t.Parallel()

	rs := &ManifestRootSet{}
	rs.AddWorktreeRoots(WorktreeRoots{
		WorkRoot:      "/repo/a",
		ModelCacheKey: "key-a",
		Actions:       map[string]bool{"action.a1": true},
		Artifacts:     map[string]bool{"artifact.a1": true, "artifact.shared": true},
		RecordedAt:    time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
	})
	rs.AddWorktreeRoots(WorktreeRoots{
		WorkRoot:      "/repo/b",
		ModelCacheKey: "key-b",
		Actions:       map[string]bool{"action.b1": true},
		Artifacts:     map[string]bool{"artifact.b1": true, "artifact.shared": true},
		RecordedAt:    time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
	})

	if !rs.ProtectsAction("action.a1") {
		t.Fatal("expected action.a1 to be protected")
	}
	if !rs.ProtectsAction("action.b1") {
		t.Fatal("expected action.b1 to be protected")
	}
	if rs.ProtectsAction("action.unknown") {
		t.Fatal("expected unknown action to be unprotected")
	}
	if !rs.ProtectsArtifact("artifact.shared") {
		t.Fatal("expected artifact.shared to be protected from either worktree")
	}
	if rs.ProtectsArtifact("artifact.unknown") {
		t.Fatal("expected unknown artifact to be unprotected")
	}
}

func TestManifestRootSetProtectsFromPinnedRuns(t *testing.T) {
	t.Parallel()

	rs := &ManifestRootSet{}
	rs.AddPinnedRun(PinnedRunRoots{
		RunID:            "run-123",
		Reason:           "user pin",
		Actions:          map[string]bool{"action.pinned": true},
		Artifacts:        map[string]bool{"artifact.pinned": true},
		Materializations: map[string]bool{"mat.pinned": true},
		PinnedAt:         time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
	})

	if !rs.ProtectsAction("action.pinned") {
		t.Fatal("expected pinned action to be protected")
	}
	if !rs.ProtectsArtifact("artifact.pinned") {
		t.Fatal("expected pinned artifact to be protected")
	}
	if !rs.ProtectsMaterialization("mat.pinned") {
		t.Fatal("expected pinned materialization to be protected")
	}
}

func TestManifestRootSetNilReceiver(t *testing.T) {
	t.Parallel()

	var rs *ManifestRootSet
	if rs.ProtectsAction("anything") {
		t.Fatal("nil root set should not protect anything")
	}
	if rs.ProtectsArtifact("anything") {
		t.Fatal("nil root set should not protect anything")
	}
	if rs.ProtectsMaterialization("anything") {
		t.Fatal("nil root set should not protect anything")
	}
	if len(rs.AllProtectedActions()) != 0 {
		t.Fatal("nil root set should return empty map")
	}
}

func TestAddWorktreeRootsReplacesExisting(t *testing.T) {
	t.Parallel()

	rs := &ManifestRootSet{}
	rs.AddWorktreeRoots(WorktreeRoots{
		WorkRoot:  "/repo/a",
		Actions:   map[string]bool{"action.old": true},
		Artifacts: map[string]bool{"artifact.old": true},
	})
	rs.AddWorktreeRoots(WorktreeRoots{
		WorkRoot:  "/repo/a",
		Actions:   map[string]bool{"action.new": true},
		Artifacts: map[string]bool{"artifact.new": true},
	})

	if len(rs.Worktrees) != 1 {
		t.Fatalf("expected 1 worktree entry, got %d", len(rs.Worktrees))
	}
	if rs.ProtectsAction("action.old") {
		t.Fatal("replaced worktree should not protect old action")
	}
	if !rs.ProtectsAction("action.new") {
		t.Fatal("replaced worktree should protect new action")
	}
}

func TestAddPinnedRunReplacesExisting(t *testing.T) {
	t.Parallel()

	rs := &ManifestRootSet{}
	rs.AddPinnedRun(PinnedRunRoots{
		RunID:   "run-1",
		Actions: map[string]bool{"action.old": true},
	})
	rs.AddPinnedRun(PinnedRunRoots{
		RunID:   "run-1",
		Actions: map[string]bool{"action.new": true},
	})

	if len(rs.PinnedRuns) != 1 {
		t.Fatalf("expected 1 pinned run, got %d", len(rs.PinnedRuns))
	}
	if rs.ProtectsAction("action.old") {
		t.Fatal("replaced pin should not protect old action")
	}
	if !rs.ProtectsAction("action.new") {
		t.Fatal("replaced pin should protect new action")
	}
}

func TestRemovePinnedRun(t *testing.T) {
	t.Parallel()

	rs := &ManifestRootSet{}
	rs.AddPinnedRun(PinnedRunRoots{
		RunID:   "run-1",
		Actions: map[string]bool{"action.pinned": true},
	})

	if !rs.RemovePinnedRun("run-1") {
		t.Fatal("expected removal to return true")
	}
	if rs.ProtectsAction("action.pinned") {
		t.Fatal("removed pin should not protect action")
	}
	if rs.RemovePinnedRun("run-1") {
		t.Fatal("expected second removal to return false")
	}
}

func TestPruneWorktreesBefore(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	rs := &ManifestRootSet{}
	rs.AddWorktreeRoots(WorktreeRoots{
		WorkRoot:   "/repo/old",
		Actions:    map[string]bool{"action.old": true},
		RecordedAt: time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
	})
	rs.AddWorktreeRoots(WorktreeRoots{
		WorkRoot:   "/repo/current",
		Actions:    map[string]bool{"action.current": true},
		RecordedAt: time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
	})
	rs.AddWorktreeRoots(WorktreeRoots{
		WorkRoot: "/repo/no-timestamp",
		Actions:  map[string]bool{"action.notimestamp": true},
	})

	removed := rs.PruneWorktreesBefore(cutoff)
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if len(rs.Worktrees) != 2 {
		t.Fatalf("expected 2 remaining worktrees, got %d", len(rs.Worktrees))
	}
	if rs.ProtectsAction("action.old") {
		t.Fatal("pruned worktree should not protect action")
	}
	if !rs.ProtectsAction("action.current") {
		t.Fatal("retained worktree should protect action")
	}
	if !rs.ProtectsAction("action.notimestamp") {
		t.Fatal("zero-timestamp worktree should be retained")
	}
}

func TestAllProtectedMergesAcrossSources(t *testing.T) {
	t.Parallel()

	rs := &ManifestRootSet{}
	rs.AddWorktreeRoots(WorktreeRoots{
		WorkRoot:         "/repo/a",
		Actions:          map[string]bool{"action.w": true},
		Artifacts:        map[string]bool{"artifact.w": true},
		Materializations: map[string]bool{"mat.w": true},
	})
	rs.AddPinnedRun(PinnedRunRoots{
		RunID:            "run-1",
		Actions:          map[string]bool{"action.p": true},
		Artifacts:        map[string]bool{"artifact.p": true},
		Materializations: map[string]bool{"mat.p": true},
	})

	actions := rs.AllProtectedActions()
	if len(actions) != 2 || !actions["action.w"] || !actions["action.p"] {
		t.Fatalf("expected merged actions, got %v", actions)
	}
	artifacts := rs.AllProtectedArtifacts()
	if len(artifacts) != 2 || !artifacts["artifact.w"] || !artifacts["artifact.p"] {
		t.Fatalf("expected merged artifacts, got %v", artifacts)
	}
	mats := rs.AllProtectedMaterializations()
	if len(mats) != 2 || !mats["mat.w"] || !mats["mat.p"] {
		t.Fatalf("expected merged materializations, got %v", mats)
	}
}
