package graph

import "testing"

// buildMultiModuleGraph creates a graph with two modules (app and lib), each with
// a variant and materialization, where app's compile action consumes lib's output.
func buildMultiModuleGraph(t *testing.T) *Graph {
	t.Helper()
	g := New()

	appModule := LogicalModule{ID: "module.app", Name: "app", Path: ":app", Kind: ModuleKindAndroidApplication}
	libModule := LogicalModule{ID: "module.lib", Name: "lib", Path: ":lib", Kind: ModuleKindAndroidLibrary}
	mustAdd(t, g.AddLogicalModule(appModule))
	mustAdd(t, g.AddLogicalModule(libModule))

	appVariant := Variant{ID: "variant.app.debug", ModuleID: appModule.ID, Name: "debug", BuildType: "debug"}
	libVariant := Variant{ID: "variant.lib.debug", ModuleID: libModule.ID, Name: "debug", BuildType: "debug"}
	mustAdd(t, g.AddVariant(appVariant))
	mustAdd(t, g.AddVariant(libVariant))

	appMat := Materialization{
		ID: "mat.app.debug", ModuleID: appModule.ID, VariantID: appVariant.ID,
		Kind: MaterializationKindSourceBacked, SourceRoots: []string{"app/src/main/java", "app/src/main/kotlin"},
	}
	libMat := Materialization{
		ID: "mat.lib.debug", ModuleID: libModule.ID, VariantID: libVariant.ID,
		Kind: MaterializationKindSourceBacked, SourceRoots: []string{"lib/src/main/java"},
	}
	mustAdd(t, g.AddMaterialization(appMat))
	mustAdd(t, g.AddMaterialization(libMat))

	libSource := Artifact{ID: "artifact.lib.source", MaterializationID: libMat.ID, Kind: ArtifactKindDirectory, Path: "lib/src/main"}
	libClasses := Artifact{ID: "artifact.lib.classes", MaterializationID: libMat.ID, ProducedByActionID: "action.lib.compile", Kind: ArtifactKindJar, Path: "lib/build/classes.jar"}
	appSource := Artifact{ID: "artifact.app.source", MaterializationID: appMat.ID, Kind: ArtifactKindDirectory, Path: "app/src/main"}
	appClasses := Artifact{ID: "artifact.app.classes", MaterializationID: appMat.ID, ProducedByActionID: "action.app.compile", Kind: ArtifactKindJar, Path: "app/build/classes.jar"}
	appApk := Artifact{ID: "artifact.app.apk", MaterializationID: appMat.ID, ProducedByActionID: "action.app.package", Kind: ArtifactKindApk, Path: "app/build/app.apk"}
	mustAdd(t, g.AddArtifact(libSource))
	mustAdd(t, g.AddArtifact(libClasses))
	mustAdd(t, g.AddArtifact(appSource))
	mustAdd(t, g.AddArtifact(appClasses))
	mustAdd(t, g.AddArtifact(appApk))

	libCompile := Action{ID: "action.lib.compile", ModuleID: libModule.ID, VariantID: libVariant.ID, Name: "compileDebug", Kind: ActionKindCompile, Inputs: []ArtifactID{libSource.ID}, Outputs: []ArtifactID{libClasses.ID}}
	appCompile := Action{ID: "action.app.compile", ModuleID: appModule.ID, VariantID: appVariant.ID, Name: "compileDebug", Kind: ActionKindCompile, Inputs: []ArtifactID{appSource.ID, libClasses.ID}, Outputs: []ArtifactID{appClasses.ID}}
	appPackage := Action{ID: "action.app.package", ModuleID: appModule.ID, VariantID: appVariant.ID, Name: "packageDebug", Kind: ActionKindPackage, Inputs: []ArtifactID{appClasses.ID}, Outputs: []ArtifactID{appApk.ID}}
	mustAdd(t, g.AddAction(libCompile))
	mustAdd(t, g.AddAction(appCompile))
	mustAdd(t, g.AddAction(appPackage))

	return g
}

func TestProjectModuleDependencies(t *testing.T) {
	g := buildMultiModuleGraph(t)
	depGraph := g.ProjectModuleDependencies()

	if len(depGraph.Modules) != 2 {
		t.Fatalf("modules = %d, want 2", len(depGraph.Modules))
	}

	if len(depGraph.Dependencies) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(depGraph.Dependencies))
	}

	dep := depGraph.Dependencies[0]
	if dep.FromModuleID != "module.app" || dep.ToModuleID != "module.lib" {
		t.Fatalf("dependency = %s -> %s, want module.app -> module.lib", dep.FromModuleID, dep.ToModuleID)
	}
	if len(dep.ArtifactIDs) != 1 || dep.ArtifactIDs[0] != "artifact.lib.classes" {
		t.Fatalf("dependency artifacts = %v, want [artifact.lib.classes]", dep.ArtifactIDs)
	}
}

func TestProjectModuleDependenciesFromEdges(t *testing.T) {
	g := New()
	modA := LogicalModule{ID: "module.a", Kind: ModuleKindJvmLibrary}
	modB := LogicalModule{ID: "module.b", Kind: ModuleKindJvmLibrary}
	mustAdd(t, g.AddLogicalModule(modA))
	mustAdd(t, g.AddLogicalModule(modB))
	if _, err := g.AddEdge(Edge{From: modA.Ref(), To: modB.Ref(), Kind: EdgeKindDependsOn}); err != nil {
		t.Fatal(err)
	}

	depGraph := g.ProjectModuleDependencies()
	if len(depGraph.Dependencies) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(depGraph.Dependencies))
	}
	if depGraph.Dependencies[0].FromModuleID != "module.a" || depGraph.Dependencies[0].ToModuleID != "module.b" {
		t.Fatalf("unexpected dependency: %+v", depGraph.Dependencies[0])
	}
}

func TestProjectModuleDependenciesEmpty(t *testing.T) {
	g := New()
	depGraph := g.ProjectModuleDependencies()
	if len(depGraph.Modules) != 0 {
		t.Fatalf("modules = %d, want 0", len(depGraph.Modules))
	}
	if len(depGraph.Dependencies) != 0 {
		t.Fatalf("dependencies = %d, want 0", len(depGraph.Dependencies))
	}
}

func TestProjectTaskCatalog(t *testing.T) {
	g := buildMultiModuleGraph(t)
	catalog := g.ProjectTaskCatalog()

	if len(catalog.Modules) != 2 {
		t.Fatalf("catalog modules = %d, want 2", len(catalog.Modules))
	}

	appTasks := catalog.Modules["module.app"]
	if len(appTasks) != 2 {
		t.Fatalf("app tasks = %d, want 2", len(appTasks))
	}
	// Sorted by name: compileDebug, packageDebug
	if appTasks[0].Name != "compileDebug" {
		t.Fatalf("app task[0] = %s, want compileDebug", appTasks[0].Name)
	}
	if appTasks[1].Name != "packageDebug" {
		t.Fatalf("app task[1] = %s, want packageDebug", appTasks[1].Name)
	}
	if appTasks[0].InputCount != 2 {
		t.Fatalf("compileDebug inputs = %d, want 2", appTasks[0].InputCount)
	}

	libTasks := catalog.Modules["module.lib"]
	if len(libTasks) != 1 {
		t.Fatalf("lib tasks = %d, want 1", len(libTasks))
	}
	if libTasks[0].Kind != ActionKindCompile {
		t.Fatalf("lib task kind = %s, want compile", libTasks[0].Kind)
	}
}

func TestProjectContentRoots(t *testing.T) {
	g := buildMultiModuleGraph(t)
	projection := g.ProjectContentRoots()

	if len(projection.Variants) != 2 {
		t.Fatalf("variants = %d, want 2", len(projection.Variants))
	}

	// Sorted by moduleID then variantID: app first, then lib
	appRoots := projection.Variants[0]
	if appRoots.ModuleID != "module.app" {
		t.Fatalf("first variant module = %s, want module.app", appRoots.ModuleID)
	}
	if appRoots.VariantName != "debug" {
		t.Fatalf("variant name = %s, want debug", appRoots.VariantName)
	}
	if len(appRoots.Roots) != 2 {
		t.Fatalf("app roots = %d, want 2", len(appRoots.Roots))
	}
	// Sorted by path
	if appRoots.Roots[0].Path != "app/src/main/java" {
		t.Fatalf("app root[0] = %s, want app/src/main/java", appRoots.Roots[0].Path)
	}
	if appRoots.Roots[1].Path != "app/src/main/kotlin" {
		t.Fatalf("app root[1] = %s, want app/src/main/kotlin", appRoots.Roots[1].Path)
	}
	if appRoots.Roots[0].MaterializationKind != MaterializationKindSourceBacked {
		t.Fatalf("root kind = %s, want source_backed", appRoots.Roots[0].MaterializationKind)
	}

	libRoots := projection.Variants[1]
	if libRoots.ModuleID != "module.lib" {
		t.Fatalf("second variant module = %s, want module.lib", libRoots.ModuleID)
	}
	if len(libRoots.Roots) != 1 {
		t.Fatalf("lib roots = %d, want 1", len(libRoots.Roots))
	}
}

func TestProjectContentRootsExcludesEmptySourceRoots(t *testing.T) {
	g := New()
	mod := LogicalModule{ID: "module.x", Kind: ModuleKindJvmLibrary}
	variant := Variant{ID: "variant.x.main", ModuleID: mod.ID, Name: "main"}
	mat := Materialization{ID: "mat.x", ModuleID: mod.ID, VariantID: variant.ID, Kind: MaterializationKindArtifactBacked}

	mustAdd(t, g.AddLogicalModule(mod))
	mustAdd(t, g.AddVariant(variant))
	mustAdd(t, g.AddMaterialization(mat))

	projection := g.ProjectContentRoots()
	if len(projection.Variants) != 0 {
		t.Fatalf("variants = %d, want 0 (artifact-backed with no source roots)", len(projection.Variants))
	}
}

func TestProjectActionDependencyChains(t *testing.T) {
	g := buildMultiModuleGraph(t)
	chains := g.ProjectActionDependencyChains()

	if len(chains) != 3 {
		t.Fatalf("chains = %d, want 3", len(chains))
	}

	chainMap := map[ActionID]ActionDependencyChain{}
	for _, chain := range chains {
		chainMap[chain.ActionID] = chain
	}

	// lib.compile has no dependencies (depth 0)
	libCompile := chainMap["action.lib.compile"]
	if libCompile.Depth != 0 {
		t.Fatalf("lib.compile depth = %d, want 0", libCompile.Depth)
	}
	if len(libCompile.Dependencies) != 0 {
		t.Fatalf("lib.compile deps = %v, want empty", libCompile.Dependencies)
	}

	// app.compile depends on lib.compile (depth 1)
	appCompile := chainMap["action.app.compile"]
	if appCompile.Depth != 1 {
		t.Fatalf("app.compile depth = %d, want 1", appCompile.Depth)
	}
	if len(appCompile.Dependencies) != 1 || appCompile.Dependencies[0] != "action.lib.compile" {
		t.Fatalf("app.compile deps = %v, want [action.lib.compile]", appCompile.Dependencies)
	}

	// app.package depends on app.compile and transitively on lib.compile (depth 2)
	appPackage := chainMap["action.app.package"]
	if appPackage.Depth != 2 {
		t.Fatalf("app.package depth = %d, want 2", appPackage.Depth)
	}
	if len(appPackage.Dependencies) != 2 {
		t.Fatalf("app.package deps = %v, want 2 deps", appPackage.Dependencies)
	}
}

func TestProjectTaskCatalogEmpty(t *testing.T) {
	g := New()
	catalog := g.ProjectTaskCatalog()
	if len(catalog.Modules) != 0 {
		t.Fatalf("catalog modules = %d, want 0", len(catalog.Modules))
	}
}
