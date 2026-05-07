package nativecompile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/gradlecache"
	"github.com/kaeawc/grit/internal/modulebuild"
)

var (
	junitPlatformRunnerOnce sync.Once
	junitPlatformRunnerPath string
	junitPlatformRunnerErr  error
)

func junitPlatformRunnerJar() string {
	junitPlatformRunnerOnce.Do(func() {
		junitPlatformRunnerPath, junitPlatformRunnerErr = buildJUnitPlatformRunnerJar()
	})
	if junitPlatformRunnerErr != nil {
		return ""
	}
	return junitPlatformRunnerPath
}

func buildJUnitPlatformRunnerJar() (string, error) {
	versions := alignedJUnitRuntimeVersions()
	versionDir := versions.platform
	if versionDir == "" {
		versionDir = "latest"
	}
	root := filepath.Join(sharedNativeCacheRoot(), "jvm-support", "junit-platform-runner", versionDir)
	jarPath := filepath.Join(root, "junit-platform-runner.jar")
	if pathIsFile(jarPath) {
		return jarPath, nil
	}
	deps := junitPlatformSupportJars()
	var classpath []string
	for _, dep := range deps {
		if dep == "" {
			continue
		}
		classpath = append(classpath, dep)
	}
	if len(classpath) == 0 {
		return "", fmt.Errorf("junit platform runtime jars not found")
	}
	srcDir := filepath.Join(root, "src")
	classesDir := filepath.Join(root, "classes")
	if err := os.MkdirAll(filepath.Join(srcDir, "grit", "junit"), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return "", err
	}
	sourcePath := filepath.Join(srcDir, "grit", "junit", "PlatformRunner.java")
	source := `package grit.junit;

import java.io.PrintWriter;
import java.util.ArrayList;
import java.util.List;
import org.junit.platform.engine.DiscoverySelector;
import org.junit.platform.engine.discovery.DiscoverySelectors;
import org.junit.platform.launcher.Launcher;
import org.junit.platform.launcher.core.LauncherDiscoveryRequestBuilder;
import org.junit.platform.launcher.core.LauncherFactory;
import org.junit.platform.launcher.listeners.SummaryGeneratingListener;
import org.junit.platform.launcher.listeners.TestExecutionSummary;

public final class PlatformRunner {
  public static void main(String[] args) {
    List<DiscoverySelector> selectors = new ArrayList<>();
    for (String arg : args) {
      if (arg != null && !arg.isBlank()) {
        selectors.add(DiscoverySelectors.selectClass(arg));
      }
    }
    LauncherDiscoveryRequestBuilder builder = LauncherDiscoveryRequestBuilder.request();
    builder.selectors(selectors.toArray(new DiscoverySelector[0]));
    Launcher launcher = LauncherFactory.create();
    SummaryGeneratingListener listener = new SummaryGeneratingListener();
    launcher.registerTestExecutionListeners(listener);
    launcher.execute(builder.build());
    TestExecutionSummary summary = listener.getSummary();
    if (summary.getTotalFailureCount() > 0) {
      summary.printFailuresTo(new PrintWriter(System.err, true));
      System.exit(1);
    }
  }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o644); err != nil {
		return "", err
	}
	args := []string{"-cp", strings.Join(classpath, string(os.PathListSeparator)), "-d", classesDir, sourcePath}
	cmd := exec.Command("javac", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compile junit platform runner: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.RemoveAll(jarPath); err != nil {
		return "", err
	}
	cmd = exec.Command("jar", "cf", jarPath, ".")
	cmd.Dir = classesDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("package junit platform runner: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return jarPath, nil
}

func junitPlatformSupportJars() []string {
	versions := alignedJUnitRuntimeVersions()
	return junitPlatformSupportJarsForVersions(versions)
}

func junitPlatformSupportJarsForVersions(versions junitRuntimeVersions) []string {
	if versions.platform != "" {
		return []string{
			cachedGradleArtifactJar("org.junit.platform", "junit-platform-launcher", versions.platform),
			cachedGradleArtifactJar("org.junit.platform", "junit-platform-engine", versions.platform),
			cachedGradleArtifactJar("org.junit.platform", "junit-platform-commons", versions.platform),
			junitSupportDependencyJar(versions, "org.apiguardian", "apiguardian-api"),
			junitSupportDependencyJar(versions, "org.opentest4j", "opentest4j"),
			junitSupportDependencyJar(versions, "org.jspecify", "jspecify"),
		}
	}
	return []string{
		latestCachedArtifactJar("org.junit.platform", "junit-platform-launcher"),
		latestCachedArtifactJar("org.junit.platform", "junit-platform-engine"),
		latestCachedArtifactJar("org.junit.platform", "junit-platform-commons"),
		latestCachedArtifactJar("org.apiguardian", "apiguardian-api"),
		latestCachedArtifactJar("org.opentest4j", "opentest4j"),
		latestCachedArtifactJar("org.jspecify", "jspecify"),
	}
}

func junitJupiterEngineJar() string {
	if versions := alignedJUnitRuntimeVersions(); versions.jupiter != "" {
		return cachedGradleArtifactJar("org.junit.jupiter", "junit-jupiter-engine", versions.jupiter)
	}
	return findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.junit.jupiter", "junit-jupiter-engine", "*", "*", "junit-jupiter-engine-*.jar"))
}

func junitJupiterApiJar() string {
	if versions := alignedJUnitRuntimeVersions(); versions.jupiter != "" {
		return cachedGradleArtifactJar("org.junit.jupiter", "junit-jupiter-api", versions.jupiter)
	}
	return findLatestCachedJar(filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1", "org.junit.jupiter", "junit-jupiter-api", "*", "*", "junit-jupiter-api-*.jar"))
}

type junitRuntimeVersions struct {
	platform string
	jupiter  string
	deps     map[string]string
}

func alignedJUnitRuntimeVersions() junitRuntimeVersions {
	return alignedJUnitRuntimeVersionsWith(gradleJUnitArtifactCache{}, nil, nil)
}

type junitArtifactCache interface {
	Versions(group, module string) []string
	Jar(group, module, version string) string
	Dependencies(group, module, version string) []gradlecache.Dependency
}

type gradleJUnitArtifactCache struct{}

func (gradleJUnitArtifactCache) Versions(group, module string) []string {
	return gradlecache.ArtifactVersions(group, module, compareVersion)
}

func (gradleJUnitArtifactCache) Jar(group, module, version string) string {
	return cachedGradleArtifactJar(group, module, version)
}

func (gradleJUnitArtifactCache) Dependencies(group, module, version string) []gradlecache.Dependency {
	return gradlecache.ArtifactDependencies(group, module, version)
}

func alignedJUnitRuntimeVersionsFor(deps *modulebuild.Dependencies, cat *catalog.Catalog) junitRuntimeVersions {
	return alignedJUnitRuntimeVersionsWith(gradleJUnitArtifactCache{}, deps, cat)
}

func alignedJUnitRuntimeVersionsWith(cache junitArtifactCache, deps *modulebuild.Dependencies, cat *catalog.Catalog) junitRuntimeVersions {
	if versions := junitRuntimeVersionsFromDependencies(cache, deps, cat); versions.platform != "" || versions.jupiter != "" {
		return versions
	}
	versions := cache.Versions("org.junit.platform", "junit-platform-launcher")
	for i := len(versions) - 1; i >= 0; i-- {
		platformVersion := versions[i]
		jupiterVersion := junitJupiterVersionForPlatform(platformVersion)
		if jupiterVersion == "" {
			continue
		}
		if cache.Jar("org.junit.platform", "junit-platform-engine", platformVersion) == "" {
			continue
		}
		if cache.Jar("org.junit.platform", "junit-platform-commons", platformVersion) == "" {
			continue
		}
		if cache.Jar("org.junit.jupiter", "junit-jupiter-api", jupiterVersion) == "" {
			continue
		}
		if cache.Jar("org.junit.jupiter", "junit-jupiter-engine", jupiterVersion) == "" {
			continue
		}
		return withJUnitRuntimeDependencyVersions(cache, junitRuntimeVersions{platform: platformVersion, jupiter: jupiterVersion})
	}
	return junitRuntimeVersions{}
}

func junitRuntimeVersionsFromDependencies(cache junitArtifactCache, deps *modulebuild.Dependencies, cat *catalog.Catalog) junitRuntimeVersions {
	requested := junitRequestedVersions(deps, cat)
	versions := junitRuntimeVersions{
		platform: requested["org.junit.platform:junit-platform-launcher"],
		jupiter:  firstNonEmptyString(requested["org.junit.jupiter:junit-jupiter-engine"], requested["org.junit.jupiter:junit-jupiter-api"], requested["org.junit.jupiter:junit-jupiter"]),
		deps:     map[string]string{},
	}
	for key, version := range requested {
		switch key {
		case "org.junit.platform:junit-platform-engine", "org.junit.platform:junit-platform-commons", "org.junit.platform:junit-platform-suite-api":
			if versions.platform == "" {
				versions.platform = version
			}
		case "org.junit.jupiter:junit-jupiter-params":
			if versions.jupiter == "" {
				versions.jupiter = version
			}
		case "org.apiguardian:apiguardian-api", "org.opentest4j:opentest4j", "org.jspecify:jspecify":
			versions.deps[key] = version
		}
	}
	if versions.platform == "" && versions.jupiter != "" {
		versions.platform = junitPlatformVersionForJupiter(versions.jupiter)
	}
	if versions.jupiter == "" && versions.platform != "" {
		versions.jupiter = junitJupiterVersionForPlatform(versions.platform)
	}
	if versions.platform == "" && versions.jupiter == "" {
		return junitRuntimeVersions{}
	}
	return withJUnitRuntimeDependencyVersions(cache, versions)
}

func junitRequestedVersions(deps *modulebuild.Dependencies, cat *catalog.Catalog) map[string]string {
	out := map[string]string{}
	if deps == nil {
		return out
	}
	refs := append([]modulebuild.Ref{}, deps.Test...)
	refs = append(refs, deps.TestCompileOnly...)
	refs = append(refs, deps.TestRuntimeOnly...)
	for _, ref := range refs {
		group, module, version := junitCoordinateFromRef(ref, cat)
		if group == "" || module == "" || version == "" {
			continue
		}
		key := group + ":" + module
		if isJUnitRuntimeCoordinate(key) {
			out[key] = version
		}
	}
	return out
}

func junitCoordinateFromRef(ref modulebuild.Ref, cat *catalog.Catalog) (string, string, string) {
	switch ref.Kind {
	case "library":
		if cat == nil {
			return "", "", ""
		}
		lib, err := cat.ResolveLibrary(ref.Value)
		if err != nil {
			return "", "", ""
		}
		return lib.Group, lib.Name, lib.Version
	case "raw":
		parts := strings.Split(ref.Value, ":")
		if len(parts) == 3 {
			return parts[0], parts[1], parts[2]
		}
	}
	return "", "", ""
}

func withJUnitRuntimeDependencyVersions(cache junitArtifactCache, versions junitRuntimeVersions) junitRuntimeVersions {
	if versions.deps == nil {
		versions.deps = map[string]string{}
	}
	for _, dep := range cache.Dependencies("org.junit.platform", "junit-platform-launcher", versions.platform) {
		key := dep.Group + ":" + dep.Module
		if isJUnitRuntimeCoordinate(key) && versions.deps[key] == "" {
			versions.deps[key] = dep.Version
		}
	}
	for _, dep := range cache.Dependencies("org.junit.jupiter", "junit-jupiter-engine", versions.jupiter) {
		key := dep.Group + ":" + dep.Module
		if isJUnitRuntimeCoordinate(key) && versions.deps[key] == "" {
			versions.deps[key] = dep.Version
		}
	}
	return versions
}

func isJUnitRuntimeCoordinate(key string) bool {
	switch key {
	case "org.junit.platform:junit-platform-launcher",
		"org.junit.platform:junit-platform-engine",
		"org.junit.platform:junit-platform-commons",
		"org.junit.platform:junit-platform-suite-api",
		"org.junit.jupiter:junit-jupiter",
		"org.junit.jupiter:junit-jupiter-api",
		"org.junit.jupiter:junit-jupiter-engine",
		"org.junit.jupiter:junit-jupiter-params",
		"org.apiguardian:apiguardian-api",
		"org.opentest4j:opentest4j",
		"org.jspecify:jspecify":
		return true
	default:
		return false
	}
}

func junitJupiterVersionForPlatform(platformVersion string) string {
	parts := strings.Split(platformVersion, ".")
	if len(parts) < 3 {
		return ""
	}
	if parts[0] == "1" {
		return "5." + strings.Join(parts[1:], ".")
	}
	return platformVersion
}

func junitPlatformVersionForJupiter(jupiterVersion string) string {
	parts := strings.Split(jupiterVersion, ".")
	if len(parts) < 3 {
		return ""
	}
	if parts[0] == "5" {
		return "1." + strings.Join(parts[1:], ".")
	}
	return jupiterVersion
}

func junitSupportDependencyJar(versions junitRuntimeVersions, group, module string) string {
	if versions.deps != nil {
		if version := versions.deps[group+":"+module]; version != "" {
			return cachedGradleArtifactJar(group, module, version)
		}
	}
	return latestCachedArtifactJar(group, module)
}

func latestCachedArtifactJar(group, module string) string {
	version := gradlecache.LatestVersion(group, module)
	if version == "" {
		return ""
	}
	return cachedGradleArtifactJar(group, module, version)
}

func cachedGradleArtifactJar(group, module, version string) string {
	jars := findGradleArtifactJars(group, module, version)
	if len(jars) == 0 {
		return ""
	}
	return jars[0]
}
