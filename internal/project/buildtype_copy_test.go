package project

import (
	"reflect"
	"testing"
)

func TestModuleVariantReturnsBuildTypeCopy(t *testing.T) {
	mod := testBuildTypeCopyModule()

	variant := mod.Variant("debug")
	mutateBuildType(variant)

	got := mod.BuildTypes["debug"]
	want := testBuildTypeCopyModule().BuildTypes["debug"]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Variant should return a copy: got %#v want %#v", got, want)
	}
}

func TestModuleVariantsReturnBuildTypeCopies(t *testing.T) {
	mod := testBuildTypeCopyModule()

	variants := mod.Variants()
	if len(variants) == 0 {
		t.Fatal("expected variants")
	}
	mutateBuildType(variants[0])

	got := mod.BuildTypes["debug"]
	want := testBuildTypeCopyModule().BuildTypes["debug"]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Variants should return copies: got %#v want %#v", got, want)
	}
}

func TestModuleResolveVariantReturnsBuildTypeCopy(t *testing.T) {
	mod := testBuildTypeCopyModule()

	resolved := mod.ResolveVariant("debug")
	mutateBuildType(resolved.Config)
	mutateVariantOptimization(resolved.Optimization)

	got := mod.BuildTypes["debug"]
	want := testBuildTypeCopyModule().BuildTypes["debug"]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveVariant should return copied config: got %#v want %#v", got, want)
	}
}

func testBuildTypeCopyModule() Module {
	minify := true
	shrink := false
	return Module{
		Path: ":app",
		Type: "android-application",
		BuildTypes: map[string]BuildType{
			"debug": {
				Name:              "debug",
				Flavors:           []string{"free"},
				MatchingFallbacks: []string{"release"},
				Optimization: VariantOptimization{
					PackageOptimizations: []PackageOptimization{{
						PackageName:     "com.example",
						MinifyEnabled:   &minify,
						ShrinkResources: &shrink,
					}},
				},
				ProguardFiles: []string{"proguard-rules.pro"},
			},
		},
	}
}

func mutateBuildType(buildType BuildType) {
	buildType.Flavors[0] = "changed"
	buildType.MatchingFallbacks[0] = "changed"
	mutateVariantOptimization(buildType.Optimization)
	buildType.ProguardFiles[0] = "changed.pro"
}

func mutateVariantOptimization(optimization VariantOptimization) {
	optimization.PackageOptimizations[0].PackageName = "changed"
	*optimization.PackageOptimizations[0].MinifyEnabled = false
	*optimization.PackageOptimizations[0].ShrinkResources = true
}
