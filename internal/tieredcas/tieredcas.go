// Package tieredcas composes multiple cas.Store tiers into a single
// probe chain with promote-on-hit semantics.
//
// The tier order matches the architectural intent from
// roadmap/planning/dependency-cache-architecture.md:
//
//  1. Worktree overlay (closest, most specific)
//  2. Shared-local CAS (machine-scoped)
//  3. Remote cache (team-scoped)
//
// Reads probe tiers in order; a hit at tier N promotes the content to
// every closer tier on the way down so subsequent reads are served from
// the cheapest tier. Writes land in the primary (index 0) tier only:
// upload to upstream tiers is a separate, deliberate operation, not an
// implicit side effect of every local build action.
//
// A Store composed through tieredcas satisfies cas.Store so higher
// layers (downloaders, transforms, publishers) see it as a single store
// and do not need to know about tier composition.
package tieredcas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kaeawc/grit/internal/cas"
)

type ProbeOperation string

const (
	ProbeOperationGet             ProbeOperation = "get"
	ProbeOperationGetActionResult ProbeOperation = "getActionResult"
)

type ProbeOutcome string

const (
	ProbeOutcomeHit   ProbeOutcome = "hit"
	ProbeOutcomeMiss  ProbeOutcome = "miss"
	ProbeOutcomeError ProbeOutcome = "error"
)

// ProbeRecord describes one tier probe in order.
//
// Records are returned in deterministic probe order for a single
// operation. A hit record may mark Promoted and name the closer tiers
// that received the promotion write in the same deterministic order the
// promotion path used.
type ProbeRecord struct {
	Operation        ProbeOperation `json:"operation"`
	Tier             int            `json:"tier"`
	Outcome          ProbeOutcome   `json:"outcome"`
	DurationMs       int64          `json:"durationMs,omitempty"`
	Promoted         bool           `json:"promoted,omitempty"`
	PromotionTargets []int          `json:"promotionTargets,omitempty"`
	Error            string         `json:"error,omitempty"`
}

// ProbeOptions controls which tiers are probed during a read operation.
type ProbeOptions struct {
	// MaxTier limits probing to tiers with index < MaxTier. Zero means
	// no limit (probe all tiers). For example, MaxTier=2 probes tiers
	// 0 and 1 only, skipping any remote tiers beyond that.
	MaxTier int
}

// Store composes an ordered chain of cas.Store tiers.
//
// The tier at index 0 is the primary tier: every Put/PutBytes/PutExpected
// call writes there and only there. Tiers at higher indices are probed
// on reads and misses at lower tiers.
type Store struct {
	tiers []cas.Store
}

type actionResultHaver interface {
	HasActionResult(context.Context, cas.Hash) (bool, error)
}

// New returns a Store composing the given tiers in probe order. The
// first tier is the primary and must be writable. Subsequent tiers are
// fallbacks; they may be read-only.
//
// Returns an error if no tiers are given.
func New(tiers ...cas.Store) (*Store, error) {
	if len(tiers) == 0 {
		return nil, errors.New("tieredcas: at least one tier required")
	}
	return &Store{tiers: append([]cas.Store(nil), tiers...)}, nil
}

// Tiers returns the configured tier chain in probe order. The returned
// slice is a copy; callers may not mutate the Store by modifying it.
func (s *Store) Tiers() []cas.Store {
	return append([]cas.Store(nil), s.tiers...)
}

// Put writes to the primary tier only.
func (s *Store) Put(ctx context.Context, r io.Reader, prov cas.Provenance) (cas.BlobInfo, error) {
	return s.tiers[0].Put(ctx, r, prov)
}

// PutBytes writes to the primary tier only.
func (s *Store) PutBytes(ctx context.Context, data []byte, prov cas.Provenance) (cas.BlobInfo, error) {
	return s.tiers[0].PutBytes(ctx, data, prov)
}

// PutExpected writes to the primary tier only, verifying hash first.
func (s *Store) PutExpected(ctx context.Context, r io.Reader, expected cas.Hash, prov cas.Provenance) (cas.BlobInfo, error) {
	return s.tiers[0].PutExpected(ctx, r, expected, prov)
}

// PutBytesExpected writes to the primary tier only, verifying hash first.
func (s *Store) PutBytesExpected(ctx context.Context, data []byte, expected cas.Hash, prov cas.Provenance) (cas.BlobInfo, error) {
	return s.tiers[0].PutBytesExpected(ctx, data, expected, prov)
}

// GetWithProbeRecords probes tiers in order and returns the bytes plus
// a deterministic probe timeline. The timeline contains one record per
// tier probe, with the hit record marked when promotion occurred.
func (s *Store) GetWithProbeRecords(ctx context.Context, h cas.Hash) (io.ReadCloser, []ProbeRecord, error) {
	return s.getFromTiers(ctx, h, s.tiers)
}

// getFromTiers is the shared implementation for Get-family methods. It
// probes the given tier slice in order and returns the bytes plus probe
// records. Only the supplied tiers are probed; this enables local-only
// reads when the bandwidth budget is exhausted.
func (s *Store) getFromTiers(ctx context.Context, h cas.Hash, tiers []cas.Store) (io.ReadCloser, []ProbeRecord, error) {
	var records []ProbeRecord
	for i, tier := range tiers {
		start := time.Now()
		rc, err := tier.Get(ctx, h)
		if err == nil {
			if i == 0 {
				records = append(records, ProbeRecord{
					Operation:  ProbeOperationGet,
					Tier:       i,
					Outcome:    ProbeOutcomeHit,
					DurationMs: time.Since(start).Milliseconds(),
				})
				return rc, records, nil
			}
			data, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr != nil {
				return nil, append(records, ProbeRecord{
					Operation:  ProbeOperationGet,
					Tier:       i,
					Outcome:    ProbeOutcomeError,
					DurationMs: time.Since(start).Milliseconds(),
					Error:      readErr.Error(),
				}), readErr
			}
			targets := promotionTargets(i)
			records = append(records, ProbeRecord{
				Operation:        ProbeOperationGet,
				Tier:             i,
				Outcome:          ProbeOutcomeHit,
				DurationMs:       time.Since(start).Milliseconds(),
				Promoted:         len(targets) > 0,
				PromotionTargets: targets,
			})
			s.promote(ctx, h, data, i)
			return io.NopCloser(bytes.NewReader(data)), records, nil
		}
		if !errors.Is(err, cas.ErrNotFound) {
			return nil, append(records, ProbeRecord{
				Operation:  ProbeOperationGet,
				Tier:       i,
				Outcome:    ProbeOutcomeError,
				DurationMs: time.Since(start).Milliseconds(),
				Error:      err.Error(),
			}), fmt.Errorf("tieredcas: tier %d Get: %w", i, err)
		}
		records = append(records, ProbeRecord{
			Operation:  ProbeOperationGet,
			Tier:       i,
			Outcome:    ProbeOutcomeMiss,
			DurationMs: time.Since(start).Milliseconds(),
		})
	}
	return nil, records, cas.ErrNotFound
}

// Get probes tiers in order. On a hit at tier N > 0 the bytes are
// promoted to every closer tier (tiers with lower indices) before being
// returned to the caller. Promotion uses PutBytesExpected so the content
// hash is verified on the way down.
//
// Promotion provenance uses a generic ImportSource with a note recording
// the source tier index. Upstream provenance is not fetched — remote
// tiers typically don't track it and the extra round trip is not
// worthwhile for a promotion step.
func (s *Store) Get(ctx context.Context, h cas.Hash) (io.ReadCloser, error) {
	rc, _, err := s.GetWithProbeRecords(ctx, h)
	return rc, err
}

// GetWithOptions probes tiers in order, respecting ProbeOptions. When
// opts.MaxTier is set, only tiers with index < MaxTier are probed,
// allowing the caller to skip remote tiers (e.g. when the bandwidth
// budget is exhausted and the admission controller sets DeferRemote).
func (s *Store) GetWithOptions(ctx context.Context, h cas.Hash, opts ProbeOptions) (io.ReadCloser, []ProbeRecord, error) {
	tiers := s.probeTiers(opts)
	return s.getFromTiers(ctx, h, tiers)
}

// GetActionResultWithOptions probes tiers in order for a cached action
// result, respecting ProbeOptions. When opts.MaxTier is set, only tiers
// with index < MaxTier are probed.
func (s *Store) GetActionResultWithOptions(ctx context.Context, actionHash cas.Hash, opts ProbeOptions) (cas.ActionResult, []ProbeRecord, error) {
	tiers := s.probeTiers(opts)
	return s.getActionResultFromTiers(ctx, actionHash, tiers)
}

// probeTiers returns the subset of tiers to probe given the options.
func (s *Store) probeTiers(opts ProbeOptions) []cas.Store {
	if opts.MaxTier <= 0 || opts.MaxTier >= len(s.tiers) {
		return s.tiers
	}
	return s.tiers[:opts.MaxTier]
}

// Stat returns the first found blob info, probing tiers in order. It
// does not promote.
func (s *Store) Stat(ctx context.Context, h cas.Hash) (cas.BlobInfo, error) {
	for i, tier := range s.tiers {
		info, err := tier.Stat(ctx, h)
		if err == nil {
			return info, nil
		}
		if !errors.Is(err, cas.ErrNotFound) {
			return cas.BlobInfo{}, fmt.Errorf("tieredcas: tier %d Stat: %w", i, err)
		}
	}
	return cas.BlobInfo{}, cas.ErrNotFound
}

// Has returns true as soon as any tier reports the blob present. It
// does not promote.
func (s *Store) Has(ctx context.Context, h cas.Hash) (bool, error) {
	for i, tier := range s.tiers {
		has, err := tier.Has(ctx, h)
		if err != nil {
			return false, fmt.Errorf("tieredcas: tier %d Has: %w", i, err)
		}
		if has {
			return true, nil
		}
	}
	return false, nil
}

// HasActionResult reports whether any tier can serve the action result
// identified by actionHash. Tiers that expose a dedicated metadata query
// use it directly; tiers that do not fall back to GetActionResult.
func (s *Store) HasActionResult(ctx context.Context, actionHash cas.Hash) (bool, error) {
	for i, tier := range s.tiers {
		if haver, ok := tier.(actionResultHaver); ok {
			has, err := haver.HasActionResult(ctx, actionHash)
			if err != nil {
				return false, fmt.Errorf("tieredcas: tier %d HasActionResult: %w", i, err)
			}
			if has {
				return true, nil
			}
			continue
		}
		result, err := tier.GetActionResult(ctx, actionHash)
		if err != nil {
			if errors.Is(err, cas.ErrNotFound) {
				continue
			}
			return false, fmt.Errorf("tieredcas: tier %d GetActionResult: %w", i, err)
		}
		if result.ActionHash != actionHash {
			return false, fmt.Errorf("tieredcas: tier %d GetActionResult returned mismatched action hash %s", i, result.ActionHash)
		}
		return true, nil
	}
	return false, nil
}

// Provenance returns the provenance recorded in the primary tier.
// Upstream tiers typically do not track provenance and are not consulted.
func (s *Store) Provenance(ctx context.Context, h cas.Hash) (cas.Provenance, error) {
	return s.tiers[0].Provenance(ctx, h)
}

// PutActionResult writes to the primary tier only.
func (s *Store) PutActionResult(ctx context.Context, result cas.ActionResult) error {
	return s.tiers[0].PutActionResult(ctx, result)
}

// GetActionResult probes tiers in order. On a hit at tier N > 0 the
// result is promoted to the primary tier before returning so subsequent
// lookups are served locally.
func (s *Store) GetActionResult(ctx context.Context, actionHash cas.Hash) (cas.ActionResult, error) {
	result, _, err := s.GetActionResultWithProbeRecords(ctx, actionHash)
	return result, err
}

// GetActionResultWithProbeRecords probes tiers in order and returns the
// cached action result plus a deterministic probe timeline.
func (s *Store) GetActionResultWithProbeRecords(ctx context.Context, actionHash cas.Hash) (cas.ActionResult, []ProbeRecord, error) {
	return s.getActionResultFromTiers(ctx, actionHash, s.tiers)
}

// getActionResultFromTiers is the shared implementation for
// GetActionResult-family methods. Only the supplied tiers are probed.
func (s *Store) getActionResultFromTiers(ctx context.Context, actionHash cas.Hash, tiers []cas.Store) (cas.ActionResult, []ProbeRecord, error) {
	var records []ProbeRecord
	for i, tier := range tiers {
		start := time.Now()
		result, err := tier.GetActionResult(ctx, actionHash)
		if err == nil {
			record := ProbeRecord{
				Operation:  ProbeOperationGetActionResult,
				Tier:       i,
				Outcome:    ProbeOutcomeHit,
				DurationMs: time.Since(start).Milliseconds(),
			}
			if i > 0 {
				record.Promoted = true
				record.PromotionTargets = []int{0}
				_ = s.tiers[0].PutActionResult(ctx, result)
			}
			records = append(records, record)
			return result, records, nil
		}
		if !errors.Is(err, cas.ErrNotFound) {
			return cas.ActionResult{}, append(records, ProbeRecord{
				Operation:  ProbeOperationGetActionResult,
				Tier:       i,
				Outcome:    ProbeOutcomeError,
				DurationMs: time.Since(start).Milliseconds(),
				Error:      err.Error(),
			}), fmt.Errorf("tieredcas: tier %d GetActionResult: %w", i, err)
		}
		records = append(records, ProbeRecord{
			Operation:  ProbeOperationGetActionResult,
			Tier:       i,
			Outcome:    ProbeOutcomeMiss,
			DurationMs: time.Since(start).Milliseconds(),
		})
	}
	return cas.ActionResult{}, records, cas.ErrNotFound
}

// promote writes data to every tier closer than sourceTier (i.e. indices
// 0..sourceTier-1). Promotion errors are swallowed: a failure to promote
// to a closer tier is not a correctness failure, only a missed cache
// opportunity. The caller still gets the verified bytes.
func (s *Store) promote(ctx context.Context, h cas.Hash, data []byte, sourceTier int) {
	prov := cas.Provenance{
		Source: cas.Source{
			Kind: cas.SourceImport,
			Import: &cas.ImportSource{
				Note: fmt.Sprintf("tieredcas: promoted from tier %d", sourceTier),
			},
		},
	}
	for j := sourceTier - 1; j >= 0; j-- {
		_, _ = s.tiers[j].PutBytesExpected(ctx, data, h, prov)
	}
}

func promotionTargets(sourceTier int) []int {
	if sourceTier <= 0 {
		return nil
	}
	targets := make([]int, 0, sourceTier)
	for j := sourceTier - 1; j >= 0; j-- {
		targets = append(targets, j)
	}
	return targets
}

// Compile-time assertion that *Store satisfies cas.Store.
var _ cas.Store = (*Store)(nil)
