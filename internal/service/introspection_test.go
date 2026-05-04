package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

func TestIntrospectionMethods(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".android"), 0o755); err != nil {
		t.Fatal(err)
	}

	buildFile := filepath.Join(root, "app", "build.gradle.kts")
	if err := os.MkdirAll(filepath.Dir(buildFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildFile, []byte(`
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  compileSdk = 34
  defaultConfig {
    applicationId = "com.example.app"
    minSdk = 24
    targetSdk = 34
  }
}

dependencies {
  implementation(libs.okhttp)
  debugImplementation(project(":shared"))
  testImplementation(projects.shared.testFixtures)
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	mod := project.Module{
		Path:                      ":app",
		Dir:                       filepath.Join(root, "app"),
		BuildFile:                 buildFile,
		Type:                      "android-application",
		Namespace:                 "com.example.app",
		ApplicationID:             "com.example.app",
		VersionCode:               "42",
		VersionName:               "1.2.3",
		CompileSDK:                "34",
		BuildToolsVersion:         "34.0.0",
		MinSDK:                    "24",
		TargetSDK:                 "34",
		TestInstrumentationRunner: "androidx.test.runner.AndroidJUnitRunner",
		SourceFileCount:           2,
		UnitTestFileCount:         1,
		UsesCompose:               true,
		KotlinFreeCompilerArgs:    []string{"-opt-in=kotlin.RequiresOptIn"},
		LintDisabledChecks:        []string{"UnusedResources"},
		ConsumerProguardFiles:     []string{"consumer-rules.pro"},
		SigningConfigs: map[string]project.SigningConfig{
			"release": {Name: "release", StoreFile: "release.jks", StorePassword: "store", KeyAlias: "alias", KeyPassword: "key"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug": {
				Name:          "debug",
				SigningConfig: "debug",
			},
			"release": {
				Name:            "release",
				SigningConfig:   "release",
				IsMinifyEnabled: true,
			},
		},
	}
	prj := &project.Project{
		RootDir:         root,
		Name:            "Sample",
		SettingsFile:    filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile:   filepath.Join(root, "build.gradle.kts"),
		VersionCatalogs: []string{"libs.versions.toml"},
		Repositories:    []project.Repository{{Name: "mavenCentral", Kind: "maven", Scope: "all"}},
		GradleProperties: map[string]string{
			"org.gradle.jvmargs": "-Xmx2g",
		},
		RootPlugins: []string{"com.android.application"},
		Modules:     []project.Module{mod},
	}

	svc := New()

	inspect := svc.Inspect(prj)
	if inspect.Repo != root || inspect.Name != "Sample" || len(inspect.Modules) != 1 {
		t.Fatalf("unexpected inspect result: %#v", inspect)
	}
	if inspect.Modules[0].RequestedTasks[0] != ":app:assembleDebug" {
		t.Fatalf("unexpected inspect requested tasks: %#v", inspect.Modules[0].RequestedTasks)
	}

	tasks := svc.Tasks(&mod, prj)
	if tasks.Module != ":app" || len(tasks.Tasks) == 0 {
		t.Fatalf("unexpected tasks result: %#v", tasks)
	}

	signing := svc.SigningReport(&mod, prj)
	if len(signing.Variants) != 2 || signing.Variants[0].ResolvedConfig != "debug" {
		t.Fatalf("unexpected signing report: %#v", signing)
	}
	if signing.Variants[1].ResolvedConfig != "release" || signing.Variants[1].StoreFile != "release.jks" {
		t.Fatalf("unexpected release signing config: %#v", signing.Variants[1])
	}

	props := svc.Properties(&mod, prj)
	if props.Values.Namespace != "com.example.app" || len(props.Variants) != 2 {
		t.Fatalf("unexpected properties result: %#v", props)
	}

	deps, err := svc.Dependencies(&mod, prj)
	if err != nil {
		t.Fatal(err)
	}
	if got := deps.Scopes["main"]; len(got) != 1 || strings.TrimSpace(got[0]) != "library:okhttp" {
		t.Fatalf("unexpected main deps: %#v", deps.Scopes["main"])
	}

	insight, err := svc.DependencyInsight(&mod, prj, "okhttp")
	if err != nil {
		t.Fatal(err)
	}
	if len(insight.Matches["main"]) != 1 || strings.TrimSpace(insight.Matches["main"][0]) != "library:okhttp" {
		t.Fatalf("unexpected dependency insight matches: %#v", insight.Matches)
	}

	environment := svc.BuildEnvironment(prj)
	if environment.Repo != root || environment.GradleProperties["org.gradle.jvmargs"] != "-Xmx2g" {
		t.Fatalf("unexpected build environment: %#v", environment)
	}

	transforms := svc.ArtifactTransforms(&mod, prj)
	if len(transforms.Transforms) == 0 || transforms.Transforms[len(transforms.Transforms)-1] != "apk-signing" {
		t.Fatalf("unexpected artifact transforms: %#v", transforms.Transforms)
	}

	accessors := svc.KotlinDslAccessorsReport(&mod, prj)
	if len(accessors.Accessors) < 4 {
		t.Fatalf("unexpected kotlin dsl accessors: %#v", accessors.Accessors)
	}

	outgoing := svc.OutgoingVariants(&mod, prj)
	if len(outgoing.Variants) != 2 {
		t.Fatalf("unexpected outgoing variants: %#v", outgoing.Variants)
	}

	configs := svc.ResolvableConfigurations(&mod, prj)
	if _, ok := configs.Configurations["debugCompileClasspath"]; !ok {
		t.Fatalf("expected android debug configurations, got %#v", configs.Configurations)
	}

	plan, err := svc.ExplainPlan(context.Background(), prj, &mod, "assemble", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Module != ":app" || plan.Command != "assemble" || len(plan.Actions) == 0 {
		t.Fatalf("unexpected plan explanation: %#v", plan)
	}
	if len(plan.Reasons) == 0 || plan.ModelCacheKey == "" {
		t.Fatalf("expected reasons and model cache key, got %#v", plan)
	}
	if len(plan.Schedule.ResourceBudgets) == 0 || len(plan.Schedule.Batches) == 0 {
		t.Fatalf("expected schedule resource data, got %#v", plan.Schedule)
	}
	if len(plan.Schedule.Batches[0].Actions) == 0 || plan.Schedule.Batches[0].Actions[0].ResourceClass == "" {
		t.Fatalf("expected batch action resource metadata, got %#v", plan.Schedule.Batches[0])
	}
	if plan.Schedule.Batches[0].Actions[0].MaxParallelism == 0 || plan.Schedule.Batches[0].Actions[0].ResourceCost == 0 {
		t.Fatalf("expected batch action execution weights, got %#v", plan.Schedule.Batches[0].Actions[0])
	}
	if plan.Schedule.Batches[0].Actions[0].CacheKey == "" || plan.Schedule.Batches[0].Actions[0].RetentionClass == "" || plan.Schedule.Batches[0].Actions[0].Shareability == "" {
		t.Fatalf("expected batch action cache policy metadata, got %#v", plan.Schedule.Batches[0].Actions[0])
	}
	if !plan.Schedule.Batches[0].Actions[0].Cacheable || len(plan.Schedule.Batches[0].Actions[0].ProbeOrder) == 0 || !plan.Schedule.Batches[0].Actions[0].ExecuteOnMiss {
		t.Fatalf("expected batch action probe metadata, got %#v", plan.Schedule.Batches[0].Actions[0])
	}
	policies, err := svc.PlannedActionPolicies(context.Background(), prj, &mod, "assemble", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if policies.ModelCacheKey == "" || policies.Module != ":app" || policies.Command != "assemble" || len(policies.Policies) == 0 {
		t.Fatalf("unexpected plannedActionPolicies result: %#v", policies)
	}
	if policies.Policies[0].ResourceClass == "" || policies.Policies[0].RetentionClass == "" || len(policies.Policies[0].ProbeOrder) == 0 {
		t.Fatalf("expected policy metadata in plannedActionPolicies result, got %#v", policies.Policies[0])
	}
	policy, err := svc.PlannedActionPolicy(context.Background(), prj, &mod, "assemble", "", policies.Policies[0].ActionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if policy.ModelCacheKey == "" || policy.ActionID != policies.Policies[0].ActionID || policy.Policy.ActionID != policies.Policies[0].ActionID || policy.Policy.CacheKey == "" {
		t.Fatalf("unexpected plannedActionPolicy result: %#v", policy)
	}

	debugVariant := svc.Inspect(prj).SemanticGraph.Modules[0].Variants[0]
	varianceProv, err := svc.VariantProvenance(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if varianceProv.ModelCacheKey == "" || varianceProv.Provenance.Module.Path != ":app" || varianceProv.Provenance.Variant.Name == "" {
		t.Fatalf("unexpected variant provenance: %#v", varianceProv)
	}
	if len(debugVariant.Actions) == 0 {
		t.Fatalf("expected semantic action summaries, got %#v", debugVariant)
	}
	actionProv, err := svc.ActionProvenance(context.Background(), prj, debugVariant.Actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if actionProv.Provenance.Action.ID.String() == "" || len(actionProv.Provenance.Inputs) == 0 {
		t.Fatalf("unexpected action provenance: %#v", actionProv)
	}
	actionInputs, err := svc.ActionInputs(context.Background(), prj, debugVariant.Actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if actionInputs.ModelCacheKey == "" || actionInputs.Inputs.ModulePath != ":app" || actionInputs.Inputs.VariantName == "" || len(actionInputs.Inputs.Inputs) == 0 {
		t.Fatalf("unexpected action inputs result: %#v", actionInputs)
	}
	actionOutputs, err := svc.ActionOutputs(context.Background(), prj, debugVariant.Actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if actionOutputs.ModelCacheKey == "" || actionOutputs.Outputs.ModulePath != ":app" || actionOutputs.Outputs.VariantName == "" || len(actionOutputs.Outputs.Outputs) == 0 {
		t.Fatalf("unexpected action outputs result: %#v", actionOutputs)
	}
	actionDependencies, err := svc.ActionDependencies(context.Background(), prj, debugVariant.Actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if actionDependencies.ModelCacheKey == "" || actionDependencies.Dependencies.ModulePath != ":app" || actionDependencies.Dependencies.VariantName == "" {
		t.Fatalf("unexpected action dependencies result: %#v", actionDependencies)
	}
	actionDependents, err := svc.ActionDependents(context.Background(), prj, debugVariant.Actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if actionDependents.ModelCacheKey == "" || actionDependents.Dependents.ModulePath != ":app" || actionDependents.Dependents.VariantName == "" {
		t.Fatalf("unexpected action dependents result: %#v", actionDependents)
	}
	actionsForModule, err := svc.ActionsForModule(context.Background(), prj, ":app")
	if err != nil {
		t.Fatal(err)
	}
	if actionsForModule.ModelCacheKey == "" || actionsForModule.Module != ":app" || len(actionsForModule.Actions) == 0 {
		t.Fatalf("unexpected actionsForModule result: %#v", actionsForModule)
	}
	actionsForVariant, err := svc.ActionsForVariant(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if actionsForVariant.ModelCacheKey == "" || actionsForVariant.Module != ":app" || actionsForVariant.Variant != debugVariant.Name || len(actionsForVariant.Actions) == 0 {
		t.Fatalf("unexpected actionsForVariant result: %#v", actionsForVariant)
	}
	if len(debugVariant.Materialization.Artifacts) == 0 {
		t.Fatalf("expected semantic artifact summaries, got %#v", debugVariant.Materialization)
	}
	artifactProv, err := svc.ArtifactProvenance(context.Background(), prj, debugVariant.Materialization.Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if artifactProv.Provenance.Materialization.ID == "" || len(artifactProv.Provenance.Artifacts) == 0 {
		t.Fatalf("unexpected artifact provenance: %#v", artifactProv)
	}
	artifactConsumers, err := svc.ArtifactConsumers(context.Background(), prj, debugVariant.Materialization.Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if artifactConsumers.ModelCacheKey == "" || artifactConsumers.Consumers.Artifact.ID.String() == "" {
		t.Fatalf("unexpected artifact consumers result: %#v", artifactConsumers)
	}
	moduleArtifacts, err := svc.ArtifactsForModule(context.Background(), prj, ":app")
	if err != nil {
		t.Fatal(err)
	}
	if moduleArtifacts.ModelCacheKey == "" || moduleArtifacts.Module != ":app" || len(moduleArtifacts.Artifacts) == 0 || len(moduleArtifacts.VariantNames) != 2 {
		t.Fatalf("unexpected module artifacts result: %#v", moduleArtifacts)
	}
	moduleManifest, err := svc.ModuleManifest(context.Background(), prj, ":app")
	if err != nil {
		t.Fatal(err)
	}
	if moduleManifest.ModelCacheKey == "" || moduleManifest.Manifest.ModulePath != ":app" || len(moduleManifest.Manifest.Variants) != 2 {
		t.Fatalf("unexpected module manifest result: %#v", moduleManifest)
	}
	moduleSummary, ok := prj.SemanticModule(":app")
	if !ok {
		t.Fatal("expected semantic module summary")
	}
	moduleByID, err := svc.ModuleByID(context.Background(), prj, moduleSummary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moduleByID.ModelCacheKey == "" || moduleByID.Result.Module.ID.String() != moduleSummary.ID || moduleByID.Result.Summary.Path != ":app" || len(moduleByID.Result.Variants) != 2 {
		t.Fatalf("unexpected moduleByID result: %#v", moduleByID)
	}
	variantByID, err := svc.VariantByID(context.Background(), prj, debugVariant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if variantByID.ModelCacheKey == "" || variantByID.Result.Variant.ID.String() != debugVariant.ID || variantByID.Result.Module.Path != ":app" || len(variantByID.Result.Materializations) == 0 {
		t.Fatalf("unexpected variantByID result: %#v", variantByID)
	}
	actionByID, err := svc.ActionByID(context.Background(), prj, debugVariant.Actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if actionByID.ModelCacheKey == "" || actionByID.Result.Action.ID.String() != debugVariant.Actions[0].ID || actionByID.Result.ModulePath != ":app" || actionByID.Result.VariantName != debugVariant.Name {
		t.Fatalf("unexpected actionByID result: %#v", actionByID)
	}
	if actionByID.Result.Summary.ID != debugVariant.Actions[0].ID || len(actionByID.Result.Inputs) == 0 || len(actionByID.Result.Outputs) == 0 {
		t.Fatalf("expected inputs/outputs in actionByID result, got %#v", actionByID.Result)
	}
	artifactByID, err := svc.ArtifactByID(context.Background(), prj, debugVariant.Materialization.Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if artifactByID.ModelCacheKey == "" || artifactByID.Result.Artifact.ID.String() != debugVariant.Materialization.Artifacts[0].ID || artifactByID.Result.MaterializationID == "" || artifactByID.Result.ArtifactSnapshotID == "" {
		t.Fatalf("unexpected artifactByID result: %#v", artifactByID)
	}
	if artifactByID.Result.Summary.ID != debugVariant.Materialization.Artifacts[0].ID || len(artifactByID.Result.SiblingArtifacts) == 0 {
		t.Fatalf("expected artifact summary/siblings in artifactByID result, got %#v", artifactByID.Result)
	}
	materializationByID, err := svc.MaterializationByID(context.Background(), prj, debugVariant.Materialization.ID)
	if err != nil {
		t.Fatal(err)
	}
	if materializationByID.ModelCacheKey == "" || materializationByID.Result.Materialization.ID.String() != debugVariant.Materialization.ID || materializationByID.Result.ModulePath != ":app" || materializationByID.Result.VariantName != debugVariant.Name {
		t.Fatalf("unexpected materializationByID result: %#v", materializationByID)
	}
	if materializationByID.Result.ArtifactSnapshotID == "" || len(materializationByID.Result.Artifacts) == 0 || len(materializationByID.Result.Actions) == 0 {
		t.Fatalf("expected ids/artifacts/actions in materializationByID result, got %#v", materializationByID.Result)
	}
	materializationConsumers, err := svc.MaterializationConsumers(context.Background(), prj, debugVariant.Materialization.ID)
	if err != nil {
		t.Fatal(err)
	}
	if materializationConsumers.ModelCacheKey == "" || materializationConsumers.Consumers.MaterializationID != debugVariant.Materialization.ID || materializationConsumers.Consumers.ModulePath != ":app" {
		t.Fatalf("unexpected materializationConsumers result: %#v", materializationConsumers)
	}
	if len(materializationConsumers.Consumers.Actions) == 0 || len(materializationConsumers.Consumers.Artifacts) == 0 || len(materializationConsumers.Consumers.ManifestPaths) == 0 {
		t.Fatalf("expected action/artifact/manifest context in materializationConsumers result, got %#v", materializationConsumers.Consumers)
	}
	classpathProv, err := svc.ClasspathProvenance(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if classpathProv.ModelCacheKey == "" || classpathProv.Provenance.MaterializationID == "" || classpathProv.Provenance.ArtifactSnapshotID == "" {
		t.Fatalf("unexpected classpath provenance: %#v", classpathProv)
	}
	if len(classpathProv.Provenance.ClasspathSnapshots) == 0 || classpathProv.Provenance.ClasspathSnapshots[0].ID == "" {
		t.Fatalf("expected classpath snapshot refs in classpath provenance, got %#v", classpathProv.Provenance.ClasspathSnapshots)
	}
	if len(classpathProv.Provenance.Actions) == 0 || len(classpathProv.Provenance.Artifacts) == 0 {
		t.Fatalf("expected action and artifact context in classpath provenance, got %#v", classpathProv.Provenance)
	}
	if len(classpathProv.Provenance.ClasspathSnapshots) == 0 {
		t.Fatalf("expected classpath snapshot refs, got %#v", classpathProv.Provenance)
	}
	classpathSnapshotByID, err := svc.ClasspathSnapshotByID(context.Background(), prj, classpathProv.Provenance.ClasspathSnapshots[0].NormalizedID)
	if err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotByID.ModelCacheKey == "" || classpathSnapshotByID.Result.CanonicalID == "" || classpathSnapshotByID.Result.Result.Snapshot.ID == "" {
		t.Fatalf("unexpected classpathSnapshotByID result: %#v", classpathSnapshotByID)
	}
	classpathSnapshotConsumersByID, err := svc.ClasspathSnapshotConsumersByID(context.Background(), prj, classpathProv.Provenance.ClasspathSnapshots[0].OrderedEntriesID)
	if err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotConsumersByID.ModelCacheKey == "" || classpathSnapshotConsumersByID.Consumers.CanonicalID == "" || len(classpathSnapshotConsumersByID.Consumers.Consumers.Actions) == 0 {
		t.Fatalf("unexpected classpathSnapshotConsumersByID result: %#v", classpathSnapshotConsumersByID)
	}
	sourceSetModel, err := svc.VariantSourceSetModel(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if sourceSetModel.ModelCacheKey == "" || sourceSetModel.SourceSetModel.ModulePath != ":app" || sourceSetModel.SourceSetModel.VariantName != debugVariant.Name {
		t.Fatalf("unexpected variant source-set model result: %#v", sourceSetModel)
	}
	if len(sourceSetModel.SourceSetModel.SourceSetOrder) == 0 || len(sourceSetModel.SourceSetModel.SourceRoots) == 0 || len(sourceSetModel.SourceSetModel.ManifestPaths) == 0 {
		t.Fatalf("expected source-set/source-root metadata in variant source-set model, got %#v", sourceSetModel.SourceSetModel)
	}
	dependencyBindings, err := svc.DependencyBindingsForVariant(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if dependencyBindings.ModelCacheKey == "" || dependencyBindings.Bindings.ModulePath != ":app" || dependencyBindings.Bindings.VariantName != debugVariant.Name {
		t.Fatalf("unexpected variant dependency bindings result: %#v", dependencyBindings)
	}
	moduleBindings, err := svc.DependencyBindingsForModule(context.Background(), prj, ":app")
	if err != nil {
		t.Fatal(err)
	}
	if moduleBindings.ModelCacheKey == "" || moduleBindings.Bindings.ModulePath != ":app" || len(moduleBindings.Bindings.Variants) == 0 {
		t.Fatalf("unexpected module dependency bindings result: %#v", moduleBindings)
	}
	variantRealizations, err := svc.DependencyRealizationsForVariant(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if variantRealizations.ModelCacheKey == "" || variantRealizations.Realizations.ModulePath != ":app" || variantRealizations.Realizations.VariantName != debugVariant.Name {
		t.Fatalf("unexpected variant dependency realizations result: %#v", variantRealizations)
	}
	for _, depRealization := range variantRealizations.Realizations.Dependencies {
		if depRealization.SelectionReason == "" || len(depRealization.SelectionReasons) == 0 {
			t.Fatalf("expected bounded selection-reason metadata in dependency realization, got %#v", depRealization)
		}
		if depRealization.BackingArtifactPath == "" || depRealization.BackingArtifact == nil || len(depRealization.ManifestPaths) == 0 {
			t.Fatalf("expected backing artifact and manifest detail in dependency realization, got %#v", depRealization)
		}
		if len(depRealization.ProducedArtifacts) == 0 || len(depRealization.ProducedArtifactPaths) == 0 || len(depRealization.ProducedArtifactKinds) == 0 {
			t.Fatalf("expected produced artifact detail in dependency realization, got %#v", depRealization)
		}
	}
	moduleRealizations, err := svc.DependencyRealizationsForModule(context.Background(), prj, ":app")
	if err != nil {
		t.Fatal(err)
	}
	if moduleRealizations.ModelCacheKey == "" || moduleRealizations.Realizations.ModulePath != ":app" || len(moduleRealizations.Realizations.Variants) == 0 {
		t.Fatalf("unexpected module dependency realizations result: %#v", moduleRealizations)
	}
	classpathSnapshot, err := svc.ClasspathSnapshot(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if classpathSnapshot.ModelCacheKey == "" || classpathSnapshot.Snapshot.ModulePath != ":app" || len(classpathSnapshot.Snapshot.Snapshot.Entries) == 0 {
		t.Fatalf("unexpected classpath snapshot result: %#v", classpathSnapshot)
	}
	classpathSnapshotProv, err := svc.ClasspathSnapshotProvenance(context.Background(), prj, classpathSnapshot.Snapshot.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotProv.ModelCacheKey == "" || classpathSnapshotProv.ClasspathSnapshotID != classpathSnapshot.Snapshot.Snapshot.ID || len(classpathSnapshotProv.Provenance.Variants) == 0 {
		t.Fatalf("unexpected classpath snapshot provenance result: %#v", classpathSnapshotProv)
	}
	if len(classpathSnapshotProv.Provenance.Artifacts) == 0 || len(classpathSnapshotProv.Provenance.ManifestPaths) == 0 {
		t.Fatalf("expected artifact and manifest context in classpath snapshot provenance, got %#v", classpathSnapshotProv.Provenance)
	}
	classpathSnapshotConsumers, err := svc.ClasspathSnapshotConsumers(context.Background(), prj, classpathSnapshot.Snapshot.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if classpathSnapshotConsumers.ModelCacheKey == "" || classpathSnapshotConsumers.ClasspathSnapshotID != classpathSnapshot.Snapshot.Snapshot.ID || len(classpathSnapshotConsumers.Consumers.Actions) == 0 {
		t.Fatalf("unexpected classpath snapshot consumers result: %#v", classpathSnapshotConsumers)
	}
	if len(classpathSnapshotConsumers.Consumers.Artifacts) == 0 || len(classpathSnapshotConsumers.Consumers.ManifestPaths) == 0 {
		t.Fatalf("expected artifact and manifest context in classpath snapshot consumers, got %#v", classpathSnapshotConsumers.Consumers)
	}
	classpathLookup, err := svc.ClasspathEntryLookup(context.Background(), prj, ":app", debugVariant.Name, debugVariant.Materialization.SourceRoots[0])
	if err != nil {
		t.Fatal(err)
	}
	if classpathLookup.ModelCacheKey == "" || classpathLookup.Lookup.Entry.Path == "" || len(classpathLookup.Lookup.Decisions) == 0 {
		t.Fatalf("unexpected classpath entry lookup result: %#v", classpathLookup)
	}
	classpathPathConsumers, err := svc.ClasspathPathConsumers(context.Background(), prj, debugVariant.Materialization.SourceRoots[0])
	if err != nil {
		t.Fatal(err)
	}
	if classpathPathConsumers.ModelCacheKey == "" || classpathPathConsumers.Consumers.Path == "" || len(classpathPathConsumers.Consumers.Consumers) == 0 {
		t.Fatalf("unexpected classpath path consumers result: %#v", classpathPathConsumers)
	}
	sourceArtifactID := ""
	for _, artifact := range classpathProv.Provenance.Artifacts {
		if artifact.Path == debugVariant.Materialization.SourceRoots[0] {
			sourceArtifactID = artifact.ID.String()
			break
		}
	}
	if sourceArtifactID == "" {
		t.Fatalf("expected source artifact id for classpath-backed source root, got %#v", classpathProv.Provenance.Artifacts)
	}
	artifactOnClasspath, err := svc.ArtifactOnClasspath(context.Background(), prj, ":app", debugVariant.Name, sourceArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if artifactOnClasspath.ModelCacheKey == "" || artifactOnClasspath.Lookup.Artifact.ID.String() == "" || !artifactOnClasspath.Lookup.Present {
		t.Fatalf("unexpected artifact-on-classpath result: %#v", artifactOnClasspath)
	}
	artifactClasspathConsumers, err := svc.ArtifactClasspathConsumers(context.Background(), prj, sourceArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if artifactClasspathConsumers.ModelCacheKey == "" || artifactClasspathConsumers.Consumers.Artifact.ID.String() == "" || len(artifactClasspathConsumers.Consumers.Consumers) == 0 {
		t.Fatalf("unexpected artifact classpath consumers result: %#v", artifactClasspathConsumers)
	}
	fileOwners, err := svc.FileOwners(context.Background(), prj, filepath.Join(root, "app", "src", "main", "AndroidManifest.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if fileOwners.ModelCacheKey == "" || fileOwners.Owners.Path == "" || len(fileOwners.Owners.Owners) == 0 {
		t.Fatalf("unexpected file owners result: %#v", fileOwners)
	}
	moduleImpact, err := svc.ModuleImpact(context.Background(), prj, ":app")
	if err != nil {
		t.Fatal(err)
	}
	if moduleImpact.ModelCacheKey == "" || moduleImpact.Module != ":app" {
		t.Fatalf("unexpected module impact: %#v", moduleImpact)
	}
	capabilities, err := svc.AndroidCapabilities(context.Background(), prj, ":app")
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Repo != root || capabilities.Module != ":app" || len(capabilities.Variants) != 2 {
		t.Fatalf("unexpected android capability report: %#v", capabilities)
	}
	var debugCapability AndroidCapabilityVariantResult
	foundDebugCapability := false
	for _, candidate := range capabilities.Variants {
		if candidate.Name == "debug" {
			debugCapability = candidate
			foundDebugCapability = true
			break
		}
	}
	if !foundDebugCapability {
		t.Fatalf("expected debug capability variant, got %#v", capabilities.Variants)
	}
	if !debugCapability.Installable || !debugCapability.Testable || !debugCapability.Debuggable || !debugCapability.SigningConfigured {
		t.Fatalf("expected resolved android capability metadata, got %#v", debugCapability)
	}
	if debugCapability.VersionCode != "42" || debugCapability.MinSDK != "24" || debugCapability.TargetSDK != "34" {
		t.Fatalf("expected version/sdk metadata, got %#v", debugCapability)
	}
	if debugCapability.CompileSDK != "34" || debugCapability.BuildToolsVersion != "34.0.0" || len(debugCapability.ConsumerProguardFiles) != 1 {
		t.Fatalf("expected compile/proguard-consumer metadata, got %#v", debugCapability)
	}
	if debugCapability.VersionName != "" {
		t.Fatalf("expected bounded version name metadata, got %#v", debugCapability)
	}
	if debugCapability.Namespace != "com.example.app" || debugCapability.TestInstrumentationRunner != "androidx.test.runner.AndroidJUnitRunner" {
		t.Fatalf("expected namespace and test runner metadata, got %#v", debugCapability)
	}
	if debugCapability.DexMode != "d8" {
		t.Fatalf("expected dex mode d8 for debug variant, got %q", debugCapability.DexMode)
	}
	if debugCapability.Optimization.MinifyEnabled || debugCapability.Optimization.ShrinkResources || len(debugCapability.ProguardFiles) != 0 {
		t.Fatalf("expected bounded optimization/proguard metadata, got %#v", debugCapability)
	}
	if len(debugCapability.ManifestPaths) == 0 || debugCapability.MaterializationID == "" || debugCapability.ArtifactSnapshotID == "" {
		t.Fatalf("expected manifest and materialization metadata, got %#v", debugCapability)
	}
	if debugCapability.BackingArtifactID == "" {
		t.Fatalf("expected backing artifact metadata, got %#v", debugCapability)
	}
	if len(debugCapability.ProducedArtifactKinds) == 0 || debugCapability.InstallArtifactID == "" {
		t.Fatalf("expected produced-artifact classification metadata, got %#v", debugCapability)
	}
	if len(debugCapability.ProducedArtifactPaths) == 0 || debugCapability.BackingArtifactPath == "" {
		t.Fatalf("expected produced-artifact path metadata, got %#v", debugCapability)
	}
	if debugCapability.SigningStoreFile == "" || debugCapability.SigningKeyAlias == "" || !debugCapability.HasStorePassword || !debugCapability.HasKeyPassword {
		t.Fatalf("expected signing provenance metadata, got %#v", debugCapability)
	}
	if debugCapability.InstallTask != "installDebug" || debugCapability.UninstallTask != "uninstallDebug" {
		t.Fatalf("expected install/uninstall task metadata, got %#v", debugCapability)
	}
	if debugCapability.AndroidTestPackage != "com.example.app.test" {
		t.Fatalf("unexpected androidTest package name, got %#v", debugCapability)
	}
	if debugCapability.AndroidTestManifest == "" || !strings.Contains(debugCapability.AndroidTestManifest, "debugAndroidTest") {
		t.Fatalf("expected androidTest manifest path, got %#v", debugCapability.AndroidTestManifest)
	}
	if debugCapability.AndroidTestInstallTask != "installDebugAndroidTest" || debugCapability.AndroidTestUninstallTask != "uninstallDebugAndroidTest" {
		t.Fatalf("unexpected androidTest task aliases, got %#v", debugCapability)
	}
	var releaseCapability AndroidCapabilityVariantResult
	for _, candidate := range capabilities.Variants {
		if candidate.Name == "release" {
			releaseCapability = candidate
			break
		}
	}
	if releaseCapability.DexMode != "r8" {
		t.Fatalf("expected dex mode r8 for release variant, got %q", releaseCapability.DexMode)
	}
	if !releaseCapability.MinifyEnabled {
		t.Fatalf("expected minify enabled for release variant, got %#v", releaseCapability)
	}
	variantMat, err := svc.VariantMaterialization(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if variantMat.ModelCacheKey == "" || variantMat.Provenance.Materialization.MaterializationID != string(debugVariant.Materialization.ID) {
		t.Fatalf("unexpected variant materialization result: %#v", variantMat)
	}
	if len(variantMat.Provenance.Materialization.ManifestPaths) == 0 || len(variantMat.Provenance.Actions) == 0 || len(variantMat.Provenance.Artifacts) == 0 {
		t.Fatalf("expected manifest/action/artifact data in variant materialization, got %#v", variantMat.Provenance)
	}
	materializationProv, err := svc.MaterializationProvenance(context.Background(), prj, string(debugVariant.Materialization.ID))
	if err != nil {
		t.Fatal(err)
	}
	if materializationProv.ModelCacheKey == "" || materializationProv.Provenance.Materialization.ID.String() != string(debugVariant.Materialization.ID) || len(materializationProv.Provenance.Artifacts) == 0 {
		t.Fatalf("unexpected materialization provenance result: %#v", materializationProv)
	}
	variantCompatibility, err := svc.VariantCompatibility(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if variantCompatibility.ModelCacheKey == "" || variantCompatibility.ModulePath != ":app" || variantCompatibility.VariantName != debugVariant.Name {
		t.Fatalf("unexpected variant compatibility result: %#v", variantCompatibility)
	}
	if variantCompatibility.VariantID != string(debugVariant.ID) || variantCompatibility.MaterializationID != string(debugVariant.Materialization.ID) || variantCompatibility.ArtifactSnapshotID == "" {
		t.Fatalf("expected ids in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.VersionCode != "42" || variantCompatibility.MinSDK != "24" || variantCompatibility.TargetSDK != "34" {
		t.Fatalf("expected version/sdk metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.CompileSDK != "34" || variantCompatibility.BuildToolsVersion != "34.0.0" || len(variantCompatibility.ConsumerProguardFiles) != 1 {
		t.Fatalf("expected compile/proguard-consumer metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.VersionName != "" {
		t.Fatalf("expected bounded version name metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.Compatibility.DisplayName != debugVariant.DisplayName || len(variantCompatibility.Compatibility.SourceSetOrder) == 0 || len(variantCompatibility.Compatibility.SourceSetNames) == 0 || len(variantCompatibility.Compatibility.TaskAliases) == 0 || len(variantCompatibility.Compatibility.ModelSelectors) == 0 || len(variantCompatibility.Compatibility.SyncFragments) == 0 {
		t.Fatalf("unexpected variant compatibility metadata: %#v", variantCompatibility.Compatibility)
	}
	if variantCompatibility.Materialization.ID != string(debugVariant.Materialization.ID) || len(variantCompatibility.Provenance.ManifestPaths) == 0 {
		t.Fatalf("expected materialization and provenance in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.Namespace != "com.example.app" || variantCompatibility.TestInstrumentationRunner != "androidx.test.runner.AndroidJUnitRunner" {
		t.Fatalf("expected namespace/test runner metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.BackingArtifactPath == "" {
		t.Fatalf("expected backing artifact path metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.DexMode != "d8" {
		t.Fatalf("expected dex mode d8 in variant compatibility for debug, got %q", variantCompatibility.DexMode)
	}
	if variantCompatibility.Optimization.MinifyEnabled || variantCompatibility.Optimization.ShrinkResources || len(variantCompatibility.ProguardFiles) != 0 {
		t.Fatalf("expected bounded optimization/proguard metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if len(variantCompatibility.ProducedArtifactKinds) == 0 || variantCompatibility.InstallArtifactID == "" {
		t.Fatalf("expected produced-artifact classification metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if len(variantCompatibility.ProducedArtifactPaths) == 0 || variantCompatibility.BackingArtifactPath == "" {
		t.Fatalf("expected produced-artifact path metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.SigningStoreFile == "" || variantCompatibility.SigningKeyAlias == "" || !variantCompatibility.HasStorePassword || !variantCompatibility.HasKeyPassword {
		t.Fatalf("expected signing provenance metadata in variant compatibility, got %#v", variantCompatibility)
	}
	if variantCompatibility.InstallTask != "installDebug" || variantCompatibility.UninstallTask != "uninstallDebug" {
		t.Fatalf("expected install/uninstall task metadata in variant compatibility, got %#v", variantCompatibility)
	}
	variantManifest, err := svc.VariantManifest(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if variantManifest.ModelCacheKey == "" || variantManifest.Manifest.ModulePath != ":app" || variantManifest.Manifest.VariantName != debugVariant.Name {
		t.Fatalf("unexpected variant manifest result: %#v", variantManifest)
	}
	if variantManifest.Manifest.MaterializationID != string(debugVariant.Materialization.ID) || variantManifest.Manifest.ArtifactSnapshotID == "" {
		t.Fatalf("expected ids in variant manifest, got %#v", variantManifest.Manifest)
	}
	if len(variantManifest.Manifest.ManifestPaths) == 0 || len(variantManifest.Manifest.SourceRoots) == 0 {
		t.Fatalf("expected manifest paths and source roots in variant manifest, got %#v", variantManifest.Manifest)
	}
	if len(variantManifest.Manifest.ClasspathSnapshots) == 0 || variantManifest.Manifest.ClasspathSnapshots[0].ID == "" {
		t.Fatalf("expected classpath snapshots in variant manifest, got %#v", variantManifest.Manifest.ClasspathSnapshots)
	}
	if len(variantManifest.Manifest.ActionIDs) == 0 || len(variantManifest.Manifest.Actions) == 0 {
		t.Fatalf("expected action ids and actions in variant manifest, got %#v", variantManifest.Manifest)
	}
	if len(variantManifest.Manifest.ProducedArtifactIDs) == 0 || len(variantManifest.Manifest.ProducedArtifacts) == 0 {
		t.Fatalf("expected produced artifacts in variant manifest, got %#v", variantManifest.Manifest)
	}
	if variantManifest.Manifest.BackingArtifact == nil || variantManifest.Manifest.BackingArtifact.ID == "" {
		t.Fatalf("expected backing artifact in variant manifest, got %#v", variantManifest.Manifest)
	}
	artifactsForVariant, err := svc.ArtifactsForVariant(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if artifactsForVariant.ModelCacheKey == "" || artifactsForVariant.Module != ":app" || artifactsForVariant.Variant != debugVariant.Name {
		t.Fatalf("unexpected artifacts for variant result: %#v", artifactsForVariant)
	}
	if artifactsForVariant.MaterializationID != string(debugVariant.Materialization.ID) || artifactsForVariant.ArtifactSnapshotID == "" {
		t.Fatalf("expected ids in artifacts for variant, got %#v", artifactsForVariant)
	}
	if len(artifactsForVariant.Artifacts) == 0 {
		t.Fatalf("expected artifact summaries in artifacts for variant, got %#v", artifactsForVariant.Artifacts)
	}
	snapshotProv, err := svc.ArtifactSnapshotProvenance(context.Background(), prj, debugVariant.Materialization.ArtifactSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshotProv.ModelCacheKey == "" || snapshotProv.Provenance.ArtifactSnapshotID != debugVariant.Materialization.ArtifactSnapshotID {
		t.Fatalf("unexpected artifact snapshot provenance result: %#v", snapshotProv)
	}
	if len(snapshotProv.Provenance.Variants) == 0 || len(snapshotProv.Provenance.Artifacts) == 0 || len(snapshotProv.Provenance.ManifestPaths) == 0 {
		t.Fatalf("expected snapshot provenance data, got %#v", snapshotProv.Provenance)
	}
	snapshotConsumers, err := svc.ArtifactSnapshotConsumers(context.Background(), prj, debugVariant.Materialization.ArtifactSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshotConsumers.ModelCacheKey == "" || snapshotConsumers.Consumers.ArtifactSnapshotID != debugVariant.Materialization.ArtifactSnapshotID || len(snapshotConsumers.Consumers.Actions) == 0 {
		t.Fatalf("unexpected artifact snapshot consumers result: %#v", snapshotConsumers)
	}
	impact, err := svc.VariantImpact(context.Background(), prj, ":app", debugVariant.Name)
	if err != nil {
		t.Fatal(err)
	}
	if impact.ModelCacheKey == "" {
		t.Fatalf("expected model cache key in impact result, got %#v", impact)
	}

	cleanup, err := svc.CleanupPlan(context.Background(), prj)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup.ModelCacheKey == "" || cleanup.Plan.ModelCacheKey == "" {
		t.Fatalf("expected cleanup plan cache keys, got %#v", cleanup)
	}
	if len(cleanup.Plan.ClassPlans) == 0 || len(cleanup.Plan.Records) == 0 {
		t.Fatalf("expected dry-run cleanup accounting, got %#v", cleanup.Plan)
	}
	if cleanup.Plan.Totals["protected"] == 0 {
		t.Fatalf("expected protected cleanup records, got %#v", cleanup.Plan.Totals)
	}
	foundReasonCode := false
	foundClassWarnings := false
	for _, record := range cleanup.Plan.Records {
		if record.ReasonCode != "" {
			foundReasonCode = true
			break
		}
	}
	for _, classPlan := range cleanup.Plan.ClassPlans {
		if len(classPlan.Warnings) != 0 {
			foundClassWarnings = true
			break
		}
	}
	if !foundReasonCode {
		t.Fatalf("expected cleanup records to expose structured reason codes, got %#v", cleanup.Plan.Records)
	}
	if !foundClassWarnings {
		t.Fatalf("expected cleanup class plans to expose structured warnings, got %#v", cleanup.Plan.ClassPlans)
	}

	runSummaryPath := filepath.Join(root, "build", "grit", "run-summaries", "_app")
	if err := os.MkdirAll(runSummaryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	summaryPayload, err := json.Marshal(RunSummaryRecord{
		Command:    "assemble",
		ModulePath: ":app",
		Success:    true,
		Variant:    "debug",
		RunGraphSummary: &RunGraphSummary{
			ModuleID:           "module:app",
			VariantID:          "variant:debug",
			MaterializationID:  "materialization:debug",
			ArtifactSnapshotID: "snapshot:debug",
			PlannedActionIDs:   []string{"action:compile"},
			RootActionIDs:      []string{"action:compile"},
			ExecutedActionIDs:  []string{"action:compile"},
		},
		CriticalPathSummary: &CriticalPathSummary{
			BatchCount:           1,
			EstimatedDurationMs:  12,
			RepresentativeAction: []string{"action:compile"},
		},
		SchedulerSummary: &SchedulerSummary{
			ExecutedBatchCount:  1,
			CriticalPathActions: 1,
			QueueWaitActions:    1,
			TotalQueueWaitMs:    7,
			MaxQueueWaitMs:      7,
			WaitReasonCounts:    map[string]int{"worker-slot": 1},
			CacheResultCounts:   map[string]int{"reused": 1},
			WorkerClasses: []SchedulerBreakdownBucket{{
				Key:               "jvm-compile",
				ActionCount:       1,
				CriticalPathCount: 1,
				QueueWaitActions:  1,
				TotalQueueWaitMs:  7,
				MaxQueueWaitMs:    7,
				WaitReasonCounts:  map[string]int{"worker-slot": 1},
				CacheResultCounts: map[string]int{"reused": 1},
			}},
			ResourceClasses: []SchedulerBreakdownBucket{{
				Key:               "cpu",
				ActionCount:       1,
				CriticalPathCount: 1,
				QueueWaitActions:  1,
				TotalQueueWaitMs:  7,
				MaxQueueWaitMs:    7,
				WaitReasonCounts:  map[string]int{"worker-slot": 1},
				CacheResultCounts: map[string]int{"reused": 1},
			}},
		},
		PlannedSchedule: &PlanScheduleResult{
			ResourceBudgets: []PlanResourceBudget{{ResourceClass: "cpu", Capacity: 1}},
			Batches: []PlanScheduleBatch{{
				Actions: []InspectPlannedAction{{ID: "action:compile", Name: "compileKotlin", Operation: "compile", ModulePath: ":app", VariantName: "debug"}},
			}},
		},
		ActionExecutions: []ActionExecution{{
			ActionID:       "action:compile",
			Name:           "compileKotlin",
			Operation:      "compile",
			ModulePath:     ":app",
			VariantName:    "debug",
			BatchIndex:     0,
			CriticalPath:   true,
			QueueWaitMs:    7,
			WaitReason:     "worker-slot",
			WorkerClass:    "jvm-compile",
			ResourceClass:  "cpu",
			ResourceCost:   2,
			MaxParallelism: 1,
			CacheKey:       "compile-key",
			Cacheable:      true,
			ProbeOrder:     []string{"local", "shared"},
			ExecuteOnMiss:  true,
			RetentionClass: "machine-shareable",
			Shareability:   "machine",
			Status:         "reused",
			Timings: perf.List([]perf.TimingEntry{{
				Name:       "compile",
				DurationMs: 5,
				Children: perf.List([]perf.TimingEntry{{
					Name:       "javac",
					DurationMs: 3,
				}}),
			}}),
		}},
		ActionExplanations: []explain.Action{{
			ActionID:  "action:compile",
			Name:      "compileKotlin",
			Operation: "compile",
			Cache: &explain.Timing{
				State: explain.StateReused,
				Basis: "test",
			},
		}},
		CacheProbes: []responsepayload.CacheProbe{{
			ActionID: "action:compile",
			State:    "reused",
			Basis:    "test",
		}},
		CacheProbeRecords: []responsepayload.CacheProbeRecord{{
			ActionID: "action:compile",
			StepName: "compileKotlin",
			State:    "reused",
			Basis:    "test",
		}},
		CacheSummary: &CacheSummary{
			TotalActions:   2,
			ReusedActions:  1,
			RebuiltActions: 1,
		},
		DiagnosticSummary: &DiagnosticSummary{
			Total:               1,
			BySeverity:          []DiagnosticSummaryBucket{{Key: "error", Count: 1}},
			ByCode:              []DiagnosticSummaryBucket{{Key: "compile_failure", Count: 1}},
			ByCategory:          []DiagnosticSummaryBucket{{Key: "compile", Count: 1}},
			ByTool:              []DiagnosticSummaryBucket{{Key: "compile", Count: 1}},
			ByOrigin:            []DiagnosticSummaryBucket{{Key: "tool", Count: 1}},
			BySource:            []DiagnosticSummaryBucket{{Key: "tool-emitted", Count: 1}},
			ByStream:            []DiagnosticSummaryBucket{{Key: "stderr", Count: 1}},
			ByOperation:         []DiagnosticSummaryBucket{{Key: "compile", Count: 1}},
			ByWorkerClass:       []DiagnosticSummaryBucket{{Key: "jvm-compile", Count: 1}},
			ByFile:              []DiagnosticSummaryBucket{{Key: "/repo/app/src/main/java/App.kt", Count: 1}},
			ByRelatedDependency: []DiagnosticSummaryBucket{{Key: "com.squareup.okhttp3:okhttp:4.12.0", Count: 1}},
		},
		Diagnostics: []DiagnosticRecord{{
			ActionID:          "action:compile",
			ModulePath:        ":app",
			VariantName:       "debug",
			Tool:              "compile",
			Operation:         "compile",
			WorkerClass:       "jvm-compile",
			Origin:            "tool",
			SourceKind:        "tool-emitted",
			Stream:            "stderr",
			Severity:          "error",
			Code:              "compile_failure",
			Category:          "compile",
			Message:           "compilation failed",
			File:              "/repo/app/src/main/java/App.kt",
			Line:              12,
			Column:            4,
			RelatedDependency: "com.squareup.okhttp3:okhttp:4.12.0",
		}},
		Materializations: []project.SemanticMaterializationSummary{{
			ID:                 "materialization:debug",
			ArtifactSnapshotID: "snapshot:debug",
			SourceRoots:        []string{"/repo/app/src/main"},
		}},
		PerfTiming: perf.List([]perf.TimingEntry{{Name: "total", DurationMs: 12}}),
		WrittenAt:  "2026-04-09T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runSummaryPath, "assemble.json"), summaryPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	runSummary, err := svc.RunSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if runSummary.Path == "" || runSummary.Summary.Command != "assemble" || runSummary.Summary.CacheSummary == nil || runSummary.Summary.CacheSummary.ReusedActions != 1 {
		t.Fatalf("unexpected run summary result: %#v", runSummary)
	}
	runGraphSummary, err := svc.RunGraphSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if runGraphSummary.Summary.MaterializationID != "materialization:debug" || len(runGraphSummary.Summary.RootActionIDs) != 1 {
		t.Fatalf("unexpected run graph summary result: %#v", runGraphSummary)
	}
	criticalPathSummary, err := svc.CriticalPathSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if criticalPathSummary.Summary.BatchCount != 1 || len(criticalPathSummary.Summary.RepresentativeAction) != 1 {
		t.Fatalf("unexpected critical path summary result: %#v", criticalPathSummary)
	}
	schedulerSummary, err := svc.SchedulerSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if schedulerSummary.Summary.QueueWaitActions != 1 || schedulerSummary.Summary.WaitReasonCounts["worker-slot"] != 1 {
		t.Fatalf("unexpected scheduler summary result: %#v", schedulerSummary)
	}
	if schedulerSummary.Summary.CacheResultCounts["reused"] != 1 || len(schedulerSummary.Summary.WorkerClasses) != 1 || schedulerSummary.Summary.WorkerClasses[0].Key != "jvm-compile" || schedulerSummary.Summary.ResourceClasses[0].Key != "cpu" {
		t.Fatalf("expected richer scheduler breakdowns, got %#v", schedulerSummary.Summary)
	}
	cacheSummary, err := svc.CacheSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if cacheSummary.Summary.ReusedActions != 1 || cacheSummary.Summary.TotalActions != 2 {
		t.Fatalf("unexpected cache summary result: %#v", cacheSummary)
	}
	toolSummary, err := svc.ToolSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(toolSummary.Summary.Operations) != 1 || toolSummary.Summary.Operations[0].Key != "compile" || toolSummary.Summary.Operations[0].ActionCount != 1 {
		t.Fatalf("unexpected tool summary operations result: %#v", toolSummary)
	}
	if len(toolSummary.Summary.WorkerClasses) != 1 || toolSummary.Summary.WorkerClasses[0].Key != "jvm-compile" {
		t.Fatalf("unexpected tool summary worker classes result: %#v", toolSummary)
	}
	if len(toolSummary.Summary.ResourceClasses) != 1 || toolSummary.Summary.ResourceClasses[0].Key != "cpu" {
		t.Fatalf("unexpected tool summary resource classes result: %#v", toolSummary)
	}
	diagnostics, err := svc.Diagnostics(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics.Diagnostics) != 1 || diagnostics.Diagnostics[0].Code != "compile_failure" || diagnostics.Diagnostics[0].File == "" {
		t.Fatalf("unexpected diagnostics result: %#v", diagnostics)
	}
	if diagnostics.Diagnostics[0].Origin != "tool" || diagnostics.Diagnostics[0].Fingerprint == "" || diagnostics.Diagnostics[0].SourceKind != "tool-emitted" || diagnostics.Diagnostics[0].Stream != "stderr" || diagnostics.Diagnostics[0].RelatedDependency == "" {
		t.Fatalf("expected richer diagnostics result: %#v", diagnostics.Diagnostics[0])
	}
	diagnosticSummary, err := svc.DiagnosticSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if diagnosticSummary.Summary.Total != 1 || len(diagnosticSummary.Summary.BySeverity) != 1 || diagnosticSummary.Summary.BySeverity[0].Key != "error" {
		t.Fatalf("unexpected diagnostic summary result: %#v", diagnosticSummary)
	}
	if len(diagnosticSummary.Summary.ByOrigin) != 1 || diagnosticSummary.Summary.ByOrigin[0].Key != "tool" || len(diagnosticSummary.Summary.BySource) != 1 || diagnosticSummary.Summary.BySource[0].Key != "tool-emitted" || len(diagnosticSummary.Summary.ByFile) != 1 {
		t.Fatalf("unexpected diagnostic source summary result: %#v", diagnosticSummary)
	}
	plannedSchedule, err := svc.PlannedSchedule(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(plannedSchedule.Summary.ResourceBudgets) != 1 || len(plannedSchedule.Summary.Batches) != 1 {
		t.Fatalf("unexpected planned schedule result: %#v", plannedSchedule)
	}
	scheduleDrift, err := svc.ScheduleDrift(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if scheduleDrift.Summary.PlannedActionCount != 1 || scheduleDrift.Summary.ExecutedActionCount != 1 || scheduleDrift.Summary.MatchedActionCount != 1 || scheduleDrift.Summary.QueueWaitActions != 1 || scheduleDrift.Summary.CriticalPathActions != 1 {
		t.Fatalf("unexpected schedule drift result: %#v", scheduleDrift)
	}
	if len(scheduleDrift.Summary.Actions) != 1 || scheduleDrift.Summary.Actions[0].ActionID != "action:compile" || !scheduleDrift.Summary.Actions[0].Planned || !scheduleDrift.Summary.Actions[0].Executed || scheduleDrift.Summary.Actions[0].QueueWaitMs != 7 {
		t.Fatalf("unexpected schedule drift actions: %#v", scheduleDrift.Summary.Actions)
	}
	materializations, err := svc.Materializations(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(materializations.Materializations) != 1 || materializations.Materializations[0].ID != "materialization:debug" {
		t.Fatalf("unexpected materializations result: %#v", materializations)
	}
	actionExecution, err := svc.ActionExecution(context.Background(), prj, ":app", "assemble", "action:compile")
	if err != nil {
		t.Fatal(err)
	}
	if actionExecution.Execution.ActionID != "action:compile" || actionExecution.Execution.WaitReason != "worker-slot" || actionExecution.Explain == nil {
		t.Fatalf("unexpected action execution result: %#v", actionExecution)
	}
	actionExecutions, err := svc.ActionExecutions(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(actionExecutions.Executions) != 1 || actionExecutions.Executions[0].ActionID != "action:compile" {
		t.Fatalf("unexpected action executions result: %#v", actionExecutions)
	}
	actionExplanation, err := svc.ActionExplanation(context.Background(), prj, ":app", "assemble", "action:compile")
	if err != nil {
		t.Fatal(err)
	}
	if actionExplanation.Explain.ActionID != "action:compile" || actionExplanation.Execution == nil || actionExplanation.Execution.Status != "reused" {
		t.Fatalf("unexpected action explanation result: %#v", actionExplanation)
	}
	actionExplanations, err := svc.ActionExplanations(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(actionExplanations.Explanations) != 1 || actionExplanations.Explanations[0].ActionID != "action:compile" {
		t.Fatalf("unexpected action explanations result: %#v", actionExplanations)
	}
	cacheProbes, err := svc.CacheProbes(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(cacheProbes.Probes) != 1 || cacheProbes.Probes[0].ActionID != "action:compile" {
		t.Fatalf("unexpected cache probes result: %#v", cacheProbes)
	}
	cacheProbeRecords, err := svc.CacheProbeRecords(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(cacheProbeRecords.Records) != 1 || cacheProbeRecords.Records[0].StepName != "compileKotlin" {
		t.Fatalf("unexpected cache probe records result: %#v", cacheProbeRecords)
	}
	reuseDecision, err := svc.ReuseDecision(context.Background(), prj, ":app", "assemble", "action:compile")
	if err != nil {
		t.Fatal(err)
	}
	if reuseDecision.Decision.ActionID != "action:compile" || reuseDecision.Decision.CacheOutcome != "reused" || reuseDecision.Decision.CacheSource != "summary-cache-probe" {
		t.Fatalf("unexpected reuse decision result: %#v", reuseDecision)
	}
	if len(reuseDecision.Decision.Basis) != 1 || reuseDecision.Decision.Basis[0] != "test" || len(reuseDecision.Decision.ProbeRecords) != 1 {
		t.Fatalf("unexpected reuse decision details: %#v", reuseDecision.Decision)
	}
	reuseDecisions, err := svc.ReuseDecisions(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(reuseDecisions.Decisions) != 1 || reuseDecisions.Decisions[0].ActionID != "action:compile" || reuseDecisions.Decisions[0].CacheOutcome != "reused" {
		t.Fatalf("unexpected reuse decisions result: %#v", reuseDecisions)
	}
	actionTrace, err := svc.ActionTrace(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(actionTrace.Actions) != 1 || actionTrace.Actions[0].ActionID != "action:compile" || len(actionTrace.Actions[0].Timings) != 1 {
		t.Fatalf("unexpected action trace result: %#v", actionTrace)
	}
	if actionTrace.Actions[0].CacheResult != string(explain.StateReused) || actionTrace.Actions[0].WaitReason != "worker-slot" {
		t.Fatalf("unexpected action trace details: %#v", actionTrace.Actions[0])
	}
	if actionTrace.Actions[0].ResourceClass != "cpu" || actionTrace.Actions[0].ResourceCost != 2 || actionTrace.Actions[0].MaxParallelism != 1 || actionTrace.Actions[0].CacheKey != "compile-key" || !actionTrace.Actions[0].Cacheable || !actionTrace.Actions[0].ExecuteOnMiss || actionTrace.Actions[0].RetentionClass != "machine-shareable" || actionTrace.Actions[0].Shareability != "machine" || len(actionTrace.Actions[0].ProbeOrder) != 2 {
		t.Fatalf("expected richer action trace policy details: %#v", actionTrace.Actions[0])
	}
	if len(actionTrace.Actions[0].Substeps) != 2 || actionTrace.Actions[0].Substeps[1].Name != "javac" || actionTrace.Actions[0].Substeps[1].Depth != 1 {
		t.Fatalf("unexpected action trace substeps: %#v", actionTrace.Actions[0].Substeps)
	}
	perfTiming, err := svc.PerfTiming(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if perfTiming.Timing == nil || len(perfTiming.Timing.Entries()) != 1 || perfTiming.Timing.Entries()[0].DurationMs != 12 {
		t.Fatalf("unexpected perf timing result: %#v", perfTiming)
	}
	runSummaries, err := svc.RunSummaries(context.Background(), prj, ":app")
	if err != nil {
		t.Fatal(err)
	}
	if len(runSummaries.Entries) != 1 || runSummaries.Entries[0].Command != "assemble" || runSummaries.Entries[0].ModulePath != ":app" {
		t.Fatalf("unexpected run summaries index: %#v", runSummaries)
	}

	driftPayload, err := json.Marshal(RunSummaryRecord{
		Command:    "assembleDrift",
		ModulePath: ":app",
		Success:    false,
		RunGraphSummary: &RunGraphSummary{
			PlannedActionIDs:  []string{"action:compile", "action:package"},
			RootActionIDs:     []string{"action:package"},
			ExecutedActionIDs: []string{"action:compile", "action:lint"},
		},
		CriticalPathSummary: &CriticalPathSummary{
			BatchCount:           2,
			EstimatedDurationMs:  21,
			RepresentativeAction: []string{"action:compile"},
		},
		SchedulerSummary: &SchedulerSummary{
			ExecutedBatchCount:  2,
			CriticalPathActions: 1,
			QueueWaitActions:    1,
			MaxQueueWaitMs:      11,
			WaitReasonCounts:    map[string]int{"resource-lock": 1},
		},
		PlannedSchedule: &PlanScheduleResult{
			Batches: []PlanScheduleBatch{
				{Actions: []InspectPlannedAction{{ID: "action:compile", Name: "compileDebug", Operation: "compile", ModulePath: ":app", VariantName: "debug"}}},
				{Actions: []InspectPlannedAction{{ID: "action:package", Name: "packageDebug", Operation: "package", ModulePath: ":app", VariantName: "debug"}}},
			},
		},
		ActionExecutions: []ActionExecution{
			{ActionID: "action:compile", Name: "compileDebug", Operation: "compile", ModulePath: ":app", VariantName: "debug", BatchIndex: 1, CriticalPath: true, QueueWaitMs: 11, WaitReason: "resource-lock", Status: "executed"},
			{ActionID: "action:lint", Name: "lintDebug", Operation: "lint", ModulePath: ":app", VariantName: "debug", BatchIndex: 0, Status: "executed"},
		},
		WrittenAt: "2026-04-09T01:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runSummaryPath, "assembleDrift.json"), driftPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	driftResult, err := svc.ScheduleDrift(context.Background(), prj, ":app", "assembleDrift")
	if err != nil {
		t.Fatal(err)
	}
	if driftResult.Summary.PlannedOnlyCount != 1 || driftResult.Summary.ExecutedOnlyCount != 1 || driftResult.Summary.BatchMismatchCount != 1 {
		t.Fatalf("unexpected drift mismatch counts: %#v", driftResult.Summary)
	}
	if len(driftResult.Summary.PlannedOnlyActionIDs) != 1 || driftResult.Summary.PlannedOnlyActionIDs[0] != "action:package" || len(driftResult.Summary.ExecutedOnlyActionIDs) != 1 || driftResult.Summary.ExecutedOnlyActionIDs[0] != "action:lint" || len(driftResult.Summary.BatchMismatchActionIDs) != 1 || driftResult.Summary.BatchMismatchActionIDs[0] != "action:compile" {
		t.Fatalf("unexpected drift action ids: %#v", driftResult.Summary)
	}
}

func TestDiagnosticsReadbackNormalizesPersistedRecords(t *testing.T) {
	root := t.TempDir()
	runSummaryDir := filepath.Join(root, "build", "grit", "run-summaries", "_app")
	if err := os.MkdirAll(runSummaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prj := &project.Project{
		Name:    "demo",
		RootDir: root,
		Modules: []project.Module{{Path: ":app", Dir: filepath.Join(root, "app"), BuildFile: filepath.Join(root, "app", "build.gradle.kts"), Type: "android-application"}},
	}
	payload, err := json.Marshal(RunSummaryRecord{
		Command:    "assemble",
		ModulePath: ":app",
		Success:    false,
		Diagnostics: []DiagnosticRecord{
			{Ordinal: 9, ActionID: "action:b", Tool: "javac", Origin: "tool", SourceKind: "tool-emitted", Stream: "stderr", Severity: "error", Code: "javac_cannot_find_symbol", Category: "symbol-resolution", Message: "cannot find symbol", File: "/repo/B.kt", Line: 20},
			{Ordinal: 2, ActionID: "action:a", Tool: "kotlinc", Origin: "tool", SourceKind: "tool-emitted", Stream: "stderr", Severity: "warning", Code: "kotlinc_unused_symbol", Category: "unused-code", Message: "variable is never used", File: "/repo/A.kt", Line: 10},
		},
		DiagnosticSummary: &DiagnosticSummary{Total: 2},
		WrittenAt:         "2026-04-10T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runSummaryDir, "assemble.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	svc := New()
	diagnostics, err := svc.Diagnostics(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics.Diagnostics) != 2 || diagnostics.Diagnostics[0].ActionID != "action:a" || diagnostics.Diagnostics[0].Ordinal != 1 || diagnostics.Diagnostics[0].Fingerprint == "" {
		t.Fatalf("expected normalized diagnostics readback, got %#v", diagnostics.Diagnostics)
	}
	summary, err := svc.DiagnosticSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Summary.Total != 2 || len(summary.Summary.ByOrigin) != 1 || summary.Summary.ByOrigin[0].Key != "tool" {
		t.Fatalf("expected recomputed normalized summary, got %#v", summary.Summary)
	}
}

func TestToPlanScheduleResultPredictsDeferredRemoteProbes(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		NetworkBudgetConfig: &configmodel.ScheduleNetworkBudget{
			CapacityBytes:     100,
			RefillBytesPerSec: 0,
		},
		Batches: [][]configmodel.ActionScheduleStep{{
			{
				Action: graph.Action{
					ID:         "action:first",
					Name:       "compileDebugKotlin",
					Attributes: map[string]string{"operation": "compile", "modulePath": ":app", "variantName": "debug"},
				},
				WorkerClass:    "kotlin-compile",
				ResourceClass:  "cpu",
				ResourceCost:   1,
				MaxParallelism: 1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				ExecuteOnMiss:  true,
				EstimatedBytes: 80,
			},
			{
				Action: graph.Action{
					ID:         "action:second",
					Name:       "compileDebugJava",
					Attributes: map[string]string{"operation": "compile", "modulePath": ":app", "variantName": "debug"},
				},
				WorkerClass:    "javac",
				ResourceClass:  "cpu",
				ResourceCost:   1,
				MaxParallelism: 1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				ExecuteOnMiss:  true,
				EstimatedBytes: 80,
			},
		}},
	}

	result := toPlanScheduleResult(schedule)
	if len(result.Batches) != 1 || len(result.Batches[0].Actions) != 2 {
		t.Fatalf("unexpected planned batches: %#v", result)
	}
	first := result.Batches[0].Actions[0]
	second := result.Batches[0].Actions[1]
	if first.EstimatedBytes != 80 || second.EstimatedBytes != 80 {
		t.Fatalf("expected estimated bytes to be surfaced in plan schedule, got first=%#v second=%#v", first, second)
	}
	if first.DeferRemote {
		t.Fatalf("expected first action to keep remote probes enabled, got %#v", first)
	}
	if first.RemoteProbeAdmission == nil || first.RemoteProbeAdmission.Deferred || first.RemoteProbeAdmission.BudgetBeforeBytes != 100 || first.RemoteProbeAdmission.BudgetAfterBytes != 20 {
		t.Fatalf("expected first action admission details, got %#v", first.RemoteProbeAdmission)
	}
	if !second.DeferRemote {
		t.Fatalf("expected second action to predict deferred remote probes, got %#v", second)
	}
	if second.RemoteProbeAdmission == nil || !second.RemoteProbeAdmission.Deferred || second.RemoteProbeAdmission.BudgetBeforeBytes != 20 || second.RemoteProbeAdmission.BudgetAfterBytes != 20 {
		t.Fatalf("expected deferred action admission details, got %#v", second.RemoteProbeAdmission)
	}
}

func TestPlannedRemoteProbeDecisionsUsesStepsWithoutBatches(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		NetworkBudgetConfig: &configmodel.ScheduleNetworkBudget{
			CapacityBytes:     80,
			RefillBytesPerSec: 0,
		},
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 2}},
		Steps: []configmodel.ActionScheduleStep{
			{
				Action: graph.Action{
					ID:         "action:first",
					Name:       "compileDebugKotlin",
					Attributes: map[string]string{"operation": "compile", "modulePath": ":app", "variantName": "debug"},
				},
				WorkerClass:    "kotlin-compile",
				ResourceClass:  "cpu",
				ResourceCost:   1,
				MaxParallelism: 1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				ExecuteOnMiss:  true,
				EstimatedBytes: 80,
			},
			{
				Action: graph.Action{
					ID:         "action:second",
					Name:       "compileDebugJava",
					Attributes: map[string]string{"operation": "compile", "modulePath": ":app", "variantName": "debug"},
				},
				WorkerClass:    "javac",
				ResourceClass:  "cpu",
				ResourceCost:   1,
				MaxParallelism: 1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				ExecuteOnMiss:  true,
				EstimatedBytes: 80,
			},
		},
	}

	decisions := plannedRemoteProbeDecisions(schedule)
	if len(decisions) != 2 {
		t.Fatalf("expected decisions for steps-only schedule, got %#v", decisions)
	}
	if decisions["action:first"].DeferRemote {
		t.Fatalf("expected first step to keep remote probes enabled, got %#v", decisions)
	}
	if decisions["action:first"].BudgetBeforeBytes != 80 || decisions["action:first"].BudgetAfterBytes != 0 {
		t.Fatalf("expected first step budget details, got %#v", decisions["action:first"])
	}
	if !decisions["action:second"].DeferRemote {
		t.Fatalf("expected second step to defer remote probes once the budget is exhausted, got %#v", decisions)
	}
	if decisions["action:second"].BudgetBeforeBytes != 0 || decisions["action:second"].BudgetAfterBytes != 0 {
		t.Fatalf("expected deferred step budget details, got %#v", decisions["action:second"])
	}
}

func TestDiagnosticSummaryReadbackRecomputesFromRawDiagnostics(t *testing.T) {
	root := t.TempDir()
	runSummaryDir := filepath.Join(root, "build", "grit", "run-summaries", "_app")
	if err := os.MkdirAll(runSummaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prj := &project.Project{
		Name:    "demo",
		RootDir: root,
		Modules: []project.Module{{Path: ":app", Dir: filepath.Join(root, "app"), BuildFile: filepath.Join(root, "app", "build.gradle.kts"), Type: "android-application"}},
	}
	payload, err := json.Marshal(RunSummaryRecord{
		Command:    "assemble",
		ModulePath: ":app",
		Success:    false,
		Diagnostics: []DiagnosticRecord{
			{Ordinal: 8, ActionID: "action:compile", Tool: "kotlinc", Code: "kotlinc_unused_symbol", Category: "unused-code", RelatedDependency: "z:dep:1.0", Origin: "tool", SourceKind: "tool-emitted", Stream: "stderr", Severity: "warning", Message: "shared message", File: "/repo/App.kt", Line: 10},
			{Ordinal: 1, ActionID: "action:compile", Tool: "javac", Code: "javac_cannot_find_symbol", Category: "symbol-resolution", RelatedDependency: "a:dep:1.0", Origin: "tool", SourceKind: "tool-emitted", Stream: "stderr", Severity: "warning", Message: "shared message", File: "/repo/App.kt", Line: 10},
		},
		DiagnosticSummary: &DiagnosticSummary{
			Total:      1,
			BySeverity: []DiagnosticSummaryBucket{{Key: "error", Count: 1}},
		},
		WrittenAt: "2026-04-10T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runSummaryDir, "assemble.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	svc := New()
	diagnostics, err := svc.Diagnostics(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics.Diagnostics) != 2 || diagnostics.Diagnostics[0].Tool != "javac" || diagnostics.Diagnostics[0].Code != "javac_cannot_find_symbol" || diagnostics.Diagnostics[1].Tool != "kotlinc" {
		t.Fatalf("expected tie-breaker normalized diagnostics ordering, got %#v", diagnostics.Diagnostics)
	}
	summary, err := svc.DiagnosticSummary(context.Background(), prj, ":app", "assemble")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Summary.Total != 2 {
		t.Fatalf("expected summary recomputed from raw diagnostics, got %#v", summary.Summary)
	}
	if len(summary.Summary.BySeverity) != 1 || summary.Summary.BySeverity[0].Key != "warning" || summary.Summary.BySeverity[0].Count != 2 {
		t.Fatalf("expected recomputed severity buckets, got %#v", summary.Summary.BySeverity)
	}
	if len(summary.Summary.ByCode) != 2 || summary.Summary.ByCode[0].Key != "javac_cannot_find_symbol" || summary.Summary.ByCode[1].Key != "kotlinc_unused_symbol" {
		t.Fatalf("expected recomputed code buckets, got %#v", summary.Summary.ByCode)
	}
}

func TestIntrospectionExposesResolvedVariants(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.gradle.kts"), []byte("include(\":app\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.gradle.kts"), []byte("plugins {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildFile := filepath.Join(root, "app", "build.gradle.kts")
	if err := os.MkdirAll(filepath.Dir(buildFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildFile, []byte(`
plugins { alias(libs.plugins.android.application) }
android {
  namespace = "com.example.app"
  flavorDimensions += "tier"
  productFlavors {
    create("free") { dimension = "tier" }
    create("paid") { dimension = "tier" }
  }
  buildTypes {
    debug { applicationIdSuffix = ".debug" }
    release { }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	mod := project.Module{
		Path:      ":app",
		Dir:       filepath.Join(root, "app"),
		BuildFile: buildFile,
		Type:      "android-application",
		Namespace: "com.example.app",
		DefaultConfig: project.DefaultConfig{
			ApplicationID: "com.example.app",
		},
		FlavorDimensions: []string{"tier"},
		ProductFlavors: map[string]project.ProductFlavor{
			"free": {Name: "free", Dimension: "tier"},
			"paid": {Name: "paid", Dimension: "tier"},
		},
		BuildTypes: map[string]project.BuildType{
			"debug":   {Name: "debug", ApplicationIDSuffix: ".debug"},
			"release": {Name: "release"},
		},
	}
	prj := &project.Project{
		RootDir:       root,
		Name:          "FlavorSample",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules:       []project.Module{mod},
	}

	svc := New()
	inspect := svc.Inspect(prj)
	if len(inspect.Modules) != 1 {
		t.Fatalf("unexpected inspect result: %#v", inspect)
	}
	if got, want := len(inspect.Modules[0].Variants), 4; got != want {
		t.Fatalf("expected raw variant configs to stay available, got %#v", inspect.Modules[0].Variants)
	}
	if got, want := len(inspect.Modules[0].ResolvedVariants), 4; got != want {
		t.Fatalf("expected resolved variants, got %#v", inspect.Modules[0].ResolvedVariants)
	}
	if inspect.Modules[0].ResolvedVariants[0].Coordinate.Name == "" {
		t.Fatalf("expected structured variant coordinates, got %#v", inspect.Modules[0].ResolvedVariants[0])
	}
	foundFreeDebug := false
	for _, variant := range inspect.Modules[0].ResolvedVariants {
		if variant.Name != "freeDebug" {
			continue
		}
		foundFreeDebug = true
		if variant.Coordinate.BuildType != "debug" || len(variant.Coordinate.Flavors) != 1 || variant.Coordinate.Flavors[0] != "free" {
			t.Fatalf("unexpected resolved coordinates: %#v", variant.Coordinate)
		}
		if variant.MaterializationID == "" || variant.ArtifactSnapshotID == "" || len(variant.ManifestPaths) == 0 || len(variant.SourceRoots) == 0 || len(variant.ProducedArtifactIDs) == 0 {
			t.Fatalf("expected graph-backed resolved-variant metadata, got %#v", variant)
		}
	}
	if !foundFreeDebug {
		t.Fatalf("expected freeDebug resolved variant, got %#v", inspect.Modules[0].ResolvedVariants)
	}

	props := svc.Properties(&mod, prj)
	if got, want := len(props.ResolvedVariants), 4; got != want {
		t.Fatalf("expected resolved property variants, got %#v", props.ResolvedVariants)
	}
	foundFreeDebug = false
	for _, variant := range props.ResolvedVariants {
		if variant.Name != "freeDebug" {
			continue
		}
		foundFreeDebug = true
		if variant.Coordinate.BuildType != "debug" || len(variant.Coordinate.Flavors) != 1 || variant.Coordinate.Flavors[0] != "free" {
			t.Fatalf("unexpected property variant coordinates: %#v", variant.Coordinate)
		}
		if variant.MaterializationID == "" || variant.ArtifactSnapshotID == "" || len(variant.ManifestPaths) == 0 || len(variant.ProducedArtifactIDs) == 0 {
			t.Fatalf("expected property resolved variant metadata, got %#v", variant)
		}
	}
	if !foundFreeDebug {
		t.Fatalf("expected freeDebug in property variants, got %#v", props.ResolvedVariants)
	}
	outgoing := svc.OutgoingVariants(&mod, prj)
	if got, want := len(outgoing.ResolvedVariants), 4; got != want {
		t.Fatalf("expected resolved outgoing variants, got %#v", outgoing.ResolvedVariants)
	}
	foundFreeDebug = false
	for _, variant := range outgoing.ResolvedVariants {
		if variant.Name != "freeDebug" {
			continue
		}
		foundFreeDebug = true
		if variant.Coordinate.BuildType != "debug" || len(variant.Coordinate.Flavors) != 1 || variant.Coordinate.Flavors[0] != "free" {
			t.Fatalf("unexpected outgoing variant coordinates: %#v", variant.Coordinate)
		}
		if variant.MaterializationID == "" || variant.ArtifactSnapshotID == "" || len(variant.ManifestPaths) == 0 || len(variant.ProducedArtifactIDs) == 0 {
			t.Fatalf("expected outgoing resolved variant metadata, got %#v", variant)
		}
	}
	if !foundFreeDebug {
		t.Fatalf("expected freeDebug in outgoing variants, got %#v", outgoing.ResolvedVariants)
	}

	configs := svc.ResolvableConfigurations(&mod, prj)
	if _, ok := configs.Configurations["freeDebugCompileClasspath"]; !ok {
		t.Fatalf("expected flavored compile classpath config, got %#v", configs.Configurations)
	}
	if _, ok := configs.Configurations["freeDebugRuntimeClasspath"]; !ok {
		t.Fatalf("expected flavored runtime classpath config, got %#v", configs.Configurations)
	}
	if _, ok := configs.Configurations["debugCompileClasspath"]; ok {
		t.Fatalf("expected flavored configs to avoid plain debug names, got %#v", configs.Configurations)
	}
}

func TestResolverReportReadsCachedM2LocalProduct(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "settings.gradle.kts"), []byte("include(\":app\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.gradle.kts"), []byte("plugins {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildFile := filepath.Join(root, "app", "build.gradle.kts")
	if err := os.MkdirAll(filepath.Dir(buildFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(buildFile, []byte("dependencies {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prj := &project.Project{
		RootDir:       root,
		Name:          "ResolverSurface",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules: []project.Module{{
			Path:      ":app",
			Dir:       filepath.Join(root, "app"),
			BuildFile: buildFile,
			Type:      "android-application",
		}},
	}
	deps := &modulebuild.Dependencies{}
	emptyCatalog := loadEmptyResolverCatalog()
	cachePath, err := m2local.ResolvedCachePath(filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1"), root, nil, emptyCatalog, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(m2local.ResolvedEnvelope{
		SchemaVersion: 1,
		Format:        "m2local-resolved",
		Topology:      m2local.New(filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1"), root, nil, nil).Topology(),
		Resolved: m2local.Resolved{
			CompileJars: []string{filepath.Join(root, "build", "deps", "compile.jar")},
			RuntimeJars: []string{filepath.Join(root, "build", "deps", "runtime.jar")},
			TestJars:    []string{filepath.Join(root, "build", "deps", "test.jar")},
			Report: m2local.ResolutionReport{
				Selections: []m2local.ResolutionSelection{{
					Kind:       "variant_selection",
					Coordinate: "g:m:1.0.0",
					Chosen:     "releaseRuntimeElements",
					MetadataSource: &m2local.ResolutionMetadataSource{
						Kind:          "module",
						Path:          filepath.Join(root, ".grit", "metadata", "g", "m", "1.0.0", "m-1.0.0.module"),
						RepositoryURL: "https://repo1.maven.org/maven2/",
						Fetched:       true,
					},
				}},
				Conflicts: []m2local.ResolutionConflict{{
					Kind:      "version_conflict",
					Module:    "g:m",
					Selected:  "1.0.0",
					Discarded: "2.0.0",
				}},
			},
			Replay: m2local.ResolutionReplay{
				Pins: []m2local.ResolutionPin{{
					Coordinate:    "g:m:1.0.0",
					Variant:       "releaseRuntimeElements",
					RepositoryURL: "https://repo1.maven.org/maven2/",
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	lockfilePath := strings.TrimSuffix(cachePath, ".json") + ".lockfile.json"
	lockfilePayload, err := json.Marshal(m2local.ResolutionLockfile{
		SchemaVersion: 1,
		Format:        "m2local-lockfile",
		Pins: []m2local.ResolutionPin{{
			Coordinate:    "g:m:1.0.0",
			Variant:       "releaseRuntimeElements",
			RepositoryURL: "https://repo1.maven.org/maven2/",
		}},
		Selections: []m2local.ResolutionSelection{{
			Kind:       "variant_selection",
			Coordinate: "g:m:1.0.0",
			Chosen:     "releaseRuntimeElements",
			MetadataSource: &m2local.ResolutionMetadataSource{
				Kind:          "module",
				Path:          filepath.Join(root, ".grit", "metadata", "g", "m", "1.0.0", "m-1.0.0.module"),
				RepositoryURL: "https://repo1.maven.org/maven2/",
				Fetched:       true,
			},
		}},
		Conflicts: []m2local.ResolutionConflict{{
			Kind:      "version_conflict",
			Module:    "g:m",
			Selected:  "1.0.0",
			Discarded: "2.0.0",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockfilePath, lockfilePayload, 0o644); err != nil {
		t.Fatal(err)
	}

	svc := New()
	result, err := svc.ResolverReport(&prj.Modules[0], prj)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Found || result.CachePath != cachePath {
		t.Fatalf("expected found cached resolver report, got %#v", result)
	}
	if result.LockfilePath == "" {
		t.Fatalf("expected lockfile path in resolver report, got %#v", result)
	}
	if result.Topology.CacheRoot == "" || result.Topology.WorkMetadataRoot == "" || len(result.Topology.Layers) < 3 {
		t.Fatalf("expected richer resolver topology in resolver report, got %#v", result.Topology)
	}
	if result.ReportPath == "" || result.ReplayPath == "" {
		t.Fatalf("expected report and replay paths in resolver report, got %#v", result)
	}
	if _, err := os.Stat(result.ReportPath); err != nil {
		t.Fatalf("expected resolver report artifact to exist: %v", err)
	}
	if _, err := os.Stat(result.ReplayPath); err != nil {
		t.Fatalf("expected resolver replay artifact to exist: %v", err)
	}
	if result.Summary.CompileJarCount != 1 || result.Summary.ConflictCount != 1 || result.Summary.PinCount != 1 {
		t.Fatalf("unexpected resolver summary: %#v", result.Summary)
	}
	if result.Inputs.CacheStatus != "hit" || result.Inputs.CacheKey == "" || result.Inputs.CacheVersion != "16" {
		t.Fatalf("unexpected resolver inputs: %#v", result.Inputs)
	}
	if len(result.Report.Selections) != 1 || result.Report.Selections[0].Chosen != "releaseRuntimeElements" {
		t.Fatalf("unexpected resolver report: %#v", result.Report)
	}
	if result.Report.Selections[0].MetadataSource == nil || result.Report.Selections[0].MetadataSource.RepositoryURL == "" {
		t.Fatalf("expected metadata provenance in resolver report: %#v", result.Report.Selections[0])
	}
	if len(result.Replay.Pins) != 1 || result.Replay.Pins[0].Variant != "releaseRuntimeElements" {
		t.Fatalf("unexpected resolver replay: %#v", result.Replay)
	}
	if result.Replay.Pins[0].RepositoryURL != "https://repo1.maven.org/maven2/" {
		t.Fatalf("expected repository provenance in resolver replay pin: %#v", result.Replay.Pins[0])
	}
	if result.Lockfile.SchemaVersion != 1 || len(result.Lockfile.Conflicts) != 1 {
		t.Fatalf("unexpected resolver lockfile: %#v", result.Lockfile)
	}
	if len(result.Lockfile.Pins) != 1 || result.Lockfile.Pins[0].RepositoryURL != "https://repo1.maven.org/maven2/" {
		t.Fatalf("expected repository provenance in resolver lockfile pin: %#v", result.Lockfile)
	}
}

func TestCacheTopologyExposesResolverRootsAndLayers(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	cacheRoot := filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	prj := &project.Project{
		RootDir: root,
		Name:    "CacheTopology",
	}
	svc := New()
	result, err := svc.CacheTopology(prj)
	if err != nil {
		t.Fatal(err)
	}
	if result.Repo != root {
		t.Fatalf("unexpected cache topology repo: %#v", result)
	}
	if result.Topology.CacheRoot != cacheRoot {
		t.Fatalf("unexpected cache root in cache topology: %#v", result.Topology)
	}
	if result.Topology.WorkRoot != root || result.Topology.WorkMetadataRoot != filepath.Join(root, ".grit", "metadata") {
		t.Fatalf("unexpected work roots in cache topology: %#v", result.Topology)
	}
	if len(result.Topology.Layers) < 3 {
		t.Fatalf("expected normalized cache layers in cache topology: %#v", result.Topology)
	}
}

func loadEmptyResolverCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		Versions:  map[string]string{},
		Libraries: map[string]catalog.Library{},
		Bundles:   map[string][]string{},
	}
}
