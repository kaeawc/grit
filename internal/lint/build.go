package lint

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/project"
)

// ActionFromVariant constructs a lint Action from the resolved variant
// configuration, threading variant-level paths into the action's declared
// inputs so they participate in cache keying.
func ActionFromVariant(v project.ResolvedVariant) LintAction {
	return LintAction{
		Sources:      sourceInputs(v.SourceRoots),
		ResourceDirs: append([]string(nil), v.ResourceArtifactPaths...),
		ManifestPath: firstNonEmpty(v.ManifestPaths),
		Baseline:     v.LintBaselinePath,
	}
}

// ActionFromVariantInModule augments ActionFromVariant with module-local lint
// configuration discovery for files that are conventionally rooted at the
// module directory.
func ActionFromVariantInModule(v project.ResolvedVariant, moduleDir string) LintAction {
	action := ActionFromVariant(v)
	moduleDir = strings.TrimSpace(moduleDir)
	if moduleDir == "" {
		return action
	}
	if action.LintConfig == "" {
		action.LintConfig = optionalFile(filepath.Join(moduleDir, "lint.xml"))
	}
	if action.Baseline == "" {
		action.Baseline = optionalFile(filepath.Join(moduleDir, "lint-baseline.xml"))
	}
	return action
}

func firstNonEmpty(paths []string) string {
	for _, p := range paths {
		if p != "" {
			return p
		}
	}
	return ""
}

func sourceInputs(sourceRoots []string) []FileInput {
	var out []FileInput
	for _, root := range sourceRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() || !isLintSourceFile(path) {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			out = append(out, FileInput{
				Path: path,
				Hash: cas.HashBytes(data),
			})
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}

func isLintSourceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".java", ".kt":
		return true
	default:
		return false
	}
}

func optionalFile(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}
