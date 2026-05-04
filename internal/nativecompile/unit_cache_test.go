package nativecompile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func TestUnitTestResolvedCachePathChangesWithCatalogIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	buildFile := filepath.Join(root, "app", "build.gradle.kts")
	if err := os.MkdirAll(filepath.Dir(buildFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "gradle", "libs.versions.toml")
	if err := os.MkdirAll(filepath.Dir(catalogPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, []byte("[versions]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prj := &project.Project{
		RootDir:         root,
		VersionCatalogs: []string{catalogPath},
	}
	mod := &project.Module{
		Path:      ":app",
		Dir:       filepath.Join(root, "app"),
		BuildFile: buildFile,
	}
	deps := &modulebuild.Dependencies{}

	first, err := unitTestResolvedCachePath(prj, mod, "debug", deps)
	if err != nil {
		t.Fatalf("first cache path: %v", err)
	}
	if err := os.WriteFile(catalogPath, []byte("[versions]\nandroidx = \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := unitTestResolvedCachePath(prj, mod, "debug", deps)
	if err != nil {
		t.Fatalf("second cache path: %v", err)
	}
	if first == second {
		t.Fatalf("expected cache path to change when catalog changes")
	}
}
