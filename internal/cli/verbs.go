package cli

import (
	"context"
	"io"
	"time"

	"github.com/kaeawc/grit/internal/perf"
)

// verbRunner is the canonical signature every CLI verb dispatches to.
type verbRunner func(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int

// verbDef describes a single CLI verb. The first entry of names is the primary
// name; any further entries are aliases that share the same runner.
type verbDef struct {
	names  []string
	runner verbRunner
}

// nativeBuildVerb wraps runNativeBuild for a fixed command name. The wrapper
// preserves the runner signature so the dispatch table stays homogeneous.
func nativeBuildVerb(command string) verbRunner {
	return func(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
		return runNativeBuild(ctx, args, stdout, stderr, tracker, start, command)
	}
}

// nativeBuildVerbDef builds a verbDef whose runner forwards to runNativeBuild
// using the verb's primary name as the command argument. Aliases (when given)
// share that same primary command name.
func nativeBuildVerbDef(names ...string) verbDef {
	return verbDef{names: names, runner: nativeBuildVerb(names[0])}
}

// verbTable enumerates every CLI verb. Order is informational; lookup happens
// through verbLookup which is built once at init time.
var verbTable = []verbDef{
	{names: []string{"inspect"}, runner: runInspect},
	{names: []string{"tasks"}, runner: runTasks},
	{names: []string{"signingReport"}, runner: runSigningReport},
	{names: []string{"dependencies"}, runner: runDependencies},
	{names: []string{"projects"}, runner: runProjects},
	{names: []string{"properties"}, runner: runProperties},
	{names: []string{"buildEnvironment"}, runner: runBuildEnvironment},
	{names: []string{"artifactTransforms"}, runner: runArtifactTransforms},
	{names: []string{"dependencyInsight"}, runner: runDependencyInsight},
	{names: []string{"resolverReport"}, runner: runResolverReport},
	{names: []string{"cacheTopology"}, runner: runCacheTopology},
	{names: []string{"explainPlan"}, runner: runExplainPlan},
	{names: []string{"variantProvenance"}, runner: runVariantProvenance},
	{names: []string{"actionProvenance"}, runner: runActionProvenance},
	{names: []string{"cleanupPlan"}, runner: runCleanupPlan},
	{names: []string{"runSummary"}, runner: runRunSummary},
	{names: []string{"runSummaries"}, runner: runRunSummaries},
	{names: []string{"runGraphSummary"}, runner: runRunGraphSummary},
	{names: []string{"criticalPathSummary"}, runner: runCriticalPathSummary},
	{names: []string{"schedulerSummary"}, runner: runSchedulerSummary},
	{names: []string{"cacheSummary"}, runner: runCacheSummary},
	{names: []string{"toolSummary"}, runner: runToolSummary},
	{names: []string{"diagnostics"}, runner: runDiagnostics},
	{names: []string{"diagnosticSummary"}, runner: runDiagnosticSummary},
	{names: []string{"plannedSchedule"}, runner: runPlannedSchedule},
	{names: []string{"scheduleDrift"}, runner: runScheduleDrift},
	{names: []string{"actionExecution"}, runner: runActionExecution},
	{names: []string{"actionExplanation"}, runner: runActionExplanation},
	{names: []string{"actionExecutions"}, runner: runActionExecutions},
	{names: []string{"actionExplanations"}, runner: runActionExplanations},
	{names: []string{"cacheProbes"}, runner: runCacheProbes},
	{names: []string{"cacheProbeRecords"}, runner: runCacheProbeRecords},
	{names: []string{"reuseDecision"}, runner: runReuseDecision},
	{names: []string{"reuseDecisions"}, runner: runReuseDecisions},
	{names: []string{"materializations"}, runner: runMaterializations},
	{names: []string{"actionTrace"}, runner: runActionTrace},
	{names: []string{"perfTiming"}, runner: runPerfTiming},
	{names: []string{"classpathSnapshot"}, runner: runClasspathSnapshot},
	{names: []string{"classpathSnapshotByID"}, runner: runClasspathSnapshotByID},
	{names: []string{"classpathSnapshotProvenance"}, runner: runClasspathSnapshotProvenance},
	{names: []string{"classpathSnapshotConsumers"}, runner: runClasspathSnapshotConsumers},
	{names: []string{"classpathSnapshotConsumersByID"}, runner: runClasspathSnapshotConsumersByID},
	{names: []string{"classpathEntryLookup"}, runner: runClasspathEntryLookup},
	{names: []string{"classpathPathConsumers"}, runner: runClasspathPathConsumers},
	{names: []string{"artifactOnClasspath"}, runner: runArtifactOnClasspath},
	{names: []string{"artifactClasspathConsumers"}, runner: runArtifactClasspathConsumers},
	{names: []string{"fileOwners"}, runner: runFileOwners},
	{names: []string{"moduleByID"}, runner: runModuleByID},
	{names: []string{"variantByID"}, runner: runVariantByID},
	{names: []string{"actionByID"}, runner: runActionByID},
	{names: []string{"artifactByID"}, runner: runArtifactByID},
	{names: []string{"materializationByID"}, runner: runMaterializationByID},
	{names: []string{"materializationConsumers"}, runner: runMaterializationConsumers},
	{names: []string{"actionInputs"}, runner: runActionInputs},
	{names: []string{"actionOutputs"}, runner: runActionOutputs},
	{names: []string{"actionDependencies"}, runner: runActionDependencies},
	{names: []string{"actionDependents"}, runner: runActionDependents},
	{names: []string{"actionsForModule"}, runner: runActionsForModule},
	{names: []string{"actionsForVariant"}, runner: runActionsForVariant},
	{names: []string{"intellijSyncModel"}, runner: runIntelliJSyncModel},
	{names: []string{"resolveIntelliJTasks"}, runner: runResolveIntelliJTasks},
	{names: []string{"variantMaterialization"}, runner: runVariantMaterialization},
	{names: []string{"variantSourceSetModel"}, runner: runVariantSourceSetModel},
	{names: []string{"dependencyBindingsForVariant"}, runner: runDependencyBindingsForVariant},
	{names: []string{"dependencyBindingsForModule"}, runner: runDependencyBindingsForModule},
	{names: []string{"dependencyRealizationsForVariant"}, runner: runDependencyRealizationsForVariant},
	{names: []string{"dependencyRealizationsForModule"}, runner: runDependencyRealizationsForModule},
	{names: []string{"plannedActionPolicy"}, runner: runPlannedActionPolicy},
	{names: []string{"plannedActionPolicies"}, runner: runPlannedActionPolicies},
	{names: []string{"materializationProvenance"}, runner: runMaterializationProvenance},
	{names: []string{"variantCompatibility"}, runner: runVariantCompatibility},
	{names: []string{"artifactsForVariant"}, runner: runArtifactsForVariant},
	{names: []string{"artifactsForModule"}, runner: runArtifactsForModule},
	{names: []string{"moduleManifest"}, runner: runModuleManifest},
	{names: []string{"variantManifest"}, runner: runVariantManifest},
	{names: []string{"artifactSnapshotProvenance"}, runner: runArtifactSnapshotProvenance},
	{names: []string{"artifactSnapshotConsumers"}, runner: runArtifactSnapshotConsumers},
	{names: []string{"artifactProvenance"}, runner: runArtifactProvenance},
	{names: []string{"artifactConsumers"}, runner: runArtifactConsumers},
	{names: []string{"variantImpact"}, runner: runVariantImpact},
	{names: []string{"moduleImpact"}, runner: runModuleImpact},
	{names: []string{"classpathProvenance"}, runner: runClasspathProvenance},
	{names: []string{"androidCapabilities"}, runner: runAndroidCapabilities},
	{names: []string{"javaToolchains"}, runner: runJavaToolchains},
	{names: []string{"kotlinDslAccessorsReport"}, runner: runKotlinDslAccessorsReport},
	{names: []string{"outgoingVariants"}, runner: runOutgoingVariants},
	{names: []string{"resolvableConfigurations"}, runner: runResolvableConfigurations},
	{names: []string{"doctor"}, runner: runDoctor},

	// Native build verbs forward to runNativeBuild with a fixed command name.
	nativeBuildVerbDef("uninstallDebug"),
	nativeBuildVerbDef("uninstallRelease"),
	nativeBuildVerbDef("uninstallAll"),
	nativeBuildVerbDef("clean"),
	nativeBuildVerbDef("build"),
	nativeBuildVerbDef("buildNeeded"),
	// buildDependents and compile share a runner, but each forwards its own
	// alias as the native command (preserving prior `command := args[0]`
	// behavior of the switch).
	{names: []string{"buildDependents"}, runner: nativeBuildVerb("buildDependents")},
	{names: []string{"compile"}, runner: nativeBuildVerb("compile")},
	nativeBuildVerbDef("test"),
	nativeBuildVerbDef("assembleDebug"),
	nativeBuildVerbDef("assembleRelease"),
	nativeBuildVerbDef("compileDebugSources"),
	nativeBuildVerbDef("compileReleaseSources"),
	nativeBuildVerbDef("compileDebugUnitTestSources"),
	nativeBuildVerbDef("assembleUnitTest"),
	nativeBuildVerbDef("compileDebugAndroidTestSources"),
	nativeBuildVerbDef("installDebug"),
	nativeBuildVerbDef("installRelease"),
	nativeBuildVerbDef("installDebugAndroidTest"),
	nativeBuildVerbDef("testDebugUnitTest"),
	nativeBuildVerbDef("uninstallDebugAndroidTest"),
	nativeBuildVerbDef("check"),
	nativeBuildVerbDef("compile-debug"),
	nativeBuildVerbDef("assemble-debug"),
	nativeBuildVerbDef("assemble-release"),
	nativeBuildVerbDef("assemble"),
	nativeBuildVerbDef("install-debug"),
	nativeBuildVerbDef("install"),
	nativeBuildVerbDef("test-debug-unit"),

	{names: []string{"gc"}, runner: runGC},
}

// verbLookup maps every verb name (including aliases) to its runner.
var verbLookup = func() map[string]verbRunner {
	m := make(map[string]verbRunner, len(verbTable)*2)
	for _, v := range verbTable {
		for _, name := range v.names {
			m[name] = v.runner
		}
	}
	return m
}()
