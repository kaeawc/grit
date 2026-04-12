package nativecompile

import (
	"testing"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestCloneAncestryCopiesMap(t *testing.T) {
	t.Parallel()

	orig := map[string]bool{":app#debug": true}
	cloned := cloneAncestry(orig)
	if len(cloned) != 1 || !cloned[":app#debug"] {
		t.Fatalf("clone mismatch: %#v", cloned)
	}
	cloned[":lib#debug"] = true
	if len(orig) != 1 || orig[":lib#debug"] {
		t.Fatalf("clone should not mutate original: %#v", orig)
	}
}

func TestModuleCompileCacheDirStableForSameInputs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := testutil.WriteFile(t, dir, "input.txt", "hello")
	first := moduleCompileCacheDir(":app", "debug", "abc123", []string{src})
	second := moduleCompileCacheDir(":app", "debug", "abc123", []string{src})
	if first != second {
		t.Fatalf("cache dir should be stable: %q != %q", first, second)
	}
	if other := moduleCompileCacheDir(":app", "release", "abc123", []string{src}); other == first {
		t.Fatalf("variant should affect cache dir: %q", other)
	}
	if other := moduleCompileCacheDir(":app", "debug", "def456", []string{src}); other == first {
		t.Fatalf("configHash should affect cache dir: %q", other)
	}
}

func TestAabAssemblyCacheDirStableForSameInputs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jar := testutil.WriteFile(t, dir, "bundletool.jar", "fake-jar")
	zip1 := testutil.WriteFile(t, dir, "base.zip", "base-contents")
	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}

	first := aabAssemblyCacheDir(tc, []string{zip1}, "")
	second := aabAssemblyCacheDir(tc, []string{zip1}, "")
	if first != second {
		t.Fatalf("cache dir should be stable: %q != %q", first, second)
	}
}

func TestAabAssemblyCacheDirVariesByVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jar := testutil.WriteFile(t, dir, "bundletool.jar", "fake-jar")
	zip1 := testutil.WriteFile(t, dir, "base.zip", "base-contents")

	tc1 := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}
	tc2 := &bundletoolToolchain{Version: "1.16.0", JarPath: jar}

	d1 := aabAssemblyCacheDir(tc1, []string{zip1}, "")
	d2 := aabAssemblyCacheDir(tc2, []string{zip1}, "")
	if d1 == d2 {
		t.Fatalf("different bundletool versions should produce different cache dirs")
	}
}

func TestAabAssemblyCacheDirVariesByModuleZips(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jar := testutil.WriteFile(t, dir, "bundletool.jar", "fake-jar")
	zip1 := testutil.WriteFile(t, dir, "base.zip", "base-contents")
	zip2 := testutil.WriteFile(t, dir, "feature.zip", "feature-contents")
	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}

	d1 := aabAssemblyCacheDir(tc, []string{zip1}, "")
	d2 := aabAssemblyCacheDir(tc, []string{zip1, zip2}, "")
	if d1 == d2 {
		t.Fatalf("different module zips should produce different cache dirs")
	}
}

func TestAabAssemblyCacheDirVariesByBundleConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jar := testutil.WriteFile(t, dir, "bundletool.jar", "fake-jar")
	zip1 := testutil.WriteFile(t, dir, "base.zip", "base-contents")
	config := testutil.WriteFile(t, dir, "BundleConfig.pb", "config-data")
	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}

	d1 := aabAssemblyCacheDir(tc, []string{zip1}, "")
	d2 := aabAssemblyCacheDir(tc, []string{zip1}, config)
	if d1 == d2 {
		t.Fatalf("bundle config presence should affect cache dir")
	}
}

func TestAabAssemblyCacheDirHandlesNilToolchain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zip1 := testutil.WriteFile(t, dir, "base.zip", "base-contents")

	// Should not panic with nil toolchain.
	d := aabAssemblyCacheDir(nil, []string{zip1}, "")
	if d == "" {
		t.Fatal("expected non-empty cache dir for nil toolchain")
	}
}
