package nativecompile

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestExtractProtoAPK(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	// Create a fake proto-format APK (zip) with the expected structure.
	protoAPK := filepath.Join(tmp, "proto-resources.apk")
	createTestZip(t, protoAPK, map[string]string{
		"AndroidManifest.xml":    "<manifest proto/>",
		"resources.pb":           "proto-resource-table",
		"res/values/strings.xml": "string-resources",
		"res/drawable/icon.png":  "png-data",
	})

	destDir := filepath.Join(tmp, "extracted")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := extractProtoAPK(protoAPK, destDir); err != nil {
		t.Fatalf("extractProtoAPK failed: %v", err)
	}

	// Verify extracted files exist with correct content.
	wantFiles := map[string]string{
		"AndroidManifest.xml":    "<manifest proto/>",
		"resources.pb":           "proto-resource-table",
		"res/values/strings.xml": "string-resources",
		"res/drawable/icon.png":  "png-data",
	}
	for name, wantContent := range wantFiles {
		path := filepath.Join(destDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("expected file %s: %v", name, err)
			continue
		}
		if string(data) != wantContent {
			t.Errorf("file %s: got %q, want %q", name, string(data), wantContent)
		}
	}
}

func TestExtractProtoAPKMissingFile(t *testing.T) {
	t.Parallel()
	err := extractProtoAPK("/nonexistent/proto.apk", t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing APK")
	}
	if !strings.Contains(err.Error(), "open proto APK") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAssembleModuleZipWithResourceTable(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}
	resourcesPB := filepath.Join(tmp, "resources.pb")
	if err := os.WriteFile(resourcesPB, []byte("proto-table"), 0644); err != nil {
		t.Fatal(err)
	}
	resDir := filepath.Join(tmp, "res")
	if err := os.MkdirAll(filepath.Join(resDir, "values"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "values", "strings.xml"), []byte("strings"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath:      manifest,
		ResourceTablePath: resourcesPB,
		ResourceDir:       resDir,
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	want := []string{
		"manifest/AndroidManifest.xml",
		"res/values/strings.xml",
		"resources.pb",
	}
	if !stringSlicesEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}

	// Verify resources.pb content is preserved.
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name == "resources.pb" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 256)
			n, _ := rc.Read(buf)
			rc.Close()
			if string(buf[:n]) != "proto-table" {
				t.Fatalf("resources.pb content = %q, want %q", string(buf[:n]), "proto-table")
			}
			return
		}
	}
	t.Fatal("resources.pb entry not found in zip")
}

func TestAssembleModuleZipSkipsMissingResourceTable(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath:      manifest,
		ResourceTablePath: "/nonexistent/resources.pb",
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	if len(entries) != 1 || entries[0] != "manifest/AndroidManifest.xml" {
		t.Fatalf("expected only manifest, got %v", entries)
	}
}

func TestAssembleModuleZipFullWithProtoResources(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	// Manifest.
	manifest := filepath.Join(tmp, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte("<manifest/>"), 0644); err != nil {
		t.Fatal(err)
	}

	// Resource table.
	resourcesPB := filepath.Join(tmp, "resources.pb")
	if err := os.WriteFile(resourcesPB, []byte("table"), 0644); err != nil {
		t.Fatal(err)
	}

	// Resources.
	resDir := filepath.Join(tmp, "res")
	if err := os.MkdirAll(filepath.Join(resDir, "drawable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "drawable", "icon.png"), []byte("img"), 0644); err != nil {
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

	// Assets.
	assetsDir := filepath.Join(tmp, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "data.bin"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(tmp, "base.zip")
	err := assembleModuleZip(moduleZipInputs{
		ManifestPath:      manifest,
		ResourceTablePath: resourcesPB,
		ResourceDir:       resDir,
		DexDir:            dexDir,
		AssetsDir:         assetsDir,
	}, out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := zipEntries(t, out)
	want := []string{
		"assets/data.bin",
		"dex/classes.dex",
		"manifest/AndroidManifest.xml",
		"res/drawable/icon.png",
		"resources.pb",
	}
	if !stringSlicesEqual(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}
}

func TestExtractProtoAPKPreservesDirectoryStructure(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	protoAPK := filepath.Join(tmp, "proto.apk")
	createTestZip(t, protoAPK, map[string]string{
		"res/values/strings.xml":    "s1",
		"res/values-es/strings.xml": "s2",
		"res/layout/main.xml":       "layout",
	})

	destDir := filepath.Join(tmp, "out")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractProtoAPK(protoAPK, destDir); err != nil {
		t.Fatal(err)
	}

	// Verify nested directories are created.
	for _, rel := range []string{
		"res/values/strings.xml",
		"res/values-es/strings.xml",
		"res/layout/main.xml",
	} {
		if !pathIsFile(filepath.Join(destDir, rel)) {
			t.Errorf("expected extracted file: %s", rel)
		}
	}
}

// createTestZip creates a zip file at path with the given entries.
func createTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(entries[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
