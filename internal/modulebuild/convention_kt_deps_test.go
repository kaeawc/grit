package modulebuild

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestParseClassDependencyExprResolvesCommonForms(t *testing.T) {
	cases := []struct {
		expr string
		want Ref
	}{
		{`libs.findLibrary("hilt.android").get()`, Ref{Kind: "library", Value: "hilt.android"}},
		{`libs.findLibrary("hilt-android").get()`, Ref{Kind: "library", Value: "hilt-android"}},
		{`libs.findLibrary("hilt.compiler").get()`, Ref{Kind: "library", Value: "hilt.compiler"}},
		{`libs.findBundle("compose.ui").get()`, Ref{Kind: "bundle", Value: "compose.ui"}},
		{`libs.androidx.tracing.ktx`, Ref{Kind: "library", Value: "androidx.tracing.ktx"}},
		{`libs.bundles.compose.ui`, Ref{Kind: "bundle", Value: "compose.ui"}},
		{`project(":core:model")`, Ref{Kind: "project", Value: ":core:model"}},
		{`platform(libs.findLibrary("firebase.bom").get())`, Ref{Kind: "platform-library", Value: "firebase.bom"}},
		{`enforcedPlatform(libs.findLibrary("firebase.bom").get())`, Ref{Kind: "enforced-platform-library", Value: "firebase.bom"}},
		{`"com.example:lib:1.0"`, Ref{Kind: "raw", Value: "com.example:lib:1.0"}},
		{`kotlin("test")`, Ref{Kind: "raw", Value: "org.jetbrains.kotlin:kotlin-test"}},
		{`kotlin("reflect")`, Ref{Kind: "raw", Value: "org.jetbrains.kotlin:kotlin-reflect"}},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			if got := parseClassDependencyExpr(tc.expr); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseClassDependencyExpr(%q):\n got  %#v\n want %#v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestParseClassConventionDependenciesExtractsAddCalls(t *testing.T) {
	body := `
import org.gradle.api.Plugin
import org.gradle.api.Project

class HiltConventionPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        with(target) {
            pluginManager.apply("com.google.devtools.ksp")
            dependencies {
                add("ksp", libs.findLibrary("hilt.compiler").get())
            }
            pluginManager.withPlugin("org.jetbrains.kotlin.jvm") {
                dependencies {
                    add("implementation", libs.findLibrary("hilt.core").get())
                }
            }
            pluginManager.withPlugin("com.android.base") {
                dependencies {
                    add("implementation", libs.findLibrary("hilt.android").get())
                }
            }
        }
    }
}
`
	deps := &Dependencies{}
	parseClassConventionDependencies(body, deps)

	mainAliases := refValues(deps.Main, "library")
	sort.Strings(mainAliases)
	wantMain := []string{"hilt.android", "hilt.core"}
	if !reflect.DeepEqual(mainAliases, wantMain) {
		t.Fatalf("main library aliases: got %v want %v", mainAliases, wantMain)
	}

	kspAliases := refValues(deps.Scoped["ksp"], "library")
	if !reflect.DeepEqual(kspAliases, []string{"hilt.compiler"}) {
		t.Fatalf("ksp aliases: got %v want [hilt.compiler]", kspAliases)
	}
}

func TestParseClassConventionDependenciesHandlesMultiLineAddCalls(t *testing.T) {
	body := `
class Plugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.dependencies {
            add(
                "implementation",
                libs.findLibrary("hilt.android").get(),
            )
            add("ksp", libs.findLibrary("hilt.compiler").get())  // single-line still works
        }
    }
}
`
	deps := &Dependencies{}
	parseClassConventionDependencies(body, deps)
	mainLibs := refValues(deps.Main, "library")
	if !reflect.DeepEqual(mainLibs, []string{"hilt.android"}) {
		t.Fatalf("multi-line implementation: got %v want [hilt.android]", mainLibs)
	}
	ksp := refValues(deps.Scoped["ksp"], "library")
	if !reflect.DeepEqual(ksp, []string{"hilt.compiler"}) {
		t.Fatalf("single-line ksp: got %v want [hilt.compiler]", ksp)
	}
}

// TestParseClassConventionDependenciesSkipsBareIdentifierRefs covers
// the NIA-style pattern where a convention binds a catalog accessor to
// a local val and then passes it to add(...). Without filtering we'd
// emit Ref{Kind:"platform-raw", Value:"bom"} which the downstream
// resolver rejects with "invalid raw coordinate".
func TestParseClassConventionDependenciesSkipsBareIdentifierRefs(t *testing.T) {
	body := `
class FirebasePlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.dependencies {
            val bom = libs.findLibrary("firebase-bom").get()
            add("implementation", platform(bom))
            add("implementation", libs.findLibrary("firebase.analytics").get())
        }
    }
}
`
	deps := &Dependencies{}
	parseClassConventionDependencies(body, deps)
	libs := refValues(deps.Main, "library")
	if !reflect.DeepEqual(libs, []string{"firebase.analytics"}) {
		t.Fatalf("library refs: got %v want [firebase.analytics]", libs)
	}
	for scope, refs := range deps.Scoped {
		for _, ref := range refs {
			if strings.HasSuffix(ref.Kind, "raw") && ref.Value == "bom" {
				t.Fatalf("bare-identifier raw ref leaked into scope %q: %#v", scope, ref)
			}
		}
	}
}

func TestParseClassConventionDependenciesHandlesProjectAndPlatform(t *testing.T) {
	body := `
class AnyPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.dependencies {
            add("implementation", project(":core:model"))
            add("prodImplementation", platform(libs.findLibrary("firebase.bom").get()))
            add("prodImplementation", libs.findLibrary("firebase.analytics").get())
        }
    }
}
`
	deps := &Dependencies{}
	parseClassConventionDependencies(body, deps)

	projects := refValues(deps.Main, "project")
	if !reflect.DeepEqual(projects, []string{":core:model"}) {
		t.Fatalf("project deps: got %v want [:core:model]", projects)
	}
	prodImpls := deps.Scoped["prodImplementation"]
	if len(prodImpls) != 2 {
		t.Fatalf("prodImplementation count: got %d want 2 (%v)", len(prodImpls), prodImpls)
	}
	// First entry should be platform-library:firebase.bom; second library:firebase.analytics.
	kinds := []string{prodImpls[0].Kind, prodImpls[1].Kind}
	wantKinds := []string{"platform-library", "library"}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("prodImplementation kinds: got %v want %v", kinds, wantKinds)
	}
}

func TestClassConventionPluginFilesMatchesRegisteredImplClass(t *testing.T) {
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
            implementationClass = "com.example.HiltConventionPlugin"
        }
        register("library") {
            id = "demo.library"
            implementationClass = "AndroidLibraryConventionPlugin"
        }
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	hiltPath := filepath.Join(srcDir, "HiltConventionPlugin.kt")
	if err := os.WriteFile(hiltPath, []byte("// hilt body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "AndroidLibraryConventionPlugin.kt"), []byte("// lib body"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Applying demo.hilt returns only the registered impl class .kt —
	// AndroidLibraryConventionPlugin.kt has no call from HiltConventionPlugin.kt
	// so it isn't pulled into the closure.
	got := classConventionPluginFiles(root, []string{"demo.hilt"})
	if !reflect.DeepEqual(got, []string{hiltPath}) {
		t.Fatalf("class convention files (no call-graph): got %v want [%s]", got, hiltPath)
	}

	// Unrelated plugin ids return no matches.
	if got := classConventionPluginFiles(root, []string{"demo.other"}); len(got) != 0 {
		t.Fatalf("unrelated plugin: got %v want []", got)
	}
}

// TestParseDependenciesForModulePicksUpHelperFileDeps covers the
// NIA-style pattern where the registered convention plugin delegates
// dependency contributions to a top-level helper function defined in a
// sibling .kt file. Concretely: AndroidLibraryComposeConventionPlugin
// calls configureAndroidCompose(extension), and configureAndroidCompose
// (in AndroidCompose.kt) is what actually writes
// `add("implementation", libs.findLibrary("compose-ui-tooling-preview")...)`.
func TestParseDependenciesForModulePicksUpHelperFileDeps(t *testing.T) {
	root := t.TempDir()
	buildLogic := filepath.Join(root, "build-logic", "convention")
	srcDir := filepath.Join(buildLogic, "src", "main", "kotlin")
	helperDir := filepath.Join(srcDir, "com", "example")
	if err := os.MkdirAll(helperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildLogic, "build.gradle.kts"), []byte(`
gradlePlugin {
    plugins {
        register("compose") { id = "demo.compose"; implementationClass = "ComposeConventionPlugin" }
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Registered convention plugin only applies sibling plugins and calls
	// the helper — no add(...) of its own.
	if err := os.WriteFile(filepath.Join(srcDir, "ComposeConventionPlugin.kt"), []byte(`
class ComposeConventionPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.pluginManager.apply("com.android.library")
        configureAndroidCompose(target)
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Helper in a sibling .kt that's never registered as a plugin class.
	if err := os.WriteFile(filepath.Join(helperDir, "AndroidCompose.kt"), []byte(`
internal fun Project.configureAndroidCompose() {
    dependencies {
        add("implementation", libs.findLibrary("compose-ui-tooling-preview").get())
        add("debugImplementation", libs.findLibrary("compose-ui-tooling").get())
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(root, "app")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildFile := filepath.Join(moduleDir, "build.gradle.kts")
	if err := os.WriteFile(buildFile, []byte(`
plugins {
    id("demo.compose")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, err := ParseDependenciesForModule(buildFile, root, []string{"demo.compose"})
	if err != nil {
		t.Fatalf("ParseDependenciesForModule: %v", err)
	}
	mainLibs := refValues(deps.Main, "library")
	sort.Strings(mainLibs)
	wantMain := []string{"compose-ui-tooling-preview"}
	if !reflect.DeepEqual(mainLibs, wantMain) {
		t.Fatalf("main libs from helper: got %v want %v", mainLibs, wantMain)
	}
	debugLibs := refValues(deps.Scoped["debugImplementation"], "library")
	if !reflect.DeepEqual(debugLibs, []string{"compose-ui-tooling"}) {
		t.Fatalf("debugImpl libs from helper: got %v want [compose-ui-tooling]", debugLibs)
	}
}

func TestParseDependenciesForModulePicksUpClassConventionDeps(t *testing.T) {
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
            add("implementation", libs.findLibrary("hilt.android").get())
            add("ksp", libs.findLibrary("hilt.compiler").get())
        }
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Module declares its own implementation dep on libs.androidx.activity
	// in a script-style block; plugin contributes hilt-android and hilt-compiler.
	moduleDir := filepath.Join(root, "app")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildFile := filepath.Join(moduleDir, "build.gradle.kts")
	if err := os.WriteFile(buildFile, []byte(`
plugins {
    id("demo.hilt")
}
dependencies {
    implementation(libs.androidx.activity)
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, err := ParseDependenciesForModule(buildFile, root, []string{"demo.hilt"})
	if err != nil {
		t.Fatalf("ParseDependenciesForModule: %v", err)
	}

	mainAliases := refValues(deps.Main, "library")
	sort.Strings(mainAliases)
	want := []string{"androidx.activity", "hilt.android"}
	if !reflect.DeepEqual(mainAliases, want) {
		t.Fatalf("main libs: got %v want %v", mainAliases, want)
	}
	kspAliases := refValues(deps.Scoped["ksp"], "library")
	if !reflect.DeepEqual(kspAliases, []string{"hilt.compiler"}) {
		t.Fatalf("ksp libs: got %v want [hilt.compiler]", kspAliases)
	}
}

func refValues(refs []Ref, wantKind string) []string {
	var out []string
	for _, r := range refs {
		if r.Kind == wantKind {
			out = append(out, r.Value)
		}
	}
	return out
}
