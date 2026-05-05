package m2local

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestFindCachedVersionPrefersNumericVersionOrdering(t *testing.T) {
	t.Parallel()

	resolver := New(t.TempDir(), t.TempDir(), nil, nil)
	root := filepath.Join(resolver.CacheRoot, "com.example", "demo")
	for _, version := range []string{"2.0", "10.0"} {
		if err := os.MkdirAll(filepath.Join(root, version), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := resolver.findCachedVersion("com.example", "demo")
	if got != "10.0" {
		t.Fatalf("expected numeric latest version, got %q", got)
	}
}

func TestFindCachedVersionPrefersReleaseOverQualifier(t *testing.T) {
	t.Parallel()

	resolver := New(t.TempDir(), t.TempDir(), nil, nil)
	root := filepath.Join(resolver.CacheRoot, "com.example", "demo")
	for _, version := range []string{"1.2.3-alpha1", "1.2.3", "1.2.3-rc1", "1.2.3-ga"} {
		if err := os.MkdirAll(filepath.Join(root, version), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := resolver.findCachedVersion("com.example", "demo")
	if got != "1.2.3" {
		t.Fatalf("expected release version to win over qualifiers, got %q", got)
	}
}

func TestFindCachedVersionOrdersQualifiersDeterministically(t *testing.T) {
	t.Parallel()

	resolver := New(t.TempDir(), t.TempDir(), nil, nil)
	root := filepath.Join(resolver.CacheRoot, "com.example", "demo")
	for _, version := range []string{"1.0.0-beta2", "1.0.0-rc1"} {
		if err := os.MkdirAll(filepath.Join(root, version), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := resolver.findCachedVersion("com.example", "demo")
	if got != "1.0.0-rc1" {
		t.Fatalf("expected rc to win over beta, got %q", got)
	}
}

func TestLoadBOMFetchesWhenLocalCacheMisses(t *testing.T) {
	t.Parallel()

	bomBody := `<project>
  <groupId>com.example</groupId>
  <artifactId>example-bom</artifactId>
  <version>1.0.0</version>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>com.example</groupId>
        <artifactId>lib-a</artifactId>
        <version>2.5.0</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/com/example/example-bom/1.0.0/example-bom-1.0.0.pom") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(bomBody))
	}))
	t.Cleanup(server.Close)

	cacheRoot := filepath.Join(t.TempDir(), "cache")
	workRoot := t.TempDir()
	resolver := New(cacheRoot, workRoot, []project.Repository{
		{Kind: "maven", Scope: "dependency", URL: server.URL},
	}, nil)

	managed, err := resolver.loadBOM(Coordinate{Group: "com.example", Module: "example-bom", Version: "1.0.0"})
	if err != nil {
		t.Fatalf("loadBOM returned error: %v", err)
	}
	if got := managed["com.example:lib-a"]; got != "2.5.0" {
		t.Fatalf("expected managed version 2.5.0 for com.example:lib-a, got %q", got)
	}
}

func TestLoadBOMReturnsErrorWhenNotFoundLocallyOrRemotely(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(server.Close)

	cacheRoot := filepath.Join(t.TempDir(), "cache")
	workRoot := t.TempDir()
	resolver := New(cacheRoot, workRoot, []project.Repository{
		{Kind: "maven", Scope: "dependency", URL: server.URL},
	}, nil)

	if _, err := resolver.loadBOM(Coordinate{Group: "com.example", Module: "missing-bom", Version: "1.0.0"}); err == nil {
		t.Fatal("expected loadBOM to return an error when the POM is unavailable")
	}
}
