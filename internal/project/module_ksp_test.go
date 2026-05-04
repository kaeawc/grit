package project

import (
	"testing"

	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/testutil"
)

func TestDetectKSPApplied(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"id-form", `plugins { id("com.google.devtools.ksp") }`, true},
		{"groovy-form", `plugins { id 'com.google.devtools.ksp' }`, true},
		{"catalog-alias", `plugins { alias(libs.plugins.ksp) }`, true},
		{"unrelated-plugin", `plugins { id("com.android.library") }`, false},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectKSPApplied(tc.body); got != tc.want {
				t.Fatalf("detectKSPApplied(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestParseKSPProcessorsAndOptions(t *testing.T) {
	body := `
plugins {
  id("com.google.devtools.ksp")
}

dependencies {
  implementation(libs.glide.core)
  ksp(libs.glide.ksp)
  ksp("com.google.dagger:hilt-compiler:2.51")
}

ksp {
  arg("room.schemaLocation", "$projectDir/schemas")
  arg("dagger.fastInit", "enabled")
}
`
	procs := parseKSPProcessors(body)
	if got, want := len(procs), 2; got != want {
		t.Fatalf("processor count: got %d want %d (%#v)", got, want, procs)
	}
	if procs[0].Kind != "library" || procs[0].Value != "glide.ksp" {
		t.Fatalf("unexpected first processor: %+v", procs[0])
	}
	if procs[1].Kind != "raw" || procs[1].Value != "com.google.dagger:hilt-compiler:2.51" {
		t.Fatalf("unexpected second processor: %+v", procs[1])
	}

	opts := parseKSPOptions(body)
	if got, want := len(opts), 2; got != want {
		t.Fatalf("option count: got %d want %d (%#v)", got, want, opts)
	}
	if opts["room.schemaLocation"] != "$projectDir/schemas" {
		t.Fatalf("unexpected room.schemaLocation: %q", opts["room.schemaLocation"])
	}
	if opts["dagger.fastInit"] != "enabled" {
		t.Fatalf("unexpected dagger.fastInit: %q", opts["dagger.fastInit"])
	}
}

func TestParseKSPProcessorsDeduplicates(t *testing.T) {
	body := `
dependencies {
  ksp(libs.glide.ksp)
  ksp(libs.glide.ksp)
}
`
	procs := parseKSPProcessors(body)
	if got, want := len(procs), 1; got != want {
		t.Fatalf("expected dedup to 1 processor, got %d (%#v)", got, procs)
	}
}

func TestLoadModulePopulatesKSPConfig(t *testing.T) {
	root := t.TempDir()
	prj := &Project{RootDir: root}
	testutil.WriteFile(t, root, "glide-config/build.gradle.kts", `
plugins {
  id("signal-library")
  id("com.google.devtools.ksp")
}

android {
  namespace = "org.signal.glide.config"
}

dependencies {
  ksp(libs.glide.ksp)
}
`)

	mod, err := loadModule(prj, ":glide-config")
	if err != nil {
		t.Fatalf("loadModule: %v", err)
	}
	if !mod.UsesKSP {
		t.Fatal("expected UsesKSP=true")
	}
	if len(mod.KSP.Processors) != 1 {
		t.Fatalf("expected 1 KSP processor, got %d (%#v)", len(mod.KSP.Processors), mod.KSP.Processors)
	}
	if got, want := mod.KSP.Processors[0], (modulebuild.Ref{Kind: "library", Value: "glide.ksp"}); got != want {
		t.Fatalf("unexpected processor ref: got %+v want %+v", got, want)
	}
}

func TestKSPConfigIsEmpty(t *testing.T) {
	if !(modulebuild.KSPConfig{}).IsEmpty() {
		t.Fatal("zero KSPConfig should be empty")
	}
	cfg := modulebuild.KSPConfig{Processors: []modulebuild.Ref{{Kind: "raw", Value: "x:y:1"}}}
	if cfg.IsEmpty() {
		t.Fatal("non-empty processors should not be empty")
	}
}
