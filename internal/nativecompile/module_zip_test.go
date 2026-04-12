package nativecompile

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestAssembleModuleZipRequiresManifest(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "base.zip")
	err := assembleModuleZip(moduleZipInputs{}, out)
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !strings.Contains(err.Error(), "manifest path is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssembleModuleZipRejectsMissingManifest(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath: "/nonexistent/AndroidManifest.xml",
	}, out)
	if err == nil {
		t.Fatal("expected error for missing manifest file")
	}
	if !strings.Contains(err.Error(), "manifest not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssembleModuleZipManifestOnly(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "out", "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath: manifest,
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(entries), entries)
	}
	if entries[0] != "manifest/AndroidManifest.xml" {
		t.Fatalf("expected manifest entry, got %q", entries[0])
	}
}

func TestAssembleModuleZipWithDex(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}
	dexDir := filepath.Join(tmp, "dex")
	if err := os.MkdirAll(dexDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dexDir, "classes.dex"), []byte("dex1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dexDir, "classes2.dex"), []byte("dex2"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath: manifest,
		DexDir:       dexDir,
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	want := []string{
		"dex/classes.dex",
		"dex/classes2.dex",
		"manifest/AndroidManifest.xml",
	}
	if !stringSlicesEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
}

func TestAssembleModuleZipWithResources(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}
	resDir := filepath.Join(tmp, "res")
	if err := os.MkdirAll(filepath.Join(resDir, "values"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "values", "strings.flat"), []byte("res"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath: manifest,
		ResourceDir:  resDir,
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	want := []string{
		"manifest/AndroidManifest.xml",
		"res/values/strings.flat",
	}
	if !stringSlicesEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
}

func TestAssembleModuleZipWithAssets(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}
	assetsDir := filepath.Join(tmp, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath: manifest,
		AssetsDir:    assetsDir,
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	want := []string{
		"assets/config.json",
		"manifest/AndroidManifest.xml",
	}
	if !stringSlicesEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
}

func TestAssembleModuleZipWithNativeLibs(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}

	armDir := filepath.Join(tmp, "arm64-v8a")
	if err := os.MkdirAll(armDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(armDir, "libnative.so"), []byte("elf"), 0644); err != nil {
		t.Fatal(err)
	}

	x86Dir := filepath.Join(tmp, "x86_64")
	if err := os.MkdirAll(x86Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x86Dir, "libnative.so"), []byte("elf"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath: manifest,
		NativeLibDirs: map[string]string{
			"arm64-v8a": armDir,
			"x86_64":    x86Dir,
		},
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	want := []string{
		"lib/arm64-v8a/libnative.so",
		"lib/x86_64/libnative.so",
		"manifest/AndroidManifest.xml",
	}
	if !stringSlicesEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
}

func TestAssembleModuleZipFullModule(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	// Manifest.
	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}

	// Dex.
	dexDir := filepath.Join(tmp, "dex")
	if err := os.MkdirAll(dexDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dexDir, "classes.dex"), []byte("dex"), 0644); err != nil {
		t.Fatal(err)
	}

	// Resources.
	resDir := filepath.Join(tmp, "res")
	if err := os.MkdirAll(filepath.Join(resDir, "drawable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "drawable", "icon.flat"), []byte("img"), 0644); err != nil {
		t.Fatal(err)
	}

	// Assets.
	assetsDir := filepath.Join(tmp, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "data.bin"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Native libs.
	armDir := filepath.Join(tmp, "arm64-v8a")
	if err := os.MkdirAll(armDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(armDir, "libapp.so"), []byte("elf"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath:  manifest,
		DexDir:        dexDir,
		ResourceDir:   resDir,
		AssetsDir:     assetsDir,
		NativeLibDirs: map[string]string{"arm64-v8a": armDir},
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	want := []string{
		"assets/data.bin",
		"dex/classes.dex",
		"lib/arm64-v8a/libapp.so",
		"manifest/AndroidManifest.xml",
		"res/drawable/icon.flat",
	}
	if !stringSlicesEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
}

func TestAssembleModuleZipSkipsMissingOptionalDirs(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath:  manifest,
		DexDir:        "/nonexistent/dex",
		ResourceDir:   "/nonexistent/res",
		AssetsDir:     "/nonexistent/assets",
		NativeLibDirs: map[string]string{"arm64-v8a": "/nonexistent/arm64"},
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	if len(entries) != 1 || entries[0] != "manifest/AndroidManifest.xml" {
		t.Fatalf("expected only manifest, got %v", entries)
	}
}

func TestAssembleModuleZipFileContents(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	manifestContent := "<manifest package=\"com.example\"/>"
	if err := os.WriteFile(manifest, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{ManifestPath: manifest}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file content is preserved.
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == "manifest/AndroidManifest.xml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 256)
			n, _ := rc.Read(buf)
			rc.Close()
			got := string(buf[:n])
			if got != manifestContent {
				t.Fatalf("manifest content = %q, want %q", got, manifestContent)
			}
			return
		}
	}
	t.Fatal("manifest entry not found in zip")
}

// zipEntries returns the sorted list of entry names in a zip file.
func zipEntries(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
