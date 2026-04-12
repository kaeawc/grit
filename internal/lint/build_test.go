package lint

import (
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
