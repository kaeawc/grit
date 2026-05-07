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
	  - compile
	  - test
	  - assembleDebug
	  - assembleRelease
	  - compileDebugSources
	  - compileReleaseSources
	  - compileDebugUnitTestSources
	  - assembleUnitTest
	  - compileDebugAndroidTestSources
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
	  - gc

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
	  grit compile --repo ~/path/to/android-repo --module :lib
	  grit test --repo ~/path/to/android-repo --module :app
	  grit assembleDebug --repo ~/path/to/android-repo --module :app
	  grit compileDebugSources --repo ~/path/to/android-repo --module :app
	  grit assembleUnitTest --repo ~/path/to/android-repo --module :app
	  grit compileDebugAndroidTestSources --repo ~/path/to/android-repo --module :app
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
	  grit gc --cache-dir ~/.grit/cas --max-age 720h
	  grit gc --cache-dir ~/.grit/cas --max-size 1073741824
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

	if command == "-h" || command == "--help" || command == "help" {
		return writeResponse(stdout, response{
			Success:    true,
			Command:    "help",
			DurationMs: time.Since(start).Milliseconds(),
			Result:     resultJSON(responsepayload.UsageResult{Usage: usageText}),
			PerfTiming: tracker.GetTimings(),
		}, 0, stderr)
	}

	if runner, ok := verbLookup[command]; ok {
		return runner(ctx, args[1:], stdout, stderr, tracker, start)
	}

	return writeResponse(stdout, response{
		Success:    false,
		Command:    command,
		DurationMs: time.Since(start).Milliseconds(),
		Error:      &responseError{Message: fmt.Sprintf("unknown command %q", command)},
		Result:     resultJSON(responsepayload.UsageResult{Usage: usageText}),
		PerfTiming: tracker.GetTimings(),
	}, 2, stderr)
}
