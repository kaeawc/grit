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
//
// A probe may be chained to a fallback probe via WithFallback. Reads
// query the primary root first and fall through to the fallback on
// miss. An optional Stager populates the primary root from the
// fallback on first hit so subsequent reads find the artifact locally
// without traversing the chain.
type Probe struct {
	root     string
	fallback *Probe
	stager   Stager
}

// Dependency captures a direct dependency of a coordinate as recorded
// in module metadata or a POM.
type Dependency struct {
	Group   string
	Module  string
	Version string
}

// Stager copies an artifact from a source path on the fallback probe
// into the primary probe's root. Implementations return the
// destination path on success or ("", error) on failure. The supplied
// destDir is the directory the file should land in inside the primary
// root; the caller assembles it so implementations don't need to know
// the cache layout.
type Stager interface {
	Stage(destDir, sourcePath string) (string, error)
}

// StagerFunc adapts a plain function to the Stager interface.
type StagerFunc func(destDir, sourcePath string) (string, error)

// Stage satisfies Stager.
func (f StagerFunc) Stage(destDir, sourcePath string) (string, error) {
	return f(destDir, sourcePath)
}

// NewProbe returns a probe rooted at root. An empty root makes every
// method return the zero value, which lets callers wire a probe that
// is intentionally a no-op (e.g. when no cache directory has been
// staged yet).
func NewProbe(root string) *Probe {
	return &Probe{root: root}
}

// WithFallback returns a shallow copy of p with fallback set. Reads
// against the returned probe query p's root first and fall through to
// fallback on miss. Passing a nil fallback clears any existing one.
func (p *Probe) WithFallback(fallback *Probe) *Probe {
	if p == nil {
		return nil
	}
	cp := *p
	cp.fallback = fallback
	return &cp
}

// WithStaging returns a shallow copy of p that runs stager when a read
// hits the fallback chain rather than the primary root. The stager is
// expected to copy or hardlink the fallback's file into the primary
// root so subsequent reads can find it without descending the chain.
// Passing a nil stager clears any existing one.
func (p *Probe) WithStaging(stager Stager) *Probe {
	if p == nil {
		return nil
	}
	cp := *p
	cp.stager = stager
	return &cp
}

// Root returns the directory the probe reads from.
func (p *Probe) Root() string {
	if p == nil {
		return ""
	}
	return p.root
}

// FindJars returns jar paths for the given coordinate, excluding
// -sources and -javadoc jars. Reads check the primary root first and
// fall through to the fallback chain on miss; when a stager is
// configured the discovered jars are also copied into the primary
// root so subsequent calls resolve locally.
func (p *Probe) FindJars(group, module, version string) []string {
	if p == nil || group == "" || module == "" || version == "" {
		return nil
	}
	if jars := p.localFindJars(group, module, version); len(jars) > 0 {
		return jars
	}
	if p.fallback == nil {
		return nil
	}
	jars := p.fallback.FindJars(group, module, version)
	if len(jars) == 0 || p.stager == nil || p.root == "" {
		return jars
	}
	return p.stageJars(group, module, version, jars)
}

func (p *Probe) localFindJars(group, module, version string) []string {
	if p == nil || p.root == "" {
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

// stageJars materializes each jar via the configured stager, falling
// back to the source path on per-jar failure so the caller can still
// proceed; the next read retries staging.
func (p *Probe) stageJars(group, module, version string, jars []string) []string {
	destDir := filepath.Join(p.root, group, module, version)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return jars
	}
	out := make([]string, 0, len(jars))
	for _, src := range jars {
		staged, err := p.stager.Stage(destDir, src)
		if err != nil || staged == "" {
			out = append(out, src)
			continue
		}
		out = append(out, staged)
	}
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
// compare (lexicographic when compare is nil). The result is the union
// of versions cached at the primary root and any fallback chain.
func (p *Probe) Versions(group, module string, compare func(a, b string) int) []string {
	if p == nil || group == "" || module == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var versions []string
	for _, v := range p.localVersions(group, module) {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		versions = append(versions, v)
	}
	for f := p.fallback; f != nil; f = f.fallback {
		for _, v := range f.localVersions(group, module) {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			versions = append(versions, v)
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

func (p *Probe) localVersions(group, module string) []string {
	if p == nil || p.root == "" {
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
// back to POMs when no module file is present. Reads check the
// primary root first and fall through to the fallback chain on miss.
func (p *Probe) Dependencies(group, module, version string) []Dependency {
	if p == nil || group == "" || module == "" || version == "" {
		return nil
	}
	if deps := p.localDependencies(group, module, version); len(deps) > 0 {
		return deps
	}
	if p.fallback == nil {
		return nil
	}
	return p.fallback.Dependencies(group, module, version)
}

func (p *Probe) localDependencies(group, module, version string) []Dependency {
	if p == nil || p.root == "" {
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
