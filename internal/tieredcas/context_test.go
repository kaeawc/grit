package tieredcas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func TestWithMaxTierZeroIsNoOp(t *testing.T) {
	ctx := WithMaxTier(context.Background(), 0)
	if mt := maxTierFromContext(ctx); mt != 0 {
		t.Fatalf("expected 0, got %d", mt)
	}
}

func TestWithMaxTierNegativeIsNoOp(t *testing.T) {
	ctx := WithMaxTier(context.Background(), -1)
	if mt := maxTierFromContext(ctx); mt != 0 {
		t.Fatalf("expected 0, got %d", mt)
	}
}

func TestWithMaxTierCarriesValue(t *testing.T) {
	ctx := WithMaxTier(context.Background(), 2)
	if mt := maxTierFromContext(ctx); mt != 2 {
		t.Fatalf("expected 2, got %d", mt)
	}
}

func TestWithLocalOnlySetsLocalTierCount(t *testing.T) {
	ctx := WithLocalOnly(context.Background())
	mt := maxTierFromContext(ctx)
	if mt != LocalTierCount {
		t.Fatalf("expected %d, got %d", LocalTierCount, mt)
	}
}

func TestGetRespectsContextMaxTier(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	shared := cas.NewFilesystemStore(t.TempDir())
	remote := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	// Seed only the remote tier.
	payload := []byte("remote-only via context")
	info, err := remote.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := New(primary, shared, remote)
	if err != nil {
		t.Fatal(err)
	}

	// With context MaxTier=2, Get should miss (remote at tier 2 is skipped).
	ctxLocal := WithMaxTier(ctx, 2)
	_, err = s.Get(ctxLocal, info.Hash)
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound with context MaxTier=2, got %v", err)
	}

	// Without context limit, Get should hit the remote tier.
	rc, err := s.Get(ctx, info.Hash)
	if err != nil {
		t.Fatalf("expected hit without context limit, got %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch")
	}
}

func TestGetActionResultRespectsContextMaxTier(t *testing.T) {
	primary := cas.NewFilesystemStore(t.TempDir())
	shared := cas.NewFilesystemStore(t.TempDir())
	remote := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	actionHash := cas.HashBytes([]byte("action-ctx"))
	result := cas.ActionResult{
		ActionHash: actionHash,
		Outputs: []cas.NamedOutput{
			{Role: "out", Blob: cas.BlobInfo{Hash: cas.HashBytes([]byte("output")), Size: 6}},
		},
	}
	if err := remote.PutActionResult(ctx, result); err != nil {
		t.Fatal(err)
	}

	s, err := New(primary, shared, remote)
	if err != nil {
		t.Fatal(err)
	}

	// With WithLocalOnly, GetActionResult should miss.
	ctxLocal := WithLocalOnly(ctx)
	_, err = s.GetActionResult(ctxLocal, actionHash)
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound with local-only context, got %v", err)
	}

	// Without context limit, GetActionResult should hit.
	loaded, err := s.GetActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("expected hit without context limit, got %v", err)
	}
	if loaded.ActionHash != actionHash {
		t.Fatalf("action hash mismatch")
	}
}

func TestWithLocalOnlyGetSkipsRemoteForSingleLocalTier(t *testing.T) {
	// When there's only one tier (local), WithLocalOnly should still work
	// without panicking or misbehaving.
	primary := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	payload := []byte("local content")
	info, err := primary.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := New(primary)
	if err != nil {
		t.Fatal(err)
	}

	ctxLocal := WithLocalOnly(ctx)
	rc, err := s.Get(ctxLocal, info.Hash)
	if err != nil {
		t.Fatalf("expected hit on single local tier, got %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("content mismatch")
	}
}
