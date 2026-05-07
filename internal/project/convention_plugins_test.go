package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectPluginIDsCapturesIdAndAliasForms(t *testing.T) {
	body := `import foo
plugins {
  id("com.android.application")
  id("kotlin-android")
  alias(libs.plugins.kotlin.serialization)
  id("com.squareup.wire")
  kotlin("plugin.compose")
  id 'org.jlleitschuh.gradle.ktlint'
}

android { ... }
`
	got := collectPluginIDs(body)
	want := map[string]bool{
		"com.android.application":             true,
		"kotlin-android":                      true,
		"kotlin.serialization":                true,
		"com.squareup.wire":                   true,
		"org.jetbrains.kotlin.plugin.compose": true,
		"org.jlleitschuh.gradle.ktlint":       true,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d plugins, got %d: %v", len(want), len(got), got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected plugin id %q", id)
		}
	}
}

func TestConventionPluginMapExpandsTransitively(t *testing.T) {
	root := t.TempDir()
	conv := filepath.Join(root, "build-logic", "plugins", "src", "main", "java")
	if err := os.MkdirAll(conv, 0o755); err != nil {
		t.Fatal(err)
	}
	library := `plugins {
  id("com.android.library")
  id("org.jetbrains.kotlin.plugin.compose")
}
`
	if err := os.WriteFile(filepath.Join(conv, "convention-library.gradle.kts"), []byte(library), 0o644); err != nil {
		t.Fatal(err)
	}
	conventions := conventionPluginMap(root)
	got, ok := conventions["convention-library"]
	if !ok {
		t.Fatalf("convention-library convention plugin not detected")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 transitive plugins, got %d (%v)", len(got), got)
	}

	expanded := expandPlugins([]string{"convention-library"}, conventions)
	want := []string{"com.android.library", "convention-library", "org.jetbrains.kotlin.plugin.compose"}
	if len(expanded) != len(want) {
		t.Fatalf("expected %d expanded ids, got %v", len(want), expanded)
	}
	for i, id := range want {
		if expanded[i] != id {
			t.Errorf("expanded[%d]=%q want %q", i, expanded[i], id)
		}
	}
}
