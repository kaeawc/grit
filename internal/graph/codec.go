package graph

import "sort"

type Snapshot struct {
	LogicalModules   []LogicalModule   `json:"logicalModules,omitempty"`
	Variants         []Variant         `json:"variants,omitempty"`
	Materializations []Materialization `json:"materializations,omitempty"`
	Artifacts        []Artifact        `json:"artifacts,omitempty"`
	Actions          []Action          `json:"actions,omitempty"`
	Edges            []Edge            `json:"edges,omitempty"`
}

func (g *Graph) Snapshot() Snapshot {
	if g == nil {
		return Snapshot{}
	}
	snapshot := Snapshot{
		LogicalModules:   g.LogicalModules(),
		Variants:         make([]Variant, 0, len(g.variants)),
		Materializations: make([]Materialization, 0, len(g.materializations)),
		Artifacts:        make([]Artifact, 0, len(g.artifacts)),
		Actions:          make([]Action, 0, len(g.actions)),
		Edges:            make([]Edge, 0, len(g.edges)),
	}
	for _, variant := range g.variants {
		snapshot.Variants = append(snapshot.Variants, cloneVariant(variant))
	}
	for _, materialization := range g.materializations {
		snapshot.Materializations = append(snapshot.Materializations, cloneMaterialization(materialization))
	}
	for _, artifact := range g.artifacts {
		snapshot.Artifacts = append(snapshot.Artifacts, cloneArtifact(artifact))
	}
	for _, action := range g.actions {
		snapshot.Actions = append(snapshot.Actions, cloneAction(action))
	}
	for _, edge := range g.edges {
		snapshot.Edges = append(snapshot.Edges, cloneEdge(edge))
	}
	sort.Slice(snapshot.Variants, func(i, j int) bool { return snapshot.Variants[i].ID < snapshot.Variants[j].ID })
	sort.Slice(snapshot.Materializations, func(i, j int) bool { return snapshot.Materializations[i].ID < snapshot.Materializations[j].ID })
	sort.Slice(snapshot.Artifacts, func(i, j int) bool { return snapshot.Artifacts[i].ID < snapshot.Artifacts[j].ID })
	sort.Slice(snapshot.Actions, func(i, j int) bool { return snapshot.Actions[i].ID < snapshot.Actions[j].ID })
	sort.Slice(snapshot.Edges, func(i, j int) bool { return snapshot.Edges[i].ID < snapshot.Edges[j].ID })
	return snapshot
}

func FromSnapshot(snapshot Snapshot) (*Graph, error) {
	g := New()
	for _, module := range snapshot.LogicalModules {
		if err := g.AddLogicalModule(module); err != nil {
			return nil, err
		}
	}
	for _, variant := range snapshot.Variants {
		if err := g.AddVariant(variant); err != nil {
			return nil, err
		}
	}
	for _, materialization := range snapshot.Materializations {
		if err := g.AddMaterialization(materialization); err != nil {
			return nil, err
		}
	}
	for _, artifact := range snapshot.Artifacts {
		if err := g.AddArtifact(artifact); err != nil {
			return nil, err
		}
	}
	for _, action := range snapshot.Actions {
		if err := g.AddAction(action); err != nil {
			return nil, err
		}
	}
	for _, edge := range snapshot.Edges {
		if _, err := g.AddEdge(edge); err != nil {
			return nil, err
		}
	}
	return g, nil
}
