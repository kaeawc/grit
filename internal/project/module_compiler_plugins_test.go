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

func TestLoadRefreshesCompilerPluginsFromConventionPlugins(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
pluginManagement {
  repositories { google(); mavenCentral(); gradlePluginPortal() }
}
dependencyResolutionManagement { repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS) }
rootProject.name = "Example"
include(":core-ui")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", "")
	testutil.WriteFile(t, root, "build-logic/plugins/src/main/kotlin/convention-library.gradle.kts", `
plugins {
  id("com.android.library")
  id("org.jetbrains.kotlin.plugin.compose")
}
`)
	testutil.WriteFile(t, root, "core-ui/build.gradle.kts", `
plugins {
  id("convention-library")
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	mod := prj.FindModule(":core-ui")
	if mod == nil {
		t.Fatal("expected module")
	}
	if !mod.UsesCompose {
		t.Fatal("expected expanded compose plugin to mark module as using Compose")
	}
	plugins := mod.ActiveCompilerPlugins("release")
	if got, want := len(plugins), 1; got != want {
		t.Fatalf("unexpected compiler plugin count: got %d want %d (%#v)", got, want, plugins)
	}
	if got, want := plugins[0].ID, modulebuild.ComposeCompilerPluginID; got != want {
		t.Fatalf("unexpected compiler plugin id: got %q want %q", got, want)
	}
}

func TestLoadExpandsVersionCatalogPluginAliases(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
pluginManagement {
  repositories { google(); mavenCentral(); gradlePluginPortal() }
}
dependencyResolutionManagement { repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS) }
rootProject.name = "Example"
include(":lib")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", "")
	testutil.WriteFile(t, root, "gradle/libs.versions.toml", `
[plugins]
android-library = { id = "com.android.library", version = "8.13.1" }
kotlin-serialization = { id = "org.jetbrains.kotlin.plugin.serialization", version = "2.3.0" }
compose-compiler = { id = "org.jetbrains.kotlin.plugin.compose", version = "2.3.0" }
ksp = { id = "com.google.devtools.ksp", version = "2.3.0-1.0.0" }
`)
	testutil.WriteFile(t, root, "lib/build.gradle.kts", `
plugins {
  alias(libs.plugins.android.library)
  alias(libs.plugins.kotlin.serialization)
  alias(libs.plugins.compose.compiler)
  alias(libs.plugins.ksp)
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	mod := prj.FindModule(":lib")
	if mod == nil {
		t.Fatal("expected module")
	}
	if got, want := mod.Type, "android-library"; got != want {
		t.Fatalf("unexpected module type: got %q want %q", got, want)
	}
	if !mod.UsesCompose {
		t.Fatal("expected compose alias to enable Compose compiler")
	}
	if !mod.UsesKotlinSerialization {
		t.Fatal("expected serialization alias to enable serialization compiler")
	}
	if !mod.UsesKSP {
		t.Fatal("expected ksp alias to enable KSP")
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

	compositeDebugPlugins := mod.ActiveCompilerPlugins("freeDebug")
	if got, want := len(compositeDebugPlugins), 2; got != want {
		t.Fatalf("unexpected composite debug plugin count: got %d want %d (%#v)", got, want, compositeDebugPlugins)
	}
	if got, want := compositeDebugPlugins[0].ID, "com.example.wire"; got != want {
		t.Fatalf("unexpected composite debug scoped plugin id: got %q want %q", got, want)
	}

	releasePlugins := mod.ActiveCompilerPlugins("release")
	if got, want := len(releasePlugins), 1; got != want {
		t.Fatalf("unexpected release plugin count: got %d want %d (%#v)", got, want, releasePlugins)
	}
	if got, want := releasePlugins[0].ID, "global"; got != want {
		t.Fatalf("unexpected release plugin id: got %q want %q", got, want)
	}
}
