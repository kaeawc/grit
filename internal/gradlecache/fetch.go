package gradlecache

import (
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
)

// MavenCentralBaseURL is the canonical Maven Central base URL. The
// trailing slash is required so URL joins land at the right path.
const MavenCentralBaseURL = "https://repo.maven.apache.org/maven2/"

// maxArtifactSize caps any single artifact body to 256 MiB so a
// misbehaving mirror can't fill the disk before the atomic rename.
const maxArtifactSize = 256 << 20

// HTTPFetcher downloads .jar, .pom, and .module files for a coordinate
// from a Maven-layout HTTP repository, landing them directly in the
// probe's primary-root layout. Missing files (HTTP 404) are skipped
// quietly so a coordinate that has a .jar but no .module still
// resolves; any other transport error aborts the fetch.
type HTTPFetcher struct {
	baseURL    *url.URL
	httpClient *http.Client
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
	}
	for _, opt := range opts {
		opt(f)
	}
	return f, nil
}

// Fetch implements Fetcher. It pulls .jar, .pom, and .module in
// parallel, returning the paths of files that actually landed. A 404
// on any file is treated as "remote does not have it" and silently
// skipped; the first hard error aborts the fetch and discards any
// concurrently-completed files (atomic rename means partials don't
// leak onto disk past the failure boundary).
func (f *HTTPFetcher) Fetch(destDir, group, module, version string) ([]string, error) {
	if f == nil || destDir == "" || group == "" || module == "" || version == "" {
		return nil, nil
	}
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
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return false, fmt.Errorf("gradlecache: build request: %w", err)
	}
	req.Header.Set("User-Agent", "grit-gradlecache/1")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("gradlecache: GET %s: %w", target, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("gradlecache: GET %s: %s", target, resp.Status)
	}

	body := io.LimitReader(resp.Body, maxArtifactSize)
	if err := fsutil.WriteFileAtomicStream(dest, 0o644, func(w io.Writer) error {
		_, copyErr := io.Copy(w, body)
		return copyErr
	}); err != nil {
		return false, fmt.Errorf("gradlecache: write %s: %w", dest, err)
	}
	return true, nil
}

var _ Fetcher = (*HTTPFetcher)(nil)
