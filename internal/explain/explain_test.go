package explain

import (
	"testing"

	"github.com/kaeawc/grit/internal/graph"
)

func TestInferTiming(t *testing.T) {
	t.Run("cacheable reused step", func(t *testing.T) {
		got := InferTiming("compileKotlin", 0, nil)
		if got == nil {
			t.Fatalf("expected explanation")
		}
		if got.State != StateReused {
			t.Fatalf("state = %q, want %q", got.State, StateReused)
		}
		if got.Basis != "perf-duration" {
			t.Fatalf("basis = %q, want perf-duration", got.Basis)
		}
	})

	t.Run("cacheable rebuilt step", func(t *testing.T) {
		got := InferTiming("jarClasses", 12, nil)
		if got == nil {
			t.Fatalf("expected explanation")
		}
		if got.State != StateRebuilt {
			t.Fatalf("state = %q, want %q", got.State, StateRebuilt)
		}
	})

	t.Run("non cacheable step", func(t *testing.T) {
		if got := InferTiming("loadProject", 0, nil); got != nil {
			t.Fatalf("expected no explanation, got %#v", got)
		}
	})
}

func TestForGraphActionUsesGraphInputsAndVariantDependencies(t *testing.T) {
	g := graph.New()
	moduleID := graph.LogicalModuleID("module:app")
	variantID := graph.VariantID("variant:app:debug")
	materializationID := graph.MaterializationID("materialization:app:debug")
	artifactID := graph.ArtifactID("artifact:lib:debug")
	actionID := graph.ActionID("action:assemble:app:debug")

	if err := g.AddLogicalModule(graph.LogicalModule{ID: moduleID, Kind: graph.ModuleKindAndroidApplication, Path: ":app"}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddVariant(graph.Variant{ID: variantID, ModuleID: moduleID, Name: "debug", BuildType: "debug"}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddMaterialization(graph.Materialization{ID: materializationID, ModuleID: moduleID, VariantID: variantID, Kind: graph.MaterializationKindSourceBacked}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddArtifact(graph.Artifact{ID: artifactID, MaterializationID: materializationID, Kind: graph.ArtifactKindDirectory, Path: "src/debug"}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddAction(graph.Action{
		ID:        actionID,
		ModuleID:  moduleID,
		VariantID: variantID,
		Name:      "assembleDebug",
		Kind:      graph.ActionKindPackage,
		Attributes: map[string]string{
			"operation": "assemble",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.SetActionInputs(actionID, []graph.ArtifactID{artifactID}); err != nil {
		t.Fatal(err)
	}
	action, ok := g.Action(actionID)
	if !ok {
		t.Fatal("expected action")
	}
	expl := ForGraphAction(g, action)
	if len(expl.InputArtifacts) != 1 {
		t.Fatalf("expected one input artifact, got %#v", expl)
	}
	if len(expl.VariantDependencies) != 1 || expl.VariantDependencies[0].ID != variantID.String() {
		t.Fatalf("expected variant dependency, got %#v", expl.VariantDependencies)
	}
	if len(expl.MaterializationDependencies) != 1 || expl.MaterializationDependencies[0].ID != materializationID.String() {
		t.Fatalf("expected materialization dependency, got %#v", expl.MaterializationDependencies)
	}
}
