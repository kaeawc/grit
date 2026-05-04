package configmodel

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/testutil"
)

func TestStoreLoadOrBuildPersistsModel(t *testing.T) {
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
			Path:       ":app",
			Dir:        filepath.Join(root, "app"),
			BuildFile:  filepath.Join(root, "app", "build.gradle.kts"),
			Type:       "android-application",
			BuildTypes: map[string]project.BuildType{"debug": {Name: "debug"}},
		}},
	}
	store := NewStore(nil)
	model, err := store.LoadOrBuild(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	if model.CacheKey() == "" || model.Summary.NodeCount == 0 {
		t.Fatalf("unexpected model: %#v", model)
	}
	if model.CachePolicy.CleanupMode != "background" || model.CachePolicy.SharedTarget == 0 || model.CachePolicy.SharedHard == 0 {
		t.Fatalf("expected persisted cache policy, got %#v", model.CachePolicy)
	}
	cacheFile := cacheFilePath(root, model.CacheKey())
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("expected cache file %s: %v", cacheFile, err)
	}
	model2, err := store.LoadOrBuild(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	if model2.CacheKey() != model.CacheKey() {
		t.Fatalf("expected cache key reuse: %q vs %q", model.CacheKey(), model2.CacheKey())
	}
	if model2.CachePolicy.CleanupMode != model.CachePolicy.CleanupMode {
		t.Fatalf("expected cache policy to persist, got %#v vs %#v", model2.CachePolicy, model.CachePolicy)
	}
}

func TestStoreLoadOrBuildPersistsActionArtifactAndProvenanceSummaries(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", "include(\":app\", \":lib\")\n")
	testutil.WriteFile(t, root, "build.gradle.kts", "plugins {}\n")
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
dependencies {
  implementation(projects.lib)
}
`)
	testutil.WriteFile(t, root, "lib/build.gradle.kts", `
dependencies {
}
`)
	prj := &project.Project{
		RootDir:       root,
		Name:          "Sample",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules: []project.Module{
			{
				Path:      ":app",
				Dir:       filepath.Join(root, "app"),
				BuildFile: filepath.Join(root, "app", "build.gradle.kts"),
				Type:      "android-application",
				BuildTypes: map[string]project.BuildType{
					"debug": {Name: "debug"},
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
	store := NewStore(nil)
	model, err := store.LoadOrBuild(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.ActionSummaries) == 0 || len(model.ArtifactSummaries) == 0 || len(model.ProvenanceSummaries) == 0 {
		t.Fatalf("expected persisted summaries, got %#v", model)
	}
	var appSummary project.SemanticModuleSummary
	foundApp := false
	for _, mod := range model.Summary.Modules {
		if mod.Path == ":app" {
			appSummary = mod
			foundApp = true
			break
		}
	}
	if !foundApp {
		t.Fatal("expected app summary in persisted semantic graph")
	}
	if !reflect.DeepEqual(appSummary.DependsOn, []string{":lib"}) {
		t.Fatalf("expected direct dependency summary, got %#v", appSummary.DependsOn)
	}
	if !reflect.DeepEqual(appSummary.DependencyClosure, []string{":lib"}) {
		t.Fatalf("expected dependency closure summary, got %#v", appSummary.DependencyClosure)
	}
	if len(appSummary.Variants) == 0 {
		t.Fatalf("expected app variant summary, got %#v", appSummary)
	}
	var appDebug project.SemanticVariantSummary
	for _, variant := range appSummary.Variants {
		if variant.Name == "debug" {
			appDebug = variant
			break
		}
	}
	if len(appDebug.DependencyProvenance) != 1 || appDebug.DependencyProvenance[0].ModulePath != ":lib" || appDebug.DependencyProvenance[0].DependencyLevel != "variant" {
		t.Fatalf("expected cached dependency provenance, got %#v", appDebug.DependencyProvenance)
	}
	if len(appSummary.Variants) == 0 || len(appSummary.Variants[0].Actions) == 0 {
		t.Fatalf("expected persisted semantic action summaries, got %#v", appSummary.Variants)
	}
	if appSummary.Variants[0].Actions[0].CacheKey == "" || appSummary.Variants[0].Actions[0].WorkerClass == "" {
		t.Fatalf("expected enriched cache metadata in semantic summary, got %#v", appSummary.Variants[0].Actions[0])
	}
	if appSummary.Variants[0].Materialization.BackingArtifactID == "" || len(appSummary.Variants[0].Materialization.Artifacts) == 0 {
		t.Fatalf("expected persisted semantic artifact summary metadata, got %#v", appSummary.Variants[0].Materialization)
	}
	if _, ok := model.ProvenanceSummaryForArtifact(graph.ArtifactID(appSummary.Variants[0].Materialization.Artifacts[0].ID)); !ok {
		t.Fatalf("expected artifact-to-provenance lookup for semantic summary artifact, got %#v", appSummary.Variants[0].Materialization.Artifacts)
	}
	libActions := model.ActionSummariesForModule(":lib")
	if !hasActionOperation(libActions, "compile") || !hasActionOperation(libActions, "test") {
		t.Fatalf("expected JVM action summaries, got %#v", libActions)
	}
	libCompile, ok := actionSummaryByOperation(libActions, "compile")
	if !ok {
		t.Fatalf("expected compile action summary, got %#v", libActions)
	}
	if libCompile.CacheKey == "" || libCompile.WorkerClass != "kotlin-compile" || libCompile.ResourceClass != "jvm-process" || libCompile.ResourceCost != 1 || libCompile.MaxParallelism != 2 {
		t.Fatalf("expected rich execution/cache metadata, got %#v", libCompile)
	}
	libArtifacts := model.ArtifactSummariesForModule(":lib")
	if len(libArtifacts) == 0 {
		t.Fatalf("expected artifact summaries for JVM module, got %#v", model.ArtifactSummaries)
	}
	provenanceByArtifact, ok := model.ProvenanceSummaryByArtifact(graph.ArtifactID(libArtifacts[0].ID))
	if !ok || provenanceByArtifact.MaterializationID == "" || len(provenanceByArtifact.SourceRoots) == 0 {
		t.Fatalf("expected artifact provenance lookup to resolve source roots, got %#v %v", provenanceByArtifact, ok)
	}
	sourceRoots, ok := model.SourceRootsForArtifact(graph.ArtifactID(libArtifacts[0].ID))
	if !ok || len(sourceRoots) == 0 {
		t.Fatalf("expected artifact source-root lookup, got %#v %v", sourceRoots, ok)
	}
	libProvenance := model.ProvenanceSummariesForModule(":lib")
	if len(libProvenance) != 1 {
		t.Fatalf("expected one provenance summary for JVM module, got %#v", libProvenance)
	}
	if libProvenance[0].VariantName != "main" || len(libProvenance[0].ProducedArtifactIDs) == 0 {
		t.Fatalf("expected main variant provenance with produced artifacts, got %#v", libProvenance[0])
	}
	if len(libProvenance[0].ManifestPaths) == 0 || libProvenance[0].ManifestPaths[0] == "" {
		t.Fatalf("expected manifest candidate paths in provenance summary, got %#v", libProvenance[0])
	}
	variantProvenance, ok := model.ProvenanceSummaryForVariant(":lib", "main")
	if !ok || variantProvenance.MaterializationID == "" || len(variantProvenance.ManifestPaths) == 0 {
		t.Fatalf("expected variant provenance lookup, got %#v %v", variantProvenance, ok)
	}
	libVariantArtifacts := model.ArtifactSummariesForVariant(":lib", "main")
	if len(libVariantArtifacts) == 0 {
		t.Fatalf("expected variant artifact summaries, got %#v", libVariantArtifacts)
	}
	libVariantActions := model.ActionSummariesForVariant(":lib", "main")
	if !hasActionOperation(libVariantActions, "compile") || !hasActionOperation(libVariantActions, "test") {
		t.Fatalf("expected variant action summaries, got %#v", libVariantActions)
	}
	snapshotMatches := model.ProvenanceSummariesByArtifactSnapshot(libProvenance[0].ArtifactSnapshotID)
	if len(snapshotMatches) == 0 || snapshotMatches[0].MaterializationID != libProvenance[0].MaterializationID {
		t.Fatalf("expected artifact snapshot provenance lookup, got %#v", snapshotMatches)
	}
	snapshotArtifacts := model.ArtifactSummariesByArtifactSnapshot(libProvenance[0].ArtifactSnapshotID)
	if len(snapshotArtifacts) == 0 {
		t.Fatalf("expected artifact snapshot artifact lookup, got %#v", snapshotArtifacts)
	}

	model2, err := store.LoadOrBuild(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	if len(model2.ActionSummariesForModule(":lib")) == 0 || len(model2.ArtifactSummariesForModule(":lib")) == 0 {
		t.Fatalf("expected cached summaries on reload, got %#v", model2)
	}
	reloadedCompile, ok := actionSummaryByOperation(model2.ActionSummariesForModule(":lib"), "compile")
	if !ok || reloadedCompile.CacheKey != libCompile.CacheKey {
		t.Fatalf("expected cached action metadata to survive reload, got %#v want %#v", reloadedCompile, libCompile)
	}
	if got, want := model2.ProvenanceSummariesForModule(":lib")[0].VariantName, "main"; got != want {
		t.Fatalf("expected cached provenance summary to survive reload, got %q want %q", got, want)
	}
	if got := model2.ProvenanceSummariesForModule(":lib")[0].ManifestPaths; len(got) == 0 || got[0] == "" {
		t.Fatalf("expected cached manifest paths to survive reload, got %#v", got)
	}
}

func TestModelResolvedVariantsExposeStructuredCoordinates(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", "include(\":app\", \":lib\")\n")
	testutil.WriteFile(t, root, "build.gradle.kts", "plugins {}\n")
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	testutil.WriteFile(t, root, "lib/build.gradle.kts", "dependencies {}\n")
	prj := &project.Project{
		RootDir:       root,
		Name:          "Sample",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules: []project.Module{
			{
				Path:                      ":app",
				Dir:                       filepath.Join(root, "app"),
				BuildFile:                 filepath.Join(root, "app", "build.gradle.kts"),
				Type:                      "android-application",
				Namespace:                 "com.example.app",
				TestInstrumentationRunner: "androidx.test.runner.AndroidJUnitRunner",
				BuildTypes: map[string]project.BuildType{
					"debug":   {Name: "debug", SigningConfig: "debug"},
					"release": {Name: "release", SigningConfig: "release", IsMinifyEnabled: true, IsShrinkResources: true, Optimization: project.VariantOptimization{MinifyEnabled: true, ShrinkResources: true}},
				},
				SigningConfigs: map[string]project.SigningConfig{
					"debug":   {Name: "debug"},
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
	model, err := NewStore(nil).LoadOrBuild(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := model.ResolvedVariants(":app")
	if err != nil {
		t.Fatalf("ResolvedVariants returned error: %v", err)
	}
	if got, want := len(resolved), 2; got != want {
		t.Fatalf("unexpected resolved variant count: got %d want %d", got, want)
	}
	if resolved[0].ModulePath != ":app" || resolved[0].Name != "debug" {
		t.Fatalf("unexpected resolved android variant: %#v", resolved[0])
	}
	if resolved[0].Coordinate.Name != "debug" || resolved[0].Coordinate.BuildType != "debug" {
		t.Fatalf("expected structured coordinates, got %#v", resolved[0].Coordinate)
	}
	if resolved[0].Config.Name != "debug" {
		t.Fatalf("expected resolved config name to stay debug, got %#v", resolved[0].Config)
	}
	if !resolved[0].Installable || !resolved[0].Debuggable || !resolved[0].SigningConfigured || resolved[0].MinifyEnabled || resolved[0].ShrinkResources {
		t.Fatalf("expected top-level resolved variant metadata for debug app variant, got %#v", resolved[0])
	}
	if resolved[0].DisplayName != "Debug" {
		t.Fatalf("expected display name to survive config model, got %#v", resolved[0])
	}
	if resolved[0].MaterializationID == "" || resolved[0].ArtifactSnapshotID == "" || len(resolved[0].ClasspathSnapshotIDs) == 0 {
		t.Fatalf("expected graph-backed materialization metadata on resolved variant, got %#v", resolved[0])
	}
	if len(resolved[0].SourceRoots) == 0 || len(resolved[0].ManifestPaths) == 0 || resolved[0].BackingArtifactID == "" {
		t.Fatalf("expected source/manfiest/backing metadata on resolved variant, got %#v", resolved[0])
	}
	if len(resolved[0].ProducedArtifactIDs) == 0 || len(resolved[0].ProducedArtifacts) == 0 || resolved[0].ProducedArtifacts[0].ID == "" {
		t.Fatalf("expected produced-artifact metadata on resolved variant, got %#v", resolved[0])
	}
	if resolved[0].Namespace != "com.example.app" || resolved[0].TestInstrumentationRunner != "androidx.test.runner.AndroidJUnitRunner" {
		t.Fatalf("expected namespace/test runner metadata on resolved variant, got %#v", resolved[0])
	}
	if len(resolved[0].ProducedArtifactKinds) == 0 || resolved[0].InstallArtifactID == "" {
		t.Fatalf("expected produced-artifact classification metadata on resolved variant, got %#v", resolved[0])
	}
	if resolved[0].BackingArtifactPath == "" {
		t.Fatalf("expected backing-artifact path metadata on resolved variant, got %#v", resolved[0])
	}
	if len(resolved[0].ProducedArtifactPaths) == 0 {
		t.Fatalf("expected produced-artifact path metadata on resolved variant, got %#v", resolved[0])
	}
	if resolved[0].InstallTask != "installDebug" || resolved[0].UninstallTask != "uninstallDebug" {
		t.Fatalf("expected install/uninstall task metadata on resolved variant, got %#v", resolved[0])
	}
	if resolved[0].Compatibility.DisplayName != "Debug" {
		t.Fatalf("expected compatibility display name to survive config model, got %#v", resolved[0].Compatibility)
	}
	if got, want := resolved[0].SourceSetOrder, []string{"main", "debug"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected source-set order to survive config model: got %#v want %#v", got, want)
	}
	if got, want := resolved[0].Compatibility.SourceSetOrder, []string{"main", "debug"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected compatibility source-set order to survive config model: got %#v want %#v", got, want)
	}
	if got, want := resolved[0].Compatibility.SourceSetNames, []string{"main", "debug"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected compatibility source-set names to survive config model: got %#v want %#v", got, want)
	}
	if got, want := resolved[0].TaskAliases, []string{"assembleDebug", "compileDebugSources", "installDebug", "assembleDebugAndroidTest", "compileDebugAndroidTestSources", "installDebugAndroidTest", "uninstallDebugAndroidTest", "compileDebugUnitTestSources", "testDebugUnitTest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected task aliases to survive config model: got %#v want %#v", got, want)
	}
	if got, want := resolved[0].ModelSelectors, []string{":app", "debug", ":app#debug", "buildType:debug"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected model selectors to survive config model: got %#v want %#v", got, want)
	}
	if got, want := resolved[0].Compatibility.TaskAliases, []string{"assembleDebug", "compileDebugSources", "installDebug", "assembleDebugAndroidTest", "compileDebugAndroidTestSources", "installDebugAndroidTest", "uninstallDebugAndroidTest", "compileDebugUnitTestSources", "testDebugUnitTest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected compatibility task aliases to survive config model: got %#v want %#v", got, want)
	}
	if got, want := resolved[0].Compatibility.ModelSelectors, []string{":app", "debug", ":app#debug", "buildType:debug"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected compatibility model selectors to survive config model: got %#v want %#v", got, want)
	}
	if got, want := resolved[0].Compatibility.SyncFragments, []string{"module::app", "variant:debug", "buildType:debug", "sourceSet:main", "sourceSet:debug"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected compatibility sync fragments to survive config model: got %#v want %#v", got, want)
	}
	if resolved[0].MaterializationID == "" || resolved[0].ArtifactSnapshotID == "" || resolved[0].BackingArtifactID == "" {
		t.Fatalf("expected materialization ids on resolved variant, got %#v", resolved[0])
	}
	if len(resolved[0].ClasspathSnapshotIDs) == 0 || len(resolved[0].SourceRoots) == 0 || len(resolved[0].ManifestPaths) == 0 || len(resolved[0].ProducedArtifactIDs) == 0 {
		t.Fatalf("expected generated-artifact and packaging metadata on resolved variant, got %#v", resolved[0])
	}
	if resolved[0].DexMode != "d8" {
		t.Fatalf("expected dex mode d8 for debug variant, got %q", resolved[0].DexMode)
	}
	if !resolved[1].SigningConfigured || !resolved[1].MinifyEnabled || !resolved[1].ShrinkResources || resolved[1].Debuggable {
		t.Fatalf("expected top-level resolved variant metadata for release app variant, got %#v", resolved[1])
	}
	if resolved[1].DexMode != "r8" {
		t.Fatalf("expected dex mode r8 for release variant, got %q", resolved[1].DexMode)
	}
	jvmVariant, ok := model.ResolvedVariant(":lib", "main")
	if !ok {
		t.Fatal("expected JVM resolved variant")
	}
	if jvmVariant.ModulePath != ":lib" || jvmVariant.Name != "main" {
		t.Fatalf("unexpected JVM resolved variant: %#v", jvmVariant)
	}
	if jvmVariant.Coordinate.Name != "main" || jvmVariant.Coordinate.BuildType != "main" {
		t.Fatalf("expected JVM structured coordinates, got %#v", jvmVariant.Coordinate)
	}
	if jvmVariant.Installable || jvmVariant.SigningConfigured || jvmVariant.MinifyEnabled || jvmVariant.ShrinkResources {
		t.Fatalf("expected JVM resolved variant to leave Android-specific metadata unset, got %#v", jvmVariant)
	}
	if jvmVariant.DisplayName != "Main" {
		t.Fatalf("expected JVM display name to survive config model, got %#v", jvmVariant)
	}
	if jvmVariant.MaterializationID == "" || jvmVariant.ArtifactSnapshotID == "" || len(jvmVariant.ProducedArtifactIDs) == 0 {
		t.Fatalf("expected JVM resolved variant to expose graph-backed artifact metadata, got %#v", jvmVariant)
	}
	if jvmVariant.Compatibility.DisplayName != "Main" {
		t.Fatalf("expected JVM compatibility display name to survive config model, got %#v", jvmVariant.Compatibility)
	}
	if got, want := jvmVariant.SourceSetOrder, []string{"main"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected JVM source-set order: got %#v want %#v", got, want)
	}
	if got, want := jvmVariant.Compatibility.SourceSetOrder, []string{"main"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected JVM compatibility source-set order: got %#v want %#v", got, want)
	}
	if got, want := jvmVariant.TaskAliases, []string{"build", "check", "compile", "test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected JVM task aliases: got %#v want %#v", got, want)
	}
	if got, want := jvmVariant.ModelSelectors, []string{":lib", "main", ":lib#main", "buildType:main"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected JVM model selectors: got %#v want %#v", got, want)
	}
	if got, want := jvmVariant.Compatibility.TaskAliases, []string{"build", "check", "compile", "test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected JVM compatibility task aliases: got %#v want %#v", got, want)
	}
	if got, want := jvmVariant.Compatibility.ModelSelectors, []string{":lib", "main", ":lib#main", "buildType:main"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected JVM compatibility model selectors: got %#v want %#v", got, want)
	}
}

func TestModelResolvedVariantsExposeFlavorCoordinates(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", "include(\":app\")\n")
	testutil.WriteFile(t, root, "build.gradle.kts", "plugins {}\n")
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	prj := &project.Project{
		RootDir:       root,
		Name:          "Sample",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules: []project.Module{
			{
				Path:             ":app",
				Dir:              filepath.Join(root, "app"),
				BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
				Type:             "android-application",
				DefaultConfig:    project.DefaultConfig{ApplicationID: "dev.example", MissingDimensions: map[string][]string{"abi": []string{"x86"}}},
				FlavorDimensions: []string{"tier"},
				ProductFlavors: map[string]project.ProductFlavor{
					"free": {Name: "free", Dimension: "tier", ApplicationIDSuffix: ".free"},
					"paid": {Name: "paid", Dimension: "tier"},
				},
				BuildTypes: map[string]project.BuildType{
					"debug":   {Name: "debug", ApplicationIDSuffix: ".debug"},
					"release": {Name: "release"},
				},
			},
		},
	}
	model, err := NewStore(nil).LoadOrBuild(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := model.ResolvedVariants(":app")
	if err != nil {
		t.Fatalf("ResolvedVariants returned error: %v", err)
	}
	if got, want := len(resolved), 4; got != want {
		t.Fatalf("unexpected resolved variant count: got %d want %d", got, want)
	}
	found := false
	for _, variant := range resolved {
		if variant.Name != "freeDebug" {
			continue
		}
		found = true
		if variant.Coordinate.BuildType != "debug" {
			t.Fatalf("unexpected build type coordinate: %#v", variant.Coordinate)
		}
		if got, want := variant.Coordinate.Flavors, []string{"free"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected flavor coordinates: got %#v want %#v", got, want)
		}
		if variant.Config.Name != "freeDebug" || variant.Config.BaseBuildType != "debug" {
			t.Fatalf("unexpected resolved config: %#v", variant.Config)
		}
		if variant.ApplicationID != "dev.example.free.debug" {
			t.Fatalf("unexpected merged application id: %#v", variant)
		}
		if variant.ApplicationIDSuffix != ".free.debug" || variant.Config.ApplicationIDSuffix != ".free.debug" {
			t.Fatalf("expected applicationIdSuffix to survive resolved variant projection: %#v", variant)
		}
		if !variant.Installable || !variant.Debuggable || variant.MinifyEnabled || variant.ShrinkResources {
			t.Fatalf("unexpected top-level resolved variant flags: %#v", variant)
		}
		if variant.DisplayName != "Free Debug" {
			t.Fatalf("unexpected display name: %#v", variant)
		}
		if variant.Compatibility.DisplayName != "Free Debug" {
			t.Fatalf("unexpected compatibility display name: %#v", variant.Compatibility)
		}
		if got, want := variant.SourceSetOrder, []string{"main", "free", "debug", "freeDebug"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected source-set order: got %#v want %#v", got, want)
		}
		if got, want := variant.Compatibility.SourceSetOrder, []string{"main", "free", "debug", "freeDebug"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected compatibility source-set order: got %#v want %#v", got, want)
		}
		if got, want := variant.TaskAliases, []string{"assembleFreeDebug", "compileFreeDebugSources", "installFreeDebug", "assembleFreeDebugAndroidTest", "compileFreeDebugAndroidTestSources", "installFreeDebugAndroidTest", "uninstallFreeDebugAndroidTest", "compileFreeDebugUnitTestSources", "testFreeDebugUnitTest"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected task aliases: got %#v want %#v", got, want)
		}
		if got, want := variant.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected model selectors: got %#v want %#v", got, want)
		}
		if got, want := variant.Compatibility.TaskAliases, []string{"assembleFreeDebug", "compileFreeDebugSources", "installFreeDebug", "assembleFreeDebugAndroidTest", "compileFreeDebugAndroidTestSources", "installFreeDebugAndroidTest", "uninstallFreeDebugAndroidTest", "compileFreeDebugUnitTestSources", "testFreeDebugUnitTest"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected compatibility task aliases: got %#v want %#v", got, want)
		}
		if got, want := variant.Compatibility.ModelSelectors, []string{":app", "freeDebug", ":app#freeDebug", "buildType:debug", "free", "flavor:free"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected compatibility model selectors: got %#v want %#v", got, want)
		}
		if got, want := variant.MissingDimensions["abi"], []string{"x86"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected missing dimension strategy: got %#v want %#v", got, want)
		}
	}
	if !found {
		t.Fatalf("expected freeDebug variant in %#v", resolved)
	}
}

func TestCacheKeyChangesWhenBuildFileChanges(t *testing.T) {
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
			Path:      ":app",
			Dir:       filepath.Join(root, "app"),
			BuildFile: filepath.Join(root, "app", "build.gradle.kts"),
			Type:      "android-application",
		}},
	}
	key1, _, err := CacheKey(prj)
	if err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies { implementation(projects.lib) }\n")
	key2, _, err := CacheKey(prj)
	if err != nil {
		t.Fatal(err)
	}
	if key1 == key2 {
		t.Fatalf("expected cache key change after build file update: %q", key1)
	}
}

func TestStoreLoadOrBuildConcurrentWriters(t *testing.T) {
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
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < cap(errCh); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.LoadOrBuild(context.Background(), prj)
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func hasActionOperation(actions []ActionSummary, operation string) bool {
	for _, action := range actions {
		if action.Operation == operation {
			return true
		}
	}
	return false
}

func actionSummaryByOperation(actions []ActionSummary, operation string) (ActionSummary, bool) {
	for _, action := range actions {
		if action.Operation == operation {
			return action, true
		}
	}
	return ActionSummary{}, false
}
