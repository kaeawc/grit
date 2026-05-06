package env

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/gradlecache"
	"github.com/kaeawc/grit/internal/project"
)

// KotlincLibDir resolves kotlinc on PATH (following symlinks) and returns its
// adjacent lib/ directory, or "" if it can't be resolved.
func KotlincLibDir() string {
	path, err := exec.LookPath("kotlinc")
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	bin := filepath.Dir(path)
	candidate := filepath.Join(filepath.Dir(bin), "lib")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return ""
}

// LocateComposeCompilerPlugin returns the path to the compose compiler plugin
// jar, preferring the kotlinc/lib distribution and falling back to the local
// Gradle cache. Returns "" if neither is available.
func LocateComposeCompilerPlugin() string {
	if dir := KotlincLibDir(); dir != "" {
		path := filepath.Join(dir, "compose-compiler-plugin.jar")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	const group = "org.jetbrains.kotlin"
	const module = "kotlin-compose-compiler-plugin-embeddable"
	if version := gradlecache.LatestVersion(group, module); version != "" {
		if jars := gradlecache.FindArtifactJars(group, module, version); len(jars) > 0 {
			return jars[0]
		}
	}
	return ""
}

// LocateSerializationCompilerPlugin returns the path to the kotlinx
// serialization compiler plugin jar, preferring kotlinc/lib then the local
// Gradle cache. Returns "" if not available.
func LocateSerializationCompilerPlugin() string {
	if dir := KotlincLibDir(); dir != "" {
		path := filepath.Join(dir, "kotlin-serialization-compiler-plugin.jar")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	const group = "org.jetbrains.kotlin"
	const module = "kotlin-serialization-compiler-plugin-embeddable"
	if version := gradlecache.LatestVersion(group, module); version != "" {
		if jars := gradlecache.FindArtifactJars(group, module, version); len(jars) > 0 {
			return jars[0]
		}
	}
	return ""
}

// LocateKotlinCompiler returns either the project-requested Kotlin compiler
// embeddable jar from the Gradle cache or the latest cached compiler jar.
func LocateKotlinCompiler(prj *project.Project) string {
	version := projectKotlinVersion(prj)
	if version == "" {
		version = gradlecache.LatestVersion("org.jetbrains.kotlin", "kotlin-compiler-embeddable")
	}
	if version == "" {
		return ""
	}
	jars := gradlecache.FindArtifactJars("org.jetbrains.kotlin", "kotlin-compiler-embeddable", version)
	if len(jars) == 0 {
		return ""
	}
	return jars[0]
}

func projectKotlinVersion(prj *project.Project) string {
	if prj == nil {
		return ""
	}
	for _, key := range []string{"kotlin", "build-kotlin", "kotlin-version"} {
		if v := strings.TrimSpace(prj.VersionCatalogData[key]); v != "" {
			return v
		}
	}
	return ""
}
