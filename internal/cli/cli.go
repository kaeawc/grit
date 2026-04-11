package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/responsepayload"
)

const usageText = `grit is a constrained Android/JVM build runner aimed at eventually replacing existing Android build tooling.

	Current MVP:
	  - inspect
	  - tasks
	  - signingReport
	  - dependencies
	  - projects
	  - properties
	  - buildEnvironment
	  - artifactTransforms
	  - dependencyInsight
	  - resolverReport
	  - cacheTopology
	  - explainPlan
	  - variantProvenance
	  - actionProvenance
	  - cleanupPlan
	  - runSummary
	  - runSummaries
	  - runGraphSummary
	  - criticalPathSummary
	  - schedulerSummary
	  - cacheSummary
	  - toolSummary
	  - diagnostics
	  - diagnosticSummary
	  - plannedSchedule
	  - scheduleDrift
	  - actionExecution
	  - actionExplanation
	  - actionExecutions
	  - actionExplanations
	  - cacheProbes
	  - cacheProbeRecords
	  - reuseDecision
	  - reuseDecisions
	  - materializations
	  - actionTrace
	  - perfTiming
	  - classpathSnapshot
	  - classpathSnapshotProvenance
	  - classpathSnapshotConsumers
	  - classpathEntryLookup
	  - classpathPathConsumers
	  - artifactOnClasspath
	  - artifactClasspathConsumers
	  - fileOwners
	  - actionInputs
	  - actionOutputs
	  - actionByID
	  - artifactByID
	  - materializationByID
	  - materializationConsumers
	  - actionDependencies
	  - actionDependents
	  - plannedActionPolicy
	  - plannedActionPolicies
	  - actionsForModule
	  - actionsForVariant
	  - intellijSyncModel
	  - resolveIntelliJTasks
	  - variantMaterialization
	  - variantSourceSetModel
	  - dependencyBindingsForVariant
	  - dependencyBindingsForModule
	  - dependencyRealizationsForVariant
	  - dependencyRealizationsForModule
	  - materializationProvenance
	  - variantCompatibility
	  - artifactsForVariant
	  - artifactsForModule
	  - moduleManifest
	  - variantManifest
	  - artifactSnapshotProvenance
	  - artifactSnapshotConsumers
	  - artifactProvenance
	  - artifactConsumers
	  - variantImpact
	  - moduleImpact
	  - classpathProvenance
	  - androidCapabilities
	  - javaToolchains
	  - kotlinDslAccessorsReport
	  - outgoingVariants
	  - resolvableConfigurations
	  - doctor
	  - uninstallDebug
	  - uninstallRelease
	  - uninstallAll
	  - clean
	  - build
	  - buildNeeded
	  - buildDependents
	  - test
	  - assembleDebug
	  - assembleRelease
	  - compileDebugSources
	  - compileReleaseSources
	  - compileDebugUnitTestSources
	  - assembleUnitTest
	  - installDebug
	  - installRelease
	  - installDebugAndroidTest
	  - testDebugUnitTest
	  - uninstallDebugAndroidTest
	  - check
	  - compile-debug
	  - assemble-debug
	  - assemble-release
	  - assemble --variant <name>
	  - install-debug
	  - install --variant <name>
	  - test-debug-unit

Examples:
	  grit inspect --repo ~/path/to/android-repo
	  grit tasks --repo ~/path/to/android-repo --module :app
	  grit signingReport --repo ~/path/to/android-repo --module :app
	  grit dependencies --repo ~/path/to/android-repo --module :app
	  grit projects --repo ~/path/to/android-repo
	  grit properties --repo ~/path/to/android-repo --module :app
	  grit buildEnvironment --repo ~/path/to/android-repo
	  grit artifactTransforms --repo ~/path/to/android-repo --module :app
	  grit dependencyInsight --repo ~/path/to/android-repo --module :app --dependency okhttp
	  grit resolverReport --repo ~/path/to/android-repo --module :app
	  grit cacheTopology --repo ~/path/to/android-repo
	  grit explainPlan --repo ~/path/to/android-repo --module :app --command assemble --variant debug
	  grit variantProvenance --repo ~/path/to/android-repo --module :app --variant debug
	  grit actionProvenance --repo ~/path/to/android-repo --action <id>
	  grit cleanupPlan --repo ~/path/to/android-repo
	  grit runSummary --repo ~/path/to/android-repo --module :app --command assemble
	  grit runSummaries --repo ~/path/to/android-repo --module :app
	  grit runGraphSummary --repo ~/path/to/android-repo --module :app --command assemble
	  grit criticalPathSummary --repo ~/path/to/android-repo --module :app --command assemble
	  grit schedulerSummary --repo ~/path/to/android-repo --module :app --command assemble
	  grit cacheSummary --repo ~/path/to/android-repo --module :app --command assemble
	  grit toolSummary --repo ~/path/to/android-repo --module :app --command assemble
	  grit diagnostics --repo ~/path/to/android-repo --module :app --command assemble
	  grit diagnosticSummary --repo ~/path/to/android-repo --module :app --command assemble
	  grit plannedSchedule --repo ~/path/to/android-repo --module :app --command assemble
	  grit scheduleDrift --repo ~/path/to/android-repo --module :app --command assemble
	  grit actionExecution --repo ~/path/to/android-repo --module :app --command assemble --action <id>
	  grit actionExplanation --repo ~/path/to/android-repo --module :app --command assemble --action <id>
	  grit actionExecutions --repo ~/path/to/android-repo --module :app --command assemble
	  grit actionExplanations --repo ~/path/to/android-repo --module :app --command assemble
	  grit cacheProbes --repo ~/path/to/android-repo --module :app --command assemble
	  grit cacheProbeRecords --repo ~/path/to/android-repo --module :app --command assemble
	  grit reuseDecision --repo ~/path/to/android-repo --module :app --command assemble --action action:compile
	  grit reuseDecisions --repo ~/path/to/android-repo --module :app --command assemble
	  grit materializations --repo ~/path/to/android-repo --module :app --command assemble
	  grit actionTrace --repo ~/path/to/android-repo --module :app --command assemble
	  grit perfTiming --repo ~/path/to/android-repo --module :app --command assemble
	  grit classpathSnapshot --repo ~/path/to/android-repo --module :app --variant debug
	  grit classpathSnapshotProvenance --repo ~/path/to/android-repo --snapshot <id>
	  grit classpathSnapshotConsumers --repo ~/path/to/android-repo --snapshot <id>
	  grit classpathEntryLookup --repo ~/path/to/android-repo --module :app --variant debug --path app/src/main
	  grit classpathPathConsumers --repo ~/path/to/android-repo --path app/build/classes.jar
	  grit artifactOnClasspath --repo ~/path/to/android-repo --module :app --variant debug --artifact <id>
	  grit artifactClasspathConsumers --repo ~/path/to/android-repo --artifact <id>
	  grit fileOwners --repo ~/path/to/android-repo --path ~/path/to/android-repo/app/src/main/AndroidManifest.xml
	  grit actionInputs --repo ~/path/to/android-repo --action action:compile
	  grit actionOutputs --repo ~/path/to/android-repo --action action:compile
	  grit actionDependencies --repo ~/path/to/android-repo --action action:compile
	  grit actionDependents --repo ~/path/to/android-repo --action action:compile
	  grit plannedActionPolicy --repo ~/path/to/android-repo --module :app --command assemble --variant freeDebug --action action:compile
	  grit plannedActionPolicies --repo ~/path/to/android-repo --module :app --command assemble --variant freeDebug
	  grit actionsForModule --repo ~/path/to/android-repo --module :app
	  grit actionsForVariant --repo ~/path/to/android-repo --module :app --variant freeDebug
	  grit intellijSyncModel --repo ~/path/to/android-repo
	  grit resolveIntelliJTasks --repo ~/path/to/android-repo --module :app --task assembleFreeDebug --task compileFreeDebugUnitTestSources
	  grit variantMaterialization --repo ~/path/to/android-repo --module :app --variant debug
	  grit dependencyBindingsForVariant --repo ~/path/to/android-repo --module :app --variant debug
	  grit dependencyBindingsForModule --repo ~/path/to/android-repo --module :app
	  grit dependencyRealizationsForVariant --repo ~/path/to/android-repo --module :app --variant freeDebug
	  grit dependencyRealizationsForModule --repo ~/path/to/android-repo --module :app
	  grit materializationProvenance --repo ~/path/to/android-repo --materialization <id>
	  grit variantCompatibility --repo ~/path/to/android-repo --module :app --variant debug
	  grit artifactsForVariant --repo ~/path/to/android-repo --module :app --variant debug
	  grit artifactsForModule --repo ~/path/to/android-repo --module :app
	  grit moduleManifest --repo ~/path/to/android-repo --module :app
	  grit variantManifest --repo ~/path/to/android-repo --module :app --variant debug
	  grit artifactSnapshotProvenance --repo ~/path/to/android-repo --snapshot <id>
	  grit artifactSnapshotConsumers --repo ~/path/to/android-repo --snapshot <id>
	  grit artifactProvenance --repo ~/path/to/android-repo --artifact <id>
	  grit artifactConsumers --repo ~/path/to/android-repo --artifact <id>
	  grit variantImpact --repo ~/path/to/android-repo --module :app --variant debug
	  grit moduleImpact --repo ~/path/to/android-repo --module :app
	  grit classpathProvenance --repo ~/path/to/android-repo --module :app --variant debug
	  grit androidCapabilities --repo ~/path/to/android-repo --module :app
	  grit javaToolchains --repo ~/path/to/android-repo
	  grit kotlinDslAccessorsReport --repo ~/path/to/android-repo --module :app
	  grit outgoingVariants --repo ~/path/to/android-repo --module :app
	  grit resolvableConfigurations --repo ~/path/to/android-repo --module :app
	  grit doctor --repo ~/path/to/android-repo
	  grit uninstallDebug --repo ~/path/to/android-repo --module :app
	  grit clean --repo ~/path/to/android-repo --module :app
	  grit build --repo ~/path/to/android-repo --module :app
	  grit buildDependents --repo ~/path/to/android-repo --module :lib
	  grit test --repo ~/path/to/android-repo --module :app
	  grit assembleDebug --repo ~/path/to/android-repo --module :app
	  grit compileDebugSources --repo ~/path/to/android-repo --module :app
	  grit assembleUnitTest --repo ~/path/to/android-repo --module :app
	  grit installRelease --repo ~/path/to/android-repo --module :app
	  grit installDebugAndroidTest --repo ~/path/to/android-repo --module :app
	  grit testDebugUnitTest --repo ~/path/to/android-repo --module :app
	  grit uninstallDebugAndroidTest --repo ~/path/to/android-repo --module :app
	  grit compile-debug --repo ~/path/to/android-repo
	  grit assemble-debug --repo ~/path/to/android-repo
	  grit assemble-release --repo ~/path/to/android-repo
	  grit assemble --repo ~/path/to/android-repo --variant release
	  grit install-debug --repo ~/path/to/android-repo
	  grit install --repo ~/path/to/android-repo --variant debug
	  grit test-debug-unit --repo ~/path/to/android-repo
`

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	args, perfEnabled := stripBoolFlag(args, "--perf")
	start := time.Now()

	if len(args) == 0 {
		return writeResponse(stdout, response{
			Success:    false,
			Command:    "help",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      &responseError{Message: "missing command"},
			Result:     resultJSON(responsepayload.UsageResult{Usage: usageText}),
		}, 2, stderr)
	}

	tracker := perf.New(perfEnabled)
	command := args[0]
	switch args[0] {
	case "inspect":
		return runInspect(ctx, args[1:], stdout, stderr, tracker, start)
	case "tasks":
		return runTasks(ctx, args[1:], stdout, stderr, tracker, start)
	case "signingReport":
		return runSigningReport(ctx, args[1:], stdout, stderr, tracker, start)
	case "dependencies":
		return runDependencies(ctx, args[1:], stdout, stderr, tracker, start)
	case "projects":
		return runProjects(ctx, args[1:], stdout, stderr, tracker, start)
	case "properties":
		return runProperties(ctx, args[1:], stdout, stderr, tracker, start)
	case "buildEnvironment":
		return runBuildEnvironment(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactTransforms":
		return runArtifactTransforms(ctx, args[1:], stdout, stderr, tracker, start)
	case "dependencyInsight":
		return runDependencyInsight(ctx, args[1:], stdout, stderr, tracker, start)
	case "resolverReport":
		return runResolverReport(ctx, args[1:], stdout, stderr, tracker, start)
	case "cacheTopology":
		return runCacheTopology(ctx, args[1:], stdout, stderr, tracker, start)
	case "explainPlan":
		return runExplainPlan(ctx, args[1:], stdout, stderr, tracker, start)
	case "variantProvenance":
		return runVariantProvenance(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionProvenance":
		return runActionProvenance(ctx, args[1:], stdout, stderr, tracker, start)
	case "cleanupPlan":
		return runCleanupPlan(ctx, args[1:], stdout, stderr, tracker, start)
	case "runSummary":
		return runRunSummary(ctx, args[1:], stdout, stderr, tracker, start)
	case "runSummaries":
		return runRunSummaries(ctx, args[1:], stdout, stderr, tracker, start)
	case "runGraphSummary":
		return runRunGraphSummary(ctx, args[1:], stdout, stderr, tracker, start)
	case "criticalPathSummary":
		return runCriticalPathSummary(ctx, args[1:], stdout, stderr, tracker, start)
	case "schedulerSummary":
		return runSchedulerSummary(ctx, args[1:], stdout, stderr, tracker, start)
	case "cacheSummary":
		return runCacheSummary(ctx, args[1:], stdout, stderr, tracker, start)
	case "toolSummary":
		return runToolSummary(ctx, args[1:], stdout, stderr, tracker, start)
	case "diagnostics":
		return runDiagnostics(ctx, args[1:], stdout, stderr, tracker, start)
	case "diagnosticSummary":
		return runDiagnosticSummary(ctx, args[1:], stdout, stderr, tracker, start)
	case "plannedSchedule":
		return runPlannedSchedule(ctx, args[1:], stdout, stderr, tracker, start)
	case "scheduleDrift":
		return runScheduleDrift(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionExecution":
		return runActionExecution(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionExplanation":
		return runActionExplanation(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionExecutions":
		return runActionExecutions(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionExplanations":
		return runActionExplanations(ctx, args[1:], stdout, stderr, tracker, start)
	case "cacheProbes":
		return runCacheProbes(ctx, args[1:], stdout, stderr, tracker, start)
	case "cacheProbeRecords":
		return runCacheProbeRecords(ctx, args[1:], stdout, stderr, tracker, start)
	case "reuseDecision":
		return runReuseDecision(ctx, args[1:], stdout, stderr, tracker, start)
	case "reuseDecisions":
		return runReuseDecisions(ctx, args[1:], stdout, stderr, tracker, start)
	case "materializations":
		return runMaterializations(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionTrace":
		return runActionTrace(ctx, args[1:], stdout, stderr, tracker, start)
	case "perfTiming":
		return runPerfTiming(ctx, args[1:], stdout, stderr, tracker, start)
	case "classpathSnapshot":
		return runClasspathSnapshot(ctx, args[1:], stdout, stderr, tracker, start)
	case "classpathSnapshotByID":
		return runClasspathSnapshotByID(ctx, args[1:], stdout, stderr, tracker, start)
	case "classpathSnapshotProvenance":
		return runClasspathSnapshotProvenance(ctx, args[1:], stdout, stderr, tracker, start)
	case "classpathSnapshotConsumers":
		return runClasspathSnapshotConsumers(ctx, args[1:], stdout, stderr, tracker, start)
	case "classpathSnapshotConsumersByID":
		return runClasspathSnapshotConsumersByID(ctx, args[1:], stdout, stderr, tracker, start)
	case "classpathEntryLookup":
		return runClasspathEntryLookup(ctx, args[1:], stdout, stderr, tracker, start)
	case "classpathPathConsumers":
		return runClasspathPathConsumers(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactOnClasspath":
		return runArtifactOnClasspath(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactClasspathConsumers":
		return runArtifactClasspathConsumers(ctx, args[1:], stdout, stderr, tracker, start)
	case "fileOwners":
		return runFileOwners(ctx, args[1:], stdout, stderr, tracker, start)
	case "moduleByID":
		return runModuleByID(ctx, args[1:], stdout, stderr, tracker, start)
	case "variantByID":
		return runVariantByID(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionByID":
		return runActionByID(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactByID":
		return runArtifactByID(ctx, args[1:], stdout, stderr, tracker, start)
	case "materializationByID":
		return runMaterializationByID(ctx, args[1:], stdout, stderr, tracker, start)
	case "materializationConsumers":
		return runMaterializationConsumers(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionInputs":
		return runActionInputs(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionOutputs":
		return runActionOutputs(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionDependencies":
		return runActionDependencies(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionDependents":
		return runActionDependents(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionsForModule":
		return runActionsForModule(ctx, args[1:], stdout, stderr, tracker, start)
	case "actionsForVariant":
		return runActionsForVariant(ctx, args[1:], stdout, stderr, tracker, start)
	case "intellijSyncModel":
		return runIntelliJSyncModel(ctx, args[1:], stdout, stderr, tracker, start)
	case "resolveIntelliJTasks":
		return runResolveIntelliJTasks(ctx, args[1:], stdout, stderr, tracker, start)
	case "variantMaterialization":
		return runVariantMaterialization(ctx, args[1:], stdout, stderr, tracker, start)
	case "variantSourceSetModel":
		return runVariantSourceSetModel(ctx, args[1:], stdout, stderr, tracker, start)
	case "dependencyBindingsForVariant":
		return runDependencyBindingsForVariant(ctx, args[1:], stdout, stderr, tracker, start)
	case "dependencyBindingsForModule":
		return runDependencyBindingsForModule(ctx, args[1:], stdout, stderr, tracker, start)
	case "dependencyRealizationsForVariant":
		return runDependencyRealizationsForVariant(ctx, args[1:], stdout, stderr, tracker, start)
	case "dependencyRealizationsForModule":
		return runDependencyRealizationsForModule(ctx, args[1:], stdout, stderr, tracker, start)
	case "plannedActionPolicy":
		return runPlannedActionPolicy(ctx, args[1:], stdout, stderr, tracker, start)
	case "plannedActionPolicies":
		return runPlannedActionPolicies(ctx, args[1:], stdout, stderr, tracker, start)
	case "materializationProvenance":
		return runMaterializationProvenance(ctx, args[1:], stdout, stderr, tracker, start)
	case "variantCompatibility":
		return runVariantCompatibility(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactsForVariant":
		return runArtifactsForVariant(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactsForModule":
		return runArtifactsForModule(ctx, args[1:], stdout, stderr, tracker, start)
	case "moduleManifest":
		return runModuleManifest(ctx, args[1:], stdout, stderr, tracker, start)
	case "variantManifest":
		return runVariantManifest(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactSnapshotProvenance":
		return runArtifactSnapshotProvenance(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactSnapshotConsumers":
		return runArtifactSnapshotConsumers(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactProvenance":
		return runArtifactProvenance(ctx, args[1:], stdout, stderr, tracker, start)
	case "artifactConsumers":
		return runArtifactConsumers(ctx, args[1:], stdout, stderr, tracker, start)
	case "variantImpact":
		return runVariantImpact(ctx, args[1:], stdout, stderr, tracker, start)
	case "moduleImpact":
		return runModuleImpact(ctx, args[1:], stdout, stderr, tracker, start)
	case "classpathProvenance":
		return runClasspathProvenance(ctx, args[1:], stdout, stderr, tracker, start)
	case "androidCapabilities":
		return runAndroidCapabilities(ctx, args[1:], stdout, stderr, tracker, start)
	case "javaToolchains":
		return runJavaToolchains(ctx, args[1:], stdout, stderr, tracker, start)
	case "kotlinDslAccessorsReport":
		return runKotlinDslAccessorsReport(ctx, args[1:], stdout, stderr, tracker, start)
	case "outgoingVariants":
		return runOutgoingVariants(ctx, args[1:], stdout, stderr, tracker, start)
	case "resolvableConfigurations":
		return runResolvableConfigurations(ctx, args[1:], stdout, stderr, tracker, start)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr, tracker, start)
	case "uninstallDebug":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "uninstallDebug")
	case "uninstallRelease":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "uninstallRelease")
	case "uninstallAll":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "uninstallAll")
	case "clean":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "clean")
	case "build":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "build")
	case "buildNeeded":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "buildNeeded")
	case "buildDependents":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "buildDependents")
	case "test":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "test")
	case "assembleDebug":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "assembleDebug")
	case "assembleRelease":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "assembleRelease")
	case "compileDebugSources":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "compileDebugSources")
	case "compileReleaseSources":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "compileReleaseSources")
	case "compileDebugUnitTestSources":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "compileDebugUnitTestSources")
	case "assembleUnitTest":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "assembleUnitTest")
	case "installDebug":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "installDebug")
	case "installRelease":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "installRelease")
	case "installDebugAndroidTest":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "installDebugAndroidTest")
	case "testDebugUnitTest":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "testDebugUnitTest")
	case "uninstallDebugAndroidTest":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "uninstallDebugAndroidTest")
	case "check":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "check")
	case "compile-debug":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "compile-debug")
	case "assemble-debug":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "assemble-debug")
	case "assemble-release":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "assemble-release")
	case "assemble":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "assemble")
	case "install-debug":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "install-debug")
	case "install":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "install")
	case "test-debug-unit":
		return runNativeBuild(ctx, args[1:], stdout, stderr, tracker, start, "test-debug-unit")
	case "-h", "--help", "help":
		return writeResponse(stdout, response{
			Success:    true,
			Command:    "help",
			DurationMs: time.Since(start).Milliseconds(),
			Result:     resultJSON(responsepayload.UsageResult{Usage: usageText}),
			PerfTiming: tracker.GetTimings(),
		}, 0, stderr)
	default:
		return writeResponse(stdout, response{
			Success:    false,
			Command:    command,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      &responseError{Message: fmt.Sprintf("unknown command %q", command)},
			Result:     resultJSON(responsepayload.UsageResult{Usage: usageText}),
			PerfTiming: tracker.GetTimings(),
		}, 2, stderr)
	}
}
