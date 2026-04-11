package cas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestHashBytesDeterministic(t *testing.T) {
	a := HashBytes([]byte("hello"))
	b := HashBytes([]byte("hello"))
	if a != b {
		t.Fatalf("HashBytes not deterministic: %s vs %s", a, b)
	}
	if a.IsZero() {
		t.Fatalf("HashBytes returned zero hash for non-empty input")
	}
}

func TestHashHexRoundTrip(t *testing.T) {
	original := HashBytes([]byte("round trip"))
	parsed, err := ParseHash(original.String())
	if err != nil {
		t.Fatalf("ParseHash: %v", err)
	}
	if parsed != original {
		t.Fatalf("hash mismatch: %s vs %s", parsed, original)
	}
}

func TestHashParseRejectsBadInput(t *testing.T) {
	if _, err := ParseHash("not hex"); err == nil {
		t.Fatalf("expected error for non-hex input")
	}
	if _, err := ParseHash("abcd"); err == nil {
		t.Fatalf("expected error for short input")
	}
	if _, err := ParseHash("zz" + hex63()); err == nil {
		t.Fatalf("expected error for invalid hex characters")
	}
}

func TestHashReaderMatchesHashBytes(t *testing.T) {
	payload := []byte("hash via reader")
	h, n, err := HashReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("HashReader: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("HashReader size mismatch: got %d want %d", n, len(payload))
	}
	if h != HashBytes(payload) {
		t.Fatalf("HashReader and HashBytes disagree")
	}
}

func TestHashJSONRoundTrip(t *testing.T) {
	h := HashBytes([]byte("json"))
	encoded, err := h.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var decoded Hash
	if err := decoded.UnmarshalJSON(encoded); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if decoded != h {
		t.Fatalf("JSON round trip mismatch: %s vs %s", decoded, h)
	}
}

func TestFilesystemStorePutGetRoundTrip(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	payload := []byte("the quick brown fox")
	prov := Provenance{
		Source: Source{
			Kind:   SourceImport,
			Import: &ImportSource{Note: "test fixture"},
		},
	}

	info, err := s.PutBytes(ctx, payload, prov)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	if info.Hash != HashBytes(payload) {
		t.Fatalf("returned hash does not match payload: got %s", info.Hash)
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("size mismatch: got %d want %d", info.Size, len(payload))
	}

	rc, err := s.Get(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	if closeErr := rc.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, payload)
	}

	statInfo, err := s.Stat(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if statInfo != info {
		t.Fatalf("Stat mismatch: got %+v want %+v", statInfo, info)
	}

	has, err := s.Has(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !has {
		t.Fatalf("expected Has to report stored blob")
	}
}

func TestFilesystemStoreProvenancePersisted(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	prov := Provenance{
		Source: Source{
			Kind: SourceDownload,
			Download: &DownloadSource{
				Downloader:   "maven",
				RepositoryID: "central",
				URL:          "https://repo.maven.apache.org/maven2/foo/bar/1.0/bar-1.0.jar",
				Coordinate:   "foo:bar:1.0",
			},
		},
		Attributes: map[string]string{"classifier": "sources"},
	}
	info, err := s.PutBytes(ctx, []byte("artifact"), prov)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	loaded, err := s.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if loaded.Source.Kind != SourceDownload {
		t.Fatalf("unexpected kind: %s", loaded.Source.Kind)
	}
	if loaded.Source.Download == nil || loaded.Source.Download.Coordinate != "foo:bar:1.0" {
		t.Fatalf("download source not preserved: %+v", loaded.Source.Download)
	}
	if loaded.Attributes["classifier"] != "sources" {
		t.Fatalf("attributes not preserved: %+v", loaded.Attributes)
	}
	if loaded.CreatedAt.IsZero() {
		t.Fatalf("expected CreatedAt to be populated")
	}
}

func TestFilesystemStoreIdempotentPutPreservesFirstProvenance(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	payload := []byte("idempotent payload")

	firstProv := Provenance{
		Source: Source{Kind: SourceImport, Import: &ImportSource{Note: "first"}},
	}
	first, err := s.PutBytes(ctx, payload, firstProv)
	if err != nil {
		t.Fatalf("first PutBytes: %v", err)
	}

	secondProv := Provenance{
		Source: Source{Kind: SourceImport, Import: &ImportSource{Note: "second"}},
	}
	second, err := s.PutBytes(ctx, payload, secondProv)
	if err != nil {
		t.Fatalf("second PutBytes: %v", err)
	}

	if first != second {
		t.Fatalf("repeated PutBytes produced different BlobInfo: %+v vs %+v", first, second)
	}

	loaded, err := s.Provenance(ctx, first.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if loaded.Source.Import == nil || loaded.Source.Import.Note != "first" {
		t.Fatalf("first-writer provenance not preserved: %+v", loaded.Source.Import)
	}
}

func TestFilesystemStoreGetNotFound(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	missing := HashBytes([]byte("never stored"))

	if _, err := s.Get(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from Get, got %v", err)
	}
	if _, err := s.Stat(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from Stat, got %v", err)
	}
	if _, err := s.Provenance(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from Provenance, got %v", err)
	}
	has, err := s.Has(ctx, missing)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if has {
		t.Fatalf("expected Has to be false for missing blob")
	}
}

func TestFilesystemStoreInjectedClock(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	fixed := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }
	ctx := context.Background()
	info, err := s.PutBytes(ctx, []byte("fixed time"), Provenance{
		Source: Source{Kind: SourceImport, Import: &ImportSource{Note: "t"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	prov, err := s.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if !prov.CreatedAt.Equal(fixed) {
		t.Fatalf("CreatedAt not injected: %s", prov.CreatedAt)
	}
}

func TestFilesystemStoreTransformSourceRoundTrip(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	inputHash := HashBytes([]byte("input"))
	actionHash := HashBytes([]byte("action"))
	prov := Provenance{
		Source: Source{
			Kind: SourceTransform,
			Transform: &TransformSource{
				ActionHash:  actionHash,
				ActionKind:  "aar-extract",
				Tool:        "grit-aar-extract",
				ToolVersion: "test",
				Inputs: []TransformInput{
					{Role: "aar", Hash: inputHash},
				},
			},
		},
	}
	info, err := s.PutBytes(ctx, []byte("classes-jar-bytes"), prov)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	loaded, err := s.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if loaded.Source.Transform == nil {
		t.Fatalf("transform source missing")
	}
	if loaded.Source.Transform.ActionHash != actionHash {
		t.Fatalf("action hash not preserved")
	}
	if len(loaded.Source.Transform.Inputs) != 1 || loaded.Source.Transform.Inputs[0].Hash != inputHash {
		t.Fatalf("inputs not preserved: %+v", loaded.Source.Transform.Inputs)
	}
}

func TestFilesystemStorePutExpectedMatching(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	payload := []byte("expected match")
	expected := HashBytes(payload)

	info, err := s.PutBytesExpected(ctx, payload, expected, Provenance{
		Source: Source{Kind: SourceImport, Import: &ImportSource{Note: "expected"}},
	})
	if err != nil {
		t.Fatalf("PutBytesExpected: %v", err)
	}
	if info.Hash != expected {
		t.Fatalf("hash mismatch: got %s want %s", info.Hash, expected)
	}
	has, err := s.Has(ctx, expected)
	if err != nil || !has {
		t.Fatalf("expected blob present: has=%v err=%v", has, err)
	}
}

func TestFilesystemStorePutExpectedMismatchLeavesNoTrace(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	payload := []byte("real content")
	wrong := HashBytes([]byte("claimed content"))

	_, err := s.PutBytesExpected(ctx, payload, wrong, Provenance{
		Source: Source{Kind: SourceImport, Import: &ImportSource{Note: "wrong"}},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}

	// Neither the wrong hash nor the real hash should be present. The
	// wrong hash because we never wrote anything under it; the real hash
	// because PutExpected rejects the byte stream before committing.
	if has, _ := s.Has(ctx, wrong); has {
		t.Fatalf("wrong hash must not be present after mismatch")
	}
	if has, _ := s.Has(ctx, HashBytes(payload)); has {
		t.Fatalf("real hash must not be present after mismatch")
	}
	if _, err := s.Provenance(ctx, HashBytes(payload)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no provenance should be written on mismatch, got %v", err)
	}
}

func TestFilesystemStoreActionResultRoundTrip(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	outputBytes := []byte("output blob")
	outputInfo, err := s.PutBytes(ctx, outputBytes, Provenance{
		Source: Source{Kind: SourceImport, Import: &ImportSource{Note: "output"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	actionHash := HashBytes([]byte("action identity"))
	result := ActionResult{
		ActionHash: actionHash,
		Outputs: []NamedOutput{
			{Role: "classes-jar", Blob: outputInfo},
		},
	}
	if err := s.PutActionResult(ctx, result); err != nil {
		t.Fatalf("PutActionResult: %v", err)
	}
	loaded, err := s.GetActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("GetActionResult: %v", err)
	}
	if loaded.ActionHash != actionHash {
		t.Fatalf("action hash mismatch: %s vs %s", loaded.ActionHash, actionHash)
	}
	out, ok := loaded.Output("classes-jar")
	if !ok {
		t.Fatalf("classes-jar output missing")
	}
	if out.Blob.Hash != outputInfo.Hash || out.Blob.Size != outputInfo.Size {
		t.Fatalf("output blob not preserved: %+v vs %+v", out.Blob, outputInfo)
	}
}

func TestFilesystemStoreGetActionResultNotFound(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	_, err := s.GetActionResult(ctx, HashBytes([]byte("missing")))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFilesystemStorePutActionResultRejectsZeroHash(t *testing.T) {
	s := NewFilesystemStore(t.TempDir())
	ctx := context.Background()
	if err := s.PutActionResult(ctx, ActionResult{}); err == nil {
		t.Fatalf("expected error for zero action hash")
	}
}

// hex63 returns a 63-character hex string, for constructing invalid-length
// or invalid-character inputs in parse tests.
func hex63() string {
	const valid = "0123456789abcdef"
	out := make([]byte, 63)
	for i := range out {
		out[i] = valid[i%len(valid)]
	}
	return string(out)
}
