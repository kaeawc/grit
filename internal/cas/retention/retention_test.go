package retention

import (
	"context"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
)

func TestSweepAgeBased(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := cas.NewFilesystemStore(dir)
	ctx := context.Background()
	now := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

	oldProv := cas.Provenance{
		Source:    cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "old"}},
		CreatedAt: now.Add(-48 * time.Hour),
	}
	newProv := cas.Provenance{
		Source:    cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "new"}},
		CreatedAt: now.Add(-1 * time.Hour),
	}

	_, err := store.PutBytes(ctx, []byte("old-data"), oldProv)
	if err != nil {
		t.Fatalf("put old: %v", err)
	}
	_, err = store.PutBytes(ctx, []byte("new-data"), newProv)
	if err != nil {
		t.Fatalf("put new: %v", err)
	}

	policy := Policy{MaxAge: 24 * time.Hour}
	report, err := Sweep(ctx, store, policy, now)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if report.BlobsRemoved != 1 {
		t.Errorf("BlobsRemoved = %d, want 1", report.BlobsRemoved)
	}
	if report.BytesFreed != int64(len("old-data")) {
		t.Errorf("BytesFreed = %d, want %d", report.BytesFreed, len("old-data"))
	}

	// The new blob should still exist.
	blobs, err := store.ListBlobs(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("remaining blobs = %d, want 1", len(blobs))
	}
}

func TestSweepSizeBased(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := cas.NewFilesystemStore(dir)
	ctx := context.Background()
	now := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

	for i, data := range []string{"aaaa", "bbbb", "cccc"} {
		prov := cas.Provenance{
			Source:    cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: data}},
			CreatedAt: now.Add(time.Duration(-3+i) * time.Hour),
		}
		if _, err := store.PutBytes(ctx, []byte(data), prov); err != nil {
			t.Fatalf("put %s: %v", data, err)
		}
	}

	// Total is 12 bytes. MaxSize 8 should evict the oldest until <=8.
	policy := Policy{MaxSize: 8}
	report, err := Sweep(ctx, store, policy, now)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if report.BlobsRemoved != 1 {
		t.Errorf("BlobsRemoved = %d, want 1", report.BlobsRemoved)
	}
	if report.BytesFreed != 4 {
		t.Errorf("BytesFreed = %d, want 4", report.BytesFreed)
	}

	blobs, err := store.ListBlobs(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(blobs) != 2 {
		t.Fatalf("remaining blobs = %d, want 2", len(blobs))
	}
}

func TestSweepNoPolicy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := cas.NewFilesystemStore(dir)
	ctx := context.Background()
	now := time.Now()

	prov := cas.Provenance{
		Source:    cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "keep"}},
		CreatedAt: now,
	}
	if _, err := store.PutBytes(ctx, []byte("keep-me"), prov); err != nil {
		t.Fatalf("put: %v", err)
	}

	report, err := Sweep(ctx, store, Policy{}, now)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if report.BlobsRemoved != 0 {
		t.Errorf("BlobsRemoved = %d, want 0", report.BlobsRemoved)
	}
}
