package nativecompile

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

type resolvedMainDeps struct {
	deps                 *modulebuild.Dependencies
	resolved             *m2local.Resolved
	localCompileRefs     []string
	localRuntimeRefs     []string
	projectCompileCP     []string
	projectRuntimeInputs []string
	projectResources     []androidResourceArtifact
}

type preparedMainCompile struct {
	resourceDeps      []androidResourceArtifact
	androidResources  []androidResourceArtifact
	compileCP         []string
	mainSources       []string
	mainOut           string
	sharedCompileDir  string
	moduleJarPath     string
	compileStampPath  string
	pluginPaths       []string
	effectiveCompile  []string
	compileInputs     []string
	androidModuleType bool
}

func (c *Compiler) tryCompileMainCacheHit(prj *project.Project, mod *project.Module, variantName, key string, state *compileState) (compiledModule, bool) {
	mainOut := filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName, "classes")
	snapshot, ok := loadModuleSnapshot(prj, mod, variantName)
	if !ok {
		return compiledModule{}, false
	}
	out := compiledModule{
		classesDir:       mainOut,
		runtimeInputs:    snapshot.RuntimeInputs,
		androidResources: snapshot.AndroidResources,
	}
	state.mu.Lock()
	state.outputs[key] = out
	state.mu.Unlock()
	return out, true
}

func (c *Compiler) resolveMainDependencies(ctx context.Context, prj *project.Project, mod *project.Module, variantName, key string, state *compileState, childAncestry map[string]bool, stdout, stderr *os.File) (resolvedMainDeps, error) {
	var out resolvedMainDeps
	err := c.track("parseDependencies", func() error {
		var innerErr error
		out.deps, innerErr = state.dependenciesForModule(mod.BuildFile)
		if innerErr == nil {
			out.deps = dependenciesForVariant(out.deps, mod, variantName)
		}
		return innerErr
	})
	if err != nil {
		return out, err
	}
	var resolver dependencywiring.DependencyResolver
	err = c.track("loadCatalog", func() error {
		var innerErr error
		resolver, innerErr = state.resolverForProject(prj)
		return innerErr
	})
	if err != nil {
		return out, err
	}
	compileDeps := *out.deps
	compileDeps.Main = append(append([]modulebuild.Ref{}, out.deps.Main...), out.deps.CompileOnly...)
	compileDeps.Debug = append([]modulebuild.Ref{}, out.deps.Debug...)
	compileDeps.Test = nil
	compileDeps.TestCompileOnly = nil
	compileDeps.TestRuntimeOnly = nil
	err = c.track("resolveDependencies", func() error {
		var innerErr error
		out.resolved, innerErr = resolver.Resolve(&compileDeps)
		return innerErr
	})
	if err != nil {
		return out, err
	}
	out.resolved = filterResolvedForProject(prj, out.resolved)
	err = c.track("resolveLocalClasspathRefs", func() error {
		out.localCompileRefs = resolveLocalDependencyRefs(prj, mod, append(append([]modulebuild.Ref{}, out.deps.Main...), out.deps.CompileOnly...))
		out.localRuntimeRefs = resolveLocalDependencyRefs(prj, mod, append(append([]modulebuild.Ref{}, out.deps.Main...), out.deps.RuntimeOnly...))
		return nil
	})
	if err != nil {
		return out, err
	}
	err = c.track("resolveProjectDeps", func() error {
		var innerErr error
		compileRefs := append(append([]modulebuild.Ref{}, out.deps.Main...), out.deps.CompileOnly...)
		runtimeRefs := append(append([]modulebuild.Ref{}, out.deps.Main...), out.deps.RuntimeOnly...)
		out.projectCompileCP, out.projectRuntimeInputs, out.projectResources, innerErr = c.resolveProjectDeps(ctx, prj, key, compileRefs, runtimeRefs, variantName, state, childAncestry, stdout, stderr)
		return innerErr
	})
	return out, err
}

func (c *Compiler) prepareMainCompile(ctx context.Context, prj *project.Project, mod *project.Module, variantName string, state *compileState, deps resolvedMainDeps, stdout, stderr *os.File) (preparedMainCompile, error) {
	var out preparedMainCompile
	out.mainOut = filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName, "classes")
	out.androidModuleType = strings.HasPrefix(mod.Type, "android-")

	var externalResources []androidResourceArtifact
	err := c.track("compileExternalAndroidLibraries", func() error {
		var innerErr error
		externalResources, innerErr = c.compileExternalAndroidLibraries(ctx, deps.resolved.AndroidLibraries, state, stdout, stderr)
		return innerErr
	})
	if err != nil {
		return out, err
	}
	err = c.track("prepareCompileInputs", func() error {
		out.resourceDeps = uniqueResourceArtifacts(append(append([]androidResourceArtifact{}, deps.projectResources...), externalResources...))
		out.compileCP = mergePaths(deps.projectCompileCP, collapseVersions(deps.resolved.CompileJars), deps.localCompileRefs)
		out.androidResources = out.resourceDeps
		return nil
	})
	if err != nil {
		return out, err
	}
	if out.androidModuleType {
		var artifact androidResourceArtifact
		err = c.track("compileAndroidResources", func() error {
			var innerErr error
			artifact, innerErr = c.compileAndroidResources(ctx, prj, mod, variantName, out.resourceDeps, stdout, stderr)
			return innerErr
		})
		if err != nil {
			return out, err
		}
		if artifact.SymbolJar != "" {
			out.compileCP = mergePaths(out.compileCP, []string{artifact.SymbolJar})
		}
		if artifact.ManifestPath != "" {
			out.androidResources = appendResourceArtifacts(out.androidResources, artifact)
		}
	}
	err = c.track("collectMainSources", func() error {
		var innerErr error
		out.mainSources, innerErr = collectMainSourcesForVariant(mod, variantName)
		return innerErr
	})
	if err != nil {
		return out, err
	}
	if err := os.MkdirAll(out.mainOut, 0o755); err != nil {
		return out, err
	}
	toolchain, err := state.kotlinToolchainForProject(prj)
	if err != nil {
		return out, err
	}
	out.pluginPaths = compilerPluginsForModule(mod, toolchain)
	out.effectiveCompile = out.compileCP
	kotlinInputs := append([]string{}, out.mainSources...)
	kotlinInputs = append(kotlinInputs, mod.BuildFile)
	kotlinInputs = append(kotlinInputs, prj.VersionCatalogs...)
	kotlinInputs = append(kotlinInputs, mainSourceRoots(mod, variantName)...)
	kotlinInputs = append(kotlinInputs,
		filepath.Join(mod.Dir, "src", "main", "res"),
		filepath.Join(mod.Dir, "src", "main", "AndroidManifest.xml"),
	)
	out.compileInputs = append(append([]string{}, kotlinInputs...), out.effectiveCompile...)
	out.compileInputs = append(out.compileInputs, toolchain.RuntimeJars...)
	out.compileInputs = append(out.compileInputs, toolchain.CompilerClasspath...)
	out.compileInputs = append(out.compileInputs, out.pluginPaths...)
	out.sharedCompileDir = moduleCompileCacheDir(mod.Path, variantName, mod.ResolveVariant(variantName).ConfigHash(), out.compileInputs)
	out.moduleJarPath = filepath.Join(filepath.Dir(out.mainOut), "module-classes.jar")
	out.compileStampPath = filepath.Join(filepath.Dir(out.mainOut), "compile.stamp")
	return out, nil
}

func (c *Compiler) finishMainWithoutSources(prj *project.Project, mod *project.Module, variantName, key string, state *compileState, deps resolvedMainDeps, prepared preparedMainCompile) (string, []string, []androidResourceArtifact, error) {
	var runtimeInputs []string
	err := c.track("prepareRuntimeInputs", func() error {
		toolchain, innerErr := state.kotlinToolchainForProject(prj)
		if innerErr != nil {
			return innerErr
		}
		runtimeInputs = mergePaths(deps.projectRuntimeInputs, collapseVersions(deps.resolved.RuntimeJars), deps.localRuntimeRefs, toolchain.RuntimeJars)
		return nil
	})
	if err != nil {
		return "", nil, nil, err
	}
	out := compiledModule{classesDir: prepared.mainOut, runtimeInputs: runtimeInputs, androidResources: prepared.androidResources}
	state.mu.Lock()
	state.outputs[key] = out
	state.mu.Unlock()
	_ = saveModuleSnapshot(prj, mod, variantName, deps.deps, out)
	return prepared.mainOut, runtimeInputs, prepared.androidResources, nil
}

func (c *Compiler) compileMainSources(ctx context.Context, prj *project.Project, mod *project.Module, variantName, key string, state *compileState, deps resolvedMainDeps, prepared preparedMainCompile, stdout, stderr *os.File) (string, []string, []androidResourceArtifact, error) {
	restoredFromShared := false
	if err := c.track("compileKotlin", func() error {
		if stampMatches(prepared.compileStampPath, prepared.sharedCompileDir) && hasOutputFiles(prepared.mainOut) {
			recordCacheProbe(c.tracker, "compileKotlin", true, "local-up-to-date", "compile stamp matched local outputs")
			return nil
		}
		if outputsNewerThanInputs(prepared.mainOut, prepared.compileInputs) {
			_ = writeStamp(prepared.compileStampPath, prepared.sharedCompileDir)
			recordCacheProbe(c.tracker, "compileKotlin", true, "local-up-to-date", "compiled outputs newer than compile inputs")
			return nil
		}
		if restoreSharedCompileCache(prepared.mainOut, prepared.moduleJarPath, prepared.sharedCompileDir) {
			restoredFromShared = true
			_ = writeStamp(prepared.compileStampPath, prepared.sharedCompileDir)
			recordCacheProbe(c.tracker, "compileKotlin", true, "shared-cache-hit", "restored compiled classes from shared cache")
			return nil
		}
		recordCacheProbe(c.tracker, "compileKotlin", false, "cache-miss", "compiled classes required fresh Kotlin compilation")
		toolchain, toolchainErr := state.kotlinToolchainForProject(prj)
		if toolchainErr != nil {
			return toolchainErr
		}
		if err := runKotlinc(ctx, toolchain, prepared.mainSources, prepared.mainOut, prepared.effectiveCompile, prepared.pluginPaths, prepared.androidModuleType, mod.UsesCompose || prepared.androidModuleType, nil, stdout, stderr); err != nil {
			return err
		}
		_ = writeStamp(prepared.compileStampPath, prepared.sharedCompileDir)
		return nil
	}); err != nil {
		return "", nil, nil, err
	}

	var moduleJar string
	err := c.track("jarClasses", func() error {
		if restoredFromShared {
			recordCacheProbe(c.tracker, "jarClasses", true, "shared-cache-hit", "restored module jar from shared compile cache")
			moduleJar = prepared.moduleJarPath
			return nil
		}
		var innerErr error
		moduleJar, innerErr = classesJarForDir(ctx, prepared.mainOut, stdout, stderr)
		if innerErr == nil {
			_ = saveSharedCompileCache(prepared.mainOut, moduleJar, prepared.sharedCompileDir)
		}
		return innerErr
	})
	if err != nil {
		return "", nil, nil, err
	}

	var runtimeInputs []string
	err = c.track("prepareRuntimeInputs", func() error {
		toolchain, innerErr := state.kotlinToolchainForProject(prj)
		if innerErr != nil {
			return innerErr
		}
		runtimeInputs = mergePaths(deps.projectRuntimeInputs, []string{moduleJar}, collapseVersions(deps.resolved.RuntimeJars), deps.localRuntimeRefs, toolchain.RuntimeJars)
		return nil
	})
	if err != nil {
		return "", nil, nil, err
	}
	out := compiledModule{classesDir: prepared.mainOut, runtimeInputs: runtimeInputs, androidResources: prepared.androidResources}
	state.mu.Lock()
	state.outputs[key] = out
	state.mu.Unlock()
	_ = saveModuleSnapshot(prj, mod, variantName, deps.deps, out)
	return prepared.mainOut, runtimeInputs, prepared.androidResources, nil
}
