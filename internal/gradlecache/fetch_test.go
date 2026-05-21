package gradlecache

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/retry"
)

// fastRetry is a retry policy with negligible backoff for tests that
// exercise the retry loop. The 1ms base delay keeps these tests well
// under 100ms even when all attempts are consumed.
var fastRetry = retry.Policy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond, Multiplier: 2.0}

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

func TestHTTPFetcherTreatsPermanent4xxAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()

	fetcher, _ := NewHTTPFetcher(srv.URL+"/", WithRetryPolicy(fastRetry))
	if _, err := fetcher.Fetch(t.TempDir(), "com.example", "lib", "1.0"); err == nil {
		t.Fatal("expected error on 403, got nil")
	}
}

func TestHTTPFetcherRetriesTransient5xxAndSucceeds(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if strings.HasSuffix(r.URL.Path, ".jar") && n <= 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jar") {
			_, _ = w.Write([]byte("late-jar"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	fetcher, _ := NewHTTPFetcher(srv.URL+"/", WithRetryPolicy(fastRetry))
	got, err := fetcher.Fetch(t.TempDir(), "com.example", "lib", "1.0")
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "lib-1.0.jar" {
		t.Fatalf("expected lib-1.0.jar after retry, got %#v", got)
	}
	if attempts < 3 {
		t.Fatalf("expected at least 3 attempts on jar path, got %d", attempts)
	}
}

func TestHTTPFetcherDoesNotRetry404(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	fetcher, _ := NewHTTPFetcher(srv.URL+"/", WithRetryPolicy(fastRetry))
	if _, err := fetcher.Fetch(t.TempDir(), "com.example", "lib", "1.0"); err != nil {
		t.Fatalf("all-404 should be silent, got %v", err)
	}
	// One attempt per file (jar+pom+module), no retries on 404.
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("expected exactly 3 requests (no retries on 404), got %d", got)
	}
}

// headerCapture records the headers seen by a httptest server in a
// race-safe way (the server's handler goroutine and the test goroutine
// otherwise have no happens-before edge after Fetch returns).
type headerCapture struct {
	mu   sync.Mutex
	seen http.Header
}

func (c *headerCapture) record(h http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = h.Clone()
}

func (c *headerCapture) get(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		return ""
	}
	return c.seen.Get(key)
}

func TestHTTPFetcherSendsStaticAndEnvHeaders(t *testing.T) {
	var captured headerCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.record(r.Header)
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()

	t.Setenv("TEST_AUTH_TOKEN", "Bearer abc123")
	fetcher, _ := NewHTTPFetcher(
		srv.URL+"/",
		WithHeaders(map[string]string{"X-Repo-Tag": "internal"}),
		WithEnvHeader("Authorization", "TEST_AUTH_TOKEN"),
	)
	if _, err := fetcher.Fetch(t.TempDir(), "com.example", "lib", "1.0"); err != nil {
		t.Fatal(err)
	}
	if got := captured.get("X-Repo-Tag"); got != "internal" {
		t.Fatalf("static header: got %q want internal", got)
	}
	if got := captured.get("Authorization"); got != "Bearer abc123" {
		t.Fatalf("env header: got %q want Bearer abc123", got)
	}
}

func TestHTTPFetcherEnvHeaderSkippedWhenEnvUnset(t *testing.T) {
	var captured headerCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.record(r.Header)
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()

	if v, ok := os.LookupEnv("TEST_NOPE_TOKEN"); ok && v != "" {
		t.Skipf("env TEST_NOPE_TOKEN already set: %q", v)
	}
	fetcher, _ := NewHTTPFetcher(srv.URL+"/", WithEnvHeader("Authorization", "TEST_NOPE_TOKEN"))
	if _, err := fetcher.Fetch(t.TempDir(), "com.example", "lib", "1.0"); err != nil {
		t.Fatal(err)
	}
	if got := captured.get("Authorization"); got != "" {
		t.Fatalf("expected empty Authorization when env unset, got %q", got)
	}
}

func TestHTTPFetcherConcurrentFetchesToSameDestSerialize(t *testing.T) {
	// We count active concurrent calls to the destDir mutex critical
	// section. Without the mutex, two callers would land inside the
	// section together.
	var (
		active   int32
		maxSeen  int32
		requests int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()

	fetcher, _ := NewHTTPFetcher(srv.URL+"/", WithRetryPolicy(fastRetry))
	destDir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := lockDestDir(destDir + "-probe")
			defer release()
			a := atomic.AddInt32(&active, 1)
			defer atomic.AddInt32(&active, -1)
			for {
				m := atomic.LoadInt32(&maxSeen)
				if a <= m || atomic.CompareAndSwapInt32(&maxSeen, m, a) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&maxSeen); got != 1 {
		t.Fatalf("expected concurrent holders to serialize to 1, observed %d", got)
	}

	// Sanity: a real fetch still works after the mutex is exercised.
	if _, err := fetcher.Fetch(destDir, "com.example", "lib", "1.0"); err != nil {
		t.Fatalf("post-lock fetch: %v", err)
	}
}

func TestMultiRepoFetcherTriesEachUntilNonEmpty(t *testing.T) {
	calls := []string{}
	fa := FetcherFunc(func(destDir, _, _, _ string) ([]string, error) {
		calls = append(calls, "a")
		return nil, nil
	})
	fb := FetcherFunc(func(destDir, _, m, v string) ([]string, error) {
		calls = append(calls, "b")
		dest := filepath.Join(destDir, m+"-"+v+".jar")
		_ = os.WriteFile(dest, []byte("from-b"), 0o644)
		return []string{dest}, nil
	})
	fc := FetcherFunc(func(_, _, _, _ string) ([]string, error) {
		calls = append(calls, "c")
		return nil, nil
	})
	chain := NewMultiRepoFetcher(fa, fb, fc)
	got, err := chain.Fetch(t.TempDir(), "com.example", "lib", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one file from second repo, got %#v", got)
	}
	if strings.Join(calls, ",") != "a,b" {
		t.Fatalf("expected to stop after b, got %v", calls)
	}
}

func TestMultiRepoFetcherPropagatesErrors(t *testing.T) {
	fa := FetcherFunc(func(_, _, _, _ string) ([]string, error) {
		return nil, http.ErrServerClosed
	})
	fb := FetcherFunc(func(_, _, _, _ string) ([]string, error) {
		t.Fatal("should not reach second fetcher after error")
		return nil, nil
	})
	chain := NewMultiRepoFetcher(fa, fb)
	if _, err := chain.Fetch(t.TempDir(), "com.example", "lib", "1.0"); err == nil {
		t.Fatal("expected error propagation, got nil")
	}
}

func TestMultiRepoFetcherCollapsesSingleAndEmpty(t *testing.T) {
	only := FetcherFunc(func(_, _, _, _ string) ([]string, error) { return nil, nil })
	if got := NewMultiRepoFetcher(only); got == nil {
		t.Fatal("single fetcher should pass through, got nil")
	}
	if got := NewMultiRepoFetcher(); got != nil {
		t.Fatalf("empty chain should be nil, got %T", got)
	}
	if got := NewMultiRepoFetcher(nil, nil); got != nil {
		t.Fatalf("all-nil chain should be nil, got %T", got)
	}
}

func TestIsOfflineDefaultsToTestBinary(t *testing.T) {
	t.Setenv("GRIT_OFFLINE", "")
	if !isOffline() {
		t.Fatal("test binaries should default to offline")
	}
	t.Setenv("GRIT_OFFLINE", "0")
	if isOffline() {
		t.Fatal("GRIT_OFFLINE=0 should opt in to network")
	}
	for _, v := range []string{"1", "true", "YES"} {
		t.Setenv("GRIT_OFFLINE", v)
		if !isOffline() {
			t.Fatalf("GRIT_OFFLINE=%q should be offline", v)
		}
	}
}

func TestDefaultFetcherRespectsOffline(t *testing.T) {
	t.Setenv("GRIT_OFFLINE", "1")
	if got := defaultFetcher(); got != nil {
		t.Fatalf("offline should yield nil, got %T", got)
	}
	t.Setenv("GRIT_OFFLINE", "0")
	if got := defaultFetcher(); got == nil {
		t.Fatal("online mode should yield non-nil fetcher")
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
