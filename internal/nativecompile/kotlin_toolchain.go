package nativecompile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/gradlecache"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

type kotlinToolchain struct {
	Version             string
	CompilerClasspath   []string
	RuntimeJars         []string
	TestRuntimeJars     []string
	ComposePlugin       string
	SerializationPlugin string
	MetroPlugin         string
}

func (s *compileState) kotlinToolchainForProject(prj *project.Project) (*kotlinToolchain, error) {
	s.toolchainOnce.Do(func() {
		s.toolchain, s.toolchainErr = loadKotlinToolchain(prj, s)
	})
	return s.toolchain, s.toolchainErr
}

func loadKotlinToolchain(prj *project.Project, state *compileState) (*kotlinToolchain, error) {
	version := projectKotlinVersion(prj)
	if version == "" {
		return fallbackKotlinToolchain(), nil
	}
	resolver, err := state.resolverForProject(prj)
	if err != nil {
		return nil, err
	}
	resolvedSet, err := dependencywiring.ResolveToolDependencySet(resolver, kotlinToolDependencySet(version))
	if err != nil {
		return nil, err
	}
	toolchain := &kotlinToolchain{
		Version:             version,
		RuntimeJars:         mergePaths(resolvedSet.Jars("runtime"), resolvedSet.Jars("runtime-jdk7"), resolvedSet.Jars("runtime-jdk8")),
		TestRuntimeJars:     mergePaths(resolvedSet.Jars("test-runtime"), resolvedSet.Jars("test-junit"), resolvedSet.Jars("test-junit5")),
		ComposePlugin:       resolvedSet.FirstJar("compose-plugin"),
		SerializationPlugin: resolvedSet.FirstJar("serialization-plugin"),
	}
	toolchain.CompilerClasspath = filterKotlinCompilerClasspathVersion(version, mergePaths(
		singlePath(resolvedSet.FirstJar("compiler")),
		resolvedSet.Resolved.RuntimeJars,
		toolchain.RuntimeJars,
		resolvedSet.Jars("reflect"),
		resolvedSet.Jars("script-runtime"),
	))
	if metroPlugin := dependencywiring.ResolveMetroCompilerPlugin(prj, resolver); metroPlugin != "" {
		toolchain.MetroPlugin = metroPlugin
	}
	if len(toolchain.CompilerClasspath) == 0 {
		return fallbackKotlinToolchain(), nil
	}
	if len(toolchain.RuntimeJars) == 0 {
		toolchain.RuntimeJars = fallbackKotlinToolchain().RuntimeJars
	}
	if len(toolchain.TestRuntimeJars) == 0 {
		toolchain.TestRuntimeJars = fallbackKotlinToolchain().TestRuntimeJars
	}
	return toolchain, nil
}

func singlePath(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return []string{path}
}

func kotlinToolDependencySet(version string) dependencywiring.ToolDependencySet {
	deps := []dependencywiring.ToolDependency{
		{Group: "org.jetbrains.kotlin", Module: "kotlin-compiler-embeddable", Version: version, Role: "compiler"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-stdlib", Version: version, Role: "runtime"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-stdlib-jdk7", Version: version, Role: "runtime-jdk7"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-stdlib-jdk8", Version: version, Role: "runtime-jdk8"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-test", Version: version, Role: "test-runtime"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-test-junit", Version: version, Role: "test-junit"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-test-junit5", Version: version, Role: "test-junit5"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-reflect", Version: version, Role: "reflect"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-script-runtime", Version: version, Role: "script-runtime"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-compose-compiler-plugin-embeddable", Version: version, Role: "compose-plugin", Optional: true},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-serialization-compiler-plugin-embeddable", Version: version, Role: "serialization-plugin", Optional: true},
	}
	return dependencywiring.ToolDependencySet{Name: "kotlin-toolchain", Dependencies: deps}
}

func filterKotlinCompilerClasspathVersion(version string, paths []string) []string {
	if strings.TrimSpace(version) == "" {
		return paths
	}
	dotMarker := filepath.Join("org.jetbrains.kotlin") + string(filepath.Separator)
	pathMarker := filepath.Join("org", "jetbrains", "kotlin") + string(filepath.Separator)
	versionSegment := string(filepath.Separator) + version + string(filepath.Separator)
	var out []string
	for _, path := range paths {
		if !strings.Contains(path, dotMarker) && !strings.Contains(path, pathMarker) {
			out = append(out, path)
			continue
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "kotlin-stdlib") || strings.HasPrefix(base, "kotlin-test") {
			out = append(out, path)
			continue
		}
		if strings.Contains(path, versionSegment) {
			out = append(out, path)
		}
	}
	return out
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
	return latestCachedKotlinVersion("kotlin-compiler-embeddable")
}

func findGradleArtifactJars(group, module, version string) []string {
	return gradlecache.FindArtifactJars(group, module, version)
}

func latestCachedKotlinVersion(module string) string {
	return latestCachedVersionFor("org.jetbrains.kotlin", module)
}

func latestCachedVersionFor(group, module string) string {
	return gradlecache.LatestVersion(group, module)
}

func fallbackKotlinToolchain() *kotlinToolchain {
	return &kotlinToolchain{
		Version:         latestCachedKotlinVersion("kotlin-compiler-embeddable"),
		RuntimeJars:     kotlinRuntimeJars(),
		TestRuntimeJars: kotlinTestRuntimeJars(),
	}
}

func activeCompilerPluginsForModule(mod *project.Module, variantName string) []modulebuild.CompilerPlugin {
	if mod == nil {
		return nil
	}
	registered := mod.ActiveCompilerPlugins(variantName)
	if len(registered) == 0 && mod.CompilerPlugins == nil {
		if mod.UsesCompose {
			registered = append(registered, modulebuild.CompilerPlugin{ID: modulebuild.ComposeCompilerPluginID})
		}
		if mod.UsesKotlinSerialization {
			registered = append(registered, modulebuild.CompilerPlugin{ID: modulebuild.KotlinSerializationCompilerPluginID})
		}
		if mod.UsesMetro {
			registered = append(registered, modulebuild.CompilerPlugin{ID: modulebuild.MetroCompilerPluginID})
		}
	}
	return registered
}

func compilerPluginsForModule(mod *project.Module, variantName string, toolchain *kotlinToolchain) ([]string, []string) {
	registered := activeCompilerPluginsForModule(mod, variantName)
	var plugins []string
	var options []string
	for _, plugin := range registered {
		if len(plugin.Classpath) > 0 {
			plugins = append(plugins, plugin.Classpath...)
		} else {
			switch plugin.ID {
			case modulebuild.ComposeCompilerPluginID:
				if toolchain != nil && strings.TrimSpace(toolchain.ComposePlugin) != "" {
					plugins = append(plugins, toolchain.ComposePlugin)
				}
			case modulebuild.KotlinSerializationCompilerPluginID:
				if toolchain != nil && strings.TrimSpace(toolchain.SerializationPlugin) != "" {
					plugins = append(plugins, toolchain.SerializationPlugin)
				}
			case modulebuild.MetroCompilerPluginID:
				if toolchain != nil && strings.TrimSpace(toolchain.MetroPlugin) != "" {
					plugins = append(plugins, toolchain.MetroPlugin)
				}
			}
		}
		if len(plugin.Options) == 0 {
			continue
		}
		keys := make([]string, 0, len(plugin.Options))
		for key := range plugin.Options {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			options = append(options, fmt.Sprintf("plugin:%s:%s=%s", plugin.ID, key, plugin.Options[key]))
		}
	}
	return plugins, options
}

func compilerRuntimeClasspath(toolchain *kotlinToolchain, classpath []string) []string {
	if toolchain == nil {
		return classpath
	}
	return mergePaths(toolchain.RuntimeJars, classpath)
}

func (t *kotlinToolchain) validate() error {
	if t == nil {
		return nil
	}
	if len(t.CompilerClasspath) == 0 {
		return fmt.Errorf("kotlin compiler classpath is empty")
	}
	if t.Version == "" {
		return fmt.Errorf("kotlin compiler version is unknown")
	}
	var missing []string
	if len(findPathsContaining(t.CompilerClasspath, "kotlin-compiler-embeddable")) == 0 {
		missing = append(missing, "kotlin-compiler-embeddable:"+t.Version)
	}
	if len(findPathsContaining(t.CompilerClasspath, "kotlin-stdlib")) == 0 {
		missing = append(missing, "kotlin-stdlib:"+t.Version)
	}
	if len(findPathsContaining(t.CompilerClasspath, "kotlin-script-runtime")) == 0 {
		missing = append(missing, "kotlin-script-runtime:"+t.Version)
	}
	if len(findPathsContaining(t.CompilerClasspath, "annotations")) == 0 {
		missing = append(missing, "org.jetbrains:annotations")
	}
	if len(missing) > 0 {
		return fmt.Errorf("incomplete Kotlin compiler toolchain for %s; missing %s from compiler classpath", t.Version, strings.Join(missing, ", "))
	}
	return nil
}

func findPathsContaining(paths []string, needle string) []string {
	var out []string
	for _, path := range paths {
		if strings.Contains(filepath.Base(path), needle) {
			out = append(out, path)
		}
	}
	return out
}
