package nativecompile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type bundletoolToolchain struct {
	Version string
	JarPath string
}

func (s *compileState) bundletoolToolchainForProject() (*bundletoolToolchain, error) {
	s.bundletoolOnce.Do(func() {
		s.bundletool, s.bundletoolErr = loadBundletoolToolchain()
	})
	return s.bundletool, s.bundletoolErr
}

func loadBundletoolToolchain() (*bundletoolToolchain, error) {
	// 1. Explicit environment variable.
	if p := strings.TrimSpace(os.Getenv("BUNDLETOOL_JAR")); p != "" {
		if pathIsFile(p) {
			return &bundletoolToolchain{
				Version: bundletoolVersionFromPath(p),
				JarPath: p,
			}, nil
		}
		return nil, fmt.Errorf("BUNDLETOOL_JAR set to %s but file does not exist", p)
	}

	// 2. Gradle cache (com.android.tools.build:bundletool).
	if tc := bundletoolFromGradleCache(); tc != nil {
		return tc, nil
	}

	// 3. Android SDK build-tools bundled copy.
	if tc := bundletoolFromSDK(); tc != nil {
		return tc, nil
	}

	return nil, fmt.Errorf("bundletool JAR not found; set BUNDLETOOL_JAR or install via Android SDK / Gradle")
}

func bundletoolFromGradleCache() *bundletoolToolchain {
	version := latestCachedVersionFor("com.android.tools.build", "bundletool")
	if version == "" {
		return nil
	}
	jars := findGradleArtifactJars("com.android.tools.build", "bundletool", version)
	for _, jar := range jars {
		if pathIsFile(jar) {
			return &bundletoolToolchain{Version: version, JarPath: jar}
		}
	}
	return nil
}

func bundletoolFromSDK() *bundletoolToolchain {
	root := androidSDKRoot()
	buildToolsDir := filepath.Join(root, "build-tools")
	entries, err := os.ReadDir(buildToolsDir)
	if err != nil {
		return nil
	}
	// Sort versions and try newest first.
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Strings(versions)
	for i := len(versions) - 1; i >= 0; i-- {
		candidate := filepath.Join(buildToolsDir, versions[i], "lib", "bundletool.jar")
		if pathIsFile(candidate) {
			return &bundletoolToolchain{
				Version: versions[i],
				JarPath: candidate,
			}
		}
	}
	return nil
}

func bundletoolVersionFromPath(p string) string {
	base := filepath.Base(p)
	base = strings.TrimSuffix(base, ".jar")
	if strings.HasPrefix(base, "bundletool-all-") {
		return strings.TrimPrefix(base, "bundletool-all-")
	}
	if strings.HasPrefix(base, "bundletool-") {
		return strings.TrimPrefix(base, "bundletool-")
	}
	return "unknown"
}

func (t *bundletoolToolchain) validate() error {
	if t == nil {
		return fmt.Errorf("bundletool toolchain is nil")
	}
	if t.JarPath == "" {
		return fmt.Errorf("bundletool JAR path is empty")
	}
	if !pathIsFile(t.JarPath) {
		return fmt.Errorf("bundletool JAR not found at %s", t.JarPath)
	}
	return nil
}
