package nativecompile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestBaselineProfilesForVariant_DiscoversFiles(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path: ":app",
		Dir:  root,
		Type: "android-application",
		BuildTypes: map[string]project.BuildType{
			"release": {Name: "release"},
		},
	}

	// Place baseline-prof.txt in src/main and startup-prof.txt in src/release.
	mainDir := filepath.Join(root, "src", "main")
	releaseDir := filepath.Join(root, "src", "release")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "baseline-prof.txt"), []byte("Lcom/example/App;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "startup-prof.txt"), []byte("Lcom/example/Startup;"), 0o644); err != nil {
		t.Fatal(err)
	}

	profiles := baselineProfilesForVariant(mod, "release")
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profile files, got %d: %v", len(profiles), profiles)
	}
	if profiles[0] != filepath.Join(mainDir, "baseline-prof.txt") {
		t.Errorf("expected baseline-prof.txt from main, got %s", profiles[0])
	}
	if profiles[1] != filepath.Join(releaseDir, "startup-prof.txt") {
		t.Errorf("expected startup-prof.txt from release, got %s", profiles[1])
	}
}

func TestBaselineProfilesForVariant_WithFlavors(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:             ":app",
		Dir:              root,
		Type:             "android-application",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"release": {Name: "release"},
		},
	}

	// Place profiles in flavor source root.
	freeDir := filepath.Join(root, "src", "free")
	if err := os.MkdirAll(freeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(freeDir, "baseline-prof.txt"), []byte("Lcom/example/Free;"), 0o644); err != nil {
		t.Fatal(err)
	}

	profiles := baselineProfilesForVariant(mod, "freeRelease")
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile file, got %d: %v", len(profiles), profiles)
	}
	if profiles[0] != filepath.Join(freeDir, "baseline-prof.txt") {
		t.Errorf("expected baseline-prof.txt from free source root, got %s", profiles[0])
	}
}

func TestBaselineProfilesForVariant_NilModule(t *testing.T) {
	profiles := baselineProfilesForVariant(nil, "release")
	if profiles != nil {
		t.Fatalf("expected nil for nil module, got %v", profiles)
	}
}

func TestBaselineProfilesForVariant_NoFiles(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path: ":app",
		Dir:  root,
		Type: "android-application",
	}

	profiles := baselineProfilesForVariant(mod, "debug")
	if len(profiles) != 0 {
		t.Fatalf("expected no profile files, got %v", profiles)
	}
}

func TestBaselineProfilesForVariant_IgnoresDirectories(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path: ":app",
		Dir:  root,
		Type: "android-application",
	}

	// Create a directory named baseline-prof.txt — should be ignored.
	dir := filepath.Join(root, "src", "main", "baseline-prof.txt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	profiles := baselineProfilesForVariant(mod, "debug")
	if len(profiles) != 0 {
		t.Fatalf("expected directories to be ignored, got %v", profiles)
	}
}
