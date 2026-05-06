package nativecompile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnitTestRunCachePathFromCompileStampUsesStamp(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stamp := filepath.Join(root, "compile.stamp")
	if err := os.WriteFile(stamp, []byte("compile-key-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	support := filepath.Join(root, "support.jar")
	if err := os.WriteFile(support, []byte("support"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, ok := unitTestRunCachePathFromCompileStamp(root, ":app", "debug", []string{"ExampleTest"}, stamp, []string{support})
	if !ok {
		t.Fatal("expected cache path from compile stamp")
	}
	if err := os.WriteFile(stamp, []byte("compile-key-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, ok := unitTestRunCachePathFromCompileStamp(root, ":app", "debug", []string{"ExampleTest"}, stamp, []string{support})
	if !ok {
		t.Fatal("expected cache path from updated compile stamp")
	}
	if first == second {
		t.Fatal("compile stamp should affect unit test run cache path")
	}
}

func TestUnitTestRunCachePathFromCompileStampMissingStamp(t *testing.T) {
	t.Parallel()

	if path, ok := unitTestRunCachePathFromCompileStamp(t.TempDir(), ":app", "debug", []string{"ExampleTest"}, "/missing/stamp", nil); ok || path != "" {
		t.Fatalf("expected missing stamp to disable fast cache path, got ok=%v path=%q", ok, path)
	}
}
