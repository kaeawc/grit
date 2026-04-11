package nativecompile

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

func cloneAncestry(ancestry map[string]bool) map[string]bool {
	if len(ancestry) == 0 {
		return map[string]bool{}
	}
	cloned := make(map[string]bool, len(ancestry))
	for key, value := range ancestry {
		cloned[key] = value
	}
	return cloned
}

func sharedExternalDexDir(jars []string) string {
	sum := sha256.New()
	sum.Write([]byte("external-dex-v2"))
	sum.Write([]byte{0})
	sum.Write([]byte(androidJarPath()))
	sum.Write([]byte{0})
	for _, jar := range jars {
		sum.Write([]byte(filepath.Clean(jar)))
		sum.Write([]byte{0})
	}
	return filepath.Join(sharedNativeCacheRoot(), "dex", "external", hex.EncodeToString(sum.Sum(nil)))
}

func sharedProjectDexDir(jars []string) string {
	sum := sha256.New()
	sum.Write([]byte("project-dex-v1"))
	sum.Write([]byte{0})
	sum.Write([]byte(cacheIdentityForInput(androidJarPath())))
	sum.Write([]byte{0})
	for _, jar := range jars {
		sum.Write([]byte(filepath.Clean(jar)))
		sum.Write([]byte{0})
		sum.Write([]byte(cacheIdentityForInput(jar)))
		sum.Write([]byte{0})
	}
	return filepath.Join(sharedNativeCacheRoot(), "dex", "project", hex.EncodeToString(sum.Sum(nil)))
}

func sharedAppDexDir(classesJar string, runtimeCP []string) string {
	sum := sha256.New()
	sum.Write([]byte("app-dex-v2"))
	sum.Write([]byte{0})
	sum.Write([]byte(cacheIdentityForInput(androidJarPath())))
	sum.Write([]byte{0})
	sum.Write([]byte(cacheIdentityForInput(classesJar)))
	sum.Write([]byte{0})
	for _, jar := range runtimeCP {
		sum.Write([]byte(filepath.Clean(jar)))
		sum.Write([]byte{0})
		sum.Write([]byte(cacheIdentityForInput(jar)))
		sum.Write([]byte{0})
	}
	return filepath.Join(sharedNativeCacheRoot(), "dex", "app", hex.EncodeToString(sum.Sum(nil)))
}

func isSharedDexCacheReady(dexDir string) bool {
	root := sharedNativeCacheRoot() + string(os.PathSeparator)
	clean := filepath.Clean(dexDir)
	if !strings.HasPrefix(clean, root) {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(clean, "classes*.dex"))
	return err == nil && len(matches) > 0
}

func sharedNativeCacheRoot() string {
	if root, err := os.UserCacheDir(); err == nil && root != "" {
		return filepath.Join(root, "grit")
	}
	return filepath.Join(os.Getenv("HOME"), ".cache", "grit")
}

func moduleCompileCacheDir(modulePath, variantName string, inputs []string) string {
	sum := sha256.New()
	sum.Write([]byte(modulePath))
	sum.Write([]byte{0})
	sum.Write([]byte(variantName))
	sum.Write([]byte{0})
	for _, input := range inputs {
		sum.Write([]byte(input))
		sum.Write([]byte{0})
		sum.Write([]byte(cacheIdentityForInput(input)))
		sum.Write([]byte{0})
	}
	return filepath.Join(sharedNativeCacheRoot(), "compile", hex.EncodeToString(sum.Sum(nil)))
}

func moduleResourceCompileCacheDir(modulePath, variantName string, resDirs []string) string {
	sum := sha256.New()
	sum.Write([]byte("resource-compile-v3"))
	sum.Write([]byte{0})
	sum.Write([]byte(modulePath))
	sum.Write([]byte{0})
	sum.Write([]byte(variantName))
	sum.Write([]byte{0})
	for _, dir := range resDirs {
		sum.Write([]byte(filepath.Clean(dir)))
		sum.Write([]byte{0})
		sum.Write([]byte(dirFingerprint(dir)))
		sum.Write([]byte{0})
	}
	sum.Write([]byte(toolIdentity("aapt2")))
	return filepath.Join(sharedNativeCacheRoot(), "resources", "module-compile", hex.EncodeToString(sum.Sum(nil)))
}

func moduleResourceSymbolsCacheDir(mod *project.Module, variantName, manifestPath string, depResources []androidResourceArtifact, artifact androidResourceArtifact) string {
	sum := sha256.New()
	sum.Write([]byte("resource-symbols-v1"))
	sum.Write([]byte{0})
	sum.Write([]byte(mod.Path))
	sum.Write([]byte{0})
	sum.Write([]byte(variantName))
	sum.Write([]byte{0})
	sum.Write([]byte(cacheIdentityForInput(manifestPath)))
	sum.Write([]byte{0})
	sum.Write([]byte(mod.Namespace))
	sum.Write([]byte{0})
	sum.Write([]byte(mod.MinSDK))
	sum.Write([]byte{0})
	sum.Write([]byte(mod.TargetSDK))
	sum.Write([]byte{0})
	sum.Write([]byte(cacheIdentityForInput(androidJarPath())))
	sum.Write([]byte{0})
	sum.Write([]byte(toolIdentity("aapt2")))
	sum.Write([]byte{0})
	sum.Write([]byte(toolIdentity("javac")))
	sum.Write([]byte{0})
	for _, resource := range append([]androidResourceArtifact{}, depResources...) {
		sum.Write([]byte(resourceArtifactIdentity(resource)))
		sum.Write([]byte{0})
	}
	sum.Write([]byte(resourceArtifactIdentity(artifact)))
	sum.Write([]byte{0})
	return filepath.Join(sharedNativeCacheRoot(), "resources", "module-symbols", hex.EncodeToString(sum.Sum(nil)))
}

func cacheIdentityForInput(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	if info.IsDir() {
		return "dir:" + strconv.FormatInt(latestInputModTime([]string{path}).UnixNano(), 10)
	}
	clean := filepath.Clean(path)
	if strings.Contains(clean, string(os.PathSeparator)+"build"+string(os.PathSeparator)+"grit"+string(os.PathSeparator)) {
		if strings.HasSuffix(clean, ".jar") || strings.HasSuffix(clean, ".zip") {
			if digest, err := zipFingerprint(clean); err == nil {
				return "zip:" + digest
			}
		}
		if digest, err := fileSHA256(clean); err == nil {
			return "sha256:" + digest
		}
	}
	if strings.HasPrefix(clean, sharedNativeCacheRoot()+string(os.PathSeparator)) {
		return "shared:" + clean
	}
	return strings.Join([]string{
		"file",
		strconv.FormatInt(info.Size(), 10),
		strconv.FormatInt(info.ModTime().UnixNano(), 10),
	}, ":")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func zipFingerprint(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	sum := sha256.New()
	for _, f := range zr.File {
		sum.Write([]byte(f.Name))
		sum.Write([]byte{0})
		sum.Write([]byte(strconv.FormatUint(uint64(f.CRC32), 10)))
		sum.Write([]byte{0})
		sum.Write([]byte(strconv.FormatUint(f.UncompressedSize64, 10)))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func dirFingerprint(root string) string {
	sum := sha256.New()
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		sum.Write([]byte(rel))
		sum.Write([]byte{0})
		if digest, digestErr := fileSHA256(path); digestErr == nil {
			sum.Write([]byte(digest))
		} else {
			sum.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
			sum.Write([]byte{0})
			sum.Write([]byte(strconv.FormatInt(info.Size(), 10)))
		}
		sum.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(sum.Sum(nil))
}

func toolIdentity(bin string) string {
	path, err := exec.LookPath(bin)
	if err != nil {
		return "missing:" + bin
	}
	identity := cacheIdentityForInput(path)
	return bin + ":" + identity
}

func resourceArtifactIdentity(artifact androidResourceArtifact) string {
	sum := sha256.New()
	sum.Write([]byte(artifact.ModulePath))
	sum.Write([]byte{0})
	for _, file := range artifact.CompiledFiles {
		sum.Write([]byte(filepath.Base(file)))
		sum.Write([]byte{0})
		sum.Write([]byte(cacheIdentityForInput(file)))
		sum.Write([]byte{0})
	}
	if len(artifact.CompiledFiles) == 0 {
		sum.Write([]byte(cacheIdentityForInput(artifact.CompiledStamp)))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}
