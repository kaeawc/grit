package nativecompile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

// kspPluginEntry is the assembled KSP1 compiler-plugin invocation for a
// single module/variant: the kotlinc -Xplugin JAR plus the per-pass
// option list (apclasspath, projectBaseDir, output dirs, etc.).
//
// It also records the per-variant generated-source roots so callers can
// include them in compile-input hashing and downstream packaging.
type kspPluginEntry struct {
	PluginJars   []string
	Options      []string
	KSPVersion   string
	ProcessorCP  []string
	GeneratedDir string
	JavaGenDir   string
	KotlinGenDir string
	ResourceDir  string
	ClassDir     string
}

// projectKSPVersion attempts to resolve the KSP version pinned by the
// project's version catalog. Falls back to the most recent symbol-processing
// version visible in the local Gradle cache.
func projectKSPVersion(prj *project.Project) string {
	if prj != nil {
		for _, key := range []string{"ksp", "build-kotlin-ksp", "kotlin-ksp", "ksp-version", "kotlin-symbol-processing"} {
			if v := strings.TrimSpace(prj.VersionCatalogData[key]); v != "" {
				return v
			}
		}
	}
	if v := latestCachedVersionFor("com.google.devtools.ksp", "symbol-processing"); v != "" {
		return v
	}
	return ""
}

// resolveKSPRuntime resolves the symbol-processing kotlinc plugin JAR
// (and its api companion) via the project's dependency resolver. KSP's
// compiler plugin lives in the `symbol-processing` artifact; the
// `symbol-processing-api` artifact carries types referenced by processor
// authors and is included to keep classloading consistent.
func resolveKSPRuntime(state *compileState, prj *project.Project, version string) ([]string, error) {
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("ksp version unknown for project %q (set it under [versions] in libs.versions.toml)", prj.Name)
	}
	resolver, err := state.resolverForProject(prj)
	if err != nil {
		return nil, err
	}
	deps := &modulebuild.Dependencies{
		Main: []modulebuild.Ref{
			{Kind: "raw", Value: "com.google.devtools.ksp:symbol-processing:" + version},
			{Kind: "raw", Value: "com.google.devtools.ksp:symbol-processing-api:" + version},
		},
	}
	resolved, err := resolver.Resolve(deps)
	if err != nil {
		return nil, fmt.Errorf("resolve ksp runtime %s: %w", version, err)
	}
	all := mergePaths(resolved.CompileJars, resolved.RuntimeJars)
	jars := filterKSPRuntimeJars(all)
	if len(jars) == 0 {
		// Fall back to the local Gradle cache if the resolver didn't
		// materialize jars (common when offline). Both artifacts are
		// looked up because either one alone is insufficient.
		jars = mergePaths(
			findGradleArtifactJars("com.google.devtools.ksp", "symbol-processing", version),
			findGradleArtifactJars("com.google.devtools.ksp", "symbol-processing-api", version),
		)
	}
	if len(jars) == 0 {
		return nil, fmt.Errorf("ksp runtime jars not found for version %s", version)
	}
	return jars, nil
}

// filterKSPRuntimeJars narrows a resolver classpath down to KSP's own
// kotlinc-plugin jars. The resolver may surface unrelated transitive
// jars (older KSP API versions hide in older processor poms,
// kotlinx-serialization is a dependency of incremental-compile metadata,
// etc.), but only the symbol-processing core/api jars belong on
// kotlinc's -Xplugin classpath. Other transitives, if needed, flow in
// through the regular compile classpath.
func filterKSPRuntimeJars(jars []string) []string {
	var out []string
	for _, jar := range jars {
		base := filepath.Base(jar)
		if strings.HasPrefix(base, "symbol-processing-") || strings.HasPrefix(base, "symbol-processing-api-") {
			out = append(out, jar)
		}
	}
	return out
}

// resolveKSPProcessors resolves the per-module processor refs into an
// ordered list of jar paths. The list includes transitive runtime deps
// so processors that pull in additional libraries (e.g. a code-gen
// helper) load cleanly.
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

// kspOutputRoots returns the canonical output directory layout for a
// (module, variant) under build/grit. Mirrors the layout AGP produces
// under build/generated/ksp/<variant>/{kotlin,java,resources} so any
// downstream tooling that already understands AGP layouts can read it.
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

// kspPluginOptions builds the kotlinc -P option list for a single KSP
// invocation. Returned strings are suitable for direct concatenation
// after `-P plugin:<id>:<key>=<value>` (the existing pluginOptions
// pipeline prepends `-P` in command_args.go).
func kspPluginOptions(processorCP []string, projectDir, kotlinDir, javaDir, resourceDir, classDir, cachesDir string, processorOpts map[string]string) []string {
	id := modulebuild.KSPCompilerPluginID
	pair := func(k, v string) string {
		return fmt.Sprintf("plugin:%s:%s=%s", id, k, v)
	}
	cpSep := string(os.PathListSeparator)
	out := []string{
		pair("apclasspath", strings.Join(processorCP, cpSep)),
		pair("projectBaseDir", projectDir),
		pair("classOutputDir", classDir),
		pair("javaOutputDir", javaDir),
		pair("kotlinOutputDir", kotlinDir),
		pair("resourceOutputDir", resourceDir),
		pair("kspOutputDir", filepath.Dir(kotlinDir)),
		pair("cachesDir", cachesDir),
		pair("incremental", "false"),
		pair("incrementalLog", "false"),
		pair("allWarningsAsErrors", "false"),
		pair("returnOkOnError", "false"),
		pair("withCompilation", "true"),
	}
	if len(processorOpts) > 0 {
		// Per-processor options are passed individually; KSP recognizes
		// the `apoption` key with `name=value` payloads, repeated.
		keys := make([]string, 0, len(processorOpts))
		for k := range processorOpts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, pair("apoption", k+"="+processorOpts[k]))
		}
	}
	return out
}

// kspPluginForModule assembles the full kotlinc-plugin entry for a
// module's KSP processors. Returns a zero-value entry (and nil error)
// when the module has no processors declared, so callers can append
// unconditionally without branching.
func (s *compileState) kspPluginForModule(prj *project.Project, mod *project.Module, variantName, classOutputDir string) (kspPluginEntry, error) {
	var entry kspPluginEntry
	if mod == nil || !mod.UsesKSP || len(mod.KSP.Processors) == 0 {
		return entry, nil
	}
	version := projectKSPVersion(prj)
	if strings.TrimSpace(version) == "" {
		return entry, fmt.Errorf("ksp applied to %s but no KSP version found in version catalog", mod.Path)
	}
	pluginJars, err := resolveKSPRuntime(s, prj, version)
	if err != nil {
		return entry, err
	}
	processorCP, err := resolveKSPProcessors(s, prj, mod.KSP.Processors)
	if err != nil {
		return entry, err
	}
	if len(processorCP) == 0 {
		return entry, fmt.Errorf("no processor jars resolved for %s; declared refs: %v", mod.Path, mod.KSP.Processors)
	}
	root, kotlinDir, javaDir, resourceDir, classDir, cachesDir := kspOutputRoots(prj, mod, variantName, classOutputDir)
	for _, dir := range []string{root, kotlinDir, javaDir, resourceDir, cachesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return entry, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	entry = kspPluginEntry{
		PluginJars:   pluginJars,
		Options:      kspPluginOptions(processorCP, mod.Dir, kotlinDir, javaDir, resourceDir, classDir, cachesDir, mod.KSP.Options),
		KSPVersion:   version,
		ProcessorCP:  processorCP,
		GeneratedDir: root,
		JavaGenDir:   javaDir,
		KotlinGenDir: kotlinDir,
		ResourceDir:  resourceDir,
		ClassDir:     classDir,
	}
	return entry, nil
}

// collectGeneratedJavaSources walks dir for .java files and returns
// their paths sorted for determinism. KSP-generated Kotlin is compiled
// alongside originals by kotlinc in single-pass mode, but generated
// Java needs a follow-up javac since kotlinc doesn't emit .class files
// for Java sources.
func collectGeneratedJavaSources(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	var out []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
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
