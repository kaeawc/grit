package m2local

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/griterr"
)

func findFile(root, suffix string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		hashDir := filepath.Join(root, entry.Name())
		files, _ := os.ReadDir(hashDir)
		for _, f := range files {
			if strings.HasSuffix(f.Name(), suffix) {
				return filepath.Join(hashDir, f.Name()), nil
			}
		}
	}
	return "", fmt.Errorf("no file with suffix %s in %s", suffix, root)
}

func findNamedFile(root, name string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("artifact %s not found in %s", name, root)
}

func findArtifactCandidate(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		hashDir := filepath.Join(root, entry.Name())
		files, _ := os.ReadDir(hashDir)
		for _, f := range files {
			if isBinaryArtifactName(f.Name()) {
				return filepath.Join(hashDir, f.Name()), nil
			}
		}
	}
	return "", fmt.Errorf("no artifact found in %s", root)
}

func (r *Resolver) fetchArtifactCandidate(coord Coordinate, base string) (string, error) {
	if fetched, err := r.fetchArtifact(coord, ".jar"); err == nil {
		return fetched, nil
	}
	if fetched, err := r.fetchArtifact(coord, ".aar"); err == nil {
		return fetched, nil
	}
	return "", fmt.Errorf("no artifact found in %s", base)
}

func (r *Resolver) normalizeArtifact(path string) (string, *AndroidLibrary, error) {
	switch {
	case strings.HasSuffix(path, ".jar"):
		return path, nil, nil
	case strings.HasSuffix(path, ".aar"):
		coord, err := r.coordinateForArtifact(path)
		if err != nil {
			return "", nil, err
		}
		androidLibrary, err := r.extractAAR(coord, path)
		if err != nil {
			return "", nil, err
		}
		return androidLibrary.ClassesJar, &androidLibrary, nil
	default:
		return "", nil, griterr.Newf(griterr.ErrUnsupported, "artifact %s", path)
	}
}

func (r *Resolver) ensureResolvedMaterialized(resolved *Resolved) error {
	for _, jar := range append(append([]string{}, resolved.CompileJars...), resolved.RuntimeJars...) {
		if jar == "" || !fileExists(jar) {
			return fmt.Errorf("cached jar missing: %s", jar)
		}
	}
	for _, jar := range resolved.TestJars {
		if jar == "" || !fileExists(jar) {
			return fmt.Errorf("cached test jar missing: %s", jar)
		}
	}
	for i := range resolved.AndroidLibraries {
		lib, err := r.ensureAndroidLibraryMaterialized(resolved.AndroidLibraries[i])
		if err != nil {
			return err
		}
		resolved.AndroidLibraries[i] = lib
	}
	return nil
}

func (r *Resolver) ensureAndroidLibraryMaterialized(lib AndroidLibrary) (AndroidLibrary, error) {
	if lib.ID == "" {
		return lib, nil
	}
	coord, ok := coordinateFromID(lib.ID)
	if !ok {
		if lib.ClassesJar != "" && !fileExists(lib.ClassesJar) {
			return AndroidLibrary{}, fmt.Errorf("cached android library classes missing: %s", lib.ClassesJar)
		}
		if lib.ManifestPath != "" && !fileExists(lib.ManifestPath) {
			return AndroidLibrary{}, fmt.Errorf("cached android library manifest missing: %s", lib.ManifestPath)
		}
		if lib.ResDir != "" && !dirExists(lib.ResDir) {
			return AndroidLibrary{}, fmt.Errorf("cached android library resources missing: %s", lib.ResDir)
		}
		return lib, nil
	}
	if (lib.ClassesJar == "" || fileExists(lib.ClassesJar)) &&
		(lib.ManifestPath == "" || fileExists(lib.ManifestPath)) &&
		(lib.ResDir == "" || dirExists(lib.ResDir)) {
		return lib, nil
	}
	modulePath := r.moduleBasePath(coord)
	artifactPath, err := findNamedFile(modulePath, coord.Module+"-"+coord.Version+".aar")
	if err != nil {
		artifactPath, err = findFile(modulePath, ".aar")
		if err != nil {
			fetched, fetchErr := r.fetchArtifact(coord, ".aar")
			if fetchErr != nil {
				return AndroidLibrary{}, fmt.Errorf("cached android library %s missing extracted files and source AAR", lib.ID)
			}
			artifactPath = fetched
		}
	}
	return r.extractAAR(coord, artifactPath)
}

func (r *Resolver) coordinateForArtifact(path string) (Coordinate, error) {
	marker := filepath.Join("files-2.1") + string(os.PathSeparator)
	idx := strings.Index(path, marker)
	if idx < 0 {
		return Coordinate{}, fmt.Errorf("artifact path %s is not in the supported module cache layout", path)
	}
	rest := strings.Split(path[idx+len(marker):], string(os.PathSeparator))
	if len(rest) < 4 {
		return Coordinate{}, fmt.Errorf("artifact path %s does not contain group/module/version", path)
	}
	return Coordinate{Group: rest[0], Module: rest[1], Version: rest[2]}, nil
}

func (r *Resolver) extractAAR(coord Coordinate, path string) (AndroidLibrary, error) {
	outDir := filepath.Join(sharedAARCacheRoot(), coord.Group, coord.Module, coord.Version)
	outJar := filepath.Join(outDir, "classes.jar")
	manifestPath := filepath.Join(outDir, "AndroidManifest.xml")
	resDir := filepath.Join(outDir, "res")
	readyPath := filepath.Join(outDir, ".ready")
	if pathExists(readyPath) {
		return AndroidLibrary{
			ID:           coordinateID(coord),
			ClassesJar:   existingFile(outJar),
			ManifestPath: existingFile(manifestPath),
			ResDir:       existingDir(resDir),
		}, nil
	}
	tmpDir := outDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return AndroidLibrary{}, err
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return AndroidLibrary{}, err
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return AndroidLibrary{}, err
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return AndroidLibrary{}, err
		}
		target := ""
		switch {
		case f.Name == "classes.jar":
			target = filepath.Join(tmpDir, "classes.jar")
		case f.Name == "AndroidManifest.xml":
			target = filepath.Join(tmpDir, "AndroidManifest.xml")
		case strings.HasPrefix(f.Name, "res/"):
			target = filepath.Join(tmpDir, f.Name)
		default:
			_ = rc.Close()
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			_ = rc.Close()
			return AndroidLibrary{}, err
		}
		out, err := os.Create(target)
		if err != nil {
			_ = rc.Close()
			return AndroidLibrary{}, err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = rc.Close()
			_ = out.Close()
			return AndroidLibrary{}, err
		}
		_ = out.Close()
		_ = rc.Close()
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".ready"), []byte("ok\n"), 0o644); err != nil {
		return AndroidLibrary{}, err
	}
	if err := os.RemoveAll(outDir); err != nil {
		return AndroidLibrary{}, err
	}
	if err := os.Rename(tmpDir, outDir); err != nil {
		if !pathExists(readyPath) {
			return AndroidLibrary{}, err
		}
		_ = os.RemoveAll(tmpDir)
	}
	return AndroidLibrary{
		ID:           coordinateID(coord),
		ClassesJar:   existingFile(outJar),
		ManifestPath: existingFile(manifestPath),
		ResDir:       existingDir(resDir),
	}, nil
}
