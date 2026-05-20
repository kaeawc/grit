package cas

import (
	"context"
	"errors"
	"fmt"
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
	encoded, err := encodeEnvelope(summary)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, encoded, 0o644)
}

// GetActionSummary returns the sidecar CacheSummary for actionHash, or
// ErrNotFound if no summary has been written. A summary whose envelope
// schemaVersion does not match the current SchemaVersion is treated as
// absent so callers transparently re-run the producing action.
func (s *FilesystemStore) GetActionSummary(ctx context.Context, actionHash Hash) (CacheSummary, error) {
	if err := ctx.Err(); err != nil {
		return CacheSummary{}, err
	}
	data, err := readOrNotFound(s.summaryPath(actionHash))
	if err != nil {
		return CacheSummary{}, err
	}
	var summary CacheSummary
	if err := decodeEnvelope(data, &summary); err != nil {
		if errors.Is(err, ErrSchemaMismatch) {
			return CacheSummary{}, ErrNotFound
		}
		return CacheSummary{}, fmt.Errorf("cas: decode summary for %s: %w", actionHash, err)
	}
	return summary, nil
}
