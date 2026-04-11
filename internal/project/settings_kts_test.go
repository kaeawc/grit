package project

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestParseSettingsKTSRepositories(t *testing.T) {
	body := `
pluginManagement {
  repositories {
    google {
      content {
        includeGroupByRegex("com\\.android.*")
        includeGroupByRegex("androidx.*")
      }
    }
    mavenCentral()
    gradlePluginPortal()
  }
}

dependencyResolutionManagement {
  repositories {
    mavenLocal()
    google()
    mavenCentral()
    jcenter {
      content {
        includeGroup("legacy.example")
      }
    }
    maven("https://example.com/maven") {
      name = "ExampleRepo"
    }
    maven {
      url = uri("https://plugins.example.com/releases")
      content {
        includeGroupByRegex("com\\.example.*")
      }
    }
  }
}

rootProject.name = "Sample"
include(":app", ":lib")
`
	model := parseSettingsKTS(body)
	if model.Name != "Sample" {
		t.Fatalf("expected root project name Sample, got %q", model.Name)
	}
	if len(model.Includes) != 2 || model.Includes[0] != ":app" || model.Includes[1] != ":lib" {
		t.Fatalf("unexpected includes: %#v", model.Includes)
	}
	if len(model.Repositories) != 9 {
		t.Fatalf("expected 9 repositories, got %d: %#v", len(model.Repositories), model.Repositories)
	}
	var googleRepo Repository
	foundGoogle := false
	foundCustom := false
	foundJCenter := false
	foundBlockMaven := false
	foundMavenLocal := false
	for _, repo := range model.Repositories {
		if repo.Scope == "plugin" && repo.Name == "google" {
			foundGoogle = true
			googleRepo = repo
		}
		if repo.Scope == "dependency" && repo.Kind == "mavenLocal" {
			foundMavenLocal = repo.Priority == 3 && repo.Origin == "settings" && repo.OfflineAllowed
		}
		if repo.Scope == "dependency" && repo.Name == "ExampleRepo" && repo.URL == "https://example.com/maven/" {
			foundCustom = repo.Priority == 7 && repo.Origin == "settings" && !repo.OfflineAllowed
		}
		if repo.Scope == "dependency" && repo.Name == "jcenter" && repo.URL == "https://jcenter.bintray.com/" {
			foundJCenter = len(repo.IncludeGroups) == 1 && repo.IncludeGroups[0] == "legacy.example" && repo.Priority == 6 && repo.Origin == "settings"
		}
		if repo.Scope == "dependency" && repo.Name == "https://plugins.example.com/releases/" && repo.URL == "https://plugins.example.com/releases/" {
			foundBlockMaven = len(repo.IncludeGroupRegex) == 1 && strings.Contains(repo.IncludeGroupRegex[0], "com") && strings.Contains(repo.IncludeGroupRegex[0], "example") && repo.Priority == 8 && repo.Origin == "settings"
		}
	}
	if !foundGoogle {
		t.Fatalf("expected pluginManagement google repo in %#v", model.Repositories)
	}
	if len(googleRepo.IncludeGroupRegex) != 2 {
		t.Fatalf("expected google repo content filters, got %#v", googleRepo)
	}
	if !foundCustom {
		t.Fatalf("expected named custom maven repo in %#v", model.Repositories)
	}
	if !foundJCenter {
		t.Fatalf("expected jcenter repository with content filters in %#v", model.Repositories)
	}
	if !foundBlockMaven {
		t.Fatalf("expected block-style maven repository in %#v", model.Repositories)
	}
	if !foundMavenLocal {
		t.Fatalf("expected mavenLocal repository metadata in %#v", model.Repositories)
	}
}

func TestParseSettingsKTSRepositoriesResolvesGradleProperties(t *testing.T) {
	body := `
dependencyResolutionManagement {
  repositories {
    maven(findProperty("customRepoUrl")!!)
  }
}
`
	model := parseSettingsKTSWithProperties(body, map[string]string{
		"customRepoUrl": "https://repo.example.com/properties",
	})
	if len(model.Repositories) != 1 {
		t.Fatalf("expected one repository, got %#v", model.Repositories)
	}
	repo := model.Repositories[0]
	if repo.URL != "https://repo.example.com/properties/" {
		t.Fatalf("expected property-backed repository url, got %#v", repo)
	}
}

func TestLoadCollectsRepositoriesFromSettingsRootAndModuleBuildFiles(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "RepoMerge"
dependencyResolutionManagement {
  repositories {
    mavenCentral()
  }
}
include(":app")
`)
	testutil.WriteFile(t, root, "gradle.properties", `
MODULE_REPO_URL=https://modules.example.com/maven
`)
	testutil.WriteFile(t, root, "build.gradle.kts", `
plugins {}

allprojects {
  repositories {
    maven {
      url = uri("https://root.example.com/maven")
    }
  }
}
`)
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
plugins {
  alias(libs.plugins.android.application)
}

repositories {
  maven(findProperty("MODULE_REPO_URL")!!)
}

android {
  namespace = "com.example.repo"
  compileSdk = 34
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"https://repo1.maven.org/maven2/":    false,
		"https://root.example.com/maven/":    false,
		"https://modules.example.com/maven/": false,
	}
	for _, repo := range prj.Repositories {
		if _, ok := want[repo.URL]; ok {
			want[repo.URL] = true
		}
	}
	for url, found := range want {
		if !found {
			t.Fatalf("expected repository %s in %#v", url, prj.Repositories)
		}
	}
}

func TestLoadPreservesRepositoryOrderOriginAndOfflineAllowance(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "RepoOrder"
dependencyResolutionManagement {
  repositories {
    mavenLocal()
    google()
  }
}
include(":app")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", `
plugins {}

allprojects {
  repositories {
    maven {
      url = uri("https://root.example.com/maven")
    }
  }
}
`)
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
plugins {}

repositories {
  maven {
    url = uri("file:///tmp/module-repo")
  }
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(prj.Repositories) != 4 {
		t.Fatalf("expected 4 repositories, got %d: %#v", len(prj.Repositories), prj.Repositories)
	}
	checks := []struct {
		idx     int
		name    string
		url     string
		origin  string
		offline bool
	}{
		{idx: 0, name: "mavenLocal", origin: "settings", offline: true},
		{idx: 1, name: "google", origin: "settings", offline: false},
		{idx: 2, name: "https://root.example.com/maven", url: "https://root.example.com/maven/", origin: "root-build", offline: false},
		{idx: 3, name: "file:///tmp/module-repo", url: "file:///tmp/module-repo/", origin: "module-build", offline: true},
	}
	for _, check := range checks {
		repo := prj.Repositories[check.idx]
		if repo.Name != check.name {
			t.Fatalf("repo[%d] name = %q, want %q", check.idx, repo.Name, check.name)
		}
		if check.url != "" && repo.URL != check.url {
			t.Fatalf("repo[%d] url = %q, want %q", check.idx, repo.URL, check.url)
		}
		if repo.Priority != check.idx {
			t.Fatalf("repo[%d] priority = %d, want %d", check.idx, repo.Priority, check.idx)
		}
		if repo.Origin != check.origin {
			t.Fatalf("repo[%d] origin = %q, want %q", check.idx, repo.Origin, check.origin)
		}
		if repo.OfflineAllowed != check.offline {
			t.Fatalf("repo[%d] offlineAllowed = %v, want %v", check.idx, repo.OfflineAllowed, check.offline)
		}
	}
}

func TestLoadRespectsProjectDirRemapsFromSettings(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "Remap"
include(":feature")
project(":feature").projectDir = file("nested/feature-lib")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", `plugins {}`)
	testutil.WriteFile(t, root, "nested/feature-lib/build.gradle.kts", `
plugins {
  id("com.android.library")
}

android {
  namespace = "com.example.feature"
  compileSdk = 34
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	mod := prj.FindModule(":feature")
	if mod == nil {
		t.Fatalf("expected :feature module, got %#v", prj.Modules)
	}
	if mod.Dir != filepath.Join(root, "nested", "feature-lib") {
		t.Fatalf("unexpected module dir: %#v", mod)
	}
	if mod.BuildFile != filepath.Join(root, "nested", "feature-lib", "build.gradle.kts") {
		t.Fatalf("unexpected module build file: %#v", mod)
	}
}

func TestLoadSupportsGroovyModuleBuildFiles(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "GroovyModules"
include(":feature")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", `plugins {}`)
	testutil.WriteFile(t, root, "feature/build.gradle", `
plugins {
  id 'signal-library'
}

android {
  namespace 'com.example.feature'
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	mod := prj.FindModule(":feature")
	if mod == nil {
		t.Fatalf("expected :feature module, got %#v", prj.Modules)
	}
	if mod.BuildFile != filepath.Join(root, "feature", "build.gradle") {
		t.Fatalf("unexpected module build file: %#v", mod)
	}
	if mod.Type != "android-library" {
		t.Fatalf("unexpected module type: %#v", mod)
	}
}

func TestLoadSupportsMultipleVersionCatalogs(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "CatalogTest"
`)
	testutil.WriteFile(t, root, "build.gradle.kts", `
plugins {}
`)
	testutil.WriteFile(t, root, "gradle/aaa.versions.toml", `
[versions]
alpha = "1"
`)
	testutil.WriteFile(t, root, "gradle/libs.versions.toml", `
[versions]
beta = "2"
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(prj.VersionCatalogs), 2; got != want {
		t.Fatalf("expected %d catalogs, got %d (%#v)", want, got, prj.VersionCatalogs)
	}
	if filepath.Base(prj.VersionCatalog) != "libs.versions.toml" {
		t.Fatalf("expected primary catalog to be libs.versions.toml, got %q", prj.VersionCatalog)
	}
	if prj.VersionCatalogData["alpha"] != "1" || prj.VersionCatalogData["beta"] != "2" {
		t.Fatalf("expected merged version data, got %#v", prj.VersionCatalogData)
	}
}

func TestParseBuildTypesExposeOptimizationModel(t *testing.T) {
	root := t.TempDir()
	body := `
plugins {
  alias(libs.plugins.android.application)
}

android {
  namespace = "com.example.app"
  compileSdk = 34
  defaultConfig {
    minSdk = 24
    targetSdk = 34
  }
  buildTypes {
    debug {
      isMinifyEnabled = false
      isShrinkResources = false
    }
    release {
      isMinifyEnabled = true
      isShrinkResources = true
      signingConfig = signingConfigs.getByName("debug")
      proguardFiles(
        getDefaultProguardFile("proguard-android-optimize.txt"),
        "proguard-rules.pro"
      )
      packageOptimizations {
        package("com.example.placeholder") {
          minifyEnabled = true
          shrinkResources = false
          note = "future-package-scoped-optimization"
        }
      }
    }
  }
}
`
	mod := &Module{
		Path:       ":app",
		Dir:        root,
		BuildFile:  filepath.Join(root, "build.gradle.kts"),
		BuildTypes: parseBuildTypes(body, root),
		SigningConfigs: map[string]SigningConfig{
			"debug": {Name: "debug"},
		},
	}

	release := mod.Variant("release")
	if !release.Optimization.MinifyEnabled || !release.Optimization.ShrinkResources {
		t.Fatalf("expected release optimization flags to be true, got %#v", release.Optimization)
	}
	if len(release.Optimization.PackageOptimizations) != 1 {
		t.Fatalf("expected one package optimization placeholder, got %#v", release.Optimization.PackageOptimizations)
	}
	if release.Optimization.PackageOptimizations[0].PackageName != "com.example.placeholder" {
		t.Fatalf("unexpected package optimization placeholder: %#v", release.Optimization.PackageOptimizations[0])
	}
	if release.Optimization.PackageOptimizations[0].Note != "future-package-scoped-optimization" {
		t.Fatalf("expected placeholder note to be preserved, got %#v", release.Optimization.PackageOptimizations[0])
	}
}

func TestLoadParsesAppDslMetadataAndSigningFallback(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", `
rootProject.name = "PlaygroundLike"
include(":app")
`)
	testutil.WriteFile(t, root, "build.gradle.kts", `plugins {}`)
	testutil.WriteFile(t, root, "gradle.properties", `
RELEASE_KEYSTORE_PATH=keys/release.jks
RELEASE_KEYSTORE_PASSWORD=store-pass
RELEASE_KEY_ALIAS=release-key
RELEASE_KEY_PASSWORD=key-pass
`)
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
plugins {
  alias(libs.plugins.android.application)
}

val releaseStoreFilePath: String? =
    System.getenv("RELEASE_KEYSTORE_PATH") ?: findProperty("RELEASE_KEYSTORE_PATH") as String?
val releaseStorePassword: String? =
    System.getenv("RELEASE_KEYSTORE_PASSWORD")
        ?: findProperty("RELEASE_KEYSTORE_PASSWORD") as String?
val releaseKeyAlias: String? =
    System.getenv("RELEASE_KEY_ALIAS") ?: findProperty("RELEASE_KEY_ALIAS") as String?
val releaseKeyPassword: String? =
    System.getenv("RELEASE_KEY_PASSWORD") ?: findProperty("RELEASE_KEY_PASSWORD") as String?
val releaseStoreFile: File? =
    releaseStoreFilePath?.let { path ->
      val file = File(path)
      if (file.isAbsolute) file else rootProject.file(path)
    }
val hasReleaseSigning = true

android {
  namespace = "dev.example.app"
  defaultConfig {
    applicationId = "dev.example.app"
    versionCode = 7
    versionName = "1.2.3"
  }
  signingConfigs {
    create("release") {
      storeFile = releaseStoreFile
      storePassword = releaseStorePassword
      keyAlias = releaseKeyAlias
      keyPassword = releaseKeyPassword
    }
  }
  buildTypes {
    debug {
      signingConfig =
          if (hasReleaseSigning) {
            signingConfigs.getByName("release")
          } else {
            signingConfigs.getByName("debug")
          }
    }
  }
}

tasks.withType<org.jetbrains.kotlin.gradle.tasks.KotlinCompile>().configureEach {
  compilerOptions {
    freeCompilerArgs =
        listOf(
            "-opt-in=kotlinx.coroutines.ExperimentalCoroutinesApi",
            "-opt-in=kotlinx.coroutines.FlowPreview",
        )
  }
}

lint {
  disable += "UnsafeOptInUsageError"
}
`)

	prj, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(prj.Modules) != 1 {
		t.Fatalf("expected one module, got %#v", prj.Modules)
	}
	mod := prj.Modules[0]
	if mod.ApplicationID != "dev.example.app" || mod.VersionCode != "7" || mod.VersionName != "1.2.3" {
		t.Fatalf("unexpected app metadata: %#v", mod)
	}
	if len(mod.KotlinFreeCompilerArgs) != 2 {
		t.Fatalf("expected free compiler args, got %#v", mod.KotlinFreeCompilerArgs)
	}
	if len(mod.LintDisabledChecks) != 1 || mod.LintDisabledChecks[0] != "UnsafeOptInUsageError" {
		t.Fatalf("unexpected lint disables: %#v", mod.LintDisabledChecks)
	}
	cfg := mod.SigningConfigs["release"]
	if cfg.StoreFile != filepath.Join(root, "keys", "release.jks") {
		t.Fatalf("unexpected store file: %#v", cfg)
	}
	if cfg.StorePassword != "store-pass" || cfg.KeyAlias != "release-key" || cfg.KeyPassword != "key-pass" {
		t.Fatalf("unexpected signing config: %#v", cfg)
	}
	if got := mod.Variant("debug").SigningConfig; got != "debug|release" {
		t.Fatalf("expected fallback signing config ordering, got %q", got)
	}
}

func TestDetectModuleTypeSupportsDirectPluginIDs(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{body: `plugins { id("com.android.application") }`, want: "android-application"},
		{body: `plugins { id("signal-sample-app") }`, want: "android-application"},
		{body: `plugins { id 'signal-sample-app' }`, want: "android-application"},
		{body: `plugins { id("com.android.library") }`, want: "android-library"},
		{body: `plugins { id("signal-library") }`, want: "android-library"},
		{body: `plugins { id 'signal-library' }`, want: "android-library"},
		{body: `plugins { id("org.jetbrains.kotlin.jvm") }`, want: "jvm-library"},
	}
	for _, tc := range tests {
		if got := detectModuleType(tc.body); got != tc.want {
			t.Fatalf("detectModuleType(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}
