package nativecompile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

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
