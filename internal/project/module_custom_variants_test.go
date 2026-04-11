package project

import (
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestParseCustomVariantsFromGritBlock(t *testing.T) {
	root := t.TempDir()
	body := `
android {
  buildTypes {
    debug { }
  }
}

grit {
  variants {
    create("qa") {
      baseBuildType = "debug"
      flavors("free")
      applicationIdSuffix = ".qa"
      matchingFallbacks += listOf("debug")
    }
    register("smoke") {
      fromBuildType("debug")
      flavors += "free"
      versionNameSuffix = "-smoke"
    }
  }
}
`

	variants := parseCustomVariants(body, root)
	if len(variants) != 2 {
		t.Fatalf("expected two custom variants, got %#v", variants)
	}
	qa := variants["qa"]
	if qa.DeclaredName != "qa" || qa.BaseBuildType != "debug" {
		t.Fatalf("unexpected qa identity: %#v", qa)
	}
	if got, want := qa.Flavors, []string{"free"}; !sameStrings(got, want) {
		t.Fatalf("unexpected qa flavors: got %#v want %#v", got, want)
	}
	if qa.ApplicationIDSuffix != ".qa" {
		t.Fatalf("unexpected qa application id suffix: %#v", qa)
	}
	if got, want := qa.MatchingFallbacks, []string{"debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected qa matching fallbacks: got %#v want %#v", got, want)
	}
	smoke := variants["smoke"]
	if smoke.BaseBuildType != "debug" || smoke.VersionNameSuffix != "-smoke" {
		t.Fatalf("unexpected smoke variant: %#v", smoke)
	}
}

func TestLoadModuleParsesDeclaredCustomVariantNames(t *testing.T) {
	root := t.TempDir()
	prj := &Project{RootDir: root}
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
plugins {
  id("com.android.application")
}

android {
  namespace = "com.example.app"
  compileSdk = 34
  flavorDimensions += "tier"
  productFlavors {
    create("free") {
      dimension = "tier"
      applicationIdSuffix = ".free"
    }
  }
  buildTypes {
    debug {
      applicationIdSuffix = ".debug"
    }
  }
}

grit {
  variants {
    create("qa") {
      baseBuildType = "debug"
      flavors("free")
      applicationIdSuffix = ".qa"
    }
  }
}
`)

	mod, err := loadModule(prj, ":app")
	if err != nil {
		t.Fatalf("loadModule returned error: %v", err)
	}
	custom, ok := mod.BuildTypes["qa"]
	if !ok {
		t.Fatalf("expected qa custom variant in build types, got %#v", mod.BuildTypes)
	}
	if custom.DeclaredName != "qa" || custom.BaseBuildType != "debug" {
		t.Fatalf("unexpected qa custom variant metadata: %#v", custom)
	}
	if got, want := custom.Flavors, []string{"free"}; !sameStrings(got, want) {
		t.Fatalf("unexpected qa flavors: got %#v want %#v", got, want)
	}

	resolved := mod.ResolveVariant("qa")
	if resolved.Name != "qa" || resolved.DeclaredName != "qa" {
		t.Fatalf("expected resolved custom variant identity, got %#v", resolved)
	}
	if resolved.CoordinateName != "freeDebug" || resolved.Coordinate.Name != "freeDebug" {
		t.Fatalf("expected freeDebug coordinate identity, got %#v", resolved.Coordinate)
	}
	if got, want := resolved.SourceSetOrder, []string{"main", "free", "debug", "qa"}; !sameStrings(got, want) {
		t.Fatalf("unexpected source-set order: got %#v want %#v", got, want)
	}
	if got, want := resolved.TaskAliases, []string{"assembleQa", "compileQaSources", "installQa", "assembleQaAndroidTest", "compileQaAndroidTestSources", "installQaAndroidTest", "uninstallQaAndroidTest", "compileQaUnitTestSources", "testQaUnitTest"}; !sameStrings(got, want) {
		t.Fatalf("unexpected task aliases: got %#v want %#v", got, want)
	}
	if resolved.ApplicationIDSuffix != ".qa" {
		t.Fatalf("expected custom application id suffix override, got %#v", resolved)
	}
	if mod.BuildFile != filepath.Join(root, "app", "build.gradle.kts") {
		t.Fatalf("unexpected build file: %#v", mod.BuildFile)
	}
}
