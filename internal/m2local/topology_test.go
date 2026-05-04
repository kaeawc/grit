package m2local

import (
	"path/filepath"
	"testing"
)

func TestResolverTopologyIncludesKnownCacheLayers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	workRoot := filepath.Join(t.TempDir(), "workspace")
	resolver := New(cacheRoot, workRoot, nil, nil)

	topology := resolver.Topology()
	if topology.CacheRoot != cacheRoot {
		t.Fatalf("expected cache root, got %#v", topology)
	}
	if topology.WorkMetadataRoot != filepath.Join(workRoot, ".grit", "metadata") {
		t.Fatalf("expected work metadata root, got %#v", topology)
	}
	if len(topology.Layers) != 5 {
		t.Fatalf("expected five known layers, got %#v", topology.Layers)
	}
	expected := map[string]CacheLayer{
		"resolver-artifacts": {
			Name:    "resolver-artifacts",
			Root:    cacheRoot,
			Scope:   "machine",
			Content: "dependency-artifacts",
			Shared:  true,
		},
		"work-metadata": {
			Name:    "work-metadata",
			Root:    filepath.Join(workRoot, ".grit", "metadata"),
			Scope:   "worktree",
			Content: "fetched-metadata",
			Shared:  false,
		},
		"shared-machine": {
			Name:    "shared-machine",
			Root:    filepath.Join(home, ".grit"),
			Scope:   "machine",
			Content: "shared-root",
			Shared:  true,
		},
		"shared-resolved": {
			Name:    "shared-resolved",
			Root:    filepath.Join(home, ".grit", "resolve"),
			Scope:   "machine",
			Content: "resolved-products",
			Shared:  true,
		},
		"shared-aar": {
			Name:    "shared-aar",
			Root:    filepath.Join(home, ".grit", "aar"),
			Scope:   "machine",
			Content: "aar-extractions",
			Shared:  true,
		},
	}
	for _, layer := range topology.Layers {
		want, ok := expected[layer.Name]
		if !ok {
			t.Fatalf("unexpected layer %#v", layer)
		}
		if layer != want {
			t.Fatalf("unexpected layer %#v want %#v", layer, want)
		}
		delete(expected, layer.Name)
	}
	if len(expected) != 0 {
		t.Fatalf("missing layers %#v", expected)
	}
}

func TestNilResolverTopologyOmitsWorkSpecificLayers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var resolver *Resolver
	topology := resolver.Topology()
	if topology.CacheRoot != "" || topology.WorkRoot != "" || topology.WorkMetadataRoot != "" {
		t.Fatalf("expected empty resolver-local roots, got %#v", topology)
	}
	if len(topology.Layers) != 3 {
		t.Fatalf("expected shared-only layers, got %#v", topology.Layers)
	}
	for _, layer := range topology.Layers {
		if layer.Name == "resolver-artifacts" || layer.Name == "work-metadata" {
			t.Fatalf("expected nil resolver topology to omit local layers, got %#v", topology.Layers)
		}
	}
}
