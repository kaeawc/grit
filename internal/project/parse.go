package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func Load(root string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	settingsFile := filepath.Join(abs, "settings.gradle.kts")
	rootBuildFile := filepath.Join(abs, "build.gradle.kts")
	if !fileExists(settingsFile) || !fileExists(rootBuildFile) {
		return nil, fmt.Errorf("expected a Kotlin DSL Android repo with %s and %s", settingsFile, rootBuildFile)
	}

	settingsData, err := os.ReadFile(settingsFile)
	if err != nil {
		return nil, err
	}
	rootBuildData, err := os.ReadFile(rootBuildFile)
	if err != nil {
		return nil, err
	}
	gradleProperties := loadGradleProperties(filepath.Join(abs, "gradle.properties"))
	settingsModel := parseSettingsKTSWithProperties(string(settingsData), gradleProperties)
	repositories := append([]Repository(nil), settingsModel.Repositories...)
	repositories = append(repositories, collectProjectRepositoriesWithOrigin(string(rootBuildData), gradleProperties, "root-build")...)

	prj := &Project{
		Name:               settingsModel.Name,
		RootDir:            abs,
		SettingsFile:       settingsFile,
		RootBuildFile:      rootBuildFile,
		ModuleDirs:         settingsModel.ModuleDirs,
		GradleProperties:   gradleProperties,
		VersionCatalogs:    collectVersionCatalogs(filepath.Join(abs, "gradle")),
		Repositories:       repositories,
		RootPlugins:        collectPluginAliases(string(rootBuildData)),
		RecommendedBackend: "native",
	}
	if len(prj.VersionCatalogs) > 0 {
		prj.VersionCatalog = pickPrimaryCatalog(prj.VersionCatalogs)
		prj.VersionCatalogData, err = loadVersionCatalogs(prj.VersionCatalogs)
		if err != nil {
			return nil, err
		}
	}

	for _, modulePath := range settingsModel.Includes {
		mod, err := loadModule(prj, modulePath)
		if err != nil {
			return nil, err
		}
		if fileExists(mod.BuildFile) {
			data, err := os.ReadFile(mod.BuildFile)
			if err != nil {
				return nil, err
			}
			prj.Repositories = append(prj.Repositories, collectProjectRepositoriesWithOrigin(string(data), prj.GradleProperties, "module-build")...)
		}
		if mod.Type != "" {
			prj.Modules = append(prj.Modules, *mod)
		}
	}
	prj.Repositories = annotateRepositories(prj.Repositories, "", 0)
	prj.Repositories = dedupeRepositories(prj.Repositories)

	sort.Slice(prj.Modules, func(i, j int) bool { return prj.Modules[i].Path < prj.Modules[j].Path })
	if prj.Name == "" {
		prj.Name = filepath.Base(abs)
	}

	return prj, nil
}
