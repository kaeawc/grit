package modulebuild

// ComposeCompilerPluginID is the Kotlin compiler plugin identifier for Compose.
const ComposeCompilerPluginID = "androidx.compose.compiler.plugins.kotlin"

// RegisterComposePlugin adds the Compose compiler plugin to the registry
// when compose is enabled in buildFeatures. The plugin applies to all
// variants (empty Variants list).
func RegisterComposePlugin(reg *PluginRegistry, composeEnabled bool) {
	if !composeEnabled {
		return
	}
	reg.Register(CompilerPlugin{
		ID: ComposeCompilerPluginID,
	})
}
