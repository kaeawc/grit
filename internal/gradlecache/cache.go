package gradlecache

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Root returns the root of the user's Gradle modules-2 files-2.1 cache.
func Root() string {
	return filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1")
}

// FindArtifactJars returns jar paths for the given Gradle coordinate within
// the local modules-2/files-2.1 cache. It excludes -sources and -javadoc jars.
func FindArtifactJars(group, module, version string) []string {
	if group == "" || module == "" || version == "" {
		return nil
	}
	base := filepath.Join(Root(), group, module, version)
	matches, _ := filepath.Glob(filepath.Join(base, "*", module+"-"+version+"*.jar"))
	var out []string
	for _, match := range matches {
		name := filepath.Base(match)
		if strings.Contains(name, "-sources.jar") || strings.Contains(name, "-javadoc.jar") {
			continue
		}
		out = append(out, match)
	}
	sort.Strings(out)
	return out
}

// LatestVersion returns the lexicographically last version directory present
// for the given group/module within the cache, or "" if none.
func LatestVersion(group, module string) string {
	if group == "" || module == "" {
		return ""
	}
	root := filepath.Join(Root(), group, module)
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)
	if len(versions) == 0 {
		return ""
	}
	return versions[len(versions)-1]
}
