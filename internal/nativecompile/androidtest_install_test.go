package nativecompile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/project"
)

func TestAndroidTestApplicationIDUsesResolvedVariantApplicationID(t *testing.T) {
	mod := &project.Module{
		Path: ":app",
		Type: "android-application",
		DefaultConfig: project.DefaultConfig{
			ApplicationID: "com.example.base",
		},
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier", ApplicationIDSuffix: ".free"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug", ApplicationIDSuffix: ".debug"},
		},
	}
	if got, want := androidTestApplicationID(mod, "freeDebug"), "com.example.base.free.debug.test"; got != want {
		t.Fatalf("unexpected androidTest application id: got %q want %q", got, want)
	}
}

func TestAndroidTestManifestForPackagingUsesVariantAwareTargetPackage(t *testing.T) {
	root := t.TempDir()
	prj := &project.Project{RootDir: root}
	mod := &project.Module{
		Path:                      ":app",
		Dir:                       filepath.Join(root, "app"),
		Type:                      "android-application",
		Namespace:                 "com.example.app",
		ApplicationID:             "com.example.app",
		TestInstrumentationRunner: "androidx.test.runner.AndroidJUnitRunner",
		FlavorDimensions:          []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier", ApplicationIDSuffix: ".free"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug", ApplicationIDSuffix: ".debug"},
		},
	}
	path, err := androidTestManifestForPackaging(prj, mod, "freeDebug")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		`package="com.example.app.free.debug.test"`,
		`android:name="androidx.test.runner.AndroidJUnitRunner"`,
		`android:targetPackage="com.example.app.free.debug"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("manifest missing %q:\n%s", want, body)
		}
	}
}
