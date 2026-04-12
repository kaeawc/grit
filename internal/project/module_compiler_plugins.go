package project

import "github.com/kaeawc/grit/internal/modulebuild"

func buildCompilerPluginRegistry(mod *Module) *modulebuild.PluginRegistry {
	reg := modulebuild.NewPluginRegistry()
	if mod == nil {
		return reg
	}
	modulebuild.RegisterComposePlugin(reg, mod.BuildFeatures.Compose || mod.UsesCompose)
	modulebuild.RegisterKotlinSerializationPlugin(reg, mod.UsesKotlinSerialization)
	modulebuild.RegisterMetroPlugin(reg, mod.UsesMetro)
	return reg
}

// ActiveCompilerPlugins returns the compiler plugins applicable to the named
// variant. Modules without a registry simply report no active plugins.
func (m Module) ActiveCompilerPlugins(variant string) []modulebuild.CompilerPlugin {
	if m.CompilerPlugins == nil {
		return nil
	}
	return m.CompilerPlugins.ActivePlugins(variant)
}
