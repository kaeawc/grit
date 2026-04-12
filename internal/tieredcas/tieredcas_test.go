package tieredcas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

type actionResultStoreBase struct{}

func (actionResultStoreBase) Put(context.Context, io.Reader, cas.Provenance) (cas.BlobInfo, error) {
	return cas.BlobInfo{}, cas.ErrNotFound
}

func (actionResultStoreBase) PutBytes(context.Context, []byte, cas.Provenance) (cas.BlobInfo, error) {
	return cas.BlobInfo{}, cas.ErrNotFound
}

func (actionResultStoreBase) PutExpected(context.Context, io.Reader, cas.Hash, cas.Provenance) (cas.BlobInfo, error) {
	return cas.BlobInfo{}, cas.ErrNotFound
}

func (actionResultStoreBase) PutBytesExpected(context.Context, []byte, cas.Hash, cas.Provenance) (cas.BlobInfo, error) {
	return cas.BlobInfo{}, cas.ErrNotFound
}

func (actionResultStoreBase) Get(context.Context, cas.Hash) (io.ReadCloser, error) {
	return nil, cas.ErrNotFound
}

func (actionResultStoreBase) Stat(context.Context, cas.Hash) (cas.BlobInfo, error) {
	return cas.BlobInfo{}, cas.ErrNotFound
}

func (actionResultStoreBase) Has(context.Context, cas.Hash) (bool, error) {
	return false, nil
}

func (actionResultStoreBase) Provenance(context.Context, cas.Hash) (cas.Provenance, error) {
	return cas.Provenance{}, cas.ErrNotFound
}

func (actionResultStoreBase) PutActionResult(context.Context, cas.ActionResult) error {
	return nil
}

type metadataActionResultStore struct {
	actionResultStoreBase
	hasActionResultCalls int
	getActionResultCalls int
	hasActionResult      bool
	hasActionResultErr   error
	actionResult         cas.ActionResult
	getActionResultErr   error
}

func (s *metadataActionResultStore) HasActionResult(context.Context, cas.Hash) (bool, error) {
	s.hasActionResultCalls++
	if s.hasActionResultErr != nil {
		return false, s.hasActionResultErr
	}
	return s.hasActionResult, nil
}

func (s *metadataActionResultStore) GetActionResult(context.Context, cas.Hash) (cas.ActionResult, error) {
	s.getActionResultCalls++
	if s.getActionResultErr != nil {
		return cas.ActionResult{}, s.getActionResultErr
	}
	if s.actionResult.ActionHash.IsZero() {
		return cas.ActionResult{}, cas.ErrNotFound
	}
	return s.actionResult, nil
}

type getOnlyActionResultStore struct {
	actionResultStoreBase
	getActionResultCalls int
	actionResult         cas.ActionResult
	getActionResultErr   error
}

func (s *getOnlyActionResultStore) GetActionResult(context.Context, cas.Hash) (cas.ActionResult, error) {
	s.getActionResultCalls++
	if s.getActionResultErr != nil {
		return cas.ActionResult{}, s.getActionResultErr
	}
	if s.actionResult.ActionHash.IsZero() {
		return cas.ActionResult{}, cas.ErrNotFound
	}
	return s.actionResult, nil
}

func TestNewRejectsEmpty(t *testing.T) {
	if _, err := New(); err == nil {
		t.Fatalf("expected error for zero tiers")
	}
}

func TestNewSingleTierActsAsPassthrough(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	s, err := New(primary)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	info, err := s.PutBytes(ctx, []byte("hello"), cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "single"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	rc, err := s.Get(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "hello" {
		t.Fatalf("round trip mismatch")
	}
}

func TestGetProbesUpstreamAndPromotes(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	// Seed upstream with content the primary has never seen.
	payload := []byte("upstream-only content")
	seedInfo, err := upstream.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "upstream seed"}},
	})
	if err != nil {
		t.Fatalf("seed upstream: %v", err)
	}
	if has, _ := primary.Has(ctx, seedInfo.Hash); has {
		t.Fatalf("primary should start empty for this hash")
	}

	s, err := New(primary, upstream)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rc, err := s.Get(ctx, seedInfo.Hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("Get returned wrong content")
	}

	// After Get, the primary tier must hold the content (promoted).
	has, err := primary.Has(ctx, seedInfo.Hash)
	if err != nil {
		t.Fatalf("primary Has: %v", err)
	}
	if !has {
		t.Fatalf("primary did not receive promoted blob")
	}
	primaryProv, err := primary.Provenance(ctx, seedInfo.Hash)
	if err != nil {
		t.Fatalf("primary Provenance: %v", err)
	}
	if primaryProv.Source.Import == nil || primaryProv.Source.Import.Note == "" {
		t.Fatalf("promotion provenance missing: %+v", primaryProv.Source)
	}
}

func TestGetWithProbeRecordsReturnsOrderedTimeline(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	payload := []byte("probe trace")
	info, err := upstream.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := New(primary, upstream)
	if err != nil {
		t.Fatal(err)
	}

	rc, records, err := s.GetWithProbeRecords(ctx, info.Hash)
	if err != nil {
		t.Fatalf("GetWithProbeRecords: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("GetWithProbeRecords returned wrong content")
	}
	if len(records) != 2 {
		t.Fatalf("expected two probe records, got %#v", records)
	}
	if records[0].Tier != 0 || records[0].Outcome != ProbeOutcomeMiss {
		t.Fatalf("unexpected primary probe record: %#v", records[0])
	}
	if records[0].DurationMs < 0 {
		t.Fatalf("expected non-negative primary probe duration, got %#v", records[0])
	}
	if records[1].Tier != 1 || records[1].Outcome != ProbeOutcomeHit || !records[1].Promoted {
		t.Fatalf("unexpected upstream probe record: %#v", records[1])
	}
	if records[1].DurationMs < 0 {
		t.Fatalf("expected non-negative upstream probe duration, got %#v", records[1])
	}
	if len(records[1].PromotionTargets) != 1 || records[1].PromotionTargets[0] != 0 {
		t.Fatalf("unexpected promotion targets: %#v", records[1])
	}
	if has, _ := primary.Has(ctx, info.Hash); !has {
		t.Fatalf("primary did not receive promoted blob")
	}
}

func TestGetPromotesAcrossMultipleTiers(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	middle := cas.NewFilesystemStore(t.TempDir())
	distant := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	payload := []byte("distant content")
	info, err := distant.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := New(primary, middle, distant)
	if err != nil {
		t.Fatal(err)
	}

	rc, err := s.Get(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_, _ = io.ReadAll(rc)
	_ = rc.Close()

	if has, _ := primary.Has(ctx, info.Hash); !has {
		t.Fatalf("primary missing after promotion from distant")
	}
	if has, _ := middle.Has(ctx, info.Hash); !has {
		t.Fatalf("middle missing after promotion from distant")
	}
}

func TestGetNotFoundAcrossAllTiers(t *testing.T) {
	s, err := New(cas.NewFilesystemStore(t.TempDir()), cas.NewFilesystemStore(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Get(context.Background(), cas.HashBytes([]byte("never written")))
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPutWritesPrimaryOnly(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	s, err := New(primary, upstream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.PutBytes(ctx, []byte("local only"), cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "local"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if has, _ := primary.Has(ctx, info.Hash); !has {
		t.Fatalf("primary missing after Put")
	}
	if has, _ := upstream.Has(ctx, info.Hash); has {
		t.Fatalf("upstream should not receive local writes automatically")
	}
}

func TestHasProbesAllTiers(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	payload := []byte("upstream has")
	info, err := upstream.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := New(primary, upstream)
	if err != nil {
		t.Fatal(err)
	}

	has, err := s.Has(ctx, info.Hash)
	if err != nil || !has {
		t.Fatalf("expected Has to hit upstream: has=%v err=%v", has, err)
	}
}

func TestHasActionResultUsesMetadataQueryWhenAvailable(t *testing.T) {
	ctx := context.Background()
	actionHash := cas.HashBytes([]byte("metadata-action"))
	primary := &metadataActionResultStore{hasActionResult: false}
	upstream := &metadataActionResultStore{hasActionResult: true}

	s, err := New(primary, upstream)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	has, err := s.HasActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("HasActionResult: %v", err)
	}
	if !has {
		t.Fatalf("expected action result to be reported present")
	}
	if primary.hasActionResultCalls != 1 || primary.getActionResultCalls != 0 {
		t.Fatalf("expected primary metadata query only, got %#v", primary)
	}
	if upstream.hasActionResultCalls != 1 || upstream.getActionResultCalls != 0 {
		t.Fatalf("expected upstream metadata query only, got %#v", upstream)
	}
}

func TestHasActionResultFallsBackToGetActionResult(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	actionHash := cas.HashBytes([]byte("fallback-action"))
	result := cas.ActionResult{
		ActionHash: actionHash,
		Outputs: []cas.NamedOutput{
			{Role: "out", Blob: cas.BlobInfo{Hash: cas.HashBytes([]byte("output")), Size: 6}},
		},
	}
	if err := primary.PutActionResult(ctx, result); err != nil {
		t.Fatalf("PutActionResult: %v", err)
	}

	s, err := New(primary)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	has, err := s.HasActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("HasActionResult: %v", err)
	}
	if !has {
		t.Fatalf("expected action result to be reported present")
	}
}

func TestStatFallsThroughToUpstream(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	payload := []byte("stat upstream")
	info, err := upstream.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s, _ := New(primary, upstream)
	got, err := s.Stat(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got.Hash != info.Hash {
		t.Fatalf("Stat hash mismatch")
	}
}

func TestActionResultProbesAndPromotes(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	actionHash := cas.HashBytes([]byte("action"))
	result := cas.ActionResult{
		ActionHash: actionHash,
		Outputs: []cas.NamedOutput{
			{Role: "out", Blob: cas.BlobInfo{Hash: cas.HashBytes([]byte("output")), Size: 6}},
		},
	}
	if err := upstream.PutActionResult(ctx, result); err != nil {
		t.Fatal(err)
	}

	s, _ := New(primary, upstream)
	loaded, err := s.GetActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("GetActionResult: %v", err)
	}
	if loaded.ActionHash != actionHash {
		t.Fatalf("action hash mismatch")
	}

	// After the read, the primary tier should hold the promoted result.
	primaryResult, err := primary.GetActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("primary GetActionResult after promote: %v", err)
	}
	if primaryResult.ActionHash != actionHash {
		t.Fatalf("primary action hash after promote mismatch")
	}
}

func TestGetActionResultWithProbeRecordsReturnsOrderedTimeline(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	actionHash := cas.HashBytes([]byte("action-records"))
	result := cas.ActionResult{
		ActionHash: actionHash,
		Outputs: []cas.NamedOutput{
			{Role: "out", Blob: cas.BlobInfo{Hash: cas.HashBytes([]byte("output")), Size: 6}},
		},
	}
	if err := upstream.PutActionResult(ctx, result); err != nil {
		t.Fatal(err)
	}

	s, err := New(primary, upstream)
	if err != nil {
		t.Fatal(err)
	}

	loaded, records, err := s.GetActionResultWithProbeRecords(ctx, actionHash)
	if err != nil {
		t.Fatalf("GetActionResultWithProbeRecords: %v", err)
	}
	if loaded.ActionHash != actionHash {
		t.Fatalf("action hash mismatch")
	}
	if len(records) != 2 {
		t.Fatalf("expected two probe records, got %#v", records)
	}
	if records[0].Tier != 0 || records[0].Outcome != ProbeOutcomeMiss {
		t.Fatalf("unexpected primary probe record: %#v", records[0])
	}
	if records[0].DurationMs < 0 {
		t.Fatalf("expected non-negative primary action probe duration, got %#v", records[0])
	}
	if records[1].Tier != 1 || records[1].Outcome != ProbeOutcomeHit || !records[1].Promoted {
		t.Fatalf("unexpected upstream probe record: %#v", records[1])
	}
	if records[1].DurationMs < 0 {
		t.Fatalf("expected non-negative upstream action probe duration, got %#v", records[1])
	}
	if len(records[1].PromotionTargets) != 1 || records[1].PromotionTargets[0] != 0 {
		t.Fatalf("unexpected action promotion targets: %#v", records[1])
	}
	primaryResult, err := primary.GetActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("primary GetActionResult after promote: %v", err)
	}
	if primaryResult.ActionHash != actionHash {
		t.Fatalf("primary action hash after promote mismatch")
	}
}

func TestActionResultPutPrimaryOnly(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	s, _ := New(primary, upstream)
	result := cas.ActionResult{
		ActionHash: cas.HashBytes([]byte("a")),
		Outputs:    []cas.NamedOutput{{Role: "r", Blob: cas.BlobInfo{Hash: cas.HashBytes([]byte("o")), Size: 1}}},
	}
	if err := s.PutActionResult(ctx, result); err != nil {
		t.Fatal(err)
	}

	if _, err := primary.GetActionResult(ctx, result.ActionHash); err != nil {
		t.Fatalf("primary should have result: %v", err)
	}
	if _, err := upstream.GetActionResult(ctx, result.ActionHash); !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("upstream should not receive Put: err=%v", err)
	}
}

func TestTiersCopySnapshot(t *testing.T) {
	a := cas.NewFilesystemStore(t.TempDir())
	b := cas.NewFilesystemStore(t.TempDir())
	s, _ := New(a, b)
	snap := s.Tiers()
	if len(snap) != 2 {
		t.Fatalf("Tiers: expected 2, got %d", len(snap))
	}
	snap[0] = nil
	// Original Store must still work after caller mutates the snapshot.
	_, err := s.PutBytes(context.Background(), []byte("still works"), cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "t"}},
	})
	if err != nil {
		t.Fatalf("mutation of snapshot leaked into Store: %v", err)
	}
}

func TestGetFromPrimaryStreamsDirectly(t *testing.T) {
	// Regression guard: when the primary tier holds the blob, Get must
	// not round-trip through io.ReadAll. This test cannot observe that
	// directly without a mock, so it instead asserts that a Get hitting
	// the primary returns the exact same ReadCloser semantics as a
	// direct primary.Get call.
	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	payload := []byte("primary hit")
	info, err := primary.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s, _ := New(primary, upstream)
	rc, err := s.Get(ctx, info.Hash)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("primary hit returned wrong content")
	}

	// Upstream must not have received a spurious promotion write
	// (we only promote on upstream hits, not primary hits).
	if has, _ := upstream.Has(ctx, info.Hash); has {
		t.Fatalf("primary hit should not propagate upstream")
	}
}
