package m2local

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
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

	body, status, err := fetchWithBackoff(srv.URL, 4, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchWithBackoff: %v", err)
	}
	if body == nil {
		t.Fatalf("expected non-nil body after retries, got status=%d", status)
	}
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

	body, status, err := fetchWithBackoff(srv.URL, 3, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchWithBackoff: %v", err)
	}
	if body != nil {
		t.Fatalf("expected nil body when transient persists, got %q (status=%d)", body, status)
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

	body, status, err := fetchWithBackoff(srv.URL, 3, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchWithBackoff: %v", err)
	}
	if body != nil {
		t.Fatalf("expected nil body on 404, got %q", body)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status: got %d want %d", status, http.StatusNotFound)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts: got %d want 1 (404 should not retry)", got)
	}
}

func TestBackoffDelayIsExponentialAndCapped(t *testing.T) {
	base := 500 * time.Millisecond
	max := 8 * time.Second
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 8 * time.Second}, // capped
		{50, 8 * time.Second},
	}
	for _, tc := range cases {
		if got := backoffDelay(tc.attempt, base, max); got != tc.want {
			t.Errorf("backoffDelay(%d): got %v want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestParseRetryAfterSecondsAndDate(t *testing.T) {
	if got := parseRetryAfter("2"); got != 2*time.Second {
		t.Errorf("seconds: got %v want 2s", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty: got %v want 0", got)
	}
	if got := parseRetryAfter("0"); got != 0 {
		t.Errorf("zero: got %v want 0", got)
	}
	if got := parseRetryAfter("not-a-number"); got != 0 {
		t.Errorf("garbage: got %v want 0", got)
	}
	// Clamped to the max delay.
	if got := parseRetryAfter("99999"); got != fetchRetryMaxDelay {
		t.Errorf("oversized: got %v want %v", got, fetchRetryMaxDelay)
	}
	// HTTP-date in the near future resolves to a positive, clamped delay.
	future := time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > fetchRetryMaxDelay {
		t.Errorf("http-date: got %v, want (0, %v]", got, fetchRetryMaxDelay)
	}
	// HTTP-date in the past resolves to 0 (no wait).
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("past http-date: got %v want 0", got)
	}
}

func TestFetchWithBackoffHonorsRetryAfterHeader(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0") // 0 → fall back to base delay, but exercises the parse path
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	body, _, err := fetchWithBackoff(srv.URL, 3, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchWithBackoff: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body: got %q want ok", body)
	}
}

// TestFetchWithBackoffCapsConcurrencyPerHost is the core rate-limit fix:
// no matter how many goroutines fire fetchWithBackoff at one host, at most
// maxConcurrentPerHost requests are in flight simultaneously.
func TestFetchWithBackoffCapsConcurrencyPerHost(t *testing.T) {
	var inflight atomic.Int32
	var peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cur := inflight.Add(1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		inflight.Add(-1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	const callers = 24
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = fetchWithBackoff(srv.URL, 1, time.Millisecond)
		}()
	}
	wg.Wait()

	if got := peak.Load(); got > maxConcurrentPerHost {
		t.Fatalf("peak concurrency %d exceeded cap %d", got, maxConcurrentPerHost)
	}
	if peak.Load() == 0 {
		t.Fatal("expected some concurrency to be observed")
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
