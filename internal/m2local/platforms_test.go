package m2local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindCachedVersionPrefersNumericVersionOrdering(t *testing.T) {
	t.Parallel()

	resolver := New(t.TempDir(), t.TempDir(), nil, nil)
	root := filepath.Join(resolver.CacheRoot, "com.example", "demo")
	for _, version := range []string{"2.0", "10.0"} {
		if err := os.MkdirAll(filepath.Join(root, version), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := resolver.findCachedVersion("com.example", "demo")
	if got != "10.0" {
		t.Fatalf("expected numeric latest version, got %q", got)
	}
}

func TestFindCachedVersionPrefersReleaseOverQualifier(t *testing.T) {
	t.Parallel()

	resolver := New(t.TempDir(), t.TempDir(), nil, nil)
	root := filepath.Join(resolver.CacheRoot, "com.example", "demo")
	for _, version := range []string{"1.2.3-alpha1", "1.2.3", "1.2.3-rc1", "1.2.3-ga"} {
		if err := os.MkdirAll(filepath.Join(root, version), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := resolver.findCachedVersion("com.example", "demo")
	if got != "1.2.3" {
		t.Fatalf("expected release version to win over qualifiers, got %q", got)
	}
}

func TestFindCachedVersionOrdersQualifiersDeterministically(t *testing.T) {
	t.Parallel()

	resolver := New(t.TempDir(), t.TempDir(), nil, nil)
	root := filepath.Join(resolver.CacheRoot, "com.example", "demo")
	for _, version := range []string{"1.0.0-beta2", "1.0.0-rc1"} {
		if err := os.MkdirAll(filepath.Join(root, version), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := resolver.findCachedVersion("com.example", "demo")
	if got != "1.0.0-rc1" {
		t.Fatalf("expected rc to win over beta, got %q", got)
	}
}
