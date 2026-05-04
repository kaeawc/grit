package nativecompile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndRestoreSharedAABAssembly(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	// Create a fake unsigned AAB.
	unsignedAAB := filepath.Join(tmp, "app-unsigned.aab")
	if err := os.WriteFile(unsignedAAB, []byte("unsigned-aab-content"), 0644); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(tmp, "cache", "aab", "abc123")

	// Save to cache.
	if err := saveSharedAABAssembly(unsignedAAB, cacheDir); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify cached file exists.
	cachedAAB := filepath.Join(cacheDir, "unsigned.aab")
	if !pathIsFile(cachedAAB) {
		t.Fatal("expected cached unsigned.aab to exist")
	}
	data, err := os.ReadFile(cachedAAB)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "unsigned-aab-content" {
		t.Fatalf("cached content mismatch: got %q", string(data))
	}

	// Restore to a new location.
	restored := filepath.Join(tmp, "restored.aab")
	if !restoreSharedAABAssembly(restored, cacheDir) {
		t.Fatal("restore returned false")
	}
	data, err = os.ReadFile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "unsigned-aab-content" {
		t.Fatalf("restored content mismatch: got %q", string(data))
	}
}

func TestRestoreSharedAABAssemblyReturnsFalseWhenMissing(t *testing.T) {
	t.Parallel()
	if restoreSharedAABAssembly("/tmp/out.aab", "/nonexistent/cache") {
		t.Fatal("expected false for missing cache")
	}
}

func TestSaveSharedAABAssemblySkipsWhenAlreadyCached(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	cacheDir := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-populate cache with existing content.
	cachedAAB := filepath.Join(cacheDir, "unsigned.aab")
	if err := os.WriteFile(cachedAAB, []byte("existing-cached"), 0644); err != nil {
		t.Fatal(err)
	}

	// Save a different file — should be a no-op since cache already exists.
	newAAB := filepath.Join(tmp, "new.aab")
	if err := os.WriteFile(newAAB, []byte("new-content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveSharedAABAssembly(newAAB, cacheDir); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify the cache was NOT overwritten.
	data, _ := os.ReadFile(cachedAAB)
	if string(data) != "existing-cached" {
		t.Fatalf("cache should not be overwritten, got %q", string(data))
	}
}

func TestSaveSharedAABAssemblyCleansTmpOnSuccess(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	unsignedAAB := filepath.Join(tmp, "app-unsigned.aab")
	if err := os.WriteFile(unsignedAAB, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(tmp, "cache", "aab", "def456")
	if err := saveSharedAABAssembly(unsignedAAB, cacheDir); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// The .tmp directory should not linger after successful save.
	tmpDir := cacheDir + ".tmp"
	if pathIsDir(tmpDir) {
		t.Fatal("temporary directory should be cleaned up after save")
	}
}
