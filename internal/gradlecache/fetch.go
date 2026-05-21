package gradlecache

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kaeawc/grit/internal/fsutil"
	"github.com/kaeawc/grit/internal/pathutil"
	"github.com/kaeawc/grit/internal/random"
	"github.com/kaeawc/grit/internal/retry"
)

// MavenCentralBaseURL is the canonical Maven Central base URL. The
// trailing slash is required so URL joins land at the right path.
const MavenCentralBaseURL = "https://repo.maven.apache.org/maven2/"

// GoogleMavenBaseURL is the canonical Google Maven repository, where
// AndroidX and Google Play Services artifacts live.
const GoogleMavenBaseURL = "https://dl.google.com/dl/android/maven2/"

// maxArtifactSize caps any single artifact body to 256 MiB so a
// misbehaving mirror can't fill the disk before the atomic rename.
const maxArtifactSize = 256 << 20

// errRemoteNotFound is returned from the inner retry op when the
// remote responded with 404. The Retryable predicate excludes it from
// the retry loop and fetchOne converts it to a silent "no file" result.
var errRemoteNotFound = errors.New("gradlecache: remote artifact not found")

// errTransient flags errors that warrant another retry attempt.
// Network failures and HTTP 5xx/429 wrap this sentinel; permanent
// failures (4xx other than 429) do not.
var errTransient = errors.New("gradlecache: transient remote error")

// defaultRetryPolicy is the policy applied when callers don't override.
// Three attempts with 200ms→400ms→800ms backoff capped at 2s gives the
// caller ~1.4s of wall-clock to recover from a transient blip while
// staying short enough that a sustained outage fails fast.
func defaultRetryPolicy() retry.Policy {
	return retry.Policy{
		MaxAttempts: 3,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    2 * time.Second,
		Multiplier:  2.0,
		Retryable:   func(err error) bool { return errors.Is(err, errTransient) },
	}
}

// HTTPFetcher downloads .jar, .pom, and .module files for a coordinate
// from a Maven-layout HTTP repository, landing them directly in the
// probe's primary-root layout. Missing files (HTTP 404) are skipped
// quietly; transient errors retry per the configured policy.
type HTTPFetcher struct {
	baseURL    *url.URL
	httpClient *http.Client
	headers    map[string]string
	envHeaders []envHeader
	retry      retry.Policy
	executor   *retry.Executor
}

type envHeader struct {
	header string
	envVar string
}

// HTTPFetcherOption configures an HTTPFetcher at construction.
type HTTPFetcherOption func(*HTTPFetcher)

// WithHTTPClient overrides the http.Client used for requests. The
// default has a 30s per-request timeout — long enough for cold pulls
// of fat compiler jars, short enough that a hung remote doesn't stall
// the compile pipeline.
func WithHTTPClient(client *http.Client) HTTPFetcherOption {
	return func(f *HTTPFetcher) {
		if client != nil {
			f.httpClient = client
		}
	}
}

// WithHeaders attaches static request headers (e.g. Authorization for
// a private mirror). Empty values are skipped at request time so
// callers can pass placeholder maps without leaking blank headers.
func WithHeaders(headers map[string]string) HTTPFetcherOption {
	return func(f *HTTPFetcher) {
		if len(headers) == 0 {
			return
		}
		if f.headers == nil {
			f.headers = make(map[string]string, len(headers))
		}
		for k, v := range headers {
			f.headers[k] = v
		}
	}
}

// WithEnvHeader binds an HTTP header to the value of an environment
// variable, resolved at request time. Lets credentials enter via env
// without being captured in the constructor closure.
func WithEnvHeader(header, envVar string) HTTPFetcherOption {
	return func(f *HTTPFetcher) {
		if header == "" || envVar == "" {
			return
		}
		f.envHeaders = append(f.envHeaders, envHeader{header: header, envVar: envVar})
	}
}

// WithRetryPolicy overrides the default retry policy. The supplied
// policy's Retryable predicate is preserved if non-nil; otherwise the
// default predicate (retry only errors wrapping errTransient) is used.
func WithRetryPolicy(policy retry.Policy) HTTPFetcherOption {
	return func(f *HTTPFetcher) {
		if policy.Retryable == nil {
			policy.Retryable = func(err error) bool { return errors.Is(err, errTransient) }
		}
		f.retry = policy
	}
}

// NewHTTPFetcher returns a Fetcher that pulls from baseURL using the
// Maven2 layout. An empty baseURL falls back to Maven Central.
func NewHTTPFetcher(baseURL string, opts ...HTTPFetcherOption) (*HTTPFetcher, error) {
	if baseURL == "" {
		baseURL = MavenCentralBaseURL
	}
	parsed, err := url.Parse(pathutil.EnsureTrailingSlash(baseURL))
	if err != nil {
		return nil, fmt.Errorf("gradlecache: parse base URL: %w", err)
	}
	f := &HTTPFetcher{
		baseURL:    parsed,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		retry:      defaultRetryPolicy(),
		executor:   retry.New(retry.Real{}, random.NewCrypto()),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f, nil
}

// Fetch implements Fetcher. It pulls .jar, .pom, and .module in
// parallel, returning the paths of files that actually landed. A 404
// on any file is treated as "remote does not have it" and silently
// skipped; the first hard error aborts the fetch.
//
// Concurrent calls to Fetch for the same destDir serialize through a
// package-level mutex so two callers don't race on the same atomic
// rename.
func (f *HTTPFetcher) Fetch(destDir, group, module, version string) ([]string, error) {
	if f == nil || destDir == "" || group == "" || module == "" || version == "" {
		return nil, nil
	}
	release := lockDestDir(destDir)
	defer release()

	stem := module + "-" + version
	candidates := []string{stem + ".jar", stem + ".pom", stem + ".module"}
	results := make([]string, len(candidates))
	errs := make([]error, len(candidates))

	var wg sync.WaitGroup
	for i, name := range candidates {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			dest := filepath.Join(destDir, name)
			landed, err := f.fetchOne(dest, group, module, version, name)
			if err != nil {
				errs[i] = err
				return
			}
			if landed {
				results[i] = dest
			}
		}(i, name)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	var out []string
	for _, path := range results {
		if path != "" {
			out = append(out, path)
		}
	}
	return out, nil
}

func (f *HTTPFetcher) fetchOne(dest, group, module, version, name string) (bool, error) {
	if _, err := os.Stat(dest); err == nil {
		return true, nil
	}
	target := f.baseURL.JoinPath(strings.ReplaceAll(group, ".", "/"), module, version, name).String()

	err := f.executor.Do(context.Background(), f.retry, func(ctx context.Context, _ int) error {
		return f.attemptFetch(ctx, dest, target)
	})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, errRemoteNotFound):
		return false, nil
	default:
		return false, err
	}
}

// attemptFetch does one HTTP round-trip. Returns errRemoteNotFound on
// 404 (the caller converts it to a silent skip) and an error wrapping
// errTransient on retryable failures (network errors, 429, 5xx).
func (f *HTTPFetcher) attemptFetch(ctx context.Context, dest, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("gradlecache: build request: %w", err)
	}
	req.Header.Set("User-Agent", "grit-gradlecache/1")
	f.applyHeaders(req.Header)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gradlecache: GET %s: %w: %w", target, err, errTransient)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound:
		return errRemoteNotFound
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("gradlecache: GET %s: %s: %w", target, resp.Status, errTransient)
	default:
		return fmt.Errorf("gradlecache: GET %s: %s", target, resp.Status)
	}

	body := io.LimitReader(resp.Body, maxArtifactSize)
	if err := fsutil.WriteFileAtomicStream(dest, 0o644, func(w io.Writer) error {
		_, copyErr := io.Copy(w, body)
		return copyErr
	}); err != nil {
		return fmt.Errorf("gradlecache: write %s: %w", dest, err)
	}
	return nil
}

func (f *HTTPFetcher) applyHeaders(h http.Header) {
	for k, v := range f.headers {
		if v != "" {
			h.Set(k, v)
		}
	}
	for _, binding := range f.envHeaders {
		if v, ok := os.LookupEnv(binding.envVar); ok && v != "" {
			h.Set(binding.header, v)
		}
	}
}

// destLocks holds one mutex per active destDir. Entries are not
// reclaimed — for a CLI invocation the set is bounded by the
// coordinates resolved during the run, which is small.
var destLocks sync.Map

func lockDestDir(destDir string) func() {
	raw, _ := destLocks.LoadOrStore(destDir, &sync.Mutex{})
	mu := raw.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// multiRepoFetcher tries each child fetcher in order, returning the
// first non-empty result. A child returning an error aborts the
// chain — callers decide whether to treat it as terminal.
type multiRepoFetcher struct {
	fetchers []Fetcher
}

// NewMultiRepoFetcher composes child fetchers into a chain that tries
// each in order. A child returning ([]string{}, nil) is treated as
// "doesn't have it" and the next child is consulted. nil children
// are filtered out; an empty or all-nil set returns nil so callers
// can pass the result directly to Probe.WithFetcher.
func NewMultiRepoFetcher(fetchers ...Fetcher) Fetcher {
	live := make([]Fetcher, 0, len(fetchers))
	for _, f := range fetchers {
		if f != nil {
			live = append(live, f)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	}
	return &multiRepoFetcher{fetchers: live}
}

func (m *multiRepoFetcher) Fetch(destDir, group, module, version string) ([]string, error) {
	for _, child := range m.fetchers {
		out, err := child.Fetch(destDir, group, module, version)
		if err != nil {
			return nil, err
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, nil
}

var (
	sharedFetcherOnce sync.Once
	sharedFetcher     Fetcher
)

// defaultFetcher returns the fetcher used by ProjectProbe when no
// override is supplied. Returns nil when offline mode is set; test
// binaries default to offline unless GRIT_OFFLINE is explicitly set
// to a falsy value, so tests that don't seed every cache miss don't
// accidentally hit the network. The shared fetcher is memoized so
// the http.Client (and its connection pool) is reused across probes.
func defaultFetcher() Fetcher {
	if isOffline() {
		return nil
	}
	sharedFetcherOnce.Do(func() {
		central, _ := NewHTTPFetcher(MavenCentralBaseURL)
		google, _ := NewHTTPFetcher(GoogleMavenBaseURL)
		sharedFetcher = NewMultiRepoFetcher(central, google)
	})
	return sharedFetcher
}

func isOffline() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GRIT_OFFLINE"))) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	}
	return inTestBinary()
}

func inTestBinary() bool {
	return flag.Lookup("test.v") != nil
}

var _ Fetcher = (*HTTPFetcher)(nil)
var _ Fetcher = (*multiRepoFetcher)(nil)
