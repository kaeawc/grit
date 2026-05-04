package mavenlocalroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultFallsBackToStandardM2Repository(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MAVEN_USER_HOME", "")

	got := Default()
	want := filepath.Join(home, ".m2", "repository")
	if got != want {
		t.Fatalf("Default: got %q want %q", got, want)
	}
}

func TestDefaultUsesAbsoluteSettingsOverride(t *testing.T) {
	home := t.TempDir()
	confDir := filepath.Join(home, ".m2")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	override := filepath.Join(home, "custom-repo")
	if err := os.WriteFile(filepath.Join(confDir, "settings.xml"), []byte("<settings><localRepository>"+override+"</localRepository></settings>"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MAVEN_USER_HOME", "")

	if got := Default(); got != override {
		t.Fatalf("Default absolute override: got %q want %q", got, override)
	}
}

func TestDefaultUsesRelativeSettingsOverrideWithinMavenConfigHome(t *testing.T) {
	home := t.TempDir()
	confDir := filepath.Join(home, "maven-conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "settings.xml"), []byte("<settings><localRepository>repo-cache</localRepository></settings>"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MAVEN_USER_HOME", confDir)

	got := Default()
	want := filepath.Join(confDir, "repo-cache")
	if got != want {
		t.Fatalf("Default relative override: got %q want %q", got, want)
	}
}

func TestDefaultIgnoresInvalidSettingsXML(t *testing.T) {
	home := t.TempDir()
	confDir := filepath.Join(home, ".m2")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "settings.xml"), []byte("<settings><localRepository>"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MAVEN_USER_HOME", "")

	got := Default()
	want := filepath.Join(home, ".m2", "repository")
	if got != want {
		t.Fatalf("Default invalid settings fallback: got %q want %q", got, want)
	}
}
