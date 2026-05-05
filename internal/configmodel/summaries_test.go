package configmodel

import (
	"reflect"
	"testing"

	"github.com/kaeawc/grit/internal/graph"
)

func TestActionSummaryLookupsReturnCopies(t *testing.T) {
	model := &Model{
		ActionSummaries: []ActionSummary{{
			ID:          "action.compile",
			ModulePath:  ":app",
			VariantName: "debug",
			Inputs:      []string{"artifact.source"},
			Outputs:     []string{"artifact.classes"},
		}},
	}

	summary, ok := model.ActionSummary(graph.ActionID("action.compile"))
	if !ok {
		t.Fatal("expected action summary")
	}
	summary.Inputs[0] = "artifact.changed"
	summary.Outputs[0] = "artifact.changed"

	moduleSummaries := model.ActionSummariesForModule(":app")
	moduleSummaries[0].Inputs[0] = "artifact.changed"
	moduleSummaries[0].Outputs[0] = "artifact.changed"

	variantSummaries := model.ActionSummariesForVariant(":app", "debug")
	variantSummaries[0].Inputs[0] = "artifact.changed"
	variantSummaries[0].Outputs[0] = "artifact.changed"

	idSummaries := model.ActionSummariesByIDs([]string{"action.compile"})
	idSummaries[0].Inputs[0] = "artifact.changed"
	idSummaries[0].Outputs[0] = "artifact.changed"

	got, ok := model.ActionSummary(graph.ActionID("action.compile"))
	if !ok {
		t.Fatal("expected action summary after mutations")
	}
	want := ActionSummary{
		ID:          "action.compile",
		ModulePath:  ":app",
		VariantName: "debug",
		Inputs:      []string{"artifact.source"},
		Outputs:     []string{"artifact.classes"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("action summary lookup should return copies: got %#v want %#v", got, want)
	}
}

func TestProvenanceSummaryLookupsReturnCopies(t *testing.T) {
	model := &Model{
		ArtifactSummaries: []ArtifactSummary{{
			ID:                "artifact.classes",
			MaterializationID: "materialization.debug",
		}},
		ActionSummaries: []ActionSummary{{
			ID:      "action.compile",
			Inputs:  []string{"artifact.source"},
			Outputs: []string{"artifact.classes"},
		}},
		ProvenanceSummaries: []ProvenanceSummary{{
			MaterializationID:    "materialization.debug",
			ModulePath:           ":app",
			VariantName:          "debug",
			ArtifactSnapshotID:   "artifact-snapshot",
			ClasspathSnapshotIDs: []string{"classpath-snapshot"},
			SourceRoots:          []string{"src/main"},
			ManifestPaths:        []string{"src/main/AndroidManifest.xml"},
			ProducedArtifactIDs:  []string{"artifact.classes"},
			ConsumingActionIDs:   []string{"action.compile"},
		}},
	}

	summary, ok := model.ProvenanceSummaryByMaterialization(graph.MaterializationID("materialization.debug"))
	if !ok {
		t.Fatal("expected provenance summary")
	}
	mutateProvenanceSummary(summary)

	artifactSummary, ok := model.ProvenanceSummaryByArtifact(graph.ArtifactID("artifact.classes"))
	if !ok {
		t.Fatal("expected artifact provenance summary")
	}
	mutateProvenanceSummary(artifactSummary)

	moduleSummaries := model.ProvenanceSummariesForModule(":app")
	mutateProvenanceSummary(moduleSummaries[0])

	variantSummary, ok := model.ProvenanceSummaryForVariant(":app", "debug")
	if !ok {
		t.Fatal("expected variant provenance summary")
	}
	mutateProvenanceSummary(variantSummary)

	artifactSnapshotSummaries := model.ProvenanceSummariesByArtifactSnapshot("artifact-snapshot")
	mutateProvenanceSummary(artifactSnapshotSummaries[0])

	classpathSnapshotSummaries := model.ProvenanceSummariesByClasspathSnapshot("classpath-snapshot")
	mutateProvenanceSummary(classpathSnapshotSummaries[0])

	actionSummaries := model.ActionSummariesByClasspathSnapshot("classpath-snapshot")
	actionSummaries[0].Inputs[0] = "artifact.changed"
	actionSummaries[0].Outputs[0] = "artifact.changed"

	action, ok := model.ActionSummary(graph.ActionID("action.compile"))
	if !ok {
		t.Fatal("expected action summary after classpath snapshot lookup mutation")
	}
	wantAction := ActionSummary{
		ID:      "action.compile",
		Inputs:  []string{"artifact.source"},
		Outputs: []string{"artifact.classes"},
	}
	if !reflect.DeepEqual(action, wantAction) {
		t.Fatalf("classpath snapshot action lookup should return copies: got %#v want %#v", action, wantAction)
	}

	got, ok := model.ProvenanceSummaryByMaterialization(graph.MaterializationID("materialization.debug"))
	if !ok {
		t.Fatal("expected provenance summary after mutations")
	}
	want := ProvenanceSummary{
		MaterializationID:    "materialization.debug",
		ModulePath:           ":app",
		VariantName:          "debug",
		ArtifactSnapshotID:   "artifact-snapshot",
		ClasspathSnapshotIDs: []string{"classpath-snapshot"},
		SourceRoots:          []string{"src/main"},
		ManifestPaths:        []string{"src/main/AndroidManifest.xml"},
		ProducedArtifactIDs:  []string{"artifact.classes"},
		ConsumingActionIDs:   []string{"action.compile"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provenance summary lookup should return copies: got %#v want %#v", got, want)
	}
}

func mutateProvenanceSummary(summary ProvenanceSummary) {
	summary.ClasspathSnapshotIDs[0] = "classpath-changed"
	summary.SourceRoots[0] = "src/changed"
	summary.ManifestPaths[0] = "src/changed/AndroidManifest.xml"
	summary.ProducedArtifactIDs[0] = "artifact.changed"
	summary.ConsumingActionIDs[0] = "action.changed"
}
