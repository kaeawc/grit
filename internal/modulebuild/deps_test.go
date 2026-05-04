package modulebuild

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDependenciesStripsInlineComments(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    implementation(libs.ktor.server.netty)
    testImplementation(libs.core.ktx) // For ApplicationProvider
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(deps.Main), 1; got != want {
		t.Fatalf("unexpected main dep count: got %d want %d", got, want)
	}
	if deps.Main[0].Kind != "library" || deps.Main[0].Value != "ktor.server.netty" {
		t.Fatalf("unexpected main dep: %#v", deps.Main[0])
	}
	if got, want := len(deps.Test), 1; got != want {
		t.Fatalf("unexpected test dep count: got %d want %d", got, want)
	}
	if deps.Test[0].Kind != "library" || deps.Test[0].Value != "core.ktx" {
		t.Fatalf("unexpected test dep: %#v", deps.Test[0])
	}
}

func TestParseDependenciesStripsTrailingConfigurationClosure(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    implementation(libs.msal) { exclude(group = "com.google.guava", module = "guava") }
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(deps.Main), 1; got != want {
		t.Fatalf("unexpected main dep count: got %d want %d", got, want)
	}
	if deps.Main[0].Kind != "library" || deps.Main[0].Value != "msal" {
		t.Fatalf("unexpected main dep: %#v", deps.Main[0])
	}
}

func TestParseDependenciesCapturesVariantScopedConfigurations(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    freeImplementation(projects.shared)
    freeDebugImplementation(libs.okhttp)
    freeDebugCompileOnly(libs.annotations)
    freeDebugRuntimeOnly(libs.okio)
    freeDebugUnitTestImplementation(libs.junit)
    freeDebugUnitTestRuntimeOnly(libs.junit.platform.runner)
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(deps.Scoped["freeImplementation"]), 1; got != want {
		t.Fatalf("unexpected freeImplementation refs: %#v", deps.Scoped)
	}
	if got, want := len(deps.Scoped["freeDebugImplementation"]), 1; got != want {
		t.Fatalf("unexpected freeDebugImplementation refs: %#v", deps.Scoped)
	}
	if got, want := len(deps.Scoped["freeDebugCompileOnly"]), 1; got != want {
		t.Fatalf("unexpected freeDebugCompileOnly refs: %#v", deps.Scoped)
	}
	if got, want := len(deps.Scoped["freeDebugRuntimeOnly"]), 1; got != want {
		t.Fatalf("unexpected freeDebugRuntimeOnly refs: %#v", deps.Scoped)
	}
	if got, want := len(deps.Scoped["freeDebugUnitTestImplementation"]), 1; got != want {
		t.Fatalf("unexpected freeDebugUnitTestImplementation refs: %#v", deps.Scoped)
	}
	if got, want := len(deps.Scoped["freeDebugUnitTestRuntimeOnly"]), 1; got != want {
		t.Fatalf("unexpected freeDebugUnitTestRuntimeOnly refs: %#v", deps.Scoped)
	}
}

func TestParseDependenciesCapturesRuntimeOnlyConfigurations(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    runtimeOnly(libs.okio)
    testRuntimeOnly(libs.junit.platform.runner)
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(deps.RuntimeOnly), 1; got != want {
		t.Fatalf("unexpected runtimeOnly count: got %d want %d", got, want)
	}
	if deps.RuntimeOnly[0].Kind != "library" || deps.RuntimeOnly[0].Value != "okio" {
		t.Fatalf("unexpected runtimeOnly dep: %#v", deps.RuntimeOnly[0])
	}
	if got, want := len(deps.TestRuntimeOnly), 1; got != want {
		t.Fatalf("unexpected testRuntimeOnly count: got %d want %d", got, want)
	}
	if deps.TestRuntimeOnly[0].Kind != "library" || deps.TestRuntimeOnly[0].Value != "junit.platform.runner" {
		t.Fatalf("unexpected testRuntimeOnly dep: %#v", deps.TestRuntimeOnly[0])
	}
}

func TestParseDependenciesCapturesAndroidTestConfigurations(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    androidTestImplementation(libs.test.ext.junit)
    androidTestCompileOnly(libs.annotations)
    androidTestRuntimeOnly(libs.espresso.core)
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(deps.AndroidTest), 1; got != want {
		t.Fatalf("unexpected androidTestImplementation count: got %d want %d", got, want)
	}
	if got, want := len(deps.AndroidTestCompileOnly), 1; got != want {
		t.Fatalf("unexpected androidTestCompileOnly count: got %d want %d", got, want)
	}
	if got, want := len(deps.AndroidTestRuntimeOnly), 1; got != want {
		t.Fatalf("unexpected androidTestRuntimeOnly count: got %d want %d", got, want)
	}
}
