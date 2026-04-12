package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

type RunSummaryRecord struct {
	Command                string                                   `json:"command"`
	ModulePath             string                                   `json:"modulePath"`
	Success                bool                                     `json:"success"`
	Error                  string                                   `json:"error,omitempty"`
	Variant                string                                   `json:"variant,omitempty"`
	Variants               []string                                 `json:"variants,omitempty"`
	TargetResolvedVariant  *project.ResolvedVariant                 `json:"targetResolvedVariant,omitempty"`
	TargetResolvedVariants []project.ResolvedVariant                `json:"targetResolvedVariants,omitempty"`
	RunGraphSummary        *RunGraphSummary                         `json:"runGraphSummary,omitempty"`
	CriticalPathSummary    *CriticalPathSummary                     `json:"criticalPathSummary,omitempty"`
	PlannedSchedule        *PlanScheduleResult                      `json:"plannedSchedule,omitempty"`
	CacheSummary           *CacheSummary                            `json:"cacheSummary,omitempty"`
	SchedulerSummary       *SchedulerSummary                        `json:"schedulerSummary,omitempty"`
	ToolSummary            *ToolSummary                             `json:"toolSummary,omitempty"`
	DiagnosticSummary      *DiagnosticSummary                       `json:"diagnosticSummary,omitempty"`
	ExecutedTasks          []string                                 `json:"executedTasks,omitempty"`
	ActionExecutions       []ActionExecution                        `json:"actionExecutions,omitempty"`
	ActionExplanations     []explain.Action                         `json:"actionExplanations,omitempty"`
	Diagnostics            []DiagnosticRecord                       `json:"diagnostics,omitempty"`
	Materializations       []project.SemanticMaterializationSummary `json:"materializations,omitempty"`
	CacheProbes            []responsepayload.CacheProbe             `json:"cacheProbes,omitempty"`
	CacheProbeRecords      []responsepayload.CacheProbeRecord       `json:"cacheProbeRecords,omitempty"`
	PerfTiming             *perf.TimingData                         `json:"perfTiming,omitempty"`
	WrittenAt              string                                   `json:"writtenAt"`
}

type RunSummaryEntry struct {
	Path       string `json:"path"`
	ModulePath string `json:"modulePath,omitempty"`
	Command    string `json:"command,omitempty"`
	Success    bool   `json:"success"`
	Variant    string `json:"variant,omitempty"`
	WrittenAt  string `json:"writtenAt,omitempty"`
}

type ToolSummary struct {
	Operations      []ToolSummaryBucket `json:"operations,omitempty"`
	WorkerClasses   []ToolSummaryBucket `json:"workerClasses,omitempty"`
	ResourceClasses []ToolSummaryBucket `json:"resourceClasses,omitempty"`
}

type ToolSummaryBucket struct {
	Key               string         `json:"key"`
	ActionCount       int            `json:"actionCount,omitempty"`
	TotalDurationMs   int64          `json:"totalDurationMs,omitempty"`
	TotalQueueWaitMs  int64          `json:"totalQueueWaitMs,omitempty"`
	CriticalPathCount int            `json:"criticalPathCount,omitempty"`
	StatusCounts      map[string]int `json:"statusCounts,omitempty"`
	CacheResultCounts map[string]int `json:"cacheResultCounts,omitempty"`
}

type DiagnosticRecord struct {
	Ordinal           int    `json:"ordinal,omitempty"`
	Fingerprint       string `json:"fingerprint,omitempty"`
	ActionID          string `json:"actionId,omitempty"`
	BatchIndex        int    `json:"batchIndex,omitempty"`
	ModulePath        string `json:"modulePath,omitempty"`
	VariantName       string `json:"variantName,omitempty"`
	Tool              string `json:"tool,omitempty"`
	WorkerClass       string `json:"workerClass,omitempty"`
	Operation         string `json:"operation,omitempty"`
	Origin            string `json:"origin,omitempty"`
	SourceKind        string `json:"sourceKind,omitempty"`
	Stream            string `json:"stream,omitempty"`
	Severity          string `json:"severity,omitempty"`
	Code              string `json:"code,omitempty"`
	Category          string `json:"category,omitempty"`
	Message           string `json:"message,omitempty"`
	File              string `json:"file,omitempty"`
	Line              int    `json:"line,omitempty"`
	Column            int    `json:"column,omitempty"`
	RelatedArtifactID string `json:"relatedArtifactId,omitempty"`
	RelatedDependency string `json:"relatedDependency,omitempty"`
}

type DiagnosticSummary struct {
	Total               int                       `json:"total,omitempty"`
	BySeverity          []DiagnosticSummaryBucket `json:"bySeverity,omitempty"`
	ByCode              []DiagnosticSummaryBucket `json:"byCode,omitempty"`
	ByCategory          []DiagnosticSummaryBucket `json:"byCategory,omitempty"`
	ByTool              []DiagnosticSummaryBucket `json:"byTool,omitempty"`
	ByOrigin            []DiagnosticSummaryBucket `json:"byOrigin,omitempty"`
	BySource            []DiagnosticSummaryBucket `json:"bySource,omitempty"`
	ByStream            []DiagnosticSummaryBucket `json:"byStream,omitempty"`
	ByOperation         []DiagnosticSummaryBucket `json:"byOperation,omitempty"`
	ByWorkerClass       []DiagnosticSummaryBucket `json:"byWorkerClass,omitempty"`
	ByFile              []DiagnosticSummaryBucket `json:"byFile,omitempty"`
	ByRelatedDependency []DiagnosticSummaryBucket `json:"byRelatedDependency,omitempty"`
}

type DiagnosticSummaryBucket struct {
	Key   string `json:"key"`
	Count int    `json:"count,omitempty"`
}

func persistRunSummary(rootDir, modulePath string, req BuildRequest, outcome BuildOutcome, timings *perf.TimingData, buildErr error) string {
	if strings.TrimSpace(rootDir) == "" {
		return ""
	}
	var targetResolvedVariant *project.ResolvedVariant
	if outcome.TargetResolvedVariant.Name != "" || outcome.TargetResolvedVariant.ModulePath != "" {
		cloned := outcome.TargetResolvedVariant
		targetResolvedVariant = &cloned
	}
	summary := RunSummaryRecord{
		Command:                req.Command,
		ModulePath:             modulePath,
		Success:                buildErr == nil,
		Variant:                outcome.Variant,
		Variants:               append([]string(nil), outcome.Variants...),
		TargetResolvedVariant:  targetResolvedVariant,
		TargetResolvedVariants: append([]project.ResolvedVariant(nil), outcome.TargetResolvedVariants...),
		RunGraphSummary:        cloneRunGraphSummary(outcome.RunGraphSummary),
		CriticalPathSummary:    cloneCriticalPathSummary(outcome.CriticalPathSummary),
		PlannedSchedule:        clonePlanScheduleResult(outcome.PlannedSchedule),
		CacheSummary:           cloneCacheSummary(outcome.CacheSummary),
		SchedulerSummary:       cloneSchedulerSummary(outcome.SchedulerSummary),
		ToolSummary:            summarizeTooling(outcome.ActionExecutions, outcome.ActionExplanations),
		DiagnosticSummary:      summarizeDiagnostics(outcome.ActionExecutions, buildErr),
		ExecutedTasks:          append([]string(nil), outcome.ExecutedTasks...),
		ActionExecutions:       append([]ActionExecution(nil), outcome.ActionExecutions...),
		ActionExplanations:     append([]explain.Action(nil), outcome.ActionExplanations...),
		Diagnostics:            collectDiagnostics(outcome.ActionExecutions, buildErr),
		Materializations:       append([]project.SemanticMaterializationSummary(nil), outcome.Materializations...),
		CacheProbes:            append([]responsepayload.CacheProbe(nil), outcome.CacheProbes...),
		CacheProbeRecords:      append([]responsepayload.CacheProbeRecord(nil), outcome.CacheProbeRecords...),
		PerfTiming:             timings,
		WrittenAt:              time.Now().UTC().Format(time.RFC3339),
	}
	if buildErr != nil {
		summary.Error = buildErr.Error()
	}
	path := runSummaryPath(rootDir, modulePath, req.Command)
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return ""
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ""
	}
	return path
}

func sanitizeRunSummaryComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = strings.ReplaceAll(value, ":", "_")
	value = strings.ReplaceAll(value, string(os.PathSeparator), "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func runSummaryPath(rootDir, modulePath, command string) string {
	return filepath.Join(rootDir, "build", "grit", "run-summaries", sanitizeRunSummaryComponent(modulePath), sanitizeRunSummaryComponent(command)+".json")
}

func readRunSummary(path string) (RunSummaryRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RunSummaryRecord{}, err
	}
	var summary RunSummaryRecord
	if err := json.Unmarshal(data, &summary); err != nil {
		return RunSummaryRecord{}, err
	}
	return summary, nil
}

func listRunSummaryEntries(rootDir, modulePath string) ([]RunSummaryEntry, error) {
	base := filepath.Join(rootDir, "build", "grit", "run-summaries")
	if strings.TrimSpace(modulePath) != "" {
		base = filepath.Join(base, sanitizeRunSummaryComponent(modulePath))
	}
	if _, err := os.Stat(base); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []RunSummaryEntry
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		summary, readErr := readRunSummary(path)
		if readErr != nil {
			return fmt.Errorf("read run summary %s: %w", path, readErr)
		}
		entries = append(entries, RunSummaryEntry{
			Path:       path,
			ModulePath: summary.ModulePath,
			Command:    summary.Command,
			Success:    summary.Success,
			Variant:    summary.Variant,
			WrittenAt:  summary.WrittenAt,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ModulePath != entries[j].ModulePath {
			return entries[i].ModulePath < entries[j].ModulePath
		}
		if entries[i].Command != entries[j].Command {
			return entries[i].Command < entries[j].Command
		}
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

func cloneRunGraphSummary(summary *RunGraphSummary) *RunGraphSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	cloned.PlannedActionIDs = append([]string(nil), summary.PlannedActionIDs...)
	cloned.RootActionIDs = append([]string(nil), summary.RootActionIDs...)
	cloned.ExecutedActionIDs = append([]string(nil), summary.ExecutedActionIDs...)
	return &cloned
}

func cloneCriticalPathSummary(summary *CriticalPathSummary) *CriticalPathSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	cloned.RepresentativeAction = append([]string(nil), summary.RepresentativeAction...)
	return &cloned
}

func clonePlanScheduleResult(summary *PlanScheduleResult) *PlanScheduleResult {
	if summary == nil {
		return nil
	}
	cloned := &PlanScheduleResult{
		ResourceBudgets: append([]PlanResourceBudget(nil), summary.ResourceBudgets...),
		Batches:         make([]PlanScheduleBatch, 0, len(summary.Batches)),
	}
	if summary.NetworkBudgetConfig != nil {
		cfg := *summary.NetworkBudgetConfig
		cloned.NetworkBudgetConfig = &cfg
	}
	for _, batch := range summary.Batches {
		cloned.Batches = append(cloned.Batches, PlanScheduleBatch{
			Actions:   append([]InspectPlannedAction(nil), batch.Actions...),
			Resources: append([]PlanResourceUsage(nil), batch.Resources...),
		})
	}
	return cloned
}

func cloneCacheSummary(summary *CacheSummary) *CacheSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	return &cloned
}

func cloneSchedulerSummary(summary *SchedulerSummary) *SchedulerSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	if len(summary.WaitReasonCounts) > 0 {
		cloned.WaitReasonCounts = make(map[string]int, len(summary.WaitReasonCounts))
		for key, value := range summary.WaitReasonCounts {
			cloned.WaitReasonCounts[key] = value
		}
	}
	if len(summary.CacheResultCounts) > 0 {
		cloned.CacheResultCounts = make(map[string]int, len(summary.CacheResultCounts))
		for key, value := range summary.CacheResultCounts {
			cloned.CacheResultCounts[key] = value
		}
	}
	if len(summary.WorkerClasses) > 0 {
		cloned.WorkerClasses = cloneSchedulerBreakdownBuckets(summary.WorkerClasses)
	}
	if len(summary.ResourceClasses) > 0 {
		cloned.ResourceClasses = cloneSchedulerBreakdownBuckets(summary.ResourceClasses)
	}
	return &cloned
}

func cloneSchedulerBreakdownBuckets(buckets []SchedulerBreakdownBucket) []SchedulerBreakdownBucket {
	if len(buckets) == 0 {
		return nil
	}
	out := make([]SchedulerBreakdownBucket, 0, len(buckets))
	for _, bucket := range buckets {
		cloned := bucket
		if len(bucket.WaitReasonCounts) > 0 {
			cloned.WaitReasonCounts = make(map[string]int, len(bucket.WaitReasonCounts))
			for key, value := range bucket.WaitReasonCounts {
				cloned.WaitReasonCounts[key] = value
			}
		}
		if len(bucket.CacheResultCounts) > 0 {
			cloned.CacheResultCounts = make(map[string]int, len(bucket.CacheResultCounts))
			for key, value := range bucket.CacheResultCounts {
				cloned.CacheResultCounts[key] = value
			}
		}
		out = append(out, cloned)
	}
	return out
}

func cloneToolSummary(summary *ToolSummary) *ToolSummary {
	if summary == nil {
		return nil
	}
	return &ToolSummary{
		Operations:      cloneToolSummaryBuckets(summary.Operations),
		WorkerClasses:   cloneToolSummaryBuckets(summary.WorkerClasses),
		ResourceClasses: cloneToolSummaryBuckets(summary.ResourceClasses),
	}
}

func cloneDiagnosticSummary(summary *DiagnosticSummary) *DiagnosticSummary {
	if summary == nil {
		return nil
	}
	return &DiagnosticSummary{
		Total:               summary.Total,
		BySeverity:          cloneDiagnosticSummaryBuckets(summary.BySeverity),
		ByCode:              cloneDiagnosticSummaryBuckets(summary.ByCode),
		ByCategory:          cloneDiagnosticSummaryBuckets(summary.ByCategory),
		ByTool:              cloneDiagnosticSummaryBuckets(summary.ByTool),
		ByOrigin:            cloneDiagnosticSummaryBuckets(summary.ByOrigin),
		BySource:            cloneDiagnosticSummaryBuckets(summary.BySource),
		ByStream:            cloneDiagnosticSummaryBuckets(summary.ByStream),
		ByOperation:         cloneDiagnosticSummaryBuckets(summary.ByOperation),
		ByWorkerClass:       cloneDiagnosticSummaryBuckets(summary.ByWorkerClass),
		ByFile:              cloneDiagnosticSummaryBuckets(summary.ByFile),
		ByRelatedDependency: cloneDiagnosticSummaryBuckets(summary.ByRelatedDependency),
	}
}

func cloneDiagnosticSummaryBuckets(in []DiagnosticSummaryBucket) []DiagnosticSummaryBucket {
	if len(in) == 0 {
		return nil
	}
	out := make([]DiagnosticSummaryBucket, 0, len(in))
	for _, item := range in {
		out = append(out, item)
	}
	return out
}

func cloneToolSummaryBuckets(in []ToolSummaryBucket) []ToolSummaryBucket {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolSummaryBucket, 0, len(in))
	for _, item := range in {
		cloned := item
		if len(item.StatusCounts) != 0 {
			cloned.StatusCounts = cloneStringIntMap(item.StatusCounts)
		}
		if len(item.CacheResultCounts) != 0 {
			cloned.CacheResultCounts = cloneStringIntMap(item.CacheResultCounts)
		}
		out = append(out, cloned)
	}
	return out
}

func cloneStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func summarizeTooling(executions []ActionExecution, explanations []explain.Action) *ToolSummary {
	if len(executions) == 0 {
		return nil
	}
	cacheByAction := make(map[string]string, len(explanations))
	for _, item := range explanations {
		if item.ActionID == "" || item.Cache == nil || item.Cache.State == "" {
			continue
		}
		cacheByAction[item.ActionID] = string(item.Cache.State)
	}
	buildBuckets := func(keyFn func(ActionExecution) string) []ToolSummaryBucket {
		buckets := map[string]*ToolSummaryBucket{}
		for _, execution := range executions {
			key := strings.TrimSpace(keyFn(execution))
			if key == "" {
				continue
			}
			bucket := buckets[key]
			if bucket == nil {
				bucket = &ToolSummaryBucket{
					Key:               key,
					StatusCounts:      map[string]int{},
					CacheResultCounts: map[string]int{},
				}
				buckets[key] = bucket
			}
			bucket.ActionCount++
			bucket.TotalDurationMs += execution.DurationMs
			bucket.TotalQueueWaitMs += execution.QueueWaitMs
			if execution.CriticalPath {
				bucket.CriticalPathCount++
			}
			if status := strings.TrimSpace(execution.Status); status != "" {
				bucket.StatusCounts[status]++
			}
			cacheResult := ""
			if execution.CacheProbe != nil && strings.TrimSpace(execution.CacheProbe.State) != "" {
				cacheResult = execution.CacheProbe.State
			} else {
				cacheResult = cacheByAction[execution.ActionID]
			}
			if cacheResult != "" {
				bucket.CacheResultCounts[cacheResult]++
			}
		}
		if len(buckets) == 0 {
			return nil
		}
		out := make([]ToolSummaryBucket, 0, len(buckets))
		for _, item := range buckets {
			if len(item.StatusCounts) == 0 {
				item.StatusCounts = nil
			}
			if len(item.CacheResultCounts) == 0 {
				item.CacheResultCounts = nil
			}
			out = append(out, *item)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
		return out
	}
	return &ToolSummary{
		Operations:      buildBuckets(func(execution ActionExecution) string { return execution.Operation }),
		WorkerClasses:   buildBuckets(func(execution ActionExecution) string { return execution.WorkerClass }),
		ResourceClasses: buildBuckets(func(execution ActionExecution) string { return execution.ResourceClass }),
	}
}

func collectDiagnostics(executions []ActionExecution, buildErr error) []DiagnosticRecord {
	var diagnostics []DiagnosticRecord
	for _, execution := range executions {
		if len(execution.Diagnostics) != 0 {
			for _, diagnostic := range execution.Diagnostics {
				diagnostic.ActionID = firstNonEmpty(diagnostic.ActionID, execution.ActionID)
				diagnostic.BatchIndex = execution.BatchIndex
				diagnostic.ModulePath = firstNonEmpty(diagnostic.ModulePath, execution.ModulePath)
				diagnostic.VariantName = firstNonEmpty(diagnostic.VariantName, execution.VariantName)
				diagnostic.Tool = firstNonEmpty(diagnostic.Tool, execution.WorkerClass, execution.Operation)
				diagnostic.WorkerClass = firstNonEmpty(diagnostic.WorkerClass, execution.WorkerClass)
				diagnostic.Operation = firstNonEmpty(diagnostic.Operation, execution.Operation)
				diagnostic.Origin = firstNonEmpty(diagnostic.Origin, "tool")
				diagnostic.SourceKind = firstNonEmpty(diagnostic.SourceKind, "tool-emitted")
				diagnostic.RelatedArtifactID = firstNonEmpty(diagnostic.RelatedArtifactID, firstString(execution.OutputArtifacts), firstString(execution.InputArtifacts))
				diagnostics = append(diagnostics, diagnostic)
			}
			continue
		}
		message := strings.TrimSpace(execution.Error)
		if message == "" && strings.EqualFold(strings.TrimSpace(execution.Status), "success") {
			continue
		}
		if message == "" {
			message = "action failed"
		}
		diagnostics = append(diagnostics, DiagnosticRecord{
			BatchIndex:        execution.BatchIndex,
			ActionID:          execution.ActionID,
			ModulePath:        execution.ModulePath,
			VariantName:       execution.VariantName,
			Tool:              firstNonEmpty(execution.WorkerClass, execution.Operation),
			WorkerClass:       execution.WorkerClass,
			Operation:         execution.Operation,
			Origin:            "action-failure",
			SourceKind:        "synthesized",
			Severity:          "error",
			Code:              "action_execution_failed",
			Category:          firstNonEmpty(execution.Operation, "execution"),
			Message:           message,
			RelatedArtifactID: firstNonEmpty(firstString(execution.OutputArtifacts), firstString(execution.InputArtifacts)),
		})
	}
	if len(diagnostics) == 0 && buildErr != nil {
		diagnostics = append(diagnostics, DiagnosticRecord{
			Origin:     "build-failure",
			SourceKind: "synthesized",
			Severity:   "error",
			Code:       "build_failed",
			Category:   "build",
			Message:    buildErr.Error(),
		})
	}
	return normalizeDiagnostics(diagnostics)
}

func normalizeDiagnostics(diagnostics []DiagnosticRecord) []DiagnosticRecord {
	if len(diagnostics) == 0 {
		return nil
	}
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left := diagnostics[i]
		right := diagnostics[j]
		switch {
		case left.BatchIndex != right.BatchIndex:
			return left.BatchIndex < right.BatchIndex
		case left.ActionID != right.ActionID:
			return left.ActionID < right.ActionID
		case left.Ordinal != right.Ordinal:
			return left.Ordinal < right.Ordinal
		case left.File != right.File:
			return left.File < right.File
		case left.Line != right.Line:
			return left.Line < right.Line
		case left.Column != right.Column:
			return left.Column < right.Column
		case left.Origin != right.Origin:
			return left.Origin < right.Origin
		case left.SourceKind != right.SourceKind:
			return left.SourceKind < right.SourceKind
		case left.Stream != right.Stream:
			return left.Stream < right.Stream
		case left.Tool != right.Tool:
			return left.Tool < right.Tool
		case left.Code != right.Code:
			return left.Code < right.Code
		case left.Category != right.Category:
			return left.Category < right.Category
		case left.RelatedDependency != right.RelatedDependency:
			return left.RelatedDependency < right.RelatedDependency
		case left.Severity != right.Severity:
			return left.Severity < right.Severity
		default:
			return left.Message < right.Message
		}
	})
	for i := range diagnostics {
		diagnostics[i].Ordinal = i + 1
		diagnostics[i].Fingerprint = diagnosticFingerprint(diagnostics[i])
	}
	return diagnostics
}

func summarizeDiagnostics(executions []ActionExecution, buildErr error) *DiagnosticSummary {
	diagnostics := collectDiagnostics(executions, buildErr)
	return summarizeNormalizedDiagnostics(diagnostics)
}

func summarizeNormalizedDiagnostics(diagnostics []DiagnosticRecord) *DiagnosticSummary {
	if len(diagnostics) == 0 {
		return nil
	}
	buildBuckets := func(keyFn func(DiagnosticRecord) string) []DiagnosticSummaryBucket {
		buckets := map[string]int{}
		for _, diagnostic := range diagnostics {
			key := strings.TrimSpace(keyFn(diagnostic))
			if key == "" {
				continue
			}
			buckets[key]++
		}
		if len(buckets) == 0 {
			return nil
		}
		out := make([]DiagnosticSummaryBucket, 0, len(buckets))
		for key, count := range buckets {
			out = append(out, DiagnosticSummaryBucket{Key: key, Count: count})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
		return out
	}
	return &DiagnosticSummary{
		Total:               len(diagnostics),
		BySeverity:          buildBuckets(func(d DiagnosticRecord) string { return d.Severity }),
		ByCode:              buildBuckets(func(d DiagnosticRecord) string { return d.Code }),
		ByCategory:          buildBuckets(func(d DiagnosticRecord) string { return d.Category }),
		ByTool:              buildBuckets(func(d DiagnosticRecord) string { return d.Tool }),
		ByOrigin:            buildBuckets(func(d DiagnosticRecord) string { return d.Origin }),
		BySource:            buildBuckets(func(d DiagnosticRecord) string { return d.SourceKind }),
		ByStream:            buildBuckets(func(d DiagnosticRecord) string { return d.Stream }),
		ByOperation:         buildBuckets(func(d DiagnosticRecord) string { return d.Operation }),
		ByWorkerClass:       buildBuckets(func(d DiagnosticRecord) string { return d.WorkerClass }),
		ByFile:              buildBuckets(func(d DiagnosticRecord) string { return d.File }),
		ByRelatedDependency: buildBuckets(func(d DiagnosticRecord) string { return d.RelatedDependency }),
	}
}

func diagnosticFingerprint(d DiagnosticRecord) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		d.Origin,
		d.SourceKind,
		d.Stream,
		d.Tool,
		d.WorkerClass,
		d.Operation,
		d.Severity,
		d.Code,
		d.Category,
		d.File,
		fmt.Sprintf("%d", d.Line),
		fmt.Sprintf("%d", d.Column),
		d.RelatedDependency,
		d.Message,
	}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
