package gradlecache

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPFetcherLandsJarAndPom(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		switch r.URL.Path {
		case "/com/example/lib/1.0/lib-1.0.jar":
			_, _ = w.Write([]byte("jar-body"))
		case "/com/example/lib/1.0/lib-1.0.pom":
			_, _ = w.Write([]byte("<project/>"))
		case "/com/example/lib/1.0/lib-1.0.module":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	fetcher, err := NewHTTPFetcher(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()
	got, err := fetcher.Fetch(destDir, "com.example", "lib", "1.0")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	sort.Strings(got)
	want := []string{
		filepath.Join(destDir, "lib-1.0.jar"),
		filepath.Join(destDir, "lib-1.0.pom"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %#v want %#v", got, want)
	}
	if data, err := os.ReadFile(want[0]); err != nil || string(data) != "jar-body" {
		t.Fatalf("jar body: %v / %q", err, data)
	}
}

func TestHTTPFetcherSkipsAlreadyLanded(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()

	fetcher, _ := NewHTTPFetcher(srv.URL + "/")
	destDir := t.TempDir()
	// Pre-seed all three target files so Fetch should skip every request.
	for _, name := range []string{"lib-1.0.jar", "lib-1.0.pom", "lib-1.0.module"} {
		if err := os.WriteFile(filepath.Join(destDir, name), []byte("local"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fetcher.Fetch(destDir, "com.example", "lib", "1.0"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("expected no HTTP requests when files exist, got %d", got)
	}
}

func TestHTTPFetcherTreatsNon404AsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	fetcher, _ := NewHTTPFetcher(srv.URL + "/")
	if _, err := fetcher.Fetch(t.TempDir(), "com.example", "lib", "1.0"); err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestHTTPFetcherReturnsEmptyWhenEverythingIs404(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	fetcher, _ := NewHTTPFetcher(srv.URL + "/")
	got, err := fetcher.Fetch(t.TempDir(), "com.example", "lib", "1.0")
	if err != nil {
		t.Fatalf("expected nil error on all-404, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no landed files, got %#v", got)
	}
}

func TestNewHTTPFetcherDefaultsToMavenCentral(t *testing.T) {
	fetcher, err := NewHTTPFetcher("")
	if err != nil {
		t.Fatal(err)
	}
	if got := fetcher.baseURL.String(); got != MavenCentralBaseURL {
		t.Fatalf("default base URL: got %q want %q", got, MavenCentralBaseURL)
	}
}
