package lint

import (
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func TestCacheKeyStable(t *testing.T) {
	a := sampleAction()
	if a.CacheKey() != a.CacheKey() { //nolint:staticcheck
		t.Fatal("cache key not stable across calls")
	}
}

func TestCacheKeyDiffersOnToolVersion(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.ToolVersion = "31.1.0"
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different ToolVersion must change cache key")
	}
}

func TestCacheKeyDiffersOnManifestPath(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.ManifestPath = "other/AndroidManifest.xml"
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different ManifestPath must change cache key")
	}
}

func TestCacheKeyDiffersOnLintConfig(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.LintConfig = "lint-strict.xml"
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different LintConfig must change cache key")
	}
}

func TestCacheKeyDiffersOnBaseline(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Baseline = "lint-baseline-v2.xml"
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different Baseline must change cache key")
	}
}

func TestCacheKeyDiffersOnSourceContent(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Sources = append([]FileInput(nil), base.Sources...)
	mod.Sources[0].Hash = cas.HashBytes([]byte("changed-source"))
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different source hash must change cache key")
	}
}

func TestCacheKeyDiffersOnSourcePath(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Sources = append([]FileInput(nil), base.Sources...)
	mod.Sources[0].Path = "src/main/java/Renamed.kt"
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different source path must change cache key")
	}
}

func TestCacheKeyDiffersOnAddedSource(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Sources = append(append([]FileInput(nil), base.Sources...),
		FileInput{Path: "src/main/java/Extra.kt", Hash: cas.HashBytes([]byte("extra"))},
	)
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("adding a source file must change cache key")
	}
}

func TestCacheKeyInvariantToSourceOrder(t *testing.T) {
	base := sampleAction()
	shuffled := base
	shuffled.Sources = []FileInput{base.Sources[1], base.Sources[0]}
	if base.CacheKey() != shuffled.CacheKey() {
		t.Fatal("source order must not affect cache key")
	}
}

func TestCacheKeyInvariantToClasspathOrder(t *testing.T) {
	base := sampleAction()
	shuffled := base
	shuffled.CompileClasspath = []FileInput{base.CompileClasspath[1], base.CompileClasspath[0]}
	if base.CacheKey() != shuffled.CacheKey() {
		t.Fatal("classpath order must not affect cache key")
	}
}

func TestCacheKeyInvariantToLintRuleOrder(t *testing.T) {
	base := sampleAction()
	shuffled := base
	shuffled.LintRules = []FileInput{base.LintRules[1], base.LintRules[0]}
	if base.CacheKey() != shuffled.CacheKey() {
		t.Fatal("lint rule order must not affect cache key")
	}
}

func TestCacheKeyInvariantToResourceDirOrder(t *testing.T) {
	base := sampleAction()
	shuffled := base
	shuffled.ResourceDirs = []string{base.ResourceDirs[1], base.ResourceDirs[0]}
	if base.CacheKey() != shuffled.CacheKey() {
		t.Fatal("resource dir order must not affect cache key")
	}
}

func TestCacheKeyDiffersOnClasspathContent(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.CompileClasspath = append([]FileInput(nil), base.CompileClasspath...)
	mod.CompileClasspath[0].Hash = cas.HashBytes([]byte("new-jar"))
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different classpath hash must change cache key")
	}
}

func TestCacheKeyDiffersOnLintRuleContent(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.LintRules = append([]FileInput(nil), base.LintRules...)
	mod.LintRules[0].Hash = cas.HashBytes([]byte("updated-rule"))
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different lint rule hash must change cache key")
	}
}

func TestCacheKeyDiffersOnResourceDir(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.ResourceDirs = []string{"src/main/res", "src/main/res-extra"}
	if base.CacheKey() == mod.CacheKey() {
		t.Fatal("different resource dirs must change cache key")
	}
}

func sampleAction() Action {
	return Action{
		Sources: []FileInput{
			{Path: "src/main/java/Foo.kt", Hash: cas.HashBytes([]byte("foo-source"))},
			{Path: "src/main/java/Bar.kt", Hash: cas.HashBytes([]byte("bar-source"))},
		},
		ResourceDirs: []string{"src/main/res", "src/debug/res"},
		ManifestPath: "src/main/AndroidManifest.xml",
		CompileClasspath: []FileInput{
			{Path: "libs/core.jar", Hash: cas.HashBytes([]byte("core-jar"))},
			{Path: "libs/support.jar", Hash: cas.HashBytes([]byte("support-jar"))},
		},
		LintRules: []FileInput{
			{Path: "rules/custom-lint.jar", Hash: cas.HashBytes([]byte("custom-rules"))},
			{Path: "rules/extra-lint.jar", Hash: cas.HashBytes([]byte("extra-rules"))},
		},
		LintConfig:  "lint.xml",
		Baseline:    "lint-baseline.xml",
		ToolVersion: "30.4.2",
	}
}
