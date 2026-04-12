package lint

import "github.com/kaeawc/grit/internal/project"

// ActionFromVariant constructs a lint Action from the resolved variant
// configuration, threading variant-level paths into the action's declared
// inputs so they participate in cache keying.
func ActionFromVariant(v project.ResolvedVariant) Action {
	return Action{
		ManifestPath: firstNonEmpty(v.ManifestPaths),
		Baseline:     v.LintBaselinePath,
	}
}

func firstNonEmpty(paths []string) string {
	for _, p := range paths {
		if p != "" {
			return p
		}
	}
	return ""
}
