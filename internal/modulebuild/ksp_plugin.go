package modulebuild

// KSPCompilerPluginID is the kotlinc plugin identifier under which the KSP1
// runtime registers itself when loaded via -Xplugin. Plugin options use the
// kotlinc form "plugin:<id>:<key>=<value>".
const KSPCompilerPluginID = "com.google.devtools.ksp.symbol-processing"

// KSPGradlePluginID is the Gradle plugin id applied in build.gradle.kts via
// `id("com.google.devtools.ksp")`.
const KSPGradlePluginID = "com.google.devtools.ksp"

// KSPConfig captures the per-module KSP configuration parsed from a build
// script: the list of processor dependency refs (resolved via the version
// catalog at compile time) plus any processor-wide options expressed in a
// `ksp { arg("k", "v") }` block.
type KSPConfig struct {
	Processors []Ref
	Options    map[string]string
}

// IsEmpty reports whether the config has no processors and no options.
func (c KSPConfig) IsEmpty() bool {
	return len(c.Processors) == 0 && len(c.Options) == 0
}
