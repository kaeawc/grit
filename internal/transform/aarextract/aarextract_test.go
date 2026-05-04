package aarextract

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
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

	if _, ok := result.Output(RoleResourceTree); !ok {
		t.Fatalf("resource-tree output missing")
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
		case RoleResourceTree:
			assertNormalizedResourceTreeZip(t, got, []resourceEntry{
				{Name: "res/values/strings.xml", Body: []byte(`<resources><string name="app">x</string></resources>`)},
			})
			continue
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
	if _, ok := result.Output(RoleResourceTree); ok {
		t.Fatalf("resource-tree should not be present without res entries")
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
	if _, ok := result.Output(RoleResourceTree); ok {
		t.Fatalf("resource-tree should not be present")
	}
}

func TestExtractProducesDeterministicResourceTreeZip(t *testing.T) {
	store := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	entriesA := []resourceEntry{
		{Name: "res/layout/main.xml", Body: []byte(`<LinearLayout/>`)},
		{Name: "res/values/strings.xml", Body: []byte(`<resources><string name="app">x</string></resources>`)},
	}
	entriesB := []resourceEntry{
		{Name: "res/values/strings.xml", Body: []byte(`<resources><string name="app">x</string></resources>`)},
		{Name: "res/layout/main.xml", Body: []byte(`<LinearLayout/>`)},
	}

	firstInfo, err := store.PutBytes(ctx, buildAARFromEntries(t, append([]resourceEntry{{Name: "classes.jar", Body: []byte("classes")}}, entriesA...)), cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "first"}},
	})
	if err != nil {
		t.Fatalf("PutBytes first aar: %v", err)
	}
	secondInfo, err := store.PutBytes(ctx, buildAARFromEntries(t, append([]resourceEntry{{Name: "classes.jar", Body: []byte("classes")}}, entriesB...)), cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "second"}},
	})
	if err != nil {
		t.Fatalf("PutBytes second aar: %v", err)
	}

	firstResult, err := Extract(ctx, store, firstInfo.Hash)
	if err != nil {
		t.Fatalf("first Extract: %v", err)
	}
	secondResult, err := Extract(ctx, store, secondInfo.Hash)
	if err != nil {
		t.Fatalf("second Extract: %v", err)
	}

	firstTree, ok := firstResult.Output(RoleResourceTree)
	if !ok {
		t.Fatalf("first resource-tree output missing")
	}
	secondTree, ok := secondResult.Output(RoleResourceTree)
	if !ok {
		t.Fatalf("second resource-tree output missing")
	}
	if firstTree.Blob.Hash != secondTree.Blob.Hash {
		t.Fatalf("expected deterministic resource-tree hash, got %s and %s", firstTree.Blob.Hash, secondTree.Blob.Hash)
	}
}

// buildAAR produces a minimal zip file with the named entries for use as
// a test fixture. The AAR format is just a zip archive by convention, so
// the test does not need to reproduce the full Android build-tool output.
func buildAAR(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	ordered := make([]resourceEntry, 0, len(entries))
	for name, body := range entries {
		ordered = append(ordered, resourceEntry{Name: name, Body: body})
	}
	slices.SortFunc(ordered, func(a, b resourceEntry) int {
		return bytes.Compare([]byte(a.Name), []byte(b.Name))
	})
	return buildAARFromEntries(t, ordered)
}

type resourceEntry struct {
	Name string
	Body []byte
}

func buildAARFromEntries(t *testing.T, entries []resourceEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range entries {
		w, err := zw.Create(entry.Name)
		if err != nil {
			t.Fatalf("zip.Create %s: %v", entry.Name, err)
		}
		if _, err := w.Write(entry.Body); err != nil {
			t.Fatalf("zip.Write %s: %v", entry.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	return buf.Bytes()
}

func assertNormalizedResourceTreeZip(t *testing.T, data []byte, want []resourceEntry) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open normalized resource zip: %v", err)
	}
	if len(zr.File) != len(want) {
		t.Fatalf("resource entry count mismatch: got %d want %d", len(zr.File), len(want))
	}
	for i, file := range zr.File {
		if file.Name != want[i].Name {
			t.Fatalf("resource entry order mismatch at %d: got %s want %s", i, file.Name, want[i].Name)
		}
		if file.Method != zip.Store {
			t.Fatalf("resource entry method mismatch for %s: got %d want %d", file.Name, file.Method, zip.Store)
		}
		if !file.Modified.Equal(reproducibleZipTime) {
			t.Fatalf("resource entry time mismatch for %s: got %s want %s", file.Name, file.Modified.UTC(), reproducibleZipTime)
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open resource entry %s: %v", file.Name, err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read resource entry %s: %v", file.Name, err)
		}
		if !bytes.Equal(body, want[i].Body) {
			t.Fatalf("resource entry body mismatch for %s", file.Name)
		}
	}
}
