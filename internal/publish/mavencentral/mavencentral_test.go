package mavencentral

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeStaging struct {
	mu       sync.Mutex
	files    map[string][]byte
	headers  map[string]http.Header
	requests int
	status   int
}

func newFakeStaging() *fakeStaging {
	return &fakeStaging{
		files:   map[string][]byte{},
		headers: map[string]http.Header{},
		status:  http.StatusCreated,
	}
}

func (f *fakeStaging) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests++
		data, _ := io.ReadAll(r.Body)
		f.files[r.URL.Path] = data
		f.headers[r.URL.Path] = r.Header.Clone()
		w.WriteHeader(f.status)
	})
}

func (f *fakeStaging) body(path string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.files[path]...)
}

func (f *fakeStaging) header(path, key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.headers[path].Get(key)
}

func TestNewRejectsBadURL(t *testing.T) {
	if _, err := New("", AuthConfig{}); err == nil {
		t.Fatal("expected error for empty URL")
	}
	if _, err := New("relative/path", AuthConfig{}); err == nil {
		t.Fatal("expected error for relative URL")
	}
}

func TestUploadPutsArtifactToStagingPath(t *testing.T) {
	fake := newFakeStaging()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	c, err := New(ts.URL, AuthConfig{Username: "user", Password: "pass"})
	if err != nil {
		t.Fatal(err)
	}

	content := "jar-content-bytes"
	err = c.Upload(context.Background(), "repo-1001", Artifact{
		GroupID:    "org.example",
		ArtifactID: "demo",
		Version:    "1.2.3",
		Filename:   "demo-1.2.3.jar",
		Body:       strings.NewReader(content),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	wantPath := "/staging/deployByRepositoryId/repo-1001/org/example/demo/1.2.3/demo-1.2.3.jar"
	if got := string(fake.body(wantPath)); got != content {
		t.Fatalf("body mismatch: got %q, want %q", got, content)
	}
	if got := fake.header(wantPath, "User-Agent"); got != userAgent {
		t.Fatalf("User-Agent: got %q, want %q", got, userAgent)
	}
}

func TestUploadSendsBasicAuth(t *testing.T) {
	fake := newFakeStaging()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	c, err := New(ts.URL, AuthConfig{Username: "alice", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}

	err = c.Upload(context.Background(), "repo-42", Artifact{
		GroupID:    "com.test",
		ArtifactID: "lib",
		Version:    "0.1.0",
		Filename:   "lib-0.1.0.jar",
		Body:       strings.NewReader("data"),
	})
	if err != nil {
		t.Fatal(err)
	}

	p := "/staging/deployByRepositoryId/repo-42/com/test/lib/0.1.0/lib-0.1.0.jar"
	auth := fake.header(p, "Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Fatalf("expected Basic auth, got %q", auth)
	}
}

func TestUploadSendsBearerToken(t *testing.T) {
	fake := newFakeStaging()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	c, err := New(ts.URL, AuthConfig{Token: "my-token"})
	if err != nil {
		t.Fatal(err)
	}

	err = c.Upload(context.Background(), "repo-99", Artifact{
		GroupID:    "com.test",
		ArtifactID: "lib",
		Version:    "0.1.0",
		Filename:   "lib-0.1.0.jar",
		Body:       strings.NewReader("data"),
	})
	if err != nil {
		t.Fatal(err)
	}

	p := "/staging/deployByRepositoryId/repo-99/com/test/lib/0.1.0/lib-0.1.0.jar"
	if got := fake.header(p, "Authorization"); got != "Bearer my-token" {
		t.Fatalf("expected Bearer token, got %q", got)
	}
}

func TestUploadTokenTakesPrecedenceOverBasicAuth(t *testing.T) {
	fake := newFakeStaging()
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	c, err := New(ts.URL, AuthConfig{
		Username: "alice",
		Password: "secret",
		Token:    "tok",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = c.Upload(context.Background(), "repo-1", Artifact{
		GroupID:    "g",
		ArtifactID: "a",
		Version:    "1",
		Filename:   "a-1.jar",
		Body:       strings.NewReader("x"),
	})
	if err != nil {
		t.Fatal(err)
	}

	p := "/staging/deployByRepositoryId/repo-1/g/a/1/a-1.jar"
	if got := fake.header(p, "Authorization"); got != "Bearer tok" {
		t.Fatalf("expected Bearer auth to win, got %q", got)
	}
}

func TestUploadRejectsEmptyStagingID(t *testing.T) {
	c, _ := New("https://example.com", AuthConfig{})
	err := c.Upload(context.Background(), "", Artifact{
		GroupID: "g", ArtifactID: "a", Version: "1", Filename: "a.jar",
		Body: strings.NewReader("x"),
	})
	if err == nil || !strings.Contains(err.Error(), "empty stagingID") {
		t.Fatalf("expected staging ID error, got %v", err)
	}
}

func TestUploadRejectsIncompleteArtifact(t *testing.T) {
	c, _ := New("https://example.com", AuthConfig{})
	err := c.Upload(context.Background(), "repo-1", Artifact{
		GroupID: "", ArtifactID: "a", Version: "1", Filename: "a.jar",
		Body: strings.NewReader("x"),
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete error, got %v", err)
	}
}

func TestUploadRejectsNilBody(t *testing.T) {
	c, _ := New("https://example.com", AuthConfig{})
	err := c.Upload(context.Background(), "repo-1", Artifact{
		GroupID: "g", ArtifactID: "a", Version: "1", Filename: "a.jar",
		Body: nil,
	})
	if err == nil || !strings.Contains(err.Error(), "nil body") {
		t.Fatalf("expected nil body error, got %v", err)
	}
}

func TestUploadReturnsErrorOnHTTPFailure(t *testing.T) {
	fake := newFakeStaging()
	fake.status = http.StatusForbidden
	ts := httptest.NewServer(fake.handler())
	defer ts.Close()

	c, _ := New(ts.URL, AuthConfig{})
	err := c.Upload(context.Background(), "repo-1", Artifact{
		GroupID: "g", ArtifactID: "a", Version: "1", Filename: "a.jar",
		Body: strings.NewReader("x"),
	})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{}
	c, err := New("https://example.com", AuthConfig{}, WithHTTPClient(custom))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPClient != custom {
		t.Fatal("expected custom HTTP client to be set")
	}
}
