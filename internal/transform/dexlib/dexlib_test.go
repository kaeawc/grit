package dexlib

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func TestActionHashStable(t *testing.T) {
	jarHash := cas.HashBytes([]byte("some-library.jar"))
	d8Ver := "8.4.35"

	a := Action(jarHash, d8Ver)
	b := Action(jarHash, d8Ver)
	if a.Hash() != b.Hash() {
		t.Fatalf("same inputs must produce same action hash: %s vs %s", a.Hash(), b.Hash())
	}
}

func TestActionHashDiffersOnJARInput(t *testing.T) {
	d8Ver := "8.4.35"
	a := Action(cas.HashBytes([]byte("lib-a.jar")), d8Ver)
	b := Action(cas.HashBytes([]byte("lib-b.jar")), d8Ver)
	if a.Hash() == b.Hash() {
		t.Fatalf("different JAR hashes must produce different action hashes")
	}
}

func TestActionHashDiffersOnD8Version(t *testing.T) {
	jarHash := cas.HashBytes([]byte("same.jar"))
	a := Action(jarHash, "8.4.35")
	b := Action(jarHash, "8.5.0")
	if a.Hash() == b.Hash() {
		t.Fatalf("different d8 versions must produce different action hashes")
	}
}

func TestDexProducesSingleOutput(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	jarBody := []byte("fake jar bytes")
	dexBody := []byte("dex\n035\x00fake dex content")
	jarInfo, err := store.PutBytes(ctx, jarBody, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "test jar"}},
	})
	if err != nil {
		t.Fatalf("PutBytes jar: %v", err)
	}

	fakeDex := func(jar []byte) ([][]byte, error) {
		if !bytes.Equal(jar, jarBody) {
			t.Fatalf("dexBytes received wrong input")
		}
		return [][]byte{dexBody}, nil
	}

	result, err := Dex(ctx, store, jarInfo.Hash, "8.4.35", fakeDex)
	if err != nil {
		t.Fatalf("Dex: %v", err)
	}

	if len(result.Outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(result.Outputs))
	}
	if result.Outputs[0].Role != "dex-0" {
		t.Fatalf("expected role dex-0, got %s", result.Outputs[0].Role)
	}

	// Verify blob content.
	rc, err := store.Get(ctx, result.Outputs[0].Blob.Hash)
	if err != nil {
		t.Fatalf("Get dex blob: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, dexBody) {
		t.Fatalf("dex blob content mismatch")
	}
}

func TestDexProducesMultiplePartitions(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	jarBody := []byte("big jar bytes")
	jarInfo, err := store.PutBytes(ctx, jarBody, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "test"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	dex0 := []byte("classes.dex")
	dex1 := []byte("classes2.dex")
	fakeDex := func(jar []byte) ([][]byte, error) {
		return [][]byte{dex0, dex1}, nil
	}

	result, err := Dex(ctx, store, jarInfo.Hash, "8.4.35", fakeDex)
	if err != nil {
		t.Fatalf("Dex: %v", err)
	}
	if len(result.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(result.Outputs))
	}
	if result.Outputs[0].Role != "dex-0" || result.Outputs[1].Role != "dex-1" {
		t.Fatalf("unexpected roles: %s, %s", result.Outputs[0].Role, result.Outputs[1].Role)
	}
}

func TestDexCachedByActionHash(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	jarBody := []byte("cached jar")
	jarInfo, err := store.PutBytes(ctx, jarBody, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "test"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	calls := 0
	fakeDex := func(jar []byte) ([][]byte, error) {
		calls++
		return [][]byte{[]byte("dex output")}, nil
	}

	first, err := Dex(ctx, store, jarInfo.Hash, "8.4.35", fakeDex)
	if err != nil {
		t.Fatalf("first Dex: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 dex call, got %d", calls)
	}

	second, err := Dex(ctx, store, jarInfo.Hash, "8.4.35", fakeDex)
	if err != nil {
		t.Fatalf("second Dex: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected dex function not called on cache hit, got %d calls", calls)
	}
	if second.ActionHash != first.ActionHash {
		t.Fatalf("action hash not stable across calls")
	}
	if len(second.Outputs) != len(first.Outputs) {
		t.Fatalf("output count mismatch")
	}
	for i := range first.Outputs {
		if first.Outputs[i] != second.Outputs[i] {
			t.Fatalf("output %d mismatch", i)
		}
	}
}

func TestDexVerifiesProvenance(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	jarBody := []byte("provenance jar")
	jarInfo, err := store.PutBytes(ctx, jarBody, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "test"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	fakeDex := func(jar []byte) ([][]byte, error) {
		return [][]byte{[]byte("dex")}, nil
	}

	result, err := Dex(ctx, store, jarInfo.Hash, "8.4.35", fakeDex)
	if err != nil {
		t.Fatalf("Dex: %v", err)
	}

	prov, err := store.Provenance(ctx, result.Outputs[0].Blob.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Kind != cas.SourceTransform {
		t.Fatalf("expected transform source, got %s", prov.Source.Kind)
	}
	ts := prov.Source.Transform
	if ts == nil {
		t.Fatalf("transform source record missing")
	}
	if ts.ActionHash != result.ActionHash {
		t.Fatalf("provenance action hash mismatch")
	}
	if ts.ActionKind != Kind {
		t.Fatalf("provenance action kind: got %s want %s", ts.ActionKind, Kind)
	}
	if ts.Tool != Tool {
		t.Fatalf("provenance tool: got %s want %s", ts.Tool, Tool)
	}
	if len(ts.Inputs) != 1 || ts.Inputs[0].Hash != jarInfo.Hash {
		t.Fatalf("provenance inputs not recorded")
	}
	if prov.Attributes["output.role"] != "dex-0" {
		t.Fatalf("output.role attribute: got %s want dex-0", prov.Attributes["output.role"])
	}
}

func TestDexMissingInputBlob(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	fakeDex := func(jar []byte) ([][]byte, error) {
		t.Fatalf("dexBytes should not be called for missing blob")
		return nil, nil
	}

	_, err := Dex(ctx, store, cas.HashBytes([]byte("never stored")), "8.4.35", fakeDex)
	if err == nil {
		t.Fatalf("expected error for missing input blob")
	}
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected wrapped ErrNotFound, got %v", err)
	}
}

func TestDexHandlesD8Error(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	jarBody := []byte("error jar")
	jarInfo, err := store.PutBytes(ctx, jarBody, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "test"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	fakeDex := func(jar []byte) ([][]byte, error) {
		return nil, errors.New("d8: unsupported class file version 65")
	}

	_, err = Dex(ctx, store, jarInfo.Hash, "8.4.35", fakeDex)
	if err == nil {
		t.Fatalf("expected error from d8 failure")
	}
}

func TestDexRejectsEmptyOutput(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	jarBody := []byte("empty output jar")
	jarInfo, err := store.PutBytes(ctx, jarBody, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "test"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	fakeDex := func(jar []byte) ([][]byte, error) {
		return [][]byte{}, nil
	}

	_, err = Dex(ctx, store, jarInfo.Hash, "8.4.35", fakeDex)
	if err == nil {
		t.Fatalf("expected error for empty dex output")
	}
}
