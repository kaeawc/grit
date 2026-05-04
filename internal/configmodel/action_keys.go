package configmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/lint"
	"github.com/kaeawc/grit/internal/project"
)

type actionCacheKeyFunc func(*Model, graph.Action) string

var actionCacheKeyRegistry = map[graph.ActionKind]actionCacheKeyFunc{}

func init() {
	registerActionCacheKey(graph.ActionKindLint, lintActionCacheKey)
}

func registerActionCacheKey(kind graph.ActionKind, fn actionCacheKeyFunc) {
	switch kind {
	case "", graph.ActionKindUnknown:
		panic("configmodel: action cache key registration requires a concrete action kind")
	}
	if fn == nil {
		panic("configmodel: action cache key registration requires a function")
	}
	if _, exists := actionCacheKeyRegistry[kind]; exists {
		panic(fmt.Sprintf("configmodel: action cache key already registered for %q", kind))
	}
	actionCacheKeyRegistry[kind] = fn
}

func actionCacheKey(action graph.Action) string {
	return actionCacheKeyForModel(nil, action)
}

func actionCacheKeyForModel(m *Model, action graph.Action) string {
	if fn, ok := actionCacheKeyRegistry[action.Kind]; ok {
		if key := strings.TrimSpace(fn(m, action)); key != "" {
			return key
		}
	}
	return defaultActionCacheKey(action)
}

func lintActionCacheKey(m *Model, action graph.Action) string {
	if m == nil {
		return ""
	}
	modulePath := strings.TrimSpace(action.Attributes["modulePath"])
	variantName := strings.TrimSpace(action.Attributes["variantName"])
	if modulePath == "" || variantName == "" {
		return ""
	}
	resolved, ok := m.ResolvedVariant(modulePath, variantName)
	if !ok {
		return ""
	}
	moduleDir := ""
	if mod, ok := m.Module(modulePath); ok {
		moduleDir = mod.Dir
	}
	lintAction := lint.ActionFromVariantInModule(resolved, moduleDir)
	lintAction.CompileClasspath = lintCompileClasspathInputs(m, action, resolved)
	return lintAction.CacheKey().String()
}

func defaultActionCacheKey(action graph.Action) string {
	sum := sha256.New()
	parts := []string{
		action.ID.String(),
		action.ModuleID.String(),
		action.VariantID.String(),
		action.Name,
		string(action.Kind),
		action.Attributes["operation"],
		action.Attributes["variantName"],
		strings.Join(artifactIDs(action.Inputs), ","),
		strings.Join(artifactIDs(action.Outputs), ","),
		action.Note,
	}
	for _, part := range parts {
		_, _ = fmt.Fprint(sum, part)
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func lintCompileClasspathInputs(m *Model, action graph.Action, resolved project.ResolvedVariant) []lint.FileInput {
	if m == nil || len(action.Inputs) == 0 {
		return nil
	}
	ownRoots := map[string]struct{}{}
	for _, root := range resolved.SourceRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		ownRoots[filepath.Clean(root)] = struct{}{}
	}
	seen := map[string]struct{}{}
	var out []lint.FileInput
	for _, artifactID := range action.Inputs {
		for _, path := range lintClasspathPathsForArtifact(m, artifactID) {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			clean := filepath.Clean(path)
			if _, ok := ownRoots[clean]; ok {
				continue
			}
			if _, ok := seen[clean]; ok {
				continue
			}
			seen[clean] = struct{}{}
			out = append(out, lintPathInput(clean))
		}
	}
	return out
}

func lintClasspathPathsForArtifact(m *Model, artifactID graph.ArtifactID) []string {
	if roots, ok := m.SourceRootsForArtifact(artifactID); ok && len(roots) > 0 {
		return roots
	}
	if artifact, ok := m.ArtifactSummary(artifactID); ok && strings.TrimSpace(artifact.Path) != "" {
		return []string{artifact.Path}
	}
	return nil
}

func lintPathInput(path string) lint.FileInput {
	info, err := os.Stat(path)
	if err != nil {
		return lint.FileInput{Path: path}
	}
	if info.IsDir() {
		return lint.FileInput{
			Path: path,
			Hash: lintDirectoryHash(path),
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return lint.FileInput{Path: path}
	}
	return lint.FileInput{
		Path: path,
		Hash: cas.HashBytes(data),
	}
}

func lintDirectoryHash(root string) cas.Hash {
	files := make([]lint.FileInput, 0)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path) // #nosec
		if readErr != nil {
			return nil
		}
		files = append(files, lint.FileInput{
			Path: path,
			Hash: cas.HashBytes(data),
		})
		return nil
	})
	sort.Slice(files, func(i, j int) bool {
		if files[i].Path != files[j].Path {
			return files[i].Path < files[j].Path
		}
		return files[i].Hash.String() < files[j].Hash.String()
	})
	data, err := json.Marshal(struct {
		Version int              `json:"version"`
		Files   []lint.FileInput `json:"files,omitempty"`
	}{
		Version: 1,
		Files:   files,
	})
	if err != nil {
		panic("configmodel: failed to marshal lint classpath directory: " + err.Error())
	}
	return cas.HashBytes(data)
}
