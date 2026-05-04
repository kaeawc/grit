package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
)

func TestAssembleAABFailsWithoutBundletool(t *testing.T) {
	tmp := t.TempDir()

	// Ensure bundletool cannot be found.
	t.Setenv("BUNDLETOOL_JAR", "")
	t.Setenv("HOME", tmp)
	t.Setenv("ANDROID_HOME", filepath.Join(tmp, "empty-sdk"))

	prj := &project.Project{RootDir: tmp}
	mod := &project.Module{
		Path: ":app",
		Dir:  filepath.Join(tmp, "app"),
	}
	variant := project.BuildType{Name: "release"}
	tracker := perf.New(false)

	s := &compileState{}
	_, err := assembleAAB(t.Context(), s, prj, mod, variant, filepath.Join(tmp, "classes"), nil, nil, os.Stdout, os.Stderr, tracker)
	if err == nil {
		t.Fatal("expected error when bundletool is not available")
	}
	if !strings.Contains(err.Error(), "bundletool") {
		t.Fatalf("expected bundletool error, got: %v", err)
	}
}

func TestAssembleAABOutputPaths(t *testing.T) {
	// Verify that output paths are correctly computed relative to the project
	// root and variant name.
	t.Parallel()

	mod := &project.Module{Path: ":app"}
	variant := project.BuildType{Name: "release"}
	variantDir := moduleOutputRelPath(mod.Path)
	outRoot := filepath.Join("/project", "build", "grit", variantDir, variant.Name)
	unsignedAAB := filepath.Join(outRoot, "app-release-unsigned.aab")
	finalAAB := filepath.Join(outRoot, "app-release.aab")
	baseZip := filepath.Join(outRoot, "module-zip", "base.zip")

	if !strings.HasSuffix(unsignedAAB, ".aab") {
		t.Fatal("unsigned AAB should have .aab extension")
	}
	if !strings.HasSuffix(finalAAB, ".aab") {
		t.Fatal("final AAB should have .aab extension")
	}
	if !strings.HasSuffix(baseZip, ".zip") {
		t.Fatal("base zip should have .zip extension")
	}
	if !strings.Contains(unsignedAAB, "unsigned") {
		t.Fatal("unsigned AAB path should contain 'unsigned'")
	}
	if strings.Contains(finalAAB, "unsigned") {
		t.Fatal("final AAB path should not contain 'unsigned'")
	}
}

func TestAssembleAABCacheKeyIncludesBundletoolVersion(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	jar := filepath.Join(tmp, "bundletool.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	baseZip := filepath.Join(tmp, "base.zip")
	if err := os.WriteFile(baseZip, []byte("zip"), 0644); err != nil {
		t.Fatal(err)
	}

	tc1 := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}
	tc2 := &bundletoolToolchain{Version: "1.16.0", JarPath: jar}

	dir1 := aabAssemblyCacheDir(tc1, []string{baseZip}, "")
	dir2 := aabAssemblyCacheDir(tc2, []string{baseZip}, "")

	if dir1 == dir2 {
		t.Fatal("different bundletool versions should produce different cache dirs")
	}
}

func TestAssembleAABCacheKeyIncludesBundleConfig(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	jar := filepath.Join(tmp, "bundletool.jar")
	if err := os.WriteFile(jar, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	baseZip := filepath.Join(tmp, "base.zip")
	if err := os.WriteFile(baseZip, []byte("zip"), 0644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(tmp, "BundleConfig.pb")
	if err := os.WriteFile(config, []byte("config"), 0644); err != nil {
		t.Fatal(err)
	}

	tc := &bundletoolToolchain{Version: "1.15.6", JarPath: jar}

	dirNoConfig := aabAssemblyCacheDir(tc, []string{baseZip}, "")
	dirWithConfig := aabAssemblyCacheDir(tc, []string{baseZip}, config)

	if dirNoConfig == dirWithConfig {
		t.Fatal("presence of BundleConfig should change cache dir")
	}
}
