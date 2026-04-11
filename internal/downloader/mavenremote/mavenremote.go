// Package mavenremote implements a Layer 2 downloader that fetches
// artifacts from a remote Maven repository over HTTP.
//
// The downloader is the first Layer 2 adapter in grit that actually
// touches the network. It is a thin translator between lockfile pins and
// HTTP GET requests: for each file in a pin, it constructs the Maven2
// layout URL (or uses the pin-provided URL if set), issues a GET, and
// streams the response body through store.PutExpected so the declared
// content hash is verified before the bytes are committed to the CAS.
//
// Retries, backoff, and credential providers are deliberately not
// implemented here. Retry is a composition concern; credentials beyond
// static headers belong in a separate authentication surface. See
// roadmap/planning/remote-artifact-fetch.md for the broader roadmap.
package mavenremote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
)

// MavenCentralURL is the canonical Maven Central base URL. Provided as a
// convenience constant so callers can do
// mavenremote.New(mavenremote.MavenCentralURL) for the most common case.
const MavenCentralURL = "https://repo.maven.apache.org/maven2/"

// GoogleMavenURL is the canonical Google Maven repository base URL,
// used for AndroidX and Google Play Services artifacts.
const GoogleMavenURL = "https://dl.google.com/dl/android/maven2/"

// DefaultID is the downloader identifier recorded in provenance when the
// caller does not override it via WithID.
const DefaultID = "maven-remote"

const userAgent = "grit-mavenremote/1"

// ErrNotFound is an alias for the shared downloader.ErrNotFound sentinel,
// retained for convenience so mavenremote callers can write
// errors.Is(err, mavenremote.ErrNotFound) without importing the
// downloader package. Fall-through in chain aggregators checks the
// shared sentinel, so both names resolve identically.
var ErrNotFound = downloader.ErrNotFound

// ErrOffline indicates Fetch was called on a downloader configured with
// WithOffline(true). No network request is attempted.
var ErrOffline = errors.New("mavenremote: offline mode")

// Downloader fetches artifacts from one Maven-layout HTTP repository.
// Callers composing multiple remote sources (Maven Central + Google +
// Artifactory) construct one Downloader per source.
type Downloader struct {
	baseURL    *url.URL
	httpClient *http.Client
	id         string
	headers    map[string]string
	offline    bool
}

// Option configures a Downloader at construction.
type Option func(*Downloader)

// WithHTTPClient overrides the http.Client used for requests. The default
// is http.DefaultClient; production callers should supply a client with
// sane timeouts.
func WithHTTPClient(hc *http.Client) Option {
	return func(d *Downloader) {
		if hc != nil {
			d.httpClient = hc
		}
	}
}

// WithID overrides the downloader identifier recorded in provenance.
// The default is DefaultID. Callers that stand up multiple remote
// downloaders should give each one a distinct ID (e.g. "maven-central",
// "google-maven", "artifactory-internal") so provenance records can
// distinguish them.
func WithID(id string) Option {
	return func(d *Downloader) {
		if id != "" {
			d.id = id
		}
	}
}

// WithHeaders supplies static headers that are added to every request.
// Typical use is an Authorization header for private repositories. The
// map is copied at construction; later mutations by the caller do not
// affect the downloader.
func WithHeaders(headers map[string]string) Option {
	return func(d *Downloader) {
		if len(headers) == 0 {
			return
		}
		out := make(map[string]string, len(headers))
		for k, v := range headers {
			out[k] = v
		}
		d.headers = out
	}
}

// WithOffline configures the downloader to refuse every Fetch call with
// ErrOffline. This is useful for CI modes that must fail fast rather
// than block on the network.
func WithOffline(offline bool) Option {
	return func(d *Downloader) { d.offline = offline }
}

// New returns a Downloader rooted at baseURL. baseURL must be absolute
// and should point at a Maven2-layout repository root, typically ending
// with a trailing slash (a missing trailing slash is tolerated via
// url.URL.JoinPath).
func New(baseURL string, opts ...Option) (*Downloader, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("mavenremote: empty baseURL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("mavenremote: parse baseURL: %w", err)
	}
	if !u.IsAbs() {
		return nil, fmt.Errorf("mavenremote: baseURL must be absolute, got %q", baseURL)
	}
	d := &Downloader{
		baseURL:    u,
		httpClient: http.DefaultClient,
		id:         DefaultID,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d, nil
}

// BaseURL returns the repository base URL this downloader targets.
func (d *Downloader) BaseURL() string {
	return d.baseURL.String()
}

// ID implements downloader.Downloader.
func (d *Downloader) ID() string {
	return d.id
}

// Fetch materializes every file declared in pin into store. For each
// file Fetch computes the target URL (either pin.File.URL or the
// coordinate-derived Maven2 layout URL), issues a GET, and streams the
// response body through store.PutExpected so the content hash is
// verified before bytes commit. Fetch is idempotent: files already
// present in the store are not re-fetched.
func (d *Downloader) Fetch(ctx context.Context, pin lockfile.Pin, store cas.Store) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.offline {
		return fmt.Errorf("%w: cannot fetch %s", ErrOffline, pin.Coordinate)
	}
	for _, file := range pin.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		has, err := store.Has(ctx, file.Hash)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if err := d.fetchFile(ctx, pin, file, store); err != nil {
			return err
		}
	}
	return nil
}

func (d *Downloader) fetchFile(ctx context.Context, pin lockfile.Pin, file lockfile.PinFile, store cas.Store) error {
	target := d.targetURL(pin, file)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("mavenremote: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range d.headers {
		req.Header.Set(k, v)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mavenremote: GET %s: %w", target, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return fmt.Errorf("%w: GET %s", ErrNotFound, target)
	default:
		return fmt.Errorf("mavenremote: GET %s: %s", target, resp.Status)
	}

	prov := cas.Provenance{
		Source: cas.Source{
			Kind: cas.SourceDownload,
			Download: &cas.DownloadSource{
				Downloader:   d.id,
				RepositoryID: pin.RepositoryID,
				Coordinate:   pin.Coordinate.String(),
				URL:          target,
			},
		},
		Attributes: map[string]string{
			"file.kind": string(file.Kind),
			"file.name": file.Name,
		},
	}
	if _, err := store.PutExpected(ctx, resp.Body, file.Hash, prov); err != nil {
		return fmt.Errorf("mavenremote: ingest %s: %w", target, err)
	}
	return nil
}

// targetURL returns the URL the downloader will GET for this file. If
// the pin file has an explicit URL, it wins; otherwise the URL is
// constructed from the coordinate using Maven2 layout.
func (d *Downloader) targetURL(pin lockfile.Pin, file lockfile.PinFile) string {
	if file.URL != "" {
		return file.URL
	}
	return d.baseURL.JoinPath(
		groupURLPath(pin.Coordinate.Group),
		pin.Coordinate.Artifact,
		pin.Coordinate.Version,
		file.Name,
	).String()
}

// groupURLPath converts a dotted Maven group ID to a URL path segment.
// URL paths use forward slashes regardless of the host OS, so this
// helper is distinct from the filesystem-layout GroupPath in the
// Maven read adapter.
func groupURLPath(group string) string {
	return strings.ReplaceAll(group, ".", "/")
}

// Compile-time assertion that *Downloader satisfies downloader.Downloader.
var _ downloader.Downloader = (*Downloader)(nil)
