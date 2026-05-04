package graph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Graph struct {
	modules          map[LogicalModuleID]LogicalModule
	variants         map[VariantID]Variant
	materializations map[MaterializationID]Materialization
	artifacts        map[ArtifactID]Artifact
	actions          map[ActionID]Action
	edges            map[EdgeID]Edge
	nextEdge         uint64
}

func New() *Graph {
	return &Graph{
		modules:          map[LogicalModuleID]LogicalModule{},
		variants:         map[VariantID]Variant{},
		materializations: map[MaterializationID]Materialization{},
		artifacts:        map[ArtifactID]Artifact{},
		actions:          map[ActionID]Action{},
		edges:            map[EdgeID]Edge{},
	}
}

func (g *Graph) AddLogicalModule(module LogicalModule) error {
	if err := validateID(module.ID.String(), "logical module"); err != nil {
		return err
	}
	if err := validateID(string(module.Kind), "logical module.kind"); err != nil {
		return err
	}
	if _, ok := g.modules[module.ID]; ok {
		return fmt.Errorf("logical module %q already exists", module.ID)
	}
	g.modules[module.ID] = cloneLogicalModule(module)
	return nil
}

func (g *Graph) AddVariant(variant Variant) error {
	if err := validateID(variant.ID.String(), "variant"); err != nil {
		return err
	}
	if err := validateID(variant.ModuleID.String(), "variant.moduleId"); err != nil {
		return err
	}
	if _, ok := g.modules[variant.ModuleID]; !ok {
		return fmt.Errorf("variant %q references missing logical module %q", variant.ID, variant.ModuleID)
	}
	if _, ok := g.variants[variant.ID]; ok {
		return fmt.Errorf("variant %q already exists", variant.ID)
	}
	g.variants[variant.ID] = cloneVariant(variant)
	return nil
}

func (g *Graph) AddMaterialization(materialization Materialization) error {
	if err := validateID(materialization.ID.String(), "materialization"); err != nil {
		return err
	}
	if err := validateID(materialization.ModuleID.String(), "materialization.moduleId"); err != nil {
		return err
	}
	if err := validateID(materialization.VariantID.String(), "materialization.variantId"); err != nil {
		return err
	}
	if err := validateID(string(materialization.Kind), "materialization.kind"); err != nil {
		return err
	}
	if _, ok := g.modules[materialization.ModuleID]; !ok {
		return fmt.Errorf("materialization %q references missing logical module %q", materialization.ID, materialization.ModuleID)
	}
	variant, ok := g.variants[materialization.VariantID]
	if !ok {
		return fmt.Errorf("materialization %q references missing variant %q", materialization.ID, materialization.VariantID)
	}
	if variant.ModuleID != materialization.ModuleID {
		return fmt.Errorf("materialization %q references variant %q from module %q, not %q", materialization.ID, materialization.VariantID, variant.ModuleID, materialization.ModuleID)
	}
	if _, ok := g.materializations[materialization.ID]; ok {
		return fmt.Errorf("materialization %q already exists", materialization.ID)
	}
	g.materializations[materialization.ID] = cloneMaterialization(materialization)
	return nil
}

func (g *Graph) AddArtifact(artifact Artifact) error {
	if err := validateID(artifact.ID.String(), "artifact"); err != nil {
		return err
	}
	if err := validateID(string(artifact.Kind), "artifact.kind"); err != nil {
		return err
	}
	if _, ok := g.artifacts[artifact.ID]; ok {
		return fmt.Errorf("artifact %q already exists", artifact.ID)
	}
	g.artifacts[artifact.ID] = cloneArtifact(artifact)
	return nil
}

func (g *Graph) AddAction(action Action) error {
	if err := validateID(action.ID.String(), "action"); err != nil {
		return err
	}
	if err := validateID(string(action.Kind), "action.kind"); err != nil {
		return err
	}
	if _, ok := g.actions[action.ID]; ok {
		return fmt.Errorf("action %q already exists", action.ID)
	}
	g.actions[action.ID] = cloneAction(action)
	return nil
}

func (g *Graph) AddEdge(edge Edge) (Edge, error) {
	if err := validateNodeRef(edge.From, "edge.from"); err != nil {
		return Edge{}, err
	}
	if err := validateNodeRef(edge.To, "edge.to"); err != nil {
		return Edge{}, err
	}
	if err := validateEdgeKind(edge.Kind); err != nil {
		return Edge{}, err
	}
	if !g.hasNode(edge.From) {
		return Edge{}, fmt.Errorf("edge from %s does not exist", edge.From.String())
	}
	if !g.hasNode(edge.To) {
		return Edge{}, fmt.Errorf("edge to %s does not exist", edge.To.String())
	}
	if edge.ID == "" {
		edge.ID = g.nextEdgeID()
	}
	if _, ok := g.edges[edge.ID]; ok {
		return Edge{}, fmt.Errorf("edge %q already exists", edge.ID)
	}
	edge.Attributes = cloneStringMap(edge.Attributes)
	g.edges[edge.ID] = edge
	return edge, nil
}

func (g *Graph) LogicalModule(id LogicalModuleID) (LogicalModule, bool) {
	module, ok := g.modules[id]
	return cloneLogicalModule(module), ok
}

func (g *Graph) Variant(id VariantID) (Variant, bool) {
	variant, ok := g.variants[id]
	return cloneVariant(variant), ok
}

func (g *Graph) Materialization(id MaterializationID) (Materialization, bool) {
	materialization, ok := g.materializations[id]
	return cloneMaterialization(materialization), ok
}

func (g *Graph) Artifact(id ArtifactID) (Artifact, bool) {
	artifact, ok := g.artifacts[id]
	return cloneArtifact(artifact), ok
}

func (g *Graph) Action(id ActionID) (Action, bool) {
	action, ok := g.actions[id]
	return cloneAction(action), ok
}

func (g *Graph) Edge(id EdgeID) (Edge, bool) {
	edge, ok := g.edges[id]
	return cloneEdge(edge), ok
}

func (g *Graph) Node(ref NodeRef) (GraphNode, bool) {
	switch ref.Kind {
	case NodeKindLogicalModule:
		node, ok := g.LogicalModule(LogicalModuleID(ref.ID))
		if !ok {
			return nil, false
		}
		return node, true
	case NodeKindVariant:
		node, ok := g.Variant(VariantID(ref.ID))
		if !ok {
			return nil, false
		}
		return node, true
	case NodeKindMaterialization:
		node, ok := g.Materialization(MaterializationID(ref.ID))
		if !ok {
			return nil, false
		}
		return node, true
	case NodeKindArtifact:
		node, ok := g.Artifact(ArtifactID(ref.ID))
		if !ok {
			return nil, false
		}
		return node, true
	case NodeKindAction:
		node, ok := g.Action(ActionID(ref.ID))
		if !ok {
			return nil, false
		}
		return node, true
	default:
		return nil, false
	}
}

func (g *Graph) Nodes() []NodeRef {
	refs := make([]NodeRef, 0, len(g.modules)+len(g.variants)+len(g.materializations)+len(g.artifacts)+len(g.actions))
	for id := range g.modules {
		refs = append(refs, id.Ref())
	}
	for id := range g.variants {
		refs = append(refs, id.Ref())
	}
	for id := range g.materializations {
		refs = append(refs, id.Ref())
	}
	for id := range g.artifacts {
		refs = append(refs, id.Ref())
	}
	for id := range g.actions {
		refs = append(refs, id.Ref())
	}
	sortNodeRefs(refs)
	return refs
}

func (g *Graph) LogicalModules() []LogicalModule {
	out := make([]LogicalModule, 0, len(g.modules))
	for _, module := range g.modules {
		out = append(out, cloneLogicalModule(module))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) Variants() []Variant {
	out := make([]Variant, 0, len(g.variants))
	for _, variant := range g.variants {
		out = append(out, cloneVariant(variant))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModuleID == out[j].ModuleID {
			return out[i].ID < out[j].ID
		}
		return out[i].ModuleID < out[j].ModuleID
	})
	return out
}

func (g *Graph) Materializations() []Materialization {
	out := make([]Materialization, 0, len(g.materializations))
	for _, materialization := range g.materializations {
		out = append(out, cloneMaterialization(materialization))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModuleID == out[j].ModuleID {
			if out[i].VariantID == out[j].VariantID {
				return out[i].ID < out[j].ID
			}
			return out[i].VariantID < out[j].VariantID
		}
		return out[i].ModuleID < out[j].ModuleID
	})
	return out
}

func (g *Graph) Artifacts() []Artifact {
	out := make([]Artifact, 0, len(g.artifacts))
	for _, artifact := range g.artifacts {
		out = append(out, cloneArtifact(artifact))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MaterializationID == out[j].MaterializationID {
			return out[i].ID < out[j].ID
		}
		return out[i].MaterializationID < out[j].MaterializationID
	})
	return out
}

func (g *Graph) Actions() []Action {
	out := make([]Action, 0, len(g.actions))
	for _, action := range g.actions {
		out = append(out, cloneAction(action))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModuleID == out[j].ModuleID {
			if out[i].VariantID == out[j].VariantID {
				return out[i].ID < out[j].ID
			}
			return out[i].VariantID < out[j].VariantID
		}
		return out[i].ModuleID < out[j].ModuleID
	})
	return out
}

func (g *Graph) Edges() []Edge {
	out := make([]Edge, 0, len(g.edges))
	for _, edge := range g.edges {
		out = append(out, cloneEdge(edge))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *Graph) NodeCount() int {
	return len(g.modules) + len(g.variants) + len(g.materializations) + len(g.artifacts) + len(g.actions)
}

func (g *Graph) EdgeCount() int {
	return len(g.edges)
}

func (g *Graph) hasNode(ref NodeRef) bool {
	switch ref.Kind {
	case NodeKindLogicalModule:
		_, ok := g.modules[LogicalModuleID(ref.ID)]
		return ok
	case NodeKindVariant:
		_, ok := g.variants[VariantID(ref.ID)]
		return ok
	case NodeKindMaterialization:
		_, ok := g.materializations[MaterializationID(ref.ID)]
		return ok
	case NodeKindArtifact:
		_, ok := g.artifacts[ArtifactID(ref.ID)]
		return ok
	case NodeKindAction:
		_, ok := g.actions[ActionID(ref.ID)]
		return ok
	default:
		return false
	}
}

func (g *Graph) nextEdgeID() EdgeID {
	g.nextEdge++
	return EdgeID("edge-" + strconv.FormatUint(g.nextEdge, 10))
}

func validateID(value string, label string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s id must not be empty", label)
	}
	return nil
}

func validateNodeRef(ref NodeRef, label string) error {
	if err := validateID(string(ref.Kind), label+".kind"); err != nil {
		return err
	}
	if err := validateID(ref.ID, label+".id"); err != nil {
		return err
	}
	return nil
}

func validateEdgeKind(kind EdgeKind) error {
	if strings.TrimSpace(string(kind)) == "" {
		return fmt.Errorf("edge kind must not be empty")
	}
	return nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneLogicalModule(module LogicalModule) LogicalModule {
	module.Attributes = cloneStringMap(module.Attributes)
	return module
}

func cloneVariant(variant Variant) Variant {
	variant.Flavors = cloneStringSlice(variant.Flavors)
	variant.Attributes = cloneStringMap(variant.Attributes)
	return variant
}

func cloneMaterialization(materialization Materialization) Materialization {
	materialization.SourceRoots = cloneStringSlice(materialization.SourceRoots)
	materialization.ClasspathSnapshotIDs = cloneStringSlice(materialization.ClasspathSnapshotIDs)
	materialization.Attributes = cloneStringMap(materialization.Attributes)
	return materialization
}

func cloneArtifact(artifact Artifact) Artifact {
	artifact.Attributes = cloneStringMap(artifact.Attributes)
	return artifact
}

func cloneAction(action Action) Action {
	action.Inputs = cloneArtifactIDs(action.Inputs)
	action.Outputs = cloneArtifactIDs(action.Outputs)
	action.Attributes = cloneStringMap(action.Attributes)
	return action
}

func cloneEdge(edge Edge) Edge {
	edge.Attributes = cloneStringMap(edge.Attributes)
	return edge
}

func cloneArtifactIDs(values []ArtifactID) []ArtifactID {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]ArtifactID, len(values))
	copy(cloned, values)
	return cloned
}

func sortNodeRefs(refs []NodeRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind == refs[j].Kind {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Kind < refs[j].Kind
	})
}
