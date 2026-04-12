// Package remotecache implements a Turborepo-style HTTP client for a
// shared content-addressable cache.
//
// The wire protocol is deliberately minimal:
//
//	GET    /cas/<hex-hash>       → blob bytes (or 404)
//	HEAD   /cas/<hex-hash>       → 200 or 404
//	PUT    /cas/<hex-hash>       → upload blob bytes
//	HEAD   /action/<hex-hash>    → 200 or 404
//	GET    /action/<hex-hash>    → action-result JSON (or 404)
//	PUT    /action/<hex-hash>    → store action-result JSON
//
// All requests carry an Authorization: Bearer <token> header when a token
// is configured. Clients verify the content hash of every downloaded blob
// against the expected value in the URL path; the server is expected to
// do the same on uploads.
//
// This package is the client surface only. Server implementations are out
// of scope for Slice 5. See roadmap/planning/dependency-cache-architecture.md
// and roadmap/planning/shared-cache-topology.md for the architectural role.
package remotecache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/kaeawc/grit/internal/cas"
)

// userAgent is sent on every request so servers can identify grit clients
// in logs and rate-limiting rules.
const userAgent = "grit-remotecache/1"

// Client speaks the remote-cache HTTP protocol.
//
// Client methods are safe for concurrent use: all mutable state lives on
// the underlying http.Client. Constructed Clients do not retain references
// to request bodies beyond the lifetime of a single call.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	token      string
}

// New returns a Client that talks to the server rooted at baseURL using
// the given bearer token for authentication. Pass an empty token to skip
// the Authorization header entirely.
func New(baseURL, token string) (*Client, error) {
	return NewWithClient(baseURL, token, http.DefaultClient)
}

// NewWithClient is like New but takes an explicit *http.Client so callers
// can configure timeouts, proxies, and TLS.
func NewWithClient(baseURL, token string, hc *http.Client) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("remotecache: empty baseURL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("remotecache: parse baseURL: %w", err)
	}
	if !u.IsAbs() {
		return nil, fmt.Errorf("remotecache: baseURL must be absolute, got %q", baseURL)
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{baseURL: u, httpClient: hc, token: token}, nil
}

// GetBlob fetches a blob by hash. The returned bytes are verified against
// hash before returning; a server that returns different content triggers
// cas.ErrHashMismatch. A 404 response returns cas.ErrNotFound.
//
// GetBlob buffers the entire response in memory so the hash can be
// verified before any bytes are handed to the caller. A streaming variant
// is a future optimization for very large artifacts.
func (c *Client) GetBlob(ctx context.Context, hash cas.Hash) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodGet, []string{"cas", hash.String()}, nil, "")
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, cas.ErrNotFound
	default:
		return nil, fmt.Errorf("remotecache: GET /cas/%s: %s", hash, statusError(resp))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("remotecache: read body: %w", err)
	}
	if got := cas.HashBytes(data); got != hash {
		return nil, fmt.Errorf("%w: GET /cas/%s returned content hashing to %s", cas.ErrHashMismatch, hash, got)
	}
	return data, nil
}

// HasBlob returns true if the server claims to have the blob.
func (c *Client) HasBlob(ctx context.Context, hash cas.Hash) (bool, error) {
	req, err := c.newRequest(ctx, http.MethodHead, []string{"cas", hash.String()}, nil, "")
	if err != nil {
		return false, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("remotecache: HEAD /cas/%s: %s", hash, statusError(resp))
	}
}

// HasActionResult returns true if the server claims to have the action result.
func (c *Client) HasActionResult(ctx context.Context, actionHash cas.Hash) (bool, error) {
	req, err := c.newRequest(ctx, http.MethodHead, []string{"action", actionHash.String()}, nil, "")
	if err != nil {
		return false, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("remotecache: HEAD /action/%s: %s", actionHash, statusError(resp))
	}
}

// PutBlob uploads data as the blob identified by hash. The client verifies
// the content hash locally before making the request so a mismatched
// caller never hits the network. The server is expected to re-verify on
// its side; mismatches are rejected with a 4xx response.
func (c *Client) PutBlob(ctx context.Context, hash cas.Hash, data []byte) error {
	if got := cas.HashBytes(data); got != hash {
		return fmt.Errorf("%w: PutBlob data hashes to %s, declared %s", cas.ErrHashMismatch, got, hash)
	}
	req, err := c.newRequest(ctx, http.MethodPut, []string{"cas", hash.String()}, bytes.NewReader(data), "application/octet-stream")
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(data))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("remotecache: PUT /cas/%s: %s", hash, statusError(resp))
	}
	return nil
}

// GetActionResult fetches a cached action result by action hash. The
// returned result's declared ActionHash must match the request hash; a
// server that returns a mismatched record is rejected.
func (c *Client) GetActionResult(ctx context.Context, actionHash cas.Hash) (cas.ActionResult, error) {
	req, err := c.newRequest(ctx, http.MethodGet, []string{"action", actionHash.String()}, nil, "")
	if err != nil {
		return cas.ActionResult{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return cas.ActionResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return cas.ActionResult{}, cas.ErrNotFound
	default:
		return cas.ActionResult{}, fmt.Errorf("remotecache: GET /action/%s: %s", actionHash, statusError(resp))
	}
	var result cas.ActionResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return cas.ActionResult{}, fmt.Errorf("remotecache: decode action result: %w", err)
	}
	if result.ActionHash != actionHash {
		return cas.ActionResult{}, fmt.Errorf("remotecache: action result hash mismatch: url %s body %s", actionHash, result.ActionHash)
	}
	return result, nil
}

// PutActionResult uploads an action result.
func (c *Client) PutActionResult(ctx context.Context, result cas.ActionResult) error {
	if result.ActionHash.IsZero() {
		return fmt.Errorf("remotecache: PutActionResult: zero action hash")
	}
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("remotecache: encode action result: %w", err)
	}
	req, err := c.newRequest(ctx, http.MethodPut, []string{"action", result.ActionHash.String()}, bytes.NewReader(body), "application/json")
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(body))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("remotecache: PUT /action/%s: %s", result.ActionHash, statusError(resp))
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method string, pathElements []string, body io.Reader, contentType string) (*http.Request, error) {
	u := c.baseURL.JoinPath(pathElements...)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

func statusError(resp *http.Response) string {
	const maxBody = 1024
	limited := io.LimitReader(resp.Body, maxBody+1)
	body, _ := io.ReadAll(limited)
	if len(body) == 0 {
		return resp.Status
	}
	if len(body) > maxBody {
		body = append(body[:maxBody], []byte("...")...)
	}
	return fmt.Sprintf("%s: %s", resp.Status, string(body))
}
