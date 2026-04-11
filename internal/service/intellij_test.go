package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/intellijtask"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/testsupport"
)

func TestIntelliJSyncModelProjectsPersistedState(t *testing.T) {
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"), testsupport.Module(":lib", "jvm-library"))
	model, err := svc.IntelliJSyncModel(context.Background(), prj)
	if err != nil {
		t.Fatalf("IntelliJSyncModel returned error: %v", err)
	}
	if model.CacheKey == "" || len(model.Modules) != 2 {
		t.Fatalf("unexpected sync model: %#v", model)
	}
	lib, ok := model.Module(":lib")
	if !ok || len(lib.Variants) != 1 || lib.Variants[0].Name != "main" {
		t.Fatalf("unexpected JVM projection: %#v", lib)
	}
}

func TestIntelliJSyncModelPreservesFlavoredVariants(t *testing.T) {
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	prj := flavoredIntelliJProject(t)
	model, err := svc.IntelliJSyncModel(context.Background(), prj)
	if err != nil {
		t.Fatalf("IntelliJSyncModel returned error: %v", err)
	}
	app, ok := model.Module(":app")
	if !ok {
		t.Fatal("expected android module in sync model")
	}
	if len(app.Variants) != 4 {
		t.Fatalf("expected raw flavored variants in sync model, got %#v", app.Variants)
	}
	foundFreeDebug := false
	for _, variant := range app.Variants {
		if variant.Name != "freeDebug" {
			continue
		}
		foundFreeDebug = true
		if variant.BuildType != "debug" || len(variant.Flavors) != 1 || variant.Flavors[0] != "free" {
			t.Fatalf("unexpected fallback sync variant projection: %#v", variant)
		}
		if variant.Materialization.ID == "" || variant.Materialization.ArtifactSnapshotID == "" {
			t.Fatalf("expected graph-backed materialization ids in flavored sync variant, got %#v", variant.Materialization)
		}
		if len(variant.Materialization.ManifestPaths) == 0 || variant.Materialization.BackingArtifactID == "" {
			t.Fatalf("expected manifest and backing artifact metadata in flavored sync variant, got %#v", variant.Materialization)
		}
		if len(variant.Materialization.ProducedArtifactIDs) == 0 || len(variant.Materialization.ProducedArtifacts) == 0 {
			t.Fatalf("expected produced artifact metadata in flavored sync variant, got %#v", variant.Materialization)
		}
		if variant.Identity.GraphModuleID == "" || variant.Identity.GraphVariantID == "" || variant.Identity.IDEVariantID != "app/freeDebug" {
			t.Fatalf("expected identity mapping in flavored sync variant, got %#v", variant.Identity)
		}
		if len(variant.ContentRoots) == 0 {
			t.Fatalf("expected content roots in flavored sync variant, got %#v", variant.ContentRoots)
		}
		if len(variant.TaskCatalog) == 0 {
			t.Fatalf("expected task catalog in flavored sync variant, got %#v", variant.TaskCatalog)
		}
		if len(variant.Targets) == 0 {
			t.Fatalf("expected IDE targets in flavored sync variant, got %#v", variant.Targets)
		}
		if variant.ApplicationIDSuffix != ".debug" {
			t.Fatalf("expected suffix metadata in flavored sync variant, got %#v", variant)
		}
	}
	if !foundFreeDebug {
		t.Fatalf("expected freeDebug in sync model variants, got %#v", app.Variants)
	}
}

func TestIntegrationViewUsesPersistedConfigModel(t *testing.T) {
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	view, err := svc.IntegrationView(context.Background(), prj)
	if err != nil {
		t.Fatalf("IntegrationView returned error: %v", err)
	}
	if view.CacheKey() == "" {
		t.Fatalf("expected cached model view, got %#v", view)
	}
	if _, ok := view.Module(":app"); !ok {
		t.Fatalf("expected app module in view")
	}
}

func TestResolveIntelliJTaskRequestsDerivesModuleKind(t *testing.T) {
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	prj := flavoredIntelliJProject(t)
	requests, err := svc.ResolveIntelliJTaskRequests(prj, intellijtask.Request{
		Settings: intellijtask.Settings{
			TaskNames: []string{":app:assembleFreeDebug", ":app:compileFreeDebugUnitTestSources", ":lib:build"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveIntelliJTaskRequests returned error: %v", err)
	}
	if len(requests) != 3 {
		t.Fatalf("expected 3 build requests, got %#v", requests)
	}
	if requests[0].Command != "assemble-debug" || requests[0].RequestedVariant != "freeDebug" || !requests[0].VariantExplicit {
		t.Fatalf("unexpected flavored Android request: %#v", requests[0])
	}
	if requests[1].Command != "compileDebugUnitTestSources" || requests[1].RequestedVariant != "freeDebug" || !requests[1].VariantExplicit {
		t.Fatalf("unexpected flavored unit-test request: %#v", requests[1])
	}
	if requests[2].Command != "build" || requests[2].RequestedVariant != "main" {
		t.Fatalf("unexpected JVM request: %#v", requests[2])
	}
}

func flavoredIntelliJProject(t *testing.T) *project.Project {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.gradle.kts"), []byte("include(\":app\", \":lib\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.gradle.kts"), []byte("plugins {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appBuild := filepath.Join(root, "app", "build.gradle.kts")
	if err := os.MkdirAll(filepath.Dir(appBuild), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appBuild, []byte(`
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  flavorDimensions += "tier"
  productFlavors {
    create("free") { dimension = "tier" }
    create("paid") { dimension = "tier" }
  }
  buildTypes {
    debug { applicationIdSuffix = ".debug" }
    release { }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	libBuild := filepath.Join(root, "lib", "build.gradle.kts")
	if err := os.MkdirAll(filepath.Dir(libBuild), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(libBuild, []byte("dependencies {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &project.Project{
		RootDir:       root,
		Name:          "IntelliJSample",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules: []project.Module{
			{
				Path:             ":app",
				Dir:              filepath.Join(root, "app"),
				BuildFile:        appBuild,
				Type:             "android-application",
				Namespace:        "com.example.app",
				FlavorDimensions: []string{"tier"},
				ProductFlavors: map[string]project.ProductFlavor{
					"free": {Name: "free", Dimension: "tier"},
					"paid": {Name: "paid", Dimension: "tier"},
				},
				BuildTypes: map[string]project.BuildType{
					"debug":   {Name: "debug", ApplicationIDSuffix: ".debug"},
					"release": {Name: "release"},
				},
			},
			{
				Path:      ":lib",
				Dir:       filepath.Join(root, "lib"),
				BuildFile: libBuild,
				Type:      "jvm-library",
			},
		},
	}
}
