package nativecompile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/gradlecache"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

// kspRuntimeModules names the KSP2 jars that must reach the driver's
// classpath. aa-embeddable carries the engine + CLI main class; api and
// common-deps round out the contract types and arg parser.
var kspRuntimeModules = []string{
	"symbol-processing-aa-embeddable",
	"symbol-processing-api",
	"symbol-processing-common-deps",
}

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
// version catalog.
func projectKSPVersion(prj *project.Project) string {
	if prj != nil {
		for _, key := range []string{"ksp", "build-kotlin-ksp", "kotlin-ksp", "ksp-version", "kotlin-symbol-processing"} {
			if v := strings.TrimSpace(prj.VersionCatalogData[key]); v != "" {
				return v
			}
		}
		for _, path := range prj.VersionCatalogs {
			cat, err := catalog.Load(path)
			if err != nil || cat == nil {
				continue
			}
			for _, key := range []string{"ksp", "build-kotlin-ksp", "kotlin-ksp", "ksp-version", "kotlin-symbol-processing"} {
				if v := strings.TrimSpace(cat.Versions[key]); v != "" {
					return v
				}
			}
		}
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
// + common-deps) through the normal dependency resolver. All three are
// required: aa-embeddable hosts the engine, api carries the contract
// types, common-deps carries the CLI arg parser.
func resolveKSP2Runtime(state *compileState, prj *project.Project, version string) ([]string, error) {
	if strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("ksp version unknown for project %q (set it under [versions] in libs.versions.toml)", prj.Name)
	}
	resolver, err := state.resolverForProject(prj)
	if err != nil {
		return nil, err
	}
	probe := gradlecache.ProjectProbe(prj)
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
	jars = stripKSPRuntimeJars(jars)
	// m2local may substitute a nearby version per KSP runtime module
	// when exact-version sidecars are missing, which mixes ABI versions
	// (aa-embeddable@2.3.7 calling api@2.3.6 methods that don't exist
	// yet). Pick one coherent KSP version covered by the local cache and
	// add that whole set, instead of trusting per-module substitutions.
	coherent := pickCoherentKSPVersion(probe, version)
	if coherent == "" {
		return nil, fmt.Errorf("ksp2 runtime jars not resolved for version %s; gradle cache has no coherent set for any nearby version", version)
	}
	for _, module := range kspRuntimeModules {
		if jar := kspModuleCachedJar(probe, module, coherent); jar != "" {
			jars = append(jars, jar)
		}
	}
	// KSP2's standalone analysis engine pulls kotlinx-coroutines off the
	// runtime classpath; the resolver doesn't always traverse the
	// transitive edge, so backfill from the gradle cache when it isn't
	// already present.
	if !kspRuntimeContainsCoroutines(jars) {
		if jar := kotlinxCoroutinesCoreCachedJar(probe); jar != "" {
			jars = append(jars, jar)
		}
	}
	if len(jars) == 0 {
		return nil, fmt.Errorf("ksp2 runtime jars not resolved for version %s; ensure dependency repositories and lock/provenance include the KSP runtime", version)
	}
	return jars, nil
}

func kspRuntimeContainsCoroutines(paths []string) bool {
	for _, path := range paths {
		base := filepath.Base(path)
		if !strings.HasSuffix(base, ".jar") {
			continue
		}
		if strings.HasPrefix(base, "kotlinx-coroutines-core-jvm-") || strings.HasPrefix(base, "kotlinx-coroutines-core-") {
			return true
		}
	}
	return false
}

func kotlinxCoroutinesCoreCachedJar(probe *gradlecache.Probe) string {
	for _, module := range []string{"kotlinx-coroutines-core-jvm", "kotlinx-coroutines-core"} {
		latest := probe.LatestVersion("org.jetbrains.kotlinx", module)
		if latest == "" {
			continue
		}
		if jar := probe.FirstJar("org.jetbrains.kotlinx", module, latest); jar != "" {
			return jar
		}
	}
	return ""
}

// stripKSPRuntimeJars removes any jar belonging to a KSP runtime module
// (including the `-jvm` published variants) from paths so callers can
// rebuild a version-coherent set.
func stripKSPRuntimeJars(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		base := filepath.Base(path)
		drop := false
		for _, module := range kspRuntimeModules {
			if strings.HasPrefix(base, module+"-") || strings.HasPrefix(base, module+"-jvm-") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, path)
		}
	}
	return out
}

// pickCoherentKSPVersion returns a KSP version cached in full locally,
// preferring the requested version and otherwise downgrading: processor
// jars compiled for older KSP releases routinely break against a newer
// runtime. Returns "" when no covered version exists.
func pickCoherentKSPVersion(probe *gradlecache.Probe, requested string) string {
	if requested != "" && kspVersionCoversAllRuntimeModules(probe, requested) {
		return requested
	}
	var covered []string
	for _, v := range cachedKSPVersions(probe) {
		if kspVersionCoversAllRuntimeModules(probe, v) {
			covered = append(covered, v)
		}
	}
	if len(covered) == 0 {
		return ""
	}
	sort.Slice(covered, func(i, j int) bool {
		return compareVersion(covered[i], covered[j]) < 0
	})
	var best string
	for _, v := range covered {
		if requested == "" || compareVersion(v, requested) <= 0 {
			best = v
		}
	}
	if best != "" {
		return best
	}
	return covered[0]
}

func kspVersionCoversAllRuntimeModules(probe *gradlecache.Probe, version string) bool {
	for _, module := range kspRuntimeModules {
		if kspModuleCachedJar(probe, module, version) == "" {
			return false
		}
	}
	return true
}

func kspModuleCachedJar(probe *gradlecache.Probe, module, version string) string {
	for _, candidate := range []string{module, module + "-jvm"} {
		if jar := probe.FirstJar("com.google.devtools.ksp", candidate, version); jar != "" {
			return jar
		}
	}
	return ""
}

// cachedKSPVersions returns the union of version directories cached under
// any of the KSP runtime modules' coordinates. The result is unsorted.
func cachedKSPVersions(probe *gradlecache.Probe) []string {
	seen := make(map[string]bool)
	for _, module := range kspRuntimeModules {
		for _, candidate := range []string{module, module + "-jvm"} {
			for _, v := range probe.Versions("com.google.devtools.ksp", candidate, nil) {
				seen[v] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	return out
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
	jars := mergePaths(resolved.CompileJars, resolved.RuntimeJars)
	if len(jars) > 0 {
		return mergePaths(jars), nil
	}
	fallbackRefs := kspProcessorJVMFallbackRefs(prj, refs)
	if len(fallbackRefs) == 0 {
		return jars, nil
	}
	resolved, err = resolver.Resolve(&modulebuild.Dependencies{Main: fallbackRefs})
	if err != nil {
		return nil, fmt.Errorf("resolve ksp jvm processors: %w", err)
	}
	jars = mergePaths(resolved.CompileJars, resolved.RuntimeJars)
	if len(jars) > 0 {
		return jars, nil
	}
	return resolveClasspathRefs(resolver, fallbackRefs, true), nil
}

func kspProcessorJVMFallbackRefs(prj *project.Project, refs []modulebuild.Ref) []modulebuild.Ref {
	return jvmFallbackRefs(prj, refs)
}

func fallbackJVMCompileJars(prj *project.Project, resolver dependencywiring.DependencyResolver, refs []modulebuild.Ref) []string {
	coords := catalogCoordinates(prj, refs)
	fallbackRefs := append(rawCoordinateRefs(coords), jvmFallbackRefs(prj, refs)...)
	out := resolveClasspathRefs(resolver, fallbackRefs, true)
	return mergePaths(out, companionCoreJVMJars(prj, resolver, out))
}

func fallbackAndroidCompileJars(prj *project.Project, resolver dependencywiring.DependencyResolver, refs []modulebuild.Ref) []string {
	var fallbackRefs []modulebuild.Ref
	for _, coord := range catalogCoordinates(prj, refs) {
		fallbackRefs = append(fallbackRefs, modulebuild.Ref{Kind: "raw", Value: coord})
		parts := strings.Split(coord, ":")
		if len(parts) == 3 && !strings.HasSuffix(parts[1], "-android") {
			androidCoord := parts[0] + ":" + parts[1] + "-android:" + parts[2]
			fallbackRefs = append(fallbackRefs, modulebuild.Ref{Kind: "raw", Value: androidCoord})
		}
	}
	return resolveClasspathRefs(resolver, fallbackRefs, true)
}

func fallbackImportedCatalogJars(prj *project.Project, resolver dependencywiring.DependencyResolver, mod *project.Module) []string {
	cat, err := dependencywiring.LoadCatalog(prj)
	if err != nil || cat == nil || mod == nil {
		return nil
	}
	type importFallback struct {
		needle string
		groups []string
	}
	fallbacks := []importFallback{
		{needle: "androidx.compose.ui.platform.LocalResources", groups: []string{"androidx.compose.ui"}},
		{needle: "app.cash.sqldelight.", groups: []string{"app.cash.sqldelight"}},
		{needle: "androidx.compose.remote.", groups: []string{"androidx.compose.remote", "androidx.wear.compose.remote"}},
		{needle: "coil3.", groups: []string{"io.coil-kt.coil3"}},
		{needle: "co.touchlab.kermit.", groups: []string{"co.touchlab"}},
		{needle: "io.ktor.client.engine.android.", groups: []string{"io.ktor"}},
		{needle: "okio.", groups: []string{"com.squareup.okio"}},
		{needle: "org.koin.", groups: []string{"io.insert-koin"}},
	}
	var out []string
	var fallbackRefs []modulebuild.Ref
	for _, fallback := range fallbacks {
		if !moduleSourcesContain(mod, fallback.needle) {
			continue
		}
		if fallback.needle == "androidx.compose.ui.platform.LocalResources" {
			if jar := latestMaterializedAARClasses(prj, "androidx.compose.ui", "ui-android"); jar != "" {
				out = append(out, jar)
			}
		}
		if fallback.needle == "coil3." {
			if version := catalogGroupVersion(cat, "io.coil-kt.coil3"); version != "" {
				fallbackRefs = append(fallbackRefs, modulebuild.Ref{Kind: "raw", Value: "io.coil-kt.coil3:coil-core-jvm:" + version})
			}
		}
		if fallback.needle == "okio." {
			if jar := latestMaterializedJar(prj, "com.squareup.okio", "okio-jvm"); jar != "" {
				out = append(out, jar)
			}
		}
		for _, lib := range cat.Libraries {
			if !stringInList(lib.Group, fallback.groups) {
				continue
			}
			if lib.Version == "" && lib.VersionRef != "" {
				lib.Version = cat.Versions[lib.VersionRef]
			}
			if lib.Group == "" || lib.Name == "" || lib.Version == "" {
				continue
			}
			coord := lib.Group + ":" + lib.Name + ":" + lib.Version
			fallbackRefs = append(fallbackRefs, modulebuild.Ref{Kind: "raw", Value: coord})
			if !strings.HasSuffix(lib.Name, "-android") {
				androidCoord := lib.Group + ":" + lib.Name + "-android:" + lib.Version
				fallbackRefs = append(fallbackRefs, modulebuild.Ref{Kind: "raw", Value: androidCoord})
			}
			if strings.HasSuffix(lib.Name, "-jvm") {
				continue
			} else {
				jvmCoord := lib.Group + ":" + lib.Name + "-jvm:" + lib.Version
				fallbackRefs = append(fallbackRefs, modulebuild.Ref{Kind: "raw", Value: jvmCoord})
			}
		}
	}
	resolved := resolveClasspathRefs(resolver, fallbackRefs, true)
	return mergePaths(out, resolved, companionCoreJVMJars(prj, resolver, append(out, resolved...)))
}

func stringInList(value string, list []string) bool {
	for _, item := range list {
		if value == item {
			return true
		}
	}
	return false
}

func catalogGroupVersion(cat *catalog.Catalog, group string) string {
	if cat == nil || group == "" {
		return ""
	}
	for _, lib := range cat.Libraries {
		if lib.Group != group {
			continue
		}
		if lib.Version != "" {
			return lib.Version
		}
		if lib.VersionRef != "" {
			return cat.Versions[lib.VersionRef]
		}
	}
	return ""
}

func catalogCoordinates(prj *project.Project, refs []modulebuild.Ref) []string {
	cat, err := dependencywiring.LoadCatalog(prj)
	if err != nil || cat == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(group, module, version string) {
		if group == "" || module == "" || version == "" {
			return
		}
		coord := group + ":" + module + ":" + version
		if seen[coord] {
			return
		}
		seen[coord] = true
		out = append(out, coord)
	}
	for _, ref := range refs {
		switch ref.Kind {
		case "library":
			lib, err := cat.ResolveLibrary(ref.Value)
			if err == nil {
				add(lib.Group, lib.Name, lib.Version)
			}
		case "raw":
			if group, module, ok := m2local.ComposeAccessorModule(ref.Value); ok {
				version := ""
				if strings.HasPrefix(group, "androidx.compose") {
					version = latestMaterializedAARVersion(prj, group, module)
				}
				if version == "" {
					for _, key := range []string{"compose-multiplatform", "composeMultiplatform", "compose"} {
						if v := strings.TrimSpace(cat.Versions[key]); v != "" {
							version = v
							break
						}
					}
				}
				add(group, module, version)
				continue
			}
			parts := strings.Split(ref.Value, ":")
			if len(parts) == 3 {
				add(parts[0], parts[1], parts[2])
			}
		}
	}
	return out
}

func latestMaterializedAARVersion(prj *project.Project, group, module string) string {
	if prj == nil {
		return ""
	}
	root := filepath.Join(prj.RootDir, ".grit", "worktree", "aar", group, module)
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var best string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() > best {
			best = entry.Name()
		}
	}
	return best
}

func latestMaterializedAARClasses(prj *project.Project, group, module string) string {
	version := latestMaterializedAARVersion(prj, group, module)
	if version == "" {
		return ""
	}
	jar := filepath.Join(prj.RootDir, ".grit", "worktree", "aar", group, module, version, "classes.jar")
	if !pathIsFile(jar) {
		return ""
	}
	return jar
}

func latestMaterializedJar(prj *project.Project, group, module string) string {
	if prj == nil || group == "" || module == "" {
		return ""
	}
	root := filepath.Join(prj.RootDir, ".grit", "worktree", "materialized-m2", filepath.Join(strings.Split(group, ".")...), module)
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var bestVersion string
	var bestJar string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() < bestVersion {
			continue
		}
		jar := filepath.Join(root, entry.Name(), module+"-"+entry.Name()+".jar")
		if !pathIsFile(jar) {
			continue
		}
		bestVersion = entry.Name()
		bestJar = jar
	}
	return bestJar
}

func jvmFallbackRefs(prj *project.Project, refs []modulebuild.Ref) []modulebuild.Ref {
	cat, err := dependencywiring.LoadCatalog(prj)
	if err != nil || cat == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []modulebuild.Ref
	for _, ref := range refs {
		if ref.Kind != "library" {
			continue
		}
		lib, err := cat.ResolveLibrary(ref.Value)
		if err != nil || lib.Group == "" || lib.Name == "" || lib.Version == "" {
			continue
		}
		coord := lib.Group + ":" + lib.Name + "-jvm:" + lib.Version
		if seen[coord] {
			continue
		}
		seen[coord] = true
		out = append(out, modulebuild.Ref{Kind: "raw", Value: coord})
	}
	return out
}

func rawCoordinateRefs(coords []string) []modulebuild.Ref {
	refs := make([]modulebuild.Ref, 0, len(coords))
	for _, coord := range coords {
		if strings.Count(coord, ":") != 2 {
			continue
		}
		refs = append(refs, modulebuild.Ref{Kind: "raw", Value: coord})
	}
	return refs
}

func resolveClasspathRefs(resolver dependencywiring.DependencyResolver, refs []modulebuild.Ref, includeAndroid bool) []string {
	if resolver == nil || len(refs) == 0 {
		return nil
	}
	resolved, err := resolver.Resolve(&modulebuild.Dependencies{Main: append([]modulebuild.Ref{}, refs...)})
	if err != nil || resolved == nil {
		return nil
	}
	out := mergePaths(resolved.CompileJars, resolved.RuntimeJars)
	if includeAndroid {
		for _, lib := range resolved.AndroidLibraries {
			if lib.ClassesJar != "" {
				out = append(out, lib.ClassesJar)
			}
		}
	}
	return mergePaths(out)
}

func materializedJarCoordinate(prj *project.Project, path string) string {
	if prj == nil || path == "" {
		return ""
	}
	root := filepath.Join(prj.RootDir, ".grit", "worktree", "materialized-m2")
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 4 {
		return ""
	}
	version := parts[len(parts)-2]
	module := parts[len(parts)-3]
	file := parts[len(parts)-1]
	if file != module+"-"+version+".jar" {
		return ""
	}
	group := strings.Join(parts[:len(parts)-3], ".")
	if group == "" || module == "" || version == "" {
		return ""
	}
	return group + ":" + module + ":" + version
}

func companionCoreJVMJars(prj *project.Project, resolver dependencywiring.DependencyResolver, jars []string) []string {
	var refs []modulebuild.Ref
	seen := map[string]bool{}
	add := func(coord string) {
		if coord == "" || seen[coord] {
			return
		}
		seen[coord] = true
		refs = append(refs, modulebuild.Ref{Kind: "raw", Value: coord})
	}
	for _, jar := range jars {
		coord := materializedJarCoordinate(prj, jar)
		parts := strings.Split(coord, ":")
		if len(parts) != 3 || !strings.HasSuffix(parts[1], "-jvm") {
			continue
		}
		base := strings.TrimSuffix(parts[1], "-jvm")
		if strings.HasSuffix(base, "-core") {
			continue
		}
		add(parts[0] + ":" + base + "-core-jvm:" + parts[2])
	}
	return resolveClasspathRefs(resolver, refs, false)
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
		"-incremental=false",
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
	if moduleUsesKotlinMultiplatform(mod) {
		roots = []string{filepath.Join(mod.Dir, "src", "commonMain")}
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if pathIsDir(r) {
			out = append(out, r)
		}
	}
	return out
}

// kspMode reports which KSP runner to invoke. Default is mode 2 (the
// standalone KSP2 JVM driver). Set GRIT_KSP_MODE=1 to request the
// legacy KSP1 path — a kotlinc compiler plugin invocation that
// avoids the KSP2 standalone-driver coroutine live-lock observed on
// some large processor graphs. Mode 1 is reserved scaffolding; the
// body is not yet implemented and returns a structured error so
// callers see the intent without silent fallback.
func kspMode() int {
	switch strings.TrimSpace(os.Getenv("GRIT_KSP_MODE")) {
	case "1":
		return 1
	case "2", "":
		return 2
	}
	return 2
}

// runKSP2ForModule runs the configured KSP runner for one module/
// variant. Returns a zero-value compilation (with Ran=false) when the
// module declares no processors. Caller must append
// GeneratedKotlinFiles to the kotlinc source list and invoke javac on
// JavaGenDir afterward.
//
// Despite its name (kept for caller compatibility) this entry point
// dispatches via kspMode(). The KSP2 standalone driver is the
// default; GRIT_KSP_MODE=1 routes to runKSP1ForModule.
func (c *Compiler) runKSP2ForModule(ctx context.Context, state *compileState, prj *project.Project, mod *project.Module, variantName, classOutputDir string, compileCP []string, stdout, stderr *os.File) (kspCompilation, error) {
	var out kspCompilation
	if mod == nil || !mod.UsesKSP || len(mod.KSP.Processors) == 0 {
		return out, nil
	}
	if kspMode() == 1 {
		return c.runKSP1ForModule(ctx, state, prj, mod, variantName, classOutputDir, compileCP, stdout, stderr)
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
	resolver, err := state.resolverForProject(prj)
	if err != nil {
		return out, err
	}
	processorCP = mergePaths(processorCP, fallbackJVMCompileJars(prj, resolver, mod.KSP.Processors))
	processorCP = mergePaths(processorCP, compileCP)
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
		"",
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

// runKSP1ForModule is the entry point for the KSP1 (kotlinc compiler
// plugin) fallback path requested via GRIT_KSP_MODE=1. KSP1 differs
// from KSP2 in two important ways: the symbol-processing plugin runs
// inside the kotlinc process (not a separate JVM), and processors
// observe the same in-flight compilation rather than a pre-resolved
// source set. That avoids the standalone KSP2 driver's coroutine
// live-lock at the cost of a more complex kotlinc invocation.
//
// The opt-in surface lives now so callers can flip the switch and
// receive a structured error instead of silent fallback to KSP2. The
// body — resolving com.google.devtools.ksp:symbol-processing at the
// project's KSP version, building the kotlinc plugin arg list with
// the right option keys, and merging the integrated kotlinc
// invocation back into compileMainInternal — needs end-to-end
// validation on a real KSP-using project and is filed as a separate
// piece of work.
func (c *Compiler) runKSP1ForModule(_ context.Context, _ *compileState, _ *project.Project, mod *project.Module, _ string, _ string, _ []string, _, _ *os.File) (kspCompilation, error) {
	return kspCompilation{}, fmt.Errorf("ksp1 fallback requested for %s but not yet implemented; unset GRIT_KSP_MODE or set GRIT_KSP_MODE=2 to use the default KSP2 driver, or extend GRIT_KSP_TIMEOUT for the current invocation", mod.Path)
}
