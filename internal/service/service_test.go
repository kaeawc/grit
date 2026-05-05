package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/admission"
	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/clock"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/integration"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/remotecache"
	"github.com/kaeawc/grit/internal/responsepayload"
	"github.com/kaeawc/grit/internal/testsupport"
	"github.com/kaeawc/grit/internal/testutil"
	"github.com/kaeawc/grit/internal/tooldiag"
)

type androidTestCompilerStub struct {
	calls []string
}

type remoteReadCompiler struct {
	*testsupport.CompilerRecorder
	client *remotecache.Client
	hash   cas.Hash
	reads  []bool

	mu    sync.Mutex
	index int
}

func (f *androidTestCompilerStub) SetTracker(perf.Tracker) {}
func (f *androidTestCompilerStub) CompileVariant(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *androidTestCompilerStub) AssembleVariant(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *androidTestCompilerStub) InstallVariant(context.Context, *project.Project, string, string, string, *os.File, *os.File) error {
	return nil
}
func (f *androidTestCompilerStub) TestDebugUnit(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *androidTestCompilerStub) CompileDebugUnit(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *androidTestCompilerStub) CompileDebugAndroidTest(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *androidTestCompilerStub) InstallAndroidTestVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error {
	f.calls = append(f.calls, fmt.Sprintf("install-android-tests:%s:%s:%s", modulePath, variantName, deviceSerial))
	return nil
}
func (f *androidTestCompilerStub) UninstallAndroidTestVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, deviceSerial string, stdout, stderr *os.File) error {
	f.calls = append(f.calls, fmt.Sprintf("uninstall-android-tests:%s:%s:%s", modulePath, variantName, deviceSerial))
	return nil
}

func (f *remoteReadCompiler) CompileVariant(ctx context.Context, prj *project.Project, modulePath string, variantName string, stdout, stderr *os.File) error {
	if err := f.CompilerRecorder.CompileVariant(ctx, prj, modulePath, variantName, stdout, stderr); err != nil {
		return err
	}

	f.mu.Lock()
	index := f.index
	f.index++
	shouldRead := index < len(f.reads) && f.reads[index]
	f.mu.Unlock()
	if !shouldRead {
		return nil
	}

	_, err := f.client.GetBlob(ctx, f.hash)
	return err
}

func TestResolveBuildPlanPrefersCommandSemantics(t *testing.T) {
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	mod := testsupport.Module(":app", "android-application", "debug", "release")
	plan := svc.ResolveBuildPlan(&mod, "assemble", "debug", false)
	if plan.TargetVariant != "debug" {
		t.Fatalf("expected debug target variant, got %q", plan.TargetVariant)
	}
	if got, want := len(plan.TargetVariants), 2; got != want {
		t.Fatalf("unexpected target variant count: got %d want %d", got, want)
	}
	if plan.TargetVariantConfig == nil || plan.TargetVariantConfig.Name != "debug" {
		t.Fatalf("expected debug variant config, got %#v", plan.TargetVariantConfig)
	}
	if plan.TargetResolvedVariant.ModulePath != ":app" || plan.TargetResolvedVariant.Name != "debug" {
		t.Fatalf("expected resolved variant coordinate, got %#v", plan.TargetResolvedVariant)
	}
}

func TestResolveExecutionPlanUsesSemanticVariantMetadata(t *testing.T) {
	root := t.TempDir()
	prj := &project.Project{
		RootDir: root,
		Name:    "ServiceTest",
		Modules: []project.Module{
			{
				Path:       ":app",
				Dir:        filepath.Join(root, "app"),
				BuildFile:  filepath.Join(root, "app", "build.gradle.kts"),
				Type:       "android-application",
				BuildTypes: map[string]project.BuildType{"debug": {Name: "debug"}, "release": {Name: "release"}},
			},
		},
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	plan, err := svc.ResolveExecutionPlan(prj, mod, "assemble", "debug", false)
	if err != nil {
		t.Fatalf("ResolveExecutionPlan returned error: %v", err)
	}
	if plan.TargetVariantSummary == nil || plan.TargetVariantSummary.Materialization.ID == "" {
		t.Fatalf("expected target semantic variant summary, got %#v", plan.TargetVariantSummary)
	}
	if plan.TargetResolvedVariant.ModulePath != ":app" || plan.TargetResolvedVariant.Name != "debug" {
		t.Fatalf("expected resolved variant coordinate, got %#v", plan.TargetResolvedVariant)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("expected 2 graph actions, got %#v", plan.Actions)
	}
	if len(plan.Schedule.Steps) != len(plan.Actions) {
		t.Fatalf("expected scheduled action steps to match actions, got %#v", plan.Schedule)
	}
	if plan.Actions[0].ID == "" || plan.Actions[0].Attributes["operation"] == "" || plan.Actions[0].Attributes["materialization"] == "" {
		t.Fatalf("expected graph-backed action metadata, got %#v", plan.Actions[0])
	}
}

func TestResolveExecutionPlanPreservesFlavoredDebugVariant(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFile(t, root, "settings.gradle.kts", "include(\":app\")\n")
	testutil.WriteFile(t, root, "build.gradle.kts", "plugins {}\n")
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	prj := &project.Project{
		RootDir:       root,
		Name:          "ServiceTest",
		SettingsFile:  filepath.Join(root, "settings.gradle.kts"),
		RootBuildFile: filepath.Join(root, "build.gradle.kts"),
		Modules: []project.Module{{
			Path:             ":app",
			Dir:              filepath.Join(root, "app"),
			BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
			Type:             "android-application",
			FlavorDimensions: []string{"tier"},
			ProductFlavors: map[string]project.ProductFlavor{
				"free": {Name: "free", Dimension: "tier"},
				"paid": {Name: "paid", Dimension: "tier"},
			},
			BuildTypes: map[string]project.BuildType{
				"debug":   {Name: "debug"},
				"release": {Name: "release"},
			},
		}},
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	plan, err := svc.ResolveExecutionPlan(prj, mod, "testDebugUnitTest", "freeDebug", true)
	if err != nil {
		t.Fatalf("ResolveExecutionPlan returned error: %v", err)
	}
	if plan.TargetResolvedVariant.Name != "freeDebug" || plan.TargetResolvedVariant.Coordinate.BuildType != "debug" {
		t.Fatalf("expected flavored debug resolved variant, got %#v", plan.TargetResolvedVariant)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("expected planned actions for flavored debug variant")
	}
	for _, action := range plan.Actions {
		if action.Attributes["variantName"] != "freeDebug" {
			t.Fatalf("expected flavored debug actions, got %#v", plan.Actions)
		}
	}
}

func TestBuildRoutesToCompiler(t *testing.T) {
	root := t.TempDir()
	fake := &testsupport.CompilerRecorder{Diagnostics: []tooldiag.Record{
		{
			Tool:     "kotlinc",
			Severity: "warning",
			Code:     "kotlinc_warning",
			Category: "kotlinc",
			Message:  "unused variable",
			File:     filepath.Join(root, "build", "generated", "ksp", "debug", "kotlin", "App_Generated.kt"),
			Line:     7,
			Column:   3,
		},
		{
			Tool:     "kotlinc",
			Severity: "error",
			Code:     "kotlinc_error",
			Category: "kotlinc",
			Message:  "unresolved reference: missingSymbol",
			File:     "/home/test/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash/okhttp-4.12.0.jar",
			Line:     9,
			Column:   11,
		},
	}}
	svc := NewWithCompiler(fake)
	fixedNow := time.Date(2026, 5, 4, 17, 40, 23, 0, time.UTC)
	svc.SetClock(clock.NewFake(fixedNow))
	prj := testsupport.Project(root, testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	outcome, err := svc.Build(context.Background(), prj, mod, BuildRequest{
		Command:          "compile-debug",
		RequestedVariant: "debug",
		VariantExplicit:  false,
	}, os.Stdout, os.Stderr, perf.New(true))
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	if !outcome.Compiled {
		t.Fatalf("expected compiled outcome")
	}
	if outcome.VariantSummary == nil || outcome.VariantSummary.Materialization.ID == "" {
		t.Fatalf("expected first-class variant/materialization in outcome, got %#v", outcome)
	}
	if outcome.TargetResolvedVariant.ModulePath != ":app" || outcome.TargetResolvedVariant.Name != "debug" {
		t.Fatalf("expected resolved build outcome variant, got %#v", outcome.TargetResolvedVariant)
	}
	if outcome.RunGraphSummary == nil || outcome.RunGraphSummary.MaterializationID == "" || len(outcome.RunGraphSummary.PlannedActionIDs) == 0 || len(outcome.RunGraphSummary.ExecutedActionIDs) != 1 {
		t.Fatalf("expected run graph summary on outcome, got %#v", outcome.RunGraphSummary)
	}
	if outcome.CriticalPathSummary == nil || outcome.CriticalPathSummary.BatchCount == 0 || len(outcome.CriticalPathSummary.RepresentativeAction) == 0 {
		t.Fatalf("expected critical path summary on outcome, got %#v", outcome.CriticalPathSummary)
	}
	if outcome.CacheSummary == nil || outcome.CacheSummary.TotalActions != 1 || outcome.CacheSummary.ReusedActions != 1 {
		t.Fatalf("expected cache summary on outcome, got %#v", outcome.CacheSummary)
	}
	if outcome.SchedulerSummary == nil || outcome.SchedulerSummary.ExecutedBatchCount != 1 || outcome.SchedulerSummary.CriticalPathActions != 1 {
		t.Fatalf("expected scheduler summary on outcome, got %#v", outcome.SchedulerSummary)
	}
	if outcome.SchedulerSummary.Bandwidth != nil {
		t.Fatalf("expected local-only schedule to omit bandwidth summary, got %#v", outcome.SchedulerSummary)
	}
	if outcome.SchedulerSummary.CacheResultCounts["reused"] != 1 || len(outcome.SchedulerSummary.WorkerClasses) != 1 || outcome.SchedulerSummary.WorkerClasses[0].Key == "" || outcome.SchedulerSummary.WorkerClasses[0].CacheResultCounts["reused"] != 1 {
		t.Fatalf("expected worker-class cache-result scheduler breakdown on outcome, got %#v", outcome.SchedulerSummary)
	}
	if len(outcome.SchedulerSummary.ResourceClasses) != 1 || outcome.SchedulerSummary.ResourceClasses[0].Key == "" || outcome.SchedulerSummary.ResourceClasses[0].CriticalPathCount != 1 {
		t.Fatalf("expected resource-class scheduler breakdown on outcome, got %#v", outcome.SchedulerSummary)
	}
	if len(outcome.ExecutedActionIDs) != 1 {
		t.Fatalf("expected executed graph action id, got %#v", outcome)
	}
	if len(outcome.ActionExecutions) != 1 || outcome.ActionExecutions[0].Operation != "compile" || outcome.ActionExecutions[0].ModulePath != ":app" || outcome.ActionExecutions[0].VariantName != "debug" {
		t.Fatalf("expected graph-native action execution record, got %#v", outcome.ActionExecutions)
	}
	if outcome.ActionExecutions[0].BatchIndex != 0 {
		t.Fatalf("expected batch index on action execution, got %#v", outcome.ActionExecutions[0])
	}
	if !outcome.ActionExecutions[0].CriticalPath {
		t.Fatalf("expected critical-path execution, got %#v", outcome.ActionExecutions[0])
	}
	if len(outcome.ActionExecutions[0].Diagnostics) != 2 {
		t.Fatalf("expected attached tool diagnostics, got %#v", outcome.ActionExecutions[0].Diagnostics)
	}
	if got := outcome.ActionExecutions[0].Diagnostics[0]; got.File != "build/generated/ksp/debug/kotlin/App_Generated.kt" || got.SourceKind != "generated" {
		t.Fatalf("expected generated repo-relative diagnostic, got %#v", got)
	}
	if got := outcome.ActionExecutions[0].Diagnostics[1]; got.SourceKind != "dependency-cache" || got.RelatedDependency != "com.squareup.okhttp3:okhttp:4.12.0" {
		t.Fatalf("expected dependency-attributed external diagnostic, got %#v", got)
	}
	if outcome.ActionExecutions[0].QueueWaitMs != 0 || outcome.ActionExecutions[0].WaitReason != "" {
		t.Fatalf("expected no queue wait on the first batch action, got %#v", outcome.ActionExecutions[0])
	}
	if outcome.ActionExecutions[0].CacheKey == "" || outcome.ActionExecutions[0].RetentionClass == "" || outcome.ActionExecutions[0].Shareability == "" {
		t.Fatalf("expected scheduled action policy metadata on execution, got %#v", outcome.ActionExecutions[0])
	}
	if outcome.ActionExecutions[0].ResourceClass == "" || outcome.ActionExecutions[0].ResourceCost == 0 || outcome.ActionExecutions[0].MaxParallelism == 0 {
		t.Fatalf("expected scheduler resource metadata on execution, got %#v", outcome.ActionExecutions[0])
	}
	if !outcome.ActionExecutions[0].Cacheable || len(outcome.ActionExecutions[0].ProbeOrder) == 0 || !outcome.ActionExecutions[0].ExecuteOnMiss {
		t.Fatalf("expected scheduled action probe metadata on execution, got %#v", outcome.ActionExecutions[0])
	}
	if outcome.ActionExecutions[0].EstimatedBytes <= 0 {
		t.Fatalf("expected estimated bytes on cacheable execution, got %#v", outcome.ActionExecutions[0])
	}
	if outcome.ActionExecutions[0].Timings == nil {
		t.Fatalf("expected per-action timings, got %#v", outcome.ActionExecutions[0])
	}
	if outcome.ActionExecutions[0].CacheProbe == nil || outcome.ActionExecutions[0].CacheProbe.State == "" {
		t.Fatalf("expected structured cache probe on action execution, got %#v", outcome.ActionExecutions[0].CacheProbe)
	}
	if len(outcome.ActionExecutions[0].Diagnostics) != 2 || outcome.ActionExecutions[0].Diagnostics[0].Ordinal != 1 || outcome.ActionExecutions[0].Diagnostics[1].Ordinal != 2 {
		t.Fatalf("expected ordered tool diagnostics on action execution, got %#v", outcome.ActionExecutions[0].Diagnostics)
	}
	if outcome.ActionExecutions[0].Diagnostics[0].Code != "kotlinc_warning" || outcome.ActionExecutions[0].Diagnostics[1].Code != "kotlinc_error" {
		t.Fatalf("unexpected diagnostic codes on action execution, got %#v", outcome.ActionExecutions[0].Diagnostics)
	}
	if len(outcome.CacheProbes) != 1 || outcome.CacheProbes[0].State == "" {
		t.Fatalf("expected outcome cache probe summary, got %#v", outcome.CacheProbes)
	}
	if len(outcome.CacheProbeRecords) == 0 || outcome.CacheProbeRecords[0].StepName == "" {
		t.Fatalf("expected ordered cache probe records, got %#v", outcome.CacheProbeRecords)
	}
	if len(outcome.ActionExecutions[0].CacheProbeTrail) == 0 || outcome.ActionExecutions[0].CacheProbeTrail[0].StepName == "" {
		t.Fatalf("expected action cache probe trail, got %#v", outcome.ActionExecutions[0].CacheProbeTrail)
	}
	if len(outcome.ActionExplanations) != 1 || len(outcome.ActionExplanations[0].InputArtifacts) == 0 {
		t.Fatalf("expected graph-derived action explanation, got %#v", outcome.ActionExplanations)
	}
	if outcome.ActionExplanations[0].Cache == nil || outcome.ActionExplanations[0].Cache.State != "reused" {
		t.Fatalf("expected cache explanation derived from action timings, got %#v", outcome.ActionExplanations[0].Cache)
	}
	if len(fake.Calls) != 1 || fake.Calls[0] != "compile::app:debug" {
		t.Fatalf("unexpected compiler calls: %#v", fake.Calls)
	}
	if outcome.RunSummaryPath == "" {
		t.Fatalf("expected persisted run summary path, got %#v", outcome)
	}
	data, err := os.ReadFile(outcome.RunSummaryPath)
	if err != nil {
		t.Fatalf("read run summary: %v", err)
	}
	var summary struct {
		Command               string `json:"command"`
		ModulePath            string `json:"modulePath"`
		Success               bool   `json:"success"`
		Variant               string `json:"variant"`
		TargetResolvedVariant struct {
			ModulePath          string   `json:"modulePath"`
			Name                string   `json:"name"`
			MaterializationID   string   `json:"materializationId"`
			ArtifactSnapshotID  string   `json:"artifactSnapshotId"`
			ProducedArtifactIDs []string `json:"producedArtifactIds"`
		} `json:"targetResolvedVariant"`
		TargetResolvedVariants []struct {
			ModulePath string `json:"modulePath"`
			Name       string `json:"name"`
		} `json:"targetResolvedVariants"`
		RunGraphSummary struct {
			VariantID          string   `json:"variantId"`
			MaterializationID  string   `json:"materializationId"`
			ArtifactSnapshotID string   `json:"artifactSnapshotId"`
			PlannedActionIDs   []string `json:"plannedActionIds"`
			RootActionIDs      []string `json:"rootActionIds"`
			ExecutedActionIDs  []string `json:"executedActionIds"`
		} `json:"runGraphSummary"`
		CriticalPathSummary struct {
			BatchCount           int      `json:"batchCount"`
			EstimatedDurationMs  int64    `json:"estimatedDurationMs"`
			RepresentativeAction []string `json:"representativeActionIds"`
		} `json:"criticalPathSummary"`
		PlannedSchedule struct {
			ResourceBudgets []struct {
				ResourceClass string `json:"resourceClass"`
			} `json:"resourceBudgets"`
			Batches []struct {
				Actions []struct {
					ID string `json:"id"`
				} `json:"actions"`
			} `json:"batches"`
		} `json:"plannedSchedule"`
		CacheSummary struct {
			TotalActions   int `json:"totalActions"`
			ReusedActions  int `json:"reusedActions"`
			RebuiltActions int `json:"rebuiltActions"`
			UnknownActions int `json:"unknownActions"`
		} `json:"cacheSummary"`
		SchedulerSummary struct {
			ExecutedBatchCount  int            `json:"executedBatchCount"`
			CriticalPathActions int            `json:"criticalPathActions"`
			QueueWaitActions    int            `json:"queueWaitActions"`
			TotalQueueWaitMs    int64          `json:"totalQueueWaitMs"`
			WaitReasonCounts    map[string]int `json:"waitReasonCounts"`
			CacheResultCounts   map[string]int `json:"cacheResultCounts"`
			Bandwidth           *struct {
				DeferredActions       int   `json:"deferredActions"`
				TotalCacheableActions int   `json:"totalCacheableActions"`
				EstimatedBytesSaved   int64 `json:"estimatedBytesSaved"`
				BudgetCapacityBytes   int64 `json:"budgetCapacityBytes"`
			} `json:"bandwidth"`
			WorkerClasses []struct {
				Key               string         `json:"key"`
				ActionCount       int            `json:"actionCount"`
				CriticalPathCount int            `json:"criticalPathCount"`
				QueueWaitActions  int            `json:"queueWaitActions"`
				TotalQueueWaitMs  int64          `json:"totalQueueWaitMs"`
				CacheResultCounts map[string]int `json:"cacheResultCounts"`
			} `json:"workerClasses"`
			ResourceClasses []struct {
				Key               string         `json:"key"`
				ActionCount       int            `json:"actionCount"`
				CriticalPathCount int            `json:"criticalPathCount"`
				QueueWaitActions  int            `json:"queueWaitActions"`
				TotalQueueWaitMs  int64          `json:"totalQueueWaitMs"`
				CacheResultCounts map[string]int `json:"cacheResultCounts"`
			} `json:"resourceClasses"`
		} `json:"schedulerSummary"`
		ActionExecutions []struct {
			Operation      string   `json:"operation"`
			ModulePath     string   `json:"modulePath"`
			VariantName    string   `json:"variantName"`
			BatchIndex     int      `json:"batchIndex"`
			CriticalPath   bool     `json:"criticalPath"`
			QueueWaitMs    int64    `json:"queueWaitMs"`
			WaitReason     string   `json:"waitReason"`
			WorkerClass    string   `json:"workerClass"`
			ResourceClass  string   `json:"resourceClass"`
			ResourceCost   int      `json:"resourceCost"`
			MaxParallelism int      `json:"maxParallelism"`
			CacheKey       string   `json:"cacheKey"`
			Cacheable      bool     `json:"cacheable"`
			ProbeOrder     []string `json:"probeOrder"`
			ExecuteOnMiss  bool     `json:"executeOnMiss"`
			EstimatedBytes int64    `json:"estimatedBytes"`
			RetentionClass string   `json:"retentionClass"`
			Shareability   string   `json:"shareability"`
			Diagnostics    []struct {
				Ordinal     int    `json:"ordinal"`
				Fingerprint string `json:"fingerprint"`
				Origin      string `json:"origin"`
				Code        string `json:"code"`
				Severity    string `json:"severity"`
				File        string `json:"file"`
			} `json:"diagnostics"`
		} `json:"actionExecutions"`
		Diagnostics []struct {
			Ordinal     int    `json:"ordinal"`
			Fingerprint string `json:"fingerprint"`
			Origin      string `json:"origin"`
			Code        string `json:"code"`
			Severity    string `json:"severity"`
			File        string `json:"file"`
		} `json:"diagnostics"`
		ActionExplanations []struct {
			Cache *struct {
				State string `json:"state"`
			} `json:"cache"`
			Operation string `json:"operation"`
		} `json:"actionExplanations"`
		Materializations []struct {
			ID                 string `json:"id"`
			ArtifactSnapshotID string `json:"artifactSnapshotId"`
		} `json:"materializations"`
		CacheProbes []struct {
			State string `json:"state"`
		} `json:"cacheProbes"`
		PerfTiming json.RawMessage `json:"perfTiming"`
		WrittenAt  string          `json:"writtenAt"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("unmarshal run summary: %v", err)
	}
	if summary.WrittenAt != fixedNow.Format(time.RFC3339) {
		t.Fatalf("unexpected writtenAt in run summary: got %q want %q", summary.WrittenAt, fixedNow.Format(time.RFC3339))
	}
	if summary.Command != "compile-debug" || summary.ModulePath != ":app" || !summary.Success || summary.Variant != "debug" {
		t.Fatalf("unexpected run summary: %s", data)
	}
	if len(summary.ActionExecutions) != 1 || summary.ActionExecutions[0].Operation != "compile" || summary.ActionExecutions[0].ModulePath != ":app" || summary.ActionExecutions[0].VariantName != "debug" {
		t.Fatalf("unexpected action executions in run summary: %s", data)
	}
	if len(summary.ActionExecutions[0].Diagnostics) != 2 || summary.ActionExecutions[0].Diagnostics[0].Ordinal != 1 || summary.ActionExecutions[0].Diagnostics[1].Ordinal != 2 {
		t.Fatalf("expected ordered action diagnostics in run summary: %s", data)
	}
	if summary.ActionExecutions[0].Diagnostics[0].Origin != "tool" || summary.ActionExecutions[0].Diagnostics[0].Fingerprint == "" {
		t.Fatalf("expected diagnostic origin and fingerprint in run summary: %s", data)
	}
	if len(summary.Diagnostics) != 2 || summary.Diagnostics[0].Code != "kotlinc_warning" || summary.Diagnostics[1].Code != "kotlinc_error" {
		t.Fatalf("expected persisted tool diagnostics in run summary: %s", data)
	}
	if summary.Diagnostics[0].Origin != "tool" || summary.Diagnostics[0].Fingerprint == "" {
		t.Fatalf("expected persisted diagnostic origin/fingerprint: %s", data)
	}
	if summary.ActionExecutions[0].BatchIndex != 0 {
		t.Fatalf("unexpected batch index in run summary: %s", data)
	}
	if !summary.ActionExecutions[0].CriticalPath {
		t.Fatalf("unexpected critical-path marker in run summary: %s", data)
	}
	if summary.ActionExecutions[0].QueueWaitMs != 0 || summary.ActionExecutions[0].WaitReason != "" {
		t.Fatalf("unexpected queue wait in run summary: %s", data)
	}
	if summary.ActionExecutions[0].WorkerClass == "" || summary.ActionExecutions[0].ResourceClass == "" || summary.ActionExecutions[0].ResourceCost == 0 || summary.ActionExecutions[0].MaxParallelism == 0 {
		t.Fatalf("expected persisted scheduler resource metadata in run summary: %s", data)
	}
	if summary.ActionExecutions[0].CacheKey == "" || !summary.ActionExecutions[0].Cacheable || len(summary.ActionExecutions[0].ProbeOrder) == 0 || !summary.ActionExecutions[0].ExecuteOnMiss {
		t.Fatalf("expected persisted cache policy metadata in run summary: %s", data)
	}
	if summary.ActionExecutions[0].EstimatedBytes <= 0 {
		t.Fatalf("expected persisted estimated bytes in run summary: %s", data)
	}
	if summary.ActionExecutions[0].RetentionClass == "" || summary.ActionExecutions[0].Shareability == "" {
		t.Fatalf("expected persisted retention/shareability metadata in run summary: %s", data)
	}
	if summary.TargetResolvedVariant.ModulePath != ":app" || summary.TargetResolvedVariant.Name != "debug" {
		t.Fatalf("unexpected resolved variant in run summary: %s", data)
	}
	if summary.TargetResolvedVariant.MaterializationID == "" || summary.TargetResolvedVariant.ArtifactSnapshotID == "" || len(summary.TargetResolvedVariant.ProducedArtifactIDs) == 0 {
		t.Fatalf("expected graph-backed resolved variant metadata in run summary: %s", data)
	}
	if len(summary.TargetResolvedVariants) != 1 || summary.TargetResolvedVariants[0].Name != "debug" {
		t.Fatalf("unexpected resolved variants in run summary: %s", data)
	}
	if summary.RunGraphSummary.VariantID == "" || summary.RunGraphSummary.MaterializationID == "" || summary.RunGraphSummary.ArtifactSnapshotID == "" {
		t.Fatalf("unexpected run graph root metadata in run summary: %s", data)
	}
	if len(summary.RunGraphSummary.PlannedActionIDs) == 0 || len(summary.RunGraphSummary.RootActionIDs) == 0 || len(summary.RunGraphSummary.ExecutedActionIDs) != 1 {
		t.Fatalf("unexpected run graph action ids in run summary: %s", data)
	}
	if summary.CriticalPathSummary.BatchCount == 0 || len(summary.CriticalPathSummary.RepresentativeAction) == 0 {
		t.Fatalf("unexpected critical path summary in run summary: %s", data)
	}
	if len(summary.PlannedSchedule.ResourceBudgets) == 0 || len(summary.PlannedSchedule.Batches) == 0 || len(summary.PlannedSchedule.Batches[0].Actions) == 0 {
		t.Fatalf("expected planned schedule in run summary: %s", data)
	}
	if summary.CacheSummary.TotalActions != 1 || summary.CacheSummary.ReusedActions != 1 || summary.CacheSummary.RebuiltActions != 0 || summary.CacheSummary.UnknownActions != 0 {
		t.Fatalf("unexpected cache summary in run summary: %s", data)
	}
	if summary.SchedulerSummary.ExecutedBatchCount != 1 || summary.SchedulerSummary.CriticalPathActions != 1 || summary.SchedulerSummary.QueueWaitActions != 0 || summary.SchedulerSummary.TotalQueueWaitMs != 0 {
		t.Fatalf("unexpected scheduler summary in run summary: %s", data)
	}
	if summary.SchedulerSummary.Bandwidth != nil {
		t.Fatalf("expected local-only run summary to omit bandwidth summary: %s", data)
	}
	if summary.SchedulerSummary.CacheResultCounts["reused"] != 1 || len(summary.SchedulerSummary.WorkerClasses) != 1 || summary.SchedulerSummary.WorkerClasses[0].Key == "" || summary.SchedulerSummary.WorkerClasses[0].ActionCount != 1 || summary.SchedulerSummary.WorkerClasses[0].CacheResultCounts["reused"] != 1 {
		t.Fatalf("unexpected worker-class scheduler breakdown in run summary: %s", data)
	}
	if len(summary.SchedulerSummary.ResourceClasses) != 1 || summary.SchedulerSummary.ResourceClasses[0].Key == "" || summary.SchedulerSummary.ResourceClasses[0].CriticalPathCount != 1 || summary.SchedulerSummary.ResourceClasses[0].CacheResultCounts["reused"] != 1 {
		t.Fatalf("unexpected resource-class scheduler breakdown in run summary: %s", data)
	}
	if len(summary.ActionExplanations) != 1 || summary.ActionExplanations[0].Operation != "compile" {
		t.Fatalf("unexpected action explanations in run summary: %s", data)
	}
	if summary.ActionExplanations[0].Cache == nil || summary.ActionExplanations[0].Cache.State != "reused" {
		t.Fatalf("unexpected action explanation cache state in run summary: %s", data)
	}
	if len(summary.Materializations) != 1 || summary.Materializations[0].ID == "" || summary.Materializations[0].ArtifactSnapshotID == "" {
		t.Fatalf("unexpected materializations in run summary: %s", data)
	}
	if len(summary.CacheProbes) != 1 || summary.CacheProbes[0].State == "" {
		t.Fatalf("unexpected cache probes in run summary: %s", data)
	}
	if len(summary.PerfTiming) == 0 || string(summary.PerfTiming) == "null" {
		t.Fatalf("expected perf timing in run summary: %s", data)
	}
}

func TestInferDependencyFromPathRecognizesMaterializedRepositoryView(t *testing.T) {
	got := inferDependencyFromPath("/tmp/repo/.grit/worktree/materialized-m2/com/squareup/okhttp3/okhttp/4.12.0/okhttp-4.12.0.jar")
	if got != "com.squareup.okhttp3:okhttp:4.12.0" {
		t.Fatalf("unexpected dependency inference: %q", got)
	}
}

func TestBuildRoutesJvmModuleToCompileAndTest(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":lib", "jvm-library"))
	mod := prj.FindModule(":lib")
	if mod == nil {
		t.Fatal("expected module")
	}
	outcome, err := svc.Build(context.Background(), prj, mod, BuildRequest{
		Command:         "build",
		VariantExplicit: false,
	}, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	if !outcome.Compiled || !outcome.Tested {
		t.Fatalf("expected JVM build to compile and test, got %#v", outcome)
	}
	if outcome.Variant != "main" {
		t.Fatalf("expected JVM build to target main variant, got %#v", outcome)
	}
	if outcome.TargetResolvedVariant.ModulePath != ":lib" || outcome.TargetResolvedVariant.Name != "main" {
		t.Fatalf("expected JVM resolved variant, got %#v", outcome.TargetResolvedVariant)
	}
	if len(fake.Calls) != 3 || fake.Calls[0] != "compile::lib:main" || fake.Calls[1] != "compile-tests::lib:main" || fake.Calls[2] != "test::lib:main" {
		t.Fatalf("unexpected compiler calls: %#v", fake.Calls)
	}
	for _, call := range fake.Calls {
		if call == "assemble::lib:main" || call == "install::lib:main:" {
			t.Fatalf("unexpected Android packaging call for JVM module: %#v", fake.Calls)
		}
	}
}

func TestBuildRoutesFlavoredUnitTestExecutionToRequestedVariant(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)
	root := t.TempDir()
	prj := &project.Project{
		RootDir: root,
		Name:    "FlavorRuntimeTest",
		Modules: []project.Module{{
			Path:             ":app",
			Dir:              filepath.Join(root, "app"),
			BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
			Type:             "android-application",
			FlavorDimensions: []string{"tier"},
			ProductFlavors: map[string]project.ProductFlavor{
				"free": {Name: "free", Dimension: "tier"},
				"paid": {Name: "paid", Dimension: "tier"},
			},
			BuildTypes: map[string]project.BuildType{
				"debug":   {Name: "debug"},
				"release": {Name: "release"},
			},
		}},
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	outcome, err := svc.Build(context.Background(), prj, mod, BuildRequest{
		Command:          "testDebugUnitTest",
		RequestedVariant: "freeDebug",
		VariantExplicit:  true,
	}, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	if !outcome.Tested {
		t.Fatalf("expected tested outcome, got %#v", outcome)
	}
	if outcome.TargetResolvedVariant.Name != "freeDebug" {
		t.Fatalf("expected freeDebug resolved variant, got %#v", outcome.TargetResolvedVariant)
	}
	if len(fake.Calls) != 3 || fake.Calls[0] != "compile::app:freeDebug" || fake.Calls[1] != "compile-tests::app:freeDebug" || fake.Calls[2] != "test::app:freeDebug" {
		t.Fatalf("unexpected flavored unit-test compiler calls: %#v", fake.Calls)
	}
}

func TestBuildRoutesFlavoredAndroidTestCompilationToRequestedVariant(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)
	root := t.TempDir()
	prj := &project.Project{
		RootDir: root,
		Name:    "FlavorAndroidTestRuntimeTest",
		Modules: []project.Module{{
			Path:             ":app",
			Dir:              filepath.Join(root, "app"),
			BuildFile:        filepath.Join(root, "app", "build.gradle.kts"),
			Type:             "android-application",
			FlavorDimensions: []string{"tier"},
			ProductFlavors: map[string]project.ProductFlavor{
				"free": {Name: "free", Dimension: "tier"},
				"paid": {Name: "paid", Dimension: "tier"},
			},
			BuildTypes: map[string]project.BuildType{
				"debug":   {Name: "debug"},
				"release": {Name: "release"},
			},
		}},
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	outcome, err := svc.Build(context.Background(), prj, mod, BuildRequest{
		Command:          "compileDebugAndroidTestSources",
		RequestedVariant: "freeDebug",
		VariantExplicit:  true,
	}, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	if !outcome.Compiled {
		t.Fatalf("expected compiled outcome, got %#v", outcome)
	}
	if outcome.TargetResolvedVariant.Name != "freeDebug" {
		t.Fatalf("expected freeDebug resolved variant, got %#v", outcome.TargetResolvedVariant)
	}
	if len(fake.Calls) != 1 || fake.Calls[0] != "compile-android-tests::app:freeDebug" {
		t.Fatalf("unexpected flavored androidTest compiler calls: %#v", fake.Calls)
	}
}

func TestExecuteActionRoutesAndroidTestInstallAndUninstallOps(t *testing.T) {
	fake := &androidTestCompilerStub{}
	svc := NewWithCompiler(fake)
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	model := &configmodel.Model{}
	g := graph.New()

	install := svc.executeAction(context.Background(), prj, mod, model, g, BuildRequest{DeviceSerial: "device-123"}, 0, configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:   graph.ActionID("action:installAndroidTest"),
			Name: "installDebugAndroidTest",
			Attributes: map[string]string{
				"operation":   "install-android-tests",
				"modulePath":  ":app",
				"variantName": "debug",
			},
		},
	}, 0, false, os.Stdout, os.Stderr)
	if install.Err != nil || !install.Outcome.Installed || install.Outcome.Message != "androidTest APK installed" {
		t.Fatalf("unexpected install result: %#v", install)
	}

	uninstall := svc.executeAction(context.Background(), prj, mod, model, g, BuildRequest{DeviceSerial: "device-123"}, 0, configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:   graph.ActionID("action:uninstallAndroidTest"),
			Name: "uninstallDebugAndroidTest",
			Attributes: map[string]string{
				"operation":   "uninstall-android-tests",
				"modulePath":  ":app",
				"variantName": "debug",
			},
		},
	}, 0, false, os.Stdout, os.Stderr)
	if uninstall.Err != nil || uninstall.Outcome.Message != "androidTest APK uninstalled" {
		t.Fatalf("unexpected uninstall result: %#v", uninstall)
	}

	if got, want := fake.calls, []string{
		"install-android-tests::app:debug:device-123",
		"uninstall-android-tests::app:debug:device-123",
	}; !slices.Equal(got, want) {
		t.Fatalf("unexpected androidTest compiler calls: got %#v want %#v", got, want)
	}
}

func TestExecuteBatchPrioritizesReusableProbeHints(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	model := &configmodel.Model{}
	batch := []configmodel.ActionScheduleStep{
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:miss"),
				Name: "assembleDebug",
				Attributes: map[string]string{
					"operation":   "assemble",
					"modulePath":  ":app",
					"variantName": "release",
				},
			},
			WorkerClass:    "android-package",
			MaxParallelism: 1,
			ProbeHint: &responsepayload.CacheProbe{
				ActionID: "action:miss",
				State:    "rebuilt",
			},
		},
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:hit"),
				Name: "assembleDebug",
				Attributes: map[string]string{
					"operation":   "assemble",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "android-package",
			MaxParallelism: 1,
			ProbeHint: &responsepayload.CacheProbe{
				ActionID: "action:hit",
				State:    "reused",
			},
		},
	}
	_, err := svc.executeBatch(context.Background(), prj, mod, model, graph.New(), BuildRequest{Command: "assemble"}, 0, batch, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	calls := fake.CallsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("expected two compiler calls, got %#v", calls)
	}
	if calls[0] != "assemble::app:debug" || calls[1] != "assemble::app:release" {
		t.Fatalf("unexpected compiler calls: %#v", calls)
	}
}

func TestExecuteBatchPrioritizesHigherResourceCostWithinWorkerClass(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	model := &configmodel.Model{}
	batch := []configmodel.ActionScheduleStep{
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:light"),
				Name: "assembleLight",
				Attributes: map[string]string{
					"operation":   "assemble",
					"modulePath":  ":app",
					"variantName": "release",
				},
			},
			WorkerClass:    "android-package",
			ResourceClass:  "android-tools",
			ResourceCost:   1,
			MaxParallelism: 1,
		},
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:heavy"),
				Name: "assembleHeavy",
				Attributes: map[string]string{
					"operation":   "assemble",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "android-package",
			ResourceClass:  "android-tools",
			ResourceCost:   5,
			MaxParallelism: 1,
		},
	}
	_, err := svc.executeBatch(context.Background(), prj, mod, model, graph.New(), BuildRequest{Command: "assemble"}, 0, batch, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	calls := fake.CallsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("expected two compiler calls, got %#v", calls)
	}
	if calls[0] != "assemble::app:debug" || calls[1] != "assemble::app:release" {
		t.Fatalf("unexpected compiler calls: %#v", calls)
	}
}

type blockingCompiler struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   []string
}

func (f *blockingCompiler) SetTracker(perf.Tracker) {}
func (f *blockingCompiler) CompileVariant(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return f.block("compile")
}
func (f *blockingCompiler) AssembleVariant(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *blockingCompiler) InstallVariant(context.Context, *project.Project, string, string, string, *os.File, *os.File) error {
	return nil
}
func (f *blockingCompiler) TestDebugUnit(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *blockingCompiler) CompileDebugUnit(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *blockingCompiler) CompileDebugAndroidTest(context.Context, *project.Project, string, string, *os.File, *os.File) error {
	return nil
}
func (f *blockingCompiler) block(call string) error {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	f.started <- struct{}{}
	<-f.release
	return nil
}

func TestExecuteBatchRecordsQueueWaitForParallelSlots(t *testing.T) {
	compiler := &blockingCompiler{
		started: make(chan struct{}, 3),
		release: make(chan struct{}),
	}
	svc := NewWithCompiler(compiler)
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	batch := []configmodel.ActionScheduleStep{
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:a"),
				Name: "compileA",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "compile",
			MaxParallelism: 2,
		},
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:b"),
				Name: "compileB",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "compile",
			MaxParallelism: 2,
		},
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:c"),
				Name: "compileC",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "compile",
			MaxParallelism: 2,
		},
	}
	done := make(chan struct{})
	var outcomes []BuildOutcome
	var err error
	go func() {
		outcomes, err = svc.executeBatch(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, 0, batch, os.Stdout, os.Stderr)
		close(done)
	}()
	<-compiler.started
	<-compiler.started
	time.Sleep(20 * time.Millisecond)
	close(compiler.release)
	<-done
	if err != nil {
		t.Fatalf("executeBatch returned error: %v", err)
	}
	if len(outcomes) != 3 {
		t.Fatalf("expected three batch outcomes, got %#v", outcomes)
	}
	var sawQueueWait bool
	for _, outcome := range outcomes {
		if len(outcome.ActionExecutions) != 1 {
			t.Fatalf("expected one action execution per outcome, got %#v", outcomes)
		}
		execution := outcome.ActionExecutions[0]
		if execution.QueueWaitMs > 0 {
			sawQueueWait = true
			if execution.WaitReason != "worker-slot" {
				t.Fatalf("unexpected wait reason for queued action: %#v", execution)
			}
		}
	}
	if !sawQueueWait {
		t.Fatalf("expected at least one queued action execution, got %#v", outcomes)
	}
}

func TestExecuteBatchUsesResourceCostForWeightedAdmission(t *testing.T) {
	compiler := &blockingCompiler{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	svc := NewWithCompiler(compiler)
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	batch := []configmodel.ActionScheduleStep{
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:heavy-a"),
				Name: "compileHeavyA",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "compile",
			ResourceCost:   2,
			MaxParallelism: 2,
		},
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:heavy-b"),
				Name: "compileHeavyB",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "release",
				},
			},
			WorkerClass:    "compile",
			ResourceCost:   2,
			MaxParallelism: 2,
		},
	}

	done := make(chan struct{})
	var (
		outcomes []BuildOutcome
		err      error
	)
	go func() {
		outcomes, err = svc.executeBatch(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, 0, batch, os.Stdout, os.Stderr)
		close(done)
	}()

	<-compiler.started
	select {
	case <-compiler.started:
		t.Fatal("expected second heavy action to remain queued while the first consumes the full worker budget")
	case <-time.After(20 * time.Millisecond):
	}
	close(compiler.release)
	<-done

	if err != nil {
		t.Fatalf("executeBatch returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected two batch outcomes, got %#v", outcomes)
	}
	var sawQueueWait bool
	for _, outcome := range outcomes {
		if len(outcome.ActionExecutions) != 1 {
			t.Fatalf("expected one action execution per outcome, got %#v", outcomes)
		}
		if outcome.ActionExecutions[0].QueueWaitMs > 0 {
			sawQueueWait = true
		}
	}
	if !sawQueueWait {
		t.Fatalf("expected weighted admission to queue one heavy action, got %#v", outcomes)
	}
}

func TestExecuteBatchUsesAdmissionControllerForResourcePacing(t *testing.T) {
	compiler := &blockingCompiler{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	svc := NewWithCompiler(compiler)
	ac := admission.NewController([]configmodel.ResourceBudget{
		{ResourceClass: "cpu", Capacity: 1},
	})
	svc.SetAdmissionController(ac)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	batch := []configmodel.ActionScheduleStep{
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:a"),
				Name: "compileA",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "compile",
			ResourceClass:  "cpu",
			ResourceCost:   1,
			MaxParallelism: 2,
		},
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:b"),
				Name: "compileB",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "release",
				},
			},
			WorkerClass:    "compile",
			ResourceClass:  "cpu",
			ResourceCost:   1,
			MaxParallelism: 2,
		},
	}

	done := make(chan struct{})
	var (
		outcomes []BuildOutcome
		err      error
	)
	go func() {
		outcomes, err = svc.executeBatch(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, 0, batch, os.Stdout, os.Stderr)
		close(done)
	}()

	<-compiler.started
	select {
	case <-compiler.started:
		t.Fatal("expected second action to remain queued while the admission controller holds the only cpu slot")
	case <-time.After(20 * time.Millisecond):
	}
	close(compiler.release)
	<-done

	if err != nil {
		t.Fatalf("executeBatch returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected two batch outcomes, got %#v", outcomes)
	}
	if ac.ActiveCount() != 0 {
		t.Fatalf("expected controller reservations to be released, got %d active", ac.ActiveCount())
	}
	var sawQueueWait bool
	for _, outcome := range outcomes {
		if len(outcome.ActionExecutions) != 1 {
			t.Fatalf("expected one action execution per outcome, got %#v", outcomes)
		}
		if outcome.ActionExecutions[0].QueueWaitMs > 0 {
			sawQueueWait = true
		}
	}
	if !sawQueueWait {
		t.Fatalf("expected admission-controlled pacing to queue one action, got %#v", outcomes)
	}
}

func TestBuildAssembleRoutesEveryVariant(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)
	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug", "release"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	outcome, err := svc.Build(context.Background(), prj, mod, BuildRequest{
		Command:         "assemble",
		VariantExplicit: false,
	}, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	if len(outcome.ExecutedTasks) != 2 ||
		!slices.Contains(outcome.ExecutedTasks, "assembleDebug") ||
		!slices.Contains(outcome.ExecutedTasks, "assembleRelease") {
		t.Fatalf("unexpected outcome tasks: %#v", outcome.ExecutedTasks)
	}
	if got, want := len(fake.Calls), 2; got != want {
		t.Fatalf("unexpected compiler call count: got %d want %d (%#v)", got, want, fake.Calls)
	}
	if !slices.Contains(fake.Calls, "assemble::app:debug") || !slices.Contains(fake.Calls, "assemble::app:release") {
		t.Fatalf("unexpected compiler calls: %#v", fake.Calls)
	}
	if len(outcome.ActionExecutions) != 2 {
		t.Fatalf("expected action execution records for both variants, got %#v", outcome.ActionExecutions)
	}
	if len(outcome.CacheProbes) != 2 {
		t.Fatalf("expected cache probe records for both variants, got %#v", outcome.CacheProbes)
	}
}

func TestBuildDependentsUsesSemanticGraphPlanning(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)
	root := t.TempDir()
	prj := &project.Project{
		RootDir: root,
		Name:    "ServiceTest",
		Modules: []project.Module{
			{
				Path:      ":lib",
				Dir:       filepath.Join(root, "lib"),
				BuildFile: filepath.Join(root, "lib", "build.gradle.kts"),
				Type:      "android-library",
			},
			{
				Path:      ":app",
				Dir:       filepath.Join(root, "app"),
				BuildFile: filepath.Join(root, "app", "build.gradle.kts"),
				Type:      "android-application",
			},
			{
				Path:      ":feature",
				Dir:       filepath.Join(root, "feature"),
				BuildFile: filepath.Join(root, "feature", "build.gradle.kts"),
				Type:      "android-application",
			},
		},
	}
	if err := testsupport.EnsureModuleDirs(prj.Modules...); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, root, "lib/build.gradle.kts", "dependencies {}\n")
	testutil.WriteFile(t, root, "app/build.gradle.kts", `
dependencies {
  implementation(projects.lib)
}
`)
	testutil.WriteFile(t, root, "feature/build.gradle.kts", `
dependencies {
  implementation(projects.app)
}
`)

	mod := prj.FindModule(":lib")
	if mod == nil {
		t.Fatal("expected module")
	}
	outcome, err := svc.Build(context.Background(), prj, mod, BuildRequest{
		Command: "buildDependents",
	}, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("buildDependents returned error: %v", err)
	}
	if outcome.Message != "dependent modules built" {
		t.Fatalf("unexpected outcome message: %#v", outcome)
	}
	wantCalls := map[string]struct{}{
		"assemble::lib:debug":     {},
		"assemble::app:debug":     {},
		"assemble::feature:debug": {},
	}
	for _, call := range fake.Calls {
		delete(wantCalls, call)
	}
	if len(wantCalls) != 0 {
		t.Fatalf("missing compiler calls: %#v; got %#v", wantCalls, fake.Calls)
	}
}

func TestBuildPersistsRuntimeCacheProbeIntoSemanticSummary(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)
	root := t.TempDir()
	app := testsupport.Module(":app", "android-application", "debug")
	app.Dir = filepath.Join(root, "app")
	app.BuildFile = filepath.Join(root, "app", "build.gradle.kts")
	prj := testsupport.Project(root, app)
	if err := testsupport.EnsureModuleDirs(prj.Modules...); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	_, err := svc.Build(context.Background(), prj, mod, BuildRequest{
		Command:          "compile-debug",
		RequestedVariant: "debug",
	}, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}
	inspect := svc.Inspect(prj)
	if len(inspect.SemanticGraph.Modules) == 0 || len(inspect.SemanticGraph.Modules[0].Variants) == 0 || len(inspect.SemanticGraph.Modules[0].Variants[0].Actions) == 0 {
		t.Fatalf("expected semantic summary actions after build, got %#v", inspect.SemanticGraph)
	}
	var found bool
	for _, variant := range inspect.SemanticGraph.Modules[0].Variants {
		for _, action := range variant.Actions {
			if action.LastCacheProbe != nil && action.LastCacheProbe.State != "" && action.LastCacheProbe.ActionID != "" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("expected persisted runtime cache probe in semantic summary, got %#v", inspect.SemanticGraph.Modules[0].Variants)
	}
	var actionID string
	for _, variant := range inspect.SemanticGraph.Modules[0].Variants {
		for _, action := range variant.Actions {
			if action.LastCacheProbe != nil && action.LastCacheProbe.State != "" && action.LastCacheProbe.ActionID != "" {
				actionID = action.ID
				break
			}
		}
		if actionID != "" {
			break
		}
	}
	if actionID == "" {
		t.Fatal("expected action id with cache probe")
	}
	provenance, err := svc.ActionProvenance(context.Background(), prj, actionID)
	if err != nil {
		t.Fatal(err)
	}
	if provenance.Provenance.CacheProbe == nil || provenance.Provenance.CacheProbe.ActionID != actionID || provenance.Provenance.CacheProbe.State == "" {
		t.Fatalf("expected action provenance cache probe, got %#v", provenance.Provenance.CacheProbe)
	}
}

func TestResolveExecutionPlanInvokesHooksWithReadOnlyModel(t *testing.T) {
	root := t.TempDir()
	prj := &project.Project{
		RootDir: root,
		Name:    "ServiceTest",
		Modules: []project.Module{
			{
				Path:       ":app",
				Dir:        filepath.Join(root, "app"),
				BuildFile:  filepath.Join(root, "app", "build.gradle.kts"),
				Type:       "android-application",
				BuildTypes: map[string]project.BuildType{"debug": {Name: "debug"}},
			},
		},
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, root, "app/build.gradle.kts", "dependencies {}\n")
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})
	hook := &testsupport.HookRecorder{
		BeforeFn: func(_ context.Context, _ integration.PlanRequest, model integration.ReadOnlyModel) error {
			if model.CacheKey() == "" {
				return os.ErrInvalid
			}
			return nil
		},
		AfterFn: func(_ context.Context, result integration.PlanResult, model integration.ReadOnlyModel) error {
			for _, action := range result.Actions {
				if _, ok := model.Action(action.ID); !ok {
					return os.ErrNotExist
				}
				_ = model.ActionInputs(action.ID)
				_ = model.ActionOutputs(action.ID)
			}
			return nil
		},
	}
	svc.RegisterHook(hook)
	plan, err := svc.ResolveExecutionPlan(prj, mod, "assemble", "debug", false)
	if err != nil {
		t.Fatalf("ResolveExecutionPlan returned error: %v", err)
	}
	if len(hook.Before) != 1 || len(hook.After) != 1 {
		t.Fatalf("expected hook invocations, got before=%d after=%d", len(hook.Before), len(hook.After))
	}
	if hook.After[0].ModulePath != ":app" || len(hook.After[0].Actions) == 0 {
		t.Fatalf("unexpected hook plan result: %#v", hook.After[0])
	}
	if len(plan.Actions) == 0 || plan.Actions[0].Kind == graph.ActionKindUnknown {
		t.Fatalf("expected planned graph actions, got %#v", plan.Actions)
	}
}

func TestExecuteBatchDeferRemoteWithAdmissionController(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)

	// Create an admission controller with a tiny network budget (100 bytes, no refill).
	ac := admission.NewController([]configmodel.ResourceBudget{
		{ResourceClass: "cpu", Capacity: 100},
	})
	ac.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	}))
	svc.SetAdmissionController(ac)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	// Two cacheable compile actions with remote probes. First fits in budget,
	// second exceeds it and should have DeferRemote set.
	batch := []configmodel.ActionScheduleStep{
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:compile1"),
				Name: "compileDebug1",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "kotlin-compile",
			MaxParallelism: 4,
			ResourceClass:  "cpu",
			ResourceCost:   1,
			Cacheable:      true,
			ProbeOrder:     []string{"local-overlay", "remote"},
			EstimatedBytes: 80,
		},
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:compile2"),
				Name: "compileDebug2",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "kotlin-compile",
			MaxParallelism: 4,
			ResourceClass:  "cpu",
			ResourceCost:   1,
			Cacheable:      true,
			ProbeOrder:     []string{"local-overlay", "remote"},
			EstimatedBytes: 80,
		},
	}

	outcomes, err := svc.executeBatch(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, 0, batch, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatalf("executeBatch returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outcomes))
	}

	// First action should NOT have DeferRemote (80 bytes fits in 100-byte budget).
	if outcomes[0].ActionExecutions[0].DeferRemote {
		t.Error("expected first action DeferRemote=false, got true")
	}
	// Second action SHOULD have DeferRemote (only 20 bytes remaining < 80 needed).
	if !outcomes[1].ActionExecutions[0].DeferRemote {
		t.Error("expected second action DeferRemote=true, got false")
	}
}

func TestExecuteBatchWithSchedulerProbeDecisionDoesNotSpendFallbackControllerBudget(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)

	ac := admission.NewController([]configmodel.ResourceBudget{
		{ResourceClass: "cpu", Capacity: 0},
	})
	ac.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     80,
		RefillBytesPerSec: 0,
	}))
	svc.SetAdmissionController(ac)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	batch := []configmodel.ActionScheduleStep{{
		Action: graph.Action{
			ID:   graph.ActionID("action:compile1"),
			Name: "compileDebug1",
			Attributes: map[string]string{
				"operation":   "compile",
				"modulePath":  ":app",
				"variantName": "debug",
			},
		},
		WorkerClass:    "kotlin-compile",
		MaxParallelism: 1,
		ResourceClass:  "cpu",
		ResourceCost:   1,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote"},
		EstimatedBytes: 80,
	}}
	remoteProbeDecisions := map[string]admission.RemoteProbeDecision{
		"action:compile1": {
			ActionID:       "action:compile1",
			Eligible:       true,
			DeferRemote:    true,
			EstimatedBytes: 80,
		},
	}

	outcomes, err := svc.executeBatchWithRemoteProbeDecisions(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, 0, batch, remoteProbeDecisions, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatalf("executeBatchWithRemoteProbeDecisions returned error: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(outcomes))
	}
	if !outcomes[0].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected forced-progress fallback to honor scheduler defer decision, got %#v", outcomes[0].ActionExecutions[0])
	}
	if summary := ac.BandwidthSummary(); summary == nil || summary.BudgetRemainingBytes != 80 || summary.TotalAdmittedBytes != 0 || summary.TotalDeniedBytes != 0 {
		t.Fatalf("expected fallback launch to avoid spending controller budget when scheduler already decided, got %+v", summary)
	}
}

func TestExecuteScheduleUsesSchedulerForStepOnlyDependencies(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	first := configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:   graph.ActionID("action:compile"),
			Name: "compileDebug",
			Attributes: map[string]string{
				"operation":   "compile",
				"modulePath":  ":app",
				"variantName": "debug",
			},
		},
		WorkerClass:    "kotlin-compile",
		MaxParallelism: 1,
		ResourceClass:  "cpu",
		ResourceCost:   1,
	}
	second := configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:   graph.ActionID("action:test-compile"),
			Name: "compileDebugUnit",
			Attributes: map[string]string{
				"operation":   "compile-tests",
				"modulePath":  ":app",
				"variantName": "debug",
			},
		},
		Dependencies:   []graph.ActionID{"action:compile"},
		WorkerClass:    "test-compile",
		MaxParallelism: 1,
		ResourceClass:  "cpu",
		ResourceCost:   1,
	}
	schedule := configmodel.ActionSchedule{
		Steps: []configmodel.ActionScheduleStep{second, first},
		Dependencies: map[graph.ActionID][]graph.ActionID{
			"action:test-compile": {"action:compile"},
		},
		Dependents: map[graph.ActionID][]graph.ActionID{
			"action:compile": {"action:test-compile"},
		},
	}

	outcomes, err := svc.executeSchedule(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, schedule, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("executeSchedule returned error: %v", err)
	}
	if got, want := fake.CallsSnapshot(), []string{"compile::app:debug", "compile-tests::app:debug"}; !slices.Equal(got, want) {
		t.Fatalf("expected scheduler to honor dependency order, got %#v want %#v", got, want)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outcomes))
	}
	if outcomes[0].ActionExecutions[0].ActionID != "action:compile" || outcomes[1].ActionExecutions[0].ActionID != "action:test-compile" {
		t.Fatalf("expected dependency-first execution order, got %#v", outcomes)
	}
}

func TestExecuteScheduleUsesSchedulerRemoteProbeDecisionsWithoutDoubleSpend(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)

	ac := admission.NewController([]configmodel.ResourceBudget{
		{ResourceClass: "cpu", Capacity: 2},
	})
	ac.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     80,
		RefillBytesPerSec: 0,
	}))
	svc.SetAdmissionController(ac)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	schedule := configmodel.ActionSchedule{
		Steps: []configmodel.ActionScheduleStep{
			{
				Action: graph.Action{
					ID:   graph.ActionID("action:compile1"),
					Name: "compileDebug1",
					Attributes: map[string]string{
						"operation":   "compile",
						"modulePath":  ":app",
						"variantName": "debug",
					},
				},
				WorkerClass:    "kotlin-compile",
				MaxParallelism: 2,
				ResourceClass:  "cpu",
				ResourceCost:   1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				EstimatedBytes: 80,
			},
			{
				Action: graph.Action{
					ID:   graph.ActionID("action:compile2"),
					Name: "compileDebug2",
					Attributes: map[string]string{
						"operation":   "compile",
						"modulePath":  ":app",
						"variantName": "debug",
					},
				},
				WorkerClass:    "kotlin-compile",
				MaxParallelism: 2,
				ResourceClass:  "cpu",
				ResourceCost:   1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				EstimatedBytes: 80,
			},
		},
	}

	outcomes, err := svc.executeSchedule(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, schedule, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("executeSchedule returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outcomes))
	}
	if outcomes[0].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected first action to keep remote probing enabled, got %#v", outcomes[0].ActionExecutions[0])
	}
	if !outcomes[1].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected second action to defer remote probing, got %#v", outcomes[1].ActionExecutions[0])
	}

	summary := ac.BandwidthSummary()
	if summary == nil {
		t.Fatal("expected bandwidth summary")
	}
	if summary.TotalAdmittedBytes != 80 || summary.TotalDeniedBytes != 80 {
		t.Fatalf("expected scheduler decisions to consume and deny the budget once, got %+v", summary)
	}
	if summary.BudgetRemainingBytes != 80 {
		t.Fatalf("expected admitted estimate to be returned after zero-byte execution, got %+v", summary)
	}
}

func TestExecuteScheduleSchedulerReconciliationUsesActualRemoteBytesOnce(t *testing.T) {
	blob := bytes.Repeat([]byte("x"), 40)
	hash := cas.HashBytes(blob)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/cas/"+hash.String() {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(blob)
	}))
	defer ts.Close()

	client, err := remotecache.New(ts.URL, "")
	if err != nil {
		t.Fatalf("remotecache.New: %v", err)
	}

	fake := &remoteReadCompiler{
		CompilerRecorder: &testsupport.CompilerRecorder{},
		client:           client,
		hash:             hash,
		reads:            []bool{true, false},
	}
	svc := NewWithCompiler(fake)

	ac := admission.NewController([]configmodel.ResourceBudget{
		{ResourceClass: "cpu", Capacity: 1},
	})
	ac.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	}))
	svc.SetAdmissionController(ac)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	schedule := configmodel.ActionSchedule{
		Steps: []configmodel.ActionScheduleStep{
			{
				Action: graph.Action{
					ID:   graph.ActionID("action:compile1"),
					Name: "compileDebug1",
					Attributes: map[string]string{
						"operation":   "compile",
						"modulePath":  ":app",
						"variantName": "debug",
					},
				},
				WorkerClass:    "kotlin-compile",
				MaxParallelism: 1,
				ResourceClass:  "cpu",
				ResourceCost:   1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				EstimatedBytes: 80,
			},
			{
				Action: graph.Action{
					ID:   graph.ActionID("action:compile2"),
					Name: "compileDebug2",
					Attributes: map[string]string{
						"operation":   "compile",
						"modulePath":  ":app",
						"variantName": "debug",
					},
				},
				Dependencies:   []graph.ActionID{"action:compile1"},
				WorkerClass:    "kotlin-compile",
				MaxParallelism: 1,
				ResourceClass:  "cpu",
				ResourceCost:   1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				EstimatedBytes: 80,
			},
		},
		Dependencies: map[graph.ActionID][]graph.ActionID{
			"action:compile2": {"action:compile1"},
		},
		Dependents: map[graph.ActionID][]graph.ActionID{
			"action:compile1": {"action:compile2"},
		},
	}

	outcomes, err := svc.executeSchedule(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, schedule, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("executeSchedule returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outcomes))
	}
	if outcomes[0].ActionExecutions[0].RemoteBytesRead != 40 {
		t.Fatalf("expected first action to observe 40 remote bytes, got %#v", outcomes[0].ActionExecutions[0])
	}
	if outcomes[0].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected first action to keep remote probing enabled, got %#v", outcomes[0].ActionExecutions[0])
	}
	if !outcomes[1].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected second action to defer after only 60 bytes were restored, got %#v", outcomes[1].ActionExecutions[0])
	}

	summary := ac.BandwidthSummary()
	if summary == nil {
		t.Fatal("expected bandwidth summary")
	}
	if summary.TotalAdmittedBytes != 80 || summary.TotalDeniedBytes != 80 {
		t.Fatalf("expected one admitted and one denied scheduler probe, got %+v", summary)
	}
	if summary.BudgetRemainingBytes != 60 {
		t.Fatalf("expected only the unused 40 bytes to be returned, got %+v", summary)
	}
}

func TestExecuteScheduleSchedulerReconciliationChargesRemoteOverrun(t *testing.T) {
	blob := bytes.Repeat([]byte("x"), 95)
	hash := cas.HashBytes(blob)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/cas/"+hash.String() {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(blob)
	}))
	defer ts.Close()

	client, err := remotecache.New(ts.URL, "")
	if err != nil {
		t.Fatalf("remotecache.New: %v", err)
	}

	fake := &remoteReadCompiler{
		CompilerRecorder: &testsupport.CompilerRecorder{},
		client:           client,
		hash:             hash,
		reads:            []bool{true, false},
	}
	svc := NewWithCompiler(fake)

	ac := admission.NewController([]configmodel.ResourceBudget{
		{ResourceClass: "cpu", Capacity: 1},
	})
	ac.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	}))
	svc.SetAdmissionController(ac)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	schedule := configmodel.ActionSchedule{
		Steps: []configmodel.ActionScheduleStep{
			{
				Action: graph.Action{
					ID:   graph.ActionID("action:compile1"),
					Name: "compileDebug1",
					Attributes: map[string]string{
						"operation":   "compile",
						"modulePath":  ":app",
						"variantName": "debug",
					},
				},
				WorkerClass:    "kotlin-compile",
				MaxParallelism: 1,
				ResourceClass:  "cpu",
				ResourceCost:   1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				EstimatedBytes: 80,
			},
			{
				Action: graph.Action{
					ID:   graph.ActionID("action:compile2"),
					Name: "compileDebug2",
					Attributes: map[string]string{
						"operation":   "compile",
						"modulePath":  ":app",
						"variantName": "debug",
					},
				},
				Dependencies:   []graph.ActionID{"action:compile1"},
				WorkerClass:    "kotlin-compile",
				MaxParallelism: 1,
				ResourceClass:  "cpu",
				ResourceCost:   1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				EstimatedBytes: 80,
			},
		},
		Dependencies: map[graph.ActionID][]graph.ActionID{
			"action:compile2": {"action:compile1"},
		},
		Dependents: map[graph.ActionID][]graph.ActionID{
			"action:compile1": {"action:compile2"},
		},
	}

	outcomes, err := svc.executeSchedule(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, schedule, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("executeSchedule returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outcomes))
	}
	if outcomes[0].ActionExecutions[0].RemoteBytesRead != 95 {
		t.Fatalf("expected first action to observe 95 remote bytes, got %#v", outcomes[0].ActionExecutions[0])
	}
	if outcomes[0].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected first action to keep remote probing enabled, got %#v", outcomes[0].ActionExecutions[0])
	}
	if !outcomes[1].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected second action to defer after overrun leaves only 5 bytes, got %#v", outcomes[1].ActionExecutions[0])
	}

	summary := ac.BandwidthSummary()
	if summary == nil {
		t.Fatal("expected bandwidth summary")
	}
	if summary.TotalAdmittedBytes != 95 || summary.TotalDeniedBytes != 80 {
		t.Fatalf("expected overrun to raise admitted bytes to actual usage, got %+v", summary)
	}
	if summary.BudgetRemainingBytes != 5 {
		t.Fatalf("expected only 5 bytes remaining after overrun accounting, got %+v", summary)
	}
}

func TestExecuteBatchRestoresNetworkBudgetFromMeasuredRemoteBytes(t *testing.T) {
	svc := NewWithCompiler(&testsupport.CompilerRecorder{})

	ac := admission.NewController([]configmodel.ResourceBudget{
		{ResourceClass: "cpu", Capacity: 1},
	})
	ac.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	}))
	svc.SetAdmissionController(ac)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	batch := []configmodel.ActionScheduleStep{
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:compile1"),
				Name: "compileDebug1",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "kotlin-compile",
			MaxParallelism: 1,
			ResourceClass:  "cpu",
			ResourceCost:   1,
			Cacheable:      true,
			ProbeOrder:     []string{"local-overlay", "remote"},
			EstimatedBytes: 80,
		},
		{
			Action: graph.Action{
				ID:   graph.ActionID("action:compile2"),
				Name: "compileDebug2",
				Attributes: map[string]string{
					"operation":   "compile",
					"modulePath":  ":app",
					"variantName": "debug",
				},
			},
			WorkerClass:    "kotlin-compile",
			MaxParallelism: 1,
			ResourceClass:  "cpu",
			ResourceCost:   1,
			Cacheable:      true,
			ProbeOrder:     []string{"local-overlay", "remote"},
			EstimatedBytes: 80,
		},
	}

	outcomes, err := svc.executeBatch(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, 0, batch, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatalf("executeBatch returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outcomes))
	}
	if outcomes[0].ActionExecutions[0].DeferRemote {
		t.Fatal("expected first action DeferRemote=false")
	}
	if outcomes[1].ActionExecutions[0].DeferRemote {
		t.Fatal("expected second action DeferRemote=false after first action returned its unused estimate")
	}

	summary := ac.BandwidthSummary()
	if summary == nil {
		t.Fatal("expected bandwidth summary")
	}
	if summary.BudgetRemainingBytes != 100 {
		t.Fatalf("expected budget to refill to full capacity, got %d", summary.BudgetRemainingBytes)
	}
	if summary.TotalAdmittedBytes != 160 {
		t.Fatalf("expected both actions to consume estimated budget before reconciliation, got %d", summary.TotalAdmittedBytes)
	}
}

func TestBuildSchedulerSummaryIncludesBandwidthAccounting(t *testing.T) {
	ac := admission.NewController([]configmodel.ResourceBudget{
		{ResourceClass: "cpu", Capacity: 2},
	})
	ac.SetNetworkBudget(admission.NewNetworkBudget(admission.NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	}))

	first := configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:         graph.ActionID("action:first"),
			Attributes: map[string]string{"operation": "compile"},
		},
		WorkerClass:    "kotlin-compile",
		MaxParallelism: 2,
		ResourceClass:  "cpu",
		ResourceCost:   1,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote"},
		EstimatedBytes: 80,
	}
	second := first
	second.Action.ID = graph.ActionID("action:second")

	if decision := ac.TryAdmit(first); !decision.Admitted || decision.DeferRemote {
		t.Fatalf("expected first action to consume budget without deferral, got %+v", decision)
	}
	if err := ac.Release("action:first"); err != nil {
		t.Fatalf("release first action: %v", err)
	}
	if decision := ac.TryAdmit(second); !decision.Admitted || !decision.DeferRemote {
		t.Fatalf("expected second action to defer remote probes, got %+v", decision)
	}
	if err := ac.Release("action:second"); err != nil {
		t.Fatalf("release second action: %v", err)
	}

	summary := buildSchedulerSummary(BuildOutcome{
		ActionExecutions: []ActionExecution{
			{ActionID: "action:first", Cacheable: true, EstimatedBytes: 80},
			{ActionID: "action:second", Cacheable: true, DeferRemote: true, EstimatedBytes: 80},
		},
	}, ac)
	if summary == nil || summary.Bandwidth == nil {
		t.Fatalf("expected bandwidth summary, got %#v", summary)
	}
	if summary.Bandwidth.TotalCacheableActions != 2 || summary.Bandwidth.DeferredActions != 1 || summary.Bandwidth.EstimatedBytesSaved != 80 {
		t.Fatalf("unexpected bandwidth summary counts: %#v", summary.Bandwidth)
	}
	if summary.Bandwidth.TotalAdmittedBytes != 80 || summary.Bandwidth.TotalDeniedBytes != 80 {
		t.Fatalf("unexpected bandwidth accounting: %#v", summary.Bandwidth)
	}
}

func TestExecuteScheduleAutoCreatesScheduleAdmissionControllerWithoutLeakingState(t *testing.T) {
	fake := &testsupport.CompilerRecorder{}
	svc := NewWithCompiler(fake)

	prj := testsupport.Project(t.TempDir(), testsupport.Module(":app", "android-application", "debug"))
	mod := prj.FindModule(":app")
	if mod == nil {
		t.Fatal("expected module")
	}

	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{{ResourceClass: "cpu", Capacity: 2}},
		NetworkBudgetConfig: &configmodel.ScheduleNetworkBudget{
			CapacityBytes:     80,
			RefillBytesPerSec: 0,
		},
		Batches: [][]configmodel.ActionScheduleStep{{
			{
				Action: graph.Action{
					ID:   graph.ActionID("action:compile1"),
					Name: "compileDebug1",
					Attributes: map[string]string{
						"operation":   "compile",
						"modulePath":  ":app",
						"variantName": "debug",
					},
				},
				WorkerClass:    "kotlin-compile",
				MaxParallelism: 2,
				ResourceClass:  "cpu",
				ResourceCost:   1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				EstimatedBytes: 80,
			},
			{
				Action: graph.Action{
					ID:   graph.ActionID("action:compile2"),
					Name: "compileDebug2",
					Attributes: map[string]string{
						"operation":   "compile",
						"modulePath":  ":app",
						"variantName": "debug",
					},
				},
				WorkerClass:    "kotlin-compile",
				MaxParallelism: 2,
				ResourceClass:  "cpu",
				ResourceCost:   1,
				Cacheable:      true,
				ProbeOrder:     []string{"local-overlay", "remote"},
				EstimatedBytes: 80,
			},
		}},
	}

	outcomes, err := svc.executeSchedule(context.Background(), prj, mod, &configmodel.Model{}, graph.New(), BuildRequest{Command: "compile-debug"}, schedule, os.Stdout, os.Stderr, perf.New(false))
	if err != nil {
		t.Fatalf("executeSchedule returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outcomes))
	}
	if outcomes[0].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected first action to consume auto-created schedule budget, got %#v", outcomes[0].ActionExecutions[0])
	}
	if !outcomes[1].ActionExecutions[0].DeferRemote {
		t.Fatalf("expected second action to defer after auto-created schedule budget is exhausted, got %#v", outcomes[1].ActionExecutions[0])
	}
	if svc.admissionController != nil {
		t.Fatalf("expected auto-created controller to be released after executeSchedule, got %#v", svc.admissionController)
	}
}

func TestRuntimeObservationsFromExecutionsCarryRemoteBytes(t *testing.T) {
	observations := runtimeObservationsFromExecutions([]ActionExecution{{
		ActionID:        "action:compile",
		RemoteBytesRead: 4096,
		CacheProbe: &responsepayload.CacheProbe{
			ActionID: "action:compile",
			State:    "reused",
			Basis:    "shared-cache-hit",
		},
	}})
	if len(observations) != 1 {
		t.Fatalf("expected one observation, got %#v", observations)
	}
	if observations[0].ActionID != "action:compile" || observations[0].RemoteBytesRead != 4096 {
		t.Fatalf("unexpected observation payload: %#v", observations[0])
	}
	if observations[0].CacheProbe == nil || observations[0].CacheProbe.State != "reused" {
		t.Fatalf("expected cache probe to be preserved, got %#v", observations[0].CacheProbe)
	}
}
