package cas

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRebuildIndexesRestoresMissingProvenance(t *testing.T) {
	dir := t.TempDir()
	s := NewFilesystemStore(dir)
	ctx := context.Background()

	info, err := s.PutBytes(ctx, []byte("payload"), Provenance{
		Source: Source{Kind: SourceImport, Import: &ImportSource{Note: "original"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	if err := os.Remove(s.provenancePath(info.Hash)); err != nil {
		t.Fatalf("remove provenance: %v", err)
	}

	report, err := RebuildIndexes(ctx, s)
	if err != nil {
		t.Fatalf("RebuildIndexes: %v", err)
	}
	if report.BlobsScanned != 1 || report.ProvenanceRestored != 1 || report.ProvenanceUnchanged != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}

	prov, err := s.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Kind != SourceImport || prov.Source.Import == nil || prov.Source.Import.Note != "rebuilt-by-RebuildIndexes" {
		t.Fatalf("expected stub provenance, got %+v", prov)
	}
	if prov.CreatedAt.IsZero() {
		t.Fatalf("expected non-zero CreatedAt from blob mtime")
	}
}

func TestRebuildIndexesPreservesExistingProvenance(t *testing.T) {
	dir := t.TempDir()
	s := NewFilesystemStore(dir)
	ctx := context.Background()

	info, err := s.PutBytes(ctx, []byte("keep-me"), Provenance{
		Source: Source{Kind: SourceImport, Import: &ImportSource{Note: "original"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	report, err := RebuildIndexes(ctx, s)
	if err != nil {
		t.Fatalf("RebuildIndexes: %v", err)
	}
	if report.ProvenanceRestored != 0 {
		t.Fatalf("expected no restorations, got %+v", report)
	}
	if report.ProvenanceUnchanged != 1 {
		t.Fatalf("expected 1 unchanged provenance, got %+v", report)
	}
	prov, err := s.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Import == nil || prov.Source.Import.Note != "original" {
		t.Fatalf("provenance was overwritten: %+v", prov)
	}
}

func TestRebuildIndexesEmptyStore(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	report, err := RebuildIndexes(context.Background(), s)
	if err != nil {
		t.Fatalf("RebuildIndexes: %v", err)
	}
	if report.BlobsScanned != 0 {
		t.Fatalf("expected zero scanned, got %+v", report)
	}
}

func TestRebuildIndexesRejectsMalformedBlobPath(t *testing.T) {
	dir := t.TempDir()
	s := NewFilesystemStore(dir)
	bogus := filepath.Join(dir, "blobs", "zz", "not-a-hash")
	if err := os.MkdirAll(filepath.Dir(bogus), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bogus, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := RebuildIndexes(context.Background(), s)
	if err == nil {
		t.Fatalf("expected error for malformed blob path, got nil")
	}
}

func TestRebuildIndexesNilStore(t *testing.T) {
	_, err := RebuildIndexes(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil store")
	}
}
