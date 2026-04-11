package graph

import "sort"

func (g *Graph) ModuleVariants(moduleID LogicalModuleID) []Variant {
	out := make([]Variant, 0)
	for _, variant := range g.variants {
		if variant.ModuleID == moduleID {
			out = append(out, cloneVariant(variant))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) VariantForModule(moduleID LogicalModuleID, name string) (Variant, bool) {
	for _, variant := range g.variants {
		if variant.ModuleID == moduleID && variant.Name == name {
			return cloneVariant(variant), true
		}
	}
	return Variant{}, false
}

func (g *Graph) ResolvedVariant(moduleID LogicalModuleID, name string) (ResolvedVariant, bool) {
	variant, ok := g.VariantForModule(moduleID, name)
	if !ok {
		return ResolvedVariant{}, false
	}
	return ResolvedVariant{
		Coordinate: VariantCoordinate{
			ModuleID: moduleID,
			Name:     name,
		},
		Variant: variant,
	}, true
}

func (g *Graph) ModuleMaterializations(moduleID LogicalModuleID) []Materialization {
	out := make([]Materialization, 0)
	for _, materialization := range g.materializations {
		if materialization.ModuleID == moduleID {
			out = append(out, cloneMaterialization(materialization))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].VariantID == out[j].VariantID {
			return out[i].ID < out[j].ID
		}
		return out[i].VariantID < out[j].VariantID
	})
	return out
}

func (g *Graph) VariantMaterializations(variantID VariantID) []Materialization {
	out := make([]Materialization, 0)
	for _, materialization := range g.materializations {
		if materialization.VariantID == variantID {
			out = append(out, cloneMaterialization(materialization))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) MaterializationArtifacts(materializationID MaterializationID) []Artifact {
	out := make([]Artifact, 0)
	for _, artifact := range g.artifacts {
		if artifact.MaterializationID == materializationID {
			out = append(out, cloneArtifact(artifact))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) ActionInputs(actionID ActionID) []Artifact {
	action, ok := g.actions[actionID]
	if !ok {
		return nil
	}
	out := make([]Artifact, 0, len(action.Inputs))
	for _, id := range action.Inputs {
		if artifact, ok := g.artifacts[id]; ok {
			out = append(out, cloneArtifact(artifact))
		}
	}
	return out
}

func (g *Graph) ActionOutputs(actionID ActionID) []Artifact {
	action, ok := g.actions[actionID]
	if !ok {
		return nil
	}
	out := make([]Artifact, 0, len(action.Outputs))
	for _, id := range action.Outputs {
		if artifact, ok := g.artifacts[id]; ok {
			out = append(out, cloneArtifact(artifact))
		}
	}
	return out
}

func (g *Graph) ActionDependencies(actionID ActionID) []ActionID {
	action, ok := g.actions[actionID]
	if !ok {
		return nil
	}
	out := make([]ActionID, 0, len(action.Inputs))
	seen := map[ActionID]struct{}{}
	for _, input := range action.Inputs {
		artifact, ok := g.artifacts[input]
		if !ok || artifact.ProducedByActionID == "" {
			continue
		}
		if _, ok := seen[artifact.ProducedByActionID]; ok {
			continue
		}
		seen[artifact.ProducedByActionID] = struct{}{}
		out = append(out, artifact.ProducedByActionID)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

func (g *Graph) ActionsForVariant(variantID VariantID) []Action {
	out := make([]Action, 0)
	for _, action := range g.actions {
		if action.VariantID == variantID {
			out = append(out, cloneAction(action))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) ActionsForModule(moduleID LogicalModuleID) []Action {
	out := make([]Action, 0)
	for _, action := range g.actions {
		if action.ModuleID == moduleID {
			out = append(out, cloneAction(action))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].VariantID == out[j].VariantID {
			return out[i].ID < out[j].ID
		}
		return out[i].VariantID < out[j].VariantID
	})
	return out
}

func (g *Graph) ArtifactsProducedByAction(actionID ActionID) []Artifact {
	out := make([]Artifact, 0)
	for _, artifact := range g.artifacts {
		if artifact.ProducedByActionID == actionID {
			out = append(out, cloneArtifact(artifact))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) ActionsConsumingArtifact(artifactID ArtifactID) []Action {
	out := make([]Action, 0)
	for _, action := range g.actions {
		for _, input := range action.Inputs {
			if input == artifactID {
				out = append(out, cloneAction(action))
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) ActionsProducingArtifact(artifactID ArtifactID) []Action {
	out := make([]Action, 0)
	for _, action := range g.actions {
		for _, output := range action.Outputs {
			if output == artifactID {
				out = append(out, cloneAction(action))
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) EdgesFrom(ref NodeRef) []Edge {
	out := make([]Edge, 0)
	for _, edge := range g.edges {
		if edge.From == ref {
			out = append(out, cloneEdge(edge))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) EdgesTo(ref NodeRef) []Edge {
	out := make([]Edge, 0)
	for _, edge := range g.edges {
		if edge.To == ref {
			out = append(out, cloneEdge(edge))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) DependenciesOf(ref NodeRef) []NodeRef {
	out := make([]NodeRef, 0)
	for _, edge := range g.edges {
		if edge.From == ref {
			out = append(out, edge.To)
		}
	}
	sortNodeRefs(out)
	return out
}

func (g *Graph) DependentsOf(ref NodeRef) []NodeRef {
	out := make([]NodeRef, 0)
	for _, edge := range g.edges {
		if edge.To == ref {
			out = append(out, edge.From)
		}
	}
	sortNodeRefs(out)
	return out
}

func (g *Graph) RelatedNodes(ref NodeRef) []NodeRef {
	out := make([]NodeRef, 0)
	out = append(out, g.DependenciesOf(ref)...)
	out = append(out, g.DependentsOf(ref)...)
	sortNodeRefs(out)
	return uniqueNodeRefs(out)
}

func (g *Graph) EdgesOfKind(kind EdgeKind) []Edge {
	out := make([]Edge, 0)
	for _, edge := range g.edges {
		if edge.Kind == kind {
			out = append(out, cloneEdge(edge))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func uniqueNodeRefs(refs []NodeRef) []NodeRef {
	if len(refs) == 0 {
		return nil
	}
	out := refs[:0]
	var prev NodeRef
	for i, ref := range refs {
		if i == 0 || ref != prev {
			out = append(out, ref)
			prev = ref
		}
	}
	return out
}
