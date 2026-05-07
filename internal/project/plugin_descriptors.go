package project

import (
	"slices"
	"strings"

	"github.com/kaeawc/grit/internal/modulebuild"
)

func builtinPluginDescriptors() map[string]PluginDescriptor {
	descriptors := map[string]PluginDescriptor{}
	add := func(id string, effect PluginEffect) {
		effect.PluginID = id
		descriptors[id] = PluginDescriptor{ID: id, Known: true, Source: "builtin", Effects: effect}
	}
	add("com.android.application", PluginEffect{ModuleTypes: []string{"android-application"}})
	add("com.android.test", PluginEffect{ModuleTypes: []string{"android-application"}})
	add("com.android.library", PluginEffect{ModuleTypes: []string{"android-library"}})
	add("org.jetbrains.kotlin.android", PluginEffect{})
	add("org.jetbrains.kotlin.jvm", PluginEffect{ModuleTypes: []string{"jvm-library"}})
	add("org.jetbrains.kotlin.multiplatform", PluginEffect{})
	add("java-library", PluginEffect{ModuleTypes: []string{"jvm-library"}})
	add("com.google.devtools.ksp", PluginEffect{
		GeneratedSourceSets: []GeneratedSourceSet{
			{OwnerPlugin: "com.google.devtools.ksp", Provider: "ksp", Language: "kotlin", Scope: "main", ProducedByGrit: true, Required: true},
			{OwnerPlugin: "com.google.devtools.ksp", Provider: "ksp", Language: "java", Scope: "main", ProducedByGrit: true, Required: true},
		},
	})
	add("com.squareup.wire", PluginEffect{
		GeneratedSourceSets: []GeneratedSourceSet{
			{OwnerPlugin: "com.squareup.wire", Provider: "wire", Language: "kotlin", Scope: "main", ProducedByGrit: true, Required: true},
			{OwnerPlugin: "com.squareup.wire", Provider: "wire", Language: "java", Scope: "main", ProducedByGrit: true, Required: true},
		},
	})
	add("org.jetbrains.kotlin.plugin.compose", PluginEffect{
		CompilerPluginIDs:    []string{modulebuild.ComposeCompilerPluginID},
		AndroidBuildFeatures: BuildFeatures{Compose: true},
	})
	add("org.jetbrains.kotlin.plugin.serialization", PluginEffect{
		CompilerPluginIDs: []string{modulebuild.KotlinSerializationCompilerPluginID},
	})
	add("dev.zacsweers.metro", PluginEffect{
		CompilerPluginIDs: []string{modulebuild.MetroCompilerPluginID},
		GeneratedSourceSets: []GeneratedSourceSet{
			{OwnerPlugin: "dev.zacsweers.metro", Provider: "metro", Language: "kotlin", Scope: "main", ProducedByGrit: true, Required: true},
		},
	})
	add("maven-publish", PluginEffect{})
	return descriptors
}

func applyPluginDescriptors(mod *Module) {
	if mod == nil {
		return
	}
	descriptors := builtinPluginDescriptors()
	var effects []PluginEffect
	var observed []ObservedPlugin
	for _, id := range mod.Plugins {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if descriptor, ok := descriptors[id]; ok {
			effects = append(effects, descriptor.Effects)
			observed = append(observed, ObservedPlugin{ID: id, Known: true, Source: descriptor.Source})
			continue
		}
		observed = append(observed, ObservedPlugin{
			ID:         id,
			Known:      false,
			Source:     "build-script",
			Limitation: "custom plugin effects are limited to statically parsed build-script constructs unless a discovery snapshot contributes additional model data",
		})
	}
	mod.PluginEffects = effects
	mod.ObservedPlugins = observed
	var preserved []GeneratedSourceSet
	for _, set := range mod.GeneratedSources {
		if set.Discovered {
			preserved = append(preserved, set)
		}
	}
	mod.GeneratedSources = preserved
	for _, effect := range effects {
		mod.GeneratedSources = append(mod.GeneratedSources, effect.GeneratedSourceSets...)
		if effect.AndroidBuildFeatures.Compose {
			mod.BuildFeatures.Compose = true
			mod.UsesCompose = true
		}
		for _, compilerPluginID := range effect.CompilerPluginIDs {
			switch compilerPluginID {
			case modulebuild.ComposeCompilerPluginID:
				mod.UsesCompose = true
			case modulebuild.KotlinSerializationCompilerPluginID:
				mod.UsesKotlinSerialization = true
			case modulebuild.MetroCompilerPluginID:
				mod.UsesMetro = true
			}
		}
		if mod.Type == "" {
			if slices.Contains(effect.ModuleTypes, "android-application") {
				mod.Type = "android-application"
			} else if slices.Contains(effect.ModuleTypes, "android-library") {
				mod.Type = "android-library"
			} else if slices.Contains(effect.ModuleTypes, "jvm-library") {
				mod.Type = "jvm-library"
			}
		}
	}
	mod.GeneratedSources = uniqueGeneratedSourceSets(mod.GeneratedSources)
}

func uniqueGeneratedSourceSets(in []GeneratedSourceSet) []GeneratedSourceSet {
	seen := map[string]bool{}
	var out []GeneratedSourceSet
	for _, set := range in {
		key := strings.Join([]string{set.OwnerPlugin, set.Provider, set.Language, set.Scope, set.Variant, strings.Join(set.Dirs, "\x00")}, "\x01")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, set)
	}
	return out
}
