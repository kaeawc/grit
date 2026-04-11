package integration

import (
	"testing"

	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

func TestModelViewQueriesPersistedGraphData(t *testing.T) {
	view, moduleID, variantID, materializationID, _, outputID, actionID := testModelView(t)

	if got := view.CacheKey(); got != "cache-key" {
		t.Fatalf("unexpected cache key: %q", got)
	}
	if got := view.GraphSummary(); got.NodeCount != 6 || got.EdgeCount != 6 {
		t.Fatalf("unexpected summary: %#v", got)
	}
	if modules := view.Modules(); len(modules) != 1 || modules[0].Path != ":app" {
		t.Fatalf("unexpected modules: %#v", modules)
	}
	if mod, ok := view.Module(":app"); !ok || mod.ID != string(moduleID) {
		t.Fatalf("unexpected module lookup: %#v %v", mod, ok)
	}
	if variants := view.Variants(":app"); len(variants) != 1 || variants[0].ID != string(variantID) {
		t.Fatalf("unexpected variants: %#v", variants)
	}
	if variant, ok := view.Variant(":app", "debug"); !ok || variant.ID != string(variantID) {
		t.Fatalf("unexpected variant lookup: %#v %v", variant, ok)
	}
	if modByID, ok := view.ModuleByID(moduleID); !ok || modByID.Module.ID != moduleID || modByID.Summary.Path != ":app" || len(modByID.Variants) != 1 {
		t.Fatalf("unexpected moduleByID lookup: %#v %v", modByID, ok)
	}
	if variantByID, ok := view.VariantByID(variantID); !ok || variantByID.Variant.ID != variantID || variantByID.Module.Path != ":app" || len(variantByID.Materializations) != 1 {
		t.Fatalf("unexpected variantByID lookup: %#v %v", variantByID, ok)
	}
	if actions := view.ActionsForModule(":app"); len(actions) != 1 || actions[0].ID != actionID {
		t.Fatalf("unexpected module actions: %#v", actions)
	}
	if actions := view.ActionsForVariant(":app", "debug"); len(actions) != 1 || actions[0].ID != actionID {
		t.Fatalf("unexpected variant actions: %#v", actions)
	}
	if artifacts := view.ArtifactsForModule(":app"); len(artifacts) != 2 {
		t.Fatalf("unexpected module artifacts: %#v", artifacts)
	}
	if artifact, ok := view.Artifact(outputID); !ok || artifact.ProducedByActionID != actionID {
		t.Fatalf("unexpected artifact lookup: %#v %v", artifact, ok)
	}
	if materialization, ok := view.Materialization(materializationID); !ok || materialization.ID != materializationID {
		t.Fatalf("unexpected materialization lookup: %#v %v", materialization, ok)
	}
	if refs := view.ClasspathSnapshotsForVariant(":app", "debug"); len(refs) != 1 || refs[0].ID != "classpath-snapshot" {
		t.Fatalf("unexpected classpath snapshot refs: %#v", refs)
	}
	if refs := view.ClasspathSnapshotsForVariant(":missing", "debug"); refs != nil {
		t.Fatalf("expected nil refs for missing variant, got %#v", refs)
	}
	if cp, ok := view.ClasspathProvenanceForVariant(":app", "debug"); !ok || cp.MaterializationID != string(materializationID) {
		t.Fatalf("unexpected classpath provenance lookup: %#v %v", cp, ok)
	}
	if cp, ok := view.ClasspathProvenanceForVariant(":missing", "debug"); ok || cp.MaterializationID != "" {
		t.Fatalf("expected missing classpath provenance, got %#v %v", cp, ok)
	}
	if inputs, ok := view.ActionInputsResult(actionID); !ok || inputs.ModulePath != ":app" || inputs.VariantName != "debug" || len(inputs.Inputs) != 1 {
		t.Fatalf("unexpected action inputs result: %#v %v", inputs, ok)
	}
	if outputs, ok := view.ActionOutputsResult(actionID); !ok || outputs.ModulePath != ":app" || outputs.VariantName != "debug" || len(outputs.Outputs) != 1 {
		t.Fatalf("unexpected action outputs result: %#v %v", outputs, ok)
	}
	if dependencies, ok := view.ActionDependenciesResult(actionID); !ok || dependencies.ModulePath != ":app" || dependencies.VariantName != "debug" {
		t.Fatalf("unexpected action dependencies result: %#v %v", dependencies, ok)
	}
	if dependents, ok := view.ActionDependentsResult(actionID); !ok || dependents.ModulePath != ":app" || dependents.VariantName != "debug" {
		t.Fatalf("unexpected action dependents result: %#v %v", dependents, ok)
	}
	if variantMaterialization, ok := view.VariantMaterialization(":app", "debug"); !ok || variantMaterialization.Materialization.MaterializationID != string(materializationID) {
		t.Fatalf("unexpected variant materialization lookup: %#v %v", variantMaterialization, ok)
	}
	if sourceSetModel, ok := view.VariantSourceSetModel(":app", "debug"); !ok || sourceSetModel.ModulePath != ":app" || sourceSetModel.VariantName != "debug" {
		t.Fatalf("unexpected variant source-set model: %#v %v", sourceSetModel, ok)
	} else if len(sourceSetModel.SourceSetOrder) == 0 || len(sourceSetModel.SourceRoots) == 0 || len(sourceSetModel.ManifestPaths) == 0 {
		t.Fatalf("expected source-set/source-root metadata, got %#v", sourceSetModel)
	}
	if dependencyBindings, ok := view.DependencyBindingsForVariant(":app", "debug"); !ok || dependencyBindings.ModulePath != ":app" || dependencyBindings.VariantName != "debug" {
		t.Fatalf("unexpected variant dependency bindings result: %#v %v", dependencyBindings, ok)
	}
	if dependencyRealizations, ok := view.DependencyRealizationsForVariant(":app", "debug"); !ok || dependencyRealizations.ModulePath != ":app" || dependencyRealizations.VariantName != "debug" || len(dependencyRealizations.Dependencies) != 1 {
		t.Fatalf("unexpected variant dependency realizations result: %#v %v", dependencyRealizations, ok)
	} else if dependencyRealizations.Dependencies[0].ModulePath != ":app" || dependencyRealizations.Dependencies[0].VariantName != "debug" || dependencyRealizations.Dependencies[0].MaterializationID == "" || dependencyRealizations.Dependencies[0].ArtifactSnapshotID == "" {
		t.Fatalf("unexpected variant dependency realization detail: %#v", dependencyRealizations.Dependencies[0])
	}
	if moduleBindings, ok := view.DependencyBindingsForModule(":app"); !ok || moduleBindings.ModulePath != ":app" || len(moduleBindings.Variants) != 1 {
		t.Fatalf("unexpected module dependency bindings result: %#v %v", moduleBindings, ok)
	}
	if moduleRealizations, ok := view.DependencyRealizationsForModule(":app"); !ok || moduleRealizations.ModulePath != ":app" || len(moduleRealizations.Variants) != 1 || len(moduleRealizations.Variants[0].Dependencies) != 1 {
		t.Fatalf("unexpected module dependency realizations result: %#v %v", moduleRealizations, ok)
	}
	if snapshot, ok := view.ArtifactSnapshotProvenance("artifact-snapshot"); !ok || snapshot.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected artifact snapshot provenance lookup: %#v %v", snapshot, ok)
	}
}

func TestModelViewProvenanceLookups(t *testing.T) {
	view, _, _, materializationID, sourceID, outputID, actionID := testModelView(t)

	actionProv, ok := view.ProvenanceForAction(actionID)
	if !ok {
		t.Fatal("expected action provenance")
	}
	if actionProv.Module.Path != ":app" || actionProv.Variant.Name != "debug" {
		t.Fatalf("unexpected action provenance module/variant: %#v", actionProv)
	}
	if actionProv.Materialization.ID != string(materializationID) {
		t.Fatalf("unexpected action provenance materialization: %#v", actionProv.Materialization)
	}
	if len(actionProv.ClasspathSnapshots) != 1 || actionProv.ClasspathSnapshots[0].ID != "classpath-snapshot" {
		t.Fatalf("unexpected action provenance classpath snapshots: %#v", actionProv.ClasspathSnapshots)
	}
	if got, want := actionProv.ClasspathSnapshots[0].Entries, []string{"/repo/app/src/main"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected action provenance classpath entries: %#v", actionProv.ClasspathSnapshots)
	}
	if actionProv.ClasspathSnapshots[0].NormalizedID == "" || actionProv.ClasspathSnapshots[0].OrderedEntriesID == "" || actionProv.ClasspathSnapshots[0].EntriesDigest == "" {
		t.Fatalf("expected derived classpath snapshot metadata: %#v", actionProv.ClasspathSnapshots[0])
	}
	if len(actionProv.Inputs) != 1 || actionProv.Inputs[0].ID != sourceID {
		t.Fatalf("unexpected action provenance inputs: %#v", actionProv.Inputs)
	}
	if len(actionProv.Outputs) != 1 || actionProv.Outputs[0].ID != outputID {
		t.Fatalf("unexpected action provenance outputs: %#v", actionProv.Outputs)
	}
	if len(actionProv.Artifacts) != 2 {
		t.Fatalf("unexpected action provenance artifacts: %#v", actionProv.Artifacts)
	}
	if actionProv.CacheProbe == nil || actionProv.CacheProbe.ActionID != actionID.String() || actionProv.CacheProbe.State != "reused" {
		t.Fatalf("unexpected action cache probe: %#v", actionProv.CacheProbe)
	}

	sourceProv, ok := view.ProvenanceForArtifact(sourceID)
	if !ok {
		t.Fatal("expected source artifact provenance")
	}
	if sourceProv.Module.Path != ":app" || sourceProv.Variant.Name != "debug" {
		t.Fatalf("unexpected source provenance module/variant: %#v", sourceProv)
	}
	if len(sourceProv.Consumers) != 1 || sourceProv.Consumers[0].ID != actionID {
		t.Fatalf("unexpected source provenance consumers: %#v", sourceProv.Consumers)
	}
	if sourceProv.Producer.ID != "" {
		t.Fatalf("expected no producer for source artifact, got %#v", sourceProv.Producer)
	}

	outputProv, ok := view.ProvenanceForArtifact(outputID)
	if !ok {
		t.Fatal("expected output artifact provenance")
	}
	if outputProv.Producer.ID != actionID {
		t.Fatalf("unexpected output provenance producer: %#v", outputProv.Producer)
	}
	if outputProv.Materialization.ID != string(materializationID) {
		t.Fatalf("unexpected output provenance materialization: %#v", outputProv.Materialization)
	}
	if len(outputProv.ClasspathSnapshots) != 1 || outputProv.ClasspathSnapshots[0].ID != "classpath-snapshot" {
		t.Fatalf("unexpected output provenance classpath snapshots: %#v", outputProv.ClasspathSnapshots)
	}
	if outputProv.CacheProbe == nil || outputProv.CacheProbe.ActionID != actionID.String() || outputProv.CacheProbe.State != "reused" {
		t.Fatalf("unexpected output cache probe: %#v", outputProv.CacheProbe)
	}
}

func TestModelViewClasspathProvenanceForVariant(t *testing.T) {
	view, _, _, materializationID, sourceID, outputID, actionID := testModelView(t)

	result, ok := view.ClasspathProvenanceForVariant(":app", "debug")
	if !ok {
		t.Fatal("expected classpath provenance")
	}
	if result.ModulePath != ":app" || result.VariantName != "debug" {
		t.Fatalf("unexpected classpath provenance coordinates: %#v", result)
	}
	if result.MaterializationID != string(materializationID) {
		t.Fatalf("unexpected materialization id: %#v", result)
	}
	if result.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected artifact snapshot id: %#v", result)
	}
	if got, want := result.SourceRoots, []string{"/repo/app/src/main"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected source roots: %#v", result.SourceRoots)
	}
	if len(result.ClasspathSnapshots) != 1 || result.ClasspathSnapshots[0].ID != "classpath-snapshot" {
		t.Fatalf("unexpected classpath snapshots: %#v", result.ClasspathSnapshots)
	}
	if got, want := result.ClasspathSnapshots[0].Entries, []string{"/repo/app/src/main"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected classpath entries: %#v", result.ClasspathSnapshots)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("unexpected classpath provenance artifacts: %#v", result.Artifacts)
	}
	artifactIDs := map[graph.ArtifactID]struct{}{}
	for _, artifact := range result.Artifacts {
		artifactIDs[artifact.ID] = struct{}{}
	}
	if _, ok := artifactIDs[sourceID]; !ok {
		t.Fatalf("expected source artifact in classpath provenance: %#v", result.Artifacts)
	}
	if _, ok := artifactIDs[outputID]; !ok {
		t.Fatalf("expected output artifact in classpath provenance: %#v", result.Artifacts)
	}
	if len(result.Actions) != 1 || result.Actions[0].ID != actionID {
		t.Fatalf("unexpected classpath provenance actions: %#v", result.Actions)
	}
}

func TestModelViewClasspathSnapshotAndLookupQueries(t *testing.T) {
	view, _, _, materializationID, sourceID, _, _ := testModelView(t)

	snapshot, ok := view.ClasspathSnapshot(":app", "debug")
	if !ok {
		t.Fatal("expected classpath snapshot query")
	}
	if snapshot.ModulePath != ":app" || snapshot.VariantName != "debug" {
		t.Fatalf("unexpected classpath snapshot coordinates: %#v", snapshot)
	}
	if snapshot.MaterializationID != string(materializationID) || snapshot.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected classpath snapshot ids: %#v", snapshot)
	}
	if snapshot.Snapshot.ID != "classpath-snapshot" || snapshot.Snapshot.NormalizedID == "" || snapshot.Snapshot.OrderedEntriesID == "" {
		t.Fatalf("unexpected classpath snapshot record: %#v", snapshot.Snapshot)
	}
	if len(snapshot.Snapshot.Entries) == 0 || len(snapshot.Snapshot.Decisions) == 0 {
		t.Fatalf("expected classpath snapshot entries and decisions, got %#v", snapshot.Snapshot)
	}
	snapshotProv, ok := view.ClasspathSnapshotProvenance("classpath-snapshot")
	if !ok {
		t.Fatal("expected classpath snapshot provenance query")
	}
	if snapshotProv.ClasspathSnapshotID != "classpath-snapshot" || len(snapshotProv.Variants) != 1 || len(snapshotProv.Artifacts) != 2 {
		t.Fatalf("unexpected classpath snapshot provenance: %#v", snapshotProv)
	}
	if len(snapshotProv.ManifestPaths) != 1 || snapshotProv.ManifestPaths[0] != "/repo/app/src/main/AndroidManifest.xml" {
		t.Fatalf("unexpected classpath snapshot provenance manifests: %#v", snapshotProv.ManifestPaths)
	}
	snapshotConsumers, ok := view.ClasspathSnapshotConsumers("classpath-snapshot")
	if !ok {
		t.Fatal("expected classpath snapshot consumers query")
	}
	if snapshotConsumers.ClasspathSnapshotID != "classpath-snapshot" || len(snapshotConsumers.Variants) != 1 || len(snapshotConsumers.Actions) != 1 || len(snapshotConsumers.Artifacts) != 2 {
		t.Fatalf("unexpected classpath snapshot consumers: %#v", snapshotConsumers)
	}
	if len(snapshotConsumers.ManifestPaths) != 1 || snapshotConsumers.ManifestPaths[0] != "/repo/app/src/main/AndroidManifest.xml" {
		t.Fatalf("unexpected classpath snapshot consumer manifests: %#v", snapshotConsumers.ManifestPaths)
	}
	snapshotByID, ok := view.ClasspathSnapshotByID(snapshot.Snapshot.NormalizedID)
	if !ok || snapshotByID.LookupID != snapshot.Snapshot.NormalizedID || snapshotByID.CanonicalID != snapshot.Snapshot.ID || snapshotByID.Result.Snapshot.ID != snapshot.Snapshot.ID {
		t.Fatalf("unexpected classpath snapshot by id: %#v %v", snapshotByID, ok)
	}
	snapshotConsumersByID, ok := view.ClasspathSnapshotConsumersByID(snapshot.Snapshot.OrderedEntriesID)
	if !ok || snapshotConsumersByID.LookupID != snapshot.Snapshot.OrderedEntriesID || snapshotConsumersByID.CanonicalID != snapshot.Snapshot.ID || snapshotConsumersByID.Consumers.ClasspathSnapshotID != snapshot.Snapshot.ID {
		t.Fatalf("unexpected classpath snapshot consumers by id: %#v %v", snapshotConsumersByID, ok)
	}

	lookup, ok := view.ClasspathEntryLookup(":app", "debug", "/repo/app/src/main")
	if !ok {
		t.Fatal("expected classpath entry lookup")
	}
	if lookup.ModulePath != ":app" || lookup.VariantName != "debug" || lookup.Path != "/repo/app/src/main" {
		t.Fatalf("unexpected classpath entry lookup coordinates: %#v", lookup)
	}
	if lookup.Entry.NormalizedPath != "/repo/app/src/main" || lookup.Entry.Path != "/repo/app/src/main" {
		t.Fatalf("unexpected classpath entry lookup entry: %#v", lookup.Entry)
	}
	if len(lookup.Decisions) == 0 {
		t.Fatalf("expected classpath entry lookup decisions, got %#v", lookup.Decisions)
	}
	if _, ok := view.ClasspathEntryLookup(":app", "debug", "/missing"); ok {
		t.Fatalf("expected missing classpath entry lookup to fail")
	}
	pathConsumers, ok := view.ClasspathPathConsumers("/repo/app/src/main")
	if !ok {
		t.Fatal("expected classpath path consumers query")
	}
	if pathConsumers.Path != "/repo/app/src/main" || len(pathConsumers.Consumers) != 1 {
		t.Fatalf("unexpected classpath path consumers: %#v", pathConsumers)
	}
	if pathConsumers.Consumers[0].ModulePath != ":app" || pathConsumers.Consumers[0].VariantName != "debug" {
		t.Fatalf("unexpected classpath path consumer coordinates: %#v", pathConsumers.Consumers[0])
	}

	onClasspath, ok := view.ArtifactOnClasspath(":app", "debug", sourceID)
	if !ok {
		t.Fatal("expected artifact-on-classpath query")
	}
	if !onClasspath.Present || onClasspath.Artifact.ID != sourceID {
		t.Fatalf("unexpected artifact-on-classpath result: %#v", onClasspath)
	}
	if onClasspath.Entry.Path != "/repo/app/src/main" {
		t.Fatalf("unexpected artifact-on-classpath entry: %#v", onClasspath.Entry)
	}
	classpathConsumers, ok := view.ArtifactClasspathConsumers(sourceID)
	if !ok {
		t.Fatal("expected artifact classpath consumers query")
	}
	if classpathConsumers.Artifact.ID != sourceID || len(classpathConsumers.Consumers) != 1 {
		t.Fatalf("unexpected artifact classpath consumers: %#v", classpathConsumers)
	}
	if classpathConsumers.Consumers[0].ModulePath != ":app" || classpathConsumers.Consumers[0].VariantName != "debug" {
		t.Fatalf("unexpected artifact classpath consumer coordinates: %#v", classpathConsumers.Consumers[0])
	}
	snapshotProvenance, ok := view.ClasspathSnapshotProvenance("classpath-snapshot")
	if !ok {
		t.Fatal("expected classpath snapshot provenance query")
	}
	if snapshotProvenance.ClasspathSnapshotID != "classpath-snapshot" || len(snapshotProvenance.Variants) != 1 {
		t.Fatalf("unexpected classpath snapshot provenance: %#v", snapshotProvenance)
	}
	if len(snapshotProvenance.Artifacts) == 0 || len(snapshotProvenance.ManifestPaths) != 1 {
		t.Fatalf("expected artifact and manifest context in classpath snapshot provenance: %#v", snapshotProvenance)
	}
}

func TestModelViewFileOwnersQuery(t *testing.T) {
	view, _, _, _, _, _, _ := testModelView(t)

	sourceOwners := view.FileOwners("/repo/app/src/main/java/com/example/App.kt")
	if sourceOwners.Path != "/repo/app/src/main/java/com/example/App.kt" || len(sourceOwners.Owners) == 0 {
		t.Fatalf("unexpected source file owners: %#v", sourceOwners)
	}
	if sourceOwners.Owners[0].ModulePath != ":app" || sourceOwners.Owners[0].VariantName != "debug" || sourceOwners.Owners[0].Kind != "source-root" {
		t.Fatalf("unexpected source owner entry: %#v", sourceOwners.Owners[0])
	}

	manifestOwners := view.FileOwners("/repo/app/src/main/AndroidManifest.xml")
	foundManifest := false
	for _, owner := range manifestOwners.Owners {
		if owner.Kind == "manifest" {
			foundManifest = true
			break
		}
	}
	if !foundManifest {
		t.Fatalf("expected manifest owner entry, got %#v", manifestOwners.Owners)
	}
}

func TestModelViewArtifactProvenanceQuery(t *testing.T) {
	view, _, _, materializationID, sourceID, outputID, actionID := testModelView(t)

	source, ok := view.ArtifactProvenance(sourceID)
	if !ok {
		t.Fatal("expected source artifact provenance query")
	}
	if source.MaterializationID != string(materializationID) || source.ModulePath != ":app" || source.VariantName != "debug" {
		t.Fatalf("unexpected source artifact provenance: %#v", source)
	}
	if source.Producer.ID != "" {
		t.Fatalf("expected no producer for source artifact, got %#v", source.Producer)
	}
	if len(source.Consumers) != 1 || source.Consumers[0].ID != actionID {
		t.Fatalf("unexpected source artifact consumers: %#v", source.Consumers)
	}
	if len(source.SiblingArtifacts) != 1 || source.SiblingArtifacts[0].ID != outputID {
		t.Fatalf("unexpected source artifact siblings: %#v", source.SiblingArtifacts)
	}
	if len(source.ClasspathSnapshots) != 1 || source.ClasspathSnapshots[0].ID != "classpath-snapshot" {
		t.Fatalf("unexpected source artifact classpath snapshots: %#v", source.ClasspathSnapshots)
	}

	output, ok := view.ArtifactProvenance(outputID)
	if !ok {
		t.Fatal("expected output artifact provenance query")
	}
	if output.MaterializationID != string(materializationID) || output.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected output artifact provenance: %#v", output)
	}
	if output.Producer.ID != actionID {
		t.Fatalf("unexpected output producer: %#v", output.Producer)
	}
	if len(output.Consumers) != 0 {
		t.Fatalf("expected no output consumers in fixture, got %#v", output.Consumers)
	}
	if len(output.SiblingArtifacts) != 1 || output.SiblingArtifacts[0].ID != sourceID {
		t.Fatalf("unexpected output artifact siblings: %#v", output.SiblingArtifacts)
	}
	if _, ok := view.ArtifactProvenance(graph.ArtifactID("missing")); ok {
		t.Fatalf("expected missing artifact provenance query to fail")
	}
}

func TestModelViewVariantMaterializationQuery(t *testing.T) {
	view, _, variantID, materializationID, sourceID, outputID, actionID := testModelView(t)

	result, ok := view.VariantMaterialization(":app", "debug")
	if !ok {
		t.Fatal("expected variant materialization query")
	}
	if result.ModulePath != ":app" || result.VariantName != "debug" || result.VariantID != string(variantID) {
		t.Fatalf("unexpected variant materialization coordinates: %#v", result)
	}
	if result.Materialization.MaterializationID != string(materializationID) || result.Materialization.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected materialization summary: %#v", result.Materialization)
	}
	if len(result.Materialization.ManifestPaths) != 1 || result.Materialization.ManifestPaths[0] != "/repo/app/src/main/AndroidManifest.xml" {
		t.Fatalf("unexpected manifest candidates: %#v", result.Materialization.ManifestPaths)
	}
	if len(result.Actions) != 1 || result.Actions[0].ID != actionID.String() || result.Actions[0].CacheKey == "" {
		t.Fatalf("unexpected action summaries: %#v", result.Actions)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("unexpected artifact summaries: %#v", result.Artifacts)
	}
	artifactIDs := map[string]struct{}{}
	for _, artifact := range result.Artifacts {
		artifactIDs[artifact.ID] = struct{}{}
	}
	if _, ok := artifactIDs[sourceID.String()]; !ok {
		t.Fatalf("expected source artifact summary in result: %#v", result.Artifacts)
	}
	if _, ok := artifactIDs[outputID.String()]; !ok {
		t.Fatalf("expected output artifact summary in result: %#v", result.Artifacts)
	}
	if _, ok := view.VariantMaterialization(":app", "missing"); ok {
		t.Fatalf("expected missing variant materialization query to fail")
	}
}

func TestModelViewVariantManifestQuery(t *testing.T) {
	view, _, variantID, materializationID, sourceID, outputID, actionID := testModelView(t)

	result, ok := view.VariantManifest(":app", "debug")
	if !ok {
		t.Fatal("expected variant manifest query")
	}
	if result.ModulePath != ":app" || result.VariantName != "debug" || result.VariantID != string(variantID) {
		t.Fatalf("unexpected variant manifest coordinates: %#v", result)
	}
	if result.MaterializationID != string(materializationID) || result.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected manifest ids: %#v", result)
	}
	if len(result.SourceRoots) != 1 || result.SourceRoots[0] != "/repo/app/src/main" {
		t.Fatalf("unexpected manifest source roots: %#v", result.SourceRoots)
	}
	if len(result.ManifestPaths) != 1 || result.ManifestPaths[0] != "/repo/app/src/main/AndroidManifest.xml" {
		t.Fatalf("unexpected manifest paths: %#v", result.ManifestPaths)
	}
	if len(result.ClasspathSnapshotIDs) != 1 || result.ClasspathSnapshotIDs[0] != "classpath-snapshot" {
		t.Fatalf("unexpected manifest classpath snapshot ids: %#v", result.ClasspathSnapshotIDs)
	}
	if len(result.ClasspathSnapshots) != 1 || result.ClasspathSnapshots[0].ID != "classpath-snapshot" {
		t.Fatalf("unexpected manifest classpath snapshots: %#v", result.ClasspathSnapshots)
	}
	if len(result.ActionIDs) != 1 || result.ActionIDs[0] != actionID.String() {
		t.Fatalf("unexpected manifest action ids: %#v", result.ActionIDs)
	}
	if len(result.Actions) != 1 || result.Actions[0].ID != actionID.String() {
		t.Fatalf("unexpected manifest actions: %#v", result.Actions)
	}
	if len(result.ProducedArtifactIDs) != 2 || result.ProducedArtifactIDs[0] == "" {
		t.Fatalf("unexpected produced artifact ids: %#v", result.ProducedArtifactIDs)
	}
	if len(result.ProducedArtifacts) != 2 {
		t.Fatalf("unexpected produced artifacts: %#v", result.ProducedArtifacts)
	}
	if summaries := view.ArtifactSummariesForVariant(":app", "debug"); len(summaries) != 2 {
		t.Fatalf("unexpected artifact summaries for variant: %#v", summaries)
	}
	artifactIDs := map[string]struct{}{}
	for _, artifact := range result.ProducedArtifacts {
		artifactIDs[artifact.ID] = struct{}{}
	}
	if _, ok := artifactIDs[sourceID.String()]; !ok {
		t.Fatalf("expected source artifact in produced artifact summaries: %#v", result.ProducedArtifacts)
	}
	if _, ok := artifactIDs[outputID.String()]; !ok {
		t.Fatalf("expected output artifact in produced artifact summaries: %#v", result.ProducedArtifacts)
	}
	if result.BackingArtifact == nil || result.BackingArtifact.ID != sourceID.String() || result.BackingArtifactID != sourceID.String() {
		t.Fatalf("unexpected backing artifact: %#v", result.BackingArtifact)
	}
	if result.Materialization.ID != string(materializationID) || len(result.Materialization.SourceRoots) != 1 {
		t.Fatalf("unexpected materialization summary in manifest result: %#v", result.Materialization)
	}
	if result.Provenance.MaterializationID != string(materializationID) || len(result.Provenance.ConsumingActionIDs) != 1 {
		t.Fatalf("unexpected provenance summary in manifest result: %#v", result.Provenance)
	}
	if _, ok := view.VariantManifest(":app", "missing"); ok {
		t.Fatalf("expected missing variant manifest query to fail")
	}
}

func TestModelViewModuleManifestQuery(t *testing.T) {
	view, _, variantID, materializationID, sourceID, outputID, actionID := testModelView(t)

	result, ok := view.ModuleManifest(":app")
	if !ok {
		t.Fatal("expected module manifest query")
	}
	if result.ModulePath != ":app" {
		t.Fatalf("unexpected module manifest coordinates: %#v", result)
	}
	if len(result.VariantNames) != 1 || result.VariantNames[0] != "debug" {
		t.Fatalf("unexpected module manifest variant names: %#v", result.VariantNames)
	}
	if len(result.MaterializationIDs) != 1 || result.MaterializationIDs[0] != string(materializationID) {
		t.Fatalf("unexpected module manifest materialization ids: %#v", result.MaterializationIDs)
	}
	if len(result.ArtifactSnapshotIDs) != 1 || result.ArtifactSnapshotIDs[0] != "artifact-snapshot" {
		t.Fatalf("unexpected module manifest artifact snapshot ids: %#v", result.ArtifactSnapshotIDs)
	}
	if len(result.ManifestPaths) != 1 || result.ManifestPaths[0] != "/repo/app/src/main/AndroidManifest.xml" {
		t.Fatalf("unexpected module manifest paths: %#v", result.ManifestPaths)
	}
	if len(result.SourceRoots) != 1 || result.SourceRoots[0] != "/repo/app/src/main" {
		t.Fatalf("unexpected module manifest source roots: %#v", result.SourceRoots)
	}
	if len(result.ProducedArtifactIDs) != 2 {
		t.Fatalf("unexpected module produced artifact ids: %#v", result.ProducedArtifactIDs)
	}
	if len(result.BackingArtifactIDs) != 1 || result.BackingArtifactIDs[0] != sourceID.String() {
		t.Fatalf("unexpected module backing artifact ids: %#v", result.BackingArtifactIDs)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("unexpected module artifacts: %#v", result.Artifacts)
	}
	if len(result.Variants) != 1 || result.Variants[0].VariantID != string(variantID) || len(result.Variants[0].ActionIDs) != 1 || result.Variants[0].ActionIDs[0] != actionID.String() {
		t.Fatalf("unexpected module variant manifest payload: %#v", result.Variants)
	}
	artifactIDs := map[string]struct{}{}
	for _, artifact := range result.Artifacts {
		artifactIDs[artifact.ID] = struct{}{}
	}
	if _, ok := artifactIDs[sourceID.String()]; !ok {
		t.Fatalf("expected source artifact in module manifest artifacts: %#v", result.Artifacts)
	}
	if _, ok := artifactIDs[outputID.String()]; !ok {
		t.Fatalf("expected output artifact in module manifest artifacts: %#v", result.Artifacts)
	}
	if _, ok := view.ModuleManifest(":missing"); ok {
		t.Fatalf("expected missing module manifest query to fail")
	}
}

func TestModelViewArtifactSnapshotProvenanceQuery(t *testing.T) {
	view, _, _, materializationID, sourceID, outputID, actionID := testModelView(t)

	result, ok := view.ArtifactSnapshotProvenance("artifact-snapshot")
	if !ok {
		t.Fatal("expected artifact snapshot provenance query")
	}
	if result.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected artifact snapshot id: %#v", result)
	}
	if len(result.Variants) != 1 || result.Variants[0].MaterializationID != string(materializationID) {
		t.Fatalf("unexpected snapshot variants: %#v", result.Variants)
	}
	if len(result.Variants[0].ManifestPaths) != 1 || result.Variants[0].ManifestPaths[0] != "/repo/app/src/main/AndroidManifest.xml" {
		t.Fatalf("unexpected snapshot manifest paths: %#v", result.Variants[0].ManifestPaths)
	}
	if len(result.ManifestPaths) != 1 || result.ManifestPaths[0] != "/repo/app/src/main/AndroidManifest.xml" {
		t.Fatalf("unexpected aggregated manifest paths: %#v", result.ManifestPaths)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("unexpected snapshot artifacts: %#v", result.Artifacts)
	}
	artifactIDs := map[string]struct{}{}
	for _, artifact := range result.Artifacts {
		artifactIDs[artifact.ID] = struct{}{}
	}
	if _, ok := artifactIDs[sourceID.String()]; !ok {
		t.Fatalf("expected source artifact summary in snapshot result: %#v", result.Artifacts)
	}
	if _, ok := artifactIDs[outputID.String()]; !ok {
		t.Fatalf("expected output artifact summary in snapshot result: %#v", result.Artifacts)
	}
	if len(result.Variants[0].ConsumingActionIDs) != 1 || result.Variants[0].ConsumingActionIDs[0] != actionID.String() {
		t.Fatalf("unexpected consuming action ids: %#v", result.Variants[0].ConsumingActionIDs)
	}
	if _, ok := view.ArtifactSnapshotProvenance("missing-snapshot"); ok {
		t.Fatalf("expected missing artifact snapshot provenance query to fail")
	}
	consumers, ok := view.ArtifactSnapshotConsumers("artifact-snapshot")
	if !ok {
		t.Fatal("expected artifact snapshot consumers query")
	}
	if consumers.ArtifactSnapshotID != "artifact-snapshot" || len(consumers.Variants) != 1 || len(consumers.Actions) != 1 || len(consumers.Artifacts) != 2 {
		t.Fatalf("unexpected artifact snapshot consumers: %#v", consumers)
	}
	if len(consumers.ManifestPaths) != 1 || consumers.ManifestPaths[0] != "/repo/app/src/main/AndroidManifest.xml" {
		t.Fatalf("unexpected artifact snapshot consumer manifest paths: %#v", consumers.ManifestPaths)
	}
}

func TestModelViewMaterializationProvenanceQuery(t *testing.T) {
	view, _, _, materializationID, _, _, actionID := testModelView(t)

	result, ok := view.MaterializationProvenance(materializationID)
	if !ok {
		t.Fatal("expected materialization provenance query")
	}
	if result.Materialization.ID != materializationID || result.ModulePath != ":app" || result.VariantName != "debug" {
		t.Fatalf("unexpected materialization provenance coordinates: %#v", result)
	}
	if result.ArtifactSnapshotID != "artifact-snapshot" || result.Provenance.MaterializationID != string(materializationID) {
		t.Fatalf("unexpected materialization provenance ids: %#v", result)
	}
	if len(result.Actions) != 1 || result.Actions[0].ID != actionID.String() || len(result.Artifacts) != 2 {
		t.Fatalf("unexpected materialization provenance payload: %#v", result)
	}
	if _, ok := view.MaterializationProvenance(graph.MaterializationID("missing")); ok {
		t.Fatalf("expected missing materialization provenance query to fail")
	}
}

func TestModelViewStableIDQueries(t *testing.T) {
	view, _, variantID, materializationID, sourceID, outputID, actionID := testModelView(t)

	actionByID, ok := view.ActionByID(actionID)
	if !ok {
		t.Fatal("expected actionByID query")
	}
	if actionByID.Action.ID != actionID || actionByID.ModulePath != ":app" || actionByID.VariantName != "debug" {
		t.Fatalf("unexpected actionByID coordinates: %#v", actionByID)
	}
	if actionByID.Summary.ID != actionID.String() || len(actionByID.Inputs) != 1 || len(actionByID.Outputs) != 1 {
		t.Fatalf("unexpected actionByID payload: %#v", actionByID)
	}
	if len(actionByID.Dependencies) != 0 || len(actionByID.Dependents) != 0 {
		t.Fatalf("unexpected actionByID graph neighbors: %#v", actionByID)
	}

	artifactByID, ok := view.ArtifactByID(sourceID)
	if !ok {
		t.Fatal("expected artifactByID query")
	}
	if artifactByID.Artifact.ID != sourceID || artifactByID.ModulePath != ":app" || artifactByID.VariantName != "debug" {
		t.Fatalf("unexpected artifactByID coordinates: %#v", artifactByID)
	}
	if artifactByID.MaterializationID != string(materializationID) || artifactByID.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected artifactByID ids: %#v", artifactByID)
	}
	if artifactByID.Summary.ID != sourceID.String() || len(artifactByID.Consumers) != 1 || len(artifactByID.SiblingArtifacts) != 1 || artifactByID.SiblingArtifacts[0].ID != outputID {
		t.Fatalf("unexpected artifactByID payload: %#v", artifactByID)
	}

	materializationByID, ok := view.MaterializationByID(materializationID)
	if !ok {
		t.Fatal("expected materializationByID query")
	}
	if materializationByID.Materialization.ID != materializationID || materializationByID.ModulePath != ":app" || materializationByID.VariantName != "debug" {
		t.Fatalf("unexpected materializationByID coordinates: %#v", materializationByID)
	}
	if materializationByID.ArtifactSnapshotID != "artifact-snapshot" || len(materializationByID.ClasspathSnapshots) != 1 {
		t.Fatalf("unexpected materializationByID ids: %#v", materializationByID)
	}
	if len(materializationByID.Artifacts) != 2 || len(materializationByID.Actions) != 1 || materializationByID.Actions[0].ID != actionID.String() {
		t.Fatalf("unexpected materializationByID payload: %#v", materializationByID)
	}

	materializationConsumers, ok := view.MaterializationConsumers(materializationID)
	if !ok {
		t.Fatal("expected materializationConsumers query")
	}
	if materializationConsumers.MaterializationID != string(materializationID) || materializationConsumers.ModulePath != ":app" || materializationConsumers.VariantName != "debug" {
		t.Fatalf("unexpected materializationConsumers coordinates: %#v", materializationConsumers)
	}
	if materializationConsumers.ArtifactSnapshotID != "artifact-snapshot" || len(materializationConsumers.ManifestPaths) != 1 {
		t.Fatalf("unexpected materializationConsumers ids: %#v", materializationConsumers)
	}
	if len(materializationConsumers.Actions) != 1 || materializationConsumers.Actions[0].ID != actionID.String() || len(materializationConsumers.Artifacts) != 2 {
		t.Fatalf("unexpected materializationConsumers payload: %#v", materializationConsumers)
	}

	if _, ok := view.ActionByID(graph.ActionID("missing")); ok {
		t.Fatalf("expected missing actionByID query to fail")
	}
	if _, ok := view.ArtifactByID(graph.ArtifactID("missing")); ok {
		t.Fatalf("expected missing artifactByID query to fail")
	}
	if _, ok := view.MaterializationByID(graph.MaterializationID("missing")); ok {
		t.Fatalf("expected missing materializationByID query to fail")
	}
	if _, ok := view.MaterializationConsumers(graph.MaterializationID("missing")); ok {
		t.Fatalf("expected missing materializationConsumers query to fail")
	}
	if variantByID, ok := view.VariantByID(variantID); !ok || variantByID.Variant.ID != variantID {
		t.Fatalf("expected variantByID to remain available after stable-id additions: %#v %v", variantByID, ok)
	}
}

func TestModelViewPlannedActionPolicyQueries(t *testing.T) {
	view, _, _, _, _, _, actionID := testModelView(t)

	policy, ok := view.PlannedActionPolicy(actionID)
	if !ok {
		t.Fatal("expected plannedActionPolicy query")
	}
	if policy.Action.ID != actionID || policy.ModulePath != ":app" || policy.VariantName != "debug" {
		t.Fatalf("unexpected plannedActionPolicy coordinates: %#v", policy)
	}
	if policy.Policy.ID != actionID.String() || policy.Policy.WorkerClass != "kotlin-compile" || policy.Policy.ResourceClass != "jvm-process" {
		t.Fatalf("unexpected plannedActionPolicy policy payload: %#v", policy)
	}
	if policy.Policy.ResourceCost != 1 || policy.Policy.MaxParallelism != 4 || policy.Policy.RetentionClass == "" || policy.Policy.Shareability == "" {
		t.Fatalf("expected bounded policy metadata on plannedActionPolicy: %#v", policy)
	}

	policies, ok := view.PlannedActionPolicies(":app")
	if !ok {
		t.Fatal("expected plannedActionPolicies query")
	}
	if policies.ModulePath != ":app" || len(policies.VariantNames) != 1 || policies.VariantNames[0] != "debug" {
		t.Fatalf("unexpected plannedActionPolicies coordinates: %#v", policies)
	}
	if len(policies.Policies) != 1 || policies.Policies[0].ID != actionID.String() {
		t.Fatalf("unexpected plannedActionPolicies aggregate payload: %#v", policies)
	}
	if len(policies.Variants) != 1 || policies.Variants[0].VariantName != "debug" || len(policies.Variants[0].Policies) != 1 {
		t.Fatalf("unexpected plannedActionPolicies variant payload: %#v", policies)
	}
	if _, ok := view.PlannedActionPolicy(graph.ActionID("missing")); ok {
		t.Fatalf("expected missing plannedActionPolicy query to fail")
	}
	if _, ok := view.PlannedActionPolicies(":missing"); ok {
		t.Fatalf("expected missing plannedActionPolicies query to fail")
	}
}

func TestModelViewDependencyRealizationQueries(t *testing.T) {
	view, _, variantID, materializationID, sourceID, outputID, _ := testModelView(t)

	result, ok := view.DependencyRealizationsForVariant(":app", "debug")
	if !ok {
		t.Fatal("expected dependencyRealizationsForVariant query")
	}
	if result.ModulePath != ":app" || result.VariantName != "debug" || result.VariantID != string(variantID) {
		t.Fatalf("unexpected dependencyRealizationsForVariant coordinates: %#v", result)
	}
	if len(result.Dependencies) != 1 {
		t.Fatalf("unexpected dependencyRealizationsForVariant payload: %#v", result)
	}
	dep := result.Dependencies[0]
	if dep.ModulePath != ":app" || dep.VariantName != "debug" || dep.DependencyLevel != "variant" {
		t.Fatalf("unexpected dependency realization coordinates: %#v", dep)
	}
	if dep.ModuleID == "" || dep.VariantID != string(variantID) || dep.MaterializationID != string(materializationID) {
		t.Fatalf("expected realized dependency identity data, got %#v", dep)
	}
	if dep.ArtifactSnapshotID != "artifact-snapshot" || len(dep.ClasspathSnapshotIDs) != 1 || len(dep.SourceRoots) != 1 {
		t.Fatalf("expected realized dependency snapshot/source data, got %#v", dep)
	}
	if dep.BackingArtifactID != sourceID.String() || len(dep.ProducedArtifactIDs) != 2 {
		t.Fatalf("expected realized dependency artifact data, got %#v", dep)
	}
	if dep.SelectionReason != "semantic dependency provenance" || len(dep.SelectionReasons) != 1 {
		t.Fatalf("expected bounded selection-reason metadata, got %#v", dep)
	}
	if dep.BackingArtifactPath == "" || dep.BackingArtifactKind == "" || dep.BackingArtifact == nil {
		t.Fatalf("expected backing artifact detail, got %#v", dep)
	}
	if len(dep.ManifestPaths) == 0 || len(dep.ProducedArtifactPaths) == 0 || len(dep.ProducedArtifactKinds) == 0 || len(dep.ProducedArtifacts) != 2 {
		t.Fatalf("expected manifest and produced artifact detail, got %#v", dep)
	}
	produced := map[string]struct{}{}
	for _, id := range dep.ProducedArtifactIDs {
		produced[id] = struct{}{}
	}
	if _, ok := produced[sourceID.String()]; !ok {
		t.Fatalf("expected source artifact in dependency realization: %#v", dep.ProducedArtifactIDs)
	}
	if _, ok := produced[outputID.String()]; !ok {
		t.Fatalf("expected output artifact in dependency realization: %#v", dep.ProducedArtifactIDs)
	}
	if dep.BackingArtifact.ID != sourceID.String() {
		t.Fatalf("expected backing artifact summary to point at source artifact, got %#v", dep.BackingArtifact)
	}
	if dep.ProducedArtifacts[0].ID == "" || dep.ProducedArtifacts[0].Path == "" || dep.ProducedArtifacts[0].Kind == "" {
		t.Fatalf("expected produced artifact summary detail, got %#v", dep.ProducedArtifacts)
	}

	moduleResult, ok := view.DependencyRealizationsForModule(":app")
	if !ok {
		t.Fatal("expected dependencyRealizationsForModule query")
	}
	if moduleResult.ModulePath != ":app" || len(moduleResult.Variants) != 1 || moduleResult.Variants[0].VariantName != "debug" {
		t.Fatalf("unexpected dependencyRealizationsForModule payload: %#v", moduleResult)
	}
	if _, ok := view.DependencyRealizationsForVariant(":app", "missing"); ok {
		t.Fatalf("expected missing dependencyRealizationsForVariant query to fail")
	}
	if _, ok := view.DependencyRealizationsForModule(":missing"); ok {
		t.Fatalf("expected missing dependencyRealizationsForModule query to fail")
	}
}

func TestModelViewArtifactConsumersQuery(t *testing.T) {
	view, _, _, materializationID, sourceID, outputID, actionID := testModelView(t)

	result, ok := view.ArtifactConsumers(sourceID)
	if !ok {
		t.Fatal("expected artifact consumers query")
	}
	if result.Artifact.ID != sourceID || result.ModulePath != ":app" || result.VariantName != "debug" {
		t.Fatalf("unexpected artifact consumers coordinates: %#v", result)
	}
	if result.MaterializationID != string(materializationID) || result.ArtifactSnapshotID != "artifact-snapshot" {
		t.Fatalf("unexpected artifact consumers ids: %#v", result)
	}
	if result.Producer.ID != "" {
		t.Fatalf("expected no producer for source artifact, got %#v", result.Producer)
	}
	if len(result.Consumers) != 1 || result.Consumers[0].ID != actionID {
		t.Fatalf("unexpected artifact consumers payload: %#v", result.Consumers)
	}
	if len(result.SiblingArtifacts) != 1 || result.SiblingArtifacts[0].ID != outputID {
		t.Fatalf("unexpected artifact consumer siblings: %#v", result.SiblingArtifacts)
	}
	if len(result.ClasspathSnapshots) != 1 || result.ClasspathSnapshots[0].ID != "classpath-snapshot" {
		t.Fatalf("unexpected artifact consumer classpath snapshots: %#v", result.ClasspathSnapshots)
	}
	if _, ok := view.ArtifactConsumers(graph.ArtifactID("missing")); ok {
		t.Fatalf("expected missing artifact consumers query to fail")
	}
}

func testModelView(t *testing.T) (*ModelView, graph.LogicalModuleID, graph.VariantID, graph.MaterializationID, graph.ArtifactID, graph.ArtifactID, graph.ActionID) {
	t.Helper()

	moduleID := graph.LogicalModuleID("sample:app")
	variantID := graph.VariantID("variant:debug")
	materializationID := graph.MaterializationID("materialization:debug")
	sourceID := graph.ArtifactID("artifact:source")
	outputID := graph.ArtifactID("artifact:classes")
	actionID := graph.ActionID("action:compile")

	g := graph.New()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	must(g.AddLogicalModule(graph.LogicalModule{
		ID:   moduleID,
		Name: "app",
		Path: ":app",
		Dir:  "/repo/app",
		Kind: graph.ModuleKindAndroidApplication,
	}))
	must(g.AddVariant(graph.Variant{
		ID:        variantID,
		ModuleID:  moduleID,
		Name:      "debug",
		BuildType: "debug",
	}))
	must(g.AddMaterialization(graph.Materialization{
		ID:                   materializationID,
		ModuleID:             moduleID,
		VariantID:            variantID,
		Kind:                 graph.MaterializationKindSourceBacked,
		BackingArtifactID:    sourceID,
		SourceRoots:          []string{"/repo/app/src/main"},
		ArtifactSnapshotID:   "artifact-snapshot",
		ClasspathSnapshotIDs: []string{"classpath-snapshot"},
	}))
	must(g.AddArtifact(graph.Artifact{
		ID:                sourceID,
		MaterializationID: materializationID,
		Kind:              graph.ArtifactKindDirectory,
		Path:              "/repo/app/src/main",
	}))
	must(g.AddArtifact(graph.Artifact{
		ID:                 outputID,
		MaterializationID:  materializationID,
		ProducedByActionID: actionID,
		Kind:               graph.ArtifactKindJar,
		Path:               "/repo/app/build/classes.jar",
	}))
	must(g.AddAction(graph.Action{
		ID:        actionID,
		ModuleID:  moduleID,
		VariantID: variantID,
		Name:      "compile",
		Kind:      graph.ActionKindCompile,
		Inputs:    []graph.ArtifactID{sourceID},
		Outputs:   []graph.ArtifactID{outputID},
		Attributes: map[string]string{
			"materialization": materializationID.String(),
			"operation":       "compile",
		},
	}))
	_, err := g.AddEdge(graph.Edge{From: moduleID.Ref(), To: variantID.Ref(), Kind: graph.EdgeKindContains})
	must(err)
	_, err = g.AddEdge(graph.Edge{From: variantID.Ref(), To: materializationID.Ref(), Kind: graph.EdgeKindRealizes})
	must(err)
	_, err = g.AddEdge(graph.Edge{From: materializationID.Ref(), To: sourceID.Ref(), Kind: graph.EdgeKindBacks})
	must(err)
	_, err = g.AddEdge(graph.Edge{From: variantID.Ref(), To: actionID.Ref(), Kind: graph.EdgeKindContains})
	must(err)
	_, err = g.AddEdge(graph.Edge{From: actionID.Ref(), To: sourceID.Ref(), Kind: graph.EdgeKindConsumes})
	must(err)
	_, err = g.AddEdge(graph.Edge{From: actionID.Ref(), To: outputID.Ref(), Kind: graph.EdgeKindProduces})
	must(err)

	summary := project.SemanticGraphSummary{
		NodeCount: g.NodeCount(),
		EdgeCount: g.EdgeCount(),
		Modules: []project.SemanticModuleSummary{
			{
				ID:   string(moduleID),
				Name: "app",
				Path: ":app",
				Dir:  "/repo/app",
				Kind: "android_application",
				Variants: []project.SemanticVariantSummary{
					{
						ID:             string(variantID),
						Name:           "debug",
						CoordinateName: "debug",
						DisplayName:    "Debug",
						BuildType:      "debug",
						SourceSetOrder: []string{"main", "debug"},
						SourceSetNames: []string{"main", "debug"},
						DependencyProvenance: []project.SemanticDependencyProvenance{
							{
								ModulePath:      ":app",
								VariantName:     "debug",
								DependencyLevel: "variant",
							},
						},
						Actions: []project.SemanticActionSummary{
							{
								ID:            string(actionID),
								Name:          "compile",
								Operation:     "compile",
								WorkerClass:   "kotlin-compile",
								ResourceClass: "jvm-process",
								CacheKey:      "cache-key",
								LastCacheProbe: &responsepayload.CacheProbe{
									ActionID: string(actionID),
									State:    "reused",
									Basis:    "shared-cache-hit",
									Detail:   "restored compiled classes from shared cache",
								},
								Inputs:  []string{string(sourceID)},
								Outputs: []string{string(outputID)},
							},
						},
						Materialization: project.SemanticMaterializationSummary{
							ID:                   string(materializationID),
							Mode:                 "source-backed",
							ArtifactSnapshotID:   "artifact-snapshot",
							ClasspathSnapshotIDs: []string{"classpath-snapshot"},
							SourceRoots:          []string{"/repo/app/src/main"},
						},
					},
				},
			},
		},
	}
	model := &configmodel.Model{
		CacheKeyValue: "cache-key",
		Summary:       summary,
		ActionSummaries: []configmodel.ActionSummary{
			{
				ID:             string(actionID),
				ModuleID:       string(moduleID),
				ModulePath:     ":app",
				VariantID:      string(variantID),
				VariantName:    "debug",
				Name:           "compile",
				Kind:           string(graph.ActionKindCompile),
				Operation:      "compile",
				Inputs:         []string{string(sourceID)},
				Outputs:        []string{string(outputID)},
				CacheKey:       "cache-key",
				WorkerClass:    "kotlin-compile",
				ResourceClass:  "jvm-process",
				ResourceCost:   1,
				MaxParallelism: 4,
				RetentionClass: "machine-shareable",
				Shareability:   "machine",
			},
		},
		ArtifactSummaries: []configmodel.ArtifactSummary{
			{
				ID:                string(sourceID),
				ModuleID:          string(moduleID),
				ModulePath:        ":app",
				VariantID:         string(variantID),
				VariantName:       "debug",
				MaterializationID: string(materializationID),
				Kind:              string(graph.ArtifactKindDirectory),
				Path:              "/repo/app/src/main",
			},
			{
				ID:                 string(outputID),
				ModuleID:           string(moduleID),
				ModulePath:         ":app",
				VariantID:          string(variantID),
				VariantName:        "debug",
				MaterializationID:  string(materializationID),
				ProducedByActionID: string(actionID),
				Kind:               string(graph.ArtifactKindJar),
				Path:               "/repo/app/build/classes.jar",
			},
		},
		ProvenanceSummaries: []configmodel.ProvenanceSummary{
			{
				MaterializationID:    string(materializationID),
				ModuleID:             string(moduleID),
				ModulePath:           ":app",
				VariantID:            string(variantID),
				VariantName:          "debug",
				Mode:                 string(graph.MaterializationKindSourceBacked),
				ArtifactSnapshotID:   "artifact-snapshot",
				ClasspathSnapshotIDs: []string{"classpath-snapshot"},
				SourceRoots:          []string{"/repo/app/src/main"},
				ManifestPaths:        []string{"/repo/app/src/main/AndroidManifest.xml"},
				BackingArtifactID:    string(sourceID),
				ProducedArtifactIDs:  []string{string(sourceID), string(outputID)},
				ConsumingActionIDs:   []string{string(actionID)},
			},
		},
		Snapshot: g.Snapshot(),
	}
	return NewModelView(model), moduleID, variantID, materializationID, sourceID, outputID, actionID
}
