package graph

import "testing"

func TestGraphStoresSemanticNodesAndQueriesRelations(t *testing.T) {
	g := New()

	module := LogicalModule{
		ID:   LogicalModuleID("module.app"),
		Name: "app",
		Path: ":app",
		Dir:  "/tmp/app",
		Kind: ModuleKindAndroidApplication,
	}
	variant := Variant{
		ID:        VariantID("variant.debug"),
		ModuleID:  module.ID,
		Name:      "debug",
		BuildType: "debug",
		Flavors:   []string{"free"},
	}
	materialization := Materialization{
		ID:          MaterializationID("materialization.debug"),
		ModuleID:    module.ID,
		VariantID:   variant.ID,
		Kind:        MaterializationKindSourceBacked,
		SourceRoots: []string{"src/main"},
	}
	sourceArtifact := Artifact{
		ID:                ArtifactID("artifact.source"),
		MaterializationID: materialization.ID,
		Kind:              ArtifactKindDirectory,
		Path:              "src/main",
	}
	classesArtifact := Artifact{
		ID:                 ArtifactID("artifact.classes"),
		MaterializationID:  materialization.ID,
		ProducedByActionID: ActionID("action.compile"),
		Kind:               ArtifactKindJar,
		Path:               "build/classes.jar",
	}
	action := Action{
		ID:        ActionID("action.compile"),
		ModuleID:  module.ID,
		VariantID: variant.ID,
		Name:      "compileDebug",
		Kind:      ActionKindCompile,
		Inputs:    []ArtifactID{sourceArtifact.ID},
		Outputs:   []ArtifactID{classesArtifact.ID},
	}

	mustAdd(t, g.AddLogicalModule(module))
	mustAdd(t, g.AddVariant(variant))
	mustAdd(t, g.AddMaterialization(materialization))
	mustAdd(t, g.AddArtifact(sourceArtifact))
	mustAdd(t, g.AddArtifact(classesArtifact))
	mustAdd(t, g.AddAction(action))

	if got, ok := g.Node(module.Ref()); !ok {
		t.Fatalf("module ref not found")
	} else if _, ok := got.(LogicalModule); !ok {
		t.Fatalf("node lookup returned %T, want LogicalModule", got)
	}

	if got := g.Nodes(); len(got) != 6 {
		t.Fatalf("node count = %d, want 6", len(got))
	}

	variants := g.ModuleVariants(module.ID)
	if len(variants) != 1 || variants[0].ID != variant.ID {
		t.Fatalf("module variants = %#v, want %#v", variants, []Variant{variant})
	}

	materializations := g.VariantMaterializations(variant.ID)
	if len(materializations) != 1 || materializations[0].ID != materialization.ID {
		t.Fatalf("variant materializations = %#v, want %#v", materializations, []Materialization{materialization})
	}

	artifacts := g.MaterializationArtifacts(materialization.ID)
	if len(artifacts) != 2 {
		t.Fatalf("materialization artifacts = %d, want 2", len(artifacts))
	}
	gotArtifactIDs := map[ArtifactID]bool{}
	for _, artifact := range artifacts {
		gotArtifactIDs[artifact.ID] = true
	}
	if !gotArtifactIDs[sourceArtifact.ID] || !gotArtifactIDs[classesArtifact.ID] {
		t.Fatalf("materialization artifacts = %#v, want both source and classes", artifacts)
	}

	inputs := g.ActionInputs(action.ID)
	if len(inputs) != 1 || inputs[0].ID != sourceArtifact.ID {
		t.Fatalf("action inputs = %#v, want source artifact", inputs)
	}

	outputs := g.ActionOutputs(action.ID)
	if len(outputs) != 1 || outputs[0].ID != classesArtifact.ID {
		t.Fatalf("action outputs = %#v, want classes artifact", outputs)
	}

	if produced := g.ArtifactsProducedByAction(action.ID); len(produced) != 1 || produced[0].ID != classesArtifact.ID {
		t.Fatalf("artifacts produced by action = %#v, want classes artifact", produced)
	}

	if resolved, ok := g.ResolvedVariant(module.ID, variant.Name); !ok {
		t.Fatal("expected resolved variant lookup")
	} else if resolved.Coordinate.ModuleID != module.ID || resolved.Coordinate.Name != variant.Name || resolved.Variant.ID != variant.ID {
		t.Fatalf("unexpected resolved variant: %#v", resolved)
	}

	moduleEdge, err := g.AddEdge(Edge{From: module.Ref(), To: variant.Ref(), Kind: EdgeKindContains})
	mustAdd(t, err)
	variantEdge, err := g.AddEdge(Edge{From: variant.Ref(), To: materialization.Ref(), Kind: EdgeKindRealizes})
	mustAdd(t, err)
	consumeEdge, err := g.AddEdge(Edge{From: action.Ref(), To: sourceArtifact.Ref(), Kind: EdgeKindConsumes})
	mustAdd(t, err)
	produceEdge, err := g.AddEdge(Edge{From: action.Ref(), To: classesArtifact.Ref(), Kind: EdgeKindProduces})
	mustAdd(t, err)

	if got := g.DependenciesOf(module.Ref()); len(got) != 1 || got[0] != variant.Ref() {
		t.Fatalf("module dependencies = %#v, want variant ref", got)
	}
	if got := g.DependentsOf(variant.Ref()); len(got) != 1 || got[0] != module.Ref() {
		t.Fatalf("variant dependents = %#v, want module ref", got)
	}
	if got := g.DependenciesOf(action.Ref()); len(got) != 2 {
		t.Fatalf("action dependencies = %#v, want 2 refs", got)
	}
	if got := g.DependentsOf(sourceArtifact.Ref()); len(got) != 1 || got[0] != action.Ref() {
		t.Fatalf("source artifact dependents = %#v, want action ref", got)
	}

	edgesFromAction := g.EdgesFrom(action.Ref())
	if len(edgesFromAction) != 2 {
		t.Fatalf("edges from action = %#v, want 2", edgesFromAction)
	}
	if edgesFromAction[0].ID != consumeEdge.ID || edgesFromAction[1].ID != produceEdge.ID {
		t.Fatalf("edges from action = %#v, want consume then produce", edgesFromAction)
	}

	edgesToVariant := g.EdgesTo(variant.Ref())
	if len(edgesToVariant) != 1 || edgesToVariant[0].ID != moduleEdge.ID {
		t.Fatalf("edges to variant = %#v, want module edge", edgesToVariant)
	}
	edgesToMaterialization := g.EdgesTo(materialization.Ref())
	if len(edgesToMaterialization) != 1 || edgesToMaterialization[0].ID != variantEdge.ID {
		t.Fatalf("edges to materialization = %#v, want variant edge", edgesToMaterialization)
	}
}

func TestGraphActionDependencies(t *testing.T) {
	g := New()

	module := LogicalModule{ID: LogicalModuleID("module.app"), Kind: ModuleKindAndroidApplication}
	variant := Variant{ID: VariantID("variant.debug"), ModuleID: module.ID}
	materialization := Materialization{ID: MaterializationID("materialization.debug"), ModuleID: module.ID, VariantID: variant.ID, Kind: MaterializationKindSourceBacked}
	sourceArtifact := Artifact{ID: ArtifactID("artifact.source"), MaterializationID: materialization.ID, Kind: ArtifactKindDirectory}
	classesArtifact := Artifact{ID: ArtifactID("artifact.classes"), MaterializationID: materialization.ID, ProducedByActionID: ActionID("action.compile"), Kind: ArtifactKindJar}
	linkArtifact := Artifact{ID: ArtifactID("artifact.link"), MaterializationID: materialization.ID, ProducedByActionID: ActionID("action.link"), Kind: ArtifactKindOther}
	compile := Action{ID: ActionID("action.compile"), ModuleID: module.ID, VariantID: variant.ID, Kind: ActionKindCompile, Outputs: []ArtifactID{classesArtifact.ID}}
	link := Action{ID: ActionID("action.link"), ModuleID: module.ID, VariantID: variant.ID, Kind: ActionKindPackage, Inputs: []ArtifactID{classesArtifact.ID}, Outputs: []ArtifactID{linkArtifact.ID}}

	mustAdd(t, g.AddLogicalModule(module))
	mustAdd(t, g.AddVariant(variant))
	mustAdd(t, g.AddMaterialization(materialization))
	mustAdd(t, g.AddArtifact(sourceArtifact))
	mustAdd(t, g.AddArtifact(classesArtifact))
	mustAdd(t, g.AddArtifact(linkArtifact))
	mustAdd(t, g.AddAction(compile))
	mustAdd(t, g.AddAction(link))

	deps := g.ActionDependencies(link.ID)
	if len(deps) != 1 || deps[0] != compile.ID {
		t.Fatalf("action dependencies = %#v, want compile dependency", deps)
	}
}

func TestGraphRejectsInvalidReferencesAndDuplicates(t *testing.T) {
	g := New()

	module := LogicalModule{ID: LogicalModuleID("module.app"), Kind: ModuleKindAndroidApplication}
	otherModule := LogicalModule{ID: LogicalModuleID("module.lib"), Kind: ModuleKindAndroidLibrary}
	variant := Variant{ID: VariantID("variant.debug"), ModuleID: module.ID}

	mustAdd(t, g.AddLogicalModule(module))
	mustAdd(t, g.AddLogicalModule(otherModule))

	if err := g.AddLogicalModule(module); err == nil {
		t.Fatalf("expected duplicate logical module error")
	}

	if err := g.AddVariant(Variant{ID: VariantID("variant.missing"), ModuleID: LogicalModuleID("missing.module")}); err == nil {
		t.Fatalf("expected missing module error")
	}

	mustAdd(t, g.AddVariant(variant))

	if err := g.AddMaterialization(Materialization{
		ID:        MaterializationID("mat.missing"),
		ModuleID:  module.ID,
		VariantID: VariantID("missing.variant"),
		Kind:      MaterializationKindSourceBacked,
	}); err == nil {
		t.Fatalf("expected missing variant error")
	}

	if err := g.AddMaterialization(Materialization{
		ID:        MaterializationID("mat.mismatch"),
		ModuleID:  otherModule.ID,
		VariantID: variant.ID,
		Kind:      MaterializationKindSourceBacked,
	}); err == nil {
		t.Fatalf("expected module/variant mismatch error")
	}

	mustAdd(t, g.AddMaterialization(Materialization{
		ID:        MaterializationID("mat.debug"),
		ModuleID:  module.ID,
		VariantID: variant.ID,
		Kind:      MaterializationKindSourceBacked,
	}))

	if _, err := g.AddEdge(Edge{
		From: NodeRef{Kind: NodeKindAction, ID: "missing"},
		To:   variant.Ref(),
		Kind: EdgeKindDependsOn,
	}); err == nil {
		t.Fatalf("expected missing edge source error")
	}

	if _, err := g.AddEdge(Edge{
		From: variant.Ref(),
		To:   NodeRef{Kind: NodeKindArtifact, ID: "missing"},
		Kind: EdgeKindDependsOn,
	}); err == nil {
		t.Fatalf("expected missing edge target error")
	}

	if _, err := g.AddEdge(Edge{
		From: variant.Ref(),
		To:   NodeRef{},
		Kind: EdgeKindDependsOn,
	}); err == nil {
		t.Fatalf("expected invalid edge target error")
	}
}

func TestGraphDeterministicOrdering(t *testing.T) {
	g := New()

	mustAdd(t, g.AddLogicalModule(LogicalModule{ID: LogicalModuleID("module.z"), Kind: ModuleKindAndroidLibrary}))
	mustAdd(t, g.AddLogicalModule(LogicalModule{ID: LogicalModuleID("module.a"), Kind: ModuleKindAndroidApplication}))
	mustAdd(t, g.AddVariant(Variant{ID: VariantID("variant.z"), ModuleID: LogicalModuleID("module.z")}))
	mustAdd(t, g.AddVariant(Variant{ID: VariantID("variant.a"), ModuleID: LogicalModuleID("module.a")}))
	mustAdd(t, g.AddMaterialization(Materialization{ID: MaterializationID("mat.z"), ModuleID: LogicalModuleID("module.z"), VariantID: VariantID("variant.z"), Kind: MaterializationKindSourceBacked}))
	mustAdd(t, g.AddMaterialization(Materialization{ID: MaterializationID("mat.a"), ModuleID: LogicalModuleID("module.a"), VariantID: VariantID("variant.a"), Kind: MaterializationKindSourceBacked}))

	modules := g.LogicalModules()
	if len(modules) != 2 || modules[0].ID != "module.a" || modules[1].ID != "module.z" {
		t.Fatalf("logical modules = %#v, want sorted by id", modules)
	}

	nodes := g.Nodes()
	if len(nodes) != 6 {
		t.Fatalf("node refs = %d, want 6", len(nodes))
	}
	if nodes[0].Kind != NodeKindLogicalModule || nodes[0].ID != "module.a" {
		t.Fatalf("node refs = %#v, want logical module a first", nodes)
	}
}

func mustAdd(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
