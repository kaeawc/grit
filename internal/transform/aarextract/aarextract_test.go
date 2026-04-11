package aarextract

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func TestExtractProducesClassesJarAndManifest(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	classesBody := []byte("fake classes.jar bytes")
	manifestBody := []byte(`<?xml version="1.0"?><manifest/>`)
	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":            classesBody,
		"AndroidManifest.xml":    manifestBody,
		"res/values/strings.xml": []byte(`<resources><string name="app">x</string></resources>`),
	})

	aarInfo, err := store.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{
			Kind:   cas.SourceImport,
			Import: &cas.ImportSource{Note: "aar test fixture"},
		},
	})
	if err != nil {
		t.Fatalf("PutBytes aar: %v", err)
	}

	result, err := Extract(ctx, store, aarInfo.Hash)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	classesOut, ok := result.Output(RoleClassesJar)
	if !ok {
		t.Fatalf("classes-jar output missing")
	}
	if classesOut.Blob.Hash != cas.HashBytes(classesBody) {
		t.Fatalf("classes.jar hash mismatch: got %s want %s", classesOut.Blob.Hash, cas.HashBytes(classesBody))
	}

	manifestOut, ok := result.Output(RoleAndroidManifest)
	if !ok {
		t.Fatalf("android-manifest output missing")
	}
	if manifestOut.Blob.Hash != cas.HashBytes(manifestBody) {
		t.Fatalf("manifest hash mismatch")
	}

	// Verify the output blobs are actually retrievable from the store.
	for _, out := range result.Outputs {
		rc, err := store.Get(ctx, out.Blob.Hash)
		if err != nil {
			t.Fatalf("Get %s: %v", out.Role, err)
		}
		got, _ := io.ReadAll(rc)
		_ = rc.Close()
		var want []byte
		switch out.Role {
		case RoleClassesJar:
			want = classesBody
		case RoleAndroidManifest:
			want = manifestBody
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("blob content mismatch for role %s", out.Role)
		}
	}
}

func TestExtractVerifiesTransformProvenance(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	classesBody := []byte("classes")
	aarBytes := buildAAR(t, map[string][]byte{"classes.jar": classesBody})
	aarInfo, err := store.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "t"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	result, err := Extract(ctx, store, aarInfo.Hash)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	out, ok := result.Output(RoleClassesJar)
	if !ok {
		t.Fatalf("classes-jar missing")
	}
	prov, err := store.Provenance(ctx, out.Blob.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Kind != cas.SourceTransform {
		t.Fatalf("expected transform source, got %s", prov.Source.Kind)
	}
	if prov.Source.Transform == nil {
		t.Fatalf("transform source record missing")
	}
	if prov.Source.Transform.ActionHash != result.ActionHash {
		t.Fatalf("provenance action hash mismatch")
	}
	if prov.Source.Transform.ActionKind != Kind {
		t.Fatalf("provenance action kind mismatch: %s", prov.Source.Transform.ActionKind)
	}
	if len(prov.Source.Transform.Inputs) != 1 || prov.Source.Transform.Inputs[0].Hash != aarInfo.Hash {
		t.Fatalf("provenance inputs not recorded: %+v", prov.Source.Transform.Inputs)
	}
	if prov.Attributes["output.role"] != RoleClassesJar {
		t.Fatalf("output.role attribute missing: %+v", prov.Attributes)
	}
}

func TestExtractCachedByActionHash(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":         []byte("classes"),
		"AndroidManifest.xml": []byte("manifest"),
	})
	aarInfo, err := store.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "t"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	first, err := Extract(ctx, store, aarInfo.Hash)
	if err != nil {
		t.Fatalf("first Extract: %v", err)
	}

	// Independently confirm the result was written to the action-result
	// index and matches what the second call returns.
	cached, err := store.GetActionResult(ctx, first.ActionHash)
	if err != nil {
		t.Fatalf("GetActionResult: %v", err)
	}
	if cached.ActionHash != first.ActionHash {
		t.Fatalf("cached action hash mismatch")
	}

	second, err := Extract(ctx, store, aarInfo.Hash)
	if err != nil {
		t.Fatalf("second Extract: %v", err)
	}
	if second.ActionHash != first.ActionHash {
		t.Fatalf("action hash not stable across calls")
	}
	if len(second.Outputs) != len(first.Outputs) {
		t.Fatalf("output count mismatch")
	}
	for i := range first.Outputs {
		if first.Outputs[i] != second.Outputs[i] {
			t.Fatalf("output %d mismatch: %+v vs %+v", i, first.Outputs[i], second.Outputs[i])
		}
	}
}

func TestExtractActionHashDiffersOnInput(t *testing.T) {
	a := Action(cas.HashBytes([]byte("one")))
	b := Action(cas.HashBytes([]byte("two")))
	if a.Hash() == b.Hash() {
		t.Fatalf("different AAR hashes must produce different action hashes")
	}
}

func TestExtractRejectsNonZipInput(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	garbage := []byte("not a zip file")
	info, err := store.PutBytes(ctx, garbage, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "garbage"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	if _, err := Extract(ctx, store, info.Hash); err == nil {
		t.Fatalf("expected error for non-zip input")
	}
}

func TestExtractMissingInputBlob(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	_, err := Extract(ctx, store, cas.HashBytes([]byte("never stored")))
	if err == nil {
		t.Fatalf("expected error for missing input blob")
	}
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected wrapped ErrNotFound, got %v", err)
	}
}

func TestExtractIgnoresUnknownZipEntries(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":         []byte("classes"),
		"AndroidManifest.xml": []byte("manifest"),
		"proguard.txt":        []byte("keep class *;"),
		"R.txt":               []byte("int id foo 0x7f010000"),
	})
	info, err := store.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "t"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	result, err := Extract(ctx, store, info.Hash)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Outputs) != 2 {
		t.Fatalf("expected exactly 2 outputs, got %d", len(result.Outputs))
	}
}

func TestExtractHandlesAARWithOnlyClassesJar(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar": []byte("only classes"),
	})
	info, err := store.PutBytes(ctx, aarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "t"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	result, err := Extract(ctx, store, info.Hash)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, ok := result.Output(RoleClassesJar); !ok {
		t.Fatalf("classes-jar missing")
	}
	if _, ok := result.Output(RoleAndroidManifest); ok {
		t.Fatalf("android-manifest should not be present")
	}
}

// buildAAR produces a minimal zip file with the named entries for use as
// a test fixture. The AAR format is just a zip archive by convention, so
// the test does not need to reproduce the full Android build-tool output.
func buildAAR(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip.Create %s: %v", name, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("zip.Write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	return buf.Bytes()
}
