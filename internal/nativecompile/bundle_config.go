package nativecompile

import (
	"path/filepath"
)

// bundleConfigCandidates lists the file names searched when looking for a
// BundleConfig inside a module directory. The first match wins.
var bundleConfigCandidates = []string{
	"BundleConfig.pb",
	"bundle_config.pb",
	"BundleConfig.json",
}

// findBundleConfig looks for a BundleConfig file in the given module
// directory. It checks a set of conventional file names and returns the
// path to the first one that exists. An empty string is returned when no
// config file is found — this is not an error because BundleConfig is
// optional for bundletool build-bundle.
func findBundleConfig(moduleDir string) string {
	for _, name := range bundleConfigCandidates {
		candidate := filepath.Join(moduleDir, name)
		if pathIsFile(candidate) {
			return candidate
		}
	}
	return ""
}
