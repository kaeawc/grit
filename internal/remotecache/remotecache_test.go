package remotecache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

// fakeServer is a minimal in-memory implementation of the remote-cache
// wire protocol, for exercising the client end-to-end without standing
// up a real server. The server is an implementation detail of Slice 5
// tests; the production server is out of scope.
type fakeServer struct {
	mu          sync.Mutex
	blobs       map[cas.Hash][]byte
	actions     map[cas.Hash]cas.ActionResult
	token       string
	requireAuth bool
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		blobs:   map[cas.Hash][]byte{},
		actions: map[cas.Hash]cas.ActionResult{},
	}
}

func (f *fakeServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.requireAuth && r.Header.Get("Authorization") != "Bearer "+f.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/cas/"):
			hexHash := strings.TrimPrefix(r.URL.Path, "/cas/")
			h, err := cas.ParseHash(hexHash)
			if err != nil {
				http.Error(w, "bad hash", http.StatusBadRequest)
				return
			}
			f.handleBlob(w, r, h)
		case strings.HasPrefix(r.URL.Path, "/action/"):
			hexHash := strings.TrimPrefix(r.URL.Path, "/action/")
			h, err := cas.ParseHash(hexHash)
			if err != nil {
				http.Error(w, "bad hash", http.StatusBadRequest)
				return
			}
			f.handleAction(w, r, h)
		default:
			http.NotFound(w, r)
		}
	})
}

func (f *fakeServer) handleBlob(w http.ResponseWriter, r *http.Request, hash cas.Hash) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodHead:
		if _, ok := f.blobs[hash]; !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		body, ok := f.blobs[hash]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if cas.HashBytes(body) != hash {
			http.Error(w, "hash mismatch", http.StatusBadRequest)
			return
		}
		f.blobs[hash] = body
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fakeServer) handleAction(w http.ResponseWriter, r *http.Request, hash cas.Hash) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		result, ok := f.actions[hash]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	case http.MethodPut:
		var result cas.ActionResult
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if result.ActionHash != hash {
			http.Error(w, "action hash mismatch", http.StatusBadRequest)
			return
		}
		f.actions[hash] = result
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func startTestServer(t *testing.T) (*Client, *fakeServer, func()) {
	t.Helper()
	fake := newFakeServer()
	ts := httptest.NewServer(fake.handler())
	client, err := New(ts.URL, "")
	if err != nil {
		ts.Close()
		t.Fatalf("New: %v", err)
	}
	return client, fake, ts.Close
}

func TestPutGetBlobRoundTrip(t *testing.T) {
	client, _, cleanup := startTestServer(t)
	defer cleanup()
	ctx := context.Background()

	payload := []byte("blob bytes")
	hash := cas.HashBytes(payload)

	if err := client.PutBlob(ctx, hash, payload); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	got, err := client.GetBlob(ctx, hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, payload)
	}
}

func TestGetBlobNotFound(t *testing.T) {
	client, _, cleanup := startTestServer(t)
	defer cleanup()
	_, err := client.GetBlob(context.Background(), cas.HashBytes([]byte("nope")))
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestHasBlob(t *testing.T) {
	client, _, cleanup := startTestServer(t)
	defer cleanup()
	ctx := context.Background()

	payload := []byte("x")
	hash := cas.HashBytes(payload)

	has, err := client.HasBlob(ctx, hash)
	if err != nil || has {
		t.Fatalf("expected absent, got has=%v err=%v", has, err)
	}
	if err := client.PutBlob(ctx, hash, payload); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	has, err = client.HasBlob(ctx, hash)
	if err != nil || !has {
		t.Fatalf("expected present, got has=%v err=%v", has, err)
	}
}

func TestPutBlobRejectsClientSideHashMismatch(t *testing.T) {
	client, _, cleanup := startTestServer(t)
	defer cleanup()

	wrongHash := cas.HashBytes([]byte("wrong"))
	err := client.PutBlob(context.Background(), wrongHash, []byte("real"))
	if !errors.Is(err, cas.ErrHashMismatch) {
		t.Fatalf("expected client-side hash mismatch, got %v", err)
	}
}

func TestGetBlobRejectsServerWithTamperedContent(t *testing.T) {
	// A pathological server that returns bytes not matching the URL hash.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tampered"))
	}))
	defer ts.Close()
	client, err := New(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetBlob(context.Background(), cas.HashBytes([]byte("different content")))
	if !errors.Is(err, cas.ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
}

func TestActionResultRoundTrip(t *testing.T) {
	client, _, cleanup := startTestServer(t)
	defer cleanup()
	ctx := context.Background()

	actionHash := cas.HashBytes([]byte("action identity"))
	blobHash := cas.HashBytes([]byte("output"))
	result := cas.ActionResult{
		ActionHash: actionHash,
		Outputs: []cas.NamedOutput{
			{Role: "classes-jar", Blob: cas.BlobInfo{Hash: blobHash, Size: 6}},
			{Role: "android-manifest", Blob: cas.BlobInfo{Hash: cas.HashBytes([]byte("manifest")), Size: 8}},
		},
	}
	if err := client.PutActionResult(ctx, result); err != nil {
		t.Fatalf("PutActionResult: %v", err)
	}
	loaded, err := client.GetActionResult(ctx, actionHash)
	if err != nil {
		t.Fatalf("GetActionResult: %v", err)
	}
	if loaded.ActionHash != actionHash {
		t.Fatalf("action hash mismatch: %s vs %s", loaded.ActionHash, actionHash)
	}
	if len(loaded.Outputs) != 2 {
		t.Fatalf("output count mismatch: %d", len(loaded.Outputs))
	}
	if loaded.Outputs[0].Role != "classes-jar" || loaded.Outputs[0].Blob.Hash != blobHash {
		t.Fatalf("first output not preserved: %+v", loaded.Outputs[0])
	}
}

func TestGetActionResultNotFound(t *testing.T) {
	client, _, cleanup := startTestServer(t)
	defer cleanup()
	_, err := client.GetActionResult(context.Background(), cas.HashBytes([]byte("missing")))
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPutActionResultRejectsZeroHash(t *testing.T) {
	client, _, cleanup := startTestServer(t)
	defer cleanup()
	if err := client.PutActionResult(context.Background(), cas.ActionResult{}); err == nil {
		t.Fatalf("expected error for zero action hash")
	}
}

func TestActionResultBodyHashMustMatchURL(t *testing.T) {
	// A pathological server that returns a valid JSON action result with
	// an ActionHash that disagrees with the URL.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other := cas.ActionResult{ActionHash: cas.HashBytes([]byte("not the url hash"))}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(other)
	}))
	defer ts.Close()
	client, _ := New(ts.URL, "")
	_, err := client.GetActionResult(context.Background(), cas.HashBytes([]byte("url hash")))
	if err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestBearerTokenHeader(t *testing.T) {
	var captured string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Authorization")
		http.NotFound(w, r)
	}))
	defer ts.Close()
	client, _ := New(ts.URL, "my-token")
	_, _ = client.GetBlob(context.Background(), cas.HashBytes([]byte("x")))
	if captured != "Bearer my-token" {
		t.Fatalf("Authorization header: %q", captured)
	}
}

func TestEmptyTokenOmitsAuthHeader(t *testing.T) {
	var captured string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("Authorization")
		http.NotFound(w, r)
	}))
	defer ts.Close()
	client, _ := New(ts.URL, "")
	_, _ = client.GetBlob(context.Background(), cas.HashBytes([]byte("x")))
	if captured != "" {
		t.Fatalf("expected no Authorization header when token is empty, got %q", captured)
	}
}

func TestUserAgentSet(t *testing.T) {
	var captured string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("User-Agent")
		http.NotFound(w, r)
	}))
	defer ts.Close()
	client, _ := New(ts.URL, "")
	_, _ = client.GetBlob(context.Background(), cas.HashBytes([]byte("x")))
	if captured != userAgent {
		t.Fatalf("User-Agent: got %q want %q", captured, userAgent)
	}
}

func TestAuthEnforced(t *testing.T) {
	fake := newFakeServer()
	fake.requireAuth = true
	fake.token = "secret"
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	wrong, _ := New(ts.URL, "wrong")
	_, err := wrong.GetBlob(context.Background(), cas.HashBytes([]byte("x")))
	if err == nil {
		t.Fatalf("expected unauth error with wrong token")
	}

	right, _ := New(ts.URL, "secret")
	payload := []byte("auth payload")
	hash := cas.HashBytes(payload)
	if err := right.PutBlob(context.Background(), hash, payload); err != nil {
		t.Fatalf("authed PutBlob: %v", err)
	}
}

func TestNewRejectsBadURL(t *testing.T) {
	if _, err := New("", ""); err == nil {
		t.Fatalf("expected error for empty URL")
	}
	if _, err := New("relative/path", ""); err == nil {
		t.Fatalf("expected error for relative URL")
	}
	if _, err := New("://bad", ""); err == nil {
		t.Fatalf("expected error for malformed URL")
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	// Server only answers under /api/; verify the client honors a prefixed
	// baseURL instead of collapsing it.
	fake := newFakeServer()
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", fake.handler()))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client, err := New(ts.URL+"/api/", "")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("prefixed")
	hash := cas.HashBytes(payload)
	if err := client.PutBlob(context.Background(), hash, payload); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	got, err := client.GetBlob(context.Background(), hash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round trip with prefix failed")
	}
}

func TestBaseURLPrefixWithoutTrailingSlash(t *testing.T) {
	fake := newFakeServer()
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", fake.handler()))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// No trailing slash on the prefix — JoinPath should still route correctly.
	client, err := New(ts.URL+"/api", "")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("no trailing")
	hash := cas.HashBytes(payload)
	if err := client.PutBlob(context.Background(), hash, payload); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
}
