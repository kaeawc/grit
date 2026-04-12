package nativecompile

import (
	"os"
	"reflect"
	"strings"
	"testing"

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

func TestCompilerPluginsForModuleUsesActiveVariantPlugins(t *testing.T) {
	reg := modulebuild.NewPluginRegistry()
	reg.Register(modulebuild.CompilerPlugin{
		ID:       modulebuild.ComposeCompilerPluginID,
		Variants: []string{"debug"},
	})
	reg.Register(modulebuild.CompilerPlugin{
		ID:       modulebuild.KotlinSerializationCompilerPluginID,
		Variants: []string{"release"},
	})
	reg.Register(modulebuild.CompilerPlugin{
		ID:        "com.example.custom",
		Classpath: []string{"/plugins/custom-one.jar", "/plugins/custom-two.jar"},
		Variants:  []string{"debug"},
	})
	mod := &project.Module{CompilerPlugins: reg}
	toolchain := &kotlinToolchain{
		ComposePlugin:       "/plugins/compose.jar",
		SerializationPlugin: "/plugins/serialization.jar",
	}

	debugPlugins := compilerPluginsForModule(mod, "debug", toolchain)
	debugWant := []string{"/plugins/compose.jar", "/plugins/custom-one.jar", "/plugins/custom-two.jar"}
	if !reflect.DeepEqual(debugPlugins, debugWant) {
		t.Fatalf("unexpected debug plugins: got %#v want %#v", debugPlugins, debugWant)
	}

	releasePlugins := compilerPluginsForModule(mod, "release", toolchain)
	releaseWant := []string{"/plugins/serialization.jar"}
	if !reflect.DeepEqual(releasePlugins, releaseWant) {
		t.Fatalf("unexpected release plugins: got %#v want %#v", releasePlugins, releaseWant)
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
	}

	got := compilerPluginsForModule(mod, "debug", toolchain)
	want := []string{
		"/plugins/compose.jar",
		"/plugins/serialization.jar",
		"/home/test/.gradle/caches/modules-2/files-2.1/dev.zacsweers.metro/compiler/0.12.0/898e83c86c03300a76d55f83815ce13a1d1fc005/compiler-0.12.0.jar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected legacy plugin fallback: got %#v want %#v", got, want)
	}

	if sep := string(os.PathSeparator); !strings.Contains(got[2], sep+".gradle"+sep) {
		t.Fatalf("expected metro plugin path to use filepath.Join semantics, got %q", got[2])
	}
}
