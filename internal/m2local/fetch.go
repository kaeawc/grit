package m2local

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kaeawc/grit/internal/griterr"
	"github.com/kaeawc/grit/internal/pathutil"
	"github.com/kaeawc/grit/internal/project"
)

var ErrOffline = errors.New("m2local: offline mode")

func (r *Resolver) fetchPOM(coord Coordinate) (string, error) {
	artifactVersion := r.snapshotArtifactVersion(coord, "pom")
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + artifactVersion + ".pom"
	outPath := filepath.Join(r.WorkRoot, ".grit", "metadata", coord.Group, coord.Module, coord.Version, coord.Module+"-"+coord.Version+".pom")
	return r.fetchRemoteFile(coord, relPath, outPath, "pom")
}

func (r *Resolver) fetchModuleMetadata(coord Coordinate) (string, error) {
	artifactVersion := r.snapshotArtifactVersion(coord, "module")
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + artifactVersion + ".module"
	outPath := filepath.Join(r.WorkRoot, ".grit", "metadata", coord.Group, coord.Module, coord.Version, coord.Module+"-"+coord.Version+".module")
	return r.fetchRemoteFile(coord, relPath, outPath, "module metadata")
}

func (r *Resolver) fetchArtifact(coord Coordinate, ext string) (string, error) {
	if ext != ".jar" && ext != ".aar" {
		return "", griterr.Newf(griterr.ErrUnsupported, "artifact extension %q", ext)
	}
	artifactVersion := r.snapshotArtifactVersion(coord, strings.TrimPrefix(ext, "."))
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/" + coord.Module + "-" + artifactVersion + ext
	outPath := filepath.Join(r.moduleBasePath(coord), "downloaded", coord.Module+"-"+coord.Version+ext)
	return r.fetchRemoteFile(coord, relPath, outPath, ext[1:])
}

type snapshotMetadata struct {
	Versioning struct {
		SnapshotVersions []struct {
			Extension string `xml:"extension"`
			Value     string `xml:"value"`
		} `xml:"snapshotVersions>snapshotVersion"`
		Snapshot struct {
			Timestamp   string `xml:"timestamp"`
			BuildNumber string `xml:"buildNumber"`
		} `xml:"snapshot"`
	} `xml:"versioning"`
}

func (r *Resolver) snapshotArtifactVersion(coord Coordinate, ext string) string {
	if !strings.HasSuffix(coord.Version, "-SNAPSHOT") {
		return coord.Version
	}
	if r.Offline {
		return coord.Version
	}
	relPath := strings.ReplaceAll(coord.Group, ".", "/") + "/" + coord.Module + "/" + coord.Version + "/maven-metadata.xml"
	for _, baseURL := range r.remoteRepositoryURLs(coord) {
		resp, err := fetchClient.Get(baseURL + relPath) // #nosec G107 -- repository base URLs come from project settings
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || len(body) == 0 {
			continue
		}
		var metadata snapshotMetadata
		if xml.Unmarshal(body, &metadata) != nil {
			continue
		}
		for _, version := range metadata.Versioning.SnapshotVersions {
			if version.Extension == ext && version.Value != "" {
				return version.Value
			}
		}
		if metadata.Versioning.Snapshot.Timestamp != "" && metadata.Versioning.Snapshot.BuildNumber != "" {
			base := strings.TrimSuffix(coord.Version, "-SNAPSHOT")
			return base + "-" + metadata.Versioning.Snapshot.Timestamp + "-" + metadata.Versioning.Snapshot.BuildNumber
		}
	}
	return coord.Version
}

func (r *Resolver) fetchRemoteFile(coord Coordinate, relPath, outPath, label string) (string, error) {
	if fileExists(outPath) {
		r.recordFetchedSource(outPath, ResolutionMetadataSource{
			Kind:    metadataKindForLabel(label),
			Path:    outPath,
			Fetched: true,
		})
		return outPath, nil
	}
	if r.Offline {
		return "", fmt.Errorf("%w: cannot fetch %s for %s", ErrOffline, label, coordinateID(coord))
	}
	if r.negativeCacheHit(coord, relPath, label) {
		return "", fmt.Errorf("%s not found for %s (cached negative repository lookup)", label, coordinateID(coord))
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}
	transient := false
	for _, baseURL := range r.remoteRepositoryURLs(coord) {
		body, status, err := fetchWithBackoff(baseURL+relPath, fetchRetryAttempts, fetchRetryBaseDelay)
		if err != nil {
			// Network-level error — treat as transient so we don't lock
			// this coord into the negative cache for 24h on a flake.
			transient = true
			continue
		}
		if body == nil {
			if isTransientStatus(status) {
				transient = true
			}
			continue
		}
		if err := verifyChecksumIfAvailable(baseURL+relPath, body); err != nil {
			return "", err
		}
		tmpPath := outPath + ".tmp"
		if err := os.WriteFile(tmpPath, body, 0o644); err != nil {
			return "", err
		}
		if err := os.Rename(tmpPath, outPath); err != nil {
			if !fileExists(outPath) {
				return "", err
			}
			_ = os.Remove(tmpPath)
		}
		r.recordFetchedSource(outPath, ResolutionMetadataSource{
			Kind:          metadataKindForLabel(label),
			Path:          outPath,
			RepositoryURL: baseURL,
			Fetched:       true,
		})
		return outPath, nil
	}
	// Only persist a negative-cache entry when *every* configured
	// repository definitively returned a not-found-style status (404).
	// Transient failures (429 rate-limit, 5xx server error, network
	// flake) get retried on the next invocation; otherwise a single
	// rate-limited fetch would lock the coord out for 24h.
	if !transient {
		_ = r.recordNegativeCache(coord, relPath, label)
	}
	return "", fmt.Errorf("%s not found for %s", label, coordinateID(coord))
}

const (
	fetchRetryAttempts  = 6
	fetchRetryBaseDelay = 500 * time.Millisecond
	fetchRetryMaxDelay  = 8 * time.Second

	// maxConcurrentPerHost bounds how many HTTP requests grit fires at a
	// single repository host at once. The resolver fans out closures
	// (main/test/runtime/desugar) each with an 8-worker pool, so without
	// a cap a large project bursts dozens of parallel requests at Maven
	// Central, which then rate-limits the whole burst with sustained 429
	// responses (observed dropping io.coil-kt:coil-compose entirely). A
	// small per-host cap keeps grit a polite client and makes the retry
	// backoff actually effective.
	maxConcurrentPerHost = 4

	// fetchRequestTimeout bounds a single HTTP request (connect + headers
	// + body). The default http.Get uses http.DefaultClient, which has NO
	// timeout: under rate-limiting a Maven host can leave a connection
	// open indefinitely, hanging the whole build on one stuck request. A
	// bounded timeout turns that hang into a retriable network error.
	fetchRequestTimeout = 30 * time.Second
)

// fetchClient is the shared HTTP client for all remote repository
// fetches. Unlike http.DefaultClient it has a request timeout so a
// rate-limited or slow host can't stall the resolver forever.
var fetchClient = &http.Client{Timeout: fetchRequestTimeout}

// hostSems holds a per-host concurrency semaphore, shared process-wide so
// every resolver and every closure share one budget against a given host.
var (
	hostSemMu sync.Mutex
	hostSems  = map[string]chan struct{}{}
)

func hostSemaphore(host string) chan struct{} {
	hostSemMu.Lock()
	defer hostSemMu.Unlock()
	sem, ok := hostSems[host]
	if !ok {
		sem = make(chan struct{}, maxConcurrentPerHost)
		hostSems[host] = sem
	}
	return sem
}

// fetchWithBackoff GETs url, retrying transient failures (429, 408, 5xx,
// or network errors) with exponential backoff that honors a server
// Retry-After header. Concurrent requests to the same host are capped via
// a per-host semaphore so a burst of parallel fetches doesn't provoke
// sustained rate-limiting in the first place.
//
// Return shape (body, status, err):
//   - (body, 200, nil) on success.
//   - (nil, status, nil) when the server returned a terminal non-OK
//     status (e.g. a definitive 404, or a transient status that survived
//     every retry).
//   - (nil, 0, err) when every attempt failed at the network layer.
func fetchWithBackoff(url string, attempts int, baseDelay time.Duration) ([]byte, int, error) {
	sem := hostSemaphore(hostKey(url))
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		body, status, retryAfter, err := fetchOnce(url, sem)
		if err == nil && status == http.StatusOK {
			return body, status, nil
		}
		retriable := err != nil || isTransientStatus(status)
		if !retriable || attempt+1 >= attempts {
			if err != nil {
				return nil, 0, err
			}
			return nil, status, nil
		}
		if err != nil {
			lastErr = err
		}
		delay := retryAfter
		if delay <= 0 {
			delay = backoffDelay(attempt, baseDelay, fetchRetryMaxDelay)
		}
		time.Sleep(delay)
	}
	return nil, 0, lastErr
}

// fetchOnce performs a single throttled GET. It holds the host semaphore
// only for the duration of the request + body read, never across the
// retry sleep, so a backing-off request doesn't waste a concurrency slot.
func fetchOnce(url string, sem chan struct{}) (body []byte, status int, retryAfter time.Duration, err error) {
	sem <- struct{}{}
	defer func() { <-sem }()
	resp, err := fetchClient.Get(url) // #nosec G107 -- repository base URLs come from project settings
	if err != nil {
		return nil, 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, 0, 0, readErr
		}
		return b, resp.StatusCode, 0, nil
	}
	ra := parseRetryAfter(resp.Header.Get("Retry-After"))
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil, resp.StatusCode, ra, nil
}

// backoffDelay returns an exponentially-increasing delay (base, 2·base,
// 4·base, …) clamped to max. attempt is 0-based: the delay used after the
// first failed attempt is base.
func backoffDelay(attempt int, base, max time.Duration) time.Duration {
	if attempt < 0 || base <= 0 {
		return base
	}
	d := base
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= max {
			return max
		}
	}
	if d > max {
		return max
	}
	return d
}

// parseRetryAfter interprets an HTTP Retry-After header value, which is
// either a number of seconds or an HTTP-date. Returns 0 when absent or
// unparseable. The result is clamped to fetchRetryMaxDelay so a hostile
// or buggy header can't stall a build indefinitely.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0
		}
		return clampDelay(time.Duration(secs) * time.Second)
	}
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		return clampDelay(d)
	}
	return 0
}

func clampDelay(d time.Duration) time.Duration {
	if d > fetchRetryMaxDelay {
		return fetchRetryMaxDelay
	}
	return d
}

func hostKey(rawURL string) string {
	if u, err := neturl.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return rawURL
}

func isTransientStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func metadataKindForLabel(label string) string {
	switch label {
	case "module metadata":
		return "module"
	case "pom":
		return "pom"
	default:
		return label
	}
}

func (r *Resolver) recordFetchedSource(path string, source ResolutionMetadataSource) {
	if path == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetched == nil {
		r.fetched = map[string]ResolutionMetadataSource{}
	}
	r.fetched[path] = source
}

func (r *Resolver) metadataSourceForPath(path, kind string) *ResolutionMetadataSource {
	if path == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if source, ok := r.fetched[path]; ok {
		copy := source
		if copy.Kind == "" {
			copy.Kind = kind
		}
		if copy.Path == "" {
			copy.Path = path
		}
		return &copy
	}
	return &ResolutionMetadataSource{
		Kind: kind,
		Path: path,
	}
}

func (r *Resolver) remoteRepositoryURLs(coord Coordinate) []string {
	var urls []string
	for _, repo := range r.Repositories {
		if repo.Scope != "dependency" && repo.Scope != "" {
			continue
		}
		if repo.Kind == "mavenLocal" || repo.URL == "" {
			continue
		}
		if !repoAllowsCoordinate(repo, coord) {
			continue
		}
		urls = append(urls, pathutil.EnsureTrailingSlash(repo.URL))
	}
	if len(urls) == 0 && r.AllowRepositoryFallback {
		urls = append(urls, "https://dl.google.com/dl/android/maven2/", "https://repo1.maven.org/maven2/")
	}
	return mergeUnique(nil, urls)
}

func (r *Resolver) negativeCachePath(coord Coordinate, relPath, label string) string {
	key := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(coordinateID(coord) + "_" + label + "_" + relPath)
	return filepath.Join(r.WorkRoot, ".grit", "metadata", "missing", key+".missing")
}

func (r *Resolver) negativeCacheHit(coord Coordinate, relPath, label string) bool {
	path := r.negativeCachePath(coord, relPath, label)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 24*time.Hour
}

func (r *Resolver) recordNegativeCache(coord Coordinate, relPath, label string) error {
	path := r.negativeCachePath(coord, relPath, label)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(coordinateID(coord)+" "+label+"\n"), 0o644)
}

func verifyChecksumIfAvailable(url string, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	for _, suffix := range []string{".sha256", ".sha1"} {
		resp, err := fetchClient.Get(url + suffix) // #nosec G107 -- checksum sidecar of an already-validated repo URL
		if err != nil {
			continue
		}
		checksum, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		fields := strings.Fields(string(checksum))
		if len(fields) == 0 {
			continue
		}
		switch suffix {
		case ".sha256":
			sum := sha256.Sum256(body)
			if !strings.EqualFold(hex.EncodeToString(sum[:]), fields[0]) {
				return fmt.Errorf("checksum mismatch for %s", url)
			}
		case ".sha1":
			sum := sha1.Sum(body)
			if !strings.EqualFold(hex.EncodeToString(sum[:]), fields[0]) {
				return fmt.Errorf("checksum mismatch for %s", url)
			}
		}
		return nil
	}
	return nil
}

func repoAllowsCoordinate(repo project.Repository, coord Coordinate) bool {
	moduleID := coord.Group + ":" + coord.Module
	for _, excluded := range repo.ExcludeModules {
		if excluded == moduleID {
			return false
		}
	}
	for _, excluded := range repo.ExcludeGroups {
		if excluded == coord.Group {
			return false
		}
	}
	for _, pattern := range repo.ExcludeGroupRegex {
		if regexp.MustCompile(pattern).MatchString(coord.Group) {
			return false
		}
	}

	hasIncludeRules := len(repo.IncludeModules) > 0 || len(repo.IncludeGroups) > 0 || len(repo.IncludeGroupRegex) > 0
	if !hasIncludeRules {
		return true
	}
	for _, included := range repo.IncludeModules {
		if included == moduleID {
			return true
		}
	}
	for _, included := range repo.IncludeGroups {
		if included == coord.Group {
			return true
		}
	}
	for _, pattern := range repo.IncludeGroupRegex {
		if regexp.MustCompile(pattern).MatchString(coord.Group) {
			return true
		}
	}
	return false
}
