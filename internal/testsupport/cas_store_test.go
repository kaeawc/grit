package testsupport

import (
	"context"
	"errors"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func TestCASStoreRecorderBlobsAndProvenanceRoundTrip(t *testing.T) {
	store := NewCASStoreRecorder()
	ctx := context.Background()

	payload := []byte("blob")
	prov := cas.Provenance{Attributes: map[string]string{"kind": "test"}}
	info, err := store.PutBytes(ctx, payload, prov)
	if err != nil {
		t.Fatal(err)
	}
	if info.Hash != cas.HashBytes(payload) || info.Size != int64(len(payload)) {
		t.Fatalf("unexpected blob info: %#v", info)
	}
	gotProv, err := store.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if gotProv.Attributes["kind"] != "test" {
		t.Fatalf("unexpected provenance: %#v", gotProv)
	}
	got, err := store.Get(ctx, info.Hash)
	if err != nil {
		t.Fatal(err)
	}
	_ = got.Close()
	if len(store.CallsSnapshot()) < 3 {
		t.Fatalf("expected calls to be recorded, got %#v", store.CallsSnapshot())
	}
}

func TestCASStoreRecorderActionResultsRoundTrip(t *testing.T) {
	store := NewCASStoreRecorder()
	ctx := context.Background()

	result := cas.ActionResult{ActionHash: cas.HashBytes([]byte("action"))}
	if err := store.PutActionResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetActionResult(ctx, result.ActionHash)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActionHash != result.ActionHash {
		t.Fatalf("unexpected action result: %#v", loaded)
	}
}

func TestCASStoreRecorderCanInjectErrors(t *testing.T) {
	store := NewCASStoreRecorder()
	store.Err = errors.New("boom")

	if _, err := store.PutBytes(context.Background(), []byte("x"), cas.Provenance{}); err == nil || err.Error() != "boom" {
		t.Fatalf("unexpected put error: %v", err)
	}
	if _, err := store.Get(context.Background(), cas.HashBytes([]byte("missing"))); err == nil || err.Error() != "boom" {
		t.Fatalf("unexpected get error: %v", err)
	}
}
