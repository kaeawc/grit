package cli

import (
	"context"
	"flag"
	"io"
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
	modulePath := fs.String("module", ":app", "Android module path")
	variant := fs.String("variant", "", "Build variant name")
	deviceSerial := fs.String("device", "", "ADB device serial")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}

	prj, err := cmd.loadProject(*repo)
	if err != nil {
		return cmd.fail(1, err)
	}
	mod, err := cmd.requireModule(prj, *modulePath)
	if err != nil {
		return cmd.fail(1, err)
	}

	logCapture, err := newLogCapture()
	if err != nil {
		return cmd.fail(1, err)
	}
	defer logCapture.Close()

	tracker = tracker.Serial("execute")
	outcome, runErr := cmd.svc.Build(ctx, prj, mod, service.BuildRequest{
		Command:          command,
		RequestedVariant: *variant,
		VariantExplicit:  hasOption(args, "--variant"),
		DeviceSerial:     *deviceSerial,
	}, logCapture.Stdout, logCapture.Stderr, tracker)
	tracker = tracker.End()

	logs := logCapture.Logs()
	result := nativeResult{
		Repo:                   prj.RootDir,
		Module:                 *modulePath,
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
	resp := response{
		Success:    runErr == nil,
		Command:    cmd.name,
		DurationMs: time.Since(start).Milliseconds(),
		Result:     resultJSON(result),
		Logs:       &logs,
		PerfTiming: tracker.GetTimings(),
	}
	if runErr != nil {
		resp.Error = &responseError{Message: runErr.Error()}
		return writeResponse(stdout, resp, 1, stderr)
	}
	return writeResponse(stdout, resp, 0, stderr)
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
