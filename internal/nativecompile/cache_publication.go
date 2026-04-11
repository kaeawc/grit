package nativecompile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

func restoreSharedResourceCompile(compiledDir, compiledStamp, cacheDir string) bool {
	cachedDir := filepath.Join(cacheDir, "compiled")
	cachedStamp := filepath.Join(cacheDir, "compiled.stamp")
	if !pathIsDir(cachedDir) || !pathIsFile(cachedStamp) {
		return false
	}
	if err := os.RemoveAll(compiledDir); err != nil {
		return false
	}
	if err := copyDir(cachedDir, compiledDir); err != nil {
		return false
	}
	if err := copyFile(cachedStamp, compiledStamp); err != nil {
		return false
	}
	return true
}

func saveSharedResourceCompile(compiledDir, compiledStamp, cacheDir string) error {
	if pathIsDir(filepath.Join(cacheDir, "compiled")) && pathIsFile(filepath.Join(cacheDir, "compiled.stamp")) {
		return nil
	}
	tmpDir := cacheDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}
	if err := copyDir(compiledDir, filepath.Join(tmpDir, "compiled")); err != nil {
		return err
	}
	if err := copyFile(compiledStamp, filepath.Join(tmpDir, "compiled.stamp")); err != nil {
		return err
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		return err
	}
	return os.Rename(tmpDir, cacheDir)
}

func restoreSharedSymbolJar(symbolJar, cacheDir string) bool {
	cachedJar := filepath.Join(cacheDir, "r-symbols.jar")
	if !pathIsFile(cachedJar) {
		return false
	}
	return copyFile(cachedJar, symbolJar) == nil
}

func saveSharedSymbolJar(symbolJar, cacheDir string) error {
	if pathIsFile(filepath.Join(cacheDir, "r-symbols.jar")) {
		return nil
	}
	tmpDir := cacheDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}
	if err := copyFile(symbolJar, filepath.Join(tmpDir, "r-symbols.jar")); err != nil {
		return err
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		return err
	}
	return os.Rename(tmpDir, cacheDir)
}

func sharedSignedAPKPath(unsignedAPK, signingName string, signing project.SigningConfig) string {
	sum := sha256.New()
	sum.Write([]byte("signed-apk-v1"))
	sum.Write([]byte{0})
	sum.Write([]byte(cacheIdentityForInput(unsignedAPK)))
	sum.Write([]byte{0})
	sum.Write([]byte(cacheIdentityForInput(signing.StoreFile)))
	sum.Write([]byte{0})
	sum.Write([]byte(signingName))
	sum.Write([]byte{0})
	sum.Write([]byte(signing.KeyAlias))
	sum.Write([]byte{0})
	sum.Write([]byte(toolIdentity("apksigner")))
	sum.Write([]byte{0})
	return filepath.Join(sharedNativeCacheRoot(), "apk", "signed", hex.EncodeToString(sum.Sum(nil)), "app.apk")
}

func restoreSharedSignedAPK(finalAPK, sharedAPK string) bool {
	if !pathIsFile(sharedAPK) {
		return false
	}
	return copyFile(sharedAPK, finalAPK) == nil
}

func saveSharedSignedAPK(finalAPK, sharedAPK string) error {
	if pathIsFile(sharedAPK) {
		return nil
	}
	tmpDir := filepath.Dir(sharedAPK) + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}
	if err := copyFile(finalAPK, filepath.Join(tmpDir, "app.apk")); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Dir(sharedAPK)); err != nil {
		return err
	}
	return os.Rename(tmpDir, filepath.Dir(sharedAPK))
}

func restoreSharedCompileCache(classesDir, moduleJar, cacheDir string) bool {
	cachedClasses := filepath.Join(cacheDir, "classes")
	cachedJar := filepath.Join(cacheDir, "module-classes.jar")
	if !pathIsDir(cachedClasses) || !pathIsFile(cachedJar) {
		return false
	}
	if err := os.RemoveAll(classesDir); err != nil {
		return false
	}
	if err := copyDir(cachedClasses, classesDir); err != nil {
		return false
	}
	if err := copyFile(cachedJar, moduleJar); err != nil {
		return false
	}
	return true
}

func saveSharedCompileCache(classesDir, moduleJar, cacheDir string) error {
	if pathIsDir(filepath.Join(cacheDir, "classes")) && pathIsFile(filepath.Join(cacheDir, "module-classes.jar")) {
		return nil
	}
	tmpDir := cacheDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}
	if err := copyDir(classesDir, filepath.Join(tmpDir, "classes")); err != nil {
		return err
	}
	if err := copyFile(moduleJar, filepath.Join(tmpDir, "module-classes.jar")); err != nil {
		return err
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		return err
	}
	return os.Rename(tmpDir, cacheDir)
}

func mergeDexDirs(dst string, dirs ...string) error {
	next := 1
	for _, dir := range dirs {
		dexFiles, err := filepath.Glob(filepath.Join(dir, "classes*.dex"))
		if err != nil {
			return err
		}
		sort.Strings(dexFiles)
		for _, dexFile := range dexFiles {
			name := "classes.dex"
			if next > 1 {
				name = fmt.Sprintf("classes%d.dex", next)
			}
			if err := copyFile(dexFile, filepath.Join(dst, name)); err != nil {
				return err
			}
			next++
		}
	}
	if next == 1 {
		return fmt.Errorf("no dex files produced")
	}
	return nil
}

func writeGeneratedR8Rules(mod *project.Module, variant project.BuildType) (string, error) {
	outDir := filepath.Join(mod.Dir, "build", "grit", variant.Name)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, "grit-r8-rules.pro")
	body := strings.Join([]string{
		"-dontwarn kotlin.**",
		"-dontwarn kotlinx.coroutines.swing.**",
		"-dontwarn javax.swing.**",
		"-dontwarn java.awt.**",
		"-dontwarn java.lang.StringLatin1**",
		"-dontwarn java.lang.StringUTF16**",
		"-dontwarn android.view.RenderNode",
		"-dontwarn android.view.DisplayListCanvas",
		"-dontwarn android.view.HardwareCanvas",
		"-dontwarn androidx.**.R",
		"-dontwarn androidx.**.R$*",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func classesJarStampValue(classesDir string) string {
	compileStampPath := filepath.Join(filepath.Dir(classesDir), "compile.stamp")
	if data, err := os.ReadFile(compileStampPath); err == nil {
		value := strings.TrimSpace(string(data))
		if value != "" {
			return "compile:" + value
		}
	}
	return "dir:" + dirFingerprint(classesDir)
}
