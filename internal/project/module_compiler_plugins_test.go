package project

import (
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/testutil"
)

func TestLoadModulePopulatesCompilerPlugins(t *testing.T) {
	root := t.TempDir()
	prj := &Project{RootDir: root}
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
plugins {
  alias(libs.plugins.android.application)
  alias(libs.plugins.kotlin.serialization)
  alias(libs.plugins.metro)
}

android {
  namespace = "dev.example"
  buildFeatures {
    compose = true
  }
}
`)

	mod, err := loadModule(prj, ":app")
	if err != nil {
		t.Fatalf("loadModule returned error: %v", err)
	}
	if mod.CompilerPlugins == nil {
		t.Fatal("expected compiler plugin registry to be populated")
	}

	debugPlugins := mod.ActiveCompilerPlugins("debug")
	if got, want := len(debugPlugins), 3; got != want {
		t.Fatalf("unexpected active plugin count: got %d want %d (%#v)", got, want, debugPlugins)
	}
	wantIDs := []string{
		modulebuild.ComposeCompilerPluginID,
		modulebuild.KotlinSerializationCompilerPluginID,
		modulebuild.MetroCompilerPluginID,
	}
	for i, wantID := range wantIDs {
		if debugPlugins[i].ID != wantID {
			t.Fatalf("unexpected plugin at index %d: got %q want %q", i, debugPlugins[i].ID, wantID)
		}
	}

	releasePlugins := mod.ActiveCompilerPlugins("release")
	if got, want := len(releasePlugins), len(debugPlugins); got != want {
		t.Fatalf("unexpected release plugin count: got %d want %d", got, want)
	}
}

func TestLoadModuleParsesCustomCompilerPlugins(t *testing.T) {
	root := t.TempDir()
	prj := &Project{RootDir: root}
	testutil.WriteFile(t, root, "lib/build.gradle.kts", `
plugins {
  kotlin("jvm")
}

grit {
  compilerPlugins {
    create("wire") {
      id = "com.example.wire"
      classpath = listOf("plugins/wire.jar")
      options = mapOf(
        "mode" to "strict",
      )
      option("debug", "true")
      variants = listOf("debug")
    }

    create("global") {
      classpath += "plugins/global.jar"
      options["enabled"] = "true"
    }
  }
}
`)

	mod, err := loadModule(prj, ":lib")
	if err != nil {
		t.Fatalf("loadModule returned error: %v", err)
	}
	if mod.CompilerPlugins == nil {
		t.Fatal("expected compiler plugin registry to be populated")
	}

	debugPlugins := mod.ActiveCompilerPlugins("debug")
	if got, want := len(debugPlugins), 2; got != want {
		t.Fatalf("unexpected debug plugin count: got %d want %d (%#v)", got, want, debugPlugins)
	}
	if got, want := debugPlugins[0].ID, "com.example.wire"; got != want {
		t.Fatalf("unexpected scoped plugin id: got %q want %q", got, want)
	}
	if got, want := debugPlugins[0].Classpath, []string{filepath.Join(root, "lib", "plugins", "wire.jar")}; !sameStrings(got, want) {
		t.Fatalf("unexpected scoped plugin classpath: got %#v want %#v", got, want)
	}
	if got := debugPlugins[0].Options["mode"]; got != "strict" {
		t.Fatalf("expected parsed map option, got %q", got)
	}
	if got := debugPlugins[0].Options["debug"]; got != "true" {
		t.Fatalf("expected parsed option() value, got %q", got)
	}
	if got, want := debugPlugins[0].Variants, []string{"debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected scoped plugin variants: got %#v want %#v", got, want)
	}
	if got, want := debugPlugins[1].ID, "global"; got != want {
		t.Fatalf("unexpected global plugin id: got %q want %q", got, want)
	}
	if got, want := debugPlugins[1].Classpath, []string{filepath.Join(root, "lib", "plugins", "global.jar")}; !sameStrings(got, want) {
		t.Fatalf("unexpected global plugin classpath: got %#v want %#v", got, want)
	}
	if got := debugPlugins[1].Options["enabled"]; got != "true" {
		t.Fatalf("expected indexed option assignment to be parsed, got %q", got)
	}

	releasePlugins := mod.ActiveCompilerPlugins("release")
	if got, want := len(releasePlugins), 1; got != want {
		t.Fatalf("unexpected release plugin count: got %d want %d (%#v)", got, want, releasePlugins)
	}
	if got, want := releasePlugins[0].ID, "global"; got != want {
		t.Fatalf("unexpected release plugin id: got %q want %q", got, want)
	}
}
