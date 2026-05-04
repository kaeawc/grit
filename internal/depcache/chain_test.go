package depcache_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/downloader/chain"
	"github.com/kaeawc/grit/internal/downloader/gradlecache"
	"github.com/kaeawc/grit/internal/downloader/mavenlocal"
	"github.com/kaeawc/grit/internal/downloader/mavenremote"
	"github.com/kaeawc/grit/internal/lockfile"
)

// TestChainWithRealAdaptersFallsThroughToRemote composes every real
// Layer 2 adapter into one priority-ordered chain and proves fall-
// through semantics work end-to-end against real filesystem and HTTP
// sources, not just stubs.
//
// The scenario:
//
//  1. Empty Gradle cache directory (no files for the coordinate)
//  2. Empty Maven local directory (no files for the coordinate)
//  3. Populated Maven remote HTTP server (has the file)
//
// The chain order is gradlecache → mavenlocal → mavenremote, matching
// how a real build would prefer local sources. The first two return
// ErrNotFound; the third serves the bytes.
func TestChainWithRealAdaptersFallsThroughToRemote(t *testing.T) {
	ctx := context.Background()

	payload := []byte("chain integration payload")
	hash := cas.HashBytes(payload)

	// Tier 3: a real fake Maven server that has the file.
	server := newCountingMavenServer()
	server.set("/org/example/chain/demo/1.0.0/demo-1.0.0.jar", payload)
	ts := httptest.NewServer(server.handler())
	defer ts.Close()

	gradleRoot := t.TempDir()     // empty
	mavenLocalRoot := t.TempDir() // empty

	gradle := gradlecache.New(gradleRoot)
	local := mavenlocal.New(mavenLocalRoot)
	remote, err := mavenremote.New(ts.URL+"/", mavenremote.WithID("fake-central"))
	if err != nil {
		t.Fatalf("mavenremote.New: %v", err)
	}

	c, err := chain.New([]downloader.Downloader{gradle, local, remote})
	if err != nil {
		t.Fatalf("chain.New: %v", err)
	}

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.example.chain", Artifact: "demo", Version: "1.0.0"},
		RepositoryID: "fake-central",
		Files: []lockfile.PinFile{
			{
				Kind: lockfile.FileKindPrimary,
				Name: "demo-1.0.0.jar",
				Size: int64(len(payload)),
				Hash: hash,
			},
		},
	}

	if err := c.Fetch(ctx, pin, store); err != nil {
		t.Fatalf("chain Fetch: %v", err)
	}

	if has, _ := store.Has(ctx, hash); !has {
		t.Fatalf("blob not present after chain fetch")
	}

	// Provenance must record the downloader that actually served the
	// bytes, not an intermediate that fell through.
	prov, err := store.Provenance(ctx, hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Download == nil || prov.Source.Download.Downloader != "fake-central" {
		t.Fatalf("expected fake-central provenance, got %+v", prov.Source.Download)
	}

	if server.requestCount() != 1 {
		t.Fatalf("expected 1 remote request, got %d", server.requestCount())
	}

	// Second Fetch: the store already has the blob, so no adapter
	// should even reach the network. This is the idempotent property
	// documented on Downloader.Fetch.
	if err := c.Fetch(ctx, pin, store); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if server.requestCount() != 1 {
		t.Fatalf("second Fetch hit network: %d total requests", server.requestCount())
	}
}

// TestChainFallsThroughWhenAllLocalSourcesMiss exercises the ordering
// where two local adapters both miss and the chain must surface the
// last ErrNotFound. The remote source is absent from the chain so this
// test isolates the "everything returned not-found" path.
func TestChainAllSourcesMiss(t *testing.T) {
	ctx := context.Background()

	c, err := chain.New([]downloader.Downloader{
		gradlecache.New(t.TempDir()),
		mavenlocal.New(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}

	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "nowhere", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "nowhere-1.0.jar", Hash: cas.HashBytes([]byte("x"))},
		},
	}
	err = c.Fetch(ctx, pin, cas.NewFilesystemStore(t.TempDir()))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, downloader.ErrNotFound) {
		t.Fatalf("expected wrapped downloader.ErrNotFound from chain, got %v", err)
	}
}

// TestChainHonorsLocalBeforeRemote verifies that when a local adapter
// already has the file, the chain never touches the network.
func TestChainPrefersLocalBeforeRemote(t *testing.T) {
	ctx := context.Background()

	payload := []byte("local already has it")
	hash := cas.HashBytes(payload)

	// Stage the file in a mavenlocal layout.
	mavenRoot := t.TempDir()
	stagedDir := filepath.Join(mavenRoot, "org", "example", "local", "cached", "1.0")
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagedDir, "cached-1.0.jar"), payload, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Also stand up a remote server, but it should never be called.
	server := newCountingMavenServer()
	ts := httptest.NewServer(server.handler())
	defer ts.Close()
	remote, err := mavenremote.New(ts.URL+"/", mavenremote.WithID("remote-should-not-be-called"))
	if err != nil {
		t.Fatal(err)
	}

	c, err := chain.New([]downloader.Downloader{
		mavenlocal.New(mavenRoot),
		remote,
	})
	if err != nil {
		t.Fatal(err)
	}

	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example.local", Artifact: "cached", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "cached-1.0.jar", Hash: hash},
		},
	}

	if err := c.Fetch(ctx, pin, cas.NewFilesystemStore(t.TempDir())); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if server.requestCount() != 0 {
		t.Fatalf("remote was contacted %d times even though local had the file", server.requestCount())
	}
}

// ---- countingMavenServer: shared helper for chain integration tests ----

type countingMavenServer struct {
	mu       sync.Mutex
	files    map[string][]byte
	requests int
}

func newCountingMavenServer() *countingMavenServer {
	return &countingMavenServer{files: map[string][]byte{}}
}

func (s *countingMavenServer) set(path string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = data
}

func (s *countingMavenServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *countingMavenServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests++
		data, ok := s.files[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	})
}
