package service

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/testsupport"
	"github.com/kaeawc/grit/internal/testutil"
)

func TestMaterializationCoverageReportsMissingAndPresentLibraries(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "Example"
include(":lib")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", "")
	testutil.WriteFile(t, root, "gradle/libs.versions.toml", `
[versions]
present = "1.0.0"
missing = "1.0.0"

[libraries]
present = { group = "com.example", name = "present", version.ref = "present" }
absent = { group = "com.example", name = "absent", version.ref = "missing" }
`)
	testutil.WriteFile(t, root, "lib/build.gradle.kts", `
plugins {
    id("com.android.library")
}
dependencies {
    implementation(libs.present)
    implementation(libs.absent)
}
`)
	// Materialize the present library by dropping a jar in the maven projection.
	jarDir := filepath.Join(root, ".grit", "worktree", "materialized-m2", "com", "example", "present", "1.0.0")
	if err := os.MkdirAll(jarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jarDir, "present-1.0.0.jar"), []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	prj, err := project.Load(root)
	if err != nil {
		t.Fatalf("project.Load: %v", err)
	}

	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	report := svc.MaterializationCoverage(prj)

	if len(report.Modules) != 1 {
		t.Fatalf("expected one module, got %d (%v)", len(report.Modules), report.Modules)
	}
	got := report.Modules[0]
	if got.Module != ":lib" {
		t.Fatalf("module path: got %q want :lib", got.Module)
	}
	byAlias := map[string]MaterializationCoverageEntry{}
	for _, e := range got.Entries {
		byAlias[e.Alias] = e
	}
	if e := byAlias["present"]; e.Status != "ok" {
		t.Errorf("present entry: got status=%q want ok (%+v)", e.Status, e)
	}
	absent, ok := byAlias["absent"]
	if !ok {
		t.Fatalf("missing entry for 'absent' alias: %#v", byAlias)
	}
	if absent.Status != "missing" {
		t.Errorf("absent entry: got status=%q want missing (%+v)", absent.Status, absent)
	}
	if absent.Group != "com.example" || absent.Module != "absent" || absent.Version != "1.0.0" {
		t.Errorf("absent entry coord: got %+v", absent)
	}

	missingAliases := []string{}
	for _, e := range report.MissingAll {
		missingAliases = append(missingAliases, e.Alias)
	}
	sort.Strings(missingAliases)
	if !reflect.DeepEqual(missingAliases, []string{"absent"}) {
		t.Errorf("missing-all: got %v want [absent]", missingAliases)
	}
}

func TestMaterializationCoverageRecognizesPlatformVariant(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "Example"
include(":lib")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", "")
	testutil.WriteFile(t, root, "gradle/libs.versions.toml", `
[versions]
coroutines = "1.9.0"

[libraries]
kotlinx-coroutines-core = { group = "org.jetbrains.kotlinx", name = "kotlinx-coroutines-core", version.ref = "coroutines" }
`)
	testutil.WriteFile(t, root, "lib/build.gradle.kts", `
plugins {
    id("com.android.library")
}
dependencies {
    implementation(libs.kotlinx.coroutines.core)
}
`)
	// Materialize the -jvm sibling, NOT the umbrella, simulating the Gradle
	// Module Metadata availableAt redirect for a KMP artifact.
	jvmDir := filepath.Join(root, ".grit", "worktree", "materialized-m2", "org", "jetbrains", "kotlinx", "kotlinx-coroutines-core-jvm", "1.9.0")
	if err := os.MkdirAll(jvmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jvmDir, "kotlinx-coroutines-core-jvm-1.9.0.jar"), []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	prj, err := project.Load(root)
	if err != nil {
		t.Fatalf("project.Load: %v", err)
	}
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	report := svc.MaterializationCoverage(prj)
	if len(report.Modules) != 1 {
		t.Fatalf("expected one module, got %d", len(report.Modules))
	}
	entry := report.Modules[0].Entries[0]
	if entry.Status != "ok" {
		t.Fatalf("status: got %q want ok (%+v)", entry.Status, entry)
	}
	if entry.VariantTried != "kotlinx-coroutines-core-jvm" {
		t.Fatalf("variantTried: got %q want kotlinx-coroutines-core-jvm", entry.VariantTried)
	}
}

func TestMaterializationCoverageFlagsUnresolvedCatalogAlias(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "Example"
include(":lib")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", "")
	// Empty catalog: no entry for the alias the module references.
	testutil.WriteFile(t, root, "gradle/libs.versions.toml", `
[versions]
unused = "1.0.0"
`)
	testutil.WriteFile(t, root, "lib/build.gradle.kts", `
plugins {
    id("com.android.library")
}
dependencies {
    implementation(libs.does.not.exist)
}
`)

	prj, err := project.Load(root)
	if err != nil {
		t.Fatalf("project.Load: %v", err)
	}
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	report := svc.MaterializationCoverage(prj)
	if len(report.Modules) != 1 || len(report.Modules[0].Entries) != 1 {
		t.Fatalf("expected one module with one entry, got %#v", report.Modules)
	}
	entry := report.Modules[0].Entries[0]
	if entry.Status != "unresolved" {
		t.Fatalf("status: got %q want unresolved (%+v)", entry.Status, entry)
	}
	if entry.Alias != "does.not.exist" {
		t.Errorf("alias: got %q want does.not.exist", entry.Alias)
	}
}
