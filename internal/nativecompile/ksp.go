package nativecompile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

// KSP2 ships processors as a standalone JVM tool, not a kotlinc plugin.
// The driver lives in symbol-processing-aa-embeddable; it consumes source
// roots, runs symbol processors, and emits generated sources/resources.
// Generated Kotlin files feed back into the regular kotlinc invocation;
// generated Java files are picked up by the post-kotlinc javac pass.
const ksp2MainClass = "com.google.devtools.ksp.cmdline.KSPJvmMain"

// kspCompilation is the result of running KSP2 for a single module/variant.
// Callers append GeneratedKotlinFiles to mainSources before kotlinc and
// then run javac on JavaGenDir for any generated .java sources.
type kspCompilation struct {
	Version              string
	Ran                  bool
	GeneratedKotlinFiles []string
	GeneratedJavaFiles   []string
	JavaGenDir           string
	KotlinGenDir         string
	ResourceDir          string
	ClassDir             string
	ProcessorCP          []string
}

// projectKSPVersion resolves the KSP version pinned by the project's
// version catalog. Falls back to the most recent KSP2 runtime visible
// in the local Gradle cache.
func projectKSPVersion(prj *project.Project) string {
	if prj != nil {
		for _, key := range []string{"ksp", "build-kotlin-ksp", "kotlin-ksp", "ksp-version", "kotlin-symbol-processing"} {
			if v := strings.TrimSpace(prj.VersionCatalogData[key]); v != "" {
				return v
			}
		}
	}
	if v := latestCachedVersionFor("com.google.devtools.ksp", "symbol-processing-aa-embeddable"); v != "" {
		return v
	}
	return ""
}

// kspOutputRoots returns the canonical KSP output layout for a (module,
// variant) under build/grit. Mirrors AGP's `build/generated/ksp/<variant>`
// shape so downstream tooling that already understands AGP layouts can
// read it.
func kspOutputRoots(prj *project.Project, mod *project.Module, variantName, classOutputDir string) (root, kotlin, java, resources, classes, caches string) {
	rel := moduleOutputRelPath(mod.Path)
	root = filepath.Join(prj.RootDir, "build", "grit", rel, variantName, "ksp")
	kotlin = filepath.Join(root, "kotlin")
	java = filepath.Join(root, "java")
	resources = filepath.Join(root, "resources")
	caches = filepath.Join(root, "caches")
	classes = classOutputDir
	return
}

// resolveKSP2Runtime resolves the KSP2 driver jars (aa-embeddable + api
// + common-deps). All three are required: aa-embeddable hosts the
// engine, api carries the contract types, common-deps carries the CLI
// arg parser. Falls back to the local Gradle cache if the resolver
// returns empty (typical when offline).
func resolveKSP2Runtime(state *compileState, prj *project.Project, version string) ([]string, error) {
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("ksp version unknown for project %q (set it under [versions] in libs.versions.toml)", prj.Name)
	}
	resolver, err := state.resolverForProject(prj)
	if err != nil {
		return nil, err
	}
	deps := &modulebuild.Dependencies{
		Main: []modulebuild.Ref{
			{Kind: "raw", Value: "com.google.devtools.ksp:symbol-processing-aa-embeddable:" + version},
			{Kind: "raw", Value: "com.google.devtools.ksp:symbol-processing-api:" + version},
			{Kind: "raw", Value: "com.google.devtools.ksp:symbol-processing-common-deps:" + version},
		},
	}
	resolved, err := resolver.Resolve(deps)
	if err != nil {
		return nil, fmt.Errorf("resolve ksp2 runtime %s: %w", version, err)
	}
	jars := mergePaths(resolved.CompileJars, resolved.RuntimeJars)
	if len(jars) == 0 {
		jars = mergePaths(
			findGradleArtifactJars("com.google.devtools.ksp", "symbol-processing-aa-embeddable", version),
			findGradleArtifactJars("com.google.devtools.ksp", "symbol-processing-api", version),
			findGradleArtifactJars("com.google.devtools.ksp", "symbol-processing-common-deps", version),
		)
	}
	if len(jars) == 0 {
		return nil, fmt.Errorf("ksp2 runtime jars not found for version %s; bump KSP to a release that ships symbol-processing-aa-embeddable", version)
	}
	return jars, nil
}

// resolveKSPProcessors resolves the per-module processor refs into an
// ordered list of jar paths, including transitive runtime deps.
func resolveKSPProcessors(state *compileState, prj *project.Project, refs []modulebuild.Ref) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	resolver, err := state.resolverForProject(prj)
	if err != nil {
		return nil, err
	}
	resolved, err := resolver.Resolve(&modulebuild.Dependencies{Main: append([]modulebuild.Ref{}, refs...)})
	if err != nil {
		return nil, fmt.Errorf("resolve ksp processors: %w", err)
	}
	return mergePaths(resolved.CompileJars, resolved.RuntimeJars), nil
}

// kspLanguageVersion derives the KSP -language-version flag from the
// project's pinned Kotlin version. KSP accepts MAJOR.MINOR (e.g. "2.1");
// patch-level is dropped.
func kspLanguageVersion(kotlinVersion string) string {
	parts := strings.SplitN(kotlinVersion, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

// kspProcessorOptionsArg encodes processor options for KSP2's
// `-processor-options` flag. KSP expects `key1=value1:key2=value2` with
// the OS path separator between pairs.
func kspProcessorOptionsArg(opts map[string]string) string {
	if len(opts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+opts[k])
	}
	return strings.Join(pairs, string(os.PathListSeparator))
}

// ksp2Args builds the KSP2 CLI arg list for a single (module, variant)
// invocation. Processor classpath jars are positional and appended last.
func ksp2Args(modName, projectBaseDir, classOut, kotlinOut, javaOut, resourceOut, cachesOut, outputBase, sourceRoots, libraries, languageVersion, jvmTarget string, processorOptions string, processorJars []string) []string {
	args := []string{
		"-module-name=" + modName,
		"-source-roots=" + sourceRoots,
		"-project-base-dir=" + projectBaseDir,
		"-output-base-dir=" + outputBase,
		"-caches-dir=" + cachesOut,
		"-class-output-dir=" + classOut,
		"-kotlin-output-dir=" + kotlinOut,
		"-java-output-dir=" + javaOut,
		"-resource-output-dir=" + resourceOut,
		"-jvm-target=" + jvmTarget,
		"-language-version=" + languageVersion,
		"-api-version=" + languageVersion,
	}
	if libraries != "" {
		args = append(args, "-libraries="+libraries)
	}
	if processorOptions != "" {
		args = append(args, "-processor-options="+processorOptions)
	}
	args = append(args, processorJars...)
	return args
}

// kspSourceRoots returns the source root directories KSP2 should scan.
// Mirrors mainSourceRoots, but skips entries that don't exist on disk —
// KSP2 errors out if a passed root is missing.
func kspSourceRoots(mod *project.Module, variantName string) []string {
	roots := mainSourceRoots(mod, variantName)
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if pathIsDir(r) {
			out = append(out, r)
		}
	}
	return out
}

// runKSP2ForModule runs the KSP2 driver for one module/variant. Returns
// a zero-value compilation (with Ran=false) when the module declares no
// processors. Caller must append GeneratedKotlinFiles to the kotlinc
// source list and invoke javac on JavaGenDir afterward.
func (c *Compiler) runKSP2ForModule(ctx context.Context, state *compileState, prj *project.Project, mod *project.Module, variantName, classOutputDir string, compileCP []string, stdout, stderr *os.File) (kspCompilation, error) {
	var out kspCompilation
	if mod == nil || !mod.UsesKSP || len(mod.KSP.Processors) == 0 {
		return out, nil
	}
	version := projectKSPVersion(prj)
	if strings.TrimSpace(version) == "" {
		return out, fmt.Errorf("ksp applied to %s but no KSP version found in version catalog", mod.Path)
	}
	runtimeJars, err := resolveKSP2Runtime(state, prj, version)
	if err != nil {
		return out, err
	}
	processorCP, err := resolveKSPProcessors(state, prj, mod.KSP.Processors)
	if err != nil {
		return out, err
	}
	if len(processorCP) == 0 {
		return out, fmt.Errorf("no processor jars resolved for %s; declared refs: %v", mod.Path, mod.KSP.Processors)
	}
	root, kotlinDir, javaDir, resourceDir, classDir, cachesDir := kspOutputRoots(prj, mod, variantName, classOutputDir)
	for _, dir := range []string{root, kotlinDir, javaDir, resourceDir, cachesDir, classDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return out, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Toolchain access for kotlinc compiler version → KSP -language-version.
	toolchain, err := state.kotlinToolchainForProject(prj)
	if err != nil {
		return out, err
	}
	langVersion := kspLanguageVersion(toolchain.Version)
	if langVersion == "" {
		langVersion = "2.0"
	}

	sourceRoots := kspSourceRoots(mod, variantName)
	if len(sourceRoots) == 0 {
		return out, nil
	}

	libraries := compileCP
	if strings.HasPrefix(mod.Type, "android-") {
		libraries = append([]string{androidJarPath()}, libraries...)
	}
	// KSP scans -source-roots for sources; processors also need stdlib
	// types visible alongside compile classpath.
	libraries = mergePaths(libraries, toolchain.RuntimeJars)

	args := ksp2Args(
		ksp2ModuleName(mod, variantName),
		mod.Dir,
		classDir,
		kotlinDir,
		javaDir,
		resourceDir,
		cachesDir,
		root,
		strings.Join(sourceRoots, string(os.PathListSeparator)),
		strings.Join(libraries, string(os.PathListSeparator)),
		langVersion,
		"21",
		kspProcessorOptionsArg(mod.KSP.Options),
		processorCP,
	)

	if err := runKSP2(ctx, runtimeJars, args, stdout, stderr); err != nil {
		return out, fmt.Errorf("ksp2 run for %s/%s: %w", mod.Path, variantName, err)
	}

	out = kspCompilation{
		Version:              version,
		Ran:                  true,
		GeneratedKotlinFiles: collectGeneratedKotlinSources(kotlinDir),
		GeneratedJavaFiles:   collectGeneratedJavaSources(javaDir),
		JavaGenDir:           javaDir,
		KotlinGenDir:         kotlinDir,
		ResourceDir:          resourceDir,
		ClassDir:             classDir,
		ProcessorCP:          processorCP,
	}
	return out, nil
}

// ksp2ModuleName encodes a stable module name for KSP. Some processors
// (Hilt, Glide) bake the module name into generated class names, so
// using the module path keeps generated symbols predictable.
func ksp2ModuleName(mod *project.Module, variantName string) string {
	base := strings.TrimPrefix(mod.Path, ":")
	base = strings.ReplaceAll(base, ":", "-")
	if base == "" {
		base = "module"
	}
	if strings.TrimSpace(variantName) == "" {
		return base
	}
	return base + "-" + variantName
}

func collectGeneratedKotlinSources(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	var out []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".kt") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func collectGeneratedJavaSources(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	var out []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".java") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// kspHashTokens returns a deterministic, sorted token list summarizing
// the KSP configuration for action-hash inclusion. Includes processor
// coordinates (resolved refs in canonical "kind:value" form), processor
// options, and the KSP version. Output dirs are intentionally excluded —
// they're computed from the module path and aren't independent inputs.
func kspHashTokens(version string, refs []modulebuild.Ref, opts map[string]string) []string {
	if strings.TrimSpace(version) == "" && len(refs) == 0 && len(opts) == 0 {
		return nil
	}
	out := []string{"ksp.version=" + version}
	procs := make([]string, 0, len(refs))
	for _, r := range refs {
		procs = append(procs, "ksp.processor="+r.Kind+":"+r.Value)
	}
	sort.Strings(procs)
	out = append(out, procs...)
	if len(opts) > 0 {
		keys := make([]string, 0, len(opts))
		for k := range opts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, "ksp.option."+k+"="+opts[k])
		}
	}
	return out
}
