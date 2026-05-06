package catalog

import (
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/testutil"
)

func TestLoadParsesStringNotationLibrary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "libs.versions.toml")
	testutil.WriteFile(t, root, "libs.versions.toml", `
[libraries]
splash-screen = "androidx.core:core-splashscreen:1.2.0"
`)
	cat, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := cat.ResolveLibrary("splash.screen")
	if err != nil {
		t.Fatal(err)
	}
	if lib.Group != "androidx.core" || lib.Name != "core-splashscreen" || lib.Version != "1.2.0" {
		t.Fatalf("unexpected library: %#v", lib)
	}
}

func TestResolveBundleReturnsCopy(t *testing.T) {
	cat := &Catalog{
		Bundles: map[string][]string{
			"unit-test": {"junit", "mockk"},
		},
	}

	bundle, err := cat.ResolveBundle("unit.test")
	if err != nil {
		t.Fatal(err)
	}
	bundle[0] = "mutated"
	mutated := append(bundle, "extra")
	if got, want := len(mutated), 3; got != want {
		t.Fatalf("mutated bundle length = %d, want %d", got, want)
	}

	fresh, err := cat.ResolveBundle("unit.test")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(fresh), 2; got != want {
		t.Fatalf("fresh bundle length = %d, want %d", got, want)
	}
	if got, want := fresh[0], "junit"; got != want {
		t.Fatalf("fresh bundle first ref = %q, want %q", got, want)
	}
	if got, want := fresh[1], "mockk"; got != want {
		t.Fatalf("fresh bundle second ref = %q, want %q", got, want)
	}
}
