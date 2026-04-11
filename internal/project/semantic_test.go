package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/graph"
)

func TestSemanticGraphSummaryBuildsSemanticNodes(t *testing.T) {
	root := t.TempDir()
	prj := &Project{
		Name:    "SemanticTest",
		RootDir: root,
		Modules: []Module{
			{
				Path:      ":app",
				Dir:       filepath.Join(root, "app"),
				BuildFile: filepath.Join(root, "app", "build.gradle.kts"),
				Type:      "android-application",
				BuildTypes: map[string]BuildType{
					"debug": {Name: "debug"},
				},
			},
		},
	}

	summary := prj.SemanticGraphSummary()
	if summary.NodeCount == 0 || summary.EdgeCount == 0 {
		t.Fatalf("expected semantic graph nodes and edges, got %#v", summary)
	}
	if len(summary.Modules) != 1 {
		t.Fatalf("expected one semantic module, got %#v", summary.Modules)
	}
	if len(summary.Modules[0].Variants) != 2 {
		t.Fatalf("expected two semantic variants, got %#v", summary.Modules[0].Variants)
	}
	if len(summary.Modules[0].Tasks) == 0 {
		t.Fatalf("expected module task projections, got %#v", summary.Modules[0])
	}
	variant := summary.Modules[0].Variants[0]
	if variant.Materialization.ID == "" || variant.Materialization.ArtifactSnapshotID == "" || len(variant.Materialization.ClasspathSnapshotIDs) == 0 {
		t.Fatalf("expected semantic variant ids to be populated, got %#v", variant)
	}
	if len(variant.Materialization.SourceRoots) == 0 {
		t.Fatalf("expected semantic source roots, got %#v", variant)
	}
	if variant.Materialization.BackingArtifactID == "" || len(variant.Materialization.ProducedArtifactIDs) == 0 || len(variant.Materialization.Artifacts) == 0 {
		t.Fatalf("expected semantic artifact-to-source metadata, got %#v", variant.Materialization)
	}
	if len(variant.Actions) == 0 || variant.Actions[0].ID == "" || len(variant.Actions[0].Outputs) == 0 {
		t.Fatalf("expected semantic action summaries, got %#v", variant.Actions)
	}
	if len(variant.TaskProjections) == 0 {
		t.Fatalf("expected variant task projections, got %#v", variant)
	}
}

func TestSemanticDependentModulesUsesGraphTraversal(t *testing.T) {
	root := t.TempDir()
	prj := &Project{
		Name:    "SemanticTest",
		RootDir: root,
		Modules: []Module{
			{
				Path:      ":app",
				Dir:       filepath.Join(root, "app"),
				BuildFile: filepath.Join(root, "app", "build.gradle.kts"),
				Type:      "android-application",
			},
			{
				Path:      ":feature",
				Dir:       filepath.Join(root, "feature"),
				BuildFile: filepath.Join(root, "feature", "build.gradle.kts"),
				Type:      "android-application",
			},
			{
				Path:      ":lib",
				Dir:       filepath.Join(root, "lib"),
				BuildFile: filepath.Join(root, "lib", "build.gradle.kts"),
				Type:      "android-library",
			},
		},
	}
	for _, mod := range prj.Modules {
		if err := os.MkdirAll(mod.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteSemanticFile(t, prj.FindModule(":app").BuildFile, `
dependencies {
  implementation(projects.lib)
}
`)
	mustWriteSemanticFile(t, prj.FindModule(":feature").BuildFile, `
dependencies {
  implementation(projects.app)
}
`)
	mustWriteSemanticFile(t, prj.FindModule(":lib").BuildFile, `
dependencies {
}
`)

	dependents, err := prj.SemanticDependentModules(":lib")
	if err != nil {
		t.Fatalf("SemanticDependentModules returned error: %v", err)
	}
	if len(dependents) != 3 {
		t.Fatalf("unexpected dependent count: %#v", dependents)
	}
	if dependents[0] != ":lib" {
		t.Fatalf("expected target module first, got %#v", dependents)
	}
	want := map[string]struct{}{
		":lib":     {},
		":app":     {},
		":feature": {},
	}
	for _, path := range dependents {
		if _, ok := want[path]; !ok {
			t.Fatalf("unexpected dependent module %q in %#v", path, dependents)
		}
		delete(want, path)
	}
	if len(want) != 0 {
		t.Fatalf("missing dependent modules: %#v", want)
	}

	summary := prj.SemanticGraphSummary()
	libSummary, ok := prj.SemanticModule(":lib")
	if !ok {
		t.Fatalf("expected semantic module summary from %#v", summary.Modules)
	}
	if len(libSummary.DependsOn) != 0 {
		t.Fatalf("expected lib to have no direct module dependencies, got %#v", libSummary.DependsOn)
	}
	appSummary, ok := prj.SemanticModule(":app")
	if !ok || len(appSummary.DependsOn) != 1 || appSummary.DependsOn[0] != ":lib" {
		t.Fatalf("expected app direct dependency on lib, got %#v", appSummary)
	}
}

func TestSemanticGraphBuildsVariantAwareDependencyEdgesAndActionInputs(t *testing.T) {
	root := t.TempDir()
	prj := &Project{
		Name:    "SemanticTest",
		RootDir: root,
		Modules: []Module{
			{
				Path:      ":app",
				Dir:       filepath.Join(root, "app"),
				BuildFile: filepath.Join(root, "app", "build.gradle.kts"),
				Type:      "android-application",
				BuildTypes: map[string]BuildType{
					"debug":   {Name: "debug"},
					"release": {Name: "release"},
				},
			},
			{
				Path:      ":lib",
				Dir:       filepath.Join(root, "lib"),
				BuildFile: filepath.Join(root, "lib", "build.gradle.kts"),
				Type:      "android-library",
				BuildTypes: map[string]BuildType{
					"debug": {Name: "debug"},
				},
			},
		},
	}
	for _, mod := range prj.Modules {
		if err := os.MkdirAll(mod.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteSemanticFile(t, prj.FindModule(":app").BuildFile, `
dependencies {
  implementation(projects.lib)
}
`)
	mustWriteSemanticFile(t, prj.FindModule(":lib").BuildFile, `dependencies {}`)

	g := prj.SemanticGraphDetailed()
	appDebug, ok := prj.SemanticVariant(":app", "debug")
	if !ok {
		t.Fatal("expected app debug variant")
	}
	libDebug, ok := prj.SemanticVariant(":lib", "debug")
	if !ok {
		t.Fatal("expected lib debug variant")
	}
	variantDeps := g.DependenciesOf(graph.NodeRef{Kind: graph.NodeKindVariant, ID: appDebug.ID})
	foundVariantDep := false
	for _, dep := range variantDeps {
		if dep.Kind == graph.NodeKindVariant && dep.ID == libDebug.ID {
			foundVariantDep = true
			break
		}
	}
	if !foundVariantDep {
		t.Fatalf("expected app debug variant dependency on lib debug, got %#v", variantDeps)
	}

	actions, err := prj.SemanticActionsForCommand(":app", "assemble", []string{"debug"})
	if err != nil {
		t.Fatalf("SemanticActionsForCommand returned error: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected one assemble action, got %#v", actions)
	}
	inputs := g.ActionInputs(actions[0].ID)
	foundInput := false
	for _, input := range inputs {
		if input.MaterializationID == graph.MaterializationID(libDebug.Materialization.ID) {
			foundInput = true
			break
		}
	}
	if !foundInput {
		t.Fatalf("expected assemble action to consume lib debug artifact, got %#v", inputs)
	}
	if len(appDebug.DependsOnVariants) == 0 || appDebug.DependsOnVariants[0] != "debug" {
		t.Fatalf("expected variant dependency names in summary, got %#v", appDebug.DependsOnVariants)
	}
	if len(appDebug.DependencyProvenance) != 1 {
		t.Fatalf("expected compact dependency provenance, got %#v", appDebug.DependencyProvenance)
	}
	if got, want := appDebug.DependencyProvenance[0].ModulePath, ":lib"; got != want {
		t.Fatalf("unexpected dependency provenance module path: got %q want %q", got, want)
	}
	if got, want := appDebug.DependencyProvenance[0].DependencyLevel, "variant"; got != want {
		t.Fatalf("unexpected dependency provenance level: got %q want %q", got, want)
	}
}

func TestSemanticGraphSummaryIncludesDependencyClosure(t *testing.T) {
	root := t.TempDir()
	prj := &Project{
		Name:    "SemanticTest",
		RootDir: root,
		Modules: []Module{
			{
				Path:      ":app",
				Dir:       filepath.Join(root, "app"),
				BuildFile: filepath.Join(root, "app", "build.gradle.kts"),
				Type:      "android-application",
			},
			{
				Path:      ":feature",
				Dir:       filepath.Join(root, "feature"),
				BuildFile: filepath.Join(root, "feature", "build.gradle.kts"),
				Type:      "android-application",
			},
			{
				Path:      ":lib",
				Dir:       filepath.Join(root, "lib"),
				BuildFile: filepath.Join(root, "lib", "build.gradle.kts"),
				Type:      "android-library",
			},
		},
	}
	for _, mod := range prj.Modules {
		if err := os.MkdirAll(mod.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteSemanticFile(t, prj.FindModule(":app").BuildFile, `
dependencies {
  implementation(projects.lib)
}
`)
	mustWriteSemanticFile(t, prj.FindModule(":feature").BuildFile, `
dependencies {
  implementation(projects.app)
}
`)
	mustWriteSemanticFile(t, prj.FindModule(":lib").BuildFile, `dependencies {}`)

	summary := prj.SemanticGraphSummary()
	var featureSummary SemanticModuleSummary
	found := false
	for _, mod := range summary.Modules {
		if mod.Path == ":feature" {
			featureSummary = mod
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected feature module summary")
	}
	if !sameStrings(featureSummary.DependsOn, []string{":app"}) {
		t.Fatalf("unexpected direct dependencies: %#v", featureSummary.DependsOn)
	}
	if !sameStrings(featureSummary.DependencyClosure, []string{":app", ":lib"}) {
		t.Fatalf("unexpected dependency closure: %#v", featureSummary.DependencyClosure)
	}
}

func TestJvmModuleDefaultsExposeMainVariantAndJvmTaskSurface(t *testing.T) {
	mod := Module{
		Path: ":lib",
		Type: "jvm-library",
	}

	if got, want := mod.DefaultTasks(), []string{":lib:build", ":lib:test"}; !sameStrings(got, want) {
		t.Fatalf("unexpected default tasks: got %#v want %#v", got, want)
	}

	tasks := mod.Tasks()
	if hasTask(tasks, "installDebug") || hasTask(tasks, "testDebugUnitTest") || hasTask(tasks, "assembleDebug") {
		t.Fatalf("expected JVM tasks to avoid Android-shaped entries, got %#v", tasks)
	}
	if !hasTask(tasks, "build") || !hasTask(tasks, "test") || !hasTask(tasks, "check") {
		t.Fatalf("expected JVM build/test/check tasks, got %#v", tasks)
	}

	variants := mod.Variants()
	if len(variants) != 1 || variants[0].Name != "main" {
		t.Fatalf("expected synthetic main variant for JVM module, got %#v", variants)
	}
	if got := mod.Variant("").Name; got != "main" {
		t.Fatalf("expected empty JVM variant to resolve to main, got %q", got)
	}
}

func TestAndroidModuleDefaultsExposeFlavorAwareTaskSurface(t *testing.T) {
	mod := Module{
		Path:             ":app",
		Type:             "android-application",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
			"paid": {Name: "paid", Dimension: "tier"},
		},
		BuildTypes: map[string]BuildType{
			"debug":   {Name: "debug"},
			"release": {Name: "release"},
		},
	}

	if got, want := mod.DefaultTasks(), []string{":app:assembleFreeDebug", ":app:installFreeDebug", ":app:testFreeDebugUnitTest"}; !sameStrings(got, want) {
		t.Fatalf("unexpected flavored default tasks: got %#v want %#v", got, want)
	}

	tasks := mod.Tasks()
	if !hasTask(tasks, "assembleFreeDebug") || !hasTask(tasks, "assemblePaidRelease") {
		t.Fatalf("expected flavor-aware assemble tasks, got %#v", tasks)
	}
	if !hasTask(tasks, "compileFreeDebugSources") || !hasTask(tasks, "compilePaidReleaseSources") {
		t.Fatalf("expected flavor-aware compile tasks, got %#v", tasks)
	}
	if !hasTask(tasks, "installFreeDebug") || !hasTask(tasks, "installPaidRelease") {
		t.Fatalf("expected flavor-aware install tasks, got %#v", tasks)
	}
	if !hasTask(tasks, "testFreeDebugUnitTest") || !hasTask(tasks, "testPaidReleaseUnitTest") {
		t.Fatalf("expected flavor-aware unit test tasks, got %#v", tasks)
	}
	if !hasTask(tasks, "compileFreeDebugAndroidTestSources") || !hasTask(tasks, "assembleFreeDebugAndroidTest") {
		t.Fatalf("expected flavor-aware androidTest tasks, got %#v", tasks)
	}
	if !hasTask(tasks, "installFreeDebugAndroidTest") || !hasTask(tasks, "uninstallFreeDebugAndroidTest") {
		t.Fatalf("expected flavor-aware androidTest install tasks, got %#v", tasks)
	}
	if hasTask(tasks, "installDebug") || hasTask(tasks, "assembleDebug") {
		t.Fatalf("expected task surface to prefer flavor-qualified variants, got %#v", tasks)
	}
}

func TestSemanticGraphSummaryUsesFlavorQualifiedDebugTaskProjections(t *testing.T) {
	root := t.TempDir()
	prj := &Project{
		Name:    "SemanticTest",
		RootDir: root,
		Modules: []Module{
			{
				Path:             ":app",
				Dir:              filepath.Join(root, "app"),
				BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
				Type:             "android-application",
				FlavorDimensions: []string{"tier"},
				ProductFlavors: map[string]ProductFlavor{
					"free": {Name: "free", Dimension: "tier"},
					"paid": {Name: "paid", Dimension: "tier"},
				},
				BuildTypes: map[string]BuildType{
					"debug":   {Name: "debug"},
					"release": {Name: "release"},
				},
			},
		},
	}
	if err := os.MkdirAll(prj.Modules[0].Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteSemanticFile(t, prj.FindModule(":app").BuildFile, `dependencies {}`)

	summary := prj.SemanticGraphSummary()
	if len(summary.Modules) != 1 || len(summary.Modules[0].Variants) != 4 {
		t.Fatalf("unexpected semantic summary variants: %#v", summary.Modules)
	}
	var freeDebug SemanticVariantSummary
	found := false
	for _, variant := range summary.Modules[0].Variants {
		if variant.Name != "freeDebug" {
			continue
		}
		freeDebug = variant
		found = true
		break
	}
	if !found {
		t.Fatalf("expected freeDebug variant in %#v", summary.Modules[0].Variants)
	}
	if !sameStrings(freeDebug.TaskProjections, []string{
		"assembleFreeDebug",
		"assembleFreeDebugAndroidTest",
		"compileFreeDebugAndroidTestSources",
		"compileFreeDebugSources",
		"compileFreeDebugUnitTestSources",
		"installFreeDebug",
		"installFreeDebugAndroidTest",
		"testFreeDebugUnitTest",
		"uninstallFreeDebugAndroidTest",
	}) {
		t.Fatalf("unexpected freeDebug task projections: %#v", freeDebug.TaskProjections)
	}
	if freeDebug.DisplayName != "Free Debug" {
		t.Fatalf("unexpected freeDebug display name: %#v", freeDebug)
	}
	if freeDebug.Compatibility.DisplayName != "Free Debug" {
		t.Fatalf("unexpected freeDebug compatibility display name: %#v", freeDebug.Compatibility)
	}
	if got, want := freeDebug.SourceSetOrder, []string{"main", "free", "debug", "freeDebug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected freeDebug source-set order: got %#v want %#v", got, want)
	}
	if got, want := freeDebug.Compatibility.SourceSetOrder, []string{"main", "free", "debug", "freeDebug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected freeDebug compatibility source-set order: got %#v want %#v", got, want)
	}
	if got, want := freeDebug.TaskAliases, []string{
		"assembleFreeDebug",
		"compileFreeDebugSources",
		"installFreeDebug",
		"assembleFreeDebugAndroidTest",
		"compileFreeDebugAndroidTestSources",
		"installFreeDebugAndroidTest",
		"uninstallFreeDebugAndroidTest",
		"compileFreeDebugUnitTestSources",
		"testFreeDebugUnitTest",
	}; !sameStrings(got, want) {
		t.Fatalf("unexpected freeDebug task aliases: got %#v want %#v", got, want)
	}
	if got, want := freeDebug.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}; !sameStrings(got, want) {
		t.Fatalf("unexpected freeDebug model selectors: got %#v want %#v", got, want)
	}
	if got, want := freeDebug.Compatibility.TaskAliases, []string{
		"assembleFreeDebug",
		"compileFreeDebugSources",
		"installFreeDebug",
		"assembleFreeDebugAndroidTest",
		"compileFreeDebugAndroidTestSources",
		"installFreeDebugAndroidTest",
		"uninstallFreeDebugAndroidTest",
		"compileFreeDebugUnitTestSources",
		"testFreeDebugUnitTest",
	}; !sameStrings(got, want) {
		t.Fatalf("unexpected freeDebug compatibility task aliases: got %#v want %#v", got, want)
	}
	if got, want := freeDebug.Compatibility.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}; !sameStrings(got, want) {
		t.Fatalf("unexpected freeDebug compatibility model selectors: got %#v want %#v", got, want)
	}
	for _, forbidden := range []string{"installDebug", "testDebugUnitTest", "compileDebugUnitTestSources"} {
		if hasString(freeDebug.TaskProjections, forbidden) {
			t.Fatalf("did not expect legacy debug task projection %q in %#v", forbidden, freeDebug.TaskProjections)
		}
	}
}

func TestModuleResolveVariantReturnsStructuredCoordinateAndConfig(t *testing.T) {
	android := Module{
		Path:                      ":app",
		Type:                      "android-application",
		CompileSDK:                "34",
		BuildToolsVersion:         "34.0.0",
		Namespace:                 "com.example.app",
		DefaultConfig:             DefaultConfig{ApplicationID: "com.example.app"},
		TestInstrumentationRunner: "androidx.test.runner.AndroidJUnitRunner",
		ConsumerProguardFiles:     []string{"consumer-rules.pro"},
		BuildTypes: map[string]BuildType{
			"debug":   {Name: "debug", SigningConfig: "debug", ApplicationIDSuffix: ".debug"},
			"release": {Name: "release", SigningConfig: "release", IsMinifyEnabled: true, IsShrinkResources: true, Optimization: VariantOptimization{MinifyEnabled: true, ShrinkResources: true}},
		},
		SigningConfigs: map[string]SigningConfig{
			"debug":   {Name: "debug"},
			"release": {Name: "release"},
		},
	}
	resolvedDebug := android.ResolveVariant("")
	if resolvedDebug.ModulePath != ":app" || resolvedDebug.Name != "debug" {
		t.Fatalf("unexpected android default resolution: %#v", resolvedDebug)
	}
	if resolvedDebug.Coordinate.Name != "debug" || resolvedDebug.Coordinate.BuildType != "debug" {
		t.Fatalf("unexpected android coordinates: %#v", resolvedDebug.Coordinate)
	}
	if resolvedDebug.Config.Name != "debug" || resolvedDebug.SigningConfig != "debug" || !resolvedDebug.SigningConfigured || !resolvedDebug.Debuggable || !resolvedDebug.Installable || resolvedDebug.ModuleType != "android-application" {
		t.Fatalf("unexpected android config resolution: %#v", resolvedDebug)
	}
	if resolvedDebug.Namespace != "com.example.app" || resolvedDebug.TestInstrumentationRunner != "androidx.test.runner.AndroidJUnitRunner" {
		t.Fatalf("expected namespace and test runner metadata, got %#v", resolvedDebug)
	}
	if resolvedDebug.DisplayName != "Debug" {
		t.Fatalf("expected debug display name, got %#v", resolvedDebug)
	}
	if resolvedDebug.Compatibility.DisplayName != "Debug" {
		t.Fatalf("expected debug compatibility display name, got %#v", resolvedDebug.Compatibility)
	}
	if got, want := resolvedDebug.SourceSetOrder, []string{"main", "debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected debug source-set order: got %#v want %#v", got, want)
	}
	if got, want := resolvedDebug.Compatibility.SourceSetOrder, []string{"main", "debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected debug compatibility source-set order: got %#v want %#v", got, want)
	}
	if got, want := resolvedDebug.Compatibility.SourceSetNames, []string{"main", "debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected debug compatibility source-set names: got %#v want %#v", got, want)
	}
	if got, want := resolvedDebug.TaskAliases, []string{"assembleDebug", "compileDebugSources", "installDebug", "assembleDebugAndroidTest", "compileDebugAndroidTestSources", "installDebugAndroidTest", "uninstallDebugAndroidTest", "compileDebugUnitTestSources", "testDebugUnitTest"}; !sameStrings(got, want) {
		t.Fatalf("unexpected debug task aliases: got %#v want %#v", got, want)
	}
	if got, want := resolvedDebug.ModelSelectors, []string{":app", "debug", ":app#debug", "buildType:debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected debug model selectors: got %#v want %#v", got, want)
	}
	if got, want := resolvedDebug.Compatibility.TaskAliases, []string{"assembleDebug", "compileDebugSources", "installDebug", "assembleDebugAndroidTest", "compileDebugAndroidTestSources", "installDebugAndroidTest", "uninstallDebugAndroidTest", "compileDebugUnitTestSources", "testDebugUnitTest"}; !sameStrings(got, want) {
		t.Fatalf("unexpected debug compatibility task aliases: got %#v want %#v", got, want)
	}
	if got, want := resolvedDebug.Compatibility.ModelSelectors, []string{":app", "debug", ":app#debug", "buildType:debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected debug compatibility model selectors: got %#v want %#v", got, want)
	}
	if got, want := resolvedDebug.Compatibility.SyncFragments, []string{"module::app", "variant:debug", "buildType:debug", "sourceSet:main", "sourceSet:debug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected debug compatibility sync fragments: got %#v want %#v", got, want)
	}
	if resolvedDebug.InstallTask != "installDebug" || resolvedDebug.UninstallTask != "uninstallDebug" {
		t.Fatalf("expected install task metadata on resolved variant, got %#v", resolvedDebug)
	}
	if resolvedDebug.ApplicationIDSuffix != ".debug" || resolvedDebug.ApplicationID != "com.example.app.debug" {
		t.Fatalf("expected applicationId suffix metadata on resolved variant, got %#v", resolvedDebug)
	}
	if resolvedDebug.CompileSDK != "34" || resolvedDebug.BuildToolsVersion != "34.0.0" || len(resolvedDebug.ProguardFiles) != 0 || len(resolvedDebug.ConsumerProguardFiles) != 1 {
		t.Fatalf("expected packaging and optimization metadata on resolved variant, got %#v", resolvedDebug)
	}
	if resolvedDebug.MinifyEnabled || resolvedDebug.ShrinkResources {
		t.Fatalf("expected debug optimization flags to be false, got %#v", resolvedDebug)
	}

	resolvedRelease := android.ResolveVariant("release")
	if !resolvedRelease.MinifyEnabled || !resolvedRelease.SigningConfigured {
		t.Fatalf("expected release resolved metadata to expose top-level optimization/signing flags, got %#v", resolvedRelease)
	}

	jvm := Module{Path: ":lib", Type: "jvm-library"}
	resolvedMain := jvm.ResolveVariant("")
	if resolvedMain.ModulePath != ":lib" || resolvedMain.Name != "main" {
		t.Fatalf("unexpected JVM default resolution: %#v", resolvedMain)
	}
	if resolvedMain.Coordinate.Name != "main" || resolvedMain.Coordinate.BuildType != "main" {
		t.Fatalf("unexpected JVM coordinates: %#v", resolvedMain.Coordinate)
	}
	if resolvedMain.Config.Name != "main" || !resolvedMain.Testable || resolvedMain.ModuleType != "jvm-library" {
		t.Fatalf("unexpected JVM config resolution: %#v", resolvedMain)
	}
	if resolvedMain.DisplayName != "Main" {
		t.Fatalf("expected JVM display name, got %#v", resolvedMain)
	}
	if resolvedMain.Compatibility.DisplayName != "Main" {
		t.Fatalf("expected JVM compatibility display name, got %#v", resolvedMain.Compatibility)
	}
	if got, want := resolvedMain.SourceSetOrder, []string{"main"}; !sameStrings(got, want) {
		t.Fatalf("unexpected JVM source-set order: got %#v want %#v", got, want)
	}
	if got, want := resolvedMain.Compatibility.SourceSetOrder, []string{"main"}; !sameStrings(got, want) {
		t.Fatalf("unexpected JVM compatibility source-set order: got %#v want %#v", got, want)
	}
	if got, want := resolvedMain.TaskAliases, []string{"build", "check", "compile", "test"}; !sameStrings(got, want) {
		t.Fatalf("unexpected JVM task aliases: got %#v want %#v", got, want)
	}
	if got, want := resolvedMain.ModelSelectors, []string{":lib", "main", ":lib#main", "buildType:main"}; !sameStrings(got, want) {
		t.Fatalf("unexpected JVM model selectors: got %#v want %#v", got, want)
	}
	if got, want := resolvedMain.Compatibility.TaskAliases, []string{"build", "check", "compile", "test"}; !sameStrings(got, want) {
		t.Fatalf("unexpected JVM compatibility task aliases: got %#v want %#v", got, want)
	}
	if got, want := resolvedMain.Compatibility.ModelSelectors, []string{":lib", "main", ":lib#main", "buildType:main"}; !sameStrings(got, want) {
		t.Fatalf("unexpected JVM compatibility model selectors: got %#v want %#v", got, want)
	}
	if resolvedMain.Installable || resolvedMain.SigningConfigured || resolvedMain.MinifyEnabled || resolvedMain.ShrinkResources {
		t.Fatalf("expected JVM resolved metadata to stay unset for Android-specific flags, got %#v", resolvedMain)
	}
}

func TestModuleResolveVariantIncludesFlavorCoordinates(t *testing.T) {
	mod := Module{
		Path:             ":app",
		Type:             "android-application",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
			"paid": {Name: "paid", Dimension: "tier"},
		},
		BuildTypes: map[string]BuildType{
			"debug":   {Name: "debug"},
			"release": {Name: "release"},
		},
	}
	resolved := mod.ResolveVariant("freeDebug")
	if resolved.Name != "freeDebug" {
		t.Fatalf("unexpected variant name: %#v", resolved)
	}
	if resolved.Coordinate.BuildType != "debug" {
		t.Fatalf("unexpected build type coordinate: %#v", resolved.Coordinate)
	}
	if got, want := resolved.Coordinate.Flavors, []string{"free"}; !sameStrings(got, want) {
		t.Fatalf("unexpected flavor coordinates: got %#v want %#v", got, want)
	}
	if resolved.Config.BaseBuildType != "debug" || resolved.Config.Name != "freeDebug" {
		t.Fatalf("unexpected resolved config: %#v", resolved.Config)
	}
	if resolved.ApplicationIDSuffix != "" {
		t.Fatalf("expected no suffix metadata without configured suffixes, got %#v", resolved)
	}
	if resolved.DisplayName != "Free Debug" {
		t.Fatalf("unexpected display name: %#v", resolved)
	}
	if resolved.Compatibility.DisplayName != "Free Debug" {
		t.Fatalf("unexpected compatibility display name: %#v", resolved.Compatibility)
	}
	if got, want := resolved.SourceSetOrder, []string{"main", "free", "debug", "freeDebug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected source-set order: got %#v want %#v", got, want)
	}
	if got, want := resolved.Compatibility.SourceSetOrder, []string{"main", "free", "debug", "freeDebug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected compatibility source-set order: got %#v want %#v", got, want)
	}
	if got, want := resolved.Compatibility.SourceSetNames, []string{"main", "free", "debug", "freeDebug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected compatibility source-set names: got %#v want %#v", got, want)
	}
	if got, want := resolved.TaskAliases, []string{"assembleFreeDebug", "compileFreeDebugSources", "installFreeDebug", "assembleFreeDebugAndroidTest", "compileFreeDebugAndroidTestSources", "installFreeDebugAndroidTest", "uninstallFreeDebugAndroidTest", "compileFreeDebugUnitTestSources", "testFreeDebugUnitTest"}; !sameStrings(got, want) {
		t.Fatalf("unexpected task aliases: got %#v want %#v", got, want)
	}
	if got, want := resolved.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}; !sameStrings(got, want) {
		t.Fatalf("unexpected model selectors: got %#v want %#v", got, want)
	}
	if got, want := resolved.Compatibility.TaskAliases, []string{"assembleFreeDebug", "compileFreeDebugSources", "installFreeDebug", "assembleFreeDebugAndroidTest", "compileFreeDebugAndroidTestSources", "installFreeDebugAndroidTest", "uninstallFreeDebugAndroidTest", "compileFreeDebugUnitTestSources", "testFreeDebugUnitTest"}; !sameStrings(got, want) {
		t.Fatalf("unexpected compatibility task aliases: got %#v want %#v", got, want)
	}
	if got, want := resolved.Compatibility.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}; !sameStrings(got, want) {
		t.Fatalf("unexpected compatibility model selectors: got %#v want %#v", got, want)
	}
	if got, want := resolved.Compatibility.SyncFragments, []string{"module::app", "variant:freeDebug", "buildType:debug", "flavor:free", "sourceSet:main", "sourceSet:free", "sourceSet:debug", "sourceSet:freeDebug"}; !sameStrings(got, want) {
		t.Fatalf("unexpected compatibility sync fragments: got %#v want %#v", got, want)
	}
}

func TestResolveDependencyVariantUsesFlavorFallbacksAndMissingDimensions(t *testing.T) {
	app := Module{
		Path: ":app",
		Type: "android-application",
		DefaultConfig: DefaultConfig{
			MissingDimensions: map[string][]string{"minApi": []string{"minApi21"}},
		},
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]ProductFlavor{
			"free": {Name: "free", Dimension: "tier", MatchingFallbacks: []string{"demo"}},
			"paid": {Name: "paid", Dimension: "tier"},
		},
		BuildTypes: map[string]BuildType{
			"debug": {Name: "debug"},
		},
	}
	lib := Module{
		Path:             ":lib",
		Type:             "android-library",
		FlavorDimensions: []string{"minApi", "tier"},
		ProductFlavors: map[string]ProductFlavor{
			"minApi21": {Name: "minApi21", Dimension: "minApi"},
			"demo":     {Name: "demo", Dimension: "tier"},
			"paid":     {Name: "paid", Dimension: "tier"},
		},
		BuildTypes: map[string]BuildType{
			"debug": {Name: "debug"},
		},
	}

	target := lib.resolveDependencyVariant(app.ResolveVariant("freeDebug"))
	if target != "minApi21DemoDebug" {
		t.Fatalf("unexpected matched dependency variant: %q", target)
	}
}

func TestSemanticActionsForAndroidTestInstallCommandsUseVariantAwareOperations(t *testing.T) {
	root := t.TempDir()
	prj := &Project{
		Name:    "SemanticTest",
		RootDir: root,
		Modules: []Module{{
			Path:             ":app",
			Dir:              filepath.Join(root, "app"),
			BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
			Type:             "android-application",
			FlavorDimensions: []string{"tier"},
			ProductFlavors: map[string]ProductFlavor{
				"free": {Name: "free", Dimension: "tier"},
			},
			BuildTypes: map[string]BuildType{
				"debug": {Name: "debug"},
			},
		}},
	}
	if err := os.MkdirAll(prj.Modules[0].Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteSemanticFile(t, prj.FindModule(":app").BuildFile, `dependencies {}`)

	installActions, err := prj.SemanticActionsForCommand(":app", "install-android-tests", []string{"freeDebug"})
	if err != nil {
		t.Fatalf("SemanticActionsForCommand install returned error: %v", err)
	}
	if len(installActions) != 1 || installActions[0].Attributes["operation"] != "install-android-tests" || installActions[0].Name != "installFreeDebugAndroidTest" {
		t.Fatalf("unexpected install androidTest actions: %#v", installActions)
	}

	uninstallActions, err := prj.SemanticActionsForCommand(":app", "uninstall-android-tests", []string{"freeDebug"})
	if err != nil {
		t.Fatalf("SemanticActionsForCommand uninstall returned error: %v", err)
	}
	if len(uninstallActions) != 1 || uninstallActions[0].Attributes["operation"] != "uninstall-android-tests" || uninstallActions[0].Name != "uninstallFreeDebugAndroidTest" {
		t.Fatalf("unexpected uninstall androidTest actions: %#v", uninstallActions)
	}
}

func TestJvmSemanticGraphUsesMainVariantAndJvmActions(t *testing.T) {
	root := t.TempDir()
	prj := &Project{
		Name:    "JvmSemanticTest",
		RootDir: root,
		Modules: []Module{
			{
				Path:      ":lib",
				Dir:       filepath.Join(root, "lib"),
				BuildFile: filepath.Join(root, "lib", "build.gradle.kts"),
				Type:      "jvm-library",
			},
		},
	}
	if err := os.MkdirAll(prj.Modules[0].Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteSemanticFile(t, prj.FindModule(":lib").BuildFile, "dependencies {}\n")

	summary := prj.SemanticGraphSummary()
	if len(summary.Modules) != 1 || len(summary.Modules[0].Variants) != 1 {
		t.Fatalf("expected one JVM semantic variant, got %#v", summary.Modules)
	}
	if summary.Modules[0].Variants[0].Name != "main" {
		t.Fatalf("expected JVM semantic variant to be main, got %#v", summary.Modules[0].Variants[0])
	}

	names, err := prj.SemanticVariantNames(":lib")
	if err != nil {
		t.Fatalf("SemanticVariantNames returned error: %v", err)
	}
	if !sameStrings(names, []string{"main"}) {
		t.Fatalf("expected JVM semantic variant names to be main, got %#v", names)
	}

	if _, ok := prj.SemanticVariant(":lib", "main"); !ok {
		t.Fatal("expected main semantic variant")
	}

	buildActions, err := prj.SemanticActionsForCommand(":lib", "build", []string{"debug"})
	if err != nil {
		t.Fatalf("SemanticActionsForCommand(build) returned error: %v", err)
	}
	if len(buildActions) != 2 {
		t.Fatalf("expected JVM build to plan compile and test actions, got %#v", buildActions)
	}
	if buildActions[0].Attributes["operation"] != "compile" || buildActions[1].Attributes["operation"] != "test" {
		t.Fatalf("expected JVM build to plan compile then test, got %#v", buildActions)
	}

	assembleActions, err := prj.SemanticActionsForCommand(":lib", "assemble", nil)
	if err != nil {
		t.Fatalf("SemanticActionsForCommand(assemble) returned error: %v", err)
	}
	if len(assembleActions) != 1 || assembleActions[0].Attributes["operation"] != "compile" {
		t.Fatalf("expected JVM assemble to plan compile action, got %#v", assembleActions)
	}
}

func TestCustomVariantNamesPreserveCoordinateIdentity(t *testing.T) {
	mod := Module{
		Path:             ":app",
		Dir:              "/repo/app",
		Type:             "android-application",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]BuildType{
			"debug": {Name: "debug"},
			"qa": {
				Name:          "qa",
				DeclaredName:  "qa",
				BaseBuildType: "debug",
				Flavors:       []string{"free"},
			},
		},
	}
	prj := &Project{Name: "Sample", RootDir: "/repo", Modules: []Module{mod}}

	resolved := mod.ResolveVariant("qa")
	if resolved.Name != "qa" || resolved.DeclaredName != "qa" {
		t.Fatalf("expected custom resolved name, got %#v", resolved)
	}
	if resolved.CoordinateName != "freeDebug" || resolved.Coordinate.Name != "freeDebug" {
		t.Fatalf("expected derived coordinate name to remain freeDebug, got %#v", resolved.Coordinate)
	}
	if got, want := resolved.ModelSelectors, []string{":app", "qa", ":app#qa", "freeDebug", "coordinate:freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}; !sameStrings(got, want) {
		t.Fatalf("unexpected custom-name selectors: got %#v want %#v", got, want)
	}
	if got, want := resolved.Compatibility.SyncFragments, []string{"module::app", "variant:qa", "coordinate:freeDebug", "buildType:debug", "flavor:free", "sourceSet:main", "sourceSet:free", "sourceSet:debug", "sourceSet:qa"}; !sameStrings(got, want) {
		t.Fatalf("unexpected custom-name sync fragments: got %#v want %#v", got, want)
	}
	if got, want := resolved.SourceSetOrder, []string{"main", "free", "debug", "qa"}; !sameStrings(got, want) {
		t.Fatalf("unexpected custom-name source-set order: got %#v want %#v", got, want)
	}

	summary := prj.SemanticGraphSummary()
	if len(summary.Modules) != 1 || len(summary.Modules[0].Variants) != 2 {
		t.Fatalf("expected custom variant in summary, got %#v", summary.Modules)
	}
	var custom SemanticVariantSummary
	found := false
	for _, variant := range summary.Modules[0].Variants {
		if variant.Name == "qa" {
			custom = variant
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected qa variant in %#v", summary.Modules[0].Variants)
	}
	if custom.CoordinateName != "freeDebug" || custom.Coordinate.Name != "freeDebug" {
		t.Fatalf("expected semantic coordinate to stay freeDebug, got %#v", custom)
	}
	if semanticVariantID(prj, mod, "qa") == "" {
		t.Fatal("expected semantic variant id for custom variant")
	}
}

func mustWriteSemanticFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func hasTask(tasks []Task, name string) bool {
	for _, task := range tasks {
		if task.Name == name {
			return true
		}
	}
	return false
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
