package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
	"github.com/kaeawc/grit/internal/service"
)

func runNativeBuild(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time, command string) int {
	cmd := newCommandState(command, stdout, stderr, tracker, start)
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", ".", "Path to repository root")
	modulePath := fs.String("module", "", moduleFlagUsage)
	allModules := fs.Bool("all-modules", false, allModulesFlagUsage)
	variant := fs.String("variant", "", "Build variant name")
	deviceSerial := fs.String("device", "", "ADB device serial")
	discoveryMode := fs.String("discovery", "hybrid", "Generated-source discovery mode: static, hybrid, or snapshot")
	allowMavenCentralFallback := fs.Bool("allow-maven-central-fallback", false, "Allow implicit Google/Maven Central fallback when no declared dependency repository matches")
	offline := fs.Bool("offline", false, "Fail instead of fetching dependency metadata or artifacts from remote repositories")
	refreshDiscovery := fs.Bool("refresh-discovery", false, "Refresh cached Gradle discovery snapshots before compiling")
	timeout := fs.Duration("timeout", 0, "Cancel the build after this duration (0 = unbounded). Returns a structured timeout error naming the last-active phase.")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if *discoveryMode != "static" && *discoveryMode != "hybrid" && *discoveryMode != "snapshot" {
		return cmd.fail(2, fmt.Errorf("invalid --discovery %q (want static, hybrid, or snapshot)", *discoveryMode))
	}
	if *allModules && strings.TrimSpace(*modulePath) != "" {
		return cmd.fail(2, fmt.Errorf("--all-modules conflicts with --module"))
	}
	if *timeout < 0 {
		return cmd.fail(2, fmt.Errorf("--timeout must be non-negative"))
	}
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	prj.DiscoveryMode = *discoveryMode
	prj.AllowMavenCentralFallback = *allowMavenCentralFallback
	prj.Offline = *offline
	prj.RefreshDiscovery = *refreshDiscovery
	if err := project.RefreshDiscoverySnapshot(ctx, prj, stderr); err != nil {
		return cmd.fail(1, err)
	}
	if snapshot, err := project.LoadDiscoverySnapshot(prj); err == nil {
		project.ApplyDiscoverySnapshot(prj, snapshot)
	} else {
		return cmd.fail(1, err)
	}

	req := service.BuildRequest{
		Command:          command,
		RequestedVariant: *variant,
		VariantExplicit:  hasOption(args, "--variant"),
		DeviceSerial:     *deviceSerial,
	}

	if *allModules {
		paths := prj.AllModulePaths()
		if len(paths) == 0 {
			return cmd.fail(1, fmt.Errorf("no modules discovered; check settings.gradle.kts"))
		}
		return runNativeBuildAllModules(ctx, cmd, prj, paths, req, *timeout, tracker, start)
	}

	resolvedModule, err := resolveModulePath(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, resolvedModule)
	if err != nil {
		return cmd.fail(1, err)
	}

	logCapture, err := newLogCapture()
	if err != nil {
		return cmd.fail(1, err)
	}
	defer logCapture.Close()

	tracker = tracker.Serial("execute")
	outcome, runErr := cmd.svc.Build(ctx, prj, mod, req, logCapture.Stdout, logCapture.Stderr, tracker)
	tracker = tracker.End()

	logs := logCapture.Logs()
	result := nativeResultFromOutcome(prj.RootDir, resolvedModule, outcome, logs)
	resp := response{
		Success:    runErr == nil,
		Command:    cmd.name,
		DurationMs: time.Since(start).Milliseconds(),
		Result:     resultJSON(result),
		Logs:       &logs,
		PerfTiming: tracker.GetTimings(),
	}
	if runErr != nil {
		resp.Error = &responseError{Message: timeoutAwareErrorMessage(ctx, *timeout, runErr)}
		return writeResponse(stdout, resp, 1, stderr)
	}
	return writeResponse(stdout, resp, 0, stderr)
}

// timeoutAwareErrorMessage rewrites a downstream build error into a clear
// "build timed out" diagnostic when the cancellation came from the
// caller-supplied --timeout. The original error is preserved as a cause
// so debug output still has the underlying signal.
func timeoutAwareErrorMessage(ctx context.Context, timeout time.Duration, runErr error) string {
	if runErr == nil {
		return ""
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && timeout > 0 {
		return fmt.Sprintf("%s (see perfTiming for last-recorded phases): %s", timeoutPrefix(timeout), runErr.Error())
	}
	return runErr.Error()
}

func timeoutPrefix(timeout time.Duration) string {
	return fmt.Sprintf("build timed out after %s", timeout)
}

func nativeResultFromOutcome(repoDir, modulePath string, outcome service.BuildOutcome, logs responseLogs) nativeResult {
	return nativeResult{
		Repo:                   repoDir,
		Module:                 modulePath,
		Variant:                outcome.Variant,
		Variants:               append([]string{}, outcome.Variants...),
		VariantConfig:          outcome.VariantConfig,
		VariantSummary:         outcome.VariantSummary,
		TargetResolvedVariant:  cloneResolvedVariant(outcome.TargetResolvedVariant),
		TargetResolvedVariants: append([]project.ResolvedVariant(nil), outcome.TargetResolvedVariants...),
		RunGraphSummary:        cloneRunGraphSummary(outcome.RunGraphSummary),
		CriticalPathSummary:    cloneCriticalPathSummary(outcome.CriticalPathSummary),
		PlannedSchedule:        clonePlanScheduleResult(outcome.PlannedSchedule),
		CacheSummary:           cloneCacheSummary(outcome.CacheSummary),
		SchedulerSummary:       cloneSchedulerSummary(outcome.SchedulerSummary),
		Materializations:       append([]project.SemanticMaterializationSummary(nil), outcome.Materializations...),
		ActionExecutions:       append([]service.ActionExecution(nil), outcome.ActionExecutions...),
		CacheProbes:            append([]responsepayload.CacheProbe(nil), outcome.CacheProbes...),
		CacheProbeRecords:      append([]responsepayload.CacheProbeRecord(nil), outcome.CacheProbeRecords...),
		ActionExplanations:     append([]explain.Action(nil), outcome.ActionExplanations...),
		ExecutedTasks:          append([]string{}, outcome.ExecutedTasks...),
		Message:                outcome.Message,
		Installed:              outcome.Installed,
		Tested:                 outcome.Tested,
		Compiled:               outcome.Compiled,
		RunSummaryPath:         outcome.RunSummaryPath,
		APKPath:                parseAPKPath(logs.Stdout),
	}
}

func runNativeBuildAllModules(ctx context.Context, cmd commandState, prj *project.Project, paths []string, req service.BuildRequest, timeout time.Duration, tracker perf.Tracker, start time.Time) int {
	combinedLogs, err := newLogCapture()
	if err != nil {
		return cmd.fail(1, err)
	}
	defer combinedLogs.Close()

	execTracker := tracker.Serial("execute")
	results := make([]moduleBuildResult, 0, len(paths))
	failed := make([]string, 0)
	var firstErr error
	for _, path := range paths {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if firstErr == nil {
				firstErr = ctxErr
			}
			break
		}
		mod, modErr := cmd.requireModule(prj, path)
		if modErr != nil {
			results = append(results, moduleBuildResult{
				Module:  path,
				Success: false,
				Error:   modErr.Error(),
			})
			failed = append(failed, path)
			if firstErr == nil {
				firstErr = modErr
			}
			continue
		}
		outcome, buildErr := cmd.svc.Build(ctx, prj, mod, req, combinedLogs.Stdout, combinedLogs.Stderr, execTracker)
		entry := moduleBuildResult{
			Module:  path,
			Success: buildErr == nil,
			Result:  nativeResultFromOutcome(prj.RootDir, path, outcome, responseLogs{}),
		}
		if buildErr != nil {
			entry.Error = buildErr.Error()
			failed = append(failed, path)
			if firstErr == nil {
				firstErr = buildErr
			}
		}
		results = append(results, entry)
	}
	execTracker = execTracker.End()

	logs := combinedLogs.Logs()
	resultPayload := multiModuleResult{
		Repo:    prj.RootDir,
		Modules: results,
		Failed:  failed,
	}
	resp := response{
		Success:    firstErr == nil,
		Command:    cmd.name,
		DurationMs: time.Since(start).Milliseconds(),
		Result:     resultJSON(resultPayload),
		Logs:       &logs,
		PerfTiming: execTracker.GetTimings(),
	}
	if firstErr != nil {
		summary := fmt.Sprintf("%d of %d modules failed", len(failed), len(paths))
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && timeout > 0 {
			succeeded := len(results) - len(failed)
			summary = fmt.Sprintf("%s; %d of %d modules completed before deadline", timeoutPrefix(timeout), succeeded, len(paths))
		}
		resp.Error = &responseError{Message: summary}
		return writeResponse(cmd.stdout, resp, 1, cmd.stderr)
	}
	return writeResponse(cmd.stdout, resp, 0, cmd.stderr)
}

type moduleBuildResult struct {
	Module  string       `json:"module"`
	Success bool         `json:"success"`
	Error   string       `json:"error,omitempty"`
	Result  nativeResult `json:"result"`
}

type multiModuleResult struct {
	Repo    string              `json:"repo"`
	Modules []moduleBuildResult `json:"modules"`
	Failed  []string            `json:"failed,omitempty"`
}

func cloneResolvedVariant(variant project.ResolvedVariant) *project.ResolvedVariant {
	if variant.Name == "" && variant.ModulePath == "" {
		return nil
	}
	cloned := variant
	return &cloned
}

func cloneRunGraphSummary(summary *service.RunGraphSummary) *service.RunGraphSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	cloned.PlannedActionIDs = append([]string(nil), summary.PlannedActionIDs...)
	cloned.RootActionIDs = append([]string(nil), summary.RootActionIDs...)
	cloned.ExecutedActionIDs = append([]string(nil), summary.ExecutedActionIDs...)
	return &cloned
}

func cloneCriticalPathSummary(summary *service.CriticalPathSummary) *service.CriticalPathSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	cloned.RepresentativeAction = append([]string(nil), summary.RepresentativeAction...)
	return &cloned
}

func clonePlanScheduleResult(summary *service.PlanScheduleResult) *service.PlanScheduleResult {
	if summary == nil {
		return nil
	}
	cloned := &service.PlanScheduleResult{
		ResourceBudgets: append([]service.PlanResourceBudget(nil), summary.ResourceBudgets...),
		Batches:         make([]service.PlanScheduleBatch, 0, len(summary.Batches)),
	}
	for _, batch := range summary.Batches {
		cloned.Batches = append(cloned.Batches, service.PlanScheduleBatch{
			Actions:   append([]service.InspectPlannedAction(nil), batch.Actions...),
			Resources: append([]service.PlanResourceUsage(nil), batch.Resources...),
		})
	}
	return cloned
}

func cloneCacheSummary(summary *service.CacheSummary) *service.CacheSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	return &cloned
}

func cloneSchedulerSummary(summary *service.SchedulerSummary) *service.SchedulerSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	if summary.WaitReasonCounts != nil {
		cloned.WaitReasonCounts = make(map[string]int, len(summary.WaitReasonCounts))
		for key, value := range summary.WaitReasonCounts {
			cloned.WaitReasonCounts[key] = value
		}
	}
	return &cloned
}
