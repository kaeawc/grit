package cas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/kaeawc/grit/internal/fsutil"
)

// ProbeStep is a single tier probe captured in a CacheSummary. Mirrors the
// shape of tieredcas.ProbeRecord but is defined here so cas has no upward
// dependency on tieredcas.
type ProbeStep struct {
	Operation        string `json:"operation"`
	Tier             int    `json:"tier"`
	Outcome          string `json:"outcome"`
	DurationMs       int64  `json:"durationMs,omitempty"`
	Promoted         bool   `json:"promoted,omitempty"`
	PromotionTargets []int  `json:"promotionTargets,omitempty"`
	Error            string `json:"error,omitempty"`
}

// CacheSummary is a turborepo-style sidecar written next to an action
// result. It records the probe sequence, overall outcome, total duration,
// and timestamp, so cache behavior can be audited after the fact without
// re-running the build.
type CacheSummary struct {
	ActionHash    Hash        `json:"actionHash"`
	ProbeSequence []ProbeStep `json:"probeSequence"`
	Outcome       string      `json:"outcome"`
	DurationMs    int64       `json:"durationMs"`
	Timestamp     time.Time   `json:"timestamp"`
}

func (s *FilesystemStore) summaryPath(h Hash) string {
	hex := h.String()
	return filepath.Join(s.root, "actions", hex[:2], hex[2:]+".summary.json")
}

// PutActionSummary writes a CacheSummary as a sidecar JSON file alongside
// the action result for summary.ActionHash.
func (s *FilesystemStore) PutActionSummary(ctx context.Context, summary CacheSummary) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if summary.ActionHash.IsZero() {
		return fmt.Errorf("cas: PutActionSummary: zero action hash")
	}
	if summary.Timestamp.IsZero() {
		summary.Timestamp = s.now().UTC()
	}
	path := s.summaryPath(summary.ActionHash)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, encoded, 0o644)
}

// GetActionSummary returns the sidecar CacheSummary for actionHash, or
// ErrNotFound if no summary has been written.
func (s *FilesystemStore) GetActionSummary(ctx context.Context, actionHash Hash) (CacheSummary, error) {
	if err := ctx.Err(); err != nil {
		return CacheSummary{}, err
	}
	data, err := os.ReadFile(s.summaryPath(actionHash))
	if errors.Is(err, fs.ErrNotExist) {
		return CacheSummary{}, ErrNotFound
	}
	if err != nil {
		return CacheSummary{}, err
	}
	var summary CacheSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return CacheSummary{}, fmt.Errorf("cas: decode summary for %s: %w", actionHash, err)
	}
	return summary, nil
}
