package modulebuild

import (
	"testing"
)

func TestActivePlugins_FiltersByVariant(t *testing.T) {
	reg := NewPluginRegistry()

	// Plugin scoped to "debug" only.
	reg.Register(CompilerPlugin{
		ID:        "com.example.debug-tracer",
		Classpath: []string{"/jars/debug-tracer.jar"},
		Options:   map[string]string{"verbose": "true"},
		Variants:  []string{"debug"},
	})

	// Plugin scoped to all variants (empty Variants list).
	reg.Register(CompilerPlugin{
		ID:        "org.jetbrains.kotlin.plugin.serialization",
		Classpath: []string{"/jars/serialization-compiler.jar"},
		Options:   map[string]string{"format": "json"},
	})

	// --- variant "debug" should see both plugins ---
	debugPlugins := reg.ActivePlugins("debug")
	if len(debugPlugins) != 2 {
		t.Fatalf("expected 2 active plugins for debug, got %d", len(debugPlugins))
	}
	if debugPlugins[0].ID != "com.example.debug-tracer" {
		t.Errorf("expected first debug plugin to be debug-tracer, got %s", debugPlugins[0].ID)
	}
	if debugPlugins[1].ID != "org.jetbrains.kotlin.plugin.serialization" {
		t.Errorf("expected second debug plugin to be serialization, got %s", debugPlugins[1].ID)
	}

	// --- variant "release" should see only the unscoped plugin ---
	releasePlugins := reg.ActivePlugins("release")
	if len(releasePlugins) != 1 {
		t.Fatalf("expected 1 active plugin for release, got %d", len(releasePlugins))
	}
	if releasePlugins[0].ID != "org.jetbrains.kotlin.plugin.serialization" {
		t.Errorf("expected release plugin to be serialization, got %s", releasePlugins[0].ID)
	}
}

func TestActivePlugins_MultipleVariantScopes(t *testing.T) {
	reg := NewPluginRegistry()

	reg.Register(CompilerPlugin{
		ID:       "com.example.compose",
		Variants: []string{"debug", "staging"},
	})

	if got := reg.ActivePlugins("debug"); len(got) != 1 {
		t.Errorf("expected 1 plugin for debug, got %d", len(got))
	}
	if got := reg.ActivePlugins("staging"); len(got) != 1 {
		t.Errorf("expected 1 plugin for staging, got %d", len(got))
	}
	if got := reg.ActivePlugins("release"); len(got) != 0 {
		t.Errorf("expected 0 plugins for release, got %d", len(got))
	}
}

func TestActivePlugins_EmptyRegistry(t *testing.T) {
	reg := NewPluginRegistry()
	if got := reg.ActivePlugins("debug"); len(got) != 0 {
		t.Errorf("expected 0 plugins from empty registry, got %d", len(got))
	}
}
