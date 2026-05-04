package remotecache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func startStoreTestServer(t *testing.T) (*Store, func()) {
	t.Helper()
	fake := newFakeServer()
	ts := httptest.NewServer(fake.handler())
	client, err := New(ts.URL, "")
	if err != nil {
		ts.Close()
		t.Fatalf("New: %v", err)
	}
	return NewStore(client), ts.Close
}

func TestStoreImplementsCASStoreInterface(t *testing.T) {
	var _ cas.Store = (*Store)(nil)
}

func TestStorePutBytesGetRoundTrip(t *testing.T) {
	store, cleanup := startStoreTestServer(t)
	defer cleanup()
	ctx := context.Background()

	payload := []byte("adapter payload")
	info, err := store.PutBytes(ctx, payload, cas.Provenance{})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	if info.Hash != cas.HashBytes(payload) {
		t.Fatalf("hash mismatch")
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("size mismatch")
	}

	rc, err := store.Get(ctx, info.Hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("round trip mismatch")
	}
}

func TestStorePutExpectedRejectsMismatch(t *testing.T) {
	store, cleanup := startStoreTestServer(t)
	defer cleanup()
	_, err := store.PutBytesExpected(context.Background(), []byte("real"), cas.HashBytes([]byte("wrong")), cas.Provenance{})
	if !errors.Is(err, cas.ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
}

func TestStorePutExpectedReaderRejectsMismatch(t *testing.T) {
	store, cleanup := startStoreTestServer(t)
	defer cleanup()
	_, err := store.PutExpected(context.Background(), bytes.NewReader([]byte("real")), cas.HashBytes([]byte("wrong")), cas.Provenance{})
	if !errors.Is(err, cas.ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
}

func TestStoreHasAndStat(t *testing.T) {
	store, cleanup := startStoreTestServer(t)
	defer cleanup()
	ctx := context.Background()

	payload := []byte("has/stat test")
	hash := cas.HashBytes(payload)

	has, err := store.Has(ctx, hash)
	if err != nil || has {
		t.Fatalf("expected absent, got has=%v err=%v", has, err)
	}
	if _, err := store.Stat(ctx, hash); !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound from Stat, got %v", err)
	}

	if _, err := store.PutBytes(ctx, payload, cas.Provenance{}); err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	has, err = store.Has(ctx, hash)
	if err != nil || !has {
		t.Fatalf("expected present, got has=%v err=%v", has, err)
	}
	info, err := store.Stat(ctx, hash)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Hash != hash {
		t.Fatalf("stat hash mismatch")
	}
	// Remote Stat reports Size: 0 as "present but size unknown".
	if info.Size != 0 {
		t.Fatalf("remote Stat size: expected 0, got %d", info.Size)
	}
}

func TestStoreProvenanceAlwaysNotFound(t *testing.T) {
	store, cleanup := startStoreTestServer(t)
	defer cleanup()
	ctx := context.Background()

	payload := []byte("prov test")
	info, err := store.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "this should be dropped"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	_, err = store.Provenance(ctx, info.Hash)
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound from Provenance, got %v", err)
	}
}

func TestStoreActionResultRoundTrip(t *testing.T) {
	store, cleanup := startStoreTestServer(t)
	defer cleanup()
	ctx := context.Background()

	actionHash := cas.HashBytes([]byte("action"))
	outputHash := cas.HashBytes([]byte("output"))
	result := cas.ActionResult{
		ActionHash: actionHash,
		Outputs: []cas.NamedOutput{
			{Role: "main", Blob: cas.BlobInfo{Hash: outputHash, Size: 6}},
		},
	}
	if err := store.PutActionResult(ctx, result); err != nil {
		t.Fatalf("PutActionResult: %v", err)
	}
	loaded, err := store.GetActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("GetActionResult: %v", err)
	}
	if loaded.ActionHash != actionHash || len(loaded.Outputs) != 1 {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
}

func TestStoreHasActionResult(t *testing.T) {
	store, cleanup := startStoreTestServer(t)
	defer cleanup()
	ctx := context.Background()

	actionHash := cas.HashBytes([]byte("action"))
	has, err := store.HasActionResult(ctx, actionHash)
	if err != nil || has {
		t.Fatalf("expected absent action result, got has=%v err=%v", has, err)
	}

	result := cas.ActionResult{
		ActionHash: actionHash,
		Outputs: []cas.NamedOutput{
			{Role: "main", Blob: cas.BlobInfo{Hash: cas.HashBytes([]byte("output")), Size: 6}},
		},
	}
	if err := store.PutActionResult(ctx, result); err != nil {
		t.Fatalf("PutActionResult: %v", err)
	}

	has, err = store.HasActionResult(ctx, actionHash)
	if err != nil || !has {
		t.Fatalf("expected present action result, got has=%v err=%v", has, err)
	}
}

func TestStoreGetNotFound(t *testing.T) {
	store, cleanup := startStoreTestServer(t)
	defer cleanup()
	_, err := store.Get(context.Background(), cas.HashBytes([]byte("missing")))
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
