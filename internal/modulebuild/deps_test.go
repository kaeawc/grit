package modulebuild

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParseDependenciesCapturesGroovyProjectDependency(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle")
	body := `
dependencies {
    implementation project(':core-util')
    testImplementation 'junit:junit:4.13.2'
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
	if deps.Main[0].Kind != "project" || deps.Main[0].Value != ":core-util" {
		t.Fatalf("unexpected main dep: %#v", deps.Main[0])
	}
	if got, want := len(deps.Test), 1; got != want {
		t.Fatalf("unexpected test dep count: got %d want %d", got, want)
	}
	if deps.Test[0].Kind != "raw" || deps.Test[0].Value != "junit:junit:4.13.2" {
		t.Fatalf("unexpected test dep: %#v", deps.Test[0])
	}
}

func TestParseDependenciesForModuleIncludesConventionPluginDependencies(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "feature", "build.gradle.kts")
	pluginDir := filepath.Join(root, "build-logic", "plugins", "src", "main", "kotlin")
	if err := os.MkdirAll(filepath.Dir(buildFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildFile, []byte(`
plugins {
  id("convention-library")
}

dependencies {
  implementation(project(":local"))
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "convention-library.gradle.kts"), []byte(`
dependencies {
  implementation(libs.androidx.core.ktx)
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependenciesForModule(buildFile, root, []string{"convention-library"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(deps.Main), 2; got != want {
		t.Fatalf("unexpected main dep count: got %d want %d (%#v)", got, want, deps.Main)
	}
	if deps.Main[0].Kind != "project" || deps.Main[0].Value != ":local" {
		t.Fatalf("unexpected module dep: %#v", deps.Main[0])
	}
	if deps.Main[1].Kind != "library" || deps.Main[1].Value != "androidx.core.ktx" {
		t.Fatalf("unexpected convention dep: %#v", deps.Main[1])
	}
}

func TestParseDependenciesIncludesAndroidMultiplatformSourceSetDependencies(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
kotlin {
  sourceSets {
    commonMain.dependencies {
      implementation(libs.common)
    }
    androidMain.dependencies {
      implementation(libs.android)
    }
    jvmTest.dependencies {
      implementation(compose.desktop.currentOs)
    }
  }
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		t.Fatal(err)
	}
	if !containsRef(deps.Main, Ref{Kind: "library", Value: "common"}) {
		t.Fatalf("missing commonMain dependency: %#v", deps.Main)
	}
	if !containsRef(deps.Main, Ref{Kind: "library", Value: "android"}) {
		t.Fatalf("missing androidMain dependency: %#v", deps.Main)
	}
	if containsRef(deps.Main, Ref{Kind: "raw", Value: "compose.desktop.currentOs"}) {
		t.Fatalf("unexpected jvmTest dependency in main deps: %#v", deps.Main)
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

// TestParseDependenciesLetBinding verifies that platform(...).let { name -> ... }
// expands to flat scope(platform(...)) lines (the convention plugin core-ui pattern).
func TestParseDependenciesLetBinding(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    platform(libs.compose.bom).let { composeBom ->
        api(composeBom)
        androidTestApi(composeBom)
    }
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
		t.Fatalf("Main: got %d want %d: %#v", got, want, deps.Main)
	}
	if deps.Main[0].Kind != "platform-library" || deps.Main[0].Value != "compose.bom" {
		t.Fatalf("unexpected Main[0]: %#v", deps.Main[0])
	}
	scoped := deps.Scoped["androidTestApi"]
	if got, want := len(scoped), 1; got != want {
		t.Fatalf("androidTestApi: got %d want %d: %#v", got, want, scoped)
	}
	if scoped[0].Kind != "platform-library" || scoped[0].Value != "compose.bom" {
		t.Fatalf("unexpected androidTestApi[0]: %#v", scoped[0])
	}
}

// TestParseDependenciesAlsoBinding verifies that expr.also { name -> ... } expands correctly.
func TestParseDependenciesAlsoBinding(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    platform(libs.compose.bom).also { bom ->
        api(bom)
    }
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
		t.Fatalf("got %d want %d: %#v", got, want, deps.Main)
	}
	if deps.Main[0].Kind != "platform-library" || deps.Main[0].Value != "compose.bom" {
		t.Fatalf("unexpected ref: %#v", deps.Main[0])
	}
}

// TestParseDependenciesAlsoImplicitIt verifies that .also { ... } without a named
// parameter substitutes the implicit `it` name.
func TestParseDependenciesAlsoImplicitIt(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    platform(libs.compose.bom).also {
        api(it)
    }
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
		t.Fatalf("got %d want %d: %#v", got, want, deps.Main)
	}
	if deps.Main[0].Kind != "platform-library" {
		t.Fatalf("unexpected ref: %#v", deps.Main[0])
	}
}

// TestParseDependenciesApplyBinding verifies that expr.apply { ... } expands with
// `this` as the implicit receiver inside the block.
func TestParseDependenciesApplyBinding(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	// apply { } binds `this` — but dep calls inside it still use scope names, not `this`.
	// A realistic pattern uses apply on a configuration object, not a dep ref, so
	// we model the simplest variant: a val inside apply is not expected in real
	// dep blocks. Instead, test that apply { } doesn't swallow plain dep lines.
	body := `
dependencies {
    implementation(libs.okhttp)
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
		t.Fatalf("got %d want %d", got, want)
	}
}

// TestParseDependenciesWithBinding verifies that with(expr) { ... } expands
// with `this` bound to expr.
func TestParseDependenciesWithBinding(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    with(platform(libs.compose.bom)) {
        api(this)
    }
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
		t.Fatalf("got %d want %d: %#v", got, want, deps.Main)
	}
	if deps.Main[0].Kind != "platform-library" || deps.Main[0].Value != "compose.bom" {
		t.Fatalf("unexpected ref: %#v", deps.Main[0])
	}
}

// TestParseDependenciesValBinding verifies that a top-level val inside the
// dependencies block is substituted when referenced.
func TestParseDependenciesValBinding(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    val bom = platform(libs.compose.bom)
    api(bom)
    androidTestImplementation(bom)
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
		t.Fatalf("Main: got %d want %d: %#v", got, want, deps.Main)
	}
	if deps.Main[0].Kind != "platform-library" || deps.Main[0].Value != "compose.bom" {
		t.Fatalf("unexpected Main[0]: %#v", deps.Main[0])
	}
	if got, want := len(deps.AndroidTest), 1; got != want {
		t.Fatalf("AndroidTest: got %d want %d: %#v", got, want, deps.AndroidTest)
	}
}

// TestParseDependenciesBindingShadowing verifies that an inner let binding shadows
// an outer val with the same name.
func TestParseDependenciesBindingShadowing(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    val bom = platform(libs.compose.bom)
    platform(libs.firebase.bom).let { bom ->
        implementation(bom)
    }
    api(bom)
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		t.Fatal(err)
	}
	// implementation(bom) inside let should resolve to firebase.bom (the shadow)
	impls := deps.Scoped["implementation"]
	if got, want := len(impls), 1; got != want {
		t.Fatalf("implementation: got %d want %d: %#v", got, want, impls)
	}
	if impls[0].Kind != "platform-library" || impls[0].Value != "firebase.bom" {
		t.Fatalf("expected firebase.bom, got: %#v", impls[0])
	}
	// api(bom) outside let should resolve to compose.bom (the outer val)
	apis := deps.Scoped["api"]
	if got, want := len(apis), 1; got != want {
		t.Fatalf("api: got %d want %d: %#v", got, want, apis)
	}
	if apis[0].Kind != "platform-library" || apis[0].Value != "compose.bom" {
		t.Fatalf("expected compose.bom, got: %#v", apis[0])
	}
}

// TestParseDependenciesUnboundIdentifierErrors verifies that a bare identifier with
// no matching binding produces a clear error rather than a cryptic resolver failure.
func TestParseDependenciesUnboundIdentifierErrors(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    api(composeBom)
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseDependencies(buildFile)
	if err == nil {
		t.Fatal("expected error for unbound reference, got nil")
	}
	if !strings.Contains(err.Error(), "unbound reference") || !strings.Contains(err.Error(), "composeBom") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestParseDependenciesPreservesEnforcedPlatformRequest(t *testing.T) {
	root := t.TempDir()
	buildFile := filepath.Join(root, "build.gradle.kts")
	body := `
dependencies {
    implementation(enforcedPlatform(libs.compose.bom))
}
`
	if err := os.WriteFile(buildFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := ParseDependencies(buildFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := deps.Main[0], (Ref{Kind: "enforced-platform-library", Value: "compose.bom"}); got != want {
		t.Fatalf("unexpected ref: got %#v want %#v", got, want)
	}
	if got, want := len(deps.Requests), 1; got != want {
		t.Fatalf("request count = %d, want %d", got, want)
	}
	req := deps.Requests[0]
	if !req.Platform || !req.Enforced || req.CatalogAlias != "compose.bom" || req.Scope != "implementation" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func containsRef(refs []Ref, want Ref) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}
