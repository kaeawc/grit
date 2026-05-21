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
// artifact cache. Returns "" if neither is available.
func LocateComposeCompilerPlugin() string {
	if dir := KotlincLibDir(); dir != "" {
		path := filepath.Join(dir, "compose-compiler-plugin.jar")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return latestCachedPluginJar("kotlin-compose-compiler-plugin-embeddable")
}

// LocateSerializationCompilerPlugin returns the path to the kotlinx
// serialization compiler plugin jar, preferring kotlinc/lib then the local
// artifact cache. Returns "" if not available.
func LocateSerializationCompilerPlugin() string {
	if dir := KotlincLibDir(); dir != "" {
		path := filepath.Join(dir, "kotlin-serialization-compiler-plugin.jar")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return latestCachedPluginJar("kotlin-serialization-compiler-plugin-embeddable")
}

// LocateKotlinCompiler returns either the project-requested Kotlin compiler
// embeddable jar from the local artifact cache or the latest cached compiler
// jar.
func LocateKotlinCompiler(prj *project.Project) string {
	probe := gradlecache.DefaultProbe()
	const group = "org.jetbrains.kotlin"
	const module = "kotlin-compiler-embeddable"
	version := projectKotlinVersion(prj)
	if version == "" {
		version = probe.LatestVersion(group, module)
	}
	if version == "" {
		return ""
	}
	jars := probe.FindJars(group, module, version)
	if len(jars) == 0 {
		return ""
	}
	return jars[0]
}

// latestCachedPluginJar returns the jar path for the latest cached version
// of an org.jetbrains.kotlin compiler plugin module, or "" when none is
// present.
func latestCachedPluginJar(module string) string {
	probe := gradlecache.DefaultProbe()
	version := probe.LatestVersion("org.jetbrains.kotlin", module)
	if version == "" {
		return ""
	}
	jars := probe.FindJars("org.jetbrains.kotlin", module, version)
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
