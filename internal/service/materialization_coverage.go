package service

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

// MaterializationCoverageResult lists every catalog-resolved library
// declared as a dependency of a module and reports whether a
// corresponding artifact has landed in the project's local Maven
// projection (`.grit/worktree/materialized-m2`).
//
// Used to diagnose post-build "unresolved reference" errors that turn
// out to be about a declared library never reaching the compile
// classpath (rate-limited fetch, repo content-filter, KMP variant
// mismatch, version-conflict downgrade, etc.). The report doesn't
// re-run resolution; it answers the question "given the project as it
// currently sits on disk, what's missing?".
type MaterializationCoverageResult struct {
	Repo       string                          `json:"repo"`
	Modules    []ModuleMaterializationCoverage `json:"modules"`
	MissingAll []MaterializationCoverageEntry  `json:"missing,omitempty"`
}

// ModuleMaterializationCoverage groups the coverage entries by module.
type ModuleMaterializationCoverage struct {
	Module  string                         `json:"module"`
	Entries []MaterializationCoverageEntry `json:"entries"`
}

// MaterializationCoverageEntry is one declared library, the coordinate
// it resolved to, and whether the materialization layer has anything
// for it.
type MaterializationCoverageEntry struct {
	Alias        string `json:"alias"`
	Group        string `json:"group"`
	Module       string `json:"module"`
	Version      string `json:"version,omitempty"`
	Status       string `json:"status"`
	Detail       string `json:"detail,omitempty"`
	VariantTried string `json:"variantTried,omitempty"`
}

// MaterializationCoverage returns a per-module breakdown of declared
// library coverage in the materialized-m2 projection. It deliberately
// only looks at disk state (no resolver invocation, no network) so it
// stays cheap and produces a snapshot of the state that the compile
// step is about to consume.
func (s *Service) MaterializationCoverage(prj *project.Project) MaterializationCoverageResult {
	if prj == nil {
		return MaterializationCoverageResult{}
	}
	out := MaterializationCoverageResult{Repo: prj.RootDir}
	cat, _ := dependencywiring.LoadCatalog(prj)
	mavenRoot := dependencywiring.MaterializedRepositoryRoot(prj.RootDir)
	missing := []MaterializationCoverageEntry{}
	for _, mod := range prj.Modules {
		mod := mod
		deps, err := modulebuild.ParseDependenciesForModule(mod.BuildFile, prj.RootDir, mod.Plugins)
		if err != nil {
			continue
		}
		entries := []MaterializationCoverageEntry{}
		seen := map[string]struct{}{}
		for _, ref := range allLibraryRefs(deps) {
			lib, err := cat.ResolveLibrary(ref.Value)
			if err != nil || lib.Group == "" || lib.Name == "" {
				entries = append(entries, MaterializationCoverageEntry{
					Alias:  ref.Value,
					Status: "unresolved",
					Detail: "catalog has no entry for this alias",
				})
				continue
			}
			key := lib.Group + ":" + lib.Name + ":" + lib.Version
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			status, detail, variantTried := evaluateCoverage(mavenRoot, lib)
			entry := MaterializationCoverageEntry{
				Alias:        ref.Value,
				Group:        lib.Group,
				Module:       lib.Name,
				Version:      lib.Version,
				Status:       status,
				Detail:       detail,
				VariantTried: variantTried,
			}
			entries = append(entries, entry)
			if status != "ok" {
				missing = append(missing, entry)
			}
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Group+":"+entries[i].Module < entries[j].Group+":"+entries[j].Module
		})
		out.Modules = append(out.Modules, ModuleMaterializationCoverage{
			Module:  mod.Path,
			Entries: entries,
		})
	}
	sort.Slice(out.Modules, func(i, j int) bool { return out.Modules[i].Module < out.Modules[j].Module })
	out.MissingAll = dedupeCoverageEntries(missing)
	return out
}

// allLibraryRefs returns every `library:`-kind ref across the standard
// dependency scopes. Bundles are not expanded here — the catalog already
// records each bundle's member entries as Library entries, and reporting
// the bundle alone would hide whether each member made it through.
func allLibraryRefs(deps *modulebuild.Dependencies) []modulebuild.Ref {
	if deps == nil {
		return nil
	}
	var out []modulebuild.Ref
	for _, group := range [][]modulebuild.Ref{
		deps.Main, deps.Debug, deps.Test, deps.AndroidTest, deps.CompileOnly,
		deps.RuntimeOnly, deps.TestCompileOnly, deps.TestRuntimeOnly,
		deps.AndroidTestCompileOnly, deps.AndroidTestRuntimeOnly, deps.CoreLibraryDesugaring,
	} {
		for _, ref := range group {
			if ref.Kind == "library" {
				out = append(out, ref)
			}
		}
	}
	return out
}

// evaluateCoverage answers "is there a .jar or .aar for this coord in
// the local materialized projection?". Probes the umbrella module path
// first; if the umbrella has no jar/aar, falls back to the conventional
// KMP variant suffixes (-jvm, -android) so the report tracks Gradle's
// usual availableAt redirects.
func evaluateCoverage(mavenRoot string, lib catalog.Library) (status, detail, variantTried string) {
	if lib.Version == "" {
		return "unresolved", "library has no version (catalog declares it without version.ref and no managed-version found)", ""
	}
	groupPath := filepath.Join(strings.Split(lib.Group, ".")...)
	candidates := []string{lib.Name, lib.Name + "-jvm", lib.Name + "-android"}
	var tried []string
	for _, name := range candidates {
		dir := filepath.Join(mavenRoot, groupPath, name, lib.Version)
		if hasMaterializedArtifact(dir) {
			if name == lib.Name {
				return "ok", "", ""
			}
			return "ok", "resolved via platform variant", name
		}
		tried = append(tried, name)
	}
	return "missing", "no .jar or .aar found in materialized-m2 for any of " + strings.Join(tried, ", "), ""
}

func hasMaterializedArtifact(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".aar") {
			return true
		}
	}
	return false
}

func dedupeCoverageEntries(in []MaterializationCoverageEntry) []MaterializationCoverageEntry {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []MaterializationCoverageEntry
	for _, e := range in {
		key := e.Group + ":" + e.Module + ":" + e.Version
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Group+":"+out[i].Module < out[j].Group+":"+out[j].Module
	})
	return out
}
