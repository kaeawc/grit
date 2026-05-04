package m2localbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/m2local"
)

// stageGradleCacheFile writes body into
// <root>/files-2.1/<group>/<artifact>/<version>/<sha1>/<name> and
// returns the path. Mimics how grit's existing Gradle cache reader
// expects files to appear on disk.
func stageGradleCacheFile(t *testing.T, root, group, artifact, version, sha1, name string, body []byte) string {
	t.Helper()
	dir := filepath.Join(root, "files-2.1", group, artifact, version, sha1)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestCoordinateFromPathHappy(t *testing.T) {
	root := t.TempDir()
	p := stageGradleCacheFile(t, root, "org.example", "alpha", "1.0", "abc123", "alpha-1.0.jar", []byte("x"))
	coord, err := coordinateFromPath(p)
	if err != nil {
		t.Fatalf("coordinateFromPath: %v", err)
	}
	if coord.Group != "org.example" || coord.Artifact != "alpha" || coord.Version != "1.0" {
		t.Fatalf("unexpected coordinate: %+v", coord)
	}
}

func TestCoordinateFromPathRejectsNonGradleLayout(t *testing.T) {
	if _, err := coordinateFromPath("/some/random/path.jar"); err == nil {
		t.Fatalf("expected error for non-Gradle path")
	}
	if _, err := coordinateFromPath(""); err == nil {
		t.Fatalf("expected error for empty path")
	}
}

func TestCoordinateFromPathRejectsShallowPath(t *testing.T) {
	// files-2.1 marker present but no coordinate segments after it.
	if _, err := coordinateFromPath("/foo/files-2.1/only-one"); err == nil {
		t.Fatalf("expected error for shallow path")
	}
}

func TestClassifyFile(t *testing.T) {
	cases := []struct {
		name string
		want lockfile.FileKind
	}{
		{"alpha-1.0.jar", lockfile.FileKindPrimary},
		{"alpha-1.0.aar", lockfile.FileKindPrimary},
		{"alpha-1.0.pom", lockfile.FileKindPOM},
		{"alpha-1.0-sources.jar", lockfile.FileKindSources},
		{"alpha-1.0-javadoc.jar", lockfile.FileKindJavadoc},
		{"alpha-1.0.module", lockfile.FileKindModule},
		{"ALPHA-1.0.POM", lockfile.FileKindPOM}, // case-insensitive
	}
	for _, c := range cases {
		if got := classifyFile(c.name); got != c.want {
			t.Errorf("classifyFile(%q) = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestFromResolvedRejectsNil(t *testing.T) {
	if _, err := FromResolved(nil, "test"); err == nil {
		t.Fatalf("expected error for nil Resolved")
	}
}

func TestFromResolvedBuildsInputs(t *testing.T) {
	root := t.TempDir()

	jarBody := []byte("jar body")
	pomBody := []byte("<pom/>")
	jarPath := stageGradleCacheFile(t, root, "org.example", "alpha", "1.0", "abc", "alpha-1.0.jar", jarBody)
	pomPath := stageGradleCacheFile(t, root, "org.example", "alpha", "1.0", "def", "alpha-1.0.pom", pomBody)

	resolved := &m2local.Resolved{
		CompileJars: []string{jarPath},
		RuntimeJars: []string{jarPath, pomPath},
		TestJars:    []string{pomPath},
	}

	inputs, err := FromResolved(resolved, "gradle-cache")
	if err != nil {
		t.Fatalf("FromResolved: %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("expected 1 input, got %d", len(inputs))
	}
	in := inputs[0]
	if in.Coordinate.Group != "org.example" || in.Coordinate.Artifact != "alpha" || in.Coordinate.Version != "1.0" {
		t.Fatalf("unexpected coordinate: %+v", in.Coordinate)
	}
	if in.RepositoryID != "gradle-cache" {
		t.Fatalf("repositoryID not propagated: %s", in.RepositoryID)
	}
	if len(in.Files) != 2 {
		t.Fatalf("expected 2 files (dedup across lists), got %d", len(in.Files))
	}
	// Check both files are represented exactly once, with the right kind.
	var sawJar, sawPom bool
	for _, f := range in.Files {
		switch f.Name {
		case "alpha-1.0.jar":
			if sawJar {
				t.Fatalf("jar duplicated")
			}
			sawJar = true
			if f.Kind != lockfile.FileKindPrimary {
				t.Fatalf("jar should be primary, got %s", f.Kind)
			}
		case "alpha-1.0.pom":
			if sawPom {
				t.Fatalf("pom duplicated")
			}
			sawPom = true
			if f.Kind != lockfile.FileKindPOM {
				t.Fatalf("pom should be POM kind, got %s", f.Kind)
			}
		default:
			t.Fatalf("unexpected file: %s", f.Name)
		}
	}
	if !sawJar || !sawPom {
		t.Fatalf("missing files: sawJar=%v sawPom=%v", sawJar, sawPom)
	}
}

func TestFromResolvedGroupsMultipleCoordinates(t *testing.T) {
	root := t.TempDir()

	alphaJar := stageGradleCacheFile(t, root, "org.example", "alpha", "1.0", "s1", "alpha-1.0.jar", []byte("a"))
	betaJar := stageGradleCacheFile(t, root, "org.example", "beta", "2.0", "s2", "beta-2.0.jar", []byte("b"))
	betaPom := stageGradleCacheFile(t, root, "org.example", "beta", "2.0", "s3", "beta-2.0.pom", []byte("<b/>"))

	resolved := &m2local.Resolved{
		CompileJars: []string{alphaJar, betaJar},
		RuntimeJars: []string{betaPom},
	}

	inputs, err := FromResolved(resolved, "gc")
	if err != nil {
		t.Fatalf("FromResolved: %v", err)
	}
	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(inputs))
	}
	// Sorted by coordinate: alpha, beta.
	if inputs[0].Coordinate.Artifact != "alpha" || inputs[1].Coordinate.Artifact != "beta" {
		t.Fatalf("inputs not sorted: %+v", inputs)
	}
	if len(inputs[0].Files) != 1 {
		t.Fatalf("alpha should have 1 file, got %d", len(inputs[0].Files))
	}
	if len(inputs[1].Files) != 2 {
		t.Fatalf("beta should have 2 files (jar + pom), got %d", len(inputs[1].Files))
	}
}

func TestFromResolvedRejectsNonGradlePath(t *testing.T) {
	resolved := &m2local.Resolved{
		CompileJars: []string{"/not/a/gradle/cache/path.jar"},
	}
	_, err := FromResolved(resolved, "gc")
	if err == nil {
		t.Fatalf("expected error for non-gradle path")
	}
	if !strings.Contains(err.Error(), "files-2.1") {
		t.Fatalf("error should mention files-2.1 marker: %v", err)
	}
}

func TestFromResolvedEmptyResolvedReturnsEmptyInputs(t *testing.T) {
	inputs, err := FromResolved(&m2local.Resolved{}, "gc")
	if err != nil {
		t.Fatalf("unexpected error on empty Resolved: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("expected 0 inputs, got %d", len(inputs))
	}
}

func TestFromResolvedSkipsAndroidLibraries(t *testing.T) {
	// AndroidLibraries holds extraction outputs under ~/.grit/aar/, not
	// Gradle cache paths. The bridge must not try to parse those as
	// primary artifacts — the original AAR already appears in CompileJars.
	root := t.TempDir()
	aarPath := stageGradleCacheFile(t, root, "com.example", "widget", "1.0", "s1", "widget-1.0.aar", []byte("aar"))

	resolved := &m2local.Resolved{
		CompileJars: []string{aarPath},
		AndroidLibraries: []m2local.AndroidLibrary{
			{
				ID:         "com.example:widget:1.0",
				ClassesJar: "/tmp/not/a/real/grit/aar/path/classes.jar",
			},
		},
	}

	inputs, err := FromResolved(resolved, "gc")
	if err != nil {
		t.Fatalf("FromResolved: %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("expected 1 input (the AAR itself), got %d", len(inputs))
	}
	if inputs[0].Files[0].Name != "widget-1.0.aar" {
		t.Fatalf("unexpected file: %s", inputs[0].Files[0].Name)
	}
}
