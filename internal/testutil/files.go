package testutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func WriteFile(t testing.TB, root, rel, contents string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func WriteFiles(t testing.TB, root string, files map[string]string) {
	t.Helper()
	for rel, contents := range files {
		WriteFile(t, root, rel, contents)
	}
}

func Touch(t testing.TB, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}
