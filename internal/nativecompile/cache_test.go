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
	first := moduleCompileCacheDir(":app", "debug", []string{src})
	second := moduleCompileCacheDir(":app", "debug", []string{src})
	if first != second {
		t.Fatalf("cache dir should be stable: %q != %q", first, second)
	}
	if other := moduleCompileCacheDir(":app", "release", []string{src}); other == first {
		t.Fatalf("variant should affect cache dir: %q", other)
	}
}
