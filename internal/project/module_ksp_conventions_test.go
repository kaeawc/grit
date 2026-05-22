package project

import (
	"reflect"
	"sort"
	"testing"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/testutil"
)

// TestLoadMergesConventionPluginKSPProcessors covers the canonical
// class-based Hilt convention pattern: a module declares only
// `id("demo.hilt")`, the convention contributes
// `add("ksp", libs.findLibrary("hilt.compiler").get())`. Before this
// PR mod.KSP.Processors saw only the module's own ksp(...) declarations,
// so hilt.compiler — required to wire up @AndroidEntryPoint et al. —
// never reached the processor classpath.
func TestLoadMergesConventionPluginKSPProcessors(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
pluginManagement {
  repositories { google(); mavenCentral(); gradlePluginPortal() }
}
dependencyResolutionManagement { repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS) }
rootProject.name = "Example"
include(":analytics")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", "")
	testutil.WriteFile(t, root, "build-logic/convention/build.gradle.kts", `
gradlePlugin {
    plugins {
        register("hilt") {
            id = "demo.hilt"
            implementationClass = "HiltConventionPlugin"
        }
    }
}
`)
	testutil.WriteFile(t, root, "build-logic/convention/src/main/kotlin/HiltConventionPlugin.kt", `
class HiltConventionPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.pluginManager.apply("com.android.library")
        target.pluginManager.apply("com.google.devtools.ksp")
        target.dependencies {
            add("ksp", libs.findLibrary("hilt.compiler").get())
            add("implementation", libs.findLibrary("hilt.android").get())
        }
    }
}
`)
	testutil.WriteFile(t, root, "analytics/build.gradle.kts", `
plugins {
    id("demo.hilt")
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	mod := prj.FindModule(":analytics")
	if mod == nil {
		t.Fatal("expected :analytics module")
	}
	if !mod.UsesKSP {
		t.Fatal("expected UsesKSP=true after plugin expansion picked up com.google.devtools.ksp")
	}
	values := make([]string, 0, len(mod.KSP.Processors))
	for _, r := range mod.KSP.Processors {
		values = append(values, r.Kind+":"+r.Value)
	}
	sort.Strings(values)
	want := []string{"library:hilt.compiler"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("KSP processors after convention merge: got %v want %v", values, want)
	}
}

// TestLoadDedupesKSPProcessorsAcrossOwnBuildAndConvention covers the
// case where a module declares its own ksp(...) line AND applies a
// convention plugin that contributes the same processor. mergeKSPProcessorRefs
// must not duplicate.
func TestLoadDedupesKSPProcessorsAcrossOwnBuildAndConvention(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "Example"
include(":data")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", "")
	testutil.WriteFile(t, root, "build-logic/convention/build.gradle.kts", `
gradlePlugin {
    plugins {
        register("hilt") {
            id = "demo.hilt"
            implementationClass = "HiltConventionPlugin"
        }
    }
}
`)
	testutil.WriteFile(t, root, "build-logic/convention/src/main/kotlin/HiltConventionPlugin.kt", `
class HiltConventionPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.pluginManager.apply("com.google.devtools.ksp")
        target.dependencies {
            add("ksp", libs.findLibrary("hilt.compiler").get())
        }
    }
}
`)
	testutil.WriteFile(t, root, "data/build.gradle.kts", `
plugins {
    id("com.android.library")
    id("demo.hilt")
}
dependencies {
    ksp(libs.hilt.compiler)
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	mod := prj.FindModule(":data")
	if mod == nil {
		t.Fatal("expected :data module")
	}
	if got, want := len(mod.KSP.Processors), 1; got != want {
		t.Fatalf("processors should be deduped to %d, got %d: %v", want, got, mod.KSP.Processors)
	}
	if got, want := mod.KSP.Processors[0], (modulebuild.Ref{Kind: "library", Value: "hilt.compiler"}); got != want {
		t.Fatalf("processor ref: got %+v want %+v", got, want)
	}
}
