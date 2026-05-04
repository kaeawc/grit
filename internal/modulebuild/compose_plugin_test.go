package modulebuild

import "testing"

func TestRegisterComposePlugin_Enabled(t *testing.T) {
	reg := NewPluginRegistry()
	RegisterComposePlugin(reg, true)

	plugins := reg.ActivePlugins("debug")
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].ID != ComposeCompilerPluginID {
		t.Errorf("expected plugin ID %s, got %s", ComposeCompilerPluginID, plugins[0].ID)
	}
	if got := plugins[0].Options["suppressKotlinVersionCompatibilityCheck"]; got != "true" {
		t.Fatalf("expected compose plugin compatibility option to be set, got %q", got)
	}

	// Compose plugin should apply to all variants.
	release := reg.ActivePlugins("release")
	if len(release) != 1 {
		t.Fatalf("expected 1 plugin for release, got %d", len(release))
	}
	if release[0].ID != ComposeCompilerPluginID {
		t.Errorf("expected plugin ID %s for release, got %s", ComposeCompilerPluginID, release[0].ID)
	}
}

func TestRegisterComposePlugin_Disabled(t *testing.T) {
	reg := NewPluginRegistry()
	RegisterComposePlugin(reg, false)

	plugins := reg.ActivePlugins("debug")
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins when compose disabled, got %d", len(plugins))
	}
}

func TestRegisterComposePlugin_WithOtherPlugins(t *testing.T) {
	reg := NewPluginRegistry()

	// Register another plugin first.
	reg.Register(CompilerPlugin{
		ID:        "org.jetbrains.kotlin.plugin.serialization",
		Classpath: []string{"/jars/serialization.jar"},
	})

	RegisterComposePlugin(reg, true)

	plugins := reg.ActivePlugins("debug")
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}
	if plugins[0].ID != "org.jetbrains.kotlin.plugin.serialization" {
		t.Errorf("expected first plugin to be serialization, got %s", plugins[0].ID)
	}
	if plugins[1].ID != ComposeCompilerPluginID {
		t.Errorf("expected second plugin to be compose, got %s", plugins[1].ID)
	}
}
