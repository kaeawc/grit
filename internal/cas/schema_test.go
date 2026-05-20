package cas

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeEnvelopeStampsSchemaVersion(t *testing.T) {
	encoded, err := encodeEnvelope(Provenance{Source: Source{Kind: SourceDownload}})
	if err != nil {
		t.Fatalf("encodeEnvelope: %v", err)
	}
	var env onDiskEnvelope
	if err := json.Unmarshal(encoded, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.SchemaVersion != SchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", env.SchemaVersion, SchemaVersion)
	}
	if len(env.Payload) == 0 {
		t.Fatalf("envelope payload is empty")
	}
}

func TestDecodeEnvelopeRoundTrip(t *testing.T) {
	original := ActionResult{
		ActionHash: HashBytes([]byte("hi")),
		Outputs: []NamedOutput{{
			Role: "classes",
			Blob: BlobInfo{Hash: HashBytes([]byte("blob")), Size: 7},
		}},
	}
	encoded, err := encodeEnvelope(original)
	if err != nil {
		t.Fatalf("encodeEnvelope: %v", err)
	}
	var got ActionResult
	if err := decodeEnvelope(encoded, &got); err != nil {
		t.Fatalf("decodeEnvelope: %v", err)
	}
	if got.ActionHash != original.ActionHash {
		t.Fatalf("ActionHash mismatch: got %s want %s", got.ActionHash, original.ActionHash)
	}
	if len(got.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1", len(got.Outputs))
	}
	out := got.Outputs[0]
	want := original.Outputs[0]
	if out.Role != want.Role || out.Blob.Hash != want.Blob.Hash || out.Blob.Size != want.Blob.Size {
		t.Fatalf("output round-trip mismatch: got %+v want %+v", out, want)
	}
}

func TestDecodeEnvelopeRejectsMismatch(t *testing.T) {
	for name, payload := range map[string][]byte{
		"older version":               []byte(`{"schemaVersion":0,"payload":{}}`),
		"future version":              []byte(`{"schemaVersion":999,"payload":{}}`),
		"legacy unwrapped (no field)": []byte(`{"source":{"kind":"download"}}`),
	} {
		t.Run(name, func(t *testing.T) {
			var prov Provenance
			err := decodeEnvelope(payload, &prov)
			if !errors.Is(err, ErrSchemaMismatch) {
				t.Fatalf("expected ErrSchemaMismatch, got %v", err)
			}
		})
	}
}

func TestDecodeEnvelopeFailsOnGarbage(t *testing.T) {
	var prov Provenance
	err := decodeEnvelope([]byte("not json"), &prov)
	if err == nil {
		t.Fatalf("expected error decoding garbage")
	}
	if errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("garbage should not surface as ErrSchemaMismatch: %v", err)
	}
}

// A current-version envelope whose inner payload is malformed (string
// where an object is expected) should propagate the raw JSON error, not
// ErrSchemaMismatch. Confirms that the version check and the payload
// check stay distinct failure modes.
func TestDecodeEnvelopeMalformedInnerPayload(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"payload":"not-an-object"}`)
	var prov Provenance
	err := decodeEnvelope(data, &prov)
	if err == nil {
		t.Fatalf("expected error decoding malformed inner payload")
	}
	if errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("malformed payload should not surface as ErrSchemaMismatch: %v", err)
	}
}

func TestFilesystemStoreSchemaMismatchTreatedAsNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemStore(dir)
	ctx := context.Background()

	writeLegacy := func(t *testing.T, path string, body []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	t.Run("provenance", func(t *testing.T) {
		h := HashBytes([]byte("prov-payload"))
		path := filepath.Join(dir, "provenance", h.String()[:2], h.String()[2:]+".json")
		writeLegacy(t, path, []byte(`{"source":{"kind":"download"},"createdAt":"2024-01-01T00:00:00Z"}`))
		if _, err := store.Provenance(ctx, h); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("action result", func(t *testing.T) {
		h := HashBytes([]byte("action-payload"))
		path := filepath.Join(dir, "actions", h.String()[:2], h.String()[2:]+".json")
		writeLegacy(t, path, []byte(`{"actionHash":"`+h.String()+`","outputs":[]}`))
		if _, err := store.GetActionResult(ctx, h); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("action summary", func(t *testing.T) {
		h := HashBytes([]byte("summary-payload"))
		path := filepath.Join(dir, "actions", h.String()[:2], h.String()[2:]+".summary.json")
		writeLegacy(t, path, []byte(`{"actionHash":"`+h.String()+`","outcome":"miss"}`))
		if _, err := store.GetActionSummary(ctx, h); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestFilesystemStoreEnvelopeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFilesystemStore(dir)
	ctx := context.Background()

	info, err := store.PutBytes(ctx, []byte("data"), Provenance{Source: Source{Kind: SourceDownload}})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	prov, err := store.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Kind != SourceDownload {
		t.Fatalf("unexpected source kind: %q", prov.Source.Kind)
	}

	provPath := filepath.Join(dir, "provenance", info.Hash.String()[:2], info.Hash.String()[2:]+".json")
	raw, err := os.ReadFile(provPath)
	if err != nil {
		t.Fatalf("read provenance file: %v", err)
	}
	if !strings.Contains(string(raw), `"schemaVersion"`) {
		t.Fatalf("expected schemaVersion in on-disk provenance, got: %s", raw)
	}
}
