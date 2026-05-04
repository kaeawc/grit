package m2local

import "path/filepath"

type CacheLayer struct {
	Name    string `json:"name,omitempty"`
	Root    string `json:"root,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Content string `json:"content,omitempty"`
	Shared  bool   `json:"shared,omitempty"`
}

type CacheTopology struct {
	SchemaVersion        int          `json:"schemaVersion"`
	CacheRoot            string       `json:"cacheRoot,omitempty"`
	WorkRoot             string       `json:"workRoot,omitempty"`
	WorkMetadataRoot     string       `json:"workMetadataRoot,omitempty"`
	SharedMachineRoot    string       `json:"sharedMachineRoot,omitempty"`
	SharedResolutionRoot string       `json:"sharedResolutionRoot,omitempty"`
	SharedAARRoot        string       `json:"sharedAarRoot,omitempty"`
	Layers               []CacheLayer `json:"layers,omitempty"`
}

func (r *Resolver) Topology() CacheTopology {
	cacheRoot := ""
	workRoot := ""
	if r != nil {
		cacheRoot = r.CacheRoot
		workRoot = r.WorkRoot
	}
	topology := CacheTopology{
		SchemaVersion:        1,
		CacheRoot:            cacheRoot,
		WorkRoot:             workRoot,
		WorkMetadataRoot:     workMetadataRoot(workRoot),
		SharedMachineRoot:    sharedMachineCacheRoot(),
		SharedResolutionRoot: sharedResolveCacheRoot(),
		SharedAARRoot:        sharedAARCacheRoot(),
	}
	topology.Layers = cacheTopologyLayers(topology)
	return topology
}

func workMetadataRoot(workRoot string) string {
	if workRoot == "" {
		return ""
	}
	return filepath.Join(workRoot, ".grit", "metadata")
}

func cacheTopologyLayers(topology CacheTopology) []CacheLayer {
	layers := make([]CacheLayer, 0, 5)
	if topology.CacheRoot != "" {
		layers = append(layers, CacheLayer{
			Name:    "resolver-artifacts",
			Root:    topology.CacheRoot,
			Scope:   "machine",
			Content: "dependency-artifacts",
			Shared:  true,
		})
	}
	if topology.WorkMetadataRoot != "" {
		layers = append(layers, CacheLayer{
			Name:    "work-metadata",
			Root:    topology.WorkMetadataRoot,
			Scope:   "worktree",
			Content: "fetched-metadata",
			Shared:  false,
		})
	}
	if topology.SharedMachineRoot != "" {
		layers = append(layers, CacheLayer{
			Name:    "shared-machine",
			Root:    topology.SharedMachineRoot,
			Scope:   "machine",
			Content: "shared-root",
			Shared:  true,
		})
	}
	if topology.SharedResolutionRoot != "" {
		layers = append(layers, CacheLayer{
			Name:    "shared-resolved",
			Root:    topology.SharedResolutionRoot,
			Scope:   "machine",
			Content: "resolved-products",
			Shared:  true,
		})
	}
	if topology.SharedAARRoot != "" {
		layers = append(layers, CacheLayer{
			Name:    "shared-aar",
			Root:    topology.SharedAARRoot,
			Scope:   "machine",
			Content: "aar-extractions",
			Shared:  true,
		})
	}
	return layers
}
