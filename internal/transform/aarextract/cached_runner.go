package aarextract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/clock"
	"github.com/kaeawc/grit/internal/tieredcas"
)

// CachedRunner is the production action-cache wrapper for the aar-extract
// transform. It probes a tieredcas chain for an existing action result,
// executes Extract on miss, persists the result back into the chain, and
// emits a turborepo-style CacheSummary sidecar to the primary tier so
// post-hoc cache auditing works without re-running the build.
//
// Promotion to higher tiers is gated by an UploadPolicy: by default the
// zero-value policy denies every upload, so callers wiring this up must
// supply a populated policy to opt into remote sharing.
//
// CachedRunner is the canonical first user of the action-cache wiring.
// Once its shape is proven against aar-extract (this package), the
// pattern lifts to a shared helper and other action kinds adopt it.
type CachedRunner struct {
	Store        *tieredcas.Store
	UploadPolicy tieredcas.UploadPolicy

	// Clock provides timestamps for CacheSummary entries. Tests pass
	// clock.NewFake; nil falls back to clock.System.
	Clock clock.Clock
}

// Run extracts aarHash through the cache chain.
func (r *CachedRunner) Run(ctx context.Context, aarHash cas.Hash) (cas.ActionResult, error) {
	if r == nil || r.Store == nil {
		return cas.ActionResult{}, errors.New("aarextract: CachedRunner: nil store")
	}
	if err := ctx.Err(); err != nil {
		return cas.ActionResult{}, err
	}

	c := r.Clock
	if c == nil {
		c = clock.System{}
	}
	start := c.Now()

	action := Action(aarHash)
	actionHash := action.Hash()

	cached, records, err := r.Store.GetActionResultWithProbeRecords(ctx, actionHash)
	if err == nil {
		summary := buildSummary(actionHash, records, "hit", start, c.Now())
		_ = writePrimarySummary(ctx, r.Store, summary)
		return cached, nil
	}
	if !errors.Is(err, cas.ErrNotFound) {
		summary := buildSummary(actionHash, records, "error", start, c.Now())
		_ = writePrimarySummary(ctx, r.Store, summary)
		return cas.ActionResult{}, err
	}

	// Miss: execute against the tiered store. Extract internally probes
	// for a cached action result (which we already know is missing) and
	// then writes the new result via PutActionResult, which the tiered
	// store routes to the primary tier.
	result, runErr := Extract(ctx, r.Store, aarHash)
	outcome := "miss"
	if runErr != nil {
		outcome = "error"
	}
	summary := buildSummary(actionHash, records, outcome, start, c.Now())
	_ = writePrimarySummary(ctx, r.Store, summary)
	if runErr != nil {
		return cas.ActionResult{}, runErr
	}

	if err := r.maybePromote(ctx, action.Kind, result); err != nil {
		return cas.ActionResult{}, fmt.Errorf("aarextract: promote action result: %w", err)
	}
	return result, nil
}

// maybePromote consults UploadPolicy and, when allowed, writes the
// action result and its output blobs to non-primary tiers. The primary
// tier was already populated by Extract via PutActionResult.
func (r *CachedRunner) maybePromote(ctx context.Context, kind string, result cas.ActionResult) error {
	tiers := r.Store.Tiers()
	if len(tiers) <= 1 {
		return nil
	}
	resultSize := totalOutputSize(result)
	primary := tiers[0]
	for i := 1; i < len(tiers); i++ {
		if !r.UploadPolicy.ShouldUpload(kind, i, resultSize) {
			continue
		}
		if err := promoteOutputBlobs(ctx, primary, tiers[i], result); err != nil {
			return fmt.Errorf("tier %d outputs: %w", i, err)
		}
		if err := tiers[i].PutActionResult(ctx, result); err != nil {
			return fmt.Errorf("tier %d action-result: %w", i, err)
		}
	}
	return nil
}

func totalOutputSize(result cas.ActionResult) int64 {
	var total int64
	for _, o := range result.Outputs {
		total += o.Blob.Size
	}
	return total
}

func promoteOutputBlobs(ctx context.Context, src, dst cas.Store, result cas.ActionResult) error {
	for _, out := range result.Outputs {
		if has, err := dst.Has(ctx, out.Blob.Hash); err == nil && has {
			continue
		}
		rc, err := src.Get(ctx, out.Blob.Hash)
		if err != nil {
			return fmt.Errorf("read %s from primary: %w", out.Blob.Hash, err)
		}
		data, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		prov, _ := src.Provenance(ctx, out.Blob.Hash)
		if _, err := dst.PutBytesExpected(ctx, data, out.Blob.Hash, prov); err != nil {
			return fmt.Errorf("write %s to higher tier: %w", out.Blob.Hash, err)
		}
	}
	return nil
}

func buildSummary(actionHash cas.Hash, records []tieredcas.ProbeRecord, outcome string, start, end time.Time) cas.CacheSummary {
	steps := make([]cas.ProbeStep, 0, len(records))
	for _, r := range records {
		steps = append(steps, cas.ProbeStep{
			Operation:        string(r.Operation),
			Tier:             r.Tier,
			Outcome:          string(r.Outcome),
			DurationMs:       r.DurationMs,
			Promoted:         r.Promoted,
			PromotionTargets: append([]int(nil), r.PromotionTargets...),
			Error:            r.Error,
		})
	}
	return cas.CacheSummary{
		ActionHash:    actionHash,
		ProbeSequence: steps,
		Outcome:       outcome,
		DurationMs:    end.Sub(start).Milliseconds(),
		Timestamp:     end.UTC(),
	}
}

// writePrimarySummary writes the CacheSummary sidecar to the primary
// tier. We type-assert to the concrete *cas.FilesystemStore because
// CacheSummary persistence isn't on the cas.Store interface — it's a
// FilesystemStore extension. Other tier implementations (notably the
// remote-cache adapter) silently no-op.
func writePrimarySummary(ctx context.Context, store *tieredcas.Store, summary cas.CacheSummary) error {
	tiers := store.Tiers()
	if len(tiers) == 0 {
		return nil
	}
	fs, ok := tiers[0].(*cas.FilesystemStore)
	if !ok {
		return nil
	}
	return fs.PutActionSummary(ctx, summary)
}
