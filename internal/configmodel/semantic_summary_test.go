package configmodel

import (
	"reflect"
	"testing"

	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

func TestGraphSummaryReturnsCopy(t *testing.T) {
	model := &Model{Summary: testSemanticGraphSummary()}

	summary := model.GraphSummary()
	mutateSemanticGraphSummary(summary)

	got := model.Summary
	want := testSemanticGraphSummary()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("graph summary should be returned as a copy: got %#v want %#v", got, want)
	}
}

func TestModuleAndVariantReturnCopies(t *testing.T) {
	model := &Model{Summary: testSemanticGraphSummary()}

	module, ok := model.Module(":app")
	if !ok {
		t.Fatal("expected module summary")
	}
	mutateSemanticModuleSummary(module)

	variant, ok := model.Variant(":app", "debug")
	if !ok {
		t.Fatal("expected variant summary")
	}
	mutateSemanticVariantSummary(variant)

	got := model.Summary
	want := testSemanticGraphSummary()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("module and variant summaries should be returned as copies: got %#v want %#v", got, want)
	}
}

func testSemanticGraphSummary() project.SemanticGraphSummary {
	minify := true
	shrink := false
	return project.SemanticGraphSummary{
		NodeCount: 2,
		EdgeCount: 1,
		Modules: []project.SemanticModuleSummary{{
			ID:                    "module.app",
			Path:                  ":app",
			ConsumerProguardFiles: []string{"consumer-rules.pro"},
			Plugins:               []string{"com.android.application"},
			Tasks:                 []string{":app:assembleDebug"},
			DependsOn:             []string{":lib"},
			DependencyClosure:     []string{":lib"},
			Variants: []project.SemanticVariantSummary{{
				ID:             "variant.debug",
				Name:           "debug",
				DeclaredName:   "debug",
				CoordinateName: "debug",
				Compatibility: project.VariantCompatibility{
					SourceSetOrder: []string{"main", "debug"},
					SourceSetNames: []string{"main", "debug"},
					TaskAliases:    []string{"assemble"},
					ModelSelectors: []string{":app"},
					SyncFragments:  []string{"sources"},
				},
				Flavors: []string{"free"},
				Coordinate: project.VariantCoordinate{
					ModulePath: ":app",
					Name:       "debug",
					BuildType:  "debug",
					Flavors:    []string{"free"},
				},
				MissingDimensions: map[string][]string{"tier": {"free"}},
				Optimization: project.VariantOptimization{
					PackageOptimizations: []project.PackageOptimization{{
						PackageName:     "com.example",
						MinifyEnabled:   &minify,
						ShrinkResources: &shrink,
					}},
				},
				ProguardFiles:         []string{"proguard-rules.pro"},
				ConsumerProguardFiles: []string{"consumer-rules.pro"},
				SourceSetOrder:        []string{"main", "debug"},
				SourceSetNames:        []string{"main", "debug"},
				TaskAliases:           []string{"assemble"},
				ModelSelectors:        []string{":app"},
				SyncFragments:         []string{"sources"},
				DependsOnVariants:     []string{":lib:debug"},
				DependencyProvenance:  []project.SemanticDependencyProvenance{{ModulePath: ":lib", VariantName: "debug"}},
				TaskProjections:       []string{":app:assembleDebug"},
				Actions:               []project.SemanticActionSummary{{ID: "action.compile", LastCacheProbe: &responsepayload.CacheProbe{State: "hit"}, Inputs: []string{"artifact.source"}, Outputs: []string{"artifact.classes"}}},
				Materialization:       testSemanticMaterializationSummary(),
			}},
		}},
	}
}

func testSemanticMaterializationSummary() project.SemanticMaterializationSummary {
	return project.SemanticMaterializationSummary{
		ID:                    "materialization.debug",
		ClasspathSnapshotIDs:  []string{"classpath-snapshot"},
		SourceRoots:           []string{"src/main"},
		ProducedArtifactIDs:   []string{"artifact.classes"},
		ProducedArtifactPaths: []string{"build/classes.jar"},
		ProducedArtifactKinds: []string{"jar"},
		ResourceArtifactIDs:   []string{"artifact.resources"},
		ResourceArtifactPaths: []string{"build/resources.ap_"},
		ManifestArtifactIDs:   []string{"artifact.manifest"},
		ManifestArtifactPaths: []string{"src/main/AndroidManifest.xml"},
		ConsumingActionIDs:    []string{"action.compile"},
		Artifacts:             []project.SemanticArtifactSummary{{ID: "artifact.classes", Path: "build/classes.jar"}},
	}
}

func mutateSemanticGraphSummary(summary project.SemanticGraphSummary) {
	mutateSemanticModuleSummary(summary.Modules[0])
}

func mutateSemanticModuleSummary(summary project.SemanticModuleSummary) {
	summary.ConsumerProguardFiles[0] = "changed.pro"
	summary.Plugins[0] = "changed.plugin"
	summary.Tasks[0] = ":app:changed"
	summary.DependsOn[0] = ":changed"
	summary.DependencyClosure[0] = ":changed"
	mutateSemanticVariantSummary(summary.Variants[0])
}

func mutateSemanticVariantSummary(summary project.SemanticVariantSummary) {
	summary.Compatibility.SourceSetOrder[0] = "changed"
	summary.Compatibility.SourceSetNames[0] = "changed"
	summary.Compatibility.TaskAliases[0] = "changed"
	summary.Compatibility.ModelSelectors[0] = "changed"
	summary.Compatibility.SyncFragments[0] = "changed"
	summary.Flavors[0] = "changed"
	summary.Coordinate.Flavors[0] = "changed"
	summary.MissingDimensions["tier"][0] = "changed"
	*summary.Optimization.PackageOptimizations[0].MinifyEnabled = false
	*summary.Optimization.PackageOptimizations[0].ShrinkResources = true
	summary.ProguardFiles[0] = "changed.pro"
	summary.ConsumerProguardFiles[0] = "changed.pro"
	summary.SourceSetOrder[0] = "changed"
	summary.SourceSetNames[0] = "changed"
	summary.TaskAliases[0] = "changed"
	summary.ModelSelectors[0] = "changed"
	summary.SyncFragments[0] = "changed"
	summary.DependsOnVariants[0] = ":changed"
	summary.DependencyProvenance[0].ModulePath = ":changed"
	summary.TaskProjections[0] = ":app:changed"
	summary.Actions[0].LastCacheProbe.State = "miss"
	summary.Actions[0].Inputs[0] = "artifact.changed"
	summary.Actions[0].Outputs[0] = "artifact.changed"
	mutateSemanticMaterializationSummary(summary.Materialization)
}

func mutateSemanticMaterializationSummary(summary project.SemanticMaterializationSummary) {
	summary.ClasspathSnapshotIDs[0] = "changed"
	summary.SourceRoots[0] = "changed"
	summary.ProducedArtifactIDs[0] = "changed"
	summary.ProducedArtifactPaths[0] = "changed"
	summary.ProducedArtifactKinds[0] = "changed"
	summary.ResourceArtifactIDs[0] = "changed"
	summary.ResourceArtifactPaths[0] = "changed"
	summary.ManifestArtifactIDs[0] = "changed"
	summary.ManifestArtifactPaths[0] = "changed"
	summary.ConsumingActionIDs[0] = "changed"
	summary.Artifacts[0].ID = "changed"
}
