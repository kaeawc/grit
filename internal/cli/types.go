package cli

import (
	"encoding/json"

	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/integration"
	"github.com/kaeawc/grit/internal/intellijsync"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
	"github.com/kaeawc/grit/internal/service"
)

type response struct {
	Success    bool             `json:"success"`
	Command    string           `json:"command"`
	DurationMs int64            `json:"durationMs"`
	Result     json.RawMessage  `json:"result,omitempty"`
	Error      *responseError   `json:"error,omitempty"`
	Logs       *responseLogs    `json:"logs,omitempty"`
	PerfTiming *perf.TimingData `json:"perfTiming,omitempty"`
}

type responseError struct {
	Message string `json:"message"`
}

type responseLogs struct {
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
}

type inspectResult struct {
	Repo            string                       `json:"repo"`
	Name            string                       `json:"name"`
	Backend         string                       `json:"backend"`
	RootBuildFile   string                       `json:"rootBuildFile"`
	SettingsFile    string                       `json:"settingsFile"`
	VersionCatalog  string                       `json:"versionCatalog,omitempty"`
	VersionCatalogs []string                     `json:"versionCatalogs,omitempty"`
	Repositories    []project.Repository         `json:"repositories"`
	Plugins         []string                     `json:"plugins"`
	SemanticGraph   project.SemanticGraphSummary `json:"semanticGraph,omitempty"`
	Modules         []inspectModule              `json:"modules"`
}

type inspectModule struct {
	Path                      string                    `json:"path"`
	Dir                       string                    `json:"dir"`
	Type                      string                    `json:"type"`
	Namespace                 string                    `json:"namespace,omitempty"`
	ApplicationID             string                    `json:"applicationId,omitempty"`
	VersionCode               string                    `json:"versionCode,omitempty"`
	VersionName               string                    `json:"versionName,omitempty"`
	CompileSDK                string                    `json:"compileSdk,omitempty"`
	BuildToolsVersion         string                    `json:"buildToolsVersion,omitempty"`
	MinSDK                    string                    `json:"minSdk,omitempty"`
	TargetSDK                 string                    `json:"targetSdk,omitempty"`
	TestInstrumentationRunner string                    `json:"testInstrumentationRunner,omitempty"`
	SourceFiles               int                       `json:"sourceFiles"`
	TestFiles                 int                       `json:"testFiles"`
	AndroidTestFiles          int                       `json:"androidTestFiles"`
	UsesCompose               bool                      `json:"usesCompose"`
	UsesMetro                 bool                      `json:"usesMetro"`
	KotlinFreeArgs            []string                  `json:"kotlinFreeCompilerArgs,omitempty"`
	LintDisabled              []string                  `json:"lintDisabledChecks,omitempty"`
	ConsumerProguardFiles     []string                  `json:"consumerProguardFiles,omitempty"`
	Variants                  []project.BuildType       `json:"variants,omitempty"`
	ResolvedVariants          []project.ResolvedVariant `json:"resolvedVariants,omitempty"`
	RequestedTasks            []string                  `json:"requestedTasks"`
	Tasks                     []project.Task            `json:"tasks,omitempty"`
}

type doctorResult struct {
	Items []doctorItem `json:"items"`
}

type doctorItem struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	OK     bool   `json:"ok"`
}

type nativeResult struct {
	Repo                   string                                   `json:"repo"`
	Module                 string                                   `json:"module"`
	Variant                string                                   `json:"variant,omitempty"`
	Variants               []string                                 `json:"variants,omitempty"`
	VariantConfig          *project.BuildType                       `json:"variantConfig,omitempty"`
	VariantSummary         *project.SemanticVariantSummary          `json:"variantSummary,omitempty"`
	TargetResolvedVariant  *project.ResolvedVariant                 `json:"targetResolvedVariant,omitempty"`
	TargetResolvedVariants []project.ResolvedVariant                `json:"targetResolvedVariants,omitempty"`
	RunGraphSummary        *service.RunGraphSummary                 `json:"runGraphSummary,omitempty"`
	CriticalPathSummary    *service.CriticalPathSummary             `json:"criticalPathSummary,omitempty"`
	PlannedSchedule        *service.PlanScheduleResult              `json:"plannedSchedule,omitempty"`
	CacheSummary           *service.CacheSummary                    `json:"cacheSummary,omitempty"`
	SchedulerSummary       *service.SchedulerSummary                `json:"schedulerSummary,omitempty"`
	Materializations       []project.SemanticMaterializationSummary `json:"materializations,omitempty"`
	ActionExecutions       []service.ActionExecution                `json:"actionExecutions,omitempty"`
	CacheProbes            []responsepayload.CacheProbe             `json:"cacheProbes,omitempty"`
	CacheProbeRecords      []responsepayload.CacheProbeRecord       `json:"cacheProbeRecords,omitempty"`
	ActionExplanations     []explain.Action                         `json:"actionExplanations,omitempty"`
	APKPath                string                                   `json:"apkPath,omitempty"`
	ExecutedTasks          []string                                 `json:"executedTasks,omitempty"`
	Message                string                                   `json:"message,omitempty"`
	Installed              bool                                     `json:"installed,omitempty"`
	Tested                 bool                                     `json:"tested,omitempty"`
	Compiled               bool                                     `json:"compiled,omitempty"`
	RunSummaryPath         string                                   `json:"runSummaryPath,omitempty"`
}

type androidCapabilityReportResult struct {
	Repo     string                           `json:"repo"`
	Module   string                           `json:"module"`
	Variants []androidCapabilityVariantResult `json:"variants,omitempty"`
}

type androidCapabilityVariantResult struct {
	Name                      string                            `json:"name"`
	DisplayName               string                            `json:"displayName,omitempty"`
	BuildType                 string                            `json:"buildType,omitempty"`
	Flavors                   []string                          `json:"flavors,omitempty"`
	CompileSDK                string                            `json:"compileSdk,omitempty"`
	BuildToolsVersion         string                            `json:"buildToolsVersion,omitempty"`
	Namespace                 string                            `json:"namespace,omitempty"`
	ApplicationID             string                            `json:"applicationId,omitempty"`
	ApplicationIDSuffix       string                            `json:"applicationIdSuffix,omitempty"`
	VersionCode               string                            `json:"versionCode,omitempty"`
	VersionName               string                            `json:"versionName,omitempty"`
	VersionNameSuffix         string                            `json:"versionNameSuffix,omitempty"`
	MinSDK                    string                            `json:"minSdk,omitempty"`
	TargetSDK                 string                            `json:"targetSdk,omitempty"`
	TestInstrumentationRunner string                            `json:"testInstrumentationRunner,omitempty"`
	Optimization              project.VariantOptimization       `json:"optimization,omitempty"`
	ProguardFiles             []string                          `json:"proguardFiles,omitempty"`
	ConsumerProguardFiles     []string                          `json:"consumerProguardFiles,omitempty"`
	ManifestPaths             []string                          `json:"manifestPaths,omitempty"`
	MaterializationID         string                            `json:"materializationId,omitempty"`
	ArtifactSnapshotID        string                            `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs      []string                          `json:"classpathSnapshotIds,omitempty"`
	SourceRoots               []string                          `json:"sourceRoots,omitempty"`
	BackingArtifactID         string                            `json:"backingArtifactId,omitempty"`
	BackingArtifactPath       string                            `json:"backingArtifactPath,omitempty"`
	ProducedArtifactIDs       []string                          `json:"producedArtifactIds,omitempty"`
	ProducedArtifactPaths     []string                          `json:"producedArtifactPaths,omitempty"`
	ProducedArtifacts         []project.ResolvedVariantArtifact `json:"producedArtifacts,omitempty"`
	ProducedArtifactKinds     []string                          `json:"producedArtifactKinds,omitempty"`
	InstallArtifactID         string                            `json:"installArtifactId,omitempty"`
	InstallArtifactPath       string                            `json:"installArtifactPath,omitempty"`
	ResourceArtifactIDs       []string                          `json:"resourceArtifactIds,omitempty"`
	ResourceArtifactPaths     []string                          `json:"resourceArtifactPaths,omitempty"`
	ManifestArtifactIDs       []string                          `json:"manifestArtifactIds,omitempty"`
	ManifestArtifactPaths     []string                          `json:"manifestArtifactPaths,omitempty"`
	Installable               bool                              `json:"installable,omitempty"`
	Testable                  bool                              `json:"testable,omitempty"`
	Debuggable                bool                              `json:"debuggable,omitempty"`
	SigningConfigured         bool                              `json:"signingConfigured,omitempty"`
	SigningConfig             string                            `json:"signingConfig,omitempty"`
	MinifyEnabled             bool                              `json:"minifyEnabled,omitempty"`
	ShrinkResources           bool                              `json:"shrinkResources,omitempty"`
	InstallTask               string                            `json:"installTask,omitempty"`
	UninstallTask             string                            `json:"uninstallTask,omitempty"`
	AndroidTestPackage        string                            `json:"androidTestPackage,omitempty"`
	AndroidTestManifest       string                            `json:"androidTestManifest,omitempty"`
	AndroidTestInstallTask    string                            `json:"androidTestInstallTask,omitempty"`
	AndroidTestUninstallTask  string                            `json:"androidTestUninstallTask,omitempty"`
}

type tasksResult struct {
	Repo   string         `json:"repo"`
	Module string         `json:"module"`
	Tasks  []project.Task `json:"tasks"`
}

type projectsResult struct {
	Repo    string   `json:"repo"`
	Name    string   `json:"name"`
	Modules []string `json:"modules"`
}

type propertiesResult struct {
	Repo             string                           `json:"repo"`
	Module           string                           `json:"module"`
	Type             string                           `json:"type"`
	Values           responsepayload.PropertiesValues `json:"values"`
	Variants         []project.BuildType              `json:"variants,omitempty"`
	ResolvedVariants []project.ResolvedVariant        `json:"resolvedVariants,omitempty"`
}

type dependenciesResult struct {
	Repo   string              `json:"repo"`
	Module string              `json:"module"`
	Scopes map[string][]string `json:"scopes"`
}

type buildEnvironmentResult struct {
	Repo             string               `json:"repo"`
	SettingsFile     string               `json:"settingsFile"`
	RootBuildFile    string               `json:"rootBuildFile"`
	Repositories     []project.Repository `json:"repositories"`
	GradleProperties map[string]string    `json:"gradleProperties"`
	VersionCatalogs  []string             `json:"versionCatalogs,omitempty"`
}

type artifactTransformsResult struct {
	Repo       string   `json:"repo"`
	Module     string   `json:"module"`
	Transforms []string `json:"transforms"`
}

type dependencyInsightResult struct {
	Repo    string              `json:"repo"`
	Module  string              `json:"module"`
	Query   string              `json:"query,omitempty"`
	Scopes  map[string][]string `json:"scopes"`
	Matches map[string][]string `json:"matches,omitempty"`
}

type resolveIntelliJTasksResult struct {
	Repo      string                 `json:"repo"`
	Module    string                 `json:"module"`
	TaskNames []string               `json:"taskNames,omitempty"`
	Requests  []service.BuildRequest `json:"requests"`
}

type explainPlanResult = service.PlanExplanationResult

type variantProvenanceResult = service.ProvenanceResult

type actionProvenanceResult = service.ProvenanceResult

type cleanupPlanResult = service.CleanupPlanResult

type runSummaryResult = service.RunSummaryResult

type runSummariesResult = service.RunSummariesResult

type runGraphSummaryResult = service.RunGraphSummaryResult

type criticalPathSummaryResult = service.CriticalPathSummaryResult

type schedulerSummaryResult = service.SchedulerSummaryResult

type cacheSummaryResult = service.CacheSummaryResult

type toolSummaryResult = service.ToolSummaryResult

type diagnosticsResult = service.DiagnosticsResult

type diagnosticSummaryResult = service.DiagnosticSummaryResult

type plannedScheduleResult = service.PlannedScheduleResult

type scheduleDriftResult = service.ScheduleDriftResult

type actionExecutionResult = service.ActionExecutionResult

type actionExplanationResult = service.ActionExplanationResult

type actionExecutionsResult = service.ActionExecutionsResult

type actionExplanationsResult = service.ActionExplanationsResult

type cacheProbesResult = service.CacheProbesResult

type cacheProbeRecordsResult = service.CacheProbeRecordsResult

type reuseDecisionResult = service.ReuseDecisionResult

type reuseDecisionsResult = service.ReuseDecisionsResult

type materializationsResult = service.MaterializationsResult

type actionTraceResult = service.ActionTraceResult

type perfTimingResult = service.PerfTimingResult

type classpathSnapshotResult = service.ClasspathSnapshotResult

type classpathSnapshotProvenanceResult = service.ClasspathSnapshotProvenanceResult

type classpathSnapshotConsumersResult = service.ClasspathSnapshotConsumersResult

type classpathEntryLookupResult = service.ClasspathEntryLookupResult

type classpathPathConsumersResult = service.ClasspathPathConsumersResult

type artifactOnClasspathResult = service.ArtifactOnClasspathResult

type artifactClasspathConsumersResult = service.ArtifactClasspathConsumersResult

type fileOwnersResult = service.FileOwnersResult

type moduleByIDResult = service.ModuleByIDResult

type variantByIDResult = service.VariantByIDResult

type actionByIDResult = service.ActionByIDResult

type artifactByIDResult = service.ArtifactByIDResult

type materializationByIDResult = service.MaterializationByIDResult

type materializationConsumersResult = service.MaterializationConsumersResult

type classpathSnapshotByIDResult = service.ClasspathSnapshotByIDResult

type classpathSnapshotConsumersByIDResult = service.ClasspathSnapshotConsumersByIDResult

type actionInputsResult = service.ActionInputsResult

type actionOutputsResult = service.ActionOutputsResult

type actionDependenciesResult = service.ActionDependenciesResult

type actionDependentsResult = service.ActionDependentsResult

type actionsForModuleResult = service.ActionsForModuleResult

type actionsForVariantResult = service.ActionsForVariantResult

type variantMaterializationResult struct {
	Repo          string                                   `json:"repo"`
	Module        string                                   `json:"module"`
	Variant       string                                   `json:"variant"`
	ModelCacheKey string                                   `json:"modelCacheKey,omitempty"`
	Provenance    integration.VariantMaterializationResult `json:"provenance"`
}

type materializationProvenanceResult = service.MaterializationProvenanceResult

type variantSourceSetModelResult = service.VariantSourceSetModelResult

type dependencyBindingsForVariantResult = service.DependencyBindingsForVariantResult

type dependencyBindingsForModuleResult = service.DependencyBindingsForModuleResult

type dependencyRealizationsForVariantResult = service.DependencyRealizationsForVariantResult

type dependencyRealizationsForModuleResult = service.DependencyRealizationsForModuleResult

type plannedActionPolicyResult = service.PlannedActionPolicyResult

type plannedActionPoliciesResult = service.PlannedActionPoliciesResult

type variantCompatibilityResult = service.VariantCompatibilityResult

type intellijSyncModelResult struct {
	Repo          string              `json:"repo"`
	ModelCacheKey string              `json:"modelCacheKey,omitempty"`
	Model         *intellijsync.Model `json:"model,omitempty"`
}

type artifactsForVariantResult struct {
	Repo               string                        `json:"repo"`
	Module             string                        `json:"module"`
	Variant            string                        `json:"variant"`
	ModelCacheKey      string                        `json:"modelCacheKey,omitempty"`
	MaterializationID  string                        `json:"materializationId,omitempty"`
	ArtifactSnapshotID string                        `json:"artifactSnapshotId,omitempty"`
	Artifacts          []configmodel.ArtifactSummary `json:"artifacts,omitempty"`
}

type artifactsForModuleResult = service.ArtifactsForModuleResult

type moduleManifestResult struct {
	Repo          string                           `json:"repo"`
	Module        string                           `json:"module"`
	ModelCacheKey string                           `json:"modelCacheKey,omitempty"`
	Manifest      integration.ModuleManifestResult `json:"manifest"`
}

type variantManifestResult struct {
	Repo          string                            `json:"repo"`
	Module        string                            `json:"module"`
	Variant       string                            `json:"variant"`
	ModelCacheKey string                            `json:"modelCacheKey,omitempty"`
	Manifest      integration.VariantManifestResult `json:"manifest"`
}

type artifactSnapshotProvenanceResult struct {
	Repo               string                                       `json:"repo"`
	ArtifactSnapshotID string                                       `json:"artifactSnapshotId,omitempty"`
	ModelCacheKey      string                                       `json:"modelCacheKey,omitempty"`
	Provenance         integration.ArtifactSnapshotProvenanceResult `json:"provenance"`
}

type artifactSnapshotConsumersResult = service.ArtifactSnapshotConsumersResult

type artifactProvenanceResult struct {
	Repo          string                 `json:"repo"`
	ArtifactID    string                 `json:"artifactId"`
	ModelCacheKey string                 `json:"modelCacheKey,omitempty"`
	Provenance    integration.Provenance `json:"provenance"`
}

type artifactConsumersResult struct {
	Repo          string                              `json:"repo"`
	ArtifactID    string                              `json:"artifactId"`
	ModelCacheKey string                              `json:"modelCacheKey,omitempty"`
	Consumers     integration.ArtifactConsumersResult `json:"consumers"`
}

type variantImpactResult struct {
	Repo          string                      `json:"repo"`
	Module        string                      `json:"module"`
	Variant       string                      `json:"variant"`
	ModelCacheKey string                      `json:"modelCacheKey,omitempty"`
	Impact        service.VariantImpactResult `json:"impact"`
}

type moduleImpactResult struct {
	Repo          string                     `json:"repo"`
	Module        string                     `json:"module"`
	ModelCacheKey string                     `json:"modelCacheKey,omitempty"`
	Impact        service.ModuleImpactResult `json:"impact"`
}

type resolverReportResult struct {
	Repo         string                      `json:"repo"`
	Module       string                      `json:"module"`
	CachePath    string                      `json:"cachePath,omitempty"`
	ReportPath   string                      `json:"reportPath,omitempty"`
	ReplayPath   string                      `json:"replayPath,omitempty"`
	LockfilePath string                      `json:"lockfilePath,omitempty"`
	Found        bool                        `json:"found"`
	Topology     m2local.CacheTopology       `json:"topology,omitempty"`
	Inputs       m2local.ResolvedCacheInputs `json:"inputs,omitempty"`
	Summary      resolverReportSummary       `json:"summary,omitempty"`
	Report       m2local.ResolutionReport    `json:"report,omitempty"`
	Replay       m2local.ResolutionReplay    `json:"replay,omitempty"`
	Lockfile     m2local.ResolutionLockfile  `json:"lockfile,omitempty"`
}

type cacheTopologyResult = service.CacheTopologyResult

type resolverReportSummary struct {
	CompileJarCount     int `json:"compileJarCount,omitempty"`
	RuntimeJarCount     int `json:"runtimeJarCount,omitempty"`
	TestJarCount        int `json:"testJarCount,omitempty"`
	AndroidLibraryCount int `json:"androidLibraryCount,omitempty"`
	SelectionCount      int `json:"selectionCount,omitempty"`
	ConflictCount       int `json:"conflictCount,omitempty"`
	PinCount            int `json:"pinCount,omitempty"`
}

type javaToolchainsResult struct {
	Repo   string                              `json:"repo"`
	Java   responsepayload.JavaToolchainInfo   `json:"java"`
	Kotlin responsepayload.KotlinToolchainInfo `json:"kotlin"`
}

type kotlinDslAccessorsReportResult struct {
	Repo      string   `json:"repo"`
	Module    string   `json:"module"`
	Accessors []string `json:"accessors"`
}

type outgoingVariantsResult struct {
	Repo             string                    `json:"repo"`
	Module           string                    `json:"module"`
	Variants         []project.BuildType       `json:"variants"`
	ResolvedVariants []project.ResolvedVariant `json:"resolvedVariants,omitempty"`
}

type resolvableConfigurationsResult struct {
	Repo           string              `json:"repo"`
	Module         string              `json:"module"`
	Configurations map[string][]string `json:"configurations"`
}

type signingReportResult struct {
	Repo     string                 `json:"repo"`
	Module   string                 `json:"module"`
	Variants []signingReportVariant `json:"variants"`
}

type signingReportVariant struct {
	Name             string `json:"name"`
	SigningConfig    string `json:"signingConfig,omitempty"`
	ResolvedConfig   string `json:"resolvedConfig,omitempty"`
	StoreFile        string `json:"storeFile,omitempty"`
	KeyAlias         string `json:"keyAlias,omitempty"`
	HasStorePassword bool   `json:"hasStorePassword,omitempty"`
	HasKeyPassword   bool   `json:"hasKeyPassword,omitempty"`
}
