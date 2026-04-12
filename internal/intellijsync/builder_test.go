package intellijsync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/project"
)

func TestBuilderProjectsTypedModulesAndJvmMainVariant(t *testing.T) {
	prj := sampleSyncProject(t)
	cfg, err := configmodel.DefaultBuilder{}.Build(prj)
	if err != nil {
		t.Fatalf("config model build failed: %v", err)
	}
	model, err := Builder{}.Build(cfg, prj)
	if err != nil {
		t.Fatalf("sync build failed: %v", err)
	}
	if model.ProjectName != prj.Name || model.Repo != prj.RootDir {
		t.Fatalf("unexpected sync project metadata: %#v", model)
	}
	if len(model.Modules) != 2 {
		t.Fatalf("expected two projected modules, got %#v", model.Modules)
	}
	lib, ok := model.Module(":lib")
	if !ok {
		t.Fatal("expected jvm module")
	}
	if lib.Kind != "jvm-library" || lib.GraphKind != "jvm_library" {
		t.Fatalf("unexpected jvm module shape: %#v", lib)
	}
	if lib.Identity.GraphModuleID == "" || lib.Identity.ModulePath != ":lib" || lib.Identity.IDEModuleID != "lib" {
		t.Fatalf("expected JVM module identity mapping, got %#v", lib.Identity)
	}
	if !sameStrings(lib.DefaultTasks, []string{":lib:build", ":lib:test"}) {
		t.Fatalf("unexpected jvm default tasks: %#v", lib.DefaultTasks)
	}
	if !lib.HasTask("build") || !lib.HasTask("test") || lib.HasTask("installDebug") {
		t.Fatalf("unexpected jvm task surface: %#v", lib.Tasks)
	}
	if len(lib.TaskCatalog) == 0 {
		t.Fatalf("expected JVM module task catalog, got %#v", lib.TaskCatalog)
	}
	buildTask, ok := findTaskCatalog(lib.TaskCatalog, "build")
	if !ok {
		t.Fatalf("expected build task in JVM module task catalog, got %#v", lib.TaskCatalog)
	}
	if buildTask.NormalizedCommand != "build" || buildTask.Kind != "build" || buildTask.TargetVariant != "main" || !buildTask.Supported || !buildTask.Runnable || buildTask.Test || buildTask.Install {
		t.Fatalf("unexpected JVM build task catalog entry: %#v", buildTask)
	}
	testTask, ok := findTaskCatalog(lib.TaskCatalog, "test")
	if !ok {
		t.Fatalf("expected test task in JVM module task catalog, got %#v", lib.TaskCatalog)
	}
	if testTask.NormalizedCommand != "test" || testTask.Kind != "test" || testTask.TargetVariant != "main" || !testTask.Test {
		t.Fatalf("unexpected JVM test task catalog entry: %#v", testTask)
	}
	if len(lib.Variants) != 1 || lib.Variants[0].Name != "main" {
		t.Fatalf("expected synthetic main variant, got %#v", lib.Variants)
	}
	if libVar, ok := lib.Variant("main"); !ok || libVar.Name != "main" {
		t.Fatalf("expected lookup for main variant, got %#v", libVar)
	}
	if lib.Variants[0].DisplayName != "Main" {
		t.Fatalf("expected JVM display name in sync model, got %#v", lib.Variants[0])
	}
	if lib.Variants[0].Compatibility.DisplayName != "Main" {
		t.Fatalf("expected JVM compatibility display name in sync model, got %#v", lib.Variants[0].Compatibility)
	}
	if !sameStrings(lib.Variants[0].SourceSetOrder, []string{"main"}) {
		t.Fatalf("expected JVM source-set order in sync model, got %#v", lib.Variants[0])
	}
	if !sameStrings(lib.Variants[0].Compatibility.SourceSetOrder, []string{"main"}) {
		t.Fatalf("expected JVM compatibility source-set order in sync model, got %#v", lib.Variants[0].Compatibility)
	}
	if !sameStrings(lib.Variants[0].TaskAliases, []string{"build", "check", "compile", "test"}) {
		t.Fatalf("expected JVM task aliases in sync model, got %#v", lib.Variants[0])
	}
	if !sameStrings(lib.Variants[0].ModelSelectors, []string{":lib", "main", ":lib#main", "buildType:main"}) {
		t.Fatalf("expected JVM model selectors in sync model, got %#v", lib.Variants[0])
	}
	if !sameStrings(lib.Variants[0].Compatibility.TaskAliases, []string{"build", "check", "compile", "test"}) {
		t.Fatalf("expected JVM compatibility task aliases in sync model, got %#v", lib.Variants[0].Compatibility)
	}
	if !sameStrings(lib.Variants[0].Compatibility.ModelSelectors, []string{":lib", "main", ":lib#main", "buildType:main"}) {
		t.Fatalf("expected JVM compatibility model selectors in sync model, got %#v", lib.Variants[0].Compatibility)
	}
	if lib.Variants[0].Identity.GraphModuleID != lib.Identity.GraphModuleID || lib.Variants[0].Identity.GraphVariantID == "" {
		t.Fatalf("expected JVM variant graph identity mapping, got %#v", lib.Variants[0].Identity)
	}
	if lib.Variants[0].Identity.IDEModuleID != "lib" || lib.Variants[0].Identity.IDEVariantID != "lib/main" {
		t.Fatalf("expected JVM variant IDE identifiers, got %#v", lib.Variants[0].Identity)
	}
	if !sameStrings(lib.Variants[0].Identity.IDESourceSetIDs, []string{"lib/main/sourceSet:main"}) {
		t.Fatalf("expected JVM source-set identity mapping, got %#v", lib.Variants[0].Identity)
	}
	if !sameStrings(lib.Variants[0].Identity.ModelSelectors, []string{":lib", "main", ":lib#main", "buildType:main"}) {
		t.Fatalf("expected JVM identity model selectors in sync model, got %#v", lib.Variants[0].Identity)
	}
	if !sameStrings(lib.Variants[0].Identity.SyncFragments, []string{"module::lib", "variant:main", "buildType:main", "sourceSet:main"}) {
		t.Fatalf("expected JVM identity sync fragments in sync model, got %#v", lib.Variants[0].Identity)
	}
	if len(lib.Variants[0].Actions) != 3 {
		t.Fatalf("expected main JVM variant actions, got %#v", lib.Variants[0].Actions)
	}
	if !hasAction(lib.Variants[0].Actions, "compile") || !hasAction(lib.Variants[0].Actions, "test") {
		t.Fatalf("expected JVM variant actions to include compile and test, got %#v", lib.Variants[0].Actions)
	}
}

func TestBuilderProjectsAndroidDependencyProjection(t *testing.T) {
	prj := sampleSyncProject(t)
	cfg, err := configmodel.DefaultBuilder{}.Build(prj)
	if err != nil {
		t.Fatalf("config model build failed: %v", err)
	}
	model, err := Builder{}.Build(cfg, prj)
	if err != nil {
		t.Fatalf("sync build failed: %v", err)
	}
	app, ok := model.Module(":app")
	if !ok {
		t.Fatal("expected android module")
	}
	if len(app.Variants) < 2 {
		t.Fatalf("expected debug and release variants for app, got %#v", app.Variants)
	}
	if app.Identity.GraphModuleID == "" || app.Identity.ModulePath != ":app" || app.Identity.IDEModuleID != "app" {
		t.Fatalf("expected android module identity mapping, got %#v", app.Identity)
	}
	debugVariant, ok := app.Variant("debug")
	if !ok {
		t.Fatalf("expected debug variant for app, got %#v", app.Variants)
	}
	if len(app.Dependencies) == 0 {
		t.Fatalf("expected app dependencies, got %#v", app.Dependencies)
	}
	found := false
	for _, dep := range app.Dependencies {
		if dep.TargetModulePath == ":lib" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected dependency projection to include :lib, got %#v", app.Dependencies)
	}
	if len(debugVariant.Dependencies) == 0 {
		t.Fatalf("expected debug variant dependencies, got %#v", debugVariant.Dependencies)
	}
	if debugVariant.DisplayName != "Debug" {
		t.Fatalf("expected debug display name in sync model, got %#v", debugVariant)
	}
	if debugVariant.Compatibility.DisplayName != "Debug" {
		t.Fatalf("expected debug compatibility display name in sync model, got %#v", debugVariant.Compatibility)
	}
	if !sameStrings(debugVariant.SourceSetOrder, []string{"main", "debug"}) {
		t.Fatalf("expected debug source-set order in sync model, got %#v", debugVariant)
	}
	if !sameStrings(debugVariant.Compatibility.SourceSetOrder, []string{"main", "debug"}) {
		t.Fatalf("expected debug compatibility source-set order in sync model, got %#v", debugVariant.Compatibility)
	}
	if !sameStrings(debugVariant.Compatibility.SourceSetNames, []string{"main", "debug"}) {
		t.Fatalf("expected debug compatibility source-set names in sync model, got %#v", debugVariant.Compatibility)
	}
	if !sameStrings(debugVariant.TaskAliases, []string{"assembleDebug", "compileDebugSources", "installDebug", "assembleDebugAndroidTest", "compileDebugAndroidTestSources", "installDebugAndroidTest", "uninstallDebugAndroidTest", "compileDebugUnitTestSources", "testDebugUnitTest"}) {
		t.Fatalf("expected debug task aliases in sync model, got %#v", debugVariant)
	}
	if !sameStrings(debugVariant.ModelSelectors, []string{":app", "debug", ":app#debug", "buildType:debug"}) {
		t.Fatalf("expected debug model selectors in sync model, got %#v", debugVariant)
	}
	if !sameStrings(debugVariant.Compatibility.TaskAliases, []string{"assembleDebug", "compileDebugSources", "installDebug", "assembleDebugAndroidTest", "compileDebugAndroidTestSources", "installDebugAndroidTest", "uninstallDebugAndroidTest", "compileDebugUnitTestSources", "testDebugUnitTest"}) {
		t.Fatalf("expected debug compatibility task aliases in sync model, got %#v", debugVariant.Compatibility)
	}
	if !sameStrings(debugVariant.Compatibility.ModelSelectors, []string{":app", "debug", ":app#debug", "buildType:debug"}) {
		t.Fatalf("expected debug compatibility model selectors in sync model, got %#v", debugVariant.Compatibility)
	}
	if !sameStrings(debugVariant.Compatibility.SyncFragments, []string{"module::app", "variant:debug", "buildType:debug", "sourceSet:main", "sourceSet:debug"}) {
		t.Fatalf("expected debug compatibility sync fragments in sync model, got %#v", debugVariant.Compatibility)
	}
	if len(debugVariant.TaskCatalog) == 0 {
		t.Fatalf("expected debug variant task catalog in sync model, got %#v", debugVariant.TaskCatalog)
	}
	installTask, ok := findTaskCatalog(debugVariant.TaskCatalog, "installDebug")
	if !ok {
		t.Fatalf("expected installDebug task in debug catalog, got %#v", debugVariant.TaskCatalog)
	}
	if installTask.NormalizedCommand != "install-debug" || installTask.Kind != "install" || installTask.TargetVariant != "debug" || !installTask.Supported || !installTask.Runnable || !installTask.Install || installTask.Test {
		t.Fatalf("unexpected install task catalog entry: %#v", installTask)
	}
	androidTestInstall, ok := findTaskCatalog(debugVariant.TaskCatalog, "installDebugAndroidTest")
	if !ok {
		t.Fatalf("expected installDebugAndroidTest task in debug catalog, got %#v", debugVariant.TaskCatalog)
	}
	if androidTestInstall.NormalizedCommand != "install-android-tests" || androidTestInstall.Kind != "android-test-install" || androidTestInstall.TargetVariant != "debug" || !androidTestInstall.Install || !androidTestInstall.Test {
		t.Fatalf("unexpected android test install catalog entry: %#v", androidTestInstall)
	}
	if debugVariant.Identity.GraphModuleID != app.Identity.GraphModuleID || debugVariant.Identity.GraphVariantID == "" {
		t.Fatalf("expected debug graph identity mapping in sync model, got %#v", debugVariant.Identity)
	}
	if debugVariant.Identity.ModulePath != ":app" || debugVariant.Identity.VariantName != "debug" {
		t.Fatalf("expected debug identity module and variant names in sync model, got %#v", debugVariant.Identity)
	}
	if debugVariant.Identity.IDEModuleID != "app" || debugVariant.Identity.IDEVariantID != "app/debug" {
		t.Fatalf("expected debug IDE identifiers in sync model, got %#v", debugVariant.Identity)
	}
	if !sameStrings(debugVariant.Identity.IDESourceSetIDs, []string{"app/debug/sourceSet:main", "app/debug/sourceSet:debug"}) {
		t.Fatalf("expected debug source-set identity mapping in sync model, got %#v", debugVariant.Identity)
	}
	if !sameStrings(debugVariant.Identity.ModelSelectors, []string{":app", "debug", ":app#debug", "buildType:debug"}) {
		t.Fatalf("expected debug identity model selectors in sync model, got %#v", debugVariant.Identity)
	}
	if !sameStrings(debugVariant.Identity.SyncFragments, []string{"module::app", "variant:debug", "buildType:debug", "sourceSet:main", "sourceSet:debug"}) {
		t.Fatalf("expected debug identity sync fragments in sync model, got %#v", debugVariant.Identity)
	}
	if debugVariant.CompileSDK != "34" || debugVariant.ApplicationID != "com.example.app.debug" || debugVariant.ApplicationIDSuffix != ".debug" || debugVariant.VersionName != "1.2.3" || debugVariant.MinSDK != "24" || debugVariant.TargetSDK != "34" || len(debugVariant.ConsumerProguardFiles) != 1 {
		t.Fatalf("expected richer variant packaging metadata in sync model, got %#v", debugVariant)
	}
	if debugVariant.Materialization.ID == "" || debugVariant.Materialization.ArtifactSnapshotID == "" || len(debugVariant.Materialization.ManifestPaths) == 0 {
		t.Fatalf("expected graph-backed materialization metadata in sync model, got %#v", debugVariant.Materialization)
	}
	if len(debugVariant.Materialization.ClasspathSnapshotIDs) == 0 {
		t.Fatalf("expected classpath snapshot ids in sync model, got %#v", debugVariant.Materialization)
	}
	if len(debugVariant.Targets) != 5 {
		t.Fatalf("expected IDE targets for debug variant, got %#v", debugVariant.Targets)
	}
	buildTarget, ok := targetByKind(debugVariant.Targets, "build")
	if !ok || buildTarget.TaskName != "assembleDebug" || buildTarget.ArtifactPath == "" {
		t.Fatalf("expected build target projection in sync model, got %#v", debugVariant.Targets)
	}
	installTarget, ok := targetByKind(debugVariant.Targets, "install")
	if !ok || installTarget.TaskName != "installDebug" || installTarget.ArtifactPath == "" {
		t.Fatalf("expected install target projection in sync model, got %#v", debugVariant.Targets)
	}
	uninstallTarget, ok := targetByKind(debugVariant.Targets, "uninstall")
	if !ok || uninstallTarget.TaskName != "uninstallDebug" || uninstallTarget.PackageName != "com.example.app.debug" {
		t.Fatalf("expected uninstall target projection in sync model, got %#v", debugVariant.Targets)
	}
	unitTestTarget, ok := targetByKind(debugVariant.Targets, "unit-test")
	if !ok || unitTestTarget.TaskName != "testDebugUnitTest" || !sameStrings(unitTestTarget.TaskNames, []string{"testDebugUnitTest", "compileDebugUnitTestSources"}) {
		t.Fatalf("expected unit-test target projection in sync model, got %#v", debugVariant.Targets)
	}
	androidTestTarget, ok := targetByKind(debugVariant.Targets, "android-test")
	if !ok || androidTestTarget.TaskName != "installDebugAndroidTest" || !sameStrings(androidTestTarget.TaskNames, []string{"assembleDebugAndroidTest", "compileDebugAndroidTestSources", "installDebugAndroidTest", "uninstallDebugAndroidTest"}) {
		t.Fatalf("expected androidTest target projection in sync model, got %#v", debugVariant.Targets)
	}
	if debugVariant.Materialization.BackingArtifactID == "" || len(debugVariant.Materialization.ProducedArtifactIDs) == 0 || len(debugVariant.Materialization.ProducedArtifacts) == 0 {
		t.Fatalf("expected produced artifact metadata in sync model, got %#v", debugVariant.Materialization)
	}
	if debugVariant.Materialization.ProducedArtifacts[0].ID == "" || debugVariant.Materialization.ProducedArtifacts[0].Kind == "" {
		t.Fatalf("expected structured produced artifacts in sync model, got %#v", debugVariant.Materialization.ProducedArtifacts)
	}
	if debugVariant.Materialization.ProducedArtifacts[0].ProducedByActionID == "" {
		t.Fatalf("expected produced artifact action metadata in sync model, got %#v", debugVariant.Materialization.ProducedArtifacts)
	}
	foundArtifactPath := false
	for _, artifact := range debugVariant.Materialization.ProducedArtifacts {
		if artifact.Path != "" {
			foundArtifactPath = true
			break
		}
	}
	if !foundArtifactPath {
		t.Fatalf("expected at least one produced artifact path in sync model, got %#v", debugVariant.Materialization.ProducedArtifacts)
	}
	if len(debugVariant.ContentRoots) == 0 {
		t.Fatalf("expected content roots in sync model, got %#v", debugVariant)
	}
	appDir := prj.FindModule(":app").Dir
	if !hasContentEntry(debugVariant.ContentRoots, filepath.Join(appDir, "src", "main"), "source") {
		t.Fatalf("expected source content root entry for src/main, got %#v", debugVariant.ContentRoots)
	}
	if !hasContentEntry(debugVariant.ContentRoots, filepath.Join(appDir, "src", "debug", "res"), "resource") {
		t.Fatalf("expected resource content root entry for debug res, got %#v", debugVariant.ContentRoots)
	}
	if !hasContentEntry(debugVariant.ContentRoots, filepath.Join(appDir, "src", "test"), "test") {
		t.Fatalf("expected test content root entry, got %#v", debugVariant.ContentRoots)
	}
	if !hasContentEntry(debugVariant.ContentRoots, filepath.Join(appDir, "src", "androidTest"), "androidTest") {
		t.Fatalf("expected androidTest content root entry, got %#v", debugVariant.ContentRoots)
	}
	if !hasContentEntry(debugVariant.ContentRoots, filepath.Join(appDir, "src", "main", "AndroidManifest.xml"), "manifest") {
		t.Fatalf("expected manifest content root entry, got %#v", debugVariant.ContentRoots)
	}
}

func TestBuilderProjectsRepositoryMetadata(t *testing.T) {
	prj := sampleSyncProject(t)
	prj.Repositories = []project.Repository{
		{
			Name:           "central",
			Kind:           "maven",
			URL:            "https://repo1.maven.org/maven2/",
			Scope:          "dependency",
			Priority:       3,
			Origin:         "settings",
			OfflineAllowed: false,
			IncludeGroups:  []string{"org.example"},
		},
		{
			Name:           "local",
			Kind:           "mavenLocal",
			Scope:          "dependency",
			Priority:       7,
			Origin:         "module-build",
			OfflineAllowed: true,
		},
	}
	cfg, err := configmodel.DefaultBuilder{}.Build(prj)
	if err != nil {
		t.Fatalf("config model build failed: %v", err)
	}
	model, err := Builder{}.Build(cfg, prj)
	if err != nil {
		t.Fatalf("sync build failed: %v", err)
	}
	if len(model.Project.Repositories) != 2 {
		t.Fatalf("expected two projected repositories, got %#v", model.Project.Repositories)
	}
	repos := map[string]Repository{}
	for _, repo := range model.Project.Repositories {
		repos[repo.Name] = repo
	}
	central, ok := repos["central"]
	if !ok {
		t.Fatalf("expected central repository in %#v", model.Project.Repositories)
	}
	if central.Priority != 3 || central.Origin != "settings" || central.OfflineAllowed || !sameStrings(central.IncludeGroups, []string{"org.example"}) {
		t.Fatalf("unexpected central repository projection: %#v", central)
	}
	local, ok := repos["local"]
	if !ok {
		t.Fatalf("expected local repository in %#v", model.Project.Repositories)
	}
	if local.Priority != 7 || local.Origin != "module-build" || !local.OfflineAllowed {
		t.Fatalf("unexpected local repository projection: %#v", local)
	}
}

func TestBuilderPreservesFlavorCoordinatesWithoutGraphLookup(t *testing.T) {
	root := t.TempDir()
	mod := project.Module{
		Path:             ":app",
		Dir:              filepath.Join(root, "app"),
		BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
		Type:             "android-application",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug":   {Name: "debug"},
			"release": {Name: "release"},
		},
	}
	g := graph.New()
	built := buildModule(nil, g, mod)
	if len(built.Variants) != 2 {
		t.Fatalf("expected flavor-expanded variants, got %#v", built.Variants)
	}
	var freeDebug Variant
	found := false
	for _, variant := range built.Variants {
		if variant.Name != "freeDebug" {
			continue
		}
		freeDebug = variant
		found = true
		break
	}
	if !found {
		t.Fatalf("expected freeDebug variant in %#v", built.Variants)
	}
	if freeDebug.BuildType != "debug" {
		t.Fatalf("expected build type to survive graph fallback, got %#v", freeDebug)
	}
	if !sameStrings(freeDebug.Flavors, []string{"free"}) {
		t.Fatalf("expected flavor coordinates to survive graph fallback, got %#v", freeDebug)
	}
	if freeDebug.DisplayName != "Free Debug" {
		t.Fatalf("expected display name to survive graph fallback, got %#v", freeDebug)
	}
	if freeDebug.Compatibility.DisplayName != "Free Debug" {
		t.Fatalf("expected compatibility display name to survive graph fallback, got %#v", freeDebug.Compatibility)
	}
	if !sameStrings(freeDebug.SourceSetOrder, []string{"main", "free", "debug", "freeDebug"}) {
		t.Fatalf("expected source-set order to survive graph fallback, got %#v", freeDebug)
	}
	if !sameStrings(freeDebug.Compatibility.SourceSetOrder, []string{"main", "free", "debug", "freeDebug"}) {
		t.Fatalf("expected compatibility source-set order to survive graph fallback, got %#v", freeDebug.Compatibility)
	}
	if !sameStrings(freeDebug.Compatibility.SourceSetNames, []string{"main", "free", "debug", "freeDebug"}) {
		t.Fatalf("expected compatibility source-set names to survive graph fallback, got %#v", freeDebug.Compatibility)
	}
	if !sameStrings(freeDebug.TaskAliases, []string{"assembleFreeDebug", "compileFreeDebugSources", "installFreeDebug", "assembleFreeDebugAndroidTest", "compileFreeDebugAndroidTestSources", "installFreeDebugAndroidTest", "uninstallFreeDebugAndroidTest", "compileFreeDebugUnitTestSources", "testFreeDebugUnitTest"}) {
		t.Fatalf("expected task aliases to survive graph fallback, got %#v", freeDebug)
	}
	if !sameStrings(freeDebug.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}) {
		t.Fatalf("expected model selectors to survive graph fallback, got %#v", freeDebug)
	}
	if !sameStrings(freeDebug.Compatibility.TaskAliases, []string{"assembleFreeDebug", "compileFreeDebugSources", "installFreeDebug", "assembleFreeDebugAndroidTest", "compileFreeDebugAndroidTestSources", "installFreeDebugAndroidTest", "uninstallFreeDebugAndroidTest", "compileFreeDebugUnitTestSources", "testFreeDebugUnitTest"}) {
		t.Fatalf("expected compatibility task aliases to survive graph fallback, got %#v", freeDebug.Compatibility)
	}
	if !sameStrings(freeDebug.Compatibility.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}) {
		t.Fatalf("expected compatibility model selectors to survive graph fallback, got %#v", freeDebug.Compatibility)
	}
	if !sameStrings(freeDebug.Compatibility.SyncFragments, []string{"module::app", "variant:freeDebug", "buildType:debug", "flavor:free", "sourceSet:main", "sourceSet:free", "sourceSet:debug", "sourceSet:freeDebug"}) {
		t.Fatalf("expected compatibility sync fragments to survive graph fallback, got %#v", freeDebug.Compatibility)
	}
	if freeDebug.Identity.GraphModuleID != "" || freeDebug.Identity.GraphVariantID != "" {
		t.Fatalf("expected graph ids to stay empty during fallback projection, got %#v", freeDebug.Identity)
	}
	if freeDebug.Identity.IDEModuleID != "app" || freeDebug.Identity.IDEVariantID != "app/freeDebug" {
		t.Fatalf("expected fallback IDE identifiers, got %#v", freeDebug.Identity)
	}
	if !sameStrings(freeDebug.Identity.IDESourceSetIDs, []string{"app/freeDebug/sourceSet:main", "app/freeDebug/sourceSet:free", "app/freeDebug/sourceSet:debug", "app/freeDebug/sourceSet:freeDebug"}) {
		t.Fatalf("expected fallback source-set identity mapping, got %#v", freeDebug.Identity)
	}
	if !sameStrings(freeDebug.Identity.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}) {
		t.Fatalf("expected fallback identity model selectors, got %#v", freeDebug.Identity)
	}
	if !sameStrings(freeDebug.Identity.SyncFragments, []string{"module::app", "variant:freeDebug", "buildType:debug", "flavor:free", "sourceSet:main", "sourceSet:free", "sourceSet:debug", "sourceSet:freeDebug"}) {
		t.Fatalf("expected fallback identity sync fragments, got %#v", freeDebug.Identity)
	}
	assembleTask, ok := findTaskCatalog(freeDebug.TaskCatalog, "assembleFreeDebug")
	if !ok {
		t.Fatalf("expected assembleFreeDebug task in flavor catalog, got %#v", freeDebug.TaskCatalog)
	}
	if assembleTask.NormalizedCommand != "assemble-debug" || assembleTask.Kind != "assemble" || assembleTask.TargetVariant != "freeDebug" || !assembleTask.Supported || !assembleTask.Runnable {
		t.Fatalf("unexpected flavored assemble catalog entry: %#v", assembleTask)
	}
	unitTestTask, ok := findTaskCatalog(freeDebug.TaskCatalog, "testFreeDebugUnitTest")
	if !ok {
		t.Fatalf("expected testFreeDebugUnitTest task in flavor catalog, got %#v", freeDebug.TaskCatalog)
	}
	if unitTestTask.NormalizedCommand != "test-debug-unit" || unitTestTask.Kind != "unit-test" || unitTestTask.TargetVariant != "freeDebug" || !unitTestTask.Test || unitTestTask.Install {
		t.Fatalf("unexpected flavored unit test catalog entry: %#v", unitTestTask)
	}
	if len(freeDebug.Targets) != 5 {
		t.Fatalf("expected flavored IDE targets in fallback sync model, got %#v", freeDebug.Targets)
	}
	if target, ok := targetByKind(freeDebug.Targets, "install"); !ok || target.TaskName != "installFreeDebug" {
		t.Fatalf("expected flavored install target in fallback sync model, got %#v", freeDebug.Targets)
	}
	if target, ok := targetByKind(freeDebug.Targets, "android-test"); !ok || target.TaskName != "installFreeDebugAndroidTest" {
		t.Fatalf("expected flavored androidTest target in fallback sync model, got %#v", freeDebug.Targets)
	}
	if !hasContentEntry(freeDebug.ContentRoots, filepath.Join(mod.Dir, "src", "freeDebug", "res"), "resource") {
		t.Fatalf("expected flavor-aware resource content root in fallback, got %#v", freeDebug.ContentRoots)
	}
	if !hasContentEntry(freeDebug.ContentRoots, filepath.Join(mod.Dir, "src", "androidTestFreeDebug"), "androidTest") {
		t.Fatalf("expected variant-specific androidTest content root in fallback, got %#v", freeDebug.ContentRoots)
	}
}

func TestBuilderProjectsCustomVariantNamesWithCoordinateMetadata(t *testing.T) {
	root := t.TempDir()
	mod := project.Module{
		Path:             ":app",
		Dir:              filepath.Join(root, "app"),
		BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
		Type:             "android-application",
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {Name: "debug"},
			"qa": {
				Name:          "qa",
				DeclaredName:  "qa",
				BaseBuildType: "debug",
				Flavors:       []string{"free"},
			},
		},
	}
	g := graph.New()
	built := buildModule(nil, g, mod)
	custom, ok := built.Variant("qa")
	if !ok {
		t.Fatalf("expected qa variant in %#v", built.Variants)
	}
	if custom.CoordinateName != "freeDebug" {
		t.Fatalf("expected coordinate metadata for custom variant, got %#v", custom)
	}
	if custom.Compatibility.VariantName != "qa" || custom.Compatibility.CoordinateName != "freeDebug" {
		t.Fatalf("expected compatibility naming metadata for custom variant, got %#v", custom.Compatibility)
	}
	if custom.Identity.VariantName != "qa" || custom.Identity.DeclaredName != "qa" || custom.Identity.CoordinateName != "freeDebug" {
		t.Fatalf("expected custom variant identity naming metadata, got %#v", custom.Identity)
	}
	if custom.Identity.IDEModuleID != "app" || custom.Identity.IDEVariantID != "app/qa" {
		t.Fatalf("expected custom variant IDE identifiers, got %#v", custom.Identity)
	}
	if !sameStrings(custom.Identity.IDESourceSetIDs, []string{"app/qa/sourceSet:main", "app/qa/sourceSet:free", "app/qa/sourceSet:debug", "app/qa/sourceSet:qa"}) {
		t.Fatalf("expected custom variant source-set identity mapping, got %#v", custom.Identity)
	}
	if !sameStrings(custom.ModelSelectors, []string{":app", "qa", ":app#qa", "freeDebug", "coordinate:freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}) {
		t.Fatalf("expected selectors for custom variant, got %#v", custom.ModelSelectors)
	}
	if !sameStrings(custom.Identity.ModelSelectors, []string{":app", "qa", ":app#qa", "freeDebug", "coordinate:freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}) {
		t.Fatalf("expected identity selectors for custom variant, got %#v", custom.Identity)
	}
	if !sameStrings(custom.Identity.SyncFragments, []string{"module::app", "variant:qa", "coordinate:freeDebug", "buildType:debug", "flavor:free", "sourceSet:main", "sourceSet:free", "sourceSet:debug", "sourceSet:qa"}) {
		t.Fatalf("expected identity sync fragments for custom variant, got %#v", custom.Identity)
	}
}

func sampleSyncProject(t *testing.T) *project.Project {
	t.Helper()
	root := t.TempDir()
	prj := &project.Project{
		Name:    "SyncSample",
		RootDir: root,
		Modules: []project.Module{
			{
				Path:                  ":app",
				Dir:                   filepath.Join(root, "app"),
				BuildFile:             filepath.Join(root, "app", "build.gradle.kts"),
				Type:                  "android-application",
				CompileSDK:            "34",
				Namespace:             "com.example.app",
				ConsumerProguardFiles: []string{"consumer-rules.pro"},
				DefaultConfig: project.DefaultConfig{
					ApplicationID: "com.example.app",
					VersionName:   "1.2.3",
					MinSDK:        "24",
					TargetSDK:     "34",
				},
				BuildTypes: map[string]project.BuildType{
					"debug":   {Name: "debug", ApplicationIDSuffix: ".debug"},
					"release": {Name: "release"},
				},
			},
			{
				Path:      ":lib",
				Dir:       filepath.Join(root, "lib"),
				BuildFile: filepath.Join(root, "lib", "build.gradle.kts"),
				Type:      "jvm-library",
			},
		},
	}
	for _, mod := range prj.Modules {
		if err := os.MkdirAll(mod.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSyncFile(t, prj.FindModule(":app").BuildFile, `
android {
  namespace = "com.example.app"
  compileSdk = 34
  defaultConfig {
    applicationId = "com.example.app"
    versionName = "1.2.3"
    minSdk = 24
    targetSdk = 34
  }
  buildTypes {
    debug {
      applicationIdSuffix = ".debug"
    }
  }
}
consumerProguardFiles("consumer-rules.pro")
dependencies {
  implementation(projects.lib)
}
`)
	writeSyncFile(t, prj.FindModule(":lib").BuildFile, "dependencies {}\n")
	for _, path := range []string{
		filepath.Join(root, "app", "src", "main"),
		filepath.Join(root, "app", "src", "debug"),
		filepath.Join(root, "app", "src", "main", "res"),
		filepath.Join(root, "app", "src", "debug", "res"),
		filepath.Join(root, "app", "src", "test"),
		filepath.Join(root, "app", "src", "androidTest"),
		filepath.Join(root, "lib", "src", "main"),
		filepath.Join(root, "lib", "src", "test"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSyncFile(t, filepath.Join(root, "app", "src", "main", "AndroidManifest.xml"), `<manifest package="com.example.app"/>`)
	writeSyncFile(t, filepath.Join(root, "app", "src", "debug", "AndroidManifest.xml"), `<manifest/>`)
	return prj
}

func writeSyncFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findTaskCatalog(tasks []TaskCatalog, rawName string) (TaskCatalog, bool) {
	for _, task := range tasks {
		if task.RawName == rawName {
			return task, true
		}
	}
	return TaskCatalog{}, false
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

func hasAction(actions []Action, operation string) bool {
	for _, action := range actions {
		if action.Operation == operation {
			return true
		}
	}
	return false
}

func targetByKind(targets []Target, kind string) (Target, bool) {
	for _, target := range targets {
		if target.Kind == kind {
			return target, true
		}
	}
	return Target{}, false
}

func hasContentEntry(roots []ContentRoot, path, kind string) bool {
	for _, root := range roots {
		for _, entry := range root.Entries {
			if entry.Path == path && entry.Kind == kind {
				return true
			}
		}
	}
	return false
}

func hasGeneratedContentRoot(roots []ContentRoot, path string) bool {
	for _, root := range roots {
		if root.Path != path {
			continue
		}
		for _, entry := range root.Entries {
			if entry.Generated && entry.Kind == "generated" {
				return true
			}
		}
	}
	return false
}
