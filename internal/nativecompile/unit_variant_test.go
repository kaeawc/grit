package nativecompile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func TestDependenciesForVariantIncludesScopedMainAndUnitTestRefs(t *testing.T) {
	mod := &project.Module{
		Path:             ":app",
		Type:             "android-application",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	deps := &modulebuild.Dependencies{
		Main: []modulebuild.Ref{{Kind: "library", Value: "base"}},
		Scoped: map[string][]modulebuild.Ref{
			"freeImplementation":                 {{Kind: "library", Value: "free"}},
			"freeDebugImplementation":            {{Kind: "library", Value: "freeDebug"}},
			"freeDebugCompileOnly":               {{Kind: "library", Value: "annotations"}},
			"freeDebugRuntimeOnly":               {{Kind: "library", Value: "runtime"}},
			"freeDebugUnitTestImplementation":    {{Kind: "library", Value: "junit"}},
			"testFreeDebugRuntimeOnly":           {{Kind: "library", Value: "junit-runtime"}},
			"freeDebugUnitTestRuntimeOnly":       {{Kind: "library", Value: "free-unit-runtime"}},
			"unitTestFreeDebugImplementation":    {{Kind: "library", Value: "agp-unit"}},
			"unitTestFreeDebugRuntimeOnly":       {{Kind: "library", Value: "agp-unit-runtime"}},
			"testFreeDebugImplementation":        {{Kind: "library", Value: "agp-test"}},
			"freeDebugAndroidTestImplementation": {{Kind: "library", Value: "android-test"}},
			"androidTestFreeDebugRuntimeOnly":    {{Kind: "library", Value: "android-test-runtime"}},
		},
	}

	got := dependenciesForVariant(deps, mod, "freeDebug")
	if !hasRef(got.Main, "free") || !hasRef(got.Main, "freeDebug") {
		t.Fatalf("expected scoped main refs in %#v", got.Main)
	}
	if !hasRef(got.CompileOnly, "annotations") {
		t.Fatalf("expected scoped compileOnly refs in %#v", got.CompileOnly)
	}
	if !hasRef(got.RuntimeOnly, "runtime") {
		t.Fatalf("expected scoped runtimeOnly refs in %#v", got.RuntimeOnly)
	}
	if !hasRef(got.Test, "junit") {
		t.Fatalf("expected scoped unit-test refs in %#v", got.Test)
	}
	if !hasRef(got.Test, "agp-unit") || !hasRef(got.Test, "agp-test") {
		t.Fatalf("expected AGP-style unit-test refs in %#v", got.Test)
	}
	if !hasRef(got.TestRuntimeOnly, "junit-runtime") || !hasRef(got.TestRuntimeOnly, "free-unit-runtime") || !hasRef(got.TestRuntimeOnly, "agp-unit-runtime") {
		t.Fatalf("expected scoped unit-test runtime refs in %#v", got.TestRuntimeOnly)
	}
	if !hasRef(got.AndroidTest, "android-test") || !hasRef(got.AndroidTestRuntimeOnly, "android-test-runtime") {
		t.Fatalf("expected scoped android-test refs in %#v / %#v", got.AndroidTest, got.AndroidTestRuntimeOnly)
	}
}

func TestVariantSourceRootsIncludeFlavoredMainAndUnitTestRoots(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:             ":app",
		Dir:              root,
		Type:             "android-application",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}

	dirs := []string{
		filepath.Join(root, "src", "main", "kotlin"),
		filepath.Join(root, "src", "free", "kotlin"),
		filepath.Join(root, "src", "debug", "kotlin"),
		filepath.Join(root, "src", "freeDebug", "kotlin"),
		filepath.Join(root, "src", "test", "kotlin"),
		filepath.Join(root, "src", "freeDebugUnitTest", "kotlin"),
		filepath.Join(root, "src", "androidTestFreeDebug", "kotlin"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main", "kotlin", "Main.kt"), []byte("class Main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "freeDebug", "kotlin", "Flavor.kt"), []byte("class Flavor"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "freeDebugUnitTest", "kotlin", "FlavorTest.kt"), []byte("class FlavorTest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "androidTestFreeDebug", "kotlin", "FlavorAndroidTest.kt"), []byte("class FlavorAndroidTest"), 0o644); err != nil {
		t.Fatal(err)
	}

	mainSources, err := collectMainSourcesForVariant(mod, "freeDebug")
	if err != nil {
		t.Fatal(err)
	}
	if len(mainSources) < 2 {
		t.Fatalf("expected flavored main sources, got %#v", mainSources)
	}
	unitTestSources, err := collectUnitTestSources(mod, "freeDebug")
	if err != nil {
		t.Fatal(err)
	}
	if len(unitTestSources) != 1 || filepath.Base(unitTestSources[0]) != "FlavorTest.kt" {
		t.Fatalf("expected flavored unit-test sources, got %#v", unitTestSources)
	}
	androidTestSources, err := collectAndroidTestSources(mod, "freeDebug")
	if err != nil {
		t.Fatal(err)
	}
	if len(androidTestSources) != 1 || filepath.Base(androidTestSources[0]) != "FlavorAndroidTest.kt" {
		t.Fatalf("expected flavored androidTest sources, got %#v", androidTestSources)
	}
}

func hasRef(refs []modulebuild.Ref, value string) bool {
	for _, ref := range refs {
		if ref.Value == value {
			return true
		}
	}
	return false
}
