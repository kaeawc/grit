package nativecompile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestSharedAABAssemblyRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache", "aab-hash")
	aabPath := filepath.Join(root, "app-release-unsigned.aab")
	testutil.WriteFile(t, root, "app-release-unsigned.aab", "aab-bytes")

	// Save to shared cache.
	if err := saveSharedAABAssembly(aabPath, cacheDir); err != nil {
		t.Fatalf("save shared AAB assembly: %v", err)
	}

	// Restore from shared cache.
	restoredAAB := filepath.Join(root, "restored.aab")
	if !restoreSharedAABAssembly(restoredAAB, cacheDir) {
		t.Fatal("expected shared AAB assembly cache to restore")
	}

	got, err := os.ReadFile(restoredAAB)
	if err != nil || string(got) != "aab-bytes" {
		t.Fatalf("restored AAB mismatch: %q %v", got, err)
	}
}

func TestSharedAABAssemblyRestoreFailsWhenEmpty(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cacheDir := filepath.Join(root, "empty-cache")
	restoredAAB := filepath.Join(root, "restored.aab")

	if restoreSharedAABAssembly(restoredAAB, cacheDir) {
		t.Fatal("expected restore to fail for empty cache")
	}
}

func TestSharedAABAssemblySkipsSaveWhenAlreadyCached(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache", "aab-hash")
	aabPath := filepath.Join(root, "app.aab")
	testutil.WriteFile(t, root, "app.aab", "original")

	if err := saveSharedAABAssembly(aabPath, cacheDir); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Write different content and save again — should be a no-op.
	if err := os.WriteFile(aabPath, []byte("different"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveSharedAABAssembly(aabPath, cacheDir); err != nil {
		t.Fatalf("second save: %v", err)
	}

	// Cached version should still have original content.
	got, err := os.ReadFile(filepath.Join(cacheDir, "unsigned.aab"))
	if err != nil || string(got) != "original" {
		t.Fatalf("cached AAB should be original, got %q %v", got, err)
	}
}

func TestSharedCompileCacheRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	classesDir := filepath.Join(root, "classes")
	cacheDir := filepath.Join(root, "cache")
	moduleJar := filepath.Join(root, "module-classes.jar")
	testutil.WriteFile(t, classesDir, "com/example/App.class", "bytecode")
	testutil.WriteFile(t, root, "compile.stamp", "fingerprint")
	testutil.WriteFile(t, root, "module-classes.jar", "jar-bytes")
	if err := saveSharedCompileCache(classesDir, moduleJar, cacheDir); err != nil {
		t.Fatalf("save shared compile cache: %v", err)
	}

	restoredClasses := filepath.Join(root, "restored-classes")
	restoredJar := filepath.Join(root, "restored-module-classes.jar")
	if !restoreSharedCompileCache(restoredClasses, restoredJar, cacheDir) {
		t.Fatal("expected shared compile cache to restore")
	}

	if got, err := os.ReadFile(filepath.Join(restoredClasses, "com/example/App.class")); err != nil || string(got) != "bytecode" {
		t.Fatalf("restored classes mismatch: %q %v", got, err)
	}
	if got, err := os.ReadFile(restoredJar); err != nil || string(got) != "jar-bytes" {
		t.Fatalf("restored jar mismatch: %q %v", got, err)
	}
}
