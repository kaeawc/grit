package dependencywiring

import (
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
)

// ToolDependency describes one Maven artifact that belongs to a compiler or
// packaging toolchain. Role lets callers recover specific entrypoint jars while
// still resolving the whole set through the metadata-backed dependency resolver.
type ToolDependency struct {
	Group    string
	Module   string
	Version  string
	Role     string
	Optional bool
}

// ToolDependencySet is a named group of artifacts resolved as one metadata
// product for a toolchain.
type ToolDependencySet struct {
	Name         string
	Dependencies []ToolDependency
}

// ResolvedToolDependencySet is the materialized result of ToolDependencySet.
type ResolvedToolDependencySet struct {
	Resolved *m2local.Resolved
	ByRole   map[string][]string
}

// ResolveToolDependencySet resolves a tool dependency set with resolver and
// indexes materialized jars by the roles declared on direct dependencies.
func ResolveToolDependencySet(resolver interface {
	Resolve(*modulebuild.Dependencies) (*m2local.Resolved, error)
}, set ToolDependencySet) (*ResolvedToolDependencySet, error) {
	deps := &modulebuild.Dependencies{Main: make([]modulebuild.Ref, 0, len(set.Dependencies))}
	roles := map[lockfile.Coordinate][]string{}
	for _, dep := range set.Dependencies {
		if strings.TrimSpace(dep.Group) == "" || strings.TrimSpace(dep.Module) == "" || strings.TrimSpace(dep.Version) == "" {
			continue
		}
		coord := dep.Group + ":" + dep.Module + ":" + dep.Version
		if !dep.Optional {
			deps.Main = append(deps.Main, modulebuild.Ref{Kind: "raw", Value: coord})
		}
		if strings.TrimSpace(dep.Role) != "" {
			key := lockfile.Coordinate{Group: dep.Group, Artifact: dep.Module, Version: dep.Version}
			roles[key] = append(roles[key], dep.Role)
		}
	}
	resolved, err := resolver.Resolve(deps)
	if err != nil || resolved == nil {
		return nil, err
	}
	for _, dep := range set.Dependencies {
		if !dep.Optional || strings.TrimSpace(dep.Group) == "" || strings.TrimSpace(dep.Module) == "" || strings.TrimSpace(dep.Version) == "" {
			continue
		}
		optionalResolved, err := resolver.Resolve(&modulebuild.Dependencies{
			Main: []modulebuild.Ref{{Kind: "raw", Value: dep.Group + ":" + dep.Module + ":" + dep.Version}},
		})
		if err != nil || optionalResolved == nil {
			continue
		}
		resolved = mergeResolvedToolDependencies(resolved, optionalResolved)
	}
	out := &ResolvedToolDependencySet{
		Resolved: resolved,
		ByRole:   map[string][]string{},
	}
	for _, path := range allToolJars(resolved) {
		for _, role := range rolesForToolJar(path, roles) {
			out.ByRole[role] = append(out.ByRole[role], path)
		}
	}
	return out, nil
}

func mergeResolvedToolDependencies(base, extra *m2local.Resolved) *m2local.Resolved {
	if base == nil {
		return extra
	}
	if extra == nil {
		return base
	}
	merged := *base
	merged.CompileJars = mergeUniquePaths(append(append([]string{}, base.CompileJars...), extra.CompileJars...))
	merged.RuntimeJars = mergeUniquePaths(append(append([]string{}, base.RuntimeJars...), extra.RuntimeJars...))
	merged.TestJars = mergeUniquePaths(append(append([]string{}, base.TestJars...), extra.TestJars...))
	merged.AndroidLibraries = append(append([]m2local.AndroidLibrary{}, base.AndroidLibraries...), extra.AndroidLibraries...)
	return &merged
}

func (r *ResolvedToolDependencySet) Jars(role string) []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.ByRole[role]...)
}

func (r *ResolvedToolDependencySet) FirstJar(role string) string {
	jars := r.Jars(role)
	if len(jars) == 0 {
		return ""
	}
	return jars[0]
}

func allToolJars(resolved *m2local.Resolved) []string {
	if resolved == nil {
		return nil
	}
	return mergeUniquePaths(append(append(append([]string{}, resolved.CompileJars...), resolved.RuntimeJars...), resolved.TestJars...))
}

func rolesForToolJar(path string, roles map[lockfile.Coordinate][]string) []string {
	if coord, ok := CoordinateFromMaterializedPath(path); ok {
		return roles[coord]
	}
	base := filepath.Base(path)
	for coord, coordRoles := range roles {
		if strings.HasPrefix(base, coord.Artifact+"-"+coord.Version) && strings.HasSuffix(base, ".jar") {
			return coordRoles
		}
	}
	return nil
}

func mergeUniquePaths(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		if strings.TrimSpace(path) == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}
