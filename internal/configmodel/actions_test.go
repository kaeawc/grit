package configmodel

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
	"github.com/kaeawc/grit/internal/testutil"
)

func TestScheduleActionsPreservesDependencyOrderAndMetadata(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", "include(\":app\")\n")
	testutil.WriteFile(t, root, "build.gradle.kts", "plugins {}\n")
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  compileSdk = 34
}
dependencies {}
`)
	prj := &project.Project{
		RootDir:       root,
		Name:          "Sample",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules: []project.Module{{
			Path:      ":app",
			Dir:       filepath.Join(root, "app"),
			BuildFile: filepath.Join(root, "app", "build.gradle.kts"),
			Type:      "android-application",
			BuildTypes: map[string]project.BuildType{
				"debug": {Name: "debug"},
			},
		}},
	}
	store := NewStore(nil)
	model, err := store.LoadOrBuild(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	actions, err := model.ActionsForCommand(":app", "android-application", "assemble", []string{"debug"})
	if err != nil {
		t.Fatal(err)
	}
	schedule := model.ScheduleActions(actions)
	if len(schedule.Steps) != len(actions) {
		t.Fatalf("expected schedule step count to match actions, got %d vs %d", len(schedule.Steps), len(actions))
	}
	if len(schedule.ResourceBudgets) == 0 {
		t.Fatalf("expected explicit resource budgets, got %#v", schedule.ResourceBudgets)
	}
	if schedule.ResourceBudgets[0].ResourceClass == "" || schedule.ResourceBudgets[0].Capacity == 0 {
		t.Fatalf("expected named resource budget, got %#v", schedule.ResourceBudgets[0])
	}
	if got, want := schedule.Steps[0].Action.Attributes["operation"], "assemble"; got != want {
		t.Fatalf("expected assemble step first, got %q", got)
	}
	if len(schedule.Batches) != 1 || len(schedule.BatchResources) != 1 {
		t.Fatalf("expected one batch and one batch resource summary, got %#v %#v", schedule.Batches, schedule.BatchResources)
	}
	if len(schedule.BatchResources[0].Resources) == 0 {
		t.Fatalf("expected batch resource summary, got %#v", schedule.BatchResources[0])
	}
	ordered := schedule.OrderedActions()
	if len(ordered) != 1 || ordered[0].ID != schedule.Steps[0].Action.ID {
		t.Fatalf("unexpected ordered actions: %#v", ordered)
	}
}

func TestScheduleActionsSplitsReadyBatchBySharedResourceBudget(t *testing.T) {
	g := graph.New()
	moduleID := graph.LogicalModuleID("module:app")
	variantID := graph.VariantID("variant:app:debug")
	if err := g.AddLogicalModule(graph.LogicalModule{ID: moduleID, Path: ":app", Kind: graph.ModuleKindAndroidApplication}); err != nil {
		t.Fatal(err)
	}
	if err := g.AddVariant(graph.Variant{ID: variantID, ModuleID: moduleID, Name: "debug", BuildType: "debug"}); err != nil {
		t.Fatal(err)
	}
	actions := []graph.Action{
		{
			ID:        graph.ActionID("action:compile"),
			ModuleID:  moduleID,
			VariantID: variantID,
			Name:      "compileDebugSources",
			Kind:      graph.ActionKindCompile,
			Attributes: map[string]string{
				"operation": "compile",
			},
		},
		{
			ID:        graph.ActionID("action:compile-tests"),
			ModuleID:  moduleID,
			VariantID: variantID,
			Name:      "compileDebugUnitTestSources",
			Kind:      graph.ActionKindTest,
			Attributes: map[string]string{
				"operation": "compile-tests",
			},
		},
		{
			ID:        graph.ActionID("action:test"),
			ModuleID:  moduleID,
			VariantID: variantID,
			Name:      "testDebugUnitTest",
			Kind:      graph.ActionKindTest,
			Attributes: map[string]string{
				"operation": "test",
			},
		},
	}
	for _, action := range actions {
		if err := g.AddAction(action); err != nil {
			t.Fatal(err)
		}
	}
	model := &Model{Snapshot: g.Snapshot()}
	model.Summary = project.SemanticGraphSummary{
		Modules: []project.SemanticModuleSummary{{
			Path: ":app",
			Variants: []project.SemanticVariantSummary{{
				Name: "debug",
				Actions: []project.SemanticActionSummary{
					{ID: "action:compile", LastCacheProbe: &responsepayload.CacheProbe{ActionID: "action:compile", State: "reused", Basis: "runtime-sidecar", Detail: "previous run reused outputs"}},
					{ID: "action:compile-tests", LastCacheProbe: &responsepayload.CacheProbe{ActionID: "action:compile-tests", State: "rebuilt", Basis: "runtime-sidecar", Detail: "previous run rebuilt outputs"}},
				},
			}},
		}},
	}
	schedule := model.ScheduleActions(actions)
	if got := len(schedule.Batches); got < 2 {
		t.Fatalf("expected ready actions to be split into multiple batches, got %#v", schedule.Batches)
	}
	if schedule.Batches[0][0].CacheKey == "" || schedule.Batches[0][0].RetentionClass == "" || schedule.Batches[0][0].Shareability == "" {
		t.Fatalf("expected schedule step policy metadata, got %#v", schedule.Batches[0][0])
	}
	if !schedule.Batches[0][0].Cacheable || len(schedule.Batches[0][0].ProbeOrder) == 0 || !schedule.Batches[0][0].ExecuteOnMiss {
		t.Fatalf("expected cache probe scheduling metadata, got %#v", schedule.Batches[0][0])
	}
	if schedule.Batches[0][0].EstimatedBytes <= 0 {
		t.Fatalf("expected non-zero estimated bytes for cacheable scheduled action, got %#v", schedule.Batches[0][0])
	}
	if schedule.Batches[0][0].ProbeHint == nil || schedule.Batches[0][0].ProbeHint.State == "" {
		t.Fatalf("expected schedule probe hint, got %#v", schedule.Batches[0][0])
	}
	if got, want := schedule.Batches[0][0].ResourceClass, "jvm-process"; got != want {
		t.Fatalf("resource class = %q, want %q", got, want)
	}
	if got, want := schedule.Batches[0][0].ResourceCost, 1; got != want {
		t.Fatalf("resource cost = %d, want %d", got, want)
	}
	if got, want := schedule.BatchResources[0].Resources[0].ResourceClass, "jvm-process"; got != want {
		t.Fatalf("batch resource class = %q, want %q", got, want)
	}
	if got, want := schedule.BatchResources[0].Resources[0].Capacity, 2; got != want {
		t.Fatalf("batch resource capacity = %d, want %d", got, want)
	}
	if got, want := schedule.BatchResources[0].Resources[0].Used, 2; got != want {
		t.Fatalf("batch resource usage = %d, want %d", got, want)
	}
	if got, want := schedule.BatchResources[0].Resources[0].Remaining, 0; got != want {
		t.Fatalf("batch resource remaining = %d, want %d", got, want)
	}
	if got, want := len(schedule.ResourceBudgets), 4; got != want {
		t.Fatalf("resource budget count = %d, want %d", got, want)
	}
}

func TestScheduleStepEstimatedBytesFollowOperationHeuristic(t *testing.T) {
	model := &Model{}

	compileStep := model.scheduleStepForAction(graph.Action{
		ID:   graph.ActionID("action:compile"),
		Name: "compileDebugSources",
		Kind: graph.ActionKindCompile,
		Attributes: map[string]string{
			"operation": "compile",
		},
	}, nil)
	if compileStep.EstimatedBytes <= 0 {
		t.Fatalf("expected compile step to carry estimated bytes, got %#v", compileStep)
	}

	assembleStep := model.scheduleStepForAction(graph.Action{
		ID:   graph.ActionID("action:assemble"),
		Name: "assembleDebug",
		Kind: graph.ActionKindPackage,
		Attributes: map[string]string{
			"operation": "assemble",
		},
	}, nil)
	if assembleStep.EstimatedBytes <= compileStep.EstimatedBytes {
		t.Fatalf("expected assemble estimate to exceed compile estimate, got compile=%d assemble=%d", compileStep.EstimatedBytes, assembleStep.EstimatedBytes)
	}

	installStep := model.scheduleStepForAction(graph.Action{
		ID:   graph.ActionID("action:install"),
		Name: "installDebug",
		Kind: graph.ActionKindCustom,
		Attributes: map[string]string{
			"operation": "install",
		},
	}, nil)
	if installStep.EstimatedBytes != 0 {
		t.Fatalf("expected non-cacheable install step to have zero estimated bytes, got %#v", installStep)
	}

	lintStep := model.scheduleStepForAction(graph.Action{
		ID:   graph.ActionID("action:lint"),
		Name: "lintDebug",
		Kind: graph.ActionKindLint,
		Attributes: map[string]string{
			"operation": "lint",
		},
	}, nil)
	if !lintStep.Cacheable || len(lintStep.ProbeOrder) == 0 || !lintStep.ExecuteOnMiss {
		t.Fatalf("expected lint step to be cacheable, got %#v", lintStep)
	}
	if got, want := lintStep.WorkerClass, "lint"; got != want {
		t.Fatalf("lint worker class = %q, want %q", got, want)
	}
	if got, want := lintStep.ResourceClass, "jvm-process"; got != want {
		t.Fatalf("lint resource class = %q, want %q", got, want)
	}
	if lintStep.EstimatedBytes <= 0 {
		t.Fatalf("expected lint step to carry estimated bytes, got %#v", lintStep)
	}
}

func TestScheduleStepEstimatedBytesUseObservedRemoteBytesAsFloor(t *testing.T) {
	model := &Model{
		runtimeState: &RuntimeState{
			ActionRemoteBytes: map[string]int64{
				"action:large": 32 * 1024 * 1024,
				"action:small": 1024,
			},
		},
	}

	largeObserved := model.scheduleStepForAction(graph.Action{
		ID:   graph.ActionID("action:large"),
		Name: "assembleDebug",
		Kind: graph.ActionKindPackage,
		Attributes: map[string]string{
			"operation": "assemble",
		},
	}, nil)
	if got, want := largeObserved.EstimatedBytes, int64(32*1024*1024); got != want {
		t.Fatalf("expected observed remote bytes to raise estimate to %d, got %d", want, got)
	}

	smallObserved := model.scheduleStepForAction(graph.Action{
		ID:   graph.ActionID("action:small"),
		Name: "compileDebugSources",
		Kind: graph.ActionKindCompile,
		Attributes: map[string]string{
			"operation": "compile",
		},
	}, nil)
	if got, want := smallObserved.EstimatedBytes, estimatedProbeBytesCompile; got != want {
		t.Fatalf("expected small observed bytes to preserve heuristic floor %d, got %d", want, got)
	}
}

func TestActionsForResolvedCommandUsesFlavorAwareDebugVariants(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", "include(\":app\")\n")
	testutil.WriteFile(t, root, "build.gradle.kts", "plugins {}\n")
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	prj := &project.Project{
		RootDir:       root,
		Name:          "Sample",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules: []project.Module{{
			Path:             ":app",
			Dir:              filepath.Join(root, "app"),
			BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
			Type:             "android-application",
			FlavorDimensions: []string{"tier"},
			ProductFlavors: map[string]project.ProductFlavor{
				"free": {Name: "free", Dimension: "tier"},
				"paid": {Name: "paid", Dimension: "tier"},
			},
			BuildTypes: map[string]project.BuildType{
				"debug":   {Name: "debug"},
				"release": {Name: "release"},
			},
		}},
	}
	model, err := NewStore(nil).LoadOrBuild(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := model.ResolvedVariants(":app")
	if err != nil {
		t.Fatal(err)
	}
	var freeDebug project.ResolvedVariant
	for _, variant := range resolved {
		if variant.Name == "freeDebug" {
			freeDebug = variant
			break
		}
	}
	if freeDebug.Name == "" {
		t.Fatalf("expected freeDebug in resolved variants, got %#v", resolved)
	}

	actions, err := model.ActionsForResolvedCommand(":app", "android-application", "build", resolved)
	if err != nil {
		t.Fatal(err)
	}
	var debugVariantNames []string
	for _, action := range actions {
		if op := action.Attributes["operation"]; op == "test" || op == "compile-tests" {
			debugVariantNames = append(debugVariantNames, action.Attributes["variantName"])
		}
	}
	if !slices.Contains(debugVariantNames, "freeDebug") || !slices.Contains(debugVariantNames, "paidDebug") {
		t.Fatalf("expected flavor-aware debug variants in build plan, got %#v", debugVariantNames)
	}
	if slices.Contains(debugVariantNames, "debug") {
		t.Fatalf("expected flavored debug variants instead of plain debug, got %#v", debugVariantNames)
	}

	compileTests, err := model.ActionsForResolvedCommand(":app", "android-application", "compileDebugUnitTestSources", []project.ResolvedVariant{freeDebug})
	if err != nil {
		t.Fatal(err)
	}
	if len(compileTests) == 0 {
		t.Fatal("expected compile-tests actions for freeDebug")
	}
	for _, action := range compileTests {
		if action.Attributes["variantName"] != "freeDebug" {
			t.Fatalf("expected compile-tests actions to stay on freeDebug, got %#v", compileTests)
		}
	}

	lintActions, err := model.ActionsForCommand(":app", "android-application", "lintDebug", []string{"freeDebug"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lintActions) != 1 {
		t.Fatalf("expected one lint action for freeDebug, got %#v", lintActions)
	}
	if got, want := lintActions[0].Kind, graph.ActionKindLint; got != want {
		t.Fatalf("lint action kind = %q, want %q", got, want)
	}
	if got, want := lintActions[0].Attributes["operation"], "lint"; got != want {
		t.Fatalf("lint action operation = %q, want %q", got, want)
	}
	if got, want := lintActions[0].Attributes["variantName"], "freeDebug"; got != want {
		t.Fatalf("lint action variant = %q, want %q", got, want)
	}
}

func TestDefaultNetworkBudgetConfigNilWhenCacheableActionsOnlyUseLocalTiers(t *testing.T) {
	actions := []graph.Action{
		{ID: graph.ActionID("a1"), Attributes: map[string]string{"operation": "compile"}},
	}
	cfg := defaultNetworkBudgetConfig(actions)
	if cfg != nil {
		t.Fatalf("expected nil NetworkBudgetConfig when probe order is local-only, got %+v", cfg)
	}
}

func TestDefaultNetworkBudgetConfigNilWhenNoCacheableActions(t *testing.T) {
	actions := []graph.Action{
		{ID: graph.ActionID("a1"), Attributes: map[string]string{"operation": "install"}},
	}
	cfg := defaultNetworkBudgetConfig(actions)
	if cfg != nil {
		t.Fatalf("expected nil NetworkBudgetConfig when no cacheable actions, got %+v", cfg)
	}
}

func TestDefaultNetworkBudgetConfigNilForEmptyActions(t *testing.T) {
	cfg := defaultNetworkBudgetConfig(nil)
	if cfg != nil {
		t.Fatal("expected nil NetworkBudgetConfig for nil actions")
	}
}
