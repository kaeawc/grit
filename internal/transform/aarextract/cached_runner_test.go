package aarextract

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/clock"
	"github.com/kaeawc/grit/internal/tieredcas"
)

// TestExtractHashRoundTripAcrossStores is the load-bearing determinism
// guarantee for the action-cache wiring: extracting the same AAR bytes
// against two independent CAS stores must produce byte-identical
// ActionResults (same action hash, same output blob hashes). Without
// this, no two builds ever hit each other's cache.
func TestExtractHashRoundTripAcrossStores(t *testing.T) {
	aarBytes := buildAAR(t, map[string][]byte{
		"AndroidManifest.xml": []byte(`<manifest package="com.example"/>`),
		"classes.jar":         []byte("classes-payload"),
		"res/values/strings.xml": []byte(`<resources><string name="app">x</string></resources>`),
		"res/drawable/icon.png":  []byte("icon-bytes"),
	})

	storeA := cas.NewFilesystemStore(t.TempDir())
	storeB := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	infoA, err := storeA.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "fixture"}},
	})
	if err != nil {
		t.Fatalf("PutBytes A: %v", err)
	}
	infoB, err := storeB.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "fixture"}},
	})
	if err != nil {
		t.Fatalf("PutBytes B: %v", err)
	}
	if infoA.Hash != infoB.Hash {
		t.Fatalf("input AAR hash differed across stores: A=%s B=%s", infoA.Hash, infoB.Hash)
	}

	resultA, err := Extract(ctx, storeA, infoA.Hash)
	if err != nil {
		t.Fatalf("Extract A: %v", err)
	}
	resultB, err := Extract(ctx, storeB, infoB.Hash)
	if err != nil {
		t.Fatalf("Extract B: %v", err)
	}

	if resultA.ActionHash != resultB.ActionHash {
		t.Fatalf("action hash differs across runs: A=%s B=%s", resultA.ActionHash, resultB.ActionHash)
	}
	if len(resultA.Outputs) != len(resultB.Outputs) {
		t.Fatalf("output count differs: A=%d B=%d", len(resultA.Outputs), len(resultB.Outputs))
	}
	byRoleA := outputsByRole(resultA)
	byRoleB := outputsByRole(resultB)
	for role, a := range byRoleA {
		b, ok := byRoleB[role]
		if !ok {
			t.Fatalf("role %q missing from B", role)
		}
		if a.Blob.Hash != b.Blob.Hash {
			t.Fatalf("output blob hash differs for role %q: A=%s B=%s", role, a.Blob.Hash, b.Blob.Hash)
		}
		if a.Blob.Size != b.Blob.Size {
			t.Fatalf("output blob size differs for role %q: A=%d B=%d", role, a.Blob.Size, b.Blob.Size)
		}
	}
}

func outputsByRole(r cas.ActionResult) map[string]cas.NamedOutput {
	out := make(map[string]cas.NamedOutput, len(r.Outputs))
	for _, o := range r.Outputs {
		out[o.Role] = o
	}
	return out
}

func newRunnerWithFakeNow(store *tieredcas.Store, policy tieredcas.UploadPolicy, fixed time.Time) *CachedRunner {
	return &CachedRunner{
		Store:        store,
		UploadPolicy: policy,
		Clock:        clock.NewFake(fixed),
	}
}

func TestCachedRunnerMissThenHit(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	store, err := tieredcas.New(primary)
	if err != nil {
		t.Fatalf("tieredcas.New: %v", err)
	}
	ctx := context.Background()
	aarBytes := buildAAR(t, map[string][]byte{"AndroidManifest.xml": []byte(`<manifest/>`)})
	info, err := primary.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "fixture"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	runner := newRunnerWithFakeNow(store, tieredcas.UploadPolicy{}, time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC))
	first, err := runner.Run(ctx, info.Hash)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if len(first.Outputs) == 0 {
		t.Fatal("expected outputs from miss path")
	}

	summary, err := primary.GetActionSummary(ctx, first.ActionHash)
	if err != nil {
		t.Fatalf("GetActionSummary after miss: %v", err)
	}
	if summary.Outcome != "miss" {
		t.Fatalf("expected miss outcome, got %q", summary.Outcome)
	}

	second, err := runner.Run(ctx, info.Hash)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if second.ActionHash != first.ActionHash {
		t.Fatalf("action hash drift: first=%s second=%s", first.ActionHash, second.ActionHash)
	}

	summary2, err := primary.GetActionSummary(ctx, second.ActionHash)
	if err != nil {
		t.Fatalf("GetActionSummary after hit: %v", err)
	}
	if summary2.Outcome != "hit" {
		t.Fatalf("expected hit outcome on second run, got %q", summary2.Outcome)
	}
}

func TestCachedRunnerPromotesUnderPolicy(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	shared := cas.NewFilesystemStore(t.TempDir())
	store, err := tieredcas.New(primary, shared)
	if err != nil {
		t.Fatalf("tieredcas.New: %v", err)
	}
	ctx := context.Background()
	aarBytes := buildAAR(t, map[string][]byte{"AndroidManifest.xml": []byte(`<manifest/>`)})
	info, err := primary.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "fixture"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	policy := tieredcas.UploadPolicy{AllowedKinds: []string{Kind}, MinTier: 1}
	runner := newRunnerWithFakeNow(store, policy, time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC))

	result, err := runner.Run(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := shared.GetActionResult(ctx, result.ActionHash); err != nil {
		t.Fatalf("expected action result promoted to shared tier: %v", err)
	}
	for _, out := range result.Outputs {
		if has, err := shared.Has(ctx, out.Blob.Hash); err != nil || !has {
			t.Fatalf("expected output blob %s promoted to shared tier: has=%v err=%v", out.Role, has, err)
		}
	}
}

func TestCachedRunnerSkipsPromotionWhenPolicyDenies(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	shared := cas.NewFilesystemStore(t.TempDir())
	store, err := tieredcas.New(primary, shared)
	if err != nil {
		t.Fatalf("tieredcas.New: %v", err)
	}
	ctx := context.Background()
	aarBytes := buildAAR(t, map[string][]byte{"AndroidManifest.xml": []byte(`<manifest/>`)})
	info, err := primary.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "fixture"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	runner := newRunnerWithFakeNow(store, tieredcas.UploadPolicy{}, time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC))

	result, err := runner.Run(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := shared.GetActionResult(ctx, result.ActionHash); !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("zero-value policy should deny promotion, expected ErrNotFound, got %v", err)
	}
}

func TestCachedRunnerNilStoreReturnsError(t *testing.T) {
	r := &CachedRunner{}
	_, err := r.Run(context.Background(), cas.HashBytes([]byte("x")))
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}

func TestCachedRunnerSurfacesContextCancellation(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	store, err := tieredcas.New(primary)
	if err != nil {
		t.Fatalf("tieredcas.New: %v", err)
	}
	runner := newRunnerWithFakeNow(store, tieredcas.UploadPolicy{}, time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, cas.HashBytes([]byte("x")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
