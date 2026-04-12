package project

import (
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
