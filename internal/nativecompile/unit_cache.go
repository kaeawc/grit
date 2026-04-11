package nativecompile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func loadUnitTestResolvedCache(prj *project.Project, mod *project.Module, variantName string, deps *modulebuild.Dependencies) (*m2local.Resolved, error) {
	path, err := unitTestResolvedCachePath(prj, mod, variantName, deps)
	if err != nil {
		return nil, err
	}
	if !pathIsFile(path) {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var resolved m2local.Resolved
	if err := json.Unmarshal(data, &resolved); err != nil {
		return nil, err
	}
	for _, jar := range resolved.TestJars {
		if !pathIsFile(jar) {
			return nil, nil
		}
	}
	return &resolved, nil
}

func saveUnitTestResolvedCache(prj *project.Project, mod *project.Module, variantName string, deps *modulebuild.Dependencies, resolved *m2local.Resolved) error {
	path, err := unitTestResolvedCachePath(prj, mod, variantName, deps)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(resolved)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func unitTestResolvedCachePath(prj *project.Project, mod *project.Module, variantName string, deps *modulebuild.Dependencies) (string, error) {
	inputs := []string{
		cacheIdentityForInput(mod.BuildFile),
		cacheIdentityForInput(filepath.Join(mod.Dir, "src", "test")),
		cacheIdentityForInput(filepath.Join(mod.Dir, "src", "main")),
		cacheIdentityForInput(filepath.Join(mod.Dir, "src", "debug")),
	}
	for _, catalogPath := range prj.VersionCatalogs {
		inputs = append(inputs, cacheIdentityForInput(catalogPath))
	}
	keyData, err := json.Marshal(struct {
		CacheVersion string                    `json:"cacheVersion"`
		Repo         string                    `json:"repo"`
		Module       string                    `json:"module"`
		Variant      string                    `json:"variant"`
		Repositories []project.Repository      `json:"repositories"`
		Deps         *modulebuild.Dependencies `json:"deps"`
		Inputs       []string                  `json:"inputs"`
	}{
		CacheVersion: "unit-test-resolved-v1",
		Repo:         prj.RootDir,
		Module:       mod.Path,
		Variant:      variantName,
		Repositories: prj.Repositories,
		Deps:         deps,
		Inputs:       inputs,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(keyData)
	return filepath.Join(sharedNativeCacheRoot(), "unit-test-resolve", hex.EncodeToString(sum[:])+".json"), nil
}
