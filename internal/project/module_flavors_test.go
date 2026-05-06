package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFlavorDimensionsAndProductFlavors(t *testing.T) {
	body := `
android {
  flavorDimensions += "tier"
  flavorDimensions("api")
  productFlavors {
    create("free") {
      dimension = "tier"
      applicationIdSuffix = ".free"
    }
    pro {
      dimension = "tier"
      versionNameSuffix = "-pro"
    }
    minApi24 {
      dimension = "api"
    }
  }
}
`
	dims := parseFlavorDimensions(body)
	if got, want := len(dims), 2; got != want {
		t.Fatalf("unexpected flavor dimensions: %#v", dims)
	}
	if dims[0] != "api" && dims[0] != "tier" {
		t.Fatalf("unexpected flavor dimension ordering/content: %#v", dims)
	}
	flavors := parseProductFlavors(body)
	if got, want := len(flavors), 3; got != want {
		t.Fatalf("unexpected product flavors: %#v", flavors)
	}
	if flavors["free"].Dimension != "tier" || flavors["free"].ApplicationIDSuffix != ".free" {
		t.Fatalf("unexpected free flavor: %#v", flavors["free"])
	}
	if flavors["pro"].VersionNameSuffix != "-pro" {
		t.Fatalf("unexpected pro flavor: %#v", flavors["pro"])
	}
	if flavors["minApi24"].Dimension != "api" {
		t.Fatalf("unexpected minApi24 flavor: %#v", flavors["minApi24"])
	}
}

func TestModuleVariantsExpandFlavorDimensions(t *testing.T) {
	mod := Module{
		Path: ":app",
		Type: "android-application",
		DefaultConfig: DefaultConfig{
			ApplicationID:     "dev.example",
			VersionName:       "1.0",
			MinSDK:            "21",
			MissingDimensions: map[string][]string{"abi": {"x86", "arm64"}},
		},
		FlavorDimensions: []string{"tier", "api"},
		ProductFlavors: map[string]ProductFlavor{
			"free":     {Name: "free", Dimension: "tier", ApplicationIDSuffix: ".free", MissingDimensions: map[string][]string{"abi": {"arm64"}}},
			"pro":      {Name: "pro", Dimension: "tier"},
			"minApi21": {Name: "minApi21", Dimension: "api", MinSDK: "21"},
			"minApi24": {Name: "minApi24", Dimension: "api", MinSDK: "24", VersionNameSuffix: "-minApi24"},
		},
		BuildTypes: map[string]BuildType{
			"debug":   {Name: "debug", ApplicationIDSuffix: ".debug"},
			"release": {Name: "release"},
		},
	}

	variants := mod.Variants()
	if got, want := len(variants), 8; got != want {
		t.Fatalf("unexpected variant count: got %d want %d (%#v)", got, want, variants)
	}
	resolved := mod.ResolveVariant("freeMinApi24Debug")
	if resolved.Coordinate.BuildType != "debug" {
		t.Fatalf("unexpected build type: %#v", resolved.Coordinate)
	}
	if got, want := len(resolved.Coordinate.Flavors), 2; got != want {
		t.Fatalf("unexpected flavors: %#v", resolved.Coordinate)
	}
	if resolved.Coordinate.Flavors[0] != "free" || resolved.Coordinate.Flavors[1] != "minApi24" {
		t.Fatalf("unexpected flavor order: %#v", resolved.Coordinate.Flavors)
	}
	if resolved.Config.Name != "freeMinApi24Debug" || resolved.Config.BaseBuildType != "debug" {
		t.Fatalf("unexpected resolved config: %#v", resolved.Config)
	}
	if resolved.ApplicationID != "dev.example.free.debug" {
		t.Fatalf("unexpected application id: %#v", resolved)
	}
	if resolved.VersionName != "1.0-minApi24" {
		t.Fatalf("unexpected version name: %#v", resolved)
	}
	if resolved.MinSDK != "24" {
		t.Fatalf("unexpected minSdk: %#v", resolved)
	}
	if got, want := resolved.MissingDimensions["abi"], []string{"arm64"}; !sameStrings(got, want) {
		t.Fatalf("unexpected missing dimension strategy: got %#v want %#v", got, want)
	}
}

func TestModuleVariantsApplyEnabledVariantAllowlist(t *testing.T) {
	mod := Module{
		Path:             ":app",
		Type:             "android-application",
		FlavorDimensions: []string{"distribution", "environment"},
		ProductFlavors: map[string]ProductFlavor{
			"play":    {Name: "play", Dimension: "distribution"},
			"website": {Name: "website", Dimension: "distribution"},
			"prod":    {Name: "prod", Dimension: "environment"},
			"staging": {Name: "staging", Dimension: "environment"},
		},
		BuildTypes: map[string]BuildType{
			"debug":   {Name: "debug"},
			"release": {Name: "release"},
		},
		EnabledVariants: []string{
			"playProdDebug",
			"playStagingRelease",
			"websiteProdRelease",
		},
	}

	variants := mod.Variants()
	var names []string
	for _, variant := range variants {
		names = append(names, variant.Name)
	}
	if got, want := names, []string{"playProdDebug", "playStagingRelease", "websiteProdRelease"}; !sameStrings(got, want) {
		t.Fatalf("unexpected enabled variants: got %#v want %#v", got, want)
	}
	for _, name := range names {
		if name == "websiteStagingDebug" {
			t.Fatalf("disabled variant leaked into variant list: %#v", names)
		}
	}
}

func TestResolveVariantsPreservesDisabledVariantRequest(t *testing.T) {
	mod := Module{
		Path:             ":app",
		Type:             "android-application",
		FlavorDimensions: []string{"distribution", "environment"},
		ProductFlavors: map[string]ProductFlavor{
			"play":    {Name: "play", Dimension: "distribution"},
			"website": {Name: "website", Dimension: "distribution"},
			"prod":    {Name: "prod", Dimension: "environment"},
			"staging": {Name: "staging", Dimension: "environment"},
		},
		BuildTypes: map[string]BuildType{
			"debug":   {Name: "debug"},
			"release": {Name: "release"},
		},
		EnabledVariants: []string{"playProdDebug"},
	}

	resolved := mod.ResolveVariants([]string{"websiteStagingDebug"})
	if len(resolved) != 1 || resolved[0].Name != "websiteStagingDebug" {
		t.Fatalf("expected disabled variant request to stay explicit, got %#v", resolved)
	}
}

func TestParseEnabledVariantsFromBeforeVariantsAllowlist(t *testing.T) {
	body := `
val selectableVariants = listOf(
  "playProdDebug",
  "playProdRelease",
  "websiteProdRelease",
)

android {
  androidComponents {
    beforeVariants { variant ->
      variant.enable = variant.name in selectableVariants
    }
  }
}
`
	got := parseEnabledVariants(body)
	want := []string{"playProdDebug", "playProdRelease", "websiteProdRelease"}
	if !sameStrings(got, want) {
		t.Fatalf("parseEnabledVariants = %#v want %#v", got, want)
	}
}

func TestLoadModuleParsesFlavorMetadata(t *testing.T) {
	root := t.TempDir()
	prj := &Project{RootDir: root}
	buildFile := filepath.Join(root, "app", "build.gradle.kts")
	if err := os.MkdirAll(filepath.Dir(buildFile), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	mustWriteSemanticFile(t, buildFile, `
plugins {
  alias(libs.plugins.android.application)
}

android {
  namespace = "dev.example"
  flavorDimensions += "tier"
  productFlavors {
    create("free") {
      dimension = "tier"
      applicationIdSuffix = ".free"
    }
    create("pro") {
      dimension = "tier"
    }
  }
  buildTypes {
    debug { }
    release { isMinifyEnabled = true }
  }
}
`)

	mod, err := loadModule(prj, ":app")
	if err != nil {
		t.Fatalf("loadModule returned error: %v", err)
	}
	if got, want := len(mod.FlavorDimensions), 1; got != want || mod.FlavorDimensions[0] != "tier" {
		t.Fatalf("unexpected flavor dimensions: %#v", mod.FlavorDimensions)
	}
	if got, want := len(mod.ProductFlavors), 2; got != want {
		t.Fatalf("unexpected product flavors: %#v", mod.ProductFlavors)
	}
	if mod.ProductFlavors["free"].ApplicationIDSuffix != ".free" {
		t.Fatalf("unexpected free flavor: %#v", mod.ProductFlavors["free"])
	}
	if got, want := len(mod.Variants()), 4; got != want {
		t.Fatalf("unexpected synthesized variants: %#v", mod.Variants())
	}
}
