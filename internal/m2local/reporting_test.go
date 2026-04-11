package m2local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/project"
)

func TestLoadCachedResolvedProductReadsEnvelopeWithoutMaterializingArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	workRoot := t.TempDir()
	deps := &modulebuild.Dependencies{
		Scoped: map[string][]modulebuild.Ref{
			"implementation": {{Kind: "library", Value: "okhttp"}},
		},
	}
	repos := []project.Repository{{Name: "mavenCentral", Kind: "maven", URL: "https://repo1.maven.org/maven2/", Scope: "dependency"}}
	cat := &catalog.Catalog{
		Versions:  map[string]string{"kotlin": "2.0.0"},
		Libraries: map[string]catalog.Library{"okhttp": {Group: "com.squareup.okhttp3", Name: "okhttp", Version: "4.12.0"}},
		Bundles:   map[string][]string{"network": {"okhttp"}},
	}
	path, err := ResolvedCachePath(cacheRoot, workRoot, repos, cat, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ResolvedEnvelope{
		SchemaVersion: 1,
		Format:        "m2local-resolved",
		Topology: CacheTopology{
			SchemaVersion:        1,
			WorkRoot:             workRoot,
			SharedMachineRoot:    filepath.Join(home, ".grit-cache"),
			SharedResolutionRoot: filepath.Join(home, ".grit-cache", "resolve"),
			SharedAARRoot:        filepath.Join(home, ".grit-cache", "aar"),
		},
		Resolved: Resolved{
			CompileJars: []string{filepath.Join(workRoot, "missing.jar")},
			Report: ResolutionReport{
				Selections: []ResolutionSelection{{
					Kind:       "variant_selection",
					Coordinate: "g:m:1.0.0",
					Chosen:     "releaseRuntimeElements",
					MetadataSource: &ResolutionMetadataSource{
						Kind:          "module",
						Path:          filepath.Join(workRoot, ".grit", "metadata", "g", "m", "1.0.0", "m-1.0.0.module"),
						RepositoryURL: "https://repo1.maven.org/maven2/",
						Fetched:       true,
					},
				}},
			},
			Replay: ResolutionReplay{
				Pins: []ResolutionPin{{Coordinate: "g:m:1.0.0"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	lockfilePath := resolvedLockfilePath(path)
	reportPath := resolvedReportPath(path)
	replayPath := resolvedReplayPath(path)
	lockfileData, err := json.Marshal(ResolutionLockfile{
		SchemaVersion: 1,
		Format:        "m2local-lockfile",
		Pins:          []ResolutionPin{{Coordinate: "g:m:1.0.0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockfilePath, lockfileData, 0o644); err != nil {
		t.Fatal(err)
	}

	product, err := LoadCachedResolvedProduct(cacheRoot, workRoot, repos, cat, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !product.Found || product.CachePath != path || product.ReportPath != reportPath || product.ReplayPath != replayPath || product.LockfilePath != lockfilePath {
		t.Fatalf("expected found cached product, got %#v", product)
	}
	if product.Topology.WorkRoot != workRoot {
		t.Fatalf("expected topology from envelope, got %#v", product.Topology)
	}
	if product.Inputs.CacheStatus != "hit" || product.Inputs.CacheVersion != resolvedCacheVersion || product.Inputs.CacheKey == "" {
		t.Fatalf("expected resolver inputs, got %#v", product.Inputs)
	}
	if len(product.Inputs.Repositories) != 1 || product.Inputs.Repositories[0].Name != "mavenCentral" {
		t.Fatalf("expected repository inputs, got %#v", product.Inputs.Repositories)
	}
	if product.Inputs.Catalog.LibraryCount != 1 || !product.Inputs.Catalog.Present {
		t.Fatalf("expected catalog summary, got %#v", product.Inputs.Catalog)
	}
	if got := product.Inputs.DependencyScopes["implementation"]; len(got) != 1 || got[0] != "library:okhttp" {
		t.Fatalf("expected dependency scope summary, got %#v", product.Inputs.DependencyScopes)
	}
	if len(product.Resolved.Report.Selections) != 1 || product.Resolved.Report.Selections[0].Chosen != "releaseRuntimeElements" {
		t.Fatalf("expected report data without materialization, got %#v", product.Resolved.Report)
	}
	if product.Resolved.Report.Selections[0].MetadataSource == nil || product.Resolved.Report.Selections[0].MetadataSource.Kind != "module" {
		t.Fatalf("expected metadata source, got %#v", product.Resolved.Report.Selections[0])
	}
	if product.Resolved.Lockfile.SchemaVersion != 1 || len(product.Resolved.Lockfile.Pins) != 1 {
		t.Fatalf("expected normalized lockfile, got %#v", product.Resolved.Lockfile)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report artifact, got %v", err)
	}
	if _, err := os.Stat(replayPath); err != nil {
		t.Fatalf("expected replay artifact, got %v", err)
	}
}

func TestLoadCachedResolvedProductReturnsCachePathWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	workRoot := t.TempDir()
	deps := &modulebuild.Dependencies{
		Scoped: map[string][]modulebuild.Ref{
			"debugImplementation": {{Kind: "project", Value: ":lib"}},
		},
	}
	path, err := ResolvedCachePath(cacheRoot, workRoot, nil, nil, deps)
	if err != nil {
		t.Fatal(err)
	}

	product, err := LoadCachedResolvedProduct(cacheRoot, workRoot, nil, nil, deps)
	if err != nil {
		t.Fatal(err)
	}
	if product.Found {
		t.Fatalf("expected missing cached product, got %#v", product)
	}
	if product.CachePath != path {
		t.Fatalf("expected cache path for missing product, got %#v want %q", product, path)
	}
	if product.Inputs.CacheStatus != "miss" || product.Inputs.CacheKey == "" {
		t.Fatalf("expected missing-product inputs, got %#v", product.Inputs)
	}
	if got := product.Inputs.DependencyScopes["debugImplementation"]; len(got) != 1 || got[0] != "project::lib" {
		t.Fatalf("expected dependency scope summary, got %#v", product.Inputs.DependencyScopes)
	}
	if product.ReportPath != resolvedReportPath(path) || product.ReplayPath != resolvedReplayPath(path) {
		t.Fatalf("expected report/replay paths for missing product, got %#v", product)
	}
	if product.LockfilePath != resolvedLockfilePath(path) {
		t.Fatalf("expected lockfile path for missing product, got %#v", product)
	}
}
