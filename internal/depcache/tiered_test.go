package depcache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/remotecache"
	"github.com/kaeawc/grit/internal/tieredcas"
	"github.com/kaeawc/grit/internal/transform/aarextract"
)

// TestTieredCacheThreeTierStack stands up a realistic probe chain:
//
//	tier 0: worktree overlay (FilesystemStore)
//	tier 1: shared-local CAS (FilesystemStore)
//	tier 2: remote cache     (remotecache.Store)
//
// The test seeds content only in the remote tier, then asks the tiered
// store to serve it. The flow exercises:
//
//   - read probe falling through all three tiers
//   - promotion on hit into tiers 0 and 1
//   - content addressing surviving translation through the HTTP wire
//     protocol and back into a local store
//   - second read served entirely from the primary tier
func TestTieredCacheThreeTierStack(t *testing.T) {
	ctx := context.Background()

	// Stand up tier 2: a remote cache client backed by a fake server.
	remoteClient, remoteClose := startFakeRemoteCache(t)
	defer remoteClose()
	remoteStore := remotecache.NewStore(remoteClient)

	// Tiers 0 and 1: local filesystem stores.
	primary := cas.NewFilesystemStore(t.TempDir())
	sharedLocal := cas.NewFilesystemStore(t.TempDir())

	tiered, err := tieredcas.New(primary, sharedLocal, remoteStore)
	if err != nil {
		t.Fatalf("tieredcas.New: %v", err)
	}

	// Seed the remote tier directly via the client so the content only
	// exists there.
	payload := []byte("three-tier payload")
	hash := cas.HashBytes(payload)
	if err := remoteClient.PutBlob(ctx, hash, payload); err != nil {
		t.Fatalf("remote seed: %v", err)
	}

	// Verify the content is genuinely absent from the local tiers before
	// the first read.
	if has, _ := primary.Has(ctx, hash); has {
		t.Fatalf("primary tier should start empty")
	}
	if has, _ := sharedLocal.Has(ctx, hash); has {
		t.Fatalf("shared-local tier should start empty")
	}

	// First Get walks the full chain, promotes into tiers 0 and 1.
	rc, err := tiered.Get(ctx, hash)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	if closeErr := rc.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("tiered Get returned wrong bytes")
	}

	// After the first Get, both local tiers must hold the blob.
	if has, _ := primary.Has(ctx, hash); !has {
		t.Fatalf("primary tier missing after promotion")
	}
	if has, _ := sharedLocal.Has(ctx, hash); !has {
		t.Fatalf("shared-local tier missing after promotion")
	}

	// Second Get should be served from the primary tier directly. We
	// cannot directly observe which tier served the call, but we can
	// prove the primary tier holds the content by reading from it.
	secondRC, err := primary.Get(ctx, hash)
	if err != nil {
		t.Fatalf("primary Get after promotion: %v", err)
	}
	second, _ := io.ReadAll(secondRC)
	_ = secondRC.Close()
	if !bytes.Equal(second, payload) {
		t.Fatalf("primary tier did not store promoted bytes correctly")
	}
}

// TestTieredCacheActionResultPromotion exercises action-result promotion
// across tiers. Seed an action result in the upstream tier, read through
// the tiered store, and verify the primary tier caches the result.
func TestTieredCacheActionResultPromotion(t *testing.T) {
	ctx := context.Background()

	primary := cas.NewFilesystemStore(t.TempDir())
	upstream := cas.NewFilesystemStore(t.TempDir())

	actionHash := cas.HashBytes([]byte("action identity"))
	outputHash := cas.HashBytes([]byte("output blob"))
	result := cas.ActionResult{
		ActionHash: actionHash,
		Outputs: []cas.NamedOutput{
			{Role: "main", Blob: cas.BlobInfo{Hash: outputHash, Size: 11}},
		},
	}
	if err := upstream.PutActionResult(ctx, result); err != nil {
		t.Fatal(err)
	}

	tiered, err := tieredcas.New(primary, upstream)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := tiered.GetActionResult(ctx, actionHash)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActionHash != actionHash {
		t.Fatalf("action hash mismatch")
	}
	if _, err := primary.GetActionResult(ctx, actionHash); err != nil {
		t.Fatalf("primary should have promoted action result: %v", err)
	}
}

// TestTieredCacheServesAARExtractThroughRemote is the architectural
// money shot: an AAR is only in the remote tier; the aar-extract
// transform reads through the tiered cas.Store and produces the
// correctly-hashed outputs that land in the primary tier.
func TestTieredCacheServesAARExtractThroughRemote(t *testing.T) {
	ctx := context.Background()

	// Tier 2: remote.
	remoteClient, remoteClose := startFakeRemoteCache(t)
	defer remoteClose()
	remoteStore := remotecache.NewStore(remoteClient)

	// Tier 0: primary local store.
	primary := cas.NewFilesystemStore(t.TempDir())

	// Synthesize an AAR and upload it to the remote tier only.
	classesBody := []byte("remote-only classes")
	manifestBody := []byte(`<?xml version="1.0"?><manifest package="com.example"/>`)
	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":         classesBody,
		"AndroidManifest.xml": manifestBody,
	})
	aarHash := cas.HashBytes(aarBytes)
	if err := remoteClient.PutBlob(ctx, aarHash, aarBytes); err != nil {
		t.Fatalf("remote PutBlob: %v", err)
	}

	tiered, err := tieredcas.New(primary, remoteStore)
	if err != nil {
		t.Fatalf("tieredcas.New: %v", err)
	}

	// Run aar-extract against the tiered store. The transform reads the
	// AAR via tiered.Get, which falls through to the remote, promotes
	// into the primary, and returns the bytes. The transform then writes
	// its outputs into the primary tier (via tiered.PutBytes).
	result, err := aarextract.Extract(ctx, tiered, aarHash)
	if err != nil {
		t.Fatalf("Extract through tiered store: %v", err)
	}

	classesOut, ok := result.Output(aarextract.RoleClassesJar)
	if !ok {
		t.Fatalf("classes-jar output missing")
	}
	if classesOut.Blob.Hash != cas.HashBytes(classesBody) {
		t.Fatalf("classes-jar hash mismatch")
	}
	manifestOut, ok := result.Output(aarextract.RoleAndroidManifest)
	if !ok {
		t.Fatalf("android-manifest output missing")
	}
	if manifestOut.Blob.Hash != cas.HashBytes(manifestBody) {
		t.Fatalf("manifest hash mismatch")
	}

	// The promoted AAR must be in the primary tier.
	if has, _ := primary.Has(ctx, aarHash); !has {
		t.Fatalf("AAR not promoted into primary tier")
	}
	// The extracted outputs must be in the primary tier (transform writes
	// via the tiered store, which writes to primary only).
	if has, _ := primary.Has(ctx, classesOut.Blob.Hash); !has {
		t.Fatalf("classes-jar output not in primary tier")
	}
	if has, _ := primary.Has(ctx, manifestOut.Blob.Hash); !has {
		t.Fatalf("manifest output not in primary tier")
	}

	// The action result must be cached in the primary tier too.
	cached, err := primary.GetActionResult(ctx, result.ActionHash)
	if err != nil {
		t.Fatalf("action result not cached in primary tier: %v", err)
	}
	if cached.ActionHash != result.ActionHash {
		t.Fatalf("cached action hash mismatch")
	}
}

func TestTieredCacheGetNotFoundAcrossAllTiers(t *testing.T) {
	ctx := context.Background()

	// Remote is empty.
	fake := newFakeRemote()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()
	client, err := remotecache.New(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}

	tiered, err := tieredcas.New(
		cas.NewFilesystemStore(t.TempDir()),
		cas.NewFilesystemStore(t.TempDir()),
		remotecache.NewStore(client),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tiered.Get(ctx, cas.HashBytes([]byte("never written anywhere")))
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
