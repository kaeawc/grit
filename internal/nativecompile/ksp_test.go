package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/gradlecache"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
)

func TestProjectKSPVersionPrefersCatalogKey(t *testing.T) {
	prj := &project.Project{
		VersionCatalogData: map[string]string{
			"build-kotlin-ksp": "2.1.20-1.0.32",
		},
	}
	if got, want := projectKSPVersion(prj), "2.1.20-1.0.32"; got != want {
		t.Fatalf("projectKSPVersion: got %q want %q", got, want)
	}
}

func TestProjectKSPVersionFallsBackThroughKnownKeys(t *testing.T) {
	prj := &project.Project{
		VersionCatalogData: map[string]string{"ksp": "2.0.21-1.0.26"},
	}
	if got, want := projectKSPVersion(prj), "2.0.21-1.0.26"; got != want {
		t.Fatalf("ksp key: got %q want %q", got, want)
	}
}

func TestProjectKSPVersionDoesNotGuessFromGradleCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cached := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1", "com.google.devtools.ksp", "symbol-processing-aa-embeddable", "2.1.20-1.0.32", "sha")
	if err := os.MkdirAll(cached, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectKSPVersion(&project.Project{}); got != "" {
		t.Fatalf("projectKSPVersion should require catalog version, got cached %q", got)
	}
}

func TestResolveKSP2RuntimeIssuesAllThreeRefsAndKeepsNonKSPJars(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedKSPCacheVersion(t, home, "2.1.20-1.0.32")

	const transitive = "/repo/org/jetbrains/kotlinx/kotlinx-coroutines-core-jvm/1.9.0/kotlinx-coroutines-core-jvm-1.9.0.jar"
	resolver := &kspResolverFake{
		results: []*m2local.Resolved{{
			CompileJars: []string{
				"/repo/com/google/devtools/ksp/symbol-processing-aa-embeddable/2.1.20-1.0.32/symbol-processing-aa-embeddable-2.1.20-1.0.32.jar",
			},
			RuntimeJars: []string{transitive},
		}},
	}
	state := compileStateWithResolver(resolver)
	got, err := resolveKSP2Runtime(state, &project.Project{Name: "app"}, "2.1.20-1.0.32")
	if err != nil {
		t.Fatal(err)
	}
	hasTransitive := false
	for _, p := range got {
		if p == transitive {
			hasTransitive = true
		}
	}
	if !hasTransitive {
		t.Fatalf("non-KSP transitive should survive stripKSPRuntimeJars, got %#v", got)
	}
	calls := resolver.callsSnapshot()
	if len(calls) != 1 {
		t.Fatalf("expected one resolver call, got %d", len(calls))
	}
	want := []string{
		"com.google.devtools.ksp:symbol-processing-aa-embeddable:2.1.20-1.0.32",
		"com.google.devtools.ksp:symbol-processing-api:2.1.20-1.0.32",
		"com.google.devtools.ksp:symbol-processing-common-deps:2.1.20-1.0.32",
	}
	assertMainRefs(t, calls[0], want)
}

func TestResolveKSP2RuntimeBackfillsFromGradleCacheWhenResolverSubstitutesVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Cache has 2.1.20-1.0.32 fully covered; the resolver intentionally
	// returns nothing here so the backfill path is exercised.
	seedKSPCacheVersion(t, home, "2.1.20-1.0.32")

	state := compileStateWithResolver(&kspResolverFake{results: []*m2local.Resolved{{}}})
	got, err := resolveKSP2Runtime(state, &project.Project{Name: "app"}, "2.1.20-1.0.32")
	if err != nil {
		t.Fatalf("expected coherent cache to satisfy ksp2 runtime, got %v", err)
	}
	required := []string{"symbol-processing-aa-embeddable", "symbol-processing-api", "symbol-processing-common-deps"}
	for _, module := range required {
		if !kspRuntimeContainsModuleForTest(got, module) {
			t.Fatalf("expected %s in returned classpath, got %#v", module, got)
		}
	}
}

func TestResolveKSP2RuntimeDowngradesToCoherentVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Requested version is missing aa-embeddable; an older version
	// covers all three modules and should be picked over a newer version
	// (downgrading is safer because processor jars compiled for older KSP
	// releases routinely break against a newer runtime).
	seedKSPCacheModuleVersions(t, home, "symbol-processing-aa-embeddable", []string{"2.3.4", "2.3.7"})
	seedKSPCacheModuleVersions(t, home, "symbol-processing-api", []string{"2.3.4", "2.3.6", "2.3.7"})
	seedKSPCacheModuleVersions(t, home, "symbol-processing-common-deps", []string{"2.3.4", "2.3.6", "2.3.7"})

	state := compileStateWithResolver(&kspResolverFake{results: []*m2local.Resolved{{}}})
	got, err := resolveKSP2Runtime(state, &project.Project{Name: "app"}, "2.3.6")
	if err != nil {
		t.Fatalf("expected downgrade to coherent version, got %v", err)
	}
	for _, path := range got {
		if strings.Contains(path, "/2.3.6/") || strings.Contains(path, "/2.3.7/") {
			t.Fatalf("expected 2.3.4 (coherent ≤ requested), got %s", path)
		}
	}
	for _, module := range []string{"symbol-processing-aa-embeddable", "symbol-processing-api", "symbol-processing-common-deps"} {
		if !kspRuntimeContainsModuleForTest(got, module) {
			t.Fatalf("missing %s in coherent set: %#v", module, got)
		}
	}
}

func TestResolveKSP2RuntimeFailsWhenNoCoherentCoverage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Only aa-embeddable is cached — no version covers the full triple.
	seedKSPCacheModuleVersions(t, home, "symbol-processing-aa-embeddable", []string{"2.1.20-1.0.32"})

	state := compileStateWithResolver(&kspResolverFake{results: []*m2local.Resolved{{}}})
	if got, err := resolveKSP2Runtime(state, &project.Project{Name: "app"}, "2.1.20-1.0.32"); err == nil {
		t.Fatalf("expected error when cache has no coherent set, got %v", got)
	}
}

func TestPickCoherentKSPVersionPrefersRequestedThenDowngrades(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedKSPCacheVersion(t, home, "2.3.4")
	seedKSPCacheModuleVersions(t, home, "symbol-processing-api", []string{"2.3.6"})

	probe := gradlecache.DefaultProbe()
	if got := pickCoherentKSPVersion(probe, "2.3.4"); got != "2.3.4" {
		t.Fatalf("requested version should win when covered: got %q", got)
	}
	if got := pickCoherentKSPVersion(probe, "2.3.6"); got != "2.3.4" {
		t.Fatalf("expected 2.3.4 downgrade (only coherent ≤ 2.3.6), got %q", got)
	}
}

func seedKSPCacheVersion(t *testing.T, home, version string) {
	t.Helper()
	for _, module := range []string{
		"symbol-processing-aa-embeddable",
		"symbol-processing-api",
		"symbol-processing-common-deps",
	} {
		seedKSPCacheModuleVersions(t, home, module, []string{version})
	}
}

func seedKSPCacheModuleVersions(t *testing.T, home, module string, versions []string) {
	t.Helper()
	for _, v := range versions {
		dir := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1", "com.google.devtools.ksp", module, v, "sha")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, module+"-"+v+".jar"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func kspRuntimeContainsModuleForTest(paths []string, module string) bool {
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasPrefix(base, module+"-") || strings.HasPrefix(base, module+"-jvm-") {
			return true
		}
	}
	return false
}

func TestResolveKSPProcessorsUsesResolverJVMFallback(t *testing.T) {
	resolver := &kspResolverFake{
		results: []*m2local.Resolved{
			{},
			{RuntimeJars: []string{"/repo/com/example/proc-jvm/1.0/proc-jvm-1.0.jar"}},
		},
	}
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "libs.versions.toml")
	if err := os.WriteFile(catalogPath, []byte(`
[versions]
proc = "1.0"

[libraries]
processor = { module = "com.example:proc", version.ref = "proc" }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	prj := &project.Project{
		VersionCatalogs: []string{catalogPath},
	}
	got, err := resolveKSPProcessors(compileStateWithResolver(resolver), prj, []modulebuild.Ref{{Kind: "library", Value: "processor"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "/repo/com/example/proc-jvm/1.0/proc-jvm-1.0.jar" {
		t.Fatalf("unexpected processor jars: %v", got)
	}
	calls := resolver.callsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("expected direct and JVM fallback resolver calls, got %d", len(calls))
	}
	assertMainRefs(t, calls[1], []string{"com.example:proc-jvm:1.0"})
}

func TestKSPLanguageVersionDropsPatch(t *testing.T) {
	cases := map[string]string{
		"2.1.20":  "2.1",
		"2.0.21":  "2.0",
		"1.9.24":  "1.9",
		"2":       "",
		"":        "",
		"invalid": "",
	}
	for in, want := range cases {
		if got := kspLanguageVersion(in); got != want {
			t.Fatalf("kspLanguageVersion(%q) = %q want %q", in, got, want)
		}
	}
}

func TestKSPProcessorOptionsArgFormat(t *testing.T) {
	got := kspProcessorOptionsArg(map[string]string{
		"room.schemaLocation": "/schemas",
		"dagger.fastInit":     "enabled",
	})
	sep := string(os.PathListSeparator)
	want := "dagger.fastInit=enabled" + sep + "room.schemaLocation=/schemas"
	if got != want {
		t.Fatalf("processor options arg:\n got  %q\n want %q", got, want)
	}
}

func TestKSPProcessorOptionsArgEmpty(t *testing.T) {
	if got := kspProcessorOptionsArg(nil); got != "" {
		t.Fatalf("nil opts should produce empty arg, got %q", got)
	}
}

func TestKSP2ArgsContainsRequiredFlags(t *testing.T) {
	args := ksp2Args(
		"glide-config",
		"/proj/glide-config",
		"/out/classes",
		"/out/ksp/kotlin",
		"/out/ksp/java",
		"/out/ksp/resources",
		"/out/ksp/caches",
		"/out/ksp",
		"/proj/glide-config/src/main",
		"/m2/glide.jar:/m2/android.jar",
		"2.1",
		"21",
		"room.schemaLocation=/schemas",
		[]string{"/m2/glide-ksp.jar", "/m2/kotlinpoet.jar"},
	)
	required := []string{
		"-module-name=glide-config",
		"-source-roots=/proj/glide-config/src/main",
		"-project-base-dir=/proj/glide-config",
		"-output-base-dir=/out/ksp",
		"-caches-dir=/out/ksp/caches",
		"-class-output-dir=/out/classes",
		"-kotlin-output-dir=/out/ksp/kotlin",
		"-java-output-dir=/out/ksp/java",
		"-resource-output-dir=/out/ksp/resources",
		"-jvm-target=21",
		"-language-version=2.1",
		"-api-version=2.1",
		"-libraries=/m2/glide.jar:/m2/android.jar",
		"-processor-options=room.schemaLocation=/schemas",
	}
	joined := strings.Join(args, "\n")
	for _, want := range required {
		if !strings.Contains(joined, want) {
			t.Fatalf("ksp2 args missing %q in:\n%s", want, joined)
		}
	}
	// Processor jars are positional and must trail any flag args.
	last := args[len(args)-1]
	if last != "/m2/kotlinpoet.jar" {
		t.Fatalf("expected last positional jar /m2/kotlinpoet.jar, got %q", last)
	}
}

func TestKSP2ArgsOmitsEmptyOptionalFlags(t *testing.T) {
	args := ksp2Args("m", "/m", "/c", "/k", "/j", "/r", "/cs", "/o", "/m/src", "", "2.0", "21", "", []string{"/p.jar"})
	for _, arg := range args {
		if strings.HasPrefix(arg, "-libraries=") || strings.HasPrefix(arg, "-processor-options=") {
			t.Fatalf("expected empty optional flags omitted, got %q", arg)
		}
	}
}

func TestKSP2ModuleNameStable(t *testing.T) {
	cases := []struct {
		path    string
		variant string
		want    string
	}{
		{":glide-config", "debug", "glide-config-debug"},
		{":app", "playProdDebug", "app-playProdDebug"},
		{":nested:lib", "debug", "nested-lib-debug"},
		{":app", "", "app"},
	}
	for _, tc := range cases {
		got := ksp2ModuleName(&project.Module{Path: tc.path}, tc.variant)
		if got != tc.want {
			t.Fatalf("ksp2ModuleName(%q, %q) = %q want %q", tc.path, tc.variant, got, tc.want)
		}
	}
}

func TestKSPOutputRootsLayout(t *testing.T) {
	prj := &project.Project{RootDir: "/proj"}
	mod := &project.Module{Path: ":glide-config", Dir: "/proj/glide-config"}
	classOut := "/proj/build/grit/glide-config/debug/classes"
	root, kotlin, java, resources, classes, caches := kspOutputRoots(prj, mod, "debug", classOut)
	wantRoot := filepath.Join("/proj", "build", "grit", "glide-config", "debug", "ksp")
	if root != wantRoot {
		t.Fatalf("root: got %q want %q", root, wantRoot)
	}
	if kotlin != filepath.Join(wantRoot, "kotlin") {
		t.Fatalf("kotlin dir: got %q", kotlin)
	}
	if java != filepath.Join(wantRoot, "java") {
		t.Fatalf("java dir: got %q", java)
	}
	if resources != filepath.Join(wantRoot, "resources") {
		t.Fatalf("resources dir: got %q", resources)
	}
	if caches != filepath.Join(wantRoot, "caches") {
		t.Fatalf("caches dir: got %q", caches)
	}
	if classes != classOut {
		t.Fatalf("class dir should pass through: got %q want %q", classes, classOut)
	}
}

func TestKSPHashTokensDeterministic(t *testing.T) {
	refs := []modulebuild.Ref{
		{Kind: "raw", Value: "com.google.dagger:hilt-compiler:2.51"},
		{Kind: "library", Value: "glide.ksp"},
	}
	opts := map[string]string{
		"room.schemaLocation": "/schemas",
		"dagger.fastInit":     "enabled",
	}
	a := kspHashTokens("2.1.20-1.0.32", refs, opts)
	b := kspHashTokens("2.1.20-1.0.32", refs, opts)
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("non-deterministic hash tokens:\n%v\nvs\n%v", a, b)
	}
	c := kspHashTokens("2.1.20-1.0.33", refs, opts)
	if strings.Join(a, "\n") == strings.Join(c, "\n") {
		t.Fatalf("hash tokens should differ when version changes")
	}
}

func TestKSPHashTokensSortedRegardlessOfInputOrder(t *testing.T) {
	a := kspHashTokens("v1",
		[]modulebuild.Ref{{Kind: "raw", Value: "a:b:1"}, {Kind: "raw", Value: "a:c:1"}},
		nil,
	)
	b := kspHashTokens("v1",
		[]modulebuild.Ref{{Kind: "raw", Value: "a:c:1"}, {Kind: "raw", Value: "a:b:1"}},
		nil,
	)
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("tokens should be order-independent:\n%v\nvs\n%v", a, b)
	}
}

func TestKSPHashTokensEmpty(t *testing.T) {
	if got := kspHashTokens("", nil, nil); got != nil {
		t.Fatalf("empty input should produce nil tokens, got %v", got)
	}
}

func TestCollectGeneratedKotlinSources(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A.kt", "B.kt", "ignored.txt"} {
		if err := os.WriteFile(filepath.Join(subdir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := collectGeneratedKotlinSources(dir)
	if len(got) != 2 {
		t.Fatalf("expected 2 .kt files, got %d (%v)", len(got), got)
	}
	if !strings.HasSuffix(got[0], "A.kt") || !strings.HasSuffix(got[1], "B.kt") {
		t.Fatalf("unexpected order: %v", got)
	}
}

func TestCollectGeneratedKotlinSourcesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if got := collectGeneratedKotlinSources(dir); len(got) != 0 {
		t.Fatalf("empty dir should yield nil, got %v", got)
	}
	if got := collectGeneratedKotlinSources(""); len(got) != 0 {
		t.Fatalf("empty path should yield nil, got %v", got)
	}
}

func TestResolveClasspathRefsUsesDependencyResolver(t *testing.T) {
	resolver := &kspResolverFake{result: &m2local.Resolved{
		CompileJars: []string{"/deps/compile.jar"},
		RuntimeJars: []string{"/deps/runtime.jar"},
		AndroidLibraries: []m2local.AndroidLibrary{
			{ClassesJar: "/deps/aar-classes.jar"},
		},
	}}

	got := resolveClasspathRefs(resolver, []modulebuild.Ref{{Kind: "raw", Value: "com.example:processor:1.0"}}, true)
	want := []string{"/deps/compile.jar", "/deps/runtime.jar", "/deps/aar-classes.jar"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resolveClasspathRefs:\n got  %v\n want %v", got, want)
	}
	calls := resolver.calls
	if len(calls) != 1 || len(calls[0].Main) != 1 || calls[0].Main[0].Value != "com.example:processor:1.0" {
		t.Fatalf("resolver calls: %#v", calls)
	}
}

func TestFallbackJVMCompileJarsRoutesRawCoordinatesThroughResolver(t *testing.T) {
	resolver := &kspResolverFake{result: &m2local.Resolved{CompileJars: []string{"/deps/lib.jar"}}}

	got := fallbackJVMCompileJars(&project.Project{}, resolver, []modulebuild.Ref{{Kind: "raw", Value: "com.example:lib:1.0"}})
	if strings.Join(got, "\n") != "/deps/lib.jar" {
		t.Fatalf("fallbackJVMCompileJars: got %v", got)
	}
	calls := resolver.calls
	if len(calls) != 1 || len(calls[0].Main) != 1 || calls[0].Main[0].Value != "com.example:lib:1.0" {
		t.Fatalf("resolver calls: %#v", calls)
	}
}

type kspResolverFake struct {
	calls   []modulebuild.Dependencies
	result  *m2local.Resolved
	results []*m2local.Resolved
	err     error
	tracker perf.Tracker
}

func (f *kspResolverFake) Resolve(deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	if deps != nil {
		f.calls = append(f.calls, modulebuild.Dependencies{Main: append([]modulebuild.Ref{}, deps.Main...)})
	}
	if f.err != nil {
		return nil, f.err
	}
	if len(f.results) > 0 {
		out := f.results[0]
		f.results = f.results[1:]
		if out == nil {
			return &m2local.Resolved{}, nil
		}
		return out, nil
	}
	if f.result == nil {
		return &m2local.Resolved{}, nil
	}
	return f.result, nil
}

func (f *kspResolverFake) SetTracker(tracker perf.Tracker) {
	f.tracker = tracker
}

func (f *kspResolverFake) Topology() m2local.CacheTopology {
	return m2local.CacheTopology{}
}

func (f *kspResolverFake) callsSnapshot() []modulebuild.Dependencies {
	out := make([]modulebuild.Dependencies, len(f.calls))
	copy(out, f.calls)
	return out
}

func compileStateWithResolver(resolver *kspResolverFake) *compileState {
	state := newCompileState()
	state.resolverOnce.Do(func() {
		state.resolver = resolver
	})
	return state
}

func assertMainRefs(t *testing.T, deps modulebuild.Dependencies, want []string) {
	t.Helper()
	if len(deps.Main) != len(want) {
		t.Fatalf("main refs length: got %d (%v), want %d (%v)", len(deps.Main), deps.Main, len(want), want)
	}
	for i, ref := range deps.Main {
		if ref.Kind != "raw" || ref.Value != want[i] {
			t.Fatalf("main ref %d: got %#v want raw %q", i, ref, want[i])
		}
	}
}
