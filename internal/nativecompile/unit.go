package nativecompile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func (c *Compiler) TestDebugUnit(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	return c.compileAndMaybeRunDebugUnit(ctx, prj, modulePath, variantName, stdout, stderr, true, true)
}

func (c *Compiler) CompileDebugUnit(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	return c.compileAndMaybeRunDebugUnit(ctx, prj, modulePath, variantName, stdout, stderr, false, true)
}

func (c *Compiler) RunDebugUnit(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return fmt.Errorf("module %s not found", modulePath)
	}
	if variantName == "" {
		variantName = mod.DefaultVariantName()
	}
	testOut := filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName+"UnitTest", "classes")
	testCompileStampPath := filepath.Join(filepath.Dir(testOut), "compile.stamp")
	if !hasOutputFiles(testOut) {
		recordCacheProbe(c.tracker, "compileTests", false, "cache-miss", "compiled test outputs were not available to the test action")
		return fmt.Errorf("compiled unit test outputs for %s %s are missing; run compileDebugUnitTestSources before testDebugUnitTest", mod.Path, variantName)
	}
	recordCacheProbe(c.tracker, "compileTests", true, "local-up-to-date", "reused compiled test outputs from prior action")

	var testClasses []string
	err := c.track("discoverJUnitTests", func() error {
		var innerErr error
		testClasses, innerErr = discoverJUnitTestsInRoots(unitTestSourceRoots(mod, variantName), os.Getenv("GOJVM_INCLUDE_AUTOMOBILE") == "")
		return innerErr
	})
	if err != nil {
		return err
	}
	if len(testClasses) == 0 {
		_, _ = fmt.Fprintln(stdout, "compiled tests but found no JUnit test classes")
		return nil
	}

	runtimeSupportCP := runtimeSupportJars()
	junitSupportCP := junitRuntimeSupportJars()
	runSupportCP := mergePaths(junitSupportCP, runtimeSupportCP)
	if testRunCachePath, ok := unitTestRunCachePathFromCompileStamp(prj.RootDir, mod.Path, variantName, testClasses, testCompileStampPath, runSupportCP); ok {
		return c.track("runJUnit", func() error {
			if canReuseUnitTestRun(testRunCachePath) {
				recordCacheProbe(c.tracker, "runJUnit", true, "shared-cache-hit", "reused successful unit test result from shared cache")
				return nil
			}
			return c.compileAndMaybeRunDebugUnit(ctx, prj, modulePath, variantName, stdout, stderr, true, false)
		})
	}
	return c.compileAndMaybeRunDebugUnit(ctx, prj, modulePath, variantName, stdout, stderr, true, false)
}

func (c *Compiler) compileAndMaybeRunDebugUnit(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File, run bool, compileTests bool) error {
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return fmt.Errorf("module %s not found", modulePath)
	}
	if variantName == "" {
		variantName = mod.DefaultVariantName()
	}
	includeAndroid := mod.IsAndroid()
	state := newCompileState()
	toolchain, err := state.kotlinToolchainForProject(prj)
	if err != nil {
		return err
	}
	var mainOut string
	var cp []string
	err = c.track("compileMain", func() error {
		var innerErr error
		mainOut, cp, _, innerErr = c.compileMainInternal(ctx, prj, mod, variantName, state, nil, stdout, stderr)
		return innerErr
	})
	if err != nil {
		return err
	}

	var testSources []string
	err = c.track("collectTestSources", func() error {
		var innerErr error
		testSources, innerErr = collectUnitTestSources(mod, variantName)
		return innerErr
	})
	if err != nil {
		return err
	}
	if len(testSources) == 0 {
		_, _ = fmt.Fprintln(stdout, "no test sources found")
		return nil
	}

	testOut := filepath.Join(prj.RootDir, "build", "grit", moduleOutputRelPath(mod.Path), variantName+"UnitTest", "classes")
	if err := os.MkdirAll(testOut, 0o755); err != nil {
		return err
	}
	testJarPath := filepath.Join(filepath.Dir(testOut), "test-classes.jar")
	testCompileStampPath := filepath.Join(filepath.Dir(testOut), "compile.stamp")
	testSourceFingerprintPath := filepath.Join(filepath.Dir(testOut), "source-fingerprints.json")
	if compileTests && !run && pathIsFile(testCompileStampPath) && hasOutputFiles(testOut) && semanticSourceFingerprintsMatch(testSourceFingerprintPath, testSources) {
		recordCacheProbe(c.tracker, "compileTests", true, "local-up-to-date", "unit test source changes did not affect semantic compile inputs")
		_, _ = fmt.Fprintln(stdout, "unit test sources compiled")
		return nil
	}

	var deps *modulebuild.Dependencies
	err = c.track("parseDependencies", func() error {
		var innerErr error
		deps, innerErr = modulebuild.ParseDependenciesForModule(mod.BuildFile, prj.RootDir, mod.Plugins)
		if innerErr == nil {
			deps = dependenciesForVariant(deps, mod, variantName)
		}
		return innerErr
	})
	if err != nil {
		return err
	}
	var resolver dependencywiring.DependencyResolver
	err = c.track("loadCatalog", func() error {
		var innerErr error
		resolver, innerErr = state.resolverForProject(prj)
		return innerErr
	})
	if err != nil {
		return err
	}
	resolver.SetTracker(c.tracker)
	compileDeps := *deps
	compileDeps.Main = append(append([]modulebuild.Ref{}, deps.Main...), deps.CompileOnly...)
	var resolved *m2local.Resolved
	endResolve := c.beginSerial("resolveTestDependencies")
	err = c.track("loadUnitTestResolutionCache", func() error {
		var innerErr error
		resolved, innerErr = loadUnitTestResolvedCache(prj, mod, variantName, &compileDeps)
		if innerErr == nil {
			if resolved != nil {
				recordCacheProbe(c.tracker, "resolveTestDependencies", true, "shared-cache-hit", "restored resolved test dependencies from shared cache")
			} else {
				recordCacheProbe(c.tracker, "resolveTestDependencies", false, "cache-miss", "resolved test dependencies not present in shared cache")
			}
		}
		return innerErr
	})
	if err == nil && resolved == nil {
		err = c.track("resolveRemoteAndLocalTestDependencies", func() error {
			var innerErr error
			resolved, innerErr = resolver.Resolve(&compileDeps)
			return innerErr
		})
		if err == nil {
			err = c.track("mergeLocalTestDependencyRefs", func() error {
				localTestRefs := resolveLocalDependencyRefs(prj, mod, append(append([]modulebuild.Ref{}, deps.Test...), deps.TestCompileOnly...))
				localTestRefs = mergePaths(localTestRefs, resolveLocalDependencyRefs(prj, mod, deps.TestRuntimeOnly))
				resolved.TestJars = mergePaths(resolved.TestJars, localTestRefs)
				return nil
			})
		}
		if err == nil {
			err = c.track("saveUnitTestResolutionCache", func() error {
				return saveUnitTestResolvedCache(prj, mod, variantName, &compileDeps, resolved)
			})
		}
	}
	endResolve()
	if err != nil {
		return err
	}

	var projectTestCP []string
	var projectTestRuntimeInputs []string
	err = c.track("resolveProjectTestDeps", func() error {
		testProjectDeps := &modulebuild.Dependencies{
			Main:            append(append([]modulebuild.Ref{}, deps.Test...), deps.TestCompileOnly...),
			RuntimeOnly:     append([]modulebuild.Ref{}, deps.TestRuntimeOnly...),
			CompileOnly:     nil,
			Debug:           nil,
			Test:            nil,
			TestCompileOnly: nil,
			TestRuntimeOnly: nil,
		}
		var innerErr error
		compileRefs := append(append([]modulebuild.Ref{}, testProjectDeps.Main...), testProjectDeps.CompileOnly...)
		runtimeRefs := append(append([]modulebuild.Ref{}, testProjectDeps.Main...), testProjectDeps.RuntimeOnly...)
		projectTestCP, projectTestRuntimeInputs, _, innerErr = c.resolveProjectDeps(ctx, prj, mod.Path+"#unitTest", compileRefs, runtimeRefs, variantName, state, nil, stdout, stderr)
		return innerErr
	})
	if err != nil {
		return err
	}

	testCP := append([]string{}, resolved.TestJars...)
	testCP = append(testCP, toolchain.TestRuntimeJars...)
	testCP = append(testCP, kotlinTestShimJar())
	testCP = append(testCP, junitJupiterApiJar())
	testCP = append(testCP, projectTestCP...)
	testCP = append(testCP, projectTestRuntimeInputs...)
	testCP = append(testCP, mainOut)
	testCP = append(testCP, cp...)
	if compileTests {
		testCompileInputs := append([]string{}, testSources...)
		testCompileInputs = append(testCompileInputs, mod.BuildFile)
		testCompileInputs = append(testCompileInputs, prj.VersionCatalogs...)
		testCompileInputs = append(testCompileInputs, mainOut)
		testCompileInputs = append(testCompileInputs, testCP...)
		testCompileInputs = append(testCompileInputs, toolchain.RuntimeJars...)
		testCompileInputs = append(testCompileInputs, toolchain.CompilerClasspath...)
		testSharedCompileDir := moduleCompileCacheDir(mod.Path+"#unitTest", variantName, mod.ResolveVariant(variantName).ConfigHash(), testCompileInputs)
		endCompileTests := c.beginSerial("compileTests")
		err = c.track("restoreCompileTestsCache", func() error {
			if stampMatches(testCompileStampPath, testSharedCompileDir) && hasOutputFiles(testOut) {
				recordCacheProbe(c.tracker, "compileTests", true, "local-up-to-date", "test compile stamp matched local outputs")
				return nil
			}
			if outputsNewerThanInputs(testOut, testCompileInputs) {
				_ = writeStamp(testCompileStampPath, testSharedCompileDir)
				_ = writeSemanticSourceFingerprints(testSourceFingerprintPath, testSources)
				recordCacheProbe(c.tracker, "compileTests", true, "local-up-to-date", "compiled test outputs newer than test inputs")
				return nil
			}
			if restoreSharedCompileCache(testOut, testJarPath, testSharedCompileDir) {
				_ = writeStamp(testCompileStampPath, testSharedCompileDir)
				_ = writeSemanticSourceFingerprints(testSourceFingerprintPath, testSources)
				recordCacheProbe(c.tracker, "compileTests", true, "shared-cache-hit", "restored compiled tests from shared cache")
			}
			return nil
		})
		if err == nil && !stampMatches(testCompileStampPath, testSharedCompileDir) {
			if semanticSourceFingerprintsMatch(testSourceFingerprintPath, testSources) && hasOutputFiles(testOut) {
				recordCacheProbe(c.tracker, "compileTests", true, "local-up-to-date", "unit test source changes did not affect semantic compile inputs")
				_ = writeStamp(testCompileStampPath, testSharedCompileDir)
				_ = writeSemanticSourceFingerprints(testSourceFingerprintPath, testSources)
			} else {
				recordCacheProbe(c.tracker, "compileTests", false, "cache-miss", "compiled tests required fresh Kotlin compilation")
				extraArgs := []string{"-Xfriend-paths=" + mainOut}
				if changedSource, ok := singleChangedSourceForIncrementalCompile(testSources, testOut); ok {
					err = c.track("kotlincTestsIncremental", func() error {
						incrementalCP := mergePaths([]string{testOut}, testCP)
						return runKotlinc(ctx, toolchain, []string{changedSource}, testOut, incrementalCP, nil, nil, includeAndroid, false, extraArgs, stdout, stderr)
					})
				} else {
					err = c.track("kotlincTests", func() error {
						return runKotlinc(ctx, toolchain, testSources, testOut, testCP, nil, nil, includeAndroid, false, extraArgs, stdout, stderr)
					})
				}
				if err == nil {
					err = c.track("publishCompileTestsCache", func() error {
						testJar, innerErr := classesJarForDir(ctx, testOut, stdout, stderr)
						if innerErr != nil {
							return innerErr
						}
						testJarPath = testJar
						if innerErr := saveSharedCompileCache(testOut, testJar, testSharedCompileDir); innerErr != nil {
							return innerErr
						}
						if innerErr := writeStamp(testCompileStampPath, testSharedCompileDir); innerErr != nil {
							return innerErr
						}
						return writeSemanticSourceFingerprints(testSourceFingerprintPath, testSources)
					})
				}
			}
		}
		endCompileTests()
		if err != nil {
			return err
		}
	} else if !hasOutputFiles(testOut) {
		recordCacheProbe(c.tracker, "compileTests", false, "cache-miss", "compiled test outputs were not available to the test action")
		return fmt.Errorf("compiled unit test outputs for %s %s are missing or stale; run compileDebugUnitTestSources before testDebugUnitTest", mod.Path, variantName)
	} else {
		recordCacheProbe(c.tracker, "compileTests", true, "local-up-to-date", "reused compiled test outputs from prior action")
	}
	if !run {
		_, _ = fmt.Fprintln(stdout, "unit test sources compiled")
		return nil
	}

	var testClasses []string
	err = c.track("discoverJUnitTests", func() error {
		var innerErr error
		testClasses, innerErr = discoverJUnitTestsInRoots(unitTestSourceRoots(mod, variantName), os.Getenv("GOJVM_INCLUDE_AUTOMOBILE") == "")
		return innerErr
	})
	if err != nil {
		return err
	}
	if len(testClasses) == 0 {
		_, _ = fmt.Fprintln(stdout, "compiled tests but found no JUnit test classes")
		return nil
	}
	runtimeSupportCP := runtimeSupportJars()
	junitSupportCP := junitRuntimeSupportJars()
	runSupportCP := mergePaths(junitSupportCP, runtimeSupportCP)
	testRunCachePath, ok := unitTestRunCachePathFromCompileStamp(prj.RootDir, mod.Path, variantName, testClasses, testCompileStampPath, runSupportCP)
	if !ok {
		testRuntimeCP := mergePaths(junitSupportCP, filterJUnitRuntimeSupportJars(append(testCP, testOut)), runtimeSupportCP)
		testRunCachePath = unitTestRunCachePath(prj.RootDir, mod.Path, variantName, testClasses, testRuntimeCP)
	}
	return c.track("runJUnit", func() error {
		if canReuseUnitTestRun(testRunCachePath) {
			recordCacheProbe(c.tracker, "runJUnit", true, "shared-cache-hit", "reused successful unit test result from shared cache")
			return nil
		}
		testRuntimeCP := mergePaths(junitSupportCP, filterJUnitRuntimeSupportJars(append(testCP, testOut)), runtimeSupportCP)
		recordCacheProbe(c.tracker, "runJUnit", false, "cache-miss", "unit tests required fresh JUnit execution")
		if err := runJUnit(ctx, testClasses, testRuntimeCP, stdout, stderr); err != nil {
			return err
		}
		return markUnitTestRunSuccess(testRunCachePath)
	})
}

func dependenciesForVariant(deps *modulebuild.Dependencies, mod *project.Module, variantName string) *modulebuild.Dependencies {
	if deps == nil {
		return &modulebuild.Dependencies{}
	}
	out := &modulebuild.Dependencies{
		Main:                   append([]modulebuild.Ref(nil), deps.Main...),
		Debug:                  append([]modulebuild.Ref(nil), deps.Debug...),
		Test:                   append([]modulebuild.Ref(nil), deps.Test...),
		AndroidTest:            append([]modulebuild.Ref(nil), deps.AndroidTest...),
		CompileOnly:            append([]modulebuild.Ref(nil), deps.CompileOnly...),
		RuntimeOnly:            append([]modulebuild.Ref(nil), deps.RuntimeOnly...),
		TestCompileOnly:        append([]modulebuild.Ref(nil), deps.TestCompileOnly...),
		TestRuntimeOnly:        append([]modulebuild.Ref(nil), deps.TestRuntimeOnly...),
		AndroidTestCompileOnly: append([]modulebuild.Ref(nil), deps.AndroidTestCompileOnly...),
		AndroidTestRuntimeOnly: append([]modulebuild.Ref(nil), deps.AndroidTestRuntimeOnly...),
		CoreLibraryDesugaring:  append([]modulebuild.Ref(nil), deps.CoreLibraryDesugaring...),
	}
	if len(deps.Scoped) == 0 || mod == nil {
		return out
	}
	variant := mod.ResolveVariant(variantName)
	buildType := firstNonEmpty(strings.TrimSpace(variant.Coordinate.BuildType), strings.TrimSpace(variant.Config.BaseBuildType))
	prefixes := variantScopePrefixes(variant.Name, buildType, variant.Coordinate.Flavors)
	out.Main = appendUniqueRefs(out.Main,
		scopedRefsForSuffixes(deps.Scoped, prefixes, "Api", "Implementation")...,
	)
	out.CompileOnly = appendUniqueRefs(out.CompileOnly,
		scopedRefsForSuffixes(deps.Scoped, prefixes, "CompileOnly")...,
	)
	out.RuntimeOnly = appendUniqueRefs(out.RuntimeOnly,
		scopedRefsForSuffixes(deps.Scoped, prefixes, "RuntimeOnly")...,
	)
	out.Test = appendUniqueRefs(out.Test,
		append(append(
			scopedTestRefs(deps.Scoped, prefixes, "Implementation"),
			deps.Scoped["unitTestImplementation"]...,
		), deps.Scoped["testImplementation"]...)...,
	)
	out.TestCompileOnly = appendUniqueRefs(out.TestCompileOnly,
		append(append(
			scopedTestRefs(deps.Scoped, prefixes, "CompileOnly"),
			deps.Scoped["unitTestCompileOnly"]...,
		), deps.Scoped["testCompileOnly"]...)...,
	)
	out.TestRuntimeOnly = appendUniqueRefs(out.TestRuntimeOnly,
		append(append(
			scopedTestRefs(deps.Scoped, prefixes, "RuntimeOnly"),
			deps.Scoped["unitTestRuntimeOnly"]...,
		), deps.Scoped["testRuntimeOnly"]...)...,
	)
	out.AndroidTest = appendUniqueRefs(out.AndroidTest,
		append(append(
			scopedAndroidTestRefs(deps.Scoped, prefixes, "Implementation"),
			deps.Scoped["androidTestImplementation"]...,
		), deps.Scoped["androidTestApi"]...)...,
	)
	out.AndroidTestCompileOnly = appendUniqueRefs(out.AndroidTestCompileOnly,
		append(
			scopedAndroidTestRefs(deps.Scoped, prefixes, "CompileOnly"),
			deps.Scoped["androidTestCompileOnly"]...,
		)...,
	)
	out.AndroidTestRuntimeOnly = appendUniqueRefs(out.AndroidTestRuntimeOnly,
		append(
			scopedAndroidTestRefs(deps.Scoped, prefixes, "RuntimeOnly"),
			deps.Scoped["androidTestRuntimeOnly"]...,
		)...,
	)
	return out
}

func variantScopePrefixes(variantName, buildType string, flavors []string) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, flavor := range flavors {
		add(flavor)
	}
	add(buildType)
	add(variantName)
	return out
}

func scopedRefsForSuffixes(scoped map[string][]modulebuild.Ref, prefixes []string, suffixes ...string) []modulebuild.Ref {
	var out []modulebuild.Ref
	for _, prefix := range prefixes {
		for _, suffix := range suffixes {
			out = append(out, scoped[strings.TrimSpace(prefix)+suffix]...)
		}
	}
	return out
}

func scopedTestRefs(scoped map[string][]modulebuild.Ref, prefixes []string, suffix string) []modulebuild.Ref {
	var out []modulebuild.Ref
	for _, prefix := range prefixes {
		title := strings.ToUpper(prefix[:1]) + prefix[1:]
		out = append(out, scoped[prefix+"Test"+suffix]...)
		out = append(out, scoped[prefix+"UnitTest"+suffix]...)
		out = append(out, scoped["test"+title+suffix]...)
		out = append(out, scoped["unitTest"+title+suffix]...)
	}
	return out
}

func scopedAndroidTestRefs(scoped map[string][]modulebuild.Ref, prefixes []string, suffix string) []modulebuild.Ref {
	var out []modulebuild.Ref
	for _, prefix := range prefixes {
		title := strings.ToUpper(prefix[:1]) + prefix[1:]
		out = append(out, scoped[prefix+"AndroidTest"+suffix]...)
		out = append(out, scoped["androidTest"+title+suffix]...)
	}
	return out
}

func appendUniqueRefs(base []modulebuild.Ref, extra ...modulebuild.Ref) []modulebuild.Ref {
	if len(extra) == 0 {
		return base
	}
	seen := map[modulebuild.Ref]struct{}{}
	out := append([]modulebuild.Ref(nil), base...)
	for _, item := range base {
		seen[item] = struct{}{}
	}
	for _, item := range extra {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func collectUnitTestSources(mod *project.Module, variantName string) ([]string, error) {
	return collectSourcesFromRoots(unitTestSourceRoots(mod, variantName))
}

func singleChangedSourceForIncrementalCompile(sources []string, outDir string) (string, bool) {
	if len(sources) == 0 || !hasOutputFiles(outDir) {
		return "", false
	}
	outputTime := latestInputModTime([]string{outDir})
	if outputTime.IsZero() {
		return "", false
	}
	var changed string
	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil || info.IsDir() {
			return "", false
		}
		if !info.ModTime().After(outputTime) {
			continue
		}
		if changed != "" {
			return "", false
		}
		changed = source
	}
	return changed, changed != ""
}

func collectAndroidTestSources(mod *project.Module, variantName string) ([]string, error) {
	return collectSourcesFromRoots(androidTestSourceRoots(mod, variantName))
}

func collectMainSourcesForVariant(mod *project.Module, variantName string) ([]string, error) {
	return collectSourcesFromRoots(mainSourceRoots(mod, variantName))
}

func mainSourceRoots(mod *project.Module, variantName string) []string {
	if mod == nil {
		return nil
	}
	variant := mod.ResolveVariant(variantName)
	buildType := firstNonEmpty(strings.TrimSpace(variant.Coordinate.BuildType), strings.TrimSpace(variant.Config.BaseBuildType))
	var roots []string
	roots = append(roots, filepath.Join(mod.Dir, "src", "main"))
	if moduleUsesKotlinMultiplatform(mod) {
		roots = append(roots,
			filepath.Join(mod.Dir, "src", "commonMain"),
			filepath.Join(mod.Dir, "src", "androidMain"),
		)
	}
	for _, flavor := range variant.Coordinate.Flavors {
		roots = append(roots, filepath.Join(mod.Dir, "src", flavor))
		if moduleUsesKotlinMultiplatform(mod) {
			roots = append(roots, filepath.Join(mod.Dir, "src", flavor+"Main"))
		}
	}
	if buildType != "" {
		roots = append(roots, filepath.Join(mod.Dir, "src", buildType))
	}
	if variant.Name != "" {
		roots = append(roots, filepath.Join(mod.Dir, "src", variant.Name))
	}
	return uniqueStringPaths(roots)
}

func moduleUsesKotlinMultiplatform(mod *project.Module) bool {
	if mod == nil {
		return false
	}
	for _, plugin := range mod.Plugins {
		switch strings.TrimSpace(plugin) {
		case "org.jetbrains.kotlin.multiplatform", "kotlinMultiplatform":
			return true
		}
	}
	return false
}

func unitTestSourceRoots(mod *project.Module, variantName string) []string {
	if mod == nil {
		return nil
	}
	variant := mod.ResolveVariant(variantName)
	buildType := firstNonEmpty(strings.TrimSpace(variant.Coordinate.BuildType), strings.TrimSpace(variant.Config.BaseBuildType))
	var roots []string
	roots = append(roots, filepath.Join(mod.Dir, "src", "test"))
	for _, flavor := range variant.Coordinate.Flavors {
		roots = append(roots,
			filepath.Join(mod.Dir, "src", flavor+"Test"),
			filepath.Join(mod.Dir, "src", flavor+"UnitTest"),
			filepath.Join(mod.Dir, "src", "test"+taskNameSuffix(flavor)),
		)
	}
	if buildType != "" {
		roots = append(roots,
			filepath.Join(mod.Dir, "src", buildType+"Test"),
			filepath.Join(mod.Dir, "src", buildType+"UnitTest"),
			filepath.Join(mod.Dir, "src", "test"+taskNameSuffix(buildType)),
		)
	}
	if variant.Name != "" {
		roots = append(roots,
			filepath.Join(mod.Dir, "src", variant.Name+"Test"),
			filepath.Join(mod.Dir, "src", variant.Name+"UnitTest"),
			filepath.Join(mod.Dir, "src", "test"+taskNameSuffix(variant.Name)),
		)
	}
	return uniqueStringPaths(roots)
}

func androidTestSourceRoots(mod *project.Module, variantName string) []string {
	if mod == nil {
		return nil
	}
	variant := mod.ResolveVariant(variantName)
	buildType := firstNonEmpty(strings.TrimSpace(variant.Coordinate.BuildType), strings.TrimSpace(variant.Config.BaseBuildType))
	var roots []string
	roots = append(roots, filepath.Join(mod.Dir, "src", "androidTest"))
	for _, flavor := range variant.Coordinate.Flavors {
		roots = append(roots,
			filepath.Join(mod.Dir, "src", flavor+"AndroidTest"),
			filepath.Join(mod.Dir, "src", "androidTest"+taskNameSuffix(flavor)),
		)
	}
	if buildType != "" {
		roots = append(roots,
			filepath.Join(mod.Dir, "src", buildType+"AndroidTest"),
			filepath.Join(mod.Dir, "src", "androidTest"+taskNameSuffix(buildType)),
		)
	}
	if variant.Name != "" {
		roots = append(roots,
			filepath.Join(mod.Dir, "src", variant.Name+"AndroidTest"),
			filepath.Join(mod.Dir, "src", "androidTest"+taskNameSuffix(variant.Name)),
		)
	}
	return uniqueStringPaths(roots)
}

// baselineProfilesForVariant discovers baseline-prof.txt and startup-prof.txt
// files across the variant source roots. These profile files tell ART which
// methods to AOT-compile at install time.
func baselineProfilesForVariant(mod *project.Module, variantName string) []string {
	roots := mainSourceRoots(mod, variantName)
	var out []string
	for _, root := range roots {
		for _, name := range []string{"baseline-prof.txt", "startup-prof.txt"} {
			p := filepath.Join(root, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				out = append(out, p)
			}
		}
	}
	return out
}

func collectSourcesFromRoots(roots []string) ([]string, error) {
	var out []string
	for _, root := range roots {
		sources, err := collectSources(root)
		if err != nil {
			return nil, err
		}
		out = append(out, sources...)
	}
	return uniqueStringPaths(out), nil
}

func discoverJUnitTestsInRoots(roots []string, excludeAutoMobile bool) ([]string, error) {
	var out []string
	for _, root := range roots {
		tests, err := discoverJUnitTestsCached(root, excludeAutoMobile)
		if err != nil {
			return nil, err
		}
		out = append(out, tests...)
	}
	return uniqueStringPaths(out), nil
}

func uniqueStringPaths(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueOrderedPaths(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func taskNameSuffix(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if len(name) == 1 {
		return strings.ToUpper(name)
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
