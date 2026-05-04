package mavenremote

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

type fakePublishMaven struct {
	mu       sync.Mutex
	files    map[string][]byte
	headers  map[string]http.Header
	requests int
	status   int
}

func newFakePublishMaven() *fakePublishMaven {
	return &fakePublishMaven{
		files:   map[string][]byte{},
		headers: map[string]http.Header{},
		status:  http.StatusCreated,
	}
}

func (f *fakePublishMaven) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests++
		data, _ := io.ReadAll(r.Body)
		f.files[r.URL.Path] = data
		f.headers[r.URL.Path] = r.Header.Clone()
		w.WriteHeader(f.status)
	})
}

func (f *fakePublishMaven) body(path string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.files[path]...)
}

func (f *fakePublishMaven) header(path, key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.headers[path].Get(key)
}

func startFakePublishMaven(t *testing.T) (*Publisher, *fakePublishMaven, func()) {
	t.Helper()
	fake := newFakePublishMaven()
	ts := httptest.NewServer(fake.handler())
	p, err := New(ts.URL + "/")
	if err != nil {
		ts.Close()
		t.Fatalf("New: %v", err)
	}
	return p, fake, ts.Close
}

func TestNewRejectsBadURL(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatalf("expected error for empty URL")
	}
	if _, err := New("relative/path"); err == nil {
		t.Fatalf("expected error for relative URL")
	}
}

func TestPublishPinUploadsArtifactsAndGeneratedMetadata(t *testing.T) {
	p, fake, cleanup := startFakePublishMaven(t)
	defer cleanup()

	ctx := context.Background()
	store := cas.NewFilesystemStore(t.TempDir())
	jarBytes := []byte("published jar bytes")
	jarHash := cas.HashBytes(jarBytes)
	if _, err := store.PutBytes(ctx, jarBytes, cas.Provenance{}); err != nil {
		t.Fatal(err)
	}

	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.2.3"},
		Files: []lockfile.PinFile{{
			Kind: lockfile.FileKindPrimary,
			Name: "demo-1.2.3.jar",
			Hash: jarHash,
			Size: int64(len(jarBytes)),
		}},
		Dependencies: []lockfile.Coordinate{{
			Group: "org.example.dep", Artifact: "helper", Version: "4.5.6",
		}},
	}

	if err := p.PublishPin(ctx, pin, store); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}

	jarPath := "/org/example/demo/1.2.3/demo-1.2.3.jar"
	if got := string(fake.body(jarPath)); got != string(jarBytes) {
		t.Fatalf("jar body mismatch: %q", got)
	}
	sha1Digest := sha1.Sum(jarBytes)
	if got := string(fake.body(jarPath + ".sha1")); got != hex.EncodeToString(sha1Digest[:]) {
		t.Fatalf("sha1 sidecar mismatch: %q", got)
	}
	md5Digest := md5.Sum(jarBytes)
	if got := string(fake.body(jarPath + ".md5")); got != hex.EncodeToString(md5Digest[:]) {
		t.Fatalf("md5 sidecar mismatch: %q", got)
	}
	pomPath := "/org/example/demo/1.2.3/demo-1.2.3.pom"
	if body := string(fake.body(pomPath)); !strings.Contains(body, "<artifactId>demo</artifactId>") || !strings.Contains(body, "<dependency>") {
		t.Fatalf("expected generated pom upload, got %q", body)
	}
	modulePath := "/org/example/demo/1.2.3/demo-1.2.3.module"
	if body := string(fake.body(modulePath)); !strings.Contains(body, "\"formatVersion\": \"1.1\"") {
		t.Fatalf("expected generated module upload, got %q", body)
	}
}

func TestPublishPinSkipsGeneratedFilesWhenPinAlreadyCarriesThem(t *testing.T) {
	p, fake, cleanup := startFakePublishMaven(t)
	defer cleanup()

	ctx := context.Background()
	store := cas.NewFilesystemStore(t.TempDir())
	put := func(data []byte) cas.Hash {
		t.Helper()
		info, err := store.PutBytes(ctx, data, cas.Provenance{})
		if err != nil {
			t.Fatal(err)
		}
		return info.Hash
	}
	pomBytes := []byte("<project/>")
	moduleBytes := []byte("{}\n")
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.0.jar", Hash: put([]byte("jar")), Size: 3},
			{Kind: lockfile.FileKindPOM, Name: "demo-1.0.0.pom", Hash: put(pomBytes), Size: int64(len(pomBytes))},
			{Kind: lockfile.FileKindModule, Name: "demo-1.0.0.module", Hash: put(moduleBytes), Size: int64(len(moduleBytes))},
		},
	}

	if err := p.PublishPin(ctx, pin, store); err != nil {
		t.Fatal(err)
	}
	if got := string(fake.body("/org/example/demo/1.0.0/demo-1.0.0.pom")); got != string(pomBytes) {
		t.Fatalf("expected pinned pom bytes, got %q", got)
	}
	if got := string(fake.body("/org/example/demo/1.0.0/demo-1.0.0.module")); got != string(moduleBytes) {
		t.Fatalf("expected pinned module bytes, got %q", got)
	}
}

func TestPublishPinSendsStaticAndEnvHeaders(t *testing.T) {
	fake := newFakePublishMaven()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()
	t.Setenv("MAVEN_PUBLISH_TOKEN", "secret-token")

	p, err := New(ts.URL+"/",
		WithHeaders(map[string]string{"Authorization": "Bearer static"}),
		WithEnvHeader("X-Env-Token", "MAVEN_PUBLISH_TOKEN"),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	store := cas.NewFilesystemStore(t.TempDir())
	info, err := store.PutBytes(ctx, []byte("jar"), cas.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Files:      []lockfile.PinFile{{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.0.jar", Hash: info.Hash, Size: info.Size}},
	}
	if err := p.PublishPin(ctx, pin, store); err != nil {
		t.Fatal(err)
	}
	jarPath := "/org/example/demo/1.0.0/demo-1.0.0.jar"
	if got := fake.header(jarPath, "Authorization"); got != "Bearer static" {
		t.Fatalf("expected static auth header, got %q", got)
	}
	if got := fake.header(jarPath, "X-Env-Token"); got != "secret-token" {
		t.Fatalf("expected env header, got %q", got)
	}
}

func TestPublishPinOfflineFailsFast(t *testing.T) {
	p, err := New("https://repo.example.test/", WithOffline(true))
	if err != nil {
		t.Fatal(err)
	}
	err = p.PublishPin(context.Background(), lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "g", Artifact: "a", Version: "1"},
	}, cas.NewFilesystemStore(t.TempDir()))
	if err == nil || !errors.Is(err, ErrOffline) {
		t.Fatalf("expected offline error, got %v", err)
	}
}

func TestPublishPinRedactsCredentialsInErrors(t *testing.T) {
	fake := newFakePublishMaven()
	fake.status = http.StatusUnauthorized
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("alice", "secret")
	p, err := New(u.String() + "/")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	store := cas.NewFilesystemStore(t.TempDir())
	info, err := store.PutBytes(ctx, []byte("jar"), cas.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	err = p.PublishPin(ctx, lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Files:      []lockfile.PinFile{{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.0.jar", Hash: info.Hash, Size: info.Size}},
	}, store)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "alice") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked credentials: %v", err)
	}
}
