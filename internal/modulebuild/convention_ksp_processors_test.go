package modulebuild

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestIsKSPScopeAcceptsCanonicalAndVariantConfigs(t *testing.T) {
	cases := map[string]bool{
		"ksp":            true,
		"kspAndroidTest": true,
		"kspDebug":       true,
		"kspTest":        true,
		"":               false,
		"kspy":           false,
		"kspandroidtest": false,
		"implementation": false,
		"kapt":           false,
		"kotlin":         false,
		"kspcompiler":    false, // requires uppercase after "ksp"
	}
	for scope, want := range cases {
		if got := isKSPScope(scope); got != want {
			t.Errorf("isKSPScope(%q) = %v, want %v", scope, got, want)
		}
	}
}

func TestParseConventionKSPProcessorsFindsHiltCompiler(t *testing.T) {
	root := t.TempDir()
	buildLogic := filepath.Join(root, "build-logic", "convention")
	srcDir := filepath.Join(buildLogic, "src", "main", "kotlin")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildLogic, "build.gradle.kts"), []byte(`
gradlePlugin {
    plugins {
        register("hilt") {
            id = "demo.hilt"
            implementationClass = "HiltConventionPlugin"
        }
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "HiltConventionPlugin.kt"), []byte(`
class HiltConventionPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.dependencies {
            add("ksp", libs.findLibrary("hilt.compiler").get())
            add("kspAndroidTest", libs.findLibrary("hilt.android.testing.compiler").get())
            add("implementation", libs.findLibrary("hilt.android").get())
        }
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	refs := ParseConventionKSPProcessors(root, []string{"demo.hilt"})
	values := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Kind != "library" {
			t.Fatalf("unexpected kind in %v", r)
		}
		values = append(values, r.Value)
	}
	sort.Strings(values)
	want := []string{"hilt.android.testing.compiler", "hilt.compiler"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("processor aliases: got %v want %v", values, want)
	}
}

func TestParseConventionKSPProcessorsDedupesAcrossSources(t *testing.T) {
	root := t.TempDir()
	buildLogic := filepath.Join(root, "build-logic", "convention")
	srcDir := filepath.Join(buildLogic, "src", "main", "kotlin")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildLogic, "build.gradle.kts"), []byte(`
gradlePlugin {
    plugins {
        register("hilt") { id = "demo.hilt"; implementationClass = "HiltConventionPlugin" }
        register("room") { id = "demo.room"; implementationClass = "RoomConventionPlugin" }
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct{ name, body string }{
		{"HiltConventionPlugin.kt", `target.dependencies { add("ksp", libs.findLibrary("hilt.compiler").get()) }`},
		{"RoomConventionPlugin.kt", `target.dependencies { add("ksp", libs.findLibrary("hilt.compiler").get()); add("ksp", libs.findLibrary("room.compiler").get()) }`},
	} {
		if err := os.WriteFile(filepath.Join(srcDir, file.name), []byte(file.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	refs := ParseConventionKSPProcessors(root, []string{"demo.hilt", "demo.room"})
	values := make([]string, 0, len(refs))
	for _, r := range refs {
		values = append(values, r.Value)
	}
	sort.Strings(values)
	want := []string{"hilt.compiler", "room.compiler"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("dedup across sources: got %v want %v", values, want)
	}
}

func TestParseConventionKSPProcessorsReturnsNilWhenNoMatch(t *testing.T) {
	root := t.TempDir()
	if got := ParseConventionKSPProcessors(root, []string{"demo.hilt"}); got != nil {
		t.Fatalf("expected nil for empty project, got %v", got)
	}
	if got := ParseConventionKSPProcessors("", []string{"demo.hilt"}); got != nil {
		t.Fatalf("expected nil for empty root, got %v", got)
	}
	if got := ParseConventionKSPProcessors(root, nil); got != nil {
		t.Fatalf("expected nil for empty plugin list, got %v", got)
	}
}
