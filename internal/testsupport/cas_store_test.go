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

func TestCASStoreRecorderProvenanceReturnsCopies(t *testing.T) {
	store := NewCASStoreRecorder()
	ctx := context.Background()
	payload := []byte("blob")
	inputHash := cas.HashBytes([]byte("input"))
	prov := cas.Provenance{
		Source: cas.Source{
			Kind: cas.SourceTransform,
			Transform: &cas.TransformSource{
				ActionHash: cas.HashBytes([]byte("action")),
				ActionKind: "dex",
				Inputs: []cas.TransformInput{
					{Role: "classes", Hash: inputHash},
				},
			},
		},
		Attributes: map[string]string{"kind": "test"},
	}

	info, err := store.PutBytes(ctx, payload, prov)
	if err != nil {
		t.Fatal(err)
	}
	prov.Attributes["kind"] = "mutated"
	prov.Source.Transform.Inputs[0].Role = "mutated"

	loaded, err := store.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Attributes["kind"] = "mutated-again"
	loaded.Source.Transform.Inputs[0].Role = "mutated-again"

	fresh, err := store.Provenance(ctx, info.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if got := fresh.Attributes["kind"]; got != "test" {
		t.Fatalf("Attributes[kind] = %q", got)
	}
	if got := fresh.Source.Transform.Inputs[0].Role; got != "classes" {
		t.Fatalf("Transform.Inputs[0].Role = %q", got)
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

func TestCASStoreRecorderActionResultsReturnCopies(t *testing.T) {
	store := NewCASStoreRecorder()
	ctx := context.Background()
	result := cas.ActionResult{
		ActionHash: cas.HashBytes([]byte("action")),
		Outputs: []cas.NamedOutput{
			{Role: "jar", Blob: cas.BlobInfo{Hash: cas.HashBytes([]byte("jar")), Size: 3}},
		},
	}

	if err := store.PutActionResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	result.Outputs[0].Role = "mutated"

	loaded, err := store.GetActionResult(ctx, result.ActionHash)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Outputs[0].Role = "mutated-again"

	fresh, err := store.GetActionResult(ctx, result.ActionHash)
	if err != nil {
		t.Fatal(err)
	}
	if got := fresh.Outputs[0].Role; got != "jar" {
		t.Fatalf("Outputs[0].Role = %q", got)
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
