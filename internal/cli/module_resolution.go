package cli

import (
	"fmt"
	"strings"

	"github.com/kaeawc/grit/internal/project"
)

const moduleFlagUsage = "Module path (defaults to the repository's application module when unambiguous)"

const allModulesFlagUsage = "Run the command against every module declared in settings.gradle.kts"

// resolveModulePath returns either the user-supplied module path or, when
// empty, the repository's default. When no unambiguous default exists the
// error names the candidate modules so the caller can rerun with --module.
func resolveModulePath(prj *project.Project, requested string) (string, error) {
	if strings.TrimSpace(requested) != "" {
		return requested, nil
	}
	if path, ok := prj.DefaultModulePath(); ok {
		return path, nil
	}
	return "", ambiguousModuleError(prj)
}

func ambiguousModuleError(prj *project.Project) error {
	apps := prj.ApplicationModulePaths()
	all := prj.AllModulePaths()
	switch {
	case len(apps) > 1:
		return fmt.Errorf("multiple application modules found (%s); pass --module to choose one", strings.Join(apps, ", "))
	case len(all) == 0:
		return fmt.Errorf("no modules discovered; check settings.gradle.kts")
	default:
		return fmt.Errorf("no application module found; pass --module to choose from: %s", strings.Join(all, ", "))
	}
}
