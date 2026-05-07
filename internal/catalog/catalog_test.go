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

func TestLoadParsesRichVersionsPluginsPlatformsAndProvenance(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "libs.versions.toml")
	testutil.WriteFile(t, root, "libs.versions.toml", `
[versions]
metro = { strictly = "0.12.0" }
compose = { prefer = "2025.01.00" }

[libraries]
compose-bom = { module = "androidx.compose:compose-bom", version.ref = "compose", platform = true }

[plugins]
metro = { id = "dev.zacsweers.metro", version.ref = "metro" }
`)
	cat, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cat.Versions["metro"], "0.12.0"; got != want {
		t.Fatalf("metro version = %q, want %q", got, want)
	}
	lib, err := cat.ResolveLibrary("compose.bom")
	if err != nil {
		t.Fatal(err)
	}
	if !lib.Platform || lib.Version != "2025.01.00" {
		t.Fatalf("unexpected platform library: %#v", lib)
	}
	plugin, err := cat.ResolvePlugin("metro")
	if err != nil {
		t.Fatal(err)
	}
	if plugin.ID != "dev.zacsweers.metro" || plugin.Version != "0.12.0" {
		t.Fatalf("unexpected plugin: %#v", plugin)
	}
	prov := cat.ProvenanceFor("plugins", "metro")
	if prov.File != path || prov.Section != "plugins" || prov.Alias != "metro" {
		t.Fatalf("unexpected provenance: %#v", prov)
	}
}
