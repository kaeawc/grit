package intellijsync

import (
	"reflect"
	"testing"
)

func TestModelModuleReturnsCopy(t *testing.T) {
	model := testSyncModel()

	mod, ok := model.Module(":app")
	if !ok {
		t.Fatal("expected module")
	}
	mutateSyncModule(mod)

	got := model.Modules
	want := testSyncModel().Modules
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("module lookup should return a copy: got %#v want %#v", got, want)
	}
}

func TestModuleVariantReturnsCopy(t *testing.T) {
	model := testSyncModel()
	mod, ok := model.Module(":app")
	if !ok {
		t.Fatal("expected module")
	}

	variant, ok := mod.Variant("debug")
	if !ok {
		t.Fatal("expected variant")
	}
	mutateSyncVariant(variant)

	got := model.Modules
	want := testSyncModel().Modules
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("variant lookup should return a copy: got %#v want %#v", got, want)
	}
}

func testSyncModel() *Model {
	return &Model{
		Modules: []Module{{
			Path:                   ":app",
			Identity:               testIdentity(),
			KotlinFreeCompilerArgs: []string{"-Xcontext-receivers"},
			LintDisabledChecks:     []string{"ObsoleteSdkInt"},
			ConsumerProguardFiles:  []string{"consumer-rules.pro"},
			DefaultTasks:           []string{":app:assembleDebug"},
			Tasks:                  []Task{{Name: "assembleDebug"}},
			TaskCatalog:            []TaskCatalog{{RawName: ":app:assembleDebug"}},
			Dependencies:           []Dependency{{TargetModulePath: ":lib"}},
			Variants:               []Variant{testSyncVariant()},
		}},
	}
}

func testSyncVariant() Variant {
	return Variant{
		ID:            "variant.debug",
		Name:          "debug",
		Identity:      testIdentity(),
		Compatibility: testCompatibility(),
		Flavors:       []string{"free"},
		ProguardFiles: []string{"proguard-rules.pro"},
		ConsumerProguardFiles: []string{
			"consumer-rules.pro",
		},
		SourceSetOrder: []string{"main", "debug"},
		SourceSetNames: []string{"main", "debug"},
		TaskAliases:    []string{"assembleDebug"},
		TaskCatalog:    []TaskCatalog{{RawName: ":app:assembleDebug"}},
		ModelSelectors: []string{":app"},
		SyncFragments:  []string{"variant:debug"},
		ContentRoots: []ContentRoot{{
			Path:    "/repo/app/src/main",
			Entries: []ContentEntry{{Path: "/repo/app/src/main/java", Kind: "source"}},
		}},
		Materialization: Materialization{
			ID:                   "materialization.debug",
			ClasspathSnapshotIDs: []string{"classpath-snapshot"},
			SourceRoots:          []string{"src/main"},
			ManifestPaths:        []string{"src/main/AndroidManifest.xml"},
			ProducedArtifactIDs:  []string{"artifact.classes"},
			ProducedArtifacts:    []Artifact{{ID: "artifact.classes", Path: "build/classes.jar"}},
		},
		Dependencies: []Dependency{{TargetModulePath: ":lib"}},
		OrderEntries: []OrderEntry{{Kind: "module", Name: ":lib"}},
		Actions:      []Action{{ID: "action.compile", Inputs: []string{"artifact.source"}, Outputs: []string{"artifact.classes"}}},
		Targets:      []Target{{Kind: "compile", TaskNames: []string{":app:compileDebugKotlin"}, ArtifactIDs: []string{"artifact.classes"}}},
	}
}

func testIdentity() Identity {
	return Identity{
		IDESourceSetIDs: []string{"app/main"},
		ModelSelectors:  []string{":app"},
		SyncFragments:   []string{"module::app"},
	}
}

func testCompatibility() Compatibility {
	return Compatibility{
		SourceSetOrder: []string{"main", "debug"},
		SourceSetNames: []string{"main", "debug"},
		TaskAliases:    []string{"assembleDebug"},
		ModelSelectors: []string{":app"},
		SyncFragments:  []string{"variant:debug"},
	}
}

func mutateSyncModule(mod Module) {
	mutateIdentity(mod.Identity)
	mod.KotlinFreeCompilerArgs[0] = "changed"
	mod.LintDisabledChecks[0] = "Changed"
	mod.ConsumerProguardFiles[0] = "changed.pro"
	mod.DefaultTasks[0] = ":app:changed"
	mod.Tasks[0].Name = "changed"
	mod.TaskCatalog[0].RawName = "changed"
	mod.Dependencies[0].TargetModulePath = ":changed"
	mutateSyncVariant(mod.Variants[0])
}

func mutateSyncVariant(variant Variant) {
	mutateIdentity(variant.Identity)
	mutateCompatibility(variant.Compatibility)
	variant.Flavors[0] = "changed"
	variant.ProguardFiles[0] = "changed.pro"
	variant.ConsumerProguardFiles[0] = "changed.pro"
	variant.SourceSetOrder[0] = "changed"
	variant.SourceSetNames[0] = "changed"
	variant.TaskAliases[0] = "changed"
	variant.TaskCatalog[0].RawName = "changed"
	variant.ModelSelectors[0] = "changed"
	variant.SyncFragments[0] = "changed"
	variant.ContentRoots[0].Entries[0].Path = "changed"
	variant.Materialization.ClasspathSnapshotIDs[0] = "changed"
	variant.Materialization.SourceRoots[0] = "changed"
	variant.Materialization.ManifestPaths[0] = "changed"
	variant.Materialization.ProducedArtifactIDs[0] = "changed"
	variant.Materialization.ProducedArtifacts[0].ID = "changed"
	variant.Dependencies[0].TargetModulePath = ":changed"
	variant.OrderEntries[0].Name = "changed"
	variant.Actions[0].Inputs[0] = "changed"
	variant.Actions[0].Outputs[0] = "changed"
	variant.Targets[0].TaskNames[0] = "changed"
	variant.Targets[0].ArtifactIDs[0] = "changed"
}

func mutateIdentity(identity Identity) {
	identity.IDESourceSetIDs[0] = "changed"
	identity.ModelSelectors[0] = "changed"
	identity.SyncFragments[0] = "changed"
}

func mutateCompatibility(compatibility Compatibility) {
	compatibility.SourceSetOrder[0] = "changed"
	compatibility.SourceSetNames[0] = "changed"
	compatibility.TaskAliases[0] = "changed"
	compatibility.ModelSelectors[0] = "changed"
	compatibility.SyncFragments[0] = "changed"
}
