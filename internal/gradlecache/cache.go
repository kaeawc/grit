package gradlecache

import (
	"encoding/json"
	"encoding/xml"
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

// FirstArtifactJar returns the first jar path from the cache for the given
// coordinate, or the empty string when none is present.
func FirstArtifactJar(group, module, version string) string {
	jars := FindArtifactJars(group, module, version)
	if len(jars) == 0 {
		return ""
	}
	return jars[0]
}

// ArtifactVersions returns version directories for the given Gradle coordinate
// within the local modules-2/files-2.1 cache, sorted by compare.
func ArtifactVersions(group, module string, compare func(a, b string) int) []string {
	if group == "" || module == "" {
		return nil
	}
	root := filepath.Join(Root(), group, module)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	if compare == nil {
		sort.Strings(versions)
	} else {
		sort.Slice(versions, func(i, j int) bool {
			return compare(versions[i], versions[j]) < 0
		})
	}
	return versions
}

type Dependency struct {
	Group   string
	Module  string
	Version string
}

// ArtifactDependencies returns direct dependency metadata for a cached
// artifact. It prefers Gradle module metadata, then falls back to Maven POMs.
func ArtifactDependencies(group, module, version string) []Dependency {
	if group == "" || module == "" || version == "" {
		return nil
	}
	base := filepath.Join(Root(), group, module, version)
	moduleMatches, _ := filepath.Glob(filepath.Join(base, "*", module+"-"+version+".module"))
	sort.Strings(moduleMatches)
	for _, match := range moduleMatches {
		if deps := parseGradleModuleDependencies(match); len(deps) != 0 {
			return deps
		}
	}
	pomMatches, _ := filepath.Glob(filepath.Join(base, "*", module+"-"+version+".pom"))
	sort.Strings(pomMatches)
	for _, match := range pomMatches {
		if deps := parsePOMDependencies(match); len(deps) != 0 {
			return deps
		}
	}
	return nil
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

func parseGradleModuleDependencies(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Variants []struct {
			Attributes   map[string]string `json:"attributes"`
			Dependencies []struct {
				Group   string `json:"group"`
				Module  string `json:"module"`
				Version struct {
					Requires string `json:"requires"`
					Strictly string `json:"strictly"`
					Prefers  string `json:"prefers"`
				} `json:"version"`
			} `json:"dependencies"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var out []Dependency
	seen := map[string]struct{}{}
	for _, variant := range doc.Variants {
		usage := strings.ToLower(variant.Attributes["org.gradle.usage"])
		if usage != "" && usage != "java-runtime" && usage != "java-api" {
			continue
		}
		for _, dep := range variant.Dependencies {
			version := firstNonEmpty(dep.Version.Strictly, dep.Version.Requires, dep.Version.Prefers)
			key := dep.Group + ":" + dep.Module + ":" + version
			if dep.Group == "" || dep.Module == "" || version == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, Dependency{Group: dep.Group, Module: dep.Module, Version: version})
		}
	}
	return out
}

func parsePOMDependencies(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Dependencies []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
			Scope      string `xml:"scope"`
			Optional   string `xml:"optional"`
		} `xml:"dependencies>dependency"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var out []Dependency
	for _, dep := range doc.Dependencies {
		scope := strings.ToLower(strings.TrimSpace(dep.Scope))
		if scope == "test" || scope == "provided" || strings.EqualFold(strings.TrimSpace(dep.Optional), "true") {
			continue
		}
		version := strings.TrimSpace(dep.Version)
		if strings.HasPrefix(version, "${") {
			continue
		}
		if dep.GroupID == "" || dep.ArtifactID == "" || version == "" {
			continue
		}
		out = append(out, Dependency{Group: dep.GroupID, Module: dep.ArtifactID, Version: version})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
