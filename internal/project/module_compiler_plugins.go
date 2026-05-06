package project

import (
	"regexp"
	"slices"

	"github.com/kaeawc/grit/internal/modulebuild"
)

func buildCompilerPluginRegistry(mod *Module, body string) *modulebuild.PluginRegistry {
	reg := modulebuild.NewPluginRegistry()
	if mod == nil {
		return reg
	}
	modulebuild.RegisterComposePlugin(reg, mod.BuildFeatures.Compose || mod.UsesCompose)
	modulebuild.RegisterKotlinSerializationPlugin(reg, mod.UsesKotlinSerialization)
	modulebuild.RegisterMetroPlugin(reg, mod.UsesMetro)
	for _, plugin := range parseCustomCompilerPlugins(body, mod.Dir) {
		reg.Register(plugin)
	}
	return reg
}

func refreshDerivedCompilerPluginState(mod *Module, body string) {
	if mod == nil {
		return
	}
	mod.UsesCompose = mod.UsesCompose || slices.Contains(mod.Plugins, "org.jetbrains.kotlin.plugin.compose")
	mod.UsesKotlinSerialization = mod.UsesKotlinSerialization || slices.Contains(mod.Plugins, "org.jetbrains.kotlin.plugin.serialization")
	mod.UsesMetro = mod.UsesMetro || slices.Contains(mod.Plugins, "dev.zacsweers.metro")
	mod.UsesKSP = mod.UsesKSP || slices.Contains(mod.Plugins, "com.google.devtools.ksp")
	if mod.Type == "" {
		switch {
		case slices.Contains(mod.Plugins, "com.android.application"), slices.Contains(mod.Plugins, "com.android.test"):
			mod.Type = "android-application"
		case slices.Contains(mod.Plugins, "com.android.library"):
			mod.Type = "android-library"
		case slices.Contains(mod.Plugins, "org.jetbrains.kotlin.jvm"), slices.Contains(mod.Plugins, "java-library"):
			mod.Type = "jvm-library"
		}
	}
	mod.CompilerPlugins = buildCompilerPluginRegistry(mod, body)
}

// ActiveCompilerPlugins returns the compiler plugins applicable to the named
// variant. Modules without a registry simply report no active plugins.
func (m Module) ActiveCompilerPlugins(variant string) []modulebuild.CompilerPlugin {
	if m.CompilerPlugins == nil {
		return nil
	}
	return m.CompilerPlugins.ActivePlugins(variant)
}

func parseCustomCompilerPlugins(body, modDir string) []modulebuild.CompilerPlugin {
	gritBlock, ok := extractNamedBlock(body, "grit")
	if !ok {
		return nil
	}
	pluginsBlock, ok := extractNamedBlock(gritBlock, "compilerPlugins")
	if !ok {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*(?:create\("([^"]+)"\)|register\("([^"]+)"\)|([A-Za-z0-9_]+))\s*\{`)
	indexes := re.FindAllStringSubmatchIndex(pluginsBlock, -1)
	if len(indexes) == 0 {
		return nil
	}
	var out []modulebuild.CompilerPlugin
	for _, idx := range indexes {
		if braceDepth(pluginsBlock[:idx[0]]) != 0 {
			continue
		}
		name := firstNonEmpty(
			captureSubmatch(pluginsBlock, idx, 2),
			captureSubmatch(pluginsBlock, idx, 4),
			captureSubmatch(pluginsBlock, idx, 6),
		)
		pluginBody, _, ok := extractBraceBodyAt(pluginsBlock, idx[1]-1)
		if !ok {
			continue
		}
		pluginID := firstNonEmpty(parseAssignment(pluginBody, `id\s*=\s*"([^"]+)"`), name)
		if pluginID == "" {
			continue
		}
		out = append(out, modulebuild.CompilerPlugin{
			ID:        pluginID,
			Classpath: parseCompilerPluginClasspath(pluginBody, modDir),
			Options:   parseCompilerPluginOptions(pluginBody),
			Variants:  parseCompilerPluginVariants(pluginBody),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseCompilerPluginClasspath(body, modDir string) []string {
	var rels []string
	for _, match := range []*regexp.Regexp{
		regexp.MustCompile(`classpath\s*=\s*listOf\((?s)(.*?)\)`),
		regexp.MustCompile(`classpath\s*=\s*\[(?s)(.*?)\]`),
		regexp.MustCompile(`classpath\s*\((?s)(.*?)\)`),
		regexp.MustCompile(`classpath\s*\+=\s*listOf\((?s)(.*?)\)`),
	} {
		for _, value := range match.FindAllStringSubmatch(body, -1) {
			if len(value) < 2 {
				continue
			}
			rels = appendUniqueQuoted(rels, value[1])
		}
	}
	reSingle := regexp.MustCompile(`classpath\s*\+=\s*"([^"]+)"`)
	for _, value := range reSingle.FindAllStringSubmatch(body, -1) {
		if len(value) < 2 {
			continue
		}
		path := value[1]
		if !containsString(rels, path) {
			rels = append(rels, path)
		}
	}
	return resolveRelativeFiles(modDir, rels)
}

func parseCompilerPluginVariants(body string) []string {
	var out []string
	for _, match := range []*regexp.Regexp{
		regexp.MustCompile(`variants\s*=\s*listOf\((?s)(.*?)\)`),
		regexp.MustCompile(`variants\s*=\s*\[(?s)(.*?)\]`),
		regexp.MustCompile(`variants\s*\((?s)(.*?)\)`),
		regexp.MustCompile(`variants\s*\+=\s*listOf\((?s)(.*?)\)`),
	} {
		for _, value := range match.FindAllStringSubmatch(body, -1) {
			if len(value) < 2 {
				continue
			}
			out = appendUniqueQuoted(out, value[1])
		}
	}
	reSingle := regexp.MustCompile(`variants\s*\+=\s*"([^"]+)"`)
	for _, value := range reSingle.FindAllStringSubmatch(body, -1) {
		if len(value) < 2 {
			continue
		}
		variant := value[1]
		if !containsString(out, variant) {
			out = append(out, variant)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseCompilerPluginOptions(body string) map[string]string {
	out := map[string]string{}
	reMap := regexp.MustCompile(`options\s*=\s*mapOf\((?s)(.*?)\)`)
	rePair := regexp.MustCompile(`"([^"]+)"\s*to\s*"([^"]*)"`)
	for _, match := range reMap.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		for _, pair := range rePair.FindAllStringSubmatch(match[1], -1) {
			if len(pair) < 3 {
				continue
			}
			out[pair[1]] = pair[2]
		}
	}
	reOptionCall := regexp.MustCompile(`option\s*\(\s*"([^"]+)"\s*,\s*"([^"]*)"\s*\)`)
	for _, match := range reOptionCall.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		out[match[1]] = match[2]
	}
	reOptionIndex := regexp.MustCompile(`options\s*\[\s*"([^"]+)"\s*\]\s*=\s*"([^"]*)"`)
	for _, match := range reOptionIndex.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		out[match[1]] = match[2]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
