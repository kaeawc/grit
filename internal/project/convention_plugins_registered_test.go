package project

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestParsePluginRegistrationsExtractsIDAndImplClass(t *testing.T) {
	body := `
gradlePlugin {
    plugins {
        register("androidApplication") {
            id = "demo.android.application"
            implementationClass = "AndroidApplicationConventionPlugin"
        }
        register("androidLibrary") {
            implementationClass = "AndroidLibraryConventionPlugin"
            id = "demo.android.library"
        }
        register("missingImpl") {
            id = "demo.skip"
        }
    }
}
`
	got := parsePluginRegistrations(body)
	want := []pluginRegistration{
		{id: "demo.android.application", implClass: "AndroidApplicationConventionPlugin"},
		{id: "demo.android.library", implClass: "AndroidLibraryConventionPlugin"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registrations:\n got  %#v\n want %#v", got, want)
	}
}

func TestParseAppliedPluginIDsCollectsUniqueApplyCalls(t *testing.T) {
	body := `
class AndroidApplicationConventionPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        with(target) {
            with(pluginManager) {
                apply("com.android.application")
                apply("org.jetbrains.kotlin.android")
                apply("com.android.application") // duplicate
            }
        }
    }
}
`
	got := parseAppliedPluginIDs(body)
	sort.Strings(got)
	want := []string{"com.android.application", "org.jetbrains.kotlin.android"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("applied ids:\n got  %v\n want %v", got, want)
	}
}

func TestMergeRegisteredConventionsLinksIDToAppliedPlugins(t *testing.T) {
	root := t.TempDir()
	buildLogic := filepath.Join(root, "build-logic", "convention")
	srcDir := filepath.Join(buildLogic, "src", "main", "kotlin")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Build-logic root build.gradle.kts with the registration block.
	if err := os.WriteFile(filepath.Join(buildLogic, "build.gradle.kts"), []byte(`
gradlePlugin {
    plugins {
        register("androidApplication") {
            id = "demo.android.application"
            implementationClass = "AndroidApplicationConventionPlugin"
        }
        register("jvmLibrary") {
            id = "demo.jvm.library"
            implementationClass = "JvmLibraryConventionPlugin"
        }
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Two impl classes; the application one applies android+kotlin, the
	// jvm one applies kotlin("jvm").
	if err := os.WriteFile(filepath.Join(srcDir, "AndroidApplicationConventionPlugin.kt"), []byte(`
class AndroidApplicationConventionPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.pluginManager.apply("com.android.application")
        target.pluginManager.apply("org.jetbrains.kotlin.android")
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "JvmLibraryConventionPlugin.kt"), []byte(`
class JvmLibraryConventionPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.pluginManager.apply("org.jetbrains.kotlin.jvm")
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := map[string][]string{}
	mergeRegisteredConventions(filepath.Join(root, "build-logic"), out)

	wantApp := []string{"com.android.application", "org.jetbrains.kotlin.android"}
	if got := out["demo.android.application"]; !reflect.DeepEqual(sortedCopy(got), sortedCopy(wantApp)) {
		t.Fatalf("application convention:\n got  %v\n want %v", got, wantApp)
	}
	wantJVM := []string{"org.jetbrains.kotlin.jvm"}
	if got := out["demo.jvm.library"]; !reflect.DeepEqual(got, wantJVM) {
		t.Fatalf("jvm convention:\n got  %v\n want %v", got, wantJVM)
	}
}

func TestInferTypeFromPluginsClassifiesCommonAndroidPaths(t *testing.T) {
	cases := []struct {
		name    string
		plugins []string
		want    string
	}{
		{"empty", nil, ""},
		{"app", []string{"com.android.application", "org.jetbrains.kotlin.android"}, "android-application"},
		{"library", []string{"com.android.library", "org.jetbrains.kotlin.android"}, "android-library"},
		{"jvm", []string{"org.jetbrains.kotlin.jvm"}, "jvm-library"},
		{"java-library", []string{"java-library"}, "jvm-library"},
		{"test-module", []string{"com.android.test"}, "android-application"},
		{"library-wins-over-jvm", []string{"com.android.library", "org.jetbrains.kotlin.jvm"}, "android-library"},
		{"unknown", []string{"com.example.custom"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferTypeFromPlugins(tc.plugins); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
