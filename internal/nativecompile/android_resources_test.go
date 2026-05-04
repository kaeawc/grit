package nativecompile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestEstimatedAAPT2ArgSize(t *testing.T) {
	got := estimatedAAPT2ArgSize([]string{"/tmp/a.flat", "/tmp/b.flat"})
	if got <= 0 {
		t.Fatalf("expected positive arg size, got %d", got)
	}
}

func TestCompactAAPT2InputPathsStagesLongLists(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i := 0; i < 4000; i++ {
		path := filepath.Join(dir, "very", "long", "resource", "path", "segment", "compiled", "file", "name", "that", "keeps", "going")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		file := filepath.Join(path, fmt.Sprintf("res-%04d.flat", i))
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		files = append(files, file)
	}
	got, cleanup, err := compactAAPT2InputPaths(files)
	if err != nil {
		t.Fatalf("compactAAPT2InputPaths: %v", err)
	}
	defer cleanup()
	if len(got) != len(files) {
		t.Fatalf("expected %d staged files, got %d", len(files), len(got))
	}
	if got[0] == files[0] {
		t.Fatalf("expected staged short path, got original %q", got[0])
	}
	if _, err := os.Lstat(got[0]); err != nil {
		t.Fatalf("expected staged symlink to exist: %v", err)
	}
}

func TestResourceRootsForVariantIncludesFlavorAndBuildTypeOverlays(t *testing.T) {
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
	got := resourceRootsForVariant(mod, "freeDebug")
	want := []string{
		filepath.Join(root, "src", "debug", "res"),
		filepath.Join(root, "src", "free", "res"),
		filepath.Join(root, "src", "freeDebug", "res"),
		filepath.Join(root, "src", "main", "res"),
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected resource roots: %#v", got)
	}
	for _, path := range want {
		found := false
		for _, item := range got {
			if item == path {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing resource root %q in %#v", path, got)
		}
	}
}

func TestManifestForPackagingMergesVariantManifestOverlays(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:             ":app",
		Dir:              root,
		Type:             "android-application",
		Namespace:        "com.example.app",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	for _, path := range []string{
		filepath.Join(root, "src", "main", "AndroidManifest.xml"),
		filepath.Join(root, "src", "free", "AndroidManifest.xml"),
		filepath.Join(root, "src", "debug", "AndroidManifest.xml"),
		filepath.Join(root, "src", "freeDebug", "AndroidManifest.xml"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		var body string
		switch filepath.Base(filepath.Dir(path)) {
		case "main":
			body = `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.base"><uses-sdk android:minSdkVersion="24"/><application android:label="@string/baseLabel"><activity android:name=".MainActivity"/></application></manifest>`
		case "free":
			body = `<manifest xmlns:android="http://schemas.android.com/apk/res/android"><application android:label="@string/freeLabel"/></manifest>`
		case "debug":
			body = `<manifest xmlns:android="http://schemas.android.com/apk/res/android"><application android:debuggable="true"/></manifest>`
		case "freeDebug":
			body = `<manifest xmlns:android="http://schemas.android.com/apk/res/android"><application><activity android:name=".FreeDebugActivity"/></application></manifest>`
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := manifestForPackaging(mod, "freeDebug")
	if err != nil {
		t.Fatal(err)
	}
	if got == filepath.Join(root, "src", "freeDebug", "AndroidManifest.xml") {
		t.Fatalf("expected generated merged manifest, got source path %q", got)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		`package="com.example.base"`,
		`android:label="@string/freeLabel"`,
		`android:debuggable="true"`,
		`android:name=".MainActivity"`,
		`android:name=".FreeDebugActivity"`,
		`android:minSdkVersion="24"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("merged manifest missing %q:\n%s", want, body)
		}
	}
}

func TestManifestForPackagingAppliesPlaceholdersAndToolsNode(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:                      ":app",
		Dir:                       root,
		Type:                      "android-application",
		Namespace:                 "com.example.app",
		ApplicationID:             "com.example.app",
		TestInstrumentationRunner: "androidx.test.runner.AndroidJUnitRunner",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	debugManifest := filepath.Join(root, "src", "debug", "AndroidManifest.xml")
	for _, path := range []string{mainManifest, debugManifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools" package="${applicationId}"><application android:name="${applicationId}.App"><provider android:name="com.example.Provider" android:authorities="${applicationId}.provider"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debugManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools"><application><provider android:name="com.example.Provider" tools:node="remove"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackaging(mod, "debug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `package="com.example.app"`) || !strings.Contains(body, `android:name="com.example.app.App"`) {
		t.Fatalf("expected placeholders to be expanded:\n%s", body)
	}
	if strings.Contains(body, `provider`) || strings.Contains(body, `tools:`) {
		t.Fatalf("expected provider removal and stripped tools attrs:\n%s", body)
	}
}

func TestManifestForPackagingAppliesToolsReplaceAndRemoveAttrs(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:          ":app",
		Dir:           root,
		Type:          "android-application",
		Namespace:     "com.example.app",
		ApplicationID: "com.example.app",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	debugManifest := filepath.Join(root, "src", "debug", "AndroidManifest.xml")
	for _, path := range []string{mainManifest, debugManifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:label="@string/base" android:allowBackup="true"><provider android:name="com.example.Provider" android:authorities="com.example.app.base" android:exported="false" android:permission="com.example.OLD"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debugManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools"><application android:label="@string/debug" tools:replace="android:label" tools:remove="android:allowBackup"><provider android:name="com.example.Provider" android:authorities="com.example.app.debug" android:exported="true" tools:replace="android:authorities,android:exported" tools:remove="android:permission"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackaging(mod, "debug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		`android:label="@string/debug"`,
		`android:authorities="com.example.app.debug"`,
		`android:exported="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected merged manifest to contain %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		`android:allowBackup=`,
		`android:permission=`,
		`tools:replace=`,
		`tools:remove=`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("expected merged manifest to omit %q:\n%s", forbidden, body)
		}
	}
}

func TestManifestForPackagingHonorsToolsSelector(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:          ":app",
		Dir:           root,
		Type:          "android-application",
		Namespace:     "com.example.app",
		ApplicationID: "com.example.app",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	debugManifest := filepath.Join(root, "src", "debug", "AndroidManifest.xml")
	for _, path := range []string{mainManifest, debugManifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:label="@string/base" android:allowBackup="true"/></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debugManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools"><application android:label="@string/debug" tools:replace="android:label" tools:remove="android:allowBackup" tools:selector="com.example.other"/></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackaging(mod, "debug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `android:label="@string/debug"`) {
		t.Fatalf("expected normal merge to keep higher-priority label:\n%s", body)
	}
	if !strings.Contains(body, `android:allowBackup="true"`) {
		t.Fatalf("expected selector mismatch to skip tools:remove:\n%s", body)
	}
}

func TestManifestForPackagingHonorsToolsSelectorAgainstImportedProvenance(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:          ":app",
		Dir:           root,
		Type:          "android-application",
		Namespace:     "com.example.app",
		ApplicationID: "com.example.app",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	debugManifest := filepath.Join(root, "src", "debug", "AndroidManifest.xml")
	for _, path := range []string{mainManifest, debugManifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.base"><application><provider android:name="com.example.Provider" android:authorities="com.example.base.provider"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debugManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools" package="com.example.app"><application><provider android:name="com.example.Provider" tools:node="remove" tools:selector="com.example.base"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackaging(mod, "debug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if strings.Contains(body, `com.example.base.provider`) {
		t.Fatalf("expected selector to match imported provenance and remove provider:\n%s", body)
	}
}

func TestManifestForPackagingEnforcesToolsStrict(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:          ":app",
		Dir:           root,
		Type:          "android-application",
		Namespace:     "com.example.app",
		ApplicationID: "com.example.app",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	debugManifest := filepath.Join(root, "src", "debug", "AndroidManifest.xml")
	for _, path := range []string{mainManifest, debugManifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:label="@string/base"/></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debugManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools"><application android:label="@string/debug" tools:strict="android:label"/></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := manifestForPackaging(mod, "debug")
	if err == nil || !strings.Contains(err.Error(), "tools:strict conflict") {
		t.Fatalf("expected tools:strict conflict, got %v", err)
	}
}

func TestManifestForPackagingHonorsToolsNodeMergeOnlyAttributes(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:          ":app",
		Dir:           root,
		Type:          "android-application",
		Namespace:     "com.example.app",
		ApplicationID: "com.example.app",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	debugManifest := filepath.Join(root, "src", "debug", "AndroidManifest.xml")
	for _, path := range []string{mainManifest, debugManifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:label="@string/base"><meta-data android:name="keep.me" android:value="1"/><provider android:name="com.example.Provider" android:authorities="com.example.app.provider"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debugManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools"><application android:label="@string/debug" tools:node="mergeOnlyAttributes"><meta-data android:name="drop.me" android:value="2"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackaging(mod, "debug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `android:label="@string/debug"`) {
		t.Fatalf("expected application attrs to merge:\n%s", body)
	}
	if !strings.Contains(body, `android:name="keep.me"`) || !strings.Contains(body, `android:name="com.example.Provider"`) {
		t.Fatalf("expected existing children to be preserved:\n%s", body)
	}
	if strings.Contains(body, `android:name="drop.me"`) {
		t.Fatalf("expected mergeOnlyAttributes to ignore incoming children:\n%s", body)
	}
}

func TestManifestForPackagingHonorsChildToolsNodeMergeOnlyAttributes(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:          ":app",
		Dir:           root,
		Type:          "android-application",
		Namespace:     "com.example.app",
		ApplicationID: "com.example.app",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	debugManifest := filepath.Join(root, "src", "debug", "AndroidManifest.xml")
	for _, path := range []string{mainManifest, debugManifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application><provider android:name="com.example.Provider" android:authorities="com.example.app.provider" android:exported="false"><grant-uri-permission android:path="/main"/></provider></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debugManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools"><application><provider android:name="com.example.Provider" android:exported="true" tools:node="mergeOnlyAttributes"><grant-uri-permission android:path="/debug"/></provider></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackaging(mod, "debug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `android:exported="true"`) {
		t.Fatalf("expected provider attrs to merge:\n%s", body)
	}
	if !strings.Contains(body, `android:path="/main"`) {
		t.Fatalf("expected existing provider child to remain:\n%s", body)
	}
	if strings.Contains(body, `android:path="/debug"`) {
		t.Fatalf("expected mergeOnlyAttributes to ignore incoming provider children:\n%s", body)
	}
}

func TestManifestForPackagingUsesProjectPlaceholderSources(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:          ":app",
		Dir:           root,
		Type:          "android-application",
		Namespace:     "com.example.app",
		ApplicationID: "com.example.app",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	prj := &project.Project{
		RootDir: root,
		GradleProperties: map[string]string{
			"manifestPlaceholders.hostName": "api.example.test",
			"CUSTOM_LABEL":                  "FromGradleProps",
		},
		VersionCatalogData: map[string]string{
			"appVersion": "9.9.9",
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	if err := os.MkdirAll(filepath.Dir(mainManifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:label="${CUSTOM_LABEL}"><provider android:name="com.example.Provider" android:authorities="${hostName}" android:initOrder="${appVersion}"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackagingForProject(prj, mod, "debug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		`android:label="FromGradleProps"`,
		`android:authorities="api.example.test"`,
		`android:initOrder="9.9.9"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected project placeholder %q in merged manifest:\n%s", want, body)
		}
	}
}

func TestManifestForPackagingSupportsAdditionalDSLPlaceholderForms(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:             ":app",
		Dir:              root,
		BuildFile:        filepath.Join(root, "build.gradle.kts"),
		Type:             "android-application",
		Namespace:        "com.example.app",
		ApplicationID:    "com.example.app",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	prj := &project.Project{
		RootDir: root,
		GradleProperties: map[string]string{
			"globalHost":     "global.example.test",
			"flavorHost":     "free.example.test",
			"buildTypeHost":  "debug.example.test",
			"providerHost":   "provider.example.test",
			"projectHost":    "project.example.test",
			"fallbackEnvVar": "prop-fallback.example.test",
		},
	}
	if err := os.Setenv("HOST_FROM_ENV", "env.example.test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("HOST_FROM_ENV") })
	if err := os.MkdirAll(filepath.Join(root, "src", "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod.BuildFile, []byte(`
val providerHost = providers.gradleProperty("providerHost").orNull
val envHost = providers.environmentVariable("HOST_FROM_ENV").orNull
val projectHost: String? = project.findProperty("projectHost") as String?
val envOrProp = providers.environmentVariable("MISSING_ENV").orElse(providers.gradleProperty("fallbackEnvVar")).orNull
android {
  defaultConfig {
    manifestPlaceholders = mapOf(
      "apiHost" to "default.example.test",
      "providerHost" to providerHost,
      "envHost" to envHost,
      "projectHost" to projectHost,
      "envOrProp" to envOrProp,
    )
  }
  productFlavors {
    free {
      manifestPlaceholders.put("apiHost", "free.example.test")
      manifestPlaceholders.putAll(mapOf(
        "flavorOnly" to "flavor-only.example.test",
      ))
    }
  }
  buildTypes {
    debug {
      manifestPlaceholders.put("apiHost", "debug.example.test")
    }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:label="${apiHost}" android:name="${providerHost}"><provider android:name="${projectHost}" android:authorities="${envHost}" android:initOrder="${envOrProp}"/><meta-data android:name="flavorOnly" android:value="${flavorOnly}"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackagingForProject(prj, mod, "freeDebug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		`android:label="debug.example.test"`,
		`android:name="provider.example.test"`,
		`android:name="project.example.test"`,
		`android:authorities="env.example.test"`,
		`android:initOrder="prop-fallback.example.test"`,
		`android:value="flavor-only.example.test"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected placeholder form output %q in merged manifest:\n%s", want, body)
		}
	}
}

func TestManifestForPackagingBuiltinKeysOverrideDSLPlaceholderLayers(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:             ":app",
		Dir:              root,
		BuildFile:        filepath.Join(root, "build.gradle.kts"),
		Type:             "android-application",
		Namespace:        "com.example.app",
		ApplicationID:    "com.example.app",
		DefaultConfig:    project.DefaultConfig{ApplicationID: "com.example.app"},
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier", ApplicationIDSuffix: ".free"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug", ApplicationIDSuffix: ".debug"},
		},
	}
	prj := &project.Project{RootDir: root}
	if err := os.MkdirAll(filepath.Join(root, "src", "main"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod.BuildFile, []byte(`
android {
  defaultConfig {
    manifestPlaceholders = mapOf("applicationId" to "wrong.default")
  }
  productFlavors {
    free {
      manifestPlaceholders.put("applicationId", "wrong.flavor")
    }
  }
  buildTypes {
    debug {
      manifestPlaceholders.put("applicationId", "wrong.debug")
    }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="${applicationId}"/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackagingForProject(prj, mod, "freeDebug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `package="com.example.app.free.debug"`) {
		t.Fatalf("expected built-in applicationId to win over DSL placeholder layers:\n%s", body)
	}
}

func TestManifestForPackagingWritesMergeReport(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:          ":app",
		Dir:           root,
		Type:          "android-application",
		Namespace:     "com.example.app",
		ApplicationID: "com.example.app",
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	mainManifest := filepath.Join(root, "src", "main", "AndroidManifest.xml")
	debugManifest := filepath.Join(root, "src", "debug", "AndroidManifest.xml")
	for _, path := range []string{mainManifest, debugManifest} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mainManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:allowBackup="true"><provider android:name="com.example.Provider" android:authorities="com.example.app.provider"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debugManifest, []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools"><application tools:remove="android:allowBackup"><provider android:name="com.example.Provider" tools:node="remove"/></application></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := manifestForPackaging(mod, "debug")
	if err != nil {
		t.Fatal(err)
	}
	reportData, err := os.ReadFile(got + ".merge-report.json")
	if err != nil {
		t.Fatalf("expected merge report next to manifest: %v", err)
	}
	report := string(reportData)
	if !strings.Contains(report, `"directive": "remove"`) || !strings.Contains(report, `"type": "directive"`) || !strings.Contains(report, `"sources"`) {
		t.Fatalf("expected merge report to contain directive events and sources:\n%s", report)
	}
}

func TestMaterializeMergedResourceDirOverlaysMoreSpecificResources(t *testing.T) {
	root := t.TempDir()
	mainRes := filepath.Join(root, "src", "main", "res")
	flavorRes := filepath.Join(root, "src", "free", "res")
	variantRes := filepath.Join(root, "src", "freeDebug", "res")
	for _, dir := range []string{
		filepath.Join(mainRes, "values"),
		filepath.Join(flavorRes, "values"),
		filepath.Join(variantRes, "layout"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(mainRes, "values", "strings.xml"), []byte(`<resources><string name="title">base</string></resources>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flavorRes, "values", "strings.xml"), []byte(`<resources><string name="title">free</string></resources>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variantRes, "layout", "main.xml"), []byte(`<LinearLayout/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	mergedDir := filepath.Join(root, "build", "grit", "merged", "res")
	if err := materializeMergedResourceDir([]string{mainRes, flavorRes, variantRes}, mergedDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(mergedDir, "values", "strings.xml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, `name="title">free<`) {
		t.Fatalf("merged strings = %q", got)
	}
	if _, err := os.Stat(filepath.Join(mergedDir, "layout", "main.xml")); err != nil {
		t.Fatalf("expected variant layout to be merged: %v", err)
	}
}

func TestMaterializeMergedResourceDirMergesValuesEntriesByName(t *testing.T) {
	root := t.TempDir()
	mainRes := filepath.Join(root, "src", "main", "res", "values")
	debugRes := filepath.Join(root, "src", "debug", "res", "values")
	for _, dir := range []string{mainRes, debugRes} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(mainRes, "strings.xml"), []byte(`<resources><string name="title">base</string><string name="subtitle">base-sub</string></resources>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(debugRes, "strings.xml"), []byte(`<resources><string name="title">debug</string></resources>`), 0o644); err != nil {
		t.Fatal(err)
	}
	mergedDir := filepath.Join(root, "build", "grit", "merged-values", "res")
	if err := materializeMergedResourceDir([]string{filepath.Join(root, "src", "main", "res"), filepath.Join(root, "src", "debug", "res")}, mergedDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(mergedDir, "values", "strings.xml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `name="title">debug<`) || !strings.Contains(body, `name="subtitle">base-sub<`) {
		t.Fatalf("expected merged values xml entries, got:\n%s", body)
	}
}

func TestAndroidVariantFixtureMergesManifestAndResources(t *testing.T) {
	root := t.TempDir()
	mod := &project.Module{
		Path:             ":app",
		Dir:              root,
		Type:             "android-application",
		Namespace:        "com.example.app",
		ApplicationID:    "com.example.app",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
		},
	}
	for _, path := range []string{
		filepath.Join(root, "src", "main", "AndroidManifest.xml"),
		filepath.Join(root, "src", "free", "AndroidManifest.xml"),
		filepath.Join(root, "src", "debug", "AndroidManifest.xml"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main", "AndroidManifest.xml"), []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="${applicationId}"><application android:label="@string/app_name"/></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "free", "AndroidManifest.xml"), []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android"><application android:label="@string/free_name"/></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "debug", "AndroidManifest.xml"), []byte(`<manifest xmlns:android="http://schemas.android.com/apk/res/android"><application android:debuggable="true"/></manifest>`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{
		filepath.Join(root, "src", "main", "res", "values"),
		filepath.Join(root, "src", "free", "res", "values"),
		filepath.Join(root, "src", "debug", "res", "values"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main", "res", "values", "strings.xml"), []byte(`<resources><string name="app_name">Base</string><string name="shared">BaseShared</string></resources>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "free", "res", "values", "strings.xml"), []byte(`<resources><string name="app_name">Free</string></resources>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "debug", "res", "values", "strings.xml"), []byte(`<resources><string name="debug_only">Debug</string></resources>`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath, err := manifestForPackaging(mod, "freeDebug")
	if err != nil {
		t.Fatal(err)
	}
	manifestBody, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifestBody), `package="com.example.app"`) || !strings.Contains(string(manifestBody), `android:label="@string/free_name"`) || !strings.Contains(string(manifestBody), `android:debuggable="true"`) {
		t.Fatalf("unexpected merged manifest:\n%s", string(manifestBody))
	}
	mergedDir := filepath.Join(root, "build", "grit", "fixture", "res")
	if err := materializeMergedResourceDir(resourceRootsForVariant(mod, "freeDebug"), mergedDir); err != nil {
		t.Fatal(err)
	}
	mergedValues, err := os.ReadFile(filepath.Join(mergedDir, "values", "strings.xml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(mergedValues)
	if !strings.Contains(body, `name="app_name">Free<`) || !strings.Contains(body, `name="shared">BaseShared<`) || !strings.Contains(body, `name="debug_only">Debug<`) {
		t.Fatalf("unexpected merged fixture resources:\n%s", body)
	}
}
