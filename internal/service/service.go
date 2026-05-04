package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/admission"
	"github.com/kaeawc/grit/internal/clock"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/griterr"
	"github.com/kaeawc/grit/internal/integration"
	"github.com/kaeawc/grit/internal/nativecompile"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

type Compiler interface {
	SetTracker(perf.Tracker)
	CompileVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error
	AssembleVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error
	InstallVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error
	TestDebugUnit(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error
	CompileDebugUnit(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error
	CompileDebugAndroidTest(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error
}

type Service struct {
	compiler            Compiler
	compilerFactory     func() Compiler
	models              *configmodel.Store
	hooks               []integration.Hook
	admissionController *admission.Controller
	clock               clock.Clock
}

type BuildPlan struct {
	Command                string
	TargetVariant          string
	TargetVariants         []string
	TargetVariantConfig    *project.BuildType
	TargetVariantSummary   *project.SemanticVariantSummary
	TargetResolvedVariant  project.ResolvedVariant
	TargetResolvedVariants []project.ResolvedVariant
	VariantExplicit        bool
	Actions                []graph.Action
	Schedule               configmodel.ActionSchedule
}

type BuildRequest struct {
	ModulePath       string `json:"modulePath,omitempty"`
	TaskName         string `json:"taskName,omitempty"`
	Command          string `json:"command,omitempty"`
	RequestedVariant string `json:"requestedVariant,omitempty"`
	VariantExplicit  bool   `json:"variantExplicit,omitempty"`
	DeviceSerial     string `json:"deviceSerial,omitempty"`
}

type BuildOutcome struct {
	Variant                string
	Variants               []string
	VariantConfig          *project.BuildType
	VariantSummary         *project.SemanticVariantSummary
	TargetResolvedVariant  project.ResolvedVariant
	TargetResolvedVariants []project.ResolvedVariant
	RunGraphSummary        *RunGraphSummary
	CriticalPathSummary    *CriticalPathSummary
	PlannedSchedule        *PlanScheduleResult
	CacheSummary           *CacheSummary
	SchedulerSummary       *SchedulerSummary
	ExecutedTasks          []string
	ExecutedActionIDs      []string
	ActionExecutions       []ActionExecution
	CacheProbes            []responsepayload.CacheProbe
	CacheProbeRecords      []responsepayload.CacheProbeRecord
	ActionExplanations     []explain.Action
	Message                string
	Installed              bool
	Tested                 bool
	Compiled               bool
	Materializations       []project.SemanticMaterializationSummary
	RunSummaryPath         string
}

type RunGraphSummary struct {
	ModuleID           string   `json:"moduleId,omitempty"`
	VariantID          string   `json:"variantId,omitempty"`
	MaterializationID  string   `json:"materializationId,omitempty"`
	ArtifactSnapshotID string   `json:"artifactSnapshotId,omitempty"`
	PlannedActionIDs   []string `json:"plannedActionIds,omitempty"`
	RootActionIDs      []string `json:"rootActionIds,omitempty"`
	ExecutedActionIDs  []string `json:"executedActionIds,omitempty"`
}

type CriticalPathSummary struct {
	BatchCount           int      `json:"batchCount,omitempty"`
	EstimatedDurationMs  int64    `json:"estimatedDurationMs,omitempty"`
	RepresentativeAction []string `json:"representativeActionIds,omitempty"`
}

type CacheSummary struct {
	TotalActions   int `json:"totalActions,omitempty"`
	ReusedActions  int `json:"reusedActions,omitempty"`
	RebuiltActions int `json:"rebuiltActions,omitempty"`
	UnknownActions int `json:"unknownActions,omitempty"`
}

type SchedulerSummary struct {
	ExecutedBatchCount  int                         `json:"executedBatchCount,omitempty"`
	CriticalPathActions int                         `json:"criticalPathActions,omitempty"`
	QueueWaitActions    int                         `json:"queueWaitActions,omitempty"`
	TotalQueueWaitMs    int64                       `json:"totalQueueWaitMs,omitempty"`
	MaxQueueWaitMs      int64                       `json:"maxQueueWaitMs,omitempty"`
	WaitReasonCounts    map[string]int              `json:"waitReasonCounts,omitempty"`
	CacheResultCounts   map[string]int              `json:"cacheResultCounts,omitempty"`
	WorkerClasses       []SchedulerBreakdownBucket  `json:"workerClasses,omitempty"`
	ResourceClasses     []SchedulerBreakdownBucket  `json:"resourceClasses,omitempty"`
	Bandwidth           *admission.BandwidthSummary `json:"bandwidth,omitempty"`
}

type SchedulerBreakdownBucket struct {
	Key               string         `json:"key"`
	ActionCount       int            `json:"actionCount,omitempty"`
	CriticalPathCount int            `json:"criticalPathCount,omitempty"`
	QueueWaitActions  int            `json:"queueWaitActions,omitempty"`
	TotalQueueWaitMs  int64          `json:"totalQueueWaitMs,omitempty"`
	MaxQueueWaitMs    int64          `json:"maxQueueWaitMs,omitempty"`
	WaitReasonCounts  map[string]int `json:"waitReasonCounts,omitempty"`
	CacheResultCounts map[string]int `json:"cacheResultCounts,omitempty"`
}

type ActionExecution struct {
	ActionID        string                             `json:"actionId"`
	Name            string                             `json:"name,omitempty"`
	Operation       string                             `json:"operation,omitempty"`
	ModulePath      string                             `json:"modulePath,omitempty"`
	VariantName     string                             `json:"variantName,omitempty"`
	BatchIndex      int                                `json:"batchIndex,omitempty"`
	CriticalPath    bool                               `json:"criticalPath,omitempty"`
	QueueWaitMs     int64                              `json:"queueWaitMs,omitempty"`
	WaitReason      string                             `json:"waitReason,omitempty"`
	WorkerClass     string                             `json:"workerClass,omitempty"`
	ResourceClass   string                             `json:"resourceClass,omitempty"`
	ResourceCost    int                                `json:"resourceCost,omitempty"`
	MaxParallelism  int                                `json:"maxParallelism,omitempty"`
	CacheKey        string                             `json:"cacheKey,omitempty"`
	Cacheable       bool                               `json:"cacheable,omitempty"`
	ProbeOrder      []string                           `json:"probeOrder,omitempty"`
	ExecuteOnMiss   bool                               `json:"executeOnMiss,omitempty"`
	EstimatedBytes  int64                              `json:"estimatedBytes,omitempty"`
	ProbeHint       *responsepayload.CacheProbe        `json:"probeHint,omitempty"`
	RetentionClass  string                             `json:"retentionClass,omitempty"`
	Shareability    string                             `json:"shareability,omitempty"`
	Dependencies    []string                           `json:"dependencies,omitempty"`
	InputArtifacts  []string                           `json:"inputArtifacts,omitempty"`
	OutputArtifacts []string                           `json:"outputArtifacts,omitempty"`
	DurationMs      int64                              `json:"durationMs"`
	Status          string                             `json:"status"`
	Error           string                             `json:"error,omitempty"`
	Diagnostics     []DiagnosticRecord                 `json:"diagnostics,omitempty"`
	Timings         *perf.TimingData                   `json:"timings,omitempty"`
	CacheProbe      *responsepayload.CacheProbe        `json:"cacheProbe,omitempty"`
	CacheProbeTrail []responsepayload.CacheProbeRecord `json:"cacheProbeTrail,omitempty"`

	// DeferRemote is true when the admission controller's network budget
	// denied the remote cache probe for this action. When set, the action
	// was resolved using only local cache tiers.
	DeferRemote bool `json:"deferRemote,omitempty"`

	// RemoteBytesRead is the measured remote-cache traffic observed while the
	// action executed. The configmodel runtime sidecar feeds this back into
	// later schedules to improve bandwidth admission estimates.
	RemoteBytesRead int64 `json:"remoteBytesRead,omitempty"`
}

func New() *Service {
	return &Service{
		compilerFactory: func() Compiler { return nativecompile.New() },
		models:          configmodel.NewStore(nil),
		clock:           clock.System{},
	}
}

func NewWithCompiler(compiler Compiler) *Service {
	if compiler == nil {
		compiler = nativecompile.New()
	}
	return &Service{
		compiler:        compiler,
		compilerFactory: func() Compiler { return compiler },
		models:          configmodel.NewStore(nil),
		clock:           clock.System{},
	}
}

// SetClock overrides the service's wall-clock source. Tests pass
// clock.NewFake to make WrittenAt and other persisted timestamps
// deterministic across runs.
func (s *Service) SetClock(c clock.Clock) {
	if c == nil {
		c = clock.System{}
	}
	s.clock = c
}

// SetAdmissionController attaches a runtime admission controller. When set,
// executeBatch consults it for network budget decisions and propagates the
// DeferRemote flag to individual action executions.
func (s *Service) SetAdmissionController(ac *admission.Controller) {
	s.admissionController = ac
}

func (s *Service) newCompiler() Compiler {
	if s.compilerFactory != nil {
		return s.compilerFactory()
	}
	if s.compiler != nil {
		return s.compiler
	}
	return nativecompile.New()
}

func (s *Service) LoadProject(repo string) (*project.Project, error) {
	return project.Load(repo)
}

func (s *Service) LoadConfigurationModel(ctx context.Context, prj *project.Project) (*configmodel.Model, error) {
	if s.models == nil {
		s.models = configmodel.NewStore(nil)
	}
	return s.models.LoadOrBuild(ctx, prj)
}

func (s *Service) RegisterHook(h integration.Hook) {
	if h == nil {
		return
	}
	s.hooks = append(s.hooks, h)
}

func (s *Service) RequireModule(prj *project.Project, path string) (*project.Module, error) {
	if err := project.RequireModule(prj, path); err != nil {
		return nil, err
	}
	return prj.FindModule(path), nil
}

func (s *Service) ResolveBuildPlan(mod *project.Module, command string, requestedVariant string, variantExplicit bool) BuildPlan {
	targetVariant := strings.TrimSpace(requestedVariant)
	if targetVariant == "" {
		if mod.IsJVM() {
			targetVariant = mod.DefaultVariantName()
		} else if commandUsesDebugVariant(command) {
			targetVariant = "debug"
		} else if commandUsesReleaseVariant(command) {
			targetVariant = "release"
		} else {
			targetVariant = mod.DefaultVariantName()
		}
	}
	targetVariants := []string{targetVariant}
	if commandUsesAllModuleVariants(command) && !variantExplicit {
		targetVariants = moduleVariantNames(mod)
		if len(targetVariants) == 0 {
			targetVariants = []string{"debug"}
		}
	}
	targetResolved := mod.ResolveVariant(targetVariant)
	resolvedTargets := mod.ResolveVariants(targetVariants)
	variantConfig := mod.Variant(targetResolved.Name)
	return BuildPlan{
		Command:                command,
		TargetVariant:          targetResolved.Name,
		TargetVariants:         targetVariants,
		TargetVariantConfig:    &variantConfig,
		TargetResolvedVariant:  targetResolved,
		TargetResolvedVariants: resolvedTargets,
		VariantExplicit:        variantExplicit,
	}
}

func (s *Service) Build(ctx context.Context, prj *project.Project, mod *project.Module, req BuildRequest, stdout, stderr *os.File, tracker perf.Tracker) (BuildOutcome, error) {
	var model *configmodel.Model
	err := tracker.Track("loadConfigurationModel", func() error {
		var loadErr error
		model, loadErr = s.LoadConfigurationModel(ctx, prj)
		return loadErr
	})
	if err != nil {
		return BuildOutcome{}, err
	}
	var plan BuildPlan
	err = tracker.Track("resolveExecutionPlan", func() error {
		var planErr error
		plan, planErr = s.resolveExecutionPlanWithModel(ctx, model, prj, mod, req.Command, req.RequestedVariant, req.VariantExplicit)
		return planErr
	})
	if err != nil {
		return BuildOutcome{}, err
	}
	semanticGraph, err := model.Graph()
	if err != nil {
		return BuildOutcome{}, err
	}
	outcome := BuildOutcome{
		Variant:                plan.TargetResolvedVariant.Name,
		Variants:               append([]string{}, plan.TargetVariants...),
		VariantConfig:          plan.TargetVariantConfig,
		VariantSummary:         cloneVariantSummary(plan.TargetVariantSummary),
		TargetResolvedVariant:  plan.TargetResolvedVariant,
		TargetResolvedVariants: append([]project.ResolvedVariant(nil), plan.TargetResolvedVariants...),
	}
	s.finalizeRunSummaryState(&outcome, plan)
	switch req.Command {
	case "clean":
		outcome.Message = "build outputs cleaned"
		outcome.ExecutedTasks = []string{"clean"}
		err := cleanOutputs(prj, mod)
		outcome.RunSummaryPath = persistRunSummary(prj.RootDir, mod.Path, req, outcome, tracker.GetTimings(), err, s.clock.Now())
		return outcome, err
	case "uninstallDebug", "uninstallRelease", "uninstallAll":
		outcome.ExecutedTasks = []string{req.Command}
		outcome.Message = "application uninstalled"
		err := uninstallApplication(ctx, mod, req.DeviceSerial)
		outcome.RunSummaryPath = persistRunSummary(prj.RootDir, mod.Path, req, outcome, tracker.GetTimings(), err, s.clock.Now())
		return outcome, err
	}
	restoreAdmission := s.installScheduleAdmissionController(plan.Schedule)
	defer restoreAdmission()

	results, executeErr := s.executeSchedule(ctx, prj, mod, model, semanticGraph, req, plan.Schedule, stdout, stderr, tracker)
	if len(results) == 0 && len(plan.Actions) > 0 {
		var executionBatches [][]configmodel.ActionScheduleStep
		for _, action := range plan.Actions {
			executionBatches = append(executionBatches, []configmodel.ActionScheduleStep{{Action: action}})
		}
		for i, batch := range executionBatches {
			batchStart := time.Now()
			fallbackResults, batchErr := s.executeBatch(ctx, prj, mod, model, semanticGraph, req, i, batch, stdout, stderr)
			recordBatchTiming(tracker, i, fallbackResults, time.Since(batchStart).Milliseconds())
			results = append(results, fallbackResults...)
			if batchErr != nil && executeErr == nil {
				executeErr = batchErr
				break
			}
		}
	}
	outcome = mergeBatchOutcome(outcome, results)
	if executeErr != nil {
		s.finalizeRunSummaryState(&outcome, plan)
		if s.models != nil {
			_ = s.models.RecordRuntimeObservations(prj.RootDir, model.CacheKey(), runtimeObservationsFromExecutions(outcome.ActionExecutions))
		}
		outcome.RunSummaryPath = persistRunSummary(prj.RootDir, mod.Path, req, outcome, tracker.GetTimings(), executeErr, s.clock.Now())
		return outcome, executeErr
	}
	s.finalizeRunSummaryState(&outcome, plan)
	if s.models != nil {
		_ = s.models.RecordRuntimeObservations(prj.RootDir, model.CacheKey(), runtimeObservationsFromExecutions(outcome.ActionExecutions))
	}
	outcome.RunSummaryPath = persistRunSummary(prj.RootDir, mod.Path, req, outcome, tracker.GetTimings(), nil, s.clock.Now())
	switch req.Command {
	case "compile-debug", "compileDebugSources", "compileReleaseSources":
		outcome.Compiled = true
		return outcome, nil
	case "install", "install-debug", "installDebug", "installRelease":
		outcome.Installed = true
		return outcome, nil
	case "assemble-debug", "assembleDebug", "assemble-release", "assembleRelease", "assemble":
		return outcome, nil
	case "build", "buildNeeded":
		outcome.Tested = true
		outcome.Message = "build completed"
		return outcome, nil
	case "buildDependents":
		outcome.Message = "dependent modules built"
		return outcome, nil
	case "test-debug-unit", "testDebugUnitTest", "test", "check":
		outcome.Tested = true
		return outcome, nil
	case "compileDebugUnitTestSources", "assembleUnitTest":
		outcome.Compiled = true
		return outcome, nil
	case "compileDebugAndroidTestSources", "assembleAndroidTest":
		outcome.Compiled = true
		return outcome, nil
	case "installDebugAndroidTest", "install-android-tests":
		outcome.Installed = true
		return outcome, nil
	case "uninstallDebugAndroidTest", "uninstall-android-tests":
		return outcome, nil
	default:
		return outcome, griterr.Newf(griterr.ErrUnsupported, "command %s", req.Command)
	}
}

// installScheduleAdmissionController wires a schedule-derived controller into
// the service for the duration of a single build or direct execution call.
// Externally injected controllers are preserved; only auto-created controllers
// are removed on restore so bandwidth state does not leak across builds.
func (s *Service) installScheduleAdmissionController(schedule configmodel.ActionSchedule) func() {
	if s == nil || s.admissionController != nil {
		return func() {}
	}
	s.admissionController = admission.NewControllerFromSchedule(schedule)
	return func() {
		s.admissionController = nil
	}
}

func runtimeObservationsFromExecutions(executions []ActionExecution) []configmodel.RuntimeActionObservation {
	if len(executions) == 0 {
		return nil
	}
	out := make([]configmodel.RuntimeActionObservation, 0, len(executions))
	for _, execution := range executions {
		if execution.ActionID == "" {
			continue
		}
		observation := configmodel.RuntimeActionObservation{
			ActionID:        execution.ActionID,
			RemoteBytesRead: execution.RemoteBytesRead,
		}
		if execution.CacheProbe != nil {
			probe := *execution.CacheProbe
			observation.CacheProbe = &probe
		}
		out = append(out, observation)
	}
	return out
}

func (s *Service) ResolveExecutionPlan(prj *project.Project, mod *project.Module, command string, requestedVariant string, variantExplicit bool) (BuildPlan, error) {
	model, err := s.LoadConfigurationModel(context.Background(), prj)
	if err != nil {
		return BuildPlan{}, err
	}
	return s.resolveExecutionPlanWithModel(context.Background(), model, prj, mod, command, requestedVariant, variantExplicit)
}

func (s *Service) resolveExecutionPlanWithModel(ctx context.Context, model *configmodel.Model, prj *project.Project, mod *project.Module, command string, requestedVariant string, variantExplicit bool) (BuildPlan, error) {
	plan := s.ResolveBuildPlan(mod, command, requestedVariant, variantExplicit)
	view := integration.NewModelView(model)
	if commandUsesAllModuleVariants(command) && !variantExplicit {
		resolvedVariants, resolvedErr := model.ResolvedVariants(mod.Path)
		if resolvedErr != nil {
			return BuildPlan{}, resolvedErr
		}
		plan.TargetVariants = resolvedVariantNames(resolvedVariants)
		plan.TargetResolvedVariants = append([]project.ResolvedVariant(nil), resolvedVariants...)
		if len(plan.TargetVariants) > 0 {
			plan.TargetResolvedVariant = plan.TargetResolvedVariants[0]
			plan.TargetVariant = plan.TargetResolvedVariant.Name
			variantConfig := mod.Variant(plan.TargetResolvedVariant.Name)
			plan.TargetVariantConfig = &variantConfig
		}
	} else if resolvedVariant, ok := model.ResolvedVariant(mod.Path, plan.TargetResolvedVariant.Name); ok {
		plan.TargetResolvedVariant = resolvedVariant
		if len(plan.TargetResolvedVariants) == 0 {
			plan.TargetResolvedVariants = []project.ResolvedVariant{resolvedVariant}
		} else {
			for i := range plan.TargetResolvedVariants {
				if plan.TargetResolvedVariants[i].Name == resolvedVariant.Name {
					plan.TargetResolvedVariants[i] = resolvedVariant
				}
			}
		}
	}
	if variantSummary, ok := model.Variant(mod.Path, plan.TargetResolvedVariant.Name); ok {
		plan.TargetVariantSummary = cloneVariantSummary(&variantSummary)
	}
	for _, hook := range s.hooks {
		if err := hook.BeforePlan(ctx, integration.PlanRequest{
			Command:          command,
			ModulePath:       mod.Path,
			RequestedVariant: requestedVariant,
			VariantExplicit:  variantExplicit,
		}, view); err != nil {
			return BuildPlan{}, err
		}
	}
	actions, err := model.ActionsForResolvedCommand(mod.Path, mod.Type, plan.Command, plan.TargetResolvedVariants)
	if err != nil {
		return BuildPlan{}, err
	}
	plan.Schedule = model.ScheduleActions(actions)
	plan.Actions = plan.Schedule.OrderedActions()
	for _, hook := range s.hooks {
		if err := hook.AfterPlan(ctx, integration.PlanResult{
			Command:       plan.Command,
			ModulePath:    mod.Path,
			TargetVariant: plan.TargetVariant,
			Variants:      append([]string(nil), plan.TargetVariants...),
			Actions:       append([]graph.Action(nil), plan.Actions...),
		}, view); err != nil {
			return BuildPlan{}, err
		}
	}
	return plan, nil
}

func commandUsesDebugVariant(command string) bool {
	switch command {
	case "assemble-debug", "assembleDebug", "compile-debug", "compileDebugSources", "install-debug", "installDebug", "uninstallDebug", "test-debug-unit", "testDebugUnitTest", "compileDebugUnitTestSources", "compileDebugAndroidTestSources", "assembleAndroidTest", "installDebugAndroidTest", "install-android-tests", "uninstallDebugAndroidTest", "uninstall-android-tests", "check":
		return true
	default:
		return false
	}
}

func (s *Service) finalizeRunSummaryState(outcome *BuildOutcome, plan BuildPlan) {
	if outcome == nil {
		return
	}
	markCriticalPathExecutions(outcome, plan)
	outcome.RunGraphSummary = buildRunGraphSummary(plan, *outcome)
	outcome.CriticalPathSummary = buildCriticalPathSummary(plan, *outcome)
	schedule := toPlanScheduleResult(plan.Schedule)
	outcome.PlannedSchedule = &schedule
	outcome.CacheSummary = buildCacheSummary(*outcome)
	outcome.SchedulerSummary = buildSchedulerSummary(*outcome, s.admissionController)
}

func markCriticalPathExecutions(outcome *BuildOutcome, plan BuildPlan) {
	if outcome == nil || len(outcome.ActionExecutions) == 0 || len(plan.Schedule.Batches) == 0 {
		return
	}
	durationByAction := make(map[string]int64, len(outcome.ActionExecutions))
	executionByAction := make(map[string]*ActionExecution, len(outcome.ActionExecutions))
	for i := range outcome.ActionExecutions {
		execution := &outcome.ActionExecutions[i]
		execution.CriticalPath = false
		durationByAction[execution.ActionID] = execution.DurationMs
		executionByAction[execution.ActionID] = execution
	}
	for _, batch := range plan.Schedule.Batches {
		var batchMax int64
		var representative []string
		for _, step := range batch {
			actionID := step.Action.ID.String()
			duration := durationByAction[actionID]
			if duration > batchMax {
				batchMax = duration
				representative = representative[:0]
				representative = append(representative, actionID)
				continue
			}
			if duration == batchMax {
				representative = append(representative, actionID)
			}
		}
		for _, actionID := range representative {
			if execution, ok := executionByAction[actionID]; ok {
				execution.CriticalPath = true
			}
		}
	}
}

func buildRunGraphSummary(plan BuildPlan, outcome BuildOutcome) *RunGraphSummary {
	var summary RunGraphSummary
	if plan.TargetVariantSummary != nil {
		summary.VariantID = plan.TargetVariantSummary.ID
		summary.MaterializationID = plan.TargetVariantSummary.Materialization.ID
		summary.ArtifactSnapshotID = plan.TargetVariantSummary.Materialization.ArtifactSnapshotID
	}
	for _, explanation := range outcome.ActionExplanations {
		if summary.ModuleID == "" && explanation.ModuleID != "" {
			summary.ModuleID = explanation.ModuleID
		}
		if summary.VariantID == "" && explanation.VariantID != "" {
			summary.VariantID = explanation.VariantID
		}
	}
	summary.PlannedActionIDs = plannedActionIDs(plan.Actions)
	summary.RootActionIDs = rootActionIDs(plan.Schedule)
	summary.ExecutedActionIDs = append([]string(nil), outcome.ExecutedActionIDs...)
	if summary.ModuleID == "" &&
		summary.VariantID == "" &&
		summary.MaterializationID == "" &&
		summary.ArtifactSnapshotID == "" &&
		len(summary.PlannedActionIDs) == 0 &&
		len(summary.RootActionIDs) == 0 &&
		len(summary.ExecutedActionIDs) == 0 {
		return nil
	}
	return &summary
}

func buildCriticalPathSummary(plan BuildPlan, outcome BuildOutcome) *CriticalPathSummary {
	if len(plan.Schedule.Batches) == 0 {
		return nil
	}
	durationByAction := make(map[string]int64, len(outcome.ActionExecutions))
	for _, execution := range outcome.ActionExecutions {
		durationByAction[execution.ActionID] = execution.DurationMs
	}
	summary := &CriticalPathSummary{BatchCount: len(plan.Schedule.Batches)}
	for _, batch := range plan.Schedule.Batches {
		var batchMax int64
		var representative string
		for _, step := range batch {
			duration := durationByAction[step.Action.ID.String()]
			if duration >= batchMax {
				batchMax = duration
				representative = step.Action.ID.String()
			}
		}
		summary.EstimatedDurationMs += batchMax
		if representative != "" {
			summary.RepresentativeAction = append(summary.RepresentativeAction, representative)
		}
	}
	if summary.BatchCount == 0 && summary.EstimatedDurationMs == 0 && len(summary.RepresentativeAction) == 0 {
		return nil
	}
	return summary
}

func buildCacheSummary(outcome BuildOutcome) *CacheSummary {
	if len(outcome.ActionExecutions) == 0 {
		return nil
	}
	summary := &CacheSummary{TotalActions: len(outcome.ActionExecutions)}
	for _, execution := range outcome.ActionExecutions {
		state := ""
		if execution.CacheProbe != nil {
			state = strings.TrimSpace(execution.CacheProbe.State)
		}
		switch state {
		case "reused", "hit":
			summary.ReusedActions++
		case "rebuilt", "miss", "invalidated":
			summary.RebuiltActions++
		default:
			summary.UnknownActions++
		}
	}
	return summary
}

func buildSchedulerSummary(outcome BuildOutcome, controller *admission.Controller) *SchedulerSummary {
	if len(outcome.ActionExecutions) == 0 {
		return nil
	}
	summary := &SchedulerSummary{
		WaitReasonCounts:  map[string]int{},
		CacheResultCounts: map[string]int{},
	}
	workerBuckets := map[string]*SchedulerBreakdownBucket{}
	resourceBuckets := map[string]*SchedulerBreakdownBucket{}
	seenBatches := map[int]struct{}{}
	for _, execution := range outcome.ActionExecutions {
		seenBatches[execution.BatchIndex] = struct{}{}
		if execution.CriticalPath {
			summary.CriticalPathActions++
		}
		cacheResult := schedulerCacheResult(execution)
		summary.CacheResultCounts[cacheResult]++
		if execution.QueueWaitMs > 0 {
			summary.QueueWaitActions++
			summary.TotalQueueWaitMs += execution.QueueWaitMs
			if execution.QueueWaitMs > summary.MaxQueueWaitMs {
				summary.MaxQueueWaitMs = execution.QueueWaitMs
			}
		}
		if reason := strings.TrimSpace(execution.WaitReason); reason != "" {
			summary.WaitReasonCounts[reason]++
		}
		accumulateSchedulerBreakdown(workerBuckets, strings.TrimSpace(execution.WorkerClass), execution, cacheResult)
		accumulateSchedulerBreakdown(resourceBuckets, strings.TrimSpace(execution.ResourceClass), execution, cacheResult)
	}
	summary.ExecutedBatchCount = len(seenBatches)
	if len(summary.WaitReasonCounts) == 0 {
		summary.WaitReasonCounts = nil
	}
	if len(summary.CacheResultCounts) == 0 {
		summary.CacheResultCounts = nil
	}
	summary.WorkerClasses = materializeSchedulerBreakdowns(workerBuckets)
	summary.ResourceClasses = materializeSchedulerBreakdowns(resourceBuckets)
	summary.Bandwidth = buildBandwidthSummary(outcome.ActionExecutions, controller)
	return summary
}

func buildBandwidthSummary(executions []ActionExecution, controller *admission.Controller) *admission.BandwidthSummary {
	if controller == nil {
		return nil
	}
	summary := controller.BandwidthSummary()
	if summary == nil {
		return nil
	}
	for _, execution := range executions {
		if execution.Cacheable {
			summary.TotalCacheableActions++
		}
		if execution.DeferRemote {
			summary.DeferredActions++
			summary.EstimatedBytesSaved += execution.EstimatedBytes
		}
	}
	return summary
}

func schedulerCacheResult(execution ActionExecution) string {
	if execution.CacheProbe != nil {
		if state := strings.TrimSpace(execution.CacheProbe.State); state != "" {
			return state
		}
	}
	switch strings.ToLower(strings.TrimSpace(execution.Status)) {
	case "reused", "hit":
		return "reused"
	case "rebuilt", "miss", "invalidated":
		return "rebuilt"
	case "error":
		return "error"
	default:
		return "unknown"
	}
}

func accumulateSchedulerBreakdown(buckets map[string]*SchedulerBreakdownBucket, key string, execution ActionExecution, cacheResult string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	bucket, ok := buckets[key]
	if !ok {
		bucket = &SchedulerBreakdownBucket{
			Key:               key,
			WaitReasonCounts:  map[string]int{},
			CacheResultCounts: map[string]int{},
		}
		buckets[key] = bucket
	}
	bucket.ActionCount++
	if execution.CriticalPath {
		bucket.CriticalPathCount++
	}
	bucket.CacheResultCounts[cacheResult]++
	if execution.QueueWaitMs > 0 {
		bucket.QueueWaitActions++
		bucket.TotalQueueWaitMs += execution.QueueWaitMs
		if execution.QueueWaitMs > bucket.MaxQueueWaitMs {
			bucket.MaxQueueWaitMs = execution.QueueWaitMs
		}
	}
	if reason := strings.TrimSpace(execution.WaitReason); reason != "" {
		bucket.WaitReasonCounts[reason]++
	}
}

func materializeSchedulerBreakdowns(buckets map[string]*SchedulerBreakdownBucket) []SchedulerBreakdownBucket {
	if len(buckets) == 0 {
		return nil
	}
	out := make([]SchedulerBreakdownBucket, 0, len(buckets))
	for _, bucket := range buckets {
		item := *bucket
		if len(item.WaitReasonCounts) == 0 {
			item.WaitReasonCounts = nil
		}
		if len(item.CacheResultCounts) == 0 {
			item.CacheResultCounts = nil
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func plannedActionIDs(actions []graph.Action) []string {
	if len(actions) == 0 {
		return nil
	}
	out := make([]string, 0, len(actions))
	for _, action := range actions {
		out = append(out, action.ID.String())
	}
	return out
}

func rootActionIDs(schedule configmodel.ActionSchedule) []string {
	if len(schedule.Steps) == 0 {
		return nil
	}
	out := make([]string, 0, len(schedule.Steps))
	for _, step := range schedule.Steps {
		if len(schedule.Dependencies[step.Action.ID]) == 0 {
			out = append(out, step.Action.ID.String())
		}
	}
	return out
}

func commandUsesReleaseVariant(command string) bool {
	switch command {
	case "assemble-release", "assembleRelease", "compileReleaseSources", "installRelease", "uninstallRelease":
		return true
	default:
		return false
	}
}

func commandUsesAllModuleVariants(command string) bool {
	switch command {
	case "assemble", "build", "buildNeeded", "buildDependents":
		return true
	default:
		return false
	}
}

func resolvedVariantNames(variants []project.ResolvedVariant) []string {
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		if name := strings.TrimSpace(variant.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func cleanOutputs(prj *project.Project, mod *project.Module) error {
	if mod == nil {
		return os.RemoveAll(filepath.Join(prj.RootDir, "build", "grit"))
	}
	return os.RemoveAll(filepath.Join(prj.RootDir, "build", "grit", strings.TrimPrefix(strings.ReplaceAll(mod.Path, ":", string(os.PathSeparator)), string(os.PathSeparator))))
}

func cloneVariantSummary(v *project.SemanticVariantSummary) *project.SemanticVariantSummary {
	if v == nil {
		return nil
	}
	copy := *v
	copy.Flavors = append([]string(nil), v.Flavors...)
	copy.Coordinate.Flavors = append([]string(nil), v.Coordinate.Flavors...)
	copy.Materialization = cloneMaterializationSummary(v.Materialization)
	return &copy
}

func cloneMaterializationSummary(m project.SemanticMaterializationSummary) project.SemanticMaterializationSummary {
	m.ClasspathSnapshotIDs = append([]string(nil), m.ClasspathSnapshotIDs...)
	m.SourceRoots = append([]string(nil), m.SourceRoots...)
	return m
}

func uninstallApplication(ctx context.Context, mod *project.Module, deviceSerial string) error {
	packageName := mod.ApplicationID
	if packageName == "" {
		packageName = mod.Namespace
	}
	if packageName == "" {
		return fmt.Errorf("module %s does not declare an applicationId or namespace", mod.Path)
	}
	args := []string{}
	if deviceSerial != "" {
		args = append(args, "-s", deviceSerial)
	}
	args = append(args, "uninstall", packageName)
	cmd := exec.CommandContext(ctx, "adb", args...)
	return cmd.Run()
}
