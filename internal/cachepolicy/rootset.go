package cachepolicy

import "time"

// ManifestRootSet aggregates cache-reachability roots from multiple
// sources: active worktrees and explicitly pinned runs.  It is designed
// to be persisted as JSON at the cache-root level and loaded by the
// cleanup planner so it can protect records reachable from any active
// worktree or pinned run, not just the single model snapshot being
// planned.
type ManifestRootSet struct {
	Worktrees  []WorktreeRoots  `json:"worktrees,omitempty"`
	PinnedRuns []PinnedRunRoots `json:"pinnedRuns,omitempty"`
}

// WorktreeRoots captures the set of IDs reachable from one worktree's
// latest successful model snapshot.
type WorktreeRoots struct {
	WorkRoot         string          `json:"workRoot"`
	ModelCacheKey    string          `json:"modelCacheKey"`
	Actions          map[string]bool `json:"actions,omitempty"`
	Artifacts        map[string]bool `json:"artifacts,omitempty"`
	Materializations map[string]bool `json:"materializations,omitempty"`
	RecordedAt       time.Time       `json:"recordedAt"`
}

// PinnedRunRoots captures the set of IDs that a user has explicitly
// pinned from a specific run.
type PinnedRunRoots struct {
	RunID            string          `json:"runId"`
	Reason           string          `json:"reason,omitempty"`
	Actions          map[string]bool `json:"actions,omitempty"`
	Artifacts        map[string]bool `json:"artifacts,omitempty"`
	Materializations map[string]bool `json:"materializations,omitempty"`
	PinnedAt         time.Time       `json:"pinnedAt"`
}

// ProtectsAction returns true if any worktree or pinned run in the root
// set references the given action ID.
func (rs *ManifestRootSet) ProtectsAction(id string) bool {
	if rs == nil {
		return false
	}
	for _, w := range rs.Worktrees {
		if w.Actions[id] {
			return true
		}
	}
	for _, p := range rs.PinnedRuns {
		if p.Actions[id] {
			return true
		}
	}
	return false
}

// ProtectsArtifact returns true if any worktree or pinned run in the
// root set references the given artifact ID.
func (rs *ManifestRootSet) ProtectsArtifact(id string) bool {
	if rs == nil {
		return false
	}
	for _, w := range rs.Worktrees {
		if w.Artifacts[id] {
			return true
		}
	}
	for _, p := range rs.PinnedRuns {
		if p.Artifacts[id] {
			return true
		}
	}
	return false
}

// ProtectsMaterialization returns true if any worktree or pinned run in
// the root set references the given materialization ID.
func (rs *ManifestRootSet) ProtectsMaterialization(id string) bool {
	if rs == nil {
		return false
	}
	for _, w := range rs.Worktrees {
		if w.Materializations[id] {
			return true
		}
	}
	for _, p := range rs.PinnedRuns {
		if p.Materializations[id] {
			return true
		}
	}
	return false
}

// AddWorktreeRoots appends or replaces roots for the given work root.
// If an entry with the same WorkRoot already exists it is replaced.
func (rs *ManifestRootSet) AddWorktreeRoots(wr WorktreeRoots) {
	for i, existing := range rs.Worktrees {
		if existing.WorkRoot == wr.WorkRoot {
			rs.Worktrees[i] = wr
			return
		}
	}
	rs.Worktrees = append(rs.Worktrees, wr)
}

// AddPinnedRun appends or replaces a pinned run by RunID.
func (rs *ManifestRootSet) AddPinnedRun(pr PinnedRunRoots) {
	for i, existing := range rs.PinnedRuns {
		if existing.RunID == pr.RunID {
			rs.PinnedRuns[i] = pr
			return
		}
	}
	rs.PinnedRuns = append(rs.PinnedRuns, pr)
}

// RemovePinnedRun removes a pinned run by RunID.  Returns true if a
// run was removed.
func (rs *ManifestRootSet) RemovePinnedRun(runID string) bool {
	for i, existing := range rs.PinnedRuns {
		if existing.RunID == runID {
			rs.PinnedRuns = append(rs.PinnedRuns[:i], rs.PinnedRuns[i+1:]...)
			return true
		}
	}
	return false
}

// PruneWorktreesBefore removes worktree entries whose RecordedAt is
// before the given cutoff.  Returns the number of entries removed.
func (rs *ManifestRootSet) PruneWorktreesBefore(cutoff time.Time) int {
	kept := rs.Worktrees[:0]
	removed := 0
	for _, w := range rs.Worktrees {
		if !w.RecordedAt.IsZero() && w.RecordedAt.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, w)
	}
	rs.Worktrees = kept
	return removed
}

// AllProtectedActions returns the union of all action IDs across every
// source in the root set.
func (rs *ManifestRootSet) AllProtectedActions() map[string]bool {
	out := map[string]bool{}
	if rs == nil {
		return out
	}
	for _, w := range rs.Worktrees {
		for id := range w.Actions {
			out[id] = true
		}
	}
	for _, p := range rs.PinnedRuns {
		for id := range p.Actions {
			out[id] = true
		}
	}
	return out
}

// AllProtectedArtifacts returns the union of all artifact IDs across
// every source in the root set.
func (rs *ManifestRootSet) AllProtectedArtifacts() map[string]bool {
	out := map[string]bool{}
	if rs == nil {
		return out
	}
	for _, w := range rs.Worktrees {
		for id := range w.Artifacts {
			out[id] = true
		}
	}
	for _, p := range rs.PinnedRuns {
		for id := range p.Artifacts {
			out[id] = true
		}
	}
	return out
}

// AllProtectedMaterializations returns the union of all materialization
// IDs across every source in the root set.
func (rs *ManifestRootSet) AllProtectedMaterializations() map[string]bool {
	out := map[string]bool{}
	if rs == nil {
		return out
	}
	for _, w := range rs.Worktrees {
		for id := range w.Materializations {
			out[id] = true
		}
	}
	for _, p := range rs.PinnedRuns {
		for id := range p.Materializations {
			out[id] = true
		}
	}
	return out
}
