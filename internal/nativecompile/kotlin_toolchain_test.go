package nativecompile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/gradlecache"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func seedCacheJar(t *testing.T, home, group, module, version, hash, filename string) string {
	t.Helper()
	dir := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1", group, module, version, hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte("seeded"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKotlinStdlibJarsForVersionReadsCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stdlibJar := seedCacheJar(t, home, "org.jetbrains.kotlin", "kotlin-stdlib", "2.3.20", "abc", "kotlin-stdlib-2.3.20.jar")
	jdk7Jar := seedCacheJar(t, home, "org.jetbrains.kotlin", "kotlin-stdlib-jdk7", "2.3.20", "def", "kotlin-stdlib-jdk7-2.3.20.jar")
	jdk8Jar := seedCacheJar(t, home, "org.jetbrains.kotlin", "kotlin-stdlib-jdk8", "2.3.20", "ghi", "kotlin-stdlib-jdk8-2.3.20.jar")

	probe := gradlecache.DefaultProbe()
	got := kotlinStdlibJarsForVersion(probe, "2.3.20")
	want := []string{stdlibJar, jdk7Jar, jdk8Jar}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected stdlib jars\ngot  %#v\nwant %#v", got, want)
	}

	if jars := kotlinStdlibJarsForVersion(probe, ""); jars != nil {
		t.Fatalf("expected nil for empty version, got %#v", jars)
	}
}

func TestAnnotationsVersionForStdlibReadsModuleMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	moduleDir := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1", "org.jetbrains.kotlin", "kotlin-stdlib", "2.3.20", "hash")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"variants":[{"attributes":{"org.gradle.usage":"java-runtime"},"dependencies":[{"group":"org.jetbrains","module":"annotations","version":{"requires":"13.0"}}]}]}`
	if err := os.WriteFile(filepath.Join(moduleDir, "kotlin-stdlib-2.3.20.module"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := annotationsVersionForStdlib(gradlecache.DefaultProbe(), "2.3.20"); got != "13.0" {
		t.Fatalf("expected annotations 13.0, got %q", got)
	}
}

func TestJetbrainsAnnotationsJarsPrefersStdlibTransitiveVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	moduleDir := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1", "org.jetbrains.kotlin", "kotlin-stdlib", "2.3.20", "hash")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"variants":[{"attributes":{"org.gradle.usage":"java-runtime"},"dependencies":[{"group":"org.jetbrains","module":"annotations","version":{"requires":"13.0"}}]}]}`
	if err := os.WriteFile(filepath.Join(moduleDir, "kotlin-stdlib-2.3.20.module"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	wantJar := seedCacheJar(t, home, "org.jetbrains", "annotations", "13.0", "h1", "annotations-13.0.jar")
	seedCacheJar(t, home, "org.jetbrains", "annotations", "26.0.2", "h2", "annotations-26.0.2.jar")

	got := jetbrainsAnnotationsJars(gradlecache.DefaultProbe(), "2.3.20")
	if len(got) != 1 || got[0] != wantJar {
		t.Fatalf("expected stdlib's declared annotations version, got %#v", got)
	}
}

func TestJetbrainsAnnotationsJarsFallsBackToLatestCached(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wantJar := seedCacheJar(t, home, "org.jetbrains", "annotations", "26.0.2", "h", "annotations-26.0.2.jar")

	got := jetbrainsAnnotationsJars(gradlecache.DefaultProbe(), "2.3.20")
	if len(got) != 1 || got[0] != wantJar {
		t.Fatalf("expected fallback to latest cached annotations, got %#v", got)
	}
}

func TestJarsOrCachedPrefersResolvedThenFallsBackToCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	probe := gradlecache.DefaultProbe()
	resolved := []string{"/already/resolved.jar"}
	if got := jarsOrCached(probe, resolved, "org.example", "lib", "1.0"); !reflect.DeepEqual(got, resolved) {
		t.Fatalf("expected resolved jars to pass through, got %#v", got)
	}

	wantJar := seedCacheJar(t, home, "org.example", "lib", "1.0", "h", "lib-1.0.jar")
	got := jarsOrCached(probe, nil, "org.example", "lib", "1.0")
	if len(got) != 1 || got[0] != wantJar {
		t.Fatalf("expected cache fallback to surface %q, got %#v", wantJar, got)
	}
}

func TestKotlinToolchainValidateRequiresExplicitCompilerArtifacts(t *testing.T) {
	toolchain := &kotlinToolchain{
		Version: "2.3.3",
		CompilerClasspath: []string{
			"/tmp/kotlin-compiler-embeddable-2.3.3.jar",
			"/tmp/kotlin-stdlib-2.3.3.jar",
		},
	}
	err := toolchain.validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "kotlin-script-runtime:2.3.3") || !strings.Contains(msg, "org.jetbrains:annotations") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestKotlinToolchainValidateAcceptsExplicitCompilerClasspath(t *testing.T) {
	toolchain := &kotlinToolchain{
		Version: "2.3.3",
		CompilerClasspath: []string{
			"/tmp/kotlin-compiler-embeddable-2.3.3.jar",
			"/tmp/kotlin-stdlib-2.3.3.jar",
			"/tmp/kotlin-script-runtime-2.3.3.jar",
			"/tmp/annotations-13.0.jar",
		},
	}
	if err := toolchain.validate(); err != nil {
		t.Fatalf("expected valid toolchain, got %v", err)
	}
}

func TestFilterKotlinCompilerClasspathVersion(t *testing.T) {
	paths := []string{
		"/cache/org.jetbrains.kotlin/kotlin-compiler-embeddable/2.3.0/hash/kotlin-compiler-embeddable-2.3.0.jar",
		"/cache/org.jetbrains.kotlin/kotlin-compiler-embeddable/2.3.20/hash/kotlin-compiler-embeddable-2.3.20.jar",
		"/cache/org.jetbrains.kotlin/kotlin-stdlib/2.3.20/hash/kotlin-stdlib-2.3.20.jar",
		"/cache/org.jetbrains/annotations/24.0.0/hash/annotations-24.0.0.jar",
	}
	got := filterKotlinCompilerClasspathVersion("2.3.0", paths)
	want := []string{
		"/cache/org.jetbrains.kotlin/kotlin-compiler-embeddable/2.3.0/hash/kotlin-compiler-embeddable-2.3.0.jar",
		"/cache/org.jetbrains.kotlin/kotlin-stdlib/2.3.20/hash/kotlin-stdlib-2.3.20.jar",
		"/cache/org.jetbrains/annotations/24.0.0/hash/annotations-24.0.0.jar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected filtered classpath: got %#v want %#v", got, want)
	}
}

func TestKotlinToolDependencySetDeclaresWholeToolchain(t *testing.T) {
	got := kotlinToolDependencySet("2.3.3")
	want := []dependencywiring.ToolDependency{
		{Group: "org.jetbrains.kotlin", Module: "kotlin-compiler-embeddable", Version: "2.3.3", Role: "compiler"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-stdlib", Version: "2.3.3", Role: "runtime"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-stdlib-jdk7", Version: "2.3.3", Role: "runtime-jdk7"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-stdlib-jdk8", Version: "2.3.3", Role: "runtime-jdk8"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-test", Version: "2.3.3", Role: "test-runtime"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-test-junit", Version: "2.3.3", Role: "test-junit"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-test-junit5", Version: "2.3.3", Role: "test-junit5"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-reflect", Version: "2.3.3", Role: "reflect"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-script-runtime", Version: "2.3.3", Role: "script-runtime"},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-compose-compiler-plugin-embeddable", Version: "2.3.3", Role: "compose-plugin", Optional: true},
		{Group: "org.jetbrains.kotlin", Module: "kotlin-serialization-compiler-plugin-embeddable", Version: "2.3.3", Role: "serialization-plugin", Optional: true},
	}
	if !reflect.DeepEqual(got.Dependencies, want) {
		t.Fatalf("unexpected Kotlin tool dependencies: got %#v want %#v", got.Dependencies, want)
	}
}

func TestCompilerPluginsForModuleUsesActiveVariantPlugins(t *testing.T) {
	reg := modulebuild.NewPluginRegistry()
	reg.Register(modulebuild.CompilerPlugin{
		ID:       modulebuild.ComposeCompilerPluginID,
		Options:  map[string]string{"suppressKotlinVersionCompatibilityCheck": "true"},
		Variants: []string{"debug"},
	})
	reg.Register(modulebuild.CompilerPlugin{
		ID:       modulebuild.KotlinSerializationCompilerPluginID,
		Variants: []string{"release"},
	})
	reg.Register(modulebuild.CompilerPlugin{
		ID:        "com.example.custom",
		Classpath: []string{"/plugins/custom-one.jar", "/plugins/custom-two.jar"},
		Options:   map[string]string{"mode": "strict", "debug": "true"},
		Variants:  []string{"debug"},
	})
	mod := &project.Module{CompilerPlugins: reg}
	toolchain := &kotlinToolchain{
		ComposePlugin:       "/plugins/compose.jar",
		SerializationPlugin: "/plugins/serialization.jar",
	}

	debugPlugins, debugOptions := compilerPluginsForModule(mod, "debug", toolchain)
	debugWant := []string{"/plugins/compose.jar", "/plugins/custom-one.jar", "/plugins/custom-two.jar"}
	if !reflect.DeepEqual(debugPlugins, debugWant) {
		t.Fatalf("unexpected debug plugins: got %#v want %#v", debugPlugins, debugWant)
	}
	debugOptionsWant := []string{
		"plugin:androidx.compose.compiler.plugins.kotlin:suppressKotlinVersionCompatibilityCheck=true",
		"plugin:com.example.custom:debug=true",
		"plugin:com.example.custom:mode=strict",
	}
	if !reflect.DeepEqual(debugOptions, debugOptionsWant) {
		t.Fatalf("unexpected debug plugin options: got %#v want %#v", debugOptions, debugOptionsWant)
	}

	releasePlugins, releaseOptions := compilerPluginsForModule(mod, "release", toolchain)
	releaseWant := []string{"/plugins/serialization.jar"}
	if !reflect.DeepEqual(releasePlugins, releaseWant) {
		t.Fatalf("unexpected release plugins: got %#v want %#v", releasePlugins, releaseWant)
	}
	if len(releaseOptions) != 0 {
		t.Fatalf("expected no release plugin options, got %#v", releaseOptions)
	}
}

func TestCompilerPluginsForModuleFallsBackToLegacyModuleFlags(t *testing.T) {
	t.Setenv("HOME", "/home/test")

	mod := &project.Module{
		UsesCompose:             true,
		UsesKotlinSerialization: true,
		UsesMetro:               true,
	}
	toolchain := &kotlinToolchain{
		ComposePlugin:       "/plugins/compose.jar",
		SerializationPlugin: "/plugins/serialization.jar",
		MetroPlugin:         "/plugins/metro.jar",
	}

	got, options := compilerPluginsForModule(mod, "debug", toolchain)
	want := []string{
		"/plugins/compose.jar",
		"/plugins/serialization.jar",
		"/plugins/metro.jar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected legacy plugin fallback: got %#v want %#v", got, want)
	}
	if len(options) != 0 {
		t.Fatalf("expected no legacy plugin options, got %#v", options)
	}

}

func TestKotlinCompilerPluginJarPrefersExactVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wantJar := seedCacheJar(t, home, "org.jetbrains.kotlin", "kotlin-compose-compiler-plugin-embeddable", "2.3.20", "abc", "kotlin-compose-compiler-plugin-embeddable-2.3.20.jar")
	seedCacheJar(t, home, "org.jetbrains.kotlin", "kotlin-compose-compiler-plugin-embeddable", "2.3.21", "def", "kotlin-compose-compiler-plugin-embeddable-2.3.21.jar")

	got := kotlinCompilerPluginJar(gradlecache.DefaultProbe(), "kotlin-compose-compiler-plugin-embeddable", "2.3.20")
	if got != wantJar {
		t.Fatalf("expected exact-version match\n got: %q\nwant: %q", got, wantJar)
	}
}

func TestKotlinCompilerPluginJarFallsBackToLatestCached(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wantJar := seedCacheJar(t, home, "org.jetbrains.kotlin", "kotlin-compose-compiler-plugin-embeddable", "2.3.21", "abc", "kotlin-compose-compiler-plugin-embeddable-2.3.21.jar")

	got := kotlinCompilerPluginJar(gradlecache.DefaultProbe(), "kotlin-compose-compiler-plugin-embeddable", "2.3.20")
	if got != wantJar {
		t.Fatalf("expected fallback to latest cached version\n got: %q\nwant: %q", got, wantJar)
	}
}

func TestKotlinCompilerPluginJarReturnsEmptyWhenAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	probe := gradlecache.DefaultProbe()
	if got := kotlinCompilerPluginJar(probe, "kotlin-compose-compiler-plugin-embeddable", "2.3.20"); got != "" {
		t.Fatalf("expected empty when no cached plugin jar exists, got %q", got)
	}
	if got := kotlinCompilerPluginJar(probe, "kotlin-serialization-compiler-plugin-embeddable", "2.3.20"); got != "" {
		t.Fatalf("expected empty for serialization plugin too, got %q", got)
	}
}
