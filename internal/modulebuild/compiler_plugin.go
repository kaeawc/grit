package modulebuild

// CompilerPlugin describes a compiler plugin applied during compilation.
// Examples: KSP, kapt, Compose, kotlinx-serialization, custom annotation processors.
type CompilerPlugin struct {
	// ID is the plugin identifier, e.g. "org.jetbrains.kotlin.plugin.serialization".
	ID string

	// Classpath lists resolved JAR paths that the compiler needs to load the plugin.
	Classpath []string

	// Options holds key-value string pairs passed to the plugin via compiler flags.
	Options map[string]string

	// Variants lists the variant names this plugin applies to.
	// An empty list means the plugin applies to all variants.
	Variants []string
}

func cloneCompilerPlugin(p CompilerPlugin) CompilerPlugin {
	cloned := p
	if len(p.Classpath) > 0 {
		cloned.Classpath = append([]string(nil), p.Classpath...)
	}
	if len(p.Variants) > 0 {
		cloned.Variants = append([]string(nil), p.Variants...)
	}
	if len(p.Options) > 0 {
		cloned.Options = make(map[string]string, len(p.Options))
		for key, value := range p.Options {
			cloned.Options[key] = value
		}
	}
	return cloned
}

// PluginRegistry holds a set of compiler plugins for a module.
type PluginRegistry struct {
	plugins []CompilerPlugin
}

// NewPluginRegistry creates an empty registry.
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{}
}

// Register adds a compiler plugin to the registry.
func (r *PluginRegistry) Register(p CompilerPlugin) {
	r.plugins = append(r.plugins, cloneCompilerPlugin(p))
}

// ActivePlugins returns the plugins applicable to the given variant.
// A plugin matches if its Variants list is empty (applies to all) or
// contains the requested variant name.
func (r *PluginRegistry) ActivePlugins(variant string) []CompilerPlugin {
	var result []CompilerPlugin
	for _, p := range r.plugins {
		if len(p.Variants) == 0 {
			result = append(result, cloneCompilerPlugin(p))
			continue
		}
		for _, v := range p.Variants {
			if v == variant {
				result = append(result, cloneCompilerPlugin(p))
				break
			}
		}
	}
	return result
}
