package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	calls  []modulebuild.Dependencies
	result *m2local.Resolved
	err    error
}

func (f *kspResolverFake) Resolve(deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	if deps != nil {
		f.calls = append(f.calls, modulebuild.Dependencies{Main: append([]modulebuild.Ref{}, deps.Main...)})
	}
	return f.result, f.err
}

func (f *kspResolverFake) SetTracker(perf.Tracker) {}

func (f *kspResolverFake) Topology() m2local.CacheTopology {
	return m2local.CacheTopology{}
}
