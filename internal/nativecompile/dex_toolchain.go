package nativecompile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/project"
)

type androidDexToolchain struct {
	Version string
	Source  string
	JarPath string
}

func (s *compileState) dexToolchainForProject(prj *project.Project) (*androidDexToolchain, error) {
	s.dexToolchainOnce.Do(func() {
		s.dexToolchain, s.dexToolchainErr = loadDexToolchain(prj, s)
	})
	return s.dexToolchain, s.dexToolchainErr
}

func loadDexToolchain(prj *project.Project, state *compileState) (*androidDexToolchain, error) {
	if p := strings.TrimSpace(os.Getenv("D8_R8_JAR")); p != "" {
		if pathIsFile(p) {
			return &androidDexToolchain{Version: dexToolchainVersionFromPath(p), Source: "env", JarPath: p}, nil
		}
		return nil, fmt.Errorf("D8_R8_JAR set to %s but file does not exist", p)
	}

	if tc := dexToolchainFromSDK(); tc != nil {
		return tc, nil
	}

	if state == nil {
		return nil, fmt.Errorf("D8/R8 toolchain not found; set D8_R8_JAR, install Android SDK build-tools with lib/d8.jar, or configure R8_VERSION/catalog version for com.android.tools:r8")
	}
	resolver, err := state.resolverForProject(prj)
	if err != nil {
		return nil, err
	}
	return resolveDexToolchainFromDependencies(prj, resolver)
}

func resolveDexToolchainFromDependencies(prj *project.Project, resolver dependencywiring.ArtifactResolver) (*androidDexToolchain, error) {
	version := dexToolchainDependencyVersion(prj)
	if version == "" {
		return nil, fmt.Errorf("r8 version not configured; set R8_VERSION, D8_R8_JAR, install Android SDK build-tools with lib/d8.jar, or declare catalog version r8/android-r8/android-tools-r8")
	}
	jar, err := resolveMavenToolJar(resolver, mavenToolArtifact{
		Group:        "com.android.tools",
		Artifact:     "r8",
		Version:      version,
		JarBaseNames: []string{"r8-", "d8"},
	})
	if err != nil {
		return nil, err
	}
	return &androidDexToolchain{Version: version, Source: "dependency", JarPath: jar}, nil
}

func dexToolchainDependencyVersion(prj *project.Project) string {
	if v := strings.TrimSpace(os.Getenv("R8_VERSION")); v != "" {
		return v
	}
	if prj != nil && prj.VersionCatalogData != nil {
		for _, key := range []string{"r8", "android-r8", "android-tools-r8"} {
			if v := strings.TrimSpace(prj.VersionCatalogData[key]); v != "" {
				return v
			}
		}
	}
	cat, err := dependencywiring.LoadCatalog(prj)
	if err == nil && cat != nil {
		for _, key := range []string{"r8", "android-r8", "android-tools-r8"} {
			if v := strings.TrimSpace(cat.Versions[key]); v != "" {
				return v
			}
		}
	}
	return ""
}

func dexToolchainFromSDK() *androidDexToolchain {
	root := androidSDKRoot()
	buildToolsDir := filepath.Join(root, "build-tools")
	entries, err := os.ReadDir(buildToolsDir)
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Strings(versions)
	for i := len(versions) - 1; i >= 0; i-- {
		candidate := filepath.Join(buildToolsDir, versions[i], "lib", "d8.jar")
		if pathIsFile(candidate) {
			return &androidDexToolchain{Version: versions[i], Source: "sdk-build-tools", JarPath: candidate}
		}
	}
	return nil
}

func dexToolchainVersionFromPath(p string) string {
	base := strings.TrimSuffix(filepath.Base(p), ".jar")
	if strings.HasPrefix(base, "r8-") {
		return strings.TrimPrefix(base, "r8-")
	}
	return "unknown"
}

func (t *androidDexToolchain) validate() error {
	if t == nil {
		return fmt.Errorf("D8/R8 toolchain is nil")
	}
	if strings.TrimSpace(t.JarPath) == "" {
		return fmt.Errorf("D8/R8 JAR path is empty")
	}
	if !pathIsFile(t.JarPath) {
		return fmt.Errorf("D8/R8 JAR not found at %s", t.JarPath)
	}
	return nil
}

func dexToolchainInputs(tc *androidDexToolchain) []string {
	if tc == nil {
		return nil
	}
	return []string{tc.JarPath}
}

func dexToolchainStampPath(dexDir string) string {
	return filepath.Join(filepath.Dir(dexDir), filepath.Base(dexDir)+".toolchain.stamp")
}

func dexToolchainStampValue(tc *androidDexToolchain) string {
	sum := sha256.New()
	if tc == nil {
		sum.Write([]byte("no-dex-toolchain"))
		return hex.EncodeToString(sum.Sum(nil))
	}
	for _, part := range []string{tc.Source, tc.Version, filepath.Clean(tc.JarPath), cacheIdentityForInput(tc.JarPath)} {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func dexOutputsFresh(dexDir string, inputs []string, tc *androidDexToolchain) bool {
	if !stampMatches(dexToolchainStampPath(dexDir), dexToolchainStampValue(tc)) {
		return false
	}
	inputs = append(append([]string{}, inputs...), dexToolchainInputs(tc)...)
	return outputsNewerThanInputs(dexDir, inputs)
}

func writeDexToolchainStamp(dexDir string, tc *androidDexToolchain) error {
	return writeStamp(dexToolchainStampPath(dexDir), dexToolchainStampValue(tc))
}
