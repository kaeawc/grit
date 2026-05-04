package depcache_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader/mavenremote"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/tieredcas"
	"github.com/kaeawc/grit/internal/transform/aarextract"
)

// TestMavenRemoteThroughTieredCacheIntoTransform proves the full
// Layer 1 → Layer 2 → Layer 3 → Layer 4 chain when the source of
// truth is an HTTP Maven repository:
//
//  1. A remote Maven server serves an AAR at the canonical Maven2 path.
//  2. A lockfile pin names the coordinate + declared content hash.
//  3. The mavenremote downloader fetches the bytes, verifying the hash
//     on the way in via store.PutExpected.
//  4. The downloader writes into a tieredcas.Store whose primary tier
//     is a local FilesystemStore.
//  5. The aarextract transform reads from the tiered store and produces
//     classes-jar and android-manifest outputs.
//  6. Running Extract again hits the action-result cache without
//     re-reading the AAR.
//
// No callers of the transform or the tiered store need to know there is
// a remote downloader involved. That is the architectural payoff: the
// cas.Store interface boundary absorbs tier and source differences.
func TestMavenRemoteThroughTieredCacheIntoTransform(t *testing.T) {
	ctx := context.Background()

	// Stage 1: synthesize an AAR and serve it from a fake Maven server.
	classesBody := []byte("remote maven classes")
	manifestBody := []byte(`<?xml version="1.0"?><manifest package="com.example.remote"/>`)
	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":         classesBody,
		"AndroidManifest.xml": manifestBody,
	})
	aarHash := cas.HashBytes(aarBytes)

	maven := newFakeMavenRepo()
	maven.set("/org/example/remote/widget/4.2.0/widget-4.2.0.aar", aarBytes)

	ts := httptest.NewServer(maven.handler())
	defer ts.Close()

	// Stage 2: build a lockfile pin for the AAR.
	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.example.remote", Artifact: "widget", Version: "4.2.0"},
		RepositoryID: "fake-central",
		Files: []lockfile.PinFile{
			{
				Kind: lockfile.FileKindPrimary,
				Name: "widget-4.2.0.aar",
				Size: int64(len(aarBytes)),
				Hash: aarHash,
			},
		},
	}

	// Stage 3: build the mavenremote downloader and a tieredcas.Store
	// whose primary tier is a local FilesystemStore.
	remote, err := mavenremote.New(ts.URL+"/", mavenremote.WithID("fake-central"))
	if err != nil {
		t.Fatalf("mavenremote.New: %v", err)
	}

	primary := cas.NewFilesystemStore(t.TempDir())
	tiered, err := tieredcas.New(primary)
	if err != nil {
		t.Fatalf("tieredcas.New: %v", err)
	}

	// Stage 4: fetch via the remote downloader, landing the bytes in the
	// tiered store (which writes to primary).
	if err := remote.Fetch(ctx, pin, tiered); err != nil {
		t.Fatalf("remote Fetch: %v", err)
	}
	if has, _ := primary.Has(ctx, aarHash); !has {
		t.Fatalf("AAR did not land in primary tier")
	}

	// Provenance must carry the full download breadcrumb.
	prov, err := primary.Provenance(ctx, aarHash)
	if err != nil {
		t.Fatalf("primary Provenance: %v", err)
	}
	if prov.Source.Download == nil {
		t.Fatalf("download provenance missing")
	}
	if prov.Source.Download.Downloader != "fake-central" {
		t.Fatalf("downloader ID not recorded: %s", prov.Source.Download.Downloader)
	}
	if prov.Source.Download.URL == "" {
		t.Fatalf("URL not recorded")
	}

	// Stage 5: run aar-extract against the tiered store.
	result, err := aarextract.Extract(ctx, tiered, aarHash)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	classesOut, ok := result.Output(aarextract.RoleClassesJar)
	if !ok {
		t.Fatalf("classes-jar missing")
	}
	if classesOut.Blob.Hash != cas.HashBytes(classesBody) {
		t.Fatalf("classes-jar hash mismatch")
	}
	manifestOut, ok := result.Output(aarextract.RoleAndroidManifest)
	if !ok {
		t.Fatalf("android-manifest missing")
	}
	if manifestOut.Blob.Hash != cas.HashBytes(manifestBody) {
		t.Fatalf("manifest hash mismatch")
	}

	// Stage 6: running Extract again must not require another network
	// fetch (the action result is cached in the primary tier).
	requestsBefore := maven.requestCount()
	cached, err := aarextract.Extract(ctx, tiered, aarHash)
	if err != nil {
		t.Fatalf("cached Extract: %v", err)
	}
	if cached.ActionHash != result.ActionHash {
		t.Fatalf("action hash drifted across calls")
	}
	requestsAfter := maven.requestCount()
	if requestsAfter != requestsBefore {
		t.Fatalf("cached Extract made %d network requests, should have made 0",
			requestsAfter-requestsBefore)
	}

	// Stage 7: running the mavenremote Fetch a second time must also be
	// idempotent (short-circuited by store.Has before the HTTP call).
	if err := remote.Fetch(ctx, pin, tiered); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if maven.requestCount() != requestsAfter {
		t.Fatalf("second Fetch hit network unnecessarily")
	}

	// Sanity check: read the classes blob back out through the tiered
	// store and verify the bytes.
	rc, err := tiered.Get(ctx, classesOut.Blob.Hash)
	if err != nil {
		t.Fatalf("tiered Get classes: %v", err)
	}
	gotClasses, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(gotClasses, classesBody) {
		t.Fatalf("classes bytes mismatch after full pipeline")
	}
}

// ---------- fake Maven server used by mavenremote integration tests ----------

type fakeMavenRepo struct {
	mu       sync.Mutex
	files    map[string][]byte
	requests int
}

func newFakeMavenRepo() *fakeMavenRepo {
	return &fakeMavenRepo{files: map[string][]byte{}}
}

func (f *fakeMavenRepo) set(path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = data
}

func (f *fakeMavenRepo) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeMavenRepo) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests++
		data, ok := f.files[r.URL.Path]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	})
}
