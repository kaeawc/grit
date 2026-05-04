package dependencywiring

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
)

func TestResolverCacheRootUsesGradleDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := ResolverCacheRoot()
	want := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	if got != want {
		t.Fatalf("ResolverCacheRoot: got %q want %q", got, want)
	}
}

func TestLoadCatalogIgnoresMissingCatalogFiles(t *testing.T) {
	root := t.TempDir()
	prj := &project.Project{
		RootDir: root,
		VersionCatalogs: []string{
			filepath.Join(root, "missing.versions.toml"),
		},
	}

	cat, err := LoadCatalog(prj)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if cat == nil || len(cat.Versions) != 0 || len(cat.Libraries) != 0 || len(cat.Bundles) != 0 {
		t.Fatalf("expected empty catalog for missing files, got %#v", cat)
	}
}

func TestResolverRequiresProject(t *testing.T) {
	if _, err := Resolver(nil, nil); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("expected invalid project error, got %v", err)
	}
}

func TestResolverDelegatesTrackerAndMaterialization(t *testing.T) {
	t.Parallel()

	legacy := &fakeLegacyResolver{resolved: &m2local.Resolved{CompileJars: []string{"legacy.jar"}}}
	materialized := &m2local.Resolved{CompileJars: []string{"materialized.jar"}}
	materializer := &fakeResolvedMaterializer{resolved: materialized}
	tracker := perf.New(true)

	resolver := &wiredResolver{
		legacy:      legacy,
		materialize: materializer,
		topology:    m2local.CacheTopology{WorkRoot: "/repo"},
	}
	resolver.SetTracker(tracker)
	got, err := resolver.Resolve(&modulebuild.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if got != materialized {
		t.Fatalf("expected materialized result, got %#v", got)
	}
	if legacy.tracker != tracker {
		t.Fatalf("expected tracker propagation")
	}
	if materializer.input != legacy.resolved {
		t.Fatalf("expected materializer to receive legacy result")
	}
	if resolver.Topology().WorkRoot != "/repo" {
		t.Fatalf("unexpected topology: %#v", resolver.Topology())
	}
}

func TestResolverMaterializesIntoWorktreeCompatibilityRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	gradleRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")

	stageGradleCacheFile(t, gradleRoot, "org.example", "demo", "1.0.0", "hash-jar", "demo-1.0.0.jar", []byte("jar bytes"))
	stageGradleCacheFile(t, gradleRoot, "org.example", "demo", "1.0.0", "hash-pom", "demo-1.0.0.pom", []byte(`<project><modelVersion>4.0.0</modelVersion></project>`))

	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":            []byte("android classes"),
		"AndroidManifest.xml":    []byte(`<?xml version="1.0"?><manifest package="com.example.lib"/>`),
		"res/values/strings.xml": []byte(`<resources><string name="x">x</string></resources>`),
	})
	stageGradleCacheFile(t, gradleRoot, "org.example.android", "widget", "2.0.0", "hash-aar", "widget-2.0.0.aar", aarBytes)
	stageGradleCacheFile(t, gradleRoot, "org.example.android", "widget", "2.0.0", "hash-pom", "widget-2.0.0.pom", []byte(`<project><modelVersion>4.0.0</modelVersion></project>`))

	prj := &project.Project{
		RootDir: root,
		Repositories: []project.Repository{
			{Name: "mavenCentral", Kind: "mavenCentral", URL: "https://repo1.maven.org/maven2/"},
		},
	}
	resolver, err := Resolver(prj, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve(&modulebuild.Dependencies{
		Main: []modulebuild.Ref{
			{Kind: "raw", Value: "org.example:demo:1.0.0"},
			{Kind: "raw", Value: "org.example.android:widget:2.0.0"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	materializedJar := filepath.Join(MaterializedRepositoryRoot(root), "org", "example", "demo", "1.0.0", "demo-1.0.0.jar")
	if len(resolved.CompileJars) < 2 {
		t.Fatalf("unexpected compile jars: %#v", resolved.CompileJars)
	}
	if resolved.CompileJars[0] != materializedJar {
		t.Fatalf("expected projected jar path, got %#v", resolved.CompileJars)
	}
	if _, err := os.Stat(materializedJar); err != nil {
		t.Fatalf("expected projected jar to exist: %v", err)
	}
	if len(resolved.AndroidLibraries) != 1 {
		t.Fatalf("unexpected android libraries: %#v", resolved.AndroidLibraries)
	}
	lib := resolved.AndroidLibraries[0]
	if lib.ClassesJar == "" || lib.ManifestPath == "" || lib.ResDir == "" {
		t.Fatalf("expected projected android library outputs, got %#v", lib)
	}
	if !stringsContain(lib.ClassesJar, filepath.ToSlash(MaterializedAARRoot(root))) {
		t.Fatalf("expected classes.jar under projected aar root, got %s", lib.ClassesJar)
	}
	if _, err := os.Stat(lib.ClassesJar); err != nil {
		t.Fatalf("expected projected classes.jar: %v", err)
	}
	if _, err := os.Stat(filepath.Join(MaterializedRepositoryRoot(root), "org", "example", "android", "widget", "2.0.0", "widget-2.0.0.aar")); err != nil {
		t.Fatalf("expected projected AAR to exist: %v", err)
	}
}

func TestCoordinateFromMaterializedPathParsesBothViews(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/tmp/repo/.grit/worktree/materialized-m2/org/example/demo/1.0.0/demo-1.0.0.jar",
		"/tmp/repo/.grit/worktree/aar/org/example/demo/1.0.0/classes.jar",
	} {
		coord, ok := CoordinateFromMaterializedPath(path)
		if !ok {
			t.Fatalf("expected coordinate for %s", path)
		}
		if coord.Group != "org.example" || coord.Artifact != "demo" || coord.Version != "1.0.0" {
			t.Fatalf("unexpected coordinate for %s: %#v", path, coord)
		}
	}
}

type fakeLegacyResolver struct {
	resolved *m2local.Resolved
	tracker  perf.Tracker
}

func (f *fakeLegacyResolver) Resolve(*modulebuild.Dependencies) (*m2local.Resolved, error) {
	return f.resolved, nil
}

func (f *fakeLegacyResolver) SetTracker(tracker perf.Tracker) {
	f.tracker = tracker
}

type fakeResolvedMaterializer struct {
	input    *m2local.Resolved
	resolved *m2local.Resolved
}

func (f *fakeResolvedMaterializer) Materialize(_ context.Context, resolved *m2local.Resolved) (*m2local.Resolved, error) {
	f.input = resolved
	return f.resolved, nil
}

func stageGradleCacheFile(t *testing.T, root, group, artifact, version, hashDir, name string, data []byte) string {
	t.Helper()
	dir := filepath.Join(root, group, artifact, version, hashDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildAAR(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func stringsContain(path, needle string) bool {
	return strings.Contains(filepath.ToSlash(path), filepath.ToSlash(needle))
}

func TestSourceDownloadersRespectsDeclarationOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create Gradle cache root so the implicit gradle-cache source is added.
	gradleRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	if err := os.MkdirAll(gradleRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create Maven local root so it can be resolved.
	m2Root := filepath.Join(home, ".m2", "repository")
	if err := os.MkdirAll(m2Root, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("no_declaration_adds_implicit_mavenlocal", func(t *testing.T) {
		repos := []project.Repository{
			{Name: "mavenCentral", Kind: "mavenCentral", URL: "https://repo1.maven.org/maven2/"},
		}
		sources := sourceDownloaders(repos)
		ids := downloaderIDs(sources)

		// Gradle cache always comes first; then declared repos; then
		// implicit maven-local as fallback.
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
		sources := sourceDownloaders(repos)
		ids := downloaderIDs(sources)

		// Gradle cache first, then declared order: mavenLocal, mavenCentral, google.
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
		sources := sourceDownloaders(repos)
		ids := downloaderIDs(sources)

		// mavenLocal at the end, per declaration.
		wantOrder := []string{"gradle-cache", "google", "mavenCentral", "maven-local"}
		if !slicesEqual(ids, wantOrder) {
			t.Fatalf("expected %v, got %v", wantOrder, ids)
		}
	})

	t.Run("no_duplicate_downloaders", func(t *testing.T) {
		repos := []project.Repository{
			{Name: "mavenLocal", Kind: "mavenLocal"},
			{Name: "mavenLocal2", Kind: "mavenLocal"}, // second mavenLocal declaration
		}
		sources := sourceDownloaders(repos)
		ids := downloaderIDs(sources)

		// Second maven-local should be deduped.
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

	result := deduplicateDownloaders([]downloader.Downloader{a, b, a2})
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
