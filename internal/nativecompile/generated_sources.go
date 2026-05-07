package nativecompile

import (
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

func collectModeledGeneratedSources(mod *project.Module, variantName string) ([]string, []string, error) {
	if mod == nil {
		return nil, nil, nil
	}
	var kotlinJavaRoots []string
	var inputTokens []string
	for _, set := range mod.GeneratedSources {
		if set.Provider == "ksp" || set.Provider == "wire" {
			continue
		}
		if set.Variant != "" && !strings.EqualFold(set.Variant, variantName) {
			continue
		}
		for _, dir := range modeledGeneratedDirs(mod, set, variantName) {
			kotlinJavaRoots = append(kotlinJavaRoots, dir)
			inputTokens = append(inputTokens, dir)
		}
		inputTokens = append(inputTokens, set.FreshnessKeys...)
		inputTokens = append(inputTokens, set.Inputs...)
	}
	kotlinJavaRoots = mergePaths(nil, kotlinJavaRoots)
	if len(kotlinJavaRoots) == 0 {
		return nil, inputTokens, nil
	}
	sources, err := collectSourcesFromRoots(kotlinJavaRoots)
	if err != nil {
		return nil, nil, err
	}
	return sources, inputTokens, nil
}

func modeledGeneratedDirs(mod *project.Module, set project.GeneratedSourceSet, variantName string) []string {
	if len(set.Dirs) > 0 {
		return append([]string(nil), set.Dirs...)
	}
	switch set.Provider {
	case "metro":
		return []string{
			filepath.Join(mod.Dir, "build", "generated", "ksp", variantName, "kotlin"),
			filepath.Join(mod.Dir, "build", "generated", "metro", variantName, "kotlin"),
			filepath.Join(mod.Dir, "build", "generated", "source", "metro", variantName),
		}
	case "gradle-discovery":
		return append([]string(nil), set.Dirs...)
	default:
		return nil
	}
}
