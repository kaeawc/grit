package m2local

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func TestRepoAllowsCoordinate(t *testing.T) {
	t.Parallel()

	openRepo := project.Repository{}
	filteredRepo := project.Repository{
		IncludeGroups:     []string{"androidx.compose.runtime"},
		ExcludeGroups:     []string{"androidx.compose.material"},
		ExcludeModules:    []string{"androidx.compose.runtime:runtime"},
		ExcludeGroupRegex: []string{`^com\.example\.`},
	}

	tests := []struct {
		name  string
		repo  project.Repository
		coord Coordinate
		want  bool
	}{
		{
			name:  "default allow when no include rules",
			repo:  openRepo,
			coord: Coordinate{Group: "com.squareup.okhttp3", Module: "okhttp"},
			want:  true,
		},
		{
			name:  "excluded module",
			repo:  filteredRepo,
			coord: Coordinate{Group: "androidx.compose.runtime", Module: "runtime"},
			want:  false,
		},
		{
			name:  "excluded group",
			repo:  filteredRepo,
			coord: Coordinate{Group: "androidx.compose.material", Module: "material"},
			want:  false,
		},
		{
			name:  "included group",
			repo:  filteredRepo,
			coord: Coordinate{Group: "androidx.compose.runtime", Module: "runtime-livedata"},
			want:  true,
		},
		{
			name:  "regex exclusion",
			repo:  filteredRepo,
			coord: Coordinate{Group: "com.example.alpha", Module: "lib"},
			want:  false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := repoAllowsCoordinate(tc.repo, tc.coord); got != tc.want {
				t.Fatalf("repoAllowsCoordinate(%#v, %#v) = %v, want %v", tc.repo, tc.coord, got, tc.want)
			}
		})
	}
}

func TestRemoteRepositoryURLsFiltersRepositories(t *testing.T) {
	t.Parallel()

	resolver := newTestResolver(
		project.Repository{Kind: "maven", Scope: "compile", URL: "https://example.invalid/ignored"},
		project.Repository{Kind: "mavenLocal", Scope: "dependency", URL: "https://example.invalid/local"},
		project.Repository{
			Kind:          "maven",
			Scope:         "dependency",
			URL:           "https://example.invalid/maven",
			IncludeGroups: []string{"com.example"},
		},
		project.Repository{
			Kind:           "maven",
			Scope:          "dependency",
			URL:            "https://example.invalid/excluded",
			ExcludeModules: []string{"com.example:lib"},
		},
	)

	got := resolver.remoteRepositoryURLs(Coordinate{Group: "com.example", Module: "lib"})
	want := []string{"https://example.invalid/maven/"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected remote repository urls: got %#v want %#v", got, want)
	}
}

func TestRemoteRepositoryURLsFallsBackToDefaults(t *testing.T) {
	t.Parallel()

	resolver := newTestResolver(project.Repository{Kind: "maven", Scope: "compile", URL: "https://example.invalid/ignored"})
	resolver.AllowRepositoryFallback = true
	got := resolver.remoteRepositoryURLs(Coordinate{Group: "com.example", Module: "lib"})
	if len(got) != 2 {
		t.Fatalf("unexpected fallback urls: %#v", got)
	}
	if got[0] != "https://dl.google.com/dl/android/maven2/" || got[1] != "https://repo1.maven.org/maven2/" {
		t.Fatalf("unexpected fallback urls: %#v", got)
	}
}

func TestRemoteRepositoryURLsDoesNotFallbackByDefault(t *testing.T) {
	t.Parallel()

	resolver := newTestResolver(project.Repository{Kind: "maven", Scope: "compile", URL: "https://example.invalid/ignored"})
	got := resolver.remoteRepositoryURLs(Coordinate{Group: "com.example", Module: "lib"})
	if len(got) != 0 {
		t.Fatalf("unexpected implicit fallback urls: %#v", got)
	}
}

func TestFetchArtifactDownloadsJar(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/com/example/demo/1.2.3/demo-1.2.3.jar") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("jar-bytes"))
	}))
	t.Cleanup(server.Close)

	cacheRoot := filepath.Join(t.TempDir(), "cache")
	workRoot := t.TempDir()
	resolver := New(cacheRoot, workRoot, []project.Repository{
		{Kind: "maven", Scope: "dependency", URL: server.URL},
	}, nil)

	path, err := resolver.fetchArtifact(Coordinate{Group: "com.example", Module: "demo", Version: "1.2.3"}, ".jar")
	if err != nil {
		t.Fatalf("fetchArtifact returned error: %v", err)
	}
	if !strings.HasSuffix(path, "demo-1.2.3.jar") {
		t.Fatalf("unexpected artifact path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded artifact: %v", err)
	}
	if string(data) != "jar-bytes" {
		t.Fatalf("unexpected artifact contents: %q", string(data))
	}
}

func TestResolveBundleUnitTestIncludesMockkJvm(t *testing.T) {
	t.Parallel()

	catalogPath := "/Users/jason/kaeawc/auto-mobile/android/gradle/libs.versions.toml"
	if _, err := os.Stat(catalogPath); err != nil {
		t.Skipf("catalog path missing: %v", err)
	}
	cat, err := catalog.Load(catalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	resolver := New(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1"), t.TempDir(), nil, cat)
	resolved, err := resolver.Resolve(&modulebuild.Dependencies{
		Test: []modulebuild.Ref{{Kind: "bundle", Value: "unit.test"}},
	})
	if err != nil {
		t.Fatalf("resolve bundle: %v", err)
	}
	found := false
	for _, jar := range resolved.TestJars {
		if strings.Contains(jar, "mockk-jvm") && strings.HasSuffix(jar, ".jar") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("resolved test jars missing mockk-jvm: %#v", resolved.TestJars)
	}
}

func TestResolveOneMockkJvmArtifact(t *testing.T) {
	t.Parallel()

	catalogPath := "/Users/jason/kaeawc/auto-mobile/android/gradle/libs.versions.toml"
	if _, err := os.Stat(catalogPath); err != nil {
		t.Skipf("catalog path missing: %v", err)
	}
	cacheRoot := filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1")
	cat, err := catalog.Load(catalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	resolver := New(cacheRoot, t.TempDir(), nil, cat)
	artifact, _, deps, err := resolver.resolveOne(Coordinate{Group: "io.mockk", Module: "mockk", Version: "1.14.9"})
	if err != nil {
		t.Fatalf("resolveOne: %v", err)
	}
	if artifact == "" {
		t.Fatalf("resolveOne returned empty artifact, deps=%#v", deps)
	}
	if !strings.Contains(artifact, "mockk-jvm") {
		t.Fatalf("resolveOne artifact %q does not look like mockk-jvm", artifact)
	}
}

func TestExpandRefsBundleIncludesMockkJvm(t *testing.T) {
	t.Parallel()

	catalogPath := "/Users/jason/kaeawc/auto-mobile/android/gradle/libs.versions.toml"
	if _, err := os.Stat(catalogPath); err != nil {
		t.Skipf("catalog path missing: %v", err)
	}
	cat, err := catalog.Load(catalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	bundle, err := cat.ResolveBundle("unit.test")
	if err != nil {
		t.Fatalf("resolve bundle: %v", err)
	}
	if len(bundle) == 0 {
		t.Fatal("expected unit.test bundle to contain refs")
	}
	resolver := New(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1"), t.TempDir(), nil, cat)
	coords, err := resolver.expandRefs([]modulebuild.Ref{{Kind: "bundle", Value: "unit.test"}}, resolver.seedPlatforms())
	if err != nil {
		t.Fatalf("expandRefs: %v", err)
	}
	found := false
	for _, coord := range coords {
		if coord.Group == "io.mockk" && coord.Module == "mockk-jvm" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expandRefs missing io.mockk:mockk-jvm: %#v", coords)
	}
}

func TestExpandRefsSupportsRawCoordinates(t *testing.T) {
	t.Parallel()

	resolver := New(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1"), t.TempDir(), nil, &catalog.Catalog{
		Versions:  map[string]string{},
		Libraries: map[string]catalog.Library{},
		Bundles:   map[string][]string{},
	})
	coords, err := resolver.expandRefs([]modulebuild.Ref{{Kind: "raw", Value: "org.jetbrains.kotlin:kotlin-compiler-embeddable:2.3.0"}}, resolver.seedPlatforms())
	if err != nil {
		t.Fatalf("expandRefs raw: %v", err)
	}
	if len(coords) != 1 {
		t.Fatalf("expected one coordinate, got %#v", coords)
	}
	got := coords[0]
	if got.Group != "org.jetbrains.kotlin" || got.Module != "kotlin-compiler-embeddable" || got.Version != "2.3.0" {
		t.Fatalf("unexpected raw coordinate %#v", got)
	}
}

func TestExpandRefsSupportsComposeAccessors(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()
	jvmSibling := filepath.Join(cacheRoot, "org.jetbrains.compose.components", "components-resources-jvm", "1.10.0")
	if err := os.MkdirAll(jvmSibling, 0o755); err != nil {
		t.Fatalf("seed jvm sibling: %v", err)
	}
	resolver := New(cacheRoot, t.TempDir(), nil, &catalog.Catalog{
		Versions:  map[string]string{"compose-multiplatform": "1.10.0"},
		Libraries: map[string]catalog.Library{},
		Bundles:   map[string][]string{},
	})
	coords, err := resolver.expandRefs([]modulebuild.Ref{{Kind: "raw", Value: "compose.components.resources"}}, resolver.seedPlatforms())
	if err != nil {
		t.Fatalf("expandRefs compose accessor: %v", err)
	}
	if len(coords) != 1 {
		t.Fatalf("expected one coordinate, got %#v", coords)
	}
	got := coords[0]
	if got.Group != "org.jetbrains.compose.components" || got.Module != "components-resources-jvm" || got.Version != "1.10.0" {
		t.Fatalf("unexpected compose coordinate %#v", got)
	}
}
