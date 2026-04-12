package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/griterr"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/remotecache"
	"github.com/kaeawc/grit/internal/responsepayload"
	"github.com/kaeawc/grit/internal/tieredcas"
	"github.com/kaeawc/grit/internal/tooldiag"
)

type actionResult struct {
	Outcome           BuildOutcome
	Err               error
	ActualRemoteBytes int64
}

type androidTestInstaller interface {
	InstallAndroidTestVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error
}

type androidTestUninstaller interface {
	UninstallAndroidTestVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error
}

func (s *Service) executeBatch(ctx context.Context, prj *project.Project, rootMod *project.Module, model *configmodel.Model, semanticGraph *graph.Graph, req BuildRequest, batchIndex int, batch []configmodel.ActionScheduleStep, stdout, stderr *os.File) ([]BuildOutcome, error) {
	if len(batch) == 0 {
		return nil, nil
	}
	if s.admissionController != nil {
		return s.executeBatchWithAdmission(ctx, prj, rootMod, model, semanticGraph, req, batchIndex, batch, stdout, stderr)
	}
	return s.executeBatchWithWorkerQueues(ctx, prj, rootMod, model, semanticGraph, req, batchIndex, batch, stdout, stderr)
}

func (s *Service) executeBatchWithAdmission(ctx context.Context, prj *project.Project, rootMod *project.Module, model *configmodel.Model, semanticGraph *graph.Graph, req BuildRequest, batchIndex int, batch []configmodel.ActionScheduleStep, stdout, stderr *os.File) ([]BuildOutcome, error) {
	results := make([]actionResult, len(batch))
	type completedAction struct {
		index  int
		result actionResult
	}
	done := make(chan completedAction, len(batch))
	pending := prioritizedBatchIndexes(batch)
	batchStart := time.Now()
	running := 0
	var batchErr error

	launch := func(i int, deferRemote bool, release bool) {
		running++
		queueWaitMs := time.Since(batchStart).Milliseconds()
		if queueWaitMs < 0 {
			queueWaitMs = 0
		}
		go func() {
			result := s.executeAction(ctx, prj, rootMod, model, semanticGraph, req, batchIndex, batch[i], queueWaitMs, deferRemote, stdout, stderr)
			if release {
				if err := s.admissionController.ReleaseWithActual(batch[i].Action.ID.String(), result.ActualRemoteBytes); err != nil {
					if result.Err == nil {
						result.Err = fmt.Errorf("release %s: %w", batch[i].Action.ID.String(), err)
					} else {
						result.Err = fmt.Errorf("%v; release %s: %v", result.Err, batch[i].Action.ID.String(), err)
					}
				}
			}
			done <- completedAction{index: i, result: result}
		}()
	}

	for len(pending) > 0 || running > 0 {
		progressed := false
		nextPending := pending[:0]
		for _, idx := range pending {
			decision := s.admissionController.TryAdmit(batch[idx])
			if decision.Admitted {
				launch(idx, decision.DeferRemote, true)
				progressed = true
				continue
			}
			nextPending = append(nextPending, idx)
		}
		pending = nextPending
		if progressed {
			continue
		}
		if running == 0 && len(pending) > 0 {
			// A tighter injected controller can make an action unadmittable even
			// though it is runnable. Preserve forward progress while still
			// consulting the network budget for the local-only fallback decision.
			idx := pending[0]
			pending = pending[1:]
			deferRemote := s.admissionController.AdmitRemoteProbe(batch[idx]).DeferRemote
			launch(idx, deferRemote, false)
			continue
		}

		completed := <-done
		running--
		results[completed.index] = completed.result
		if batchErr == nil && completed.result.Err != nil {
			batchErr = completed.result.Err
		}
	}
	return batchOutcomes(results, batchErr)
}

func (s *Service) executeBatchWithWorkerQueues(ctx context.Context, prj *project.Project, rootMod *project.Module, model *configmodel.Model, semanticGraph *graph.Graph, req BuildRequest, batchIndex int, batch []configmodel.ActionScheduleStep, stdout, stderr *os.File) ([]BuildOutcome, error) {
	results := make([]actionResult, len(batch))

	// Consult the admission controller for network budget decisions. When set,
	// the controller determines which actions should defer remote cache probes.
	deferRemoteFlags := make([]bool, len(batch))
	if s.admissionController != nil {
		for i, step := range batch {
			deferRemoteFlags[i] = s.admissionController.AdmitRemoteProbe(step).DeferRemote
		}
	}

	grouped := make(map[string][]int)
	for i, step := range batch {
		grouped[step.WorkerClass] = append(grouped[step.WorkerClass], i)
	}
	var workerClasses []string
	firstIndex := make(map[string]int, len(grouped))
	classPriority := make(map[string]int, len(grouped))
	for workerClass := range grouped {
		workerClasses = append(workerClasses, workerClass)
		if indexes := grouped[workerClass]; len(indexes) > 0 {
			firstIndex[workerClass] = indexes[0]
			classPriority[workerClass] = operationPriority(batch[indexes[0]].Action.Attributes["operation"])
		}
	}
	sort.Slice(workerClasses, func(i, j int) bool {
		if classPriority[workerClasses[i]] != classPriority[workerClasses[j]] {
			return classPriority[workerClasses[i]] < classPriority[workerClasses[j]]
		}
		if firstIndex[workerClasses[i]] != firstIndex[workerClasses[j]] {
			return firstIndex[workerClasses[i]] < firstIndex[workerClasses[j]]
		}
		return workerClasses[i] < workerClasses[j]
	})
	var wg sync.WaitGroup
	for _, workerClass := range workerClasses {
		indexes := grouped[workerClass]
		sort.SliceStable(indexes, func(i, j int) bool {
			left := batch[indexes[i]]
			right := batch[indexes[j]]
			lp := probePriority(left.ProbeHint)
			rp := probePriority(right.ProbeHint)
			if lp != rp {
				return lp < rp
			}
			if left.ResourceCost != right.ResourceCost {
				return left.ResourceCost > right.ResourceCost
			}
			return false
		})
		limit := 1
		if len(indexes) > 0 && batch[indexes[0]].MaxParallelism > 0 {
			limit = batch[indexes[0]].MaxParallelism
		}
		if limit == 1 {
			for _, idx := range indexes {
				results[idx] = s.executeAction(ctx, prj, rootMod, model, semanticGraph, req, batchIndex, batch[idx], 0, deferRemoteFlags[idx], stdout, stderr)
			}
			continue
		}
		sem := make(chan struct{}, limit)
		for _, idx := range indexes {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				queuedAt := time.Now()
				weight := batch[i].ResourceCost
				if weight <= 0 {
					weight = 1
				}
				if weight > limit {
					weight = limit
				}
				for j := 0; j < weight; j++ {
					sem <- struct{}{}
				}
				queueWaitMs := time.Since(queuedAt).Milliseconds()
				defer func() {
					for j := 0; j < weight; j++ {
						<-sem
					}
				}()
				results[i] = s.executeAction(ctx, prj, rootMod, model, semanticGraph, req, batchIndex, batch[i], queueWaitMs, deferRemoteFlags[i], stdout, stderr)
			}(idx)
		}
	}
	wg.Wait()
	return batchOutcomes(results, nil)
}

func batchOutcomes(results []actionResult, batchErr error) ([]BuildOutcome, error) {
	outcomes := make([]BuildOutcome, 0, len(results))
	for _, result := range results {
		outcomes = append(outcomes, result.Outcome)
		if batchErr == nil && result.Err != nil {
			batchErr = result.Err
		}
	}
	return outcomes, batchErr
}

func prioritizedBatchIndexes(batch []configmodel.ActionScheduleStep) []int {
	grouped := make(map[string][]int)
	for i, step := range batch {
		grouped[step.WorkerClass] = append(grouped[step.WorkerClass], i)
	}
	var workerClasses []string
	firstIndex := make(map[string]int, len(grouped))
	classPriority := make(map[string]int, len(grouped))
	for workerClass, indexes := range grouped {
		workerClasses = append(workerClasses, workerClass)
		if len(indexes) == 0 {
			continue
		}
		firstIndex[workerClass] = indexes[0]
		classPriority[workerClass] = operationPriority(batch[indexes[0]].Action.Attributes["operation"])
	}
	sort.Slice(workerClasses, func(i, j int) bool {
		if classPriority[workerClasses[i]] != classPriority[workerClasses[j]] {
			return classPriority[workerClasses[i]] < classPriority[workerClasses[j]]
		}
		if firstIndex[workerClasses[i]] != firstIndex[workerClasses[j]] {
			return firstIndex[workerClasses[i]] < firstIndex[workerClasses[j]]
		}
		return workerClasses[i] < workerClasses[j]
	})

	ordered := make([]int, 0, len(batch))
	for _, workerClass := range workerClasses {
		indexes := append([]int(nil), grouped[workerClass]...)
		sort.SliceStable(indexes, func(i, j int) bool {
			left := batch[indexes[i]]
			right := batch[indexes[j]]
			lp := probePriority(left.ProbeHint)
			rp := probePriority(right.ProbeHint)
			if lp != rp {
				return lp < rp
			}
			if left.ResourceCost != right.ResourceCost {
				return left.ResourceCost > right.ResourceCost
			}
			return false
		})
		ordered = append(ordered, indexes...)
	}
	return ordered
}

func (s *Service) executeAction(ctx context.Context, prj *project.Project, rootMod *project.Module, model *configmodel.Model, semanticGraph *graph.Graph, req BuildRequest, batchIndex int, step configmodel.ActionScheduleStep, queueWaitMs int64, deferRemote bool, stdout, stderr *os.File) actionResult {
	ctx, remoteReads := remotecache.WithReadCounter(ctx)

	// When the admission controller defers remote probes (bandwidth budget
	// exhausted), constrain the context so that tieredcas.Store methods
	// called through the generic cas.Store interface automatically limit
	// probing to local tiers only.
	if deferRemote {
		ctx = tieredcas.WithLocalOnly(ctx)
	}

	action := step.Action
	variantName := action.Attributes["variantName"]
	modulePath := action.Attributes["modulePath"]
	if modulePath == "" {
		modulePath = rootMod.Path
	}
	compiler := s.newCompiler()
	diagCollector := &tooldiag.Collector{}
	ctx = tooldiag.WithCollector(ctx, diagCollector)
	actionTracker := perf.New(true)
	compiler.SetTracker(actionTracker)
	start := time.Now()
	execution := ActionExecution{
		ActionID:        action.ID.String(),
		Name:            action.Name,
		Operation:       action.Attributes["operation"],
		ModulePath:      modulePath,
		VariantName:     variantName,
		BatchIndex:      batchIndex,
		QueueWaitMs:     queueWaitMs,
		WaitReason:      waitReasonForAction(batchIndex, queueWaitMs),
		WorkerClass:     step.WorkerClass,
		ResourceClass:   step.ResourceClass,
		ResourceCost:    step.ResourceCost,
		MaxParallelism:  step.MaxParallelism,
		CacheKey:        step.CacheKey,
		Cacheable:       step.Cacheable,
		ProbeOrder:      append([]string(nil), step.ProbeOrder...),
		ExecuteOnMiss:   step.ExecuteOnMiss,
		EstimatedBytes:  step.EstimatedBytes,
		ProbeHint:       cloneCacheProbe(step.ProbeHint),
		RetentionClass:  step.RetentionClass,
		Shareability:    step.Shareability,
		Dependencies:    stringifyActionIDs(step.Dependencies),
		InputArtifacts:  stringifyArtifactIDs(action.Inputs),
		OutputArtifacts: stringifyArtifactIDs(action.Outputs),
		DeferRemote:     deferRemote,
		Status:          "success",
	}
	actionExplanation := explain.ForGraphAction(semanticGraph, action)
	outcome := BuildOutcome{
		ExecutedActionIDs:  []string{action.ID.String()},
		ActionExecutions:   []ActionExecution{execution},
		ActionExplanations: []explain.Action{actionExplanation},
	}
	if action.Name != "" {
		outcome.ExecutedTasks = []string{action.Name}
	}
	if variantSummary, ok := model.Variant(modulePath, variantName); ok {
		outcome.Variant = variantSummary.Name
		outcome.VariantSummary = cloneVariantSummary(&variantSummary)
		outcome.Materializations = append(outcome.Materializations, cloneMaterializationSummary(variantSummary.Materialization))
	}
	switch action.Attributes["operation"] {
	case "compile":
		outcome.Compiled = true
		outcome.Message = fmt.Sprintf("%s compilation completed", variantName)
		err := compiler.CompileVariant(ctx, prj, modulePath, variantName, stdout, stderr)
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	case "install":
		outcome.Installed = true
		outcome.Message = "APK installed"
		err := compiler.InstallVariant(ctx, prj, modulePath, variantName, req.DeviceSerial, stdout, stderr)
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	case "assemble":
		outcome.Message = "APK assembled"
		err := compiler.AssembleVariant(ctx, prj, modulePath, variantName, stdout, stderr)
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	case "test":
		outcome.Tested = true
		outcome.Message = "unit tests completed"
		err := compiler.TestDebugUnit(ctx, prj, modulePath, variantName, stdout, stderr)
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	case "compile-tests":
		outcome.Compiled = true
		outcome.Message = "debug unit test sources compiled"
		err := compiler.CompileDebugUnit(ctx, prj, modulePath, variantName, stdout, stderr)
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	case "compile-android-tests":
		outcome.Compiled = true
		outcome.Message = "debug androidTest sources compiled"
		err := compiler.CompileDebugAndroidTest(ctx, prj, modulePath, variantName, stdout, stderr)
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	case "install-android-tests":
		outcome.Installed = true
		outcome.Message = "androidTest APK installed"
		installer, ok := compiler.(androidTestInstaller)
		err := fmt.Errorf("compiler does not support androidTest APK installation")
		if ok {
			err = installer.InstallAndroidTestVariant(ctx, prj, modulePath, variantName, req.DeviceSerial, stdout, stderr)
		}
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	case "uninstall-android-tests":
		outcome.Message = "androidTest APK uninstalled"
		uninstaller, ok := compiler.(androidTestUninstaller)
		err := fmt.Errorf("compiler does not support androidTest APK uninstallation")
		if ok {
			err = uninstaller.UninstallAndroidTestVariant(ctx, prj, modulePath, variantName, req.DeviceSerial, stdout, stderr)
		}
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	default:
		err := griterr.Newf(griterr.ErrUnsupported, "graph action operation %s", action.Attributes["operation"])
		attachToolDiagnostics(&outcome.ActionExecutions[0], prj.RootDir, diagCollector.Records())
		completeActionExecution(&outcome.ActionExecutions[0], &outcome.ActionExplanations[0], actionTracker.GetTimings(), start, err)
		outcome.CacheProbes = refreshOutcomeCacheProbes(outcome.ActionExecutions)
		outcome.CacheProbeRecords = refreshOutcomeCacheProbeRecords(outcome.ActionExecutions)
		return actionResult{Outcome: outcome, Err: err, ActualRemoteBytes: remoteReads.Bytes()}
	}
}

func mergeBatchOutcome(base BuildOutcome, parts []BuildOutcome) BuildOutcome {
	for _, part := range parts {
		base.ExecutedTasks = append(base.ExecutedTasks, part.ExecutedTasks...)
		base.ExecutedActionIDs = append(base.ExecutedActionIDs, part.ExecutedActionIDs...)
		base.ActionExecutions = append(base.ActionExecutions, part.ActionExecutions...)
		base.CacheProbes = append(base.CacheProbes, part.CacheProbes...)
		base.CacheProbeRecords = append(base.CacheProbeRecords, part.CacheProbeRecords...)
		base.ActionExplanations = append(base.ActionExplanations, part.ActionExplanations...)
		base.Materializations = append(base.Materializations, part.Materializations...)
		if part.Variant != "" {
			base.Variant = part.Variant
		}
		if part.VariantSummary != nil {
			base.VariantSummary = part.VariantSummary
		}
		base.Compiled = base.Compiled || part.Compiled
		base.Tested = base.Tested || part.Tested
		base.Installed = base.Installed || part.Installed
		if part.Message != "" {
			base.Message = part.Message
		}
	}
	return base
}

func stringifyActionIDs(ids []graph.ActionID) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func stringifyArtifactIDs(ids []graph.ArtifactID) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func probePriority(probe *responsepayload.CacheProbe) int {
	if probe == nil {
		return 1
	}
	switch probe.State {
	case "reused":
		return 0
	case "rebuilt", "miss", "invalidated":
		return 2
	default:
		return 1
	}
}

func operationPriority(operation string) int {
	switch operation {
	case "compile":
		return 0
	case "compile-tests":
		return 1
	case "assemble":
		return 2
	case "install":
		return 3
	case "test":
		return 4
	default:
		return 100
	}
}

func cloneCacheProbe(probe *responsepayload.CacheProbe) *responsepayload.CacheProbe {
	if probe == nil {
		return nil
	}
	clone := *probe
	return &clone
}

func completeActionExecution(execution *ActionExecution, actionExplanation *explain.Action, timings *perf.TimingData, start time.Time, err error) {
	if execution == nil {
		return
	}
	execution.DurationMs = time.Since(start).Milliseconds()
	execution.Timings = timings
	if cacheProbe := summarizeActionCache(timings, err); cacheProbe != nil {
		execution.CacheProbe = toResponseCacheProbe(execution.ActionID, cacheProbe)
	}
	execution.CacheProbeTrail = cacheProbeTrail(execution.ActionID, timings)
	if len(execution.CacheProbeTrail) == 0 && execution.CacheProbe != nil {
		execution.CacheProbeTrail = []responsepayload.CacheProbeRecord{{
			ActionID: execution.CacheProbe.ActionID,
			StepName: execution.Operation,
			Order:    0,
			State:    execution.CacheProbe.State,
			Basis:    execution.CacheProbe.Basis,
			Detail:   execution.CacheProbe.Detail,
		}}
	}
	if err != nil {
		execution.Status = "error"
		execution.Error = err.Error()
	}
	if actionExplanation != nil {
		actionExplanation.Cache = summarizeActionCache(timings, err)
	}
}

func attachToolDiagnostics(execution *ActionExecution, repoRoot string, records []tooldiag.Record) {
	if execution == nil || len(records) == 0 {
		return
	}
	execution.Diagnostics = make([]DiagnosticRecord, 0, len(records))
	for i, record := range records {
		file, sourceKind := normalizeDiagnosticFile(repoRoot, record.File, record.SourceKind)
		relatedDependency := firstNonEmpty(
			record.RelatedDependency,
			inferDependencyFromPath(record.File),
			inferDependencyFromPath(file),
		)
		execution.Diagnostics = append(execution.Diagnostics, DiagnosticRecord{
			Ordinal: i + 1,
			Fingerprint: diagnosticFingerprint(DiagnosticRecord{
				ActionID:          execution.ActionID,
				BatchIndex:        execution.BatchIndex,
				ModulePath:        execution.ModulePath,
				VariantName:       execution.VariantName,
				Tool:              firstNonEmpty(record.Tool, execution.WorkerClass, execution.Operation),
				WorkerClass:       firstNonEmpty(execution.WorkerClass),
				Operation:         firstNonEmpty(execution.Operation),
				Origin:            "tool",
				SourceKind:        firstNonEmpty(sourceKind, "tool-emitted"),
				Stream:            record.Stream,
				Severity:          record.Severity,
				Code:              record.Code,
				Category:          record.Category,
				Message:           record.Message,
				File:              file,
				Line:              record.Line,
				Column:            record.Column,
				RelatedArtifactID: firstNonEmpty(firstString(execution.OutputArtifacts), firstString(execution.InputArtifacts)),
				RelatedDependency: relatedDependency,
			}),
			ActionID:          execution.ActionID,
			BatchIndex:        execution.BatchIndex,
			ModulePath:        execution.ModulePath,
			VariantName:       execution.VariantName,
			Tool:              firstNonEmpty(record.Tool, execution.WorkerClass, execution.Operation),
			WorkerClass:       firstNonEmpty(execution.WorkerClass),
			Operation:         firstNonEmpty(execution.Operation),
			Origin:            "tool",
			SourceKind:        firstNonEmpty(sourceKind, "tool-emitted"),
			Stream:            record.Stream,
			Severity:          record.Severity,
			Code:              record.Code,
			Category:          record.Category,
			Message:           record.Message,
			File:              file,
			Line:              record.Line,
			Column:            record.Column,
			RelatedArtifactID: firstNonEmpty(firstString(execution.OutputArtifacts), firstString(execution.InputArtifacts)),
			RelatedDependency: relatedDependency,
		})
	}
}

func normalizeDiagnosticFile(repoRoot, file, fallbackKind string) (string, string) {
	file = strings.TrimSpace(strings.TrimPrefix(file, "file://"))
	if file == "" {
		return "", fallbackKind
	}
	clean := filepath.Clean(file)
	if repoRoot != "" {
		root := filepath.Clean(repoRoot)
		if rel, err := filepath.Rel(root, clean); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			kind := "workspace"
			if strings.Contains(rel, "build"+string(filepath.Separator)+"generated"+string(filepath.Separator)) {
				kind = "generated"
			}
			return rel, kind
		}
	}
	if dependency := inferDependencyFromPath(clean); dependency != "" {
		return clean, "dependency-cache"
	}
	return clean, firstNonEmpty(fallbackKind, "external")
}

func inferDependencyFromPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	if idx := strings.Index(path, "/modules-2/files-2.1/"); idx >= 0 {
		parts := strings.Split(path[idx+len("/modules-2/files-2.1/"):], "/")
		if len(parts) >= 3 {
			return parts[0] + ":" + parts[1] + ":" + parts[2]
		}
	}
	if idx := strings.Index(path, "/.grit/metadata/"); idx >= 0 {
		parts := strings.Split(path[idx+len("/.grit/metadata/"):], "/")
		if len(parts) >= 3 {
			return parts[0] + ":" + parts[1] + ":" + parts[2]
		}
	}
	if coord, ok := dependencywiring.CoordinateFromMaterializedPath(path); ok {
		return coord.Group + ":" + coord.Artifact + ":" + coord.Version
	}
	return ""
}

func waitReasonForAction(batchIndex int, queueWaitMs int64) string {
	switch {
	case queueWaitMs > 0:
		return "worker-slot"
	case batchIndex > 0:
		return "batch-order"
	default:
		return ""
	}
}

func refreshOutcomeCacheProbes(executions []ActionExecution) []responsepayload.CacheProbe {
	if len(executions) == 0 {
		return nil
	}
	out := make([]responsepayload.CacheProbe, 0, len(executions))
	for _, execution := range executions {
		if execution.CacheProbe == nil {
			continue
		}
		out = append(out, *execution.CacheProbe)
	}
	return out
}

func refreshOutcomeCacheProbeRecords(executions []ActionExecution) []responsepayload.CacheProbeRecord {
	if len(executions) == 0 {
		return nil
	}
	var out []responsepayload.CacheProbeRecord
	for _, execution := range executions {
		if len(execution.CacheProbeTrail) != 0 {
			out = append(out, execution.CacheProbeTrail...)
			continue
		}
		if execution.CacheProbe != nil {
			out = append(out, responsepayload.CacheProbeRecord{
				ActionID: execution.CacheProbe.ActionID,
				StepName: execution.Operation,
				Order:    0,
				State:    execution.CacheProbe.State,
				Basis:    execution.CacheProbe.Basis,
				Detail:   execution.CacheProbe.Detail,
			})
		}
	}
	return out
}

func toResponseCacheProbe(actionID string, timing *explain.Timing) *responsepayload.CacheProbe {
	if timing == nil {
		return nil
	}
	return &responsepayload.CacheProbe{
		ActionID: actionID,
		State:    string(timing.State),
		Basis:    timing.Basis,
		Detail:   timing.Detail,
	}
}

func cacheProbeTrail(actionID string, timings *perf.TimingData) []responsepayload.CacheProbeRecord {
	if timings == nil {
		return nil
	}
	var out []responsepayload.CacheProbeRecord
	var walk func([]perf.TimingEntry)
	walk = func(entries []perf.TimingEntry) {
		for _, entry := range entries {
			if entry.Explanation != nil && strings.HasPrefix(entry.Name, "cacheProbe") {
				stepName := ""
				if idx := strings.Index(entry.Name, ":"); idx >= 0 && idx+1 < len(entry.Name) {
					stepName = entry.Name[idx+1:]
				}
				out = append(out, responsepayload.CacheProbeRecord{
					ActionID: actionID,
					StepName: stepName,
					Order:    len(out),
					State:    string(entry.Explanation.State),
					Basis:    entry.Explanation.Basis,
					Detail:   entry.Explanation.Detail,
				})
			}
			if entry.Children != nil {
				walk(entry.Children.Entries())
			}
		}
	}
	walk(timings.Entries())
	if len(out) == 0 {
		return nil
	}
	return out
}

func recordBatchTiming(tracker perf.Tracker, index int, outcomes []BuildOutcome, durationMs int64) {
	if tracker == nil || !tracker.IsEnabled() {
		return
	}
	entries := make([]perf.TimingEntry, 0, len(outcomes))
	for _, outcome := range outcomes {
		for _, execution := range outcome.ActionExecutions {
			name := execution.Name
			if name == "" {
				name = execution.Operation
			}
			entry := perf.TimingEntry{
				Name:       name,
				DurationMs: execution.DurationMs,
				Children:   execution.Timings,
			}
			if cache := actionCacheExplanation(outcome.ActionExplanations, execution.ActionID); cache != nil {
				entry.Explanation = cache
			}
			entries = append(entries, entry)
		}
	}
	tracker.Record(perf.TimingEntry{
		Name:       fmt.Sprintf("actionBatch[%d]", index),
		DurationMs: durationMs,
		Children:   perf.Map(entries),
	})
}

func actionCacheExplanation(explanations []explain.Action, actionID string) *explain.Timing {
	for i := range explanations {
		if explanations[i].ActionID == actionID {
			return explanations[i].Cache
		}
	}
	return nil
}

func summarizeActionCache(timings *perf.TimingData, err error) *explain.Timing {
	if err != nil {
		return &explain.Timing{
			State:  explain.StateUnknown,
			Basis:  "action-error",
			Detail: err.Error(),
		}
	}
	var reused, rebuilt int
	var probeReused, probeRebuilt int
	probeDetails := make([]string, 0, 4)
	var walk func(entries []perf.TimingEntry)
	walk = func(entries []perf.TimingEntry) {
		for _, entry := range entries {
			if entry.Explanation != nil {
				isProbe := strings.HasPrefix(entry.Name, "cacheProbe")
				switch entry.Explanation.State {
				case explain.StateReused:
					reused++
					if isProbe {
						probeReused++
					}
				case explain.StateRebuilt:
					rebuilt++
					if isProbe {
						probeRebuilt++
					}
				}
				if isProbe && entry.Explanation.Detail != "" {
					probeDetails = appendIfMissingString(probeDetails, entry.Explanation.Basis+": "+entry.Explanation.Detail)
				}
			}
			if entry.Children != nil {
				walk(entry.Children.Entries())
			}
		}
	}
	if timings != nil {
		walk(timings.Entries())
	}
	if probeRebuilt > 0 || probeReused > 0 {
		state := explain.StateReused
		if probeRebuilt > 0 {
			state = explain.StateRebuilt
		}
		detail := fmt.Sprintf("%d cache hits, %d cache misses", probeReused, probeRebuilt)
		if len(probeDetails) > 0 {
			detail = detail + "; " + strings.Join(probeDetails, "; ")
		}
		return &explain.Timing{
			State:  state,
			Basis:  "cache-probes",
			Detail: detail,
		}
	}
	switch {
	case rebuilt > 0:
		return &explain.Timing{
			State:  explain.StateRebuilt,
			Basis:  "action-substeps",
			Detail: fmt.Sprintf("%d rebuilt substeps, %d reused substeps", rebuilt, reused),
		}
	case reused > 0:
		return &explain.Timing{
			State:  explain.StateReused,
			Basis:  "action-substeps",
			Detail: fmt.Sprintf("%d reused substeps", reused),
		}
	case timings != nil:
		return &explain.Timing{
			State:  explain.StateUnknown,
			Basis:  "action-substeps",
			Detail: "no cacheable substeps recorded",
		}
	default:
		return nil
	}
}

func appendIfMissingString(in []string, value string) []string {
	for _, existing := range in {
		if existing == value {
			return in
		}
	}
	return append(in, value)
}
