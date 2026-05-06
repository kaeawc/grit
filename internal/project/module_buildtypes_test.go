package project

import (
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestLoadModuleParsesCreatedBuildTypesWithBeforeVariants(t *testing.T) {
	root := t.TempDir()
	prj := &Project{RootDir: root}
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
plugins {
  id("com.android.application")
}

val selectableVariants = listOf(
  "nightlyProdSpinner",
  "nightlyProdPerf",
  "nightlyProdRelease",
  "nightlyStagingRelease",
  "playProdDebug",
  "playProdSpinner",
  "playProdCanary",
  "playProdPerf",
  "playProdBenchmark",
  "playProdInstrumentation",
  "playProdRelease",
  "playStagingDebug",
  "playStagingCanary",
  "playStagingSpinner",
  "playStagingPerf",
  "playStagingInstrumentation",
  "playStagingRelease",
  "websiteProdSpinner",
  "websiteProdRelease",
)

android {
  namespace = "org.signal"
  compileSdk = 35
  flavorDimensions += listOf("distribution", "environment")

  buildTypes {
    getByName("debug") {
      signingConfig = signingConfigs["debug"]
      isMinifyEnabled = false
    }
    getByName("release") {
      isMinifyEnabled = true
    }
    create("instrumentation") {
      initWith(getByName("debug"))
      matchingFallbacks += "debug"
      applicationIdSuffix = ".instrumentation"
    }
    create("spinner") {
      initWith(getByName("debug"))
      matchingFallbacks += "debug"
    }
    create("perf") {
      initWith(getByName("debug"))
      isMinifyEnabled = true
      matchingFallbacks += "debug"
    }
    create("benchmark") {
      initWith(getByName("debug"))
      isMinifyEnabled = true
      matchingFallbacks += "debug"
    }
    create("canary") {
      initWith(getByName("debug"))
      matchingFallbacks += "debug"
    }
  }

  productFlavors {
    create("play") {
      dimension = "distribution"
    }
    create("website") {
      dimension = "distribution"
    }
    create("nightly") {
      dimension = "distribution"
    }
    create("prod") {
      dimension = "environment"
    }
    create("staging") {
      dimension = "environment"
      applicationIdSuffix = ".staging"
    }
  }

  androidComponents {
    beforeVariants { variant ->
      variant.enable = variant.name in selectableVariants
    }
  }
}
`)

	mod, err := loadModule(prj, ":app")
	if err != nil {
		t.Fatalf("loadModule returned error: %v", err)
	}
	if got, want := mod.BuildTypes["spinner"].MatchingFallbacks, []string{"debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected spinner fallbacks: got %#v want %#v", got, want)
	}
	if !mod.BuildTypes["perf"].IsMinifyEnabled || !mod.BuildTypes["release"].IsMinifyEnabled {
		t.Fatalf("expected parsed minify flags, got %#v", mod.BuildTypes)
	}

	var names []string
	for _, variant := range mod.Variants() {
		names = append(names, variant.Name)
	}
	want := []string{
		"nightlyProdPerf",
		"nightlyProdRelease",
		"nightlyProdSpinner",
		"nightlyStagingRelease",
		"playProdBenchmark",
		"playProdCanary",
		"playProdDebug",
		"playProdInstrumentation",
		"playProdPerf",
		"playProdRelease",
		"playProdSpinner",
		"playStagingCanary",
		"playStagingDebug",
		"playStagingInstrumentation",
		"playStagingPerf",
		"playStagingRelease",
		"playStagingSpinner",
		"websiteProdRelease",
		"websiteProdSpinner",
	}
	if !sameStrings(names, want) {
		t.Fatalf("unexpected Signal-style variants: got %#v want %#v", names, want)
	}

	resolved := mod.ResolveVariant("playProdSpinner")
	if resolved.Coordinate.BuildType != "spinner" {
		t.Fatalf("expected spinner build type coordinate, got %#v", resolved.Coordinate)
	}
	if !containsString(resolved.TaskAliases, "assemblePlayProdSpinner") || containsString(resolved.TaskAliases, "testPlayProdSpinnerUnitTest") {
		t.Fatalf("unexpected spinner task aliases: %#v", resolved.TaskAliases)
	}
	if got, want := mod.buildTypeFallbacks("spinner"), []string{"spinner", "debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected spinner build type fallbacks: got %#v want %#v", got, want)
	}
	if mod.BuildFile != filepath.Join(root, "app", "build.gradle.kts") {
		t.Fatalf("unexpected build file: %#v", mod.BuildFile)
	}
}
