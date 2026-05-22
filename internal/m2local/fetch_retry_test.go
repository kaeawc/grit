package m2local

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/project"
)

func repoFromURL(name, url string) project.Repository {
	return project.Repository{Name: name, Kind: "mavenCentral", URL: url, Scope: "dependency"}
}

func TestIsTransientStatusClassifiesRetriableCodes(t *testing.T) {
	cases := map[int]bool{
		http.StatusOK:                  false,
		http.StatusNotFound:            false,
		http.StatusForbidden:           false,
		http.StatusTooManyRequests:     true,
		http.StatusRequestTimeout:      true,
		http.StatusServiceUnavailable:  true,
		http.StatusBadGateway:          true,
		http.StatusGatewayTimeout:      true,
		http.StatusInternalServerError: false, // 500 is upstream-buggy, not transient
	}
	for status, want := range cases {
		if got := isTransientStatus(status); got != want {
			t.Errorf("isTransientStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestFetchWithBackoffRetries429ThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	resp, status, err := fetchWithBackoff(srv.URL, 4, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchWithBackoff: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil resp after retries, got status=%d", status)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "ok" {
		t.Fatalf("body: got %q want %q", body, "ok")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts: got %d want 3", got)
	}
}

func TestFetchWithBackoffGivesUpOnRepeatedTransient(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	resp, status, err := fetchWithBackoff(srv.URL, 3, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchWithBackoff: %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
		t.Fatalf("expected nil resp when transient persists, got resp with status=%d", status)
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("status: got %d want %d", status, http.StatusTooManyRequests)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts: got %d want 3 (exhausted retries)", got)
	}
}

func TestFetchWithBackoffStopsImmediatelyOn404(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	resp, status, err := fetchWithBackoff(srv.URL, 3, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchWithBackoff: %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
		t.Fatalf("expected nil resp on 404")
	}
	if status != http.StatusNotFound {
		t.Fatalf("status: got %d want %d", status, http.StatusNotFound)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts: got %d want 1 (404 should not retry)", got)
	}
}

// TestFetchRemoteFileSkipsNegativeCacheOnTransient verifies the contract
// at the fetchRemoteFile level: when every configured repo returned a
// transient status (e.g. 429 rate-limit), the .missing negative-cache
// file is NOT written, so the next invocation tries again. Without this
// guard a single rate-limited fetch would lock the coord out for 24h.
func TestFetchRemoteFileSkipsNegativeCacheOnTransient(t *testing.T) {
	// Build a server that always returns 429.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	r := New(t.TempDir(), t.TempDir(), nil, nil)
	r.Repositories = append(r.Repositories, repoFromURL("test-429", srv.URL+"/"))

	coord := Coordinate{Group: "g", Module: "m", Version: "1.0.0"}
	_, err := r.fetchModuleMetadata(coord)
	if err == nil {
		t.Fatalf("expected error from all-transient repo")
	}

	// Negative-cache file must NOT exist.
	if r.negativeCacheHit(coord, "g/m/1.0.0/m-1.0.0.module", "module metadata") {
		t.Fatalf("transient 429 must not poison the negative cache")
	}
}

// TestFetchRemoteFileWritesNegativeCacheOn404 keeps the existing
// behavior for genuine not-found responses.
func TestFetchRemoteFileWritesNegativeCacheOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	r := New(t.TempDir(), t.TempDir(), nil, nil)
	r.Repositories = append(r.Repositories, repoFromURL("test-404", srv.URL+"/"))

	coord := Coordinate{Group: "g", Module: "m", Version: "1.0.0"}
	_, err := r.fetchModuleMetadata(coord)
	if err == nil {
		t.Fatalf("expected error from all-404 repo")
	}
	if !r.negativeCacheHit(coord, "g/m/1.0.0/m-1.0.0.module", "module metadata") {
		t.Fatalf("404 should populate the negative cache")
	}
}
