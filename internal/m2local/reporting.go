package m2local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

type CachedResolvedProduct struct {
	CachePath    string              `json:"cachePath,omitempty"`
	ReportPath   string              `json:"reportPath,omitempty"`
	ReplayPath   string              `json:"replayPath,omitempty"`
	LockfilePath string              `json:"lockfilePath,omitempty"`
	Found        bool                `json:"found"`
	Topology     CacheTopology       `json:"topology,omitempty"`
	Inputs       ResolvedCacheInputs `json:"inputs,omitempty"`
	Resolved     Resolved            `json:"resolved,omitempty"`
}

type ResolvedCacheInputs struct {
	CacheVersion     string               `json:"cacheVersion,omitempty"`
	CacheKey         string               `json:"cacheKey,omitempty"`
	CacheStatus      string               `json:"cacheStatus,omitempty"`
	Topology         CacheTopology        `json:"topology,omitempty"`
	Repositories     []project.Repository `json:"repositories,omitempty"`
	Catalog          CatalogInputSummary  `json:"catalog,omitempty"`
	DependencyScopes map[string][]string  `json:"dependencyScopes,omitempty"`
}

type CatalogInputSummary struct {
	Present      bool `json:"present,omitempty"`
	VersionCount int  `json:"versionCount,omitempty"`
	LibraryCount int  `json:"libraryCount,omitempty"`
	BundleCount  int  `json:"bundleCount,omitempty"`
}

func ResolvedCachePath(cacheRoot, workRoot string, repos []project.Repository, cat *catalog.Catalog, deps *modulebuild.Dependencies) (string, error) {
	return New(cacheRoot, workRoot, repos, cat).resolvedCachePath(deps)
}

func LoadCachedResolvedProduct(cacheRoot, workRoot string, repos []project.Repository, cat *catalog.Catalog, deps *modulebuild.Dependencies) (CachedResolvedProduct, error) {
	resolver := New(cacheRoot, workRoot, repos, cat)
	path, err := resolver.resolvedCachePath(deps)
	if err != nil {
		return CachedResolvedProduct{}, err
	}
	inputs, err := resolver.resolvedCacheInputs(deps, false)
	if err != nil {
		return CachedResolvedProduct{}, err
	}
	product := CachedResolvedProduct{
		CachePath:    path,
		ReportPath:   resolvedReportPath(path),
		ReplayPath:   resolvedReplayPath(path),
		LockfilePath: resolvedLockfilePath(path),
		Topology:     resolver.Topology(),
		Inputs:       inputs,
	}
	if !fileExists(path) {
		return product, nil
	}
	product.Inputs.CacheStatus = "hit"
	data, err := os.ReadFile(path)
	if err != nil {
		return CachedResolvedProduct{}, err
	}
	var envelope ResolvedEnvelope
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.SchemaVersion == 1 && envelope.Format == "m2local-resolved" {
		resolved := envelope.Resolved
		resolved.Lockfile = loadOrDeriveResolutionLockfile(product.LockfilePath, resolved)
		if product.ReportPath != "" && !fileExists(product.ReportPath) {
			if err := writeResolutionReportArtifact(product.ReportPath, resolved.Report); err != nil {
				return CachedResolvedProduct{}, err
			}
		}
		if product.ReplayPath != "" && !fileExists(product.ReplayPath) {
			if err := writeResolutionReplayArtifact(product.ReplayPath, resolved.Replay); err != nil {
				return CachedResolvedProduct{}, err
			}
		}
		if product.LockfilePath != "" && !fileExists(product.LockfilePath) {
			if err := writeResolutionLockfileArtifact(product.LockfilePath, resolved.Lockfile); err != nil {
				return CachedResolvedProduct{}, err
			}
		}
		product.Found = true
		product.Topology = envelope.Topology
		product.Resolved = resolved
		if !resolvedArtifactsExist(workRoot, resolved) {
			product.Found = false
		}
		return product, nil
	}
	var resolved Resolved
	if err := json.Unmarshal(data, &resolved); err != nil {
		return CachedResolvedProduct{}, err
	}
	resolved.Lockfile = loadOrDeriveResolutionLockfile(product.LockfilePath, resolved)
	if product.ReportPath != "" && !fileExists(product.ReportPath) {
		if err := writeResolutionReportArtifact(product.ReportPath, resolved.Report); err != nil {
			return CachedResolvedProduct{}, err
		}
	}
	if product.ReplayPath != "" && !fileExists(product.ReplayPath) {
		if err := writeResolutionReplayArtifact(product.ReplayPath, resolved.Replay); err != nil {
			return CachedResolvedProduct{}, err
		}
	}
	if product.LockfilePath != "" && !fileExists(product.LockfilePath) {
		if err := writeResolutionLockfileArtifact(product.LockfilePath, resolved.Lockfile); err != nil {
			return CachedResolvedProduct{}, err
		}
	}
	product.Found = true
	product.Resolved = resolved
	if !resolvedArtifactsExist(workRoot, resolved) {
		product.Found = false
	}
	return product, nil
}

func resolvedArtifactsExist(workRoot string, resolved Resolved) bool {
	for _, path := range append(append(append([]string{}, resolved.CompileJars...), resolved.RuntimeJars...), resolved.TestJars...) {
		if shouldValidateResolvedPath(workRoot, path) && !fileExists(path) {
			return false
		}
	}
	for _, lib := range resolved.AndroidLibraries {
		for _, path := range []string{lib.ManifestPath, lib.ResDir, lib.ClassesJar} {
			if shouldValidateResolvedPath(workRoot, path) && !fileExists(path) {
				return false
			}
		}
	}
	return true
}

func shouldValidateResolvedPath(workRoot, path string) bool {
	if workRoot == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Join(workRoot, ".grit"), path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func (r *Resolver) resolvedCacheInputs(deps *modulebuild.Dependencies, found bool) (ResolvedCacheInputs, error) {
	key, err := r.resolvedCacheKey(deps)
	if err != nil {
		return ResolvedCacheInputs{}, err
	}
	return ResolvedCacheInputs{
		CacheVersion:     resolvedCacheVersion,
		CacheKey:         key,
		CacheStatus:      map[bool]string{true: "hit", false: "miss"}[found],
		Topology:         r.Topology(),
		Repositories:     append([]project.Repository(nil), r.Repositories...),
		Catalog:          summarizeCatalogInputs(r.Catalog),
		DependencyScopes: summarizeDependencyScopes(deps),
	}, nil
}

func summarizeCatalogInputs(cat *catalog.Catalog) CatalogInputSummary {
	if cat == nil {
		return CatalogInputSummary{}
	}
	return CatalogInputSummary{
		Present:      true,
		VersionCount: len(cat.Versions),
		LibraryCount: len(cat.Libraries),
		BundleCount:  len(cat.Bundles),
	}
}

func summarizeDependencyScopes(deps *modulebuild.Dependencies) map[string][]string {
	if deps == nil || len(deps.Scoped) == 0 {
		return nil
	}
	scopes := make([]string, 0, len(deps.Scoped))
	for scope := range deps.Scoped {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	out := make(map[string][]string, len(scopes))
	for _, scope := range scopes {
		refs := deps.Scoped[scope]
		values := make([]string, 0, len(refs))
		for _, ref := range refs {
			values = append(values, ref.Kind+":"+ref.Value)
		}
		sort.Strings(values)
		out[scope] = values
	}
	return out
}

func resolvedLockfilePath(resolvedCachePath string) string {
	return strings.TrimSuffix(resolvedCachePath, ".json") + ".lockfile.json"
}

func resolvedReportPath(resolvedCachePath string) string {
	return strings.TrimSuffix(resolvedCachePath, ".json") + ".report.json"
}

func resolvedReplayPath(resolvedCachePath string) string {
	return strings.TrimSuffix(resolvedCachePath, ".json") + ".replay.json"
}
