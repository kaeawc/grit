package nativecompile

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func unitTestRunCachePath(prjRoot string, modulePath string, variantName string, testClasses []string, classpath []string) string {
	sum := sha256.New()
	sum.Write([]byte("unit-test-run-v1"))
	sum.Write([]byte{0})
	sum.Write([]byte(prjRoot))
	sum.Write([]byte{0})
	sum.Write([]byte(modulePath))
	sum.Write([]byte{0})
	sum.Write([]byte(variantName))
	sum.Write([]byte{0})
	for _, testClass := range testClasses {
		sum.Write([]byte(testClass))
		sum.Write([]byte{0})
	}
	for _, entry := range classpath {
		sum.Write([]byte(entry))
		sum.Write([]byte{0})
		sum.Write([]byte(unitTestRunInputIdentity(entry)))
		sum.Write([]byte{0})
	}
	return filepath.Join(sharedNativeCacheRoot(), "unit-test-run", hex.EncodeToString(sum.Sum(nil))+".stamp")
}

func canReuseUnitTestRun(cachePath string) bool {
	return pathIsFile(cachePath)
}

func markUnitTestRunSuccess(cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	return writeStamp(cachePath, "ok")
}

func unitTestRunInputIdentity(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	if info.IsDir() {
		return "dir:" + dirFingerprint(path)
	}
	return cacheIdentityForInput(path)
}
