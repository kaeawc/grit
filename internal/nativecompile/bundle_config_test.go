package nativecompile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindBundleConfigReturnsEmpty(t *testing.T) {
	t.Parallel()
	got := findBundleConfig(t.TempDir())
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestFindBundleConfigProtoBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pb := filepath.Join(dir, "BundleConfig.pb")
	if err := os.WriteFile(pb, []byte("proto"), 0644); err != nil {
		t.Fatal(err)
	}
	got := findBundleConfig(dir)
	if got != pb {
		t.Fatalf("expected %s, got %q", pb, got)
	}
}

func TestFindBundleConfigSnakeCase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pb := filepath.Join(dir, "bundle_config.pb")
	if err := os.WriteFile(pb, []byte("proto"), 0644); err != nil {
		t.Fatal(err)
	}
	got := findBundleConfig(dir)
	if got != pb {
		t.Fatalf("expected %s, got %q", pb, got)
	}
}

func TestFindBundleConfigJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	j := filepath.Join(dir, "BundleConfig.json")
	if err := os.WriteFile(j, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	got := findBundleConfig(dir)
	if got != j {
		t.Fatalf("expected %s, got %q", j, got)
	}
}

func TestFindBundleConfigPrefersProtoBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Create both — proto binary should win.
	pb := filepath.Join(dir, "BundleConfig.pb")
	if err := os.WriteFile(pb, []byte("proto"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BundleConfig.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	got := findBundleConfig(dir)
	if got != pb {
		t.Fatalf("expected proto binary %s, got %q", pb, got)
	}
}

func TestFindBundleConfigNonexistentDir(t *testing.T) {
	t.Parallel()
	got := findBundleConfig("/nonexistent/module/dir")
	if got != "" {
		t.Fatalf("expected empty for nonexistent dir, got %q", got)
	}
}
