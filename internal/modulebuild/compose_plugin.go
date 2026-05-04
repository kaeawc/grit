package modulebuild

// ComposeCompilerPluginID is the Kotlin compiler plugin identifier for Compose.
const ComposeCompilerPluginID = "androidx.compose.compiler.plugins.kotlin"

// KotlinSerializationCompilerPluginID is the Kotlin compiler plugin identifier
// for kotlinx.serialization.
const KotlinSerializationCompilerPluginID = "org.jetbrains.kotlin.plugin.serialization"

// MetroCompilerPluginID is the Kotlin compiler plugin identifier for Metro.
const MetroCompilerPluginID = "dev.zacsweers.metro"

// RegisterComposePlugin adds the Compose compiler plugin to the registry
// when compose is enabled in buildFeatures. The plugin applies to all
// variants (empty Variants list).
func RegisterComposePlugin(reg *PluginRegistry, composeEnabled bool) {
	if !composeEnabled {
		return
	}
	reg.Register(CompilerPlugin{
		ID: ComposeCompilerPluginID,
		Options: map[string]string{
			"suppressKotlinVersionCompatibilityCheck": "true",
		},
	})
}

// RegisterKotlinSerializationPlugin adds the kotlinx.serialization compiler
// plugin to the registry when the module applies it.
func RegisterKotlinSerializationPlugin(reg *PluginRegistry, enabled bool) {
	if !enabled {
		return
	}
	reg.Register(CompilerPlugin{
		ID: KotlinSerializationCompilerPluginID,
	})
}

// RegisterMetroPlugin adds the Metro compiler plugin to the registry when the
// module applies it.
func RegisterMetroPlugin(reg *PluginRegistry, enabled bool) {
	if !enabled {
		return
	}
	reg.Register(CompilerPlugin{
		ID: MetroCompilerPluginID,
	})
}
