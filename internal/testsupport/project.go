package testsupport

import (
	"os"

	"github.com/kaeawc/grit/internal/project"
)

func Project(rootDir string, modules ...project.Module) *project.Project {
	return &project.Project{
		RootDir: rootDir,
		Modules: modules,
	}
}

func Module(path, typ string, variantNames ...string) project.Module {
	buildTypes := make(map[string]project.BuildType, len(variantNames))
	for _, name := range variantNames {
		buildTypes[name] = project.BuildType{Name: name}
	}
	return project.Module{
		Path:       path,
		Type:       typ,
		BuildTypes: buildTypes,
	}
}

func EnsureModuleDirs(modules ...project.Module) error {
	for _, mod := range modules {
		if mod.Dir == "" {
			continue
		}
		if err := os.MkdirAll(mod.Dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
