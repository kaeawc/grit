package nativecompile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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
	compilerResolved, err := resolver.Resolve(&modulebuild.Dependencies{
		Main: []modulebuild.Ref{{Kind: "raw", Value: "org.jetbrains.kotlin:kotlin-compiler-embeddable:" + version}},
	})
	if err != nil {
		return nil, err
	}
	toolchain := &kotlinToolchain{
		Version: version,
		RuntimeJars: mergePaths(
			findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib", version),
			findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib-jdk7", version),
			findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-stdlib-jdk8", version),
		),
		TestRuntimeJars: mergePaths(
			findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-test", version),
			findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-test-junit", version),
			findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-test-junit5", version),
		),
	}
	toolchain.CompilerClasspath = mergePaths(
		compilerResolved.RuntimeJars,
		toolchain.RuntimeJars,
		findGradleArtifactJars("org.jetbrains", "annotations", latestCachedVersionFor("org.jetbrains", "annotations")),
		findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-reflect", version),
		findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-script-runtime", version),
	)
	if paths := findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-compose-compiler-plugin-embeddable", version); len(paths) > 0 {
		toolchain.ComposePlugin = paths[0]
	}
	if paths := findGradleArtifactJars("org.jetbrains.kotlin", "kotlin-serialization-compiler-plugin-embeddable", version); len(paths) > 0 {
		toolchain.SerializationPlugin = paths[0]
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

func projectKotlinVersion(prj *project.Project) string {
	if prj == nil {
		return ""
	}
	if v := strings.TrimSpace(prj.VersionCatalogData["kotlin"]); v != "" {
		return v
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
				plugins = append(plugins, filepath.Join(gradlecache.Root(), "dev.zacsweers.metro", "compiler", "0.12.0", "898e83c86c03300a76d55f83815ce13a1d1fc005", "compiler-0.12.0.jar"))
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
