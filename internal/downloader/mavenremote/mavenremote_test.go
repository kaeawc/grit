package mavenremote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

// fakeMaven is a minimal HTTP server that serves a map of paths to bytes.
// Used as a test fixture so the downloader can be exercised end-to-end
// without hitting a real Maven repository.
type fakeMaven struct {
	mu       sync.Mutex
	files    map[string][]byte
	requests int32
	lastReq  *http.Request
}

func newFakeMaven() *fakeMaven {
	return &fakeMaven{files: map[string][]byte{}}
}

func (f *fakeMaven) set(path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = data
}

func (f *fakeMaven) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.requests, 1)
		f.mu.Lock()
		f.lastReq = r.Clone(r.Context())
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

func (f *fakeMaven) requestCount() int32 {
	return atomic.LoadInt32(&f.requests)
}

func startFakeMaven(t *testing.T) (*Downloader, *fakeMaven, func()) {
	t.Helper()
	fake := newFakeMaven()
	ts := httptest.NewServer(fake.handler())
	d, err := New(ts.URL + "/")
	if err != nil {
		ts.Close()
		t.Fatalf("New: %v", err)
	}
	return d, fake, ts.Close
}

func TestNewRejectsBadURL(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatalf("expected error for empty URL")
	}
	if _, err := New("relative/path"); err == nil {
		t.Fatalf("expected error for relative URL")
	}
	if _, err := New("://bad"); err == nil {
		t.Fatalf("expected error for malformed URL")
	}
}

func TestIDDefaultsAndOverride(t *testing.T) {
	d, err := New("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID() != DefaultID {
		t.Fatalf("default ID: got %q want %q", d.ID(), DefaultID)
	}

	d2, err := New("https://example.com/", WithID("maven-central"))
	if err != nil {
		t.Fatal(err)
	}
	if d2.ID() != "maven-central" {
		t.Fatalf("override ID: got %q", d2.ID())
	}
}

func TestFetchRoundTripWithDerivedURL(t *testing.T) {
	d, fake, cleanup := startFakeMaven(t)
	defer cleanup()
	ctx := context.Background()

	payload := []byte("remote jar bytes")
	hash := cas.HashBytes(payload)
	fake.set("/org/example/alpha/1.0/alpha-1.0.jar", payload)

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		RepositoryID: "central",
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Size: int64(len(payload)), Hash: hash},
		},
	}
	if err := d.Fetch(ctx, pin, store); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	has, _ := store.Has(ctx, hash)
	if !has {
		t.Fatalf("blob not present after Fetch")
	}

	// Provenance must record the exact URL the downloader fetched.
	prov, err := store.Provenance(ctx, hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Download == nil {
		t.Fatalf("download provenance missing")
	}
	expectedPathSuffix := "/org/example/alpha/1.0/alpha-1.0.jar"
	if !hasSuffix(prov.Source.Download.URL, expectedPathSuffix) {
		t.Fatalf("url does not have expected suffix: %s", prov.Source.Download.URL)
	}
	if prov.Source.Download.Downloader != DefaultID {
		t.Fatalf("downloader id not recorded: %s", prov.Source.Download.Downloader)
	}
	if prov.Source.Download.Coordinate != "org.example:alpha:1.0" {
		t.Fatalf("coordinate not recorded: %s", prov.Source.Download.Coordinate)
	}
	if prov.Source.Download.RepositoryID != "central" {
		t.Fatalf("repository id not recorded: %s", prov.Source.Download.RepositoryID)
	}
}

func TestFetchRedactsCredentialsFromBaseURLInProvenance(t *testing.T) {
	d, fake, cleanup := startFakeMaven(t)
	defer cleanup()
	ctx := context.Background()

	payload := []byte("credentialed payload")
	hash := cas.HashBytes(payload)
	fake.set("/org/example/alpha/1.0/alpha-1.0.jar", payload)

	baseURL, err := url.Parse(d.BaseURL())
	if err != nil {
		t.Fatal(err)
	}
	baseURL.User = url.UserPassword("alice", "secret")

	credentialed, err := New(baseURL.String() + "/")
	if err != nil {
		t.Fatal(err)
	}

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		RepositoryID: "central",
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Size: int64(len(payload)), Hash: hash},
		},
	}
	if err := credentialed.Fetch(ctx, pin, store); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	fake.mu.Lock()
	req := fake.lastReq
	fake.mu.Unlock()
	if req == nil {
		t.Fatalf("no request captured")
	}
	user, pass, ok := req.BasicAuth()
	if !ok || user != "alice" || pass != "secret" {
		t.Fatalf("expected basic auth from credentialed base URL, got %q %q %v", user, pass, ok)
	}

	prov, err := store.Provenance(ctx, hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Download == nil {
		t.Fatalf("download provenance missing")
	}
	if strings.Contains(prov.Source.Download.URL, "alice") || strings.Contains(prov.Source.Download.URL, "secret") {
		t.Fatalf("provenance URL leaked credentials: %s", prov.Source.Download.URL)
	}
	if !hasSuffix(prov.Source.Download.URL, "/org/example/alpha/1.0/alpha-1.0.jar") {
		t.Fatalf("provenance URL has wrong path: %s", prov.Source.Download.URL)
	}
}

func TestFetchHonorsPinFileURL(t *testing.T) {
	d, fake, cleanup := startFakeMaven(t)
	defer cleanup()
	ctx := context.Background()

	// Stage content at a NON-coordinate-derived path to prove the pin's
	// explicit URL is used.
	payload := []byte("pin override payload")
	hash := cas.HashBytes(payload)
	fake.set("/custom/path/to/artifact.jar", payload)

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "unused", Artifact: "unused", Version: "1"},
		Files: []lockfile.PinFile{
			{
				Kind: lockfile.FileKindPrimary,
				Name: "artifact.jar",
				Hash: hash,
				URL:  d.BaseURL() + "custom/path/to/artifact.jar",
			},
		},
	}
	if err := d.Fetch(ctx, pin, store); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if has, _ := store.Has(ctx, hash); !has {
		t.Fatalf("blob not fetched via pin URL override")
	}
}

func TestFetchRedactsCredentialsFromExplicitPinURLInErrors(t *testing.T) {
	fake := newFakeMaven()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	credentialedURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	credentialedURL.User = url.UserPassword("bob", "topsecret")

	d, err := New(credentialedURL.String() + "/")
	if err != nil {
		t.Fatal(err)
	}

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "unused", Artifact: "unused", Version: "1"},
		Files: []lockfile.PinFile{
			{
				Kind: lockfile.FileKindPrimary,
				Name: "artifact.jar",
				Hash: cas.HashBytes([]byte("x")),
				URL:  credentialedURL.String() + "/custom/path/to/artifact.jar",
			},
		},
	}
	err = d.Fetch(context.Background(), pin, store)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if strings.Contains(err.Error(), "bob") || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error leaked credentials: %v", err)
	}

	fake.mu.Lock()
	req := fake.lastReq
	fake.mu.Unlock()
	if req == nil {
		t.Fatalf("no request captured")
	}
	user, pass, ok := req.BasicAuth()
	if !ok || user != "bob" || pass != "topsecret" {
		t.Fatalf("expected basic auth from credentialed pin URL, got %q %q %v", user, pass, ok)
	}
}

func TestFetchNotFound(t *testing.T) {
	d, _, cleanup := startFakeMaven(t)
	defer cleanup()

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "missing", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "missing-1.0.jar", Hash: cas.HashBytes([]byte("x"))},
		},
	}
	err := d.Fetch(context.Background(), pin, store)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFetchServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()
	d, err := New(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "g", Artifact: "a", Version: "1"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "a-1.jar", Hash: cas.HashBytes([]byte("x"))},
		},
	}
	errOut := d.Fetch(context.Background(), pin, store)
	if errOut == nil {
		t.Fatalf("expected error")
	}
	if errors.Is(errOut, ErrNotFound) {
		t.Fatalf("5xx must not surface as ErrNotFound")
	}
}

func TestFetchRejectsHashMismatch(t *testing.T) {
	d, fake, cleanup := startFakeMaven(t)
	defer cleanup()
	ctx := context.Background()

	actual := []byte("real content")
	wrongHash := cas.HashBytes([]byte("claimed content"))
	fake.set("/org/example/beta/2.0/beta-2.0.jar", actual)

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "beta", Version: "2.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "beta-2.0.jar", Hash: wrongHash},
		},
	}
	err := d.Fetch(ctx, pin, store)
	if !errors.Is(err, cas.ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
	if has, _ := store.Has(ctx, wrongHash); has {
		t.Fatalf("wrong hash should not land in CAS")
	}
	if has, _ := store.Has(ctx, cas.HashBytes(actual)); has {
		t.Fatalf("real hash should not land in CAS on mismatch")
	}
}

func TestFetchIdempotent(t *testing.T) {
	d, fake, cleanup := startFakeMaven(t)
	defer cleanup()
	ctx := context.Background()

	payload := []byte("idem payload")
	hash := cas.HashBytes(payload)
	fake.set("/org/example/alpha/1.0/alpha-1.0.jar", payload)

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Hash: hash},
		},
	}

	if err := d.Fetch(ctx, pin, store); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	countAfterFirst := fake.requestCount()

	if err := d.Fetch(ctx, pin, store); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	countAfterSecond := fake.requestCount()

	if countAfterSecond != countAfterFirst {
		t.Fatalf("second Fetch must not hit network: request count went %d → %d",
			countAfterFirst, countAfterSecond)
	}
}

func TestFetchMultipleFiles(t *testing.T) {
	d, fake, cleanup := startFakeMaven(t)
	defer cleanup()
	ctx := context.Background()

	jarBytes := []byte("jar body")
	pomBytes := []byte(`<project/>`)
	jarHash := cas.HashBytes(jarBytes)
	pomHash := cas.HashBytes(pomBytes)

	fake.set("/org/example/multi/3.0/multi-3.0.jar", jarBytes)
	fake.set("/org/example/multi/3.0/multi-3.0.pom", pomBytes)

	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "multi", Version: "3.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "multi-3.0.jar", Hash: jarHash},
			{Kind: lockfile.FileKindPOM, Name: "multi-3.0.pom", Hash: pomHash},
		},
	}
	if err := d.Fetch(ctx, pin, store); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, h := range []cas.Hash{jarHash, pomHash} {
		has, _ := store.Has(ctx, h)
		if !has {
			t.Fatalf("missing blob after Fetch: %s", h)
		}
	}
}

func TestFetchOfflineErrors(t *testing.T) {
	d, fake, cleanup := startFakeMaven(t)
	defer cleanup()

	offline, err := New(d.BaseURL(), WithOffline(true))
	if err != nil {
		t.Fatal(err)
	}
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Hash: cas.HashBytes([]byte("x"))},
		},
	}
	errOut := offline.Fetch(context.Background(), pin, cas.NewFilesystemStore(t.TempDir()))
	if !errors.Is(errOut, ErrOffline) {
		t.Fatalf("expected ErrOffline, got %v", errOut)
	}
	if fake.requestCount() != 0 {
		t.Fatalf("offline mode made %d requests, should have made 0", fake.requestCount())
	}
}

func TestFetchSendsCustomHeaders(t *testing.T) {
	fake := newFakeMaven()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	payload := []byte("auth payload")
	hash := cas.HashBytes(payload)
	fake.set("/org/example/alpha/1.0/alpha-1.0.jar", payload)

	d, err := New(ts.URL+"/", WithHeaders(map[string]string{
		"Authorization": "Bearer supersecret",
		"X-Custom":      "value",
	}))
	if err != nil {
		t.Fatal(err)
	}

	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Hash: hash},
		},
	}
	if err := d.Fetch(context.Background(), pin, cas.NewFilesystemStore(t.TempDir())); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	fake.mu.Lock()
	req := fake.lastReq
	fake.mu.Unlock()
	if req == nil {
		t.Fatalf("no request captured")
	}
	if got := req.Header.Get("Authorization"); got != "Bearer supersecret" {
		t.Fatalf("Authorization header: got %q", got)
	}
	if got := req.Header.Get("X-Custom"); got != "value" {
		t.Fatalf("X-Custom header: got %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != userAgent {
		t.Fatalf("User-Agent header: got %q want %q", got, userAgent)
	}
}

func TestFetchSendsEnvBackedHeaders(t *testing.T) {
	fake := newFakeMaven()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	payload := []byte("env auth payload")
	hash := cas.HashBytes(payload)
	fake.set("/org/example/alpha/1.0/alpha-1.0.jar", payload)

	t.Setenv("GRIT_MAVEN_AUTHORIZATION", "Bearer initial")
	d, err := New(ts.URL+"/", WithEnvHeader("Authorization", "GRIT_MAVEN_AUTHORIZATION"), WithHeaders(map[string]string{
		"X-Custom": "value",
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIT_MAVEN_AUTHORIZATION", "Bearer rotated")

	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Hash: hash},
		},
	}
	if err := d.Fetch(context.Background(), pin, cas.NewFilesystemStore(t.TempDir())); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	fake.mu.Lock()
	req := fake.lastReq
	fake.mu.Unlock()
	if req == nil {
		t.Fatalf("no request captured")
	}
	if got := req.Header.Get("Authorization"); got != "Bearer rotated" {
		t.Fatalf("Authorization header: got %q want %q", got, "Bearer rotated")
	}
	if got := req.Header.Get("X-Custom"); got != "value" {
		t.Fatalf("X-Custom header: got %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != userAgent {
		t.Fatalf("User-Agent header: got %q want %q", got, userAgent)
	}
}

func TestFetchCustomHeadersAreCopied(t *testing.T) {
	// Mutating the caller's map after construction must not affect the
	// downloader.
	fake := newFakeMaven()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	headers := map[string]string{"Authorization": "Bearer one"}
	d, err := New(ts.URL+"/", WithHeaders(headers))
	if err != nil {
		t.Fatal(err)
	}
	headers["Authorization"] = "Bearer MUTATED"

	payload := []byte("mutation test")
	hash := cas.HashBytes(payload)
	fake.set("/org/example/alpha/1.0/alpha-1.0.jar", payload)

	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Hash: hash},
		},
	}
	if err := d.Fetch(context.Background(), pin, cas.NewFilesystemStore(t.TempDir())); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	fake.mu.Lock()
	req := fake.lastReq
	fake.mu.Unlock()
	if got := req.Header.Get("Authorization"); got != "Bearer one" {
		t.Fatalf("caller mutation leaked into downloader: got %q", got)
	}
}

func TestGroupURLPathUsesForwardSlashes(t *testing.T) {
	// Cross-platform guarantee: URL paths always use forward slashes,
	// regardless of filepath.Separator on the host OS.
	got := groupURLPath("org.jetbrains.kotlin")
	want := "org/jetbrains/kotlin"
	if got != want {
		t.Fatalf("groupURLPath: got %q want %q", got, want)
	}
}

func TestMavenCentralConstantIsUsable(t *testing.T) {
	// Not making a real network request — just confirm the constant
	// parses cleanly and produces a usable Downloader.
	d, err := New(MavenCentralURL)
	if err != nil {
		t.Fatal(err)
	}
	if d.BaseURL() != MavenCentralURL {
		t.Fatalf("BaseURL not preserved: %s", d.BaseURL())
	}
}

// hasSuffix is a tiny wrapper so the test reads naturally without
// importing strings for a single call.
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
