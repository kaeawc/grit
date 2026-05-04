package nativecompile

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// moduleZipInputs describes the inputs for assembling a bundletool module zip.
// Each field maps to a directory inside the zip that bundletool expects.
type moduleZipInputs struct {
	// ManifestPath is the path to the AndroidManifest.xml (proto format).
	// Required — every module must have a manifest.
	ManifestPath string

	// DexDir is the directory containing classes*.dex files.
	// These are added under dex/ in the zip.
	DexDir string

	// ResourceDir is the directory containing compiled resources (proto flat
	// files produced by aapt2). Added under res/ in the zip.
	ResourceDir string

	// AssetsDir is the directory containing raw asset files.
	// Added under assets/ in the zip.
	AssetsDir string

	// NativeLibDirs maps ABI name (e.g. "arm64-v8a") to a directory
	// containing .so files. Added under lib/<abi>/ in the zip.
	NativeLibDirs map[string]string

	// ResourceTablePath is the path to the proto-format resource table
	// (resources.pb) produced by aapt2 link --proto-format. Added at the
	// root of the zip. Empty to omit.
	ResourceTablePath string
}

// assembleModuleZip creates a bundletool-compatible module zip at outputPath
// from the given inputs. The zip layout follows the structure expected by
// bundletool build-bundle:
//
//	manifest/AndroidManifest.xml
//	dex/classes.dex
//	dex/classes2.dex
//	res/<compiled resources>
//	assets/<asset files>
//	lib/<abi>/<native lib>.so
func assembleModuleZip(inputs moduleZipInputs, outputPath string) error {
	if inputs.ManifestPath == "" {
		return fmt.Errorf("module zip: manifest path is required")
	}
	if !pathIsFile(inputs.ManifestPath) {
		return fmt.Errorf("module zip: manifest not found: %s", inputs.ManifestPath)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("module zip: create output dir: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("module zip: create file: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			f.Close()
			os.Remove(outputPath)
		}
	}()

	w := zip.NewWriter(f)

	// Manifest (required).
	if err := addFileToZip(w, inputs.ManifestPath, "manifest/AndroidManifest.xml"); err != nil {
		return fmt.Errorf("module zip: add manifest: %w", err)
	}

	// Proto resource table (resources.pb).
	if inputs.ResourceTablePath != "" && pathIsFile(inputs.ResourceTablePath) {
		if err := addFileToZip(w, inputs.ResourceTablePath, "resources.pb"); err != nil {
			return fmt.Errorf("module zip: add resources.pb: %w", err)
		}
	}

	// Dex files.
	if inputs.DexDir != "" && pathIsDir(inputs.DexDir) {
		dexFiles, globErr := filepath.Glob(filepath.Join(inputs.DexDir, "classes*.dex"))
		if globErr != nil {
			return fmt.Errorf("module zip: glob dex: %w", globErr)
		}
		sort.Strings(dexFiles)
		for _, dex := range dexFiles {
			entryName := "dex/" + filepath.Base(dex)
			if err := addFileToZip(w, dex, entryName); err != nil {
				return fmt.Errorf("module zip: add dex %s: %w", filepath.Base(dex), err)
			}
		}
	}

	// Compiled resources.
	if inputs.ResourceDir != "" && pathIsDir(inputs.ResourceDir) {
		if err := addDirToZip(w, inputs.ResourceDir, "res"); err != nil {
			return fmt.Errorf("module zip: add resources: %w", err)
		}
	}

	// Assets.
	if inputs.AssetsDir != "" && pathIsDir(inputs.AssetsDir) {
		if err := addDirToZip(w, inputs.AssetsDir, "assets"); err != nil {
			return fmt.Errorf("module zip: add assets: %w", err)
		}
	}

	// Native libraries.
	abis := make([]string, 0, len(inputs.NativeLibDirs))
	for abi := range inputs.NativeLibDirs {
		abis = append(abis, abi)
	}
	sort.Strings(abis)
	for _, abi := range abis {
		dir := inputs.NativeLibDirs[abi]
		if !pathIsDir(dir) {
			continue
		}
		prefix := "lib/" + abi
		if err := addDirToZip(w, dir, prefix); err != nil {
			return fmt.Errorf("module zip: add native libs for %s: %w", abi, err)
		}
	}

	// Explicitly close the zip writer to flush the central directory,
	// then close the file. Errors here mean a corrupt zip on disk.
	if err := w.Close(); err != nil {
		return fmt.Errorf("module zip: finalize zip: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("module zip: close file: %w", err)
	}
	succeeded = true
	return nil
}

// addFileToZip copies a single file into the zip at the given entry name.
func addFileToZip(w *zip.Writer, srcPath, entryName string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = entryName
	header.Method = zip.Deflate

	dst, err := w.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}

// addDirToZip recursively adds all files under dir into the zip, prefixed
// with the given zip path prefix. Only regular files are included.
func addDirToZip(w *zip.Writer, dir, prefix string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		entryName := prefix + "/" + strings.ReplaceAll(rel, string(os.PathSeparator), "/")
		return addFileToZip(w, path, entryName)
	})
}
