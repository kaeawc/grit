package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestCacheIdentityForBuildGritDirUsesContentFingerprint(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "build", "grit", "app", "debug", "classes")
	file := testutil.WriteFile(t, dir, "Example.class", "bytecode")
	first := cacheIdentityForInput(dir)
	if !strings.HasPrefix(first, "dirhash:") {
		t.Fatalf("expected build/grit dir content identity, got %q", first)
	}
	nextTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, nextTime, nextTime); err != nil {
		t.Fatal(err)
	}
	second := cacheIdentityForInput(dir)
	if second != first {
		t.Fatalf("mtime-only change should not affect build/grit dir identity: %q != %q", second, first)
	}
}

func TestCacheIdentityForMaterializedMavenJarUsesContentFingerprint(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	jar := testutil.WriteFile(t, filepath.Join(root, ".grit", "worktree", "materialized-m2", "org", "example", "lib", "1.0"), "lib-1.0.jar", "fake-jar")
	first := cacheIdentityForInput(jar)
	if !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("expected materialized maven file content identity, got %q", first)
	}
	nextTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(jar, nextTime, nextTime); err != nil {
		t.Fatal(err)
	}
	second := cacheIdentityForInput(jar)
	if second != first {
		t.Fatalf("mtime-only change should not affect materialized maven file identity: %q != %q", second, first)
	}
}

func TestCacheIdentityForMaterializedMavenJarUsesSHA256Sidecar(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, ".grit", "worktree", "materialized-m2", "org", "example", "lib", "1.0")
	jar := testutil.WriteFile(t, dir, "lib-1.0.jar", "fake-jar")
	sidecar := jar + ".sha256"
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(sidecar, []byte(digest), 0o644); err != nil {
		t.Fatal(err)
	}

	first := cacheIdentityForInput(jar)
	if first != "sha256-sidecar:"+digest {
		t.Fatalf("expected materialized maven sidecar identity, got %q", first)
	}
	nextTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(jar, nextTime, nextTime); err != nil {
		t.Fatal(err)
	}
	second := cacheIdentityForInput(jar)
	if second != first {
		t.Fatalf("mtime-only change should not affect sidecar identity: %q != %q", second, first)
	}
	if err := os.WriteFile(sidecar, []byte(strings.Repeat("a", 64)), 0o644); err != nil {
		t.Fatal(err)
	}
	third := cacheIdentityForInput(jar)
	if third == first {
		t.Fatalf("sidecar change should affect identity: %q", third)
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
