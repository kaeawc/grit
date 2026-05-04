package modulebuild

import (
	"strings"
	"unicode"
)

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
// contains the requested variant name or one of its variant components
// such as a flavor/build type token in a composite name like "freeDebug".
func (r *PluginRegistry) ActivePlugins(variant string) []CompilerPlugin {
	var result []CompilerPlugin
	for _, p := range r.plugins {
		if p.appliesToVariant(variant) {
			result = append(result, cloneCompilerPlugin(p))
		}
	}
	return result
}

func (p CompilerPlugin) appliesToVariant(variant string) bool {
	if len(p.Variants) == 0 {
		return true
	}
	for _, scope := range p.Variants {
		if variantScopeMatches(scope, variant) {
			return true
		}
	}
	return false
}

func variantScopeMatches(scope, variant string) bool {
	scope = strings.TrimSpace(scope)
	variant = strings.TrimSpace(variant)
	if scope == "" || variant == "" {
		return false
	}
	if strings.EqualFold(scope, variant) {
		return true
	}
	scope = strings.ToLower(scope)
	for _, token := range variantScopeTokens(variant) {
		if token == scope {
			return true
		}
	}
	return false
}

func variantScopeTokens(variant string) []string {
	variant = strings.TrimSpace(variant)
	if variant == "" {
		return nil
	}
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(string(current)))
		current = current[:0]
	}
	for i, r := range variant {
		if r == '-' || r == '_' || unicode.IsSpace(r) {
			flush()
			continue
		}
		if i > 0 && unicode.IsUpper(r) && len(current) > 0 {
			flush()
		}
		current = append(current, r)
	}
	flush()
	return tokens
}
