package nativecompile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

type bundletoolToolchain struct {
	Version string
	JarPath string
}

func (s *compileState) bundletoolToolchainForProject(prj *project.Project) (*bundletoolToolchain, error) {
	s.bundletoolOnce.Do(func() {
		s.bundletool, s.bundletoolErr = loadBundletoolToolchain(prj, s)
	})
	return s.bundletool, s.bundletoolErr
}

func loadBundletoolToolchain(prj *project.Project, state *compileState) (*bundletoolToolchain, error) {
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

	// 2. Android SDK build-tools bundled copy.
	if tc := bundletoolFromSDK(); tc != nil {
		return tc, nil
	}

	// 3. Project dependency resolver. This path honors declared
	// repositories and the resolver's persisted metadata/lockfile instead
	// of picking the newest artifact already present in a machine cache.
	if state == nil {
		return nil, fmt.Errorf("bundletool JAR not found; set BUNDLETOOL_JAR, install Android SDK bundletool, or configure BUNDLETOOL_VERSION for project resolution")
	}
	resolver, err := state.resolverForProject(prj)
	if err != nil {
		return nil, err
	}
	if tc, err := resolveBundletoolFromDependencies(resolver); err == nil {
		return tc, nil
	} else {
		return nil, err
	}
}

func resolveBundletoolFromDependencies(resolver dependencyResolverForToolArtifact) (*bundletoolToolchain, error) {
	version := strings.TrimSpace(os.Getenv("BUNDLETOOL_VERSION"))
	if version == "" {
		return nil, fmt.Errorf("bundletool version not configured; set BUNDLETOOL_VERSION or BUNDLETOOL_JAR")
	}
	jar, err := resolveMavenToolJar(resolver, mavenToolArtifact{
		Group:        "com.android.tools.build",
		Artifact:     "bundletool",
		Version:      version,
		JarBaseNames: []string{"bundletool-all-", "bundletool-"},
	})
	if err != nil {
		return nil, err
	}
	return &bundletoolToolchain{Version: version, JarPath: jar}, nil
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

type dependencyResolverForToolArtifact interface {
	Resolve(*modulebuild.Dependencies) (*m2local.Resolved, error)
}

type mavenToolArtifact struct {
	Group        string
	Artifact     string
	Version      string
	JarBaseNames []string
}

func resolveMavenToolJar(resolver dependencyResolverForToolArtifact, artifact mavenToolArtifact) (string, error) {
	if resolver == nil {
		return "", fmt.Errorf("tool artifact resolver is nil")
	}
	coord := artifact.Group + ":" + artifact.Artifact + ":" + artifact.Version
	resolved, err := resolver.Resolve(&modulebuild.Dependencies{
		Main: []modulebuild.Ref{{Kind: "raw", Value: coord}},
	})
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", coord, err)
	}
	if resolved == nil {
		return "", fmt.Errorf("resolve %s: empty result", coord)
	}
	for _, path := range mergePaths(resolved.CompileJars, resolved.RuntimeJars) {
		if toolArtifactJarMatches(path, artifact) && pathIsFile(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("resolve %s: artifact jar not found in resolved classpath", coord)
}

func toolArtifactJarMatches(path string, artifact mavenToolArtifact) bool {
	if !strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".jar") {
		return false
	}
	for _, prefix := range artifact.JarBaseNames {
		if strings.HasPrefix(filepath.Base(path), prefix) {
			return true
		}
	}
	return strings.HasPrefix(filepath.Base(path), artifact.Artifact+"-")
}
