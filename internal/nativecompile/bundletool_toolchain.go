package nativecompile

import (
	"fmt"
	"io"
	"net/http"
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

	// 4. Auto-download from Maven Central.
	if tc, err := downloadBundletool(); err == nil {
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

// defaultBundletoolVersion is the version downloaded when no local copy is
// found. Override with the BUNDLETOOL_VERSION environment variable.
const defaultBundletoolVersion = "1.17.2"

// bundletoolDownloadURL returns the Maven Central URL for the bundletool
// all-in-one JAR at the given version.
func bundletoolDownloadURL(version string) string {
	return fmt.Sprintf(
		"https://repo1.maven.org/maven2/com/android/tools/build/bundletool/%s/bundletool-all-%s.jar",
		version, version,
	)
}

// bundletoolCacheJarPath returns the local cache path where the
// auto-downloaded bundletool JAR is stored.
func bundletoolCacheJarPath(version string) string {
	return filepath.Join(sharedNativeCacheRoot(), "bundletool", fmt.Sprintf("bundletool-all-%s.jar", version))
}

// downloadBundletool downloads the bundletool JAR from Maven Central
// into the grit cache and returns a toolchain pointing at it.
func downloadBundletool() (*bundletoolToolchain, error) {
	version := strings.TrimSpace(os.Getenv("BUNDLETOOL_VERSION"))
	if version == "" {
		version = defaultBundletoolVersion
	}

	jarPath := bundletoolCacheJarPath(version)

	// Already downloaded.
	if pathIsFile(jarPath) {
		return &bundletoolToolchain{Version: version, JarPath: jarPath}, nil
	}

	url := bundletoolDownloadURL(version)
	if err := downloadFile(url, jarPath); err != nil {
		return nil, fmt.Errorf("download bundletool %s: %w", version, err)
	}
	return &bundletoolToolchain{Version: version, JarPath: jarPath}, nil
}

// downloadFile fetches url and writes the response body to dst. The
// destination directory is created if it does not exist. A partial
// download is cleaned up on error.
func downloadFile(url, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	resp, err := http.Get(url) //nolint:gosec // URL is constructed internally from a known base.
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	// Write to a temp file first so a partial download never looks valid.
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
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
