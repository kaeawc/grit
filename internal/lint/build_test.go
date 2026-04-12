package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestActionFromVariant_BaselinePathThreaded(t *testing.T) {
	v := project.ResolvedVariant{
		LintBaselinePath: "lint-baseline.xml",
	}
	action := ActionFromVariant(v)
	if action.Baseline != "lint-baseline.xml" {
		t.Fatalf("expected Baseline %q, got %q", "lint-baseline.xml", action.Baseline)
	}
}

func TestActionFromVariant_EmptyBaseline(t *testing.T) {
	v := project.ResolvedVariant{}
	action := ActionFromVariant(v)
	if action.Baseline != "" {
		t.Fatalf("expected empty Baseline, got %q", action.Baseline)
	}
}

func TestActionFromVariant_ManifestFromPaths(t *testing.T) {
	v := project.ResolvedVariant{
		ManifestPaths: []string{"src/main/AndroidManifest.xml", "src/debug/AndroidManifest.xml"},
	}
	action := ActionFromVariant(v)
	if action.ManifestPath != "src/main/AndroidManifest.xml" {
		t.Fatalf("expected ManifestPath %q, got %q", "src/main/AndroidManifest.xml", action.ManifestPath)
	}
}

func TestActionFromVariant_ResourceDirsThreaded(t *testing.T) {
	v := project.ResolvedVariant{
		ResourceArtifactPaths: []string{"src/main/res", "src/debug/res"},
	}
	action := ActionFromVariant(v)
	if got, want := action.ResourceDirs, []string{"src/main/res", "src/debug/res"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected ResourceDirs %#v, got %#v", want, got)
	}
}

func TestActionFromVariant_BaselineAffectsCacheKey(t *testing.T) {
	v1 := project.ResolvedVariant{
		LintBaselinePath: "lint-baseline.xml",
	}
	v2 := project.ResolvedVariant{
		LintBaselinePath: "lint-baseline-v2.xml",
	}
	a1 := ActionFromVariant(v1)
	a2 := ActionFromVariant(v2)
	if a1.CacheKey() == a2.CacheKey() {
		t.Fatal("different baseline paths must produce different cache keys")
	}
}

func TestActionFromVariant_SameVariantSameCacheKey(t *testing.T) {
	v := project.ResolvedVariant{
		LintBaselinePath: "lint-baseline.xml",
		ManifestPaths:    []string{"src/main/AndroidManifest.xml"},
	}
	a1 := ActionFromVariant(v)
	a2 := ActionFromVariant(v)
	if a1.CacheKey() != a2.CacheKey() {
		t.Fatal("same variant must produce identical cache keys")
	}
}

func TestActionFromVariant_SourceFilesThreadedAndHashed(t *testing.T) {
	root := t.TempDir()
	mainRoot := filepath.Join(root, "src", "main")
	debugRoot := filepath.Join(root, "src", "debug")
	mustWriteLintFile(t, filepath.Join(mainRoot, "java", "MainActivity.kt"), "class MainActivity")
	mustWriteLintFile(t, filepath.Join(debugRoot, "java", "DebugOnly.java"), "class DebugOnly {}")
	mustWriteLintFile(t, filepath.Join(mainRoot, "res", "layout.xml"), "<LinearLayout/>")

	action := ActionFromVariant(project.ResolvedVariant{
		SourceRoots: []string{mainRoot, debugRoot},
	})
	if got, want := len(action.Sources), 2; got != want {
		t.Fatalf("source input count = %d, want %d (%#v)", got, want, action.Sources)
	}
	if action.Sources[0].Path != filepath.Join(debugRoot, "java", "DebugOnly.java") && action.Sources[1].Path != filepath.Join(debugRoot, "java", "DebugOnly.java") {
		t.Fatalf("expected debug Java source in %#v", action.Sources)
	}
	if action.Sources[0].Hash == action.Sources[1].Hash {
		t.Fatalf("expected distinct source hashes, got %#v", action.Sources)
	}
}

func TestActionFromVariant_SourceContentAffectsCacheKey(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src", "main", "java", "MainActivity.kt")
	mustWriteLintFile(t, source, "class MainActivity")
	v := project.ResolvedVariant{
		SourceRoots: []string{filepath.Join(root, "src", "main")},
	}
	before := ActionFromVariant(v).CacheKey()
	mustWriteLintFile(t, source, "class MainActivityChanged")
	after := ActionFromVariant(v).CacheKey()
	if before == after {
		t.Fatal("changing source content must change cache key")
	}
}

func TestActionFromVariantInModule_DiscoversLintFiles(t *testing.T) {
	moduleDir := t.TempDir()
	mustWriteLintFile(t, filepath.Join(moduleDir, "lint.xml"), "<lint/>")
	mustWriteLintFile(t, filepath.Join(moduleDir, "lint-baseline.xml"), "<issues/>")

	action := ActionFromVariantInModule(project.ResolvedVariant{}, moduleDir)
	if got, want := action.LintConfig, filepath.Join(moduleDir, "lint.xml"); got != want {
		t.Fatalf("LintConfig = %q, want %q", got, want)
	}
	if got, want := action.Baseline, filepath.Join(moduleDir, "lint-baseline.xml"); got != want {
		t.Fatalf("Baseline = %q, want %q", got, want)
	}
}

func mustWriteLintFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
