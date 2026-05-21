package gradlecache

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Probe is a read-only view of a Maven-layout artifact cache rooted at
// a configurable directory. Layouts mirror the modules-2/files-2.1
// convention used by common build tools. The zero value (and a nil
// receiver) is a valid no-op probe so callers can omit the empty-case
// guards when threading the type through optional code paths.
type Probe struct {
	root string
}

// Dependency captures a direct dependency of a coordinate as recorded
// in module metadata or a POM.
type Dependency struct {
	Group   string
	Module  string
	Version string
}

// NewProbe returns a probe rooted at root. An empty root makes every
// method return the zero value, which lets callers wire a probe that
// is intentionally a no-op (e.g. when no cache directory has been
// staged yet).
func NewProbe(root string) *Probe {
	return &Probe{root: root}
}

// Root returns the directory the probe reads from.
func (p *Probe) Root() string {
	if p == nil {
		return ""
	}
	return p.root
}

// FindJars returns jar paths for the given coordinate, excluding
// -sources and -javadoc jars.
func (p *Probe) FindJars(group, module, version string) []string {
	if p == nil || p.root == "" || group == "" || module == "" || version == "" {
		return nil
	}
	base := filepath.Join(p.root, group, module, version)
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

// FirstJar returns the first jar from FindJars, or "" when none is
// present.
func (p *Probe) FirstJar(group, module, version string) string {
	jars := p.FindJars(group, module, version)
	if len(jars) == 0 {
		return ""
	}
	return jars[0]
}

// Versions returns version directories for the coordinate, sorted by
// compare (lexicographic when compare is nil).
func (p *Probe) Versions(group, module string, compare func(a, b string) int) []string {
	if p == nil || p.root == "" || group == "" || module == "" {
		return nil
	}
	root := filepath.Join(p.root, group, module)
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

// LatestVersion returns the lexicographically last version directory
// for the coordinate, or "" when none is present.
func (p *Probe) LatestVersion(group, module string) string {
	versions := p.Versions(group, module, nil)
	if len(versions) == 0 {
		return ""
	}
	return versions[len(versions)-1]
}

// Dependencies returns direct dependency metadata for the coordinate,
// preferring module metadata (richer variant-aware info) and falling
// back to POMs when no module file is present.
func (p *Probe) Dependencies(group, module, version string) []Dependency {
	if p == nil || p.root == "" || group == "" || module == "" || version == "" {
		return nil
	}
	base := filepath.Join(p.root, group, module, version)
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

// Root returns the default cache root that DefaultProbe is rooted at.
// Callers that need a different root should construct a Probe via
// NewProbe.
func Root() string {
	return filepath.Join(os.Getenv("HOME"), ".gradle", "caches", "modules-2", "files-2.1")
}

// DefaultProbe returns a Probe rooted at the package default cache.
// Callers that previously reached for the package-level wrappers
// should migrate to this helper (or accept a *Probe argument) so the
// cache root remains a single threadable value.
//
// The root is resolved on each call so HOME changes (e.g. t.Setenv in
// tests) are honored without further plumbing.
func DefaultProbe() *Probe {
	return NewProbe(Root())
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
