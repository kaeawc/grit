package depcache_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/depcache"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/project"
)

func TestSourceDownloadersRespectsDeclarationOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	gradleRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	if err := os.MkdirAll(gradleRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	m2Root := filepath.Join(home, ".m2", "repository")
	if err := os.MkdirAll(m2Root, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("no_declaration_adds_implicit_mavenlocal", func(t *testing.T) {
		repos := []project.Repository{
			{Name: "mavenCentral", Kind: "mavenCentral", URL: "https://repo1.maven.org/maven2/"},
		}
		sources := depcache.SourceDownloaders(repos, gradleRoot)
		ids := downloaderIDs(sources)

		wantOrder := []string{"gradle-cache", "mavenCentral", "maven-local"}
		if !slicesEqual(ids, wantOrder) {
			t.Fatalf("expected %v, got %v", wantOrder, ids)
		}
	})

	t.Run("declared_mavenlocal_uses_declared_position", func(t *testing.T) {
		repos := []project.Repository{
			{Name: "mavenLocal", Kind: "mavenLocal"},
			{Name: "mavenCentral", Kind: "mavenCentral", URL: "https://repo1.maven.org/maven2/"},
			{Name: "google", Kind: "google", URL: "https://dl.google.com/dl/android/maven2/"},
		}
		sources := depcache.SourceDownloaders(repos, gradleRoot)
		ids := downloaderIDs(sources)

		wantOrder := []string{"gradle-cache", "maven-local", "mavenCentral", "google"}
		if !slicesEqual(ids, wantOrder) {
			t.Fatalf("expected %v, got %v", wantOrder, ids)
		}
	})

	t.Run("declared_mavenlocal_after_remotes", func(t *testing.T) {
		repos := []project.Repository{
			{Name: "google", Kind: "google", URL: "https://dl.google.com/dl/android/maven2/"},
			{Name: "mavenCentral", Kind: "mavenCentral", URL: "https://repo1.maven.org/maven2/"},
			{Name: "mavenLocal", Kind: "mavenLocal"},
		}
		sources := depcache.SourceDownloaders(repos, gradleRoot)
		ids := downloaderIDs(sources)

		wantOrder := []string{"gradle-cache", "google", "mavenCentral", "maven-local"}
		if !slicesEqual(ids, wantOrder) {
			t.Fatalf("expected %v, got %v", wantOrder, ids)
		}
	})

	t.Run("no_duplicate_downloaders", func(t *testing.T) {
		repos := []project.Repository{
			{Name: "mavenLocal", Kind: "mavenLocal"},
			{Name: "mavenLocal2", Kind: "mavenLocal"},
		}
		sources := depcache.SourceDownloaders(repos, gradleRoot)
		ids := downloaderIDs(sources)

		wantOrder := []string{"gradle-cache", "maven-local"}
		if !slicesEqual(ids, wantOrder) {
			t.Fatalf("expected %v, got %v", wantOrder, ids)
		}
	})
}

func TestDeduplicateDownloaders(t *testing.T) {
	t.Parallel()

	a := &stubDownloader{id: "a"}
	b := &stubDownloader{id: "b"}
	a2 := &stubDownloader{id: "a"}

	result := depcache.DeduplicateDownloaders([]downloader.Downloader{a, b, a2})
	if len(result) != 2 {
		t.Fatalf("expected 2 downloaders, got %d", len(result))
	}
	if result[0].ID() != "a" || result[1].ID() != "b" {
		t.Fatalf("unexpected order: %v, %v", result[0].ID(), result[1].ID())
	}
}

func downloaderIDs(sources []downloader.Downloader) []string {
	ids := make([]string, len(sources))
	for i, s := range sources {
		ids[i] = s.ID()
	}
	return ids
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type stubDownloader struct {
	id string
}

func (s *stubDownloader) ID() string { return s.id }
func (s *stubDownloader) Fetch(_ context.Context, _ lockfile.Pin, _ cas.Store) error {
	return nil
}
