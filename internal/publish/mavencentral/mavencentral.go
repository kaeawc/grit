// Package mavencentral implements a Layer 5 publish adapter that uploads
// Maven-layout artifacts to a Maven Central staging repository via the
// OSSRH Nexus or Central Portal API.
package mavencentral

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const userAgent = "grit-mavencentral/1"

// Artifact describes a single file to upload into a staging repository.
type Artifact struct {
	// GroupID is the Maven groupId (e.g. "org.example").
	GroupID string
	// ArtifactID is the Maven artifactId (e.g. "demo").
	ArtifactID string
	// Version is the Maven version string (e.g. "1.2.3").
	Version string
	// Filename is the basename of the file (e.g. "demo-1.2.3.jar").
	Filename string
	// Body is the file content to upload.
	Body io.Reader
}

// AuthConfig holds credentials for authenticating with the staging API.
type AuthConfig struct {
	Username string
	Password string
	// Token is used for bearer-token authentication (Central Portal).
	// When set, Username/Password are ignored.
	Token string
}

// CentralClient communicates with a Maven Central staging repository.
type CentralClient struct {
	BaseURL    string
	Auth       AuthConfig
	HTTPClient *http.Client
}

// Option configures a CentralClient at construction.
type Option func(*CentralClient)

// WithHTTPClient overrides the http.Client used for requests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *CentralClient) {
		if hc != nil {
			c.HTTPClient = hc
		}
	}
}

// New returns a CentralClient targeting baseURL.
func New(baseURL string, auth AuthConfig, opts ...Option) (*CentralClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("mavencentral: empty baseURL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("mavencentral: parse baseURL: %w", err)
	}
	if !u.IsAbs() {
		return nil, fmt.Errorf("mavencentral: baseURL must be absolute, got %q", baseURL)
	}
	c := &CentralClient{
		BaseURL:    u.String(),
		Auth:       auth,
		HTTPClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Upload uploads a single artifact into the given staging repository.
// The artifact body is PUT to the standard Nexus staging deploy path:
//
//	{baseURL}/staging/deployByRepositoryId/{stagingID}/{group}/{artifact}/{version}/{filename}
func (c *CentralClient) Upload(ctx context.Context, stagingID string, artifact Artifact) error {
	if stagingID == "" {
		return fmt.Errorf("mavencentral upload: empty stagingID")
	}
	if artifact.GroupID == "" || artifact.ArtifactID == "" || artifact.Version == "" || artifact.Filename == "" {
		return fmt.Errorf("mavencentral upload: incomplete artifact coordinates")
	}
	if artifact.Body == nil {
		return fmt.Errorf("mavencentral upload: nil body")
	}

	target := c.uploadURL(stagingID, artifact)
	body, err := io.ReadAll(artifact.Body)
	if err != nil {
		return fmt.Errorf("mavencentral upload: read body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mavencentral upload: build request: %w", err)
	}
	c.applyAuth(req)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("mavencentral upload: PUT %s: %w", redactURL(target), err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	default:
		return fmt.Errorf("mavencentral upload: PUT %s: %s", redactURL(target), resp.Status)
	}
}

func (c *CentralClient) uploadURL(stagingID string, a Artifact) string {
	u, _ := url.Parse(c.BaseURL)
	groupPath := strings.ReplaceAll(a.GroupID, ".", "/")
	u.Path = path.Join(u.Path, "staging/deployByRepositoryId", stagingID, groupPath, a.ArtifactID, a.Version, a.Filename)
	return u.String()
}

func (c *CentralClient) applyAuth(req *http.Request) {
	if c.Auth.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Auth.Token)
		return
	}
	if c.Auth.Username != "" {
		req.SetBasicAuth(c.Auth.Username, c.Auth.Password)
	}
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}
