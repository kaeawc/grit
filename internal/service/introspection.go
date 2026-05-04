package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/admission"
	"github.com/kaeawc/grit/internal/cachepolicy"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/dependencywiring"
	"github.com/kaeawc/grit/internal/execbackend"
	"github.com/kaeawc/grit/internal/explain"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/integration"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

type InspectResult struct {
	Repo            string
	Name            string
	Backend         string
	RootBuildFile   string
	SettingsFile    string
	VersionCatalog  string
	VersionCatalogs []string
	Repositories    []project.Repository
	Plugins         []string
	SemanticGraph   project.SemanticGraphSummary
	Modules         []InspectModule
}

type PlanExplanationResult struct {
	Repo             string                 `json:"repo"`
	Module           string                 `json:"module"`
	Command          string                 `json:"command"`
	RequestedVariant string                 `json:"requestedVariant,omitempty"`
	TargetVariant    string                 `json:"targetVariant,omitempty"`
	Variants         []string               `json:"variants,omitempty"`
	VariantExplicit  bool                   `json:"variantExplicit,omitempty"`
	ModelCacheKey    string                 `json:"modelCacheKey,omitempty"`
	ActionIDs        []string               `json:"actionIds,omitempty"`
	Reasons          []string               `json:"reasons,omitempty"`
	Schedule         PlanScheduleResult     `json:"schedule,omitempty"`
	Actions          []InspectPlannedAction `json:"actions,omitempty"`
}

type InspectPlannedAction struct {
	ID                   string                      `json:"id"`
	Name                 string                      `json:"name,omitempty"`
	Operation            string                      `json:"operation,omitempty"`
	ModulePath           string                      `json:"modulePath,omitempty"`
	VariantName          string                      `json:"variantName,omitempty"`
	WorkerClass          string                      `json:"workerClass,omitempty"`
	ResourceClass        string                      `json:"resourceClass,omitempty"`
	ResourceCost         int                         `json:"resourceCost,omitempty"`
	MaxParallelism       int                         `json:"maxParallelism,omitempty"`
	CacheKey             string                      `json:"cacheKey,omitempty"`
	Cacheable            bool                        `json:"cacheable,omitempty"`
	ProbeOrder           []string                    `json:"probeOrder,omitempty"`
	ExecuteOnMiss        bool                        `json:"executeOnMiss,omitempty"`
	EstimatedBytes       int64                       `json:"estimatedBytes,omitempty"`
	DeferRemote          bool                        `json:"deferRemote,omitempty"`
	RemoteProbeAdmission *PlanRemoteProbeAdmission   `json:"remoteProbeAdmission,omitempty"`
	ProbeHint            *responsepayload.CacheProbe `json:"probeHint,omitempty"`
	RetentionClass       string                      `json:"retentionClass,omitempty"`
	Shareability         string                      `json:"shareability,omitempty"`
	Dependencies         []string                    `json:"dependencies,omitempty"`
	Inputs               []string                    `json:"inputs,omitempty"`
	Outputs              []string                    `json:"outputs,omitempty"`
}

type PlanScheduleResult struct {
	ResourceBudgets     []PlanResourceBudget `json:"resourceBudgets,omitempty"`
	NetworkBudgetConfig *PlanNetworkBudget   `json:"networkBudgetConfig,omitempty"`
	Batches             []PlanScheduleBatch  `json:"batches,omitempty"`
}

// PlanNetworkBudget surfaces the bandwidth-aware admission parameters in the
// plan schedule for introspection and run summaries.
type PlanNetworkBudget struct {
	CapacityBytes     int64 `json:"capacityBytes"`
	RefillBytesPerSec int64 `json:"refillBytesPerSec"`
}

// PlanRemoteProbeAdmission surfaces the scheduler's bandwidth-aware probe
// admission decision for a planned action.
type PlanRemoteProbeAdmission struct {
	Deferred          bool  `json:"deferred,omitempty"`
	BudgetBeforeBytes int64 `json:"budgetBeforeBytes,omitempty"`
	BudgetAfterBytes  int64 `json:"budgetAfterBytes,omitempty"`
}

type PlanResourceBudget struct {
	ResourceClass string `json:"resourceClass"`
	Capacity      int    `json:"capacity"`
}

type PlanScheduleBatch struct {
	Actions   []InspectPlannedAction `json:"actions,omitempty"`
	Resources []PlanResourceUsage    `json:"resources,omitempty"`
}

type PlanResourceUsage struct {
	ResourceClass string `json:"resourceClass"`
	Capacity      int    `json:"capacity"`
	Used          int    `json:"used"`
	Remaining     int    `json:"remaining"`
}

type ProvenanceResult struct {
	Repo          string                 `json:"repo"`
	ModelCacheKey string                 `json:"modelCacheKey,omitempty"`
	Provenance    integration.Provenance `json:"provenance"`
}

type ClasspathProvenanceResult struct {
	Repo          string                                `json:"repo"`
	ModelCacheKey string                                `json:"modelCacheKey,omitempty"`
	Provenance    integration.ClasspathProvenanceResult `json:"provenance"`
}

type ClasspathSnapshotResult struct {
	Repo          string                              `json:"repo"`
	ModelCacheKey string                              `json:"modelCacheKey,omitempty"`
	Snapshot      integration.ClasspathSnapshotResult `json:"snapshot"`
}

type ClasspathSnapshotProvenanceResult struct {
	Repo                string                                        `json:"repo"`
	ClasspathSnapshotID string                                        `json:"classpathSnapshotId"`
	ModelCacheKey       string                                        `json:"modelCacheKey,omitempty"`
	Provenance          integration.ClasspathSnapshotProvenanceResult `json:"provenance"`
}

type ClasspathSnapshotConsumersResult struct {
	Repo                string                                       `json:"repo"`
	ClasspathSnapshotID string                                       `json:"classpathSnapshotId"`
	ModelCacheKey       string                                       `json:"modelCacheKey,omitempty"`
	Consumers           integration.ClasspathSnapshotConsumersResult `json:"consumers"`
}

type ClasspathEntryLookupResult struct {
	Repo          string                                 `json:"repo"`
	ModelCacheKey string                                 `json:"modelCacheKey,omitempty"`
	Lookup        integration.ClasspathEntryLookupResult `json:"lookup"`
}

type ClasspathPathConsumersResult struct {
	Repo          string                                   `json:"repo"`
	ModelCacheKey string                                   `json:"modelCacheKey,omitempty"`
	Consumers     integration.ClasspathPathConsumersResult `json:"consumers"`
}

type ArtifactOnClasspathResult struct {
	Repo          string                                `json:"repo"`
	ModelCacheKey string                                `json:"modelCacheKey,omitempty"`
	Lookup        integration.ArtifactOnClasspathResult `json:"lookup"`
}

type ArtifactClasspathConsumersResult struct {
	Repo          string                                       `json:"repo"`
	ArtifactID    string                                       `json:"artifactId"`
	ModelCacheKey string                                       `json:"modelCacheKey,omitempty"`
	Consumers     integration.ArtifactClasspathConsumersResult `json:"consumers"`
}

type FileOwnersResult struct {
	Repo          string                       `json:"repo"`
	ModelCacheKey string                       `json:"modelCacheKey,omitempty"`
	Owners        integration.FileOwnersResult `json:"owners"`
}

type ActionInputsResult struct {
	Repo          string                         `json:"repo"`
	ActionID      string                         `json:"actionId"`
	ModelCacheKey string                         `json:"modelCacheKey,omitempty"`
	Inputs        integration.ActionInputsResult `json:"inputs"`
}

type ActionOutputsResult struct {
	Repo          string                          `json:"repo"`
	ActionID      string                          `json:"actionId"`
	ModelCacheKey string                          `json:"modelCacheKey,omitempty"`
	Outputs       integration.ActionOutputsResult `json:"outputs"`
}

type ActionDependenciesResult struct {
	Repo          string                               `json:"repo"`
	ActionID      string                               `json:"actionId"`
	ModelCacheKey string                               `json:"modelCacheKey,omitempty"`
	Dependencies  integration.ActionDependenciesResult `json:"dependencies"`
}

type ActionDependentsResult struct {
	Repo          string                             `json:"repo"`
	ActionID      string                             `json:"actionId"`
	ModelCacheKey string                             `json:"modelCacheKey,omitempty"`
	Dependents    integration.ActionDependentsResult `json:"dependents"`
}

type ActionsForModuleResult struct {
	Repo          string                      `json:"repo"`
	Module        string                      `json:"module"`
	ModelCacheKey string                      `json:"modelCacheKey,omitempty"`
	Actions       []configmodel.ActionSummary `json:"actions,omitempty"`
}

type ActionsForVariantResult struct {
	Repo          string                      `json:"repo"`
	Module        string                      `json:"module"`
	Variant       string                      `json:"variant"`
	ModelCacheKey string                      `json:"modelCacheKey,omitempty"`
	Actions       []configmodel.ActionSummary `json:"actions,omitempty"`
}

type AndroidCapabilityReportResult struct {
	Repo     string                           `json:"repo"`
	Module   string                           `json:"module"`
	Variants []AndroidCapabilityVariantResult `json:"variants,omitempty"`
}

type AndroidCapabilityVariantResult struct {
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
	SigningStoreFile          string                            `json:"signingStoreFile,omitempty"`
	SigningKeyAlias           string                            `json:"signingKeyAlias,omitempty"`
	HasStorePassword          bool                              `json:"hasStorePassword,omitempty"`
	HasKeyPassword            bool                              `json:"hasKeyPassword,omitempty"`
	DexMode                   string                            `json:"dexMode,omitempty"`
	MinifyEnabled             bool                              `json:"minifyEnabled,omitempty"`
	ShrinkResources           bool                              `json:"shrinkResources,omitempty"`
	InstallTask               string                            `json:"installTask,omitempty"`
	UninstallTask             string                            `json:"uninstallTask,omitempty"`
	AndroidTestPackage        string                            `json:"androidTestPackage,omitempty"`
	AndroidTestManifest       string                            `json:"androidTestManifest,omitempty"`
	AndroidTestInstallTask    string                            `json:"androidTestInstallTask,omitempty"`
	AndroidTestUninstallTask  string                            `json:"androidTestUninstallTask,omitempty"`
}

type VariantMaterializationResult struct {
	Repo          string                                   `json:"repo"`
	ModelCacheKey string                                   `json:"modelCacheKey,omitempty"`
	Provenance    integration.VariantMaterializationResult `json:"provenance"`
}

type VariantSourceSetModelResult struct {
	Repo           string                                  `json:"repo"`
	ModelCacheKey  string                                  `json:"modelCacheKey,omitempty"`
	SourceSetModel integration.VariantSourceSetModelResult `json:"sourceSetModel"`
}

type DependencyBindingsForVariantResult struct {
	Repo          string                                         `json:"repo"`
	ModelCacheKey string                                         `json:"modelCacheKey,omitempty"`
	Bindings      integration.DependencyBindingsForVariantResult `json:"bindings"`
}

type DependencyBindingsForModuleResult struct {
	Repo          string                                        `json:"repo"`
	ModelCacheKey string                                        `json:"modelCacheKey,omitempty"`
	Bindings      integration.DependencyBindingsForModuleResult `json:"bindings"`
}

type DependencyRealizationsForVariantResult struct {
	Repo          string                                             `json:"repo"`
	ModelCacheKey string                                             `json:"modelCacheKey,omitempty"`
	Realizations  integration.DependencyRealizationsForVariantResult `json:"realizations"`
}

type DependencyRealizationsForModuleResult struct {
	Repo          string                                            `json:"repo"`
	ModelCacheKey string                                            `json:"modelCacheKey,omitempty"`
	Realizations  integration.DependencyRealizationsForModuleResult `json:"realizations"`
}

type ModuleByIDResult struct {
	Repo          string                       `json:"repo"`
	ModuleID      string                       `json:"moduleId"`
	ModelCacheKey string                       `json:"modelCacheKey,omitempty"`
	Result        integration.ModuleByIDResult `json:"result"`
}

type VariantByIDResult struct {
	Repo          string                        `json:"repo"`
	VariantID     string                        `json:"variantId"`
	ModelCacheKey string                        `json:"modelCacheKey,omitempty"`
	Result        integration.VariantByIDResult `json:"result"`
}

type ActionByIDResult struct {
	Repo          string                       `json:"repo"`
	ActionID      string                       `json:"actionId"`
	ModelCacheKey string                       `json:"modelCacheKey,omitempty"`
	Result        integration.ActionByIDResult `json:"result"`
}

type ArtifactByIDResult struct {
	Repo          string                         `json:"repo"`
	ArtifactID    string                         `json:"artifactId"`
	ModelCacheKey string                         `json:"modelCacheKey,omitempty"`
	Result        integration.ArtifactByIDResult `json:"result"`
}

type MaterializationByIDResult struct {
	Repo              string                                `json:"repo"`
	MaterializationID string                                `json:"materializationId"`
	ModelCacheKey     string                                `json:"modelCacheKey,omitempty"`
	Result            integration.MaterializationByIDResult `json:"result"`
}

type MaterializationConsumersResult struct {
	Repo              string                                     `json:"repo"`
	MaterializationID string                                     `json:"materializationId"`
	ModelCacheKey     string                                     `json:"modelCacheKey,omitempty"`
	Consumers         integration.MaterializationConsumersResult `json:"consumers"`
}

type PlannedActionPolicy struct {
	ActionID        string                      `json:"actionId"`
	Name            string                      `json:"name,omitempty"`
	Operation       string                      `json:"operation,omitempty"`
	ModulePath      string                      `json:"modulePath,omitempty"`
	VariantName     string                      `json:"variantName,omitempty"`
	BatchIndex      int                         `json:"batchIndex,omitempty"`
	WorkerClass     string                      `json:"workerClass,omitempty"`
	ResourceClass   string                      `json:"resourceClass,omitempty"`
	ResourceCost    int                         `json:"resourceCost,omitempty"`
	MaxParallelism  int                         `json:"maxParallelism,omitempty"`
	CacheKey        string                      `json:"cacheKey,omitempty"`
	Cacheable       bool                        `json:"cacheable,omitempty"`
	ProbeOrder      []string                    `json:"probeOrder,omitempty"`
	ExecuteOnMiss   bool                        `json:"executeOnMiss,omitempty"`
	ProbeHint       *responsepayload.CacheProbe `json:"probeHint,omitempty"`
	RetentionClass  string                      `json:"retentionClass,omitempty"`
	Shareability    string                      `json:"shareability,omitempty"`
	Dependencies    []string                    `json:"dependencies,omitempty"`
	InputArtifacts  []string                    `json:"inputArtifacts,omitempty"`
	OutputArtifacts []string                    `json:"outputArtifacts,omitempty"`
}

type PlannedActionPolicyResult struct {
	Repo             string              `json:"repo"`
	Module           string              `json:"module"`
	Command          string              `json:"command"`
	RequestedVariant string              `json:"requestedVariant,omitempty"`
	TargetVariant    string              `json:"targetVariant,omitempty"`
	VariantExplicit  bool                `json:"variantExplicit,omitempty"`
	ActionID         string              `json:"actionId"`
	ModelCacheKey    string              `json:"modelCacheKey,omitempty"`
	Policy           PlannedActionPolicy `json:"policy"`
}

type PlannedActionPoliciesResult struct {
	Repo             string                `json:"repo"`
	Module           string                `json:"module"`
	Command          string                `json:"command"`
	RequestedVariant string                `json:"requestedVariant,omitempty"`
	TargetVariant    string                `json:"targetVariant,omitempty"`
	VariantExplicit  bool                  `json:"variantExplicit,omitempty"`
	ModelCacheKey    string                `json:"modelCacheKey,omitempty"`
	Policies         []PlannedActionPolicy `json:"policies,omitempty"`
}

func toPlannedActionPolicy(action InspectPlannedAction, batchIndex int) PlannedActionPolicy {
	return PlannedActionPolicy{
		ActionID:        action.ID,
		Name:            action.Name,
		Operation:       action.Operation,
		ModulePath:      action.ModulePath,
		VariantName:     action.VariantName,
		BatchIndex:      batchIndex,
		WorkerClass:     action.WorkerClass,
		ResourceClass:   action.ResourceClass,
		ResourceCost:    action.ResourceCost,
		MaxParallelism:  action.MaxParallelism,
		CacheKey:        action.CacheKey,
		Cacheable:       action.Cacheable,
		ProbeOrder:      append([]string(nil), action.ProbeOrder...),
		ExecuteOnMiss:   action.ExecuteOnMiss,
		ProbeHint:       action.ProbeHint,
		RetentionClass:  action.RetentionClass,
		Shareability:    action.Shareability,
		Dependencies:    append([]string(nil), action.Dependencies...),
		InputArtifacts:  append([]string(nil), action.Inputs...),
		OutputArtifacts: append([]string(nil), action.Outputs...),
	}
}

type ClasspathSnapshotByIDResult struct {
	Repo                string                                  `json:"repo"`
	ClasspathSnapshotID string                                  `json:"classpathSnapshotId"`
	ModelCacheKey       string                                  `json:"modelCacheKey,omitempty"`
	Result              integration.ClasspathSnapshotByIDResult `json:"result"`
}

type ClasspathSnapshotConsumersByIDResult struct {
	Repo                string                                           `json:"repo"`
	ClasspathSnapshotID string                                           `json:"classpathSnapshotId"`
	ModelCacheKey       string                                           `json:"modelCacheKey,omitempty"`
	Consumers           integration.ClasspathSnapshotConsumersByIDResult `json:"consumers"`
}

type MaterializationProvenanceResult struct {
	Repo              string                                      `json:"repo"`
	MaterializationID string                                      `json:"materializationId"`
	ModelCacheKey     string                                      `json:"modelCacheKey,omitempty"`
	Provenance        integration.MaterializationProvenanceResult `json:"provenance"`
}

type VariantCompatibilityResult struct {
	Repo                      string                                 `json:"repo"`
	ModelCacheKey             string                                 `json:"modelCacheKey,omitempty"`
	ModulePath                string                                 `json:"modulePath,omitempty"`
	VariantName               string                                 `json:"variantName,omitempty"`
	DeclaredName              string                                 `json:"declaredName,omitempty"`
	CoordinateName            string                                 `json:"coordinateName,omitempty"`
	VariantID                 string                                 `json:"variantId,omitempty"`
	DisplayName               string                                 `json:"displayName,omitempty"`
	BuildType                 string                                 `json:"buildType,omitempty"`
	Flavors                   []string                               `json:"flavors,omitempty"`
	CompileSDK                string                                 `json:"compileSdk,omitempty"`
	BuildToolsVersion         string                                 `json:"buildToolsVersion,omitempty"`
	Namespace                 string                                 `json:"namespace,omitempty"`
	ApplicationID             string                                 `json:"applicationId,omitempty"`
	ApplicationIDSuffix       string                                 `json:"applicationIdSuffix,omitempty"`
	VersionCode               string                                 `json:"versionCode,omitempty"`
	VersionName               string                                 `json:"versionName,omitempty"`
	VersionNameSuffix         string                                 `json:"versionNameSuffix,omitempty"`
	MinSDK                    string                                 `json:"minSdk,omitempty"`
	TargetSDK                 string                                 `json:"targetSdk,omitempty"`
	TestInstrumentationRunner string                                 `json:"testInstrumentationRunner,omitempty"`
	Optimization              project.VariantOptimization            `json:"optimization,omitempty"`
	ProguardFiles             []string                               `json:"proguardFiles,omitempty"`
	ConsumerProguardFiles     []string                               `json:"consumerProguardFiles,omitempty"`
	Installable               bool                                   `json:"installable,omitempty"`
	Testable                  bool                                   `json:"testable,omitempty"`
	Debuggable                bool                                   `json:"debuggable,omitempty"`
	SigningConfigured         bool                                   `json:"signingConfigured,omitempty"`
	SigningConfig             string                                 `json:"signingConfig,omitempty"`
	DexMode                   string                                 `json:"dexMode,omitempty"`
	MinifyEnabled             bool                                   `json:"minifyEnabled,omitempty"`
	ShrinkResources           bool                                   `json:"shrinkResources,omitempty"`
	MaterializationID         string                                 `json:"materializationId,omitempty"`
	ArtifactSnapshotID        string                                 `json:"artifactSnapshotId,omitempty"`
	SourceRoots               []string                               `json:"sourceRoots,omitempty"`
	ManifestPaths             []string                               `json:"manifestPaths,omitempty"`
	ClasspathSnapshotIDs      []string                               `json:"classpathSnapshotIds,omitempty"`
	ProducedArtifactPaths     []string                               `json:"producedArtifactPaths,omitempty"`
	ProducedArtifactKinds     []string                               `json:"producedArtifactKinds,omitempty"`
	InstallArtifactID         string                                 `json:"installArtifactId,omitempty"`
	InstallArtifactPath       string                                 `json:"installArtifactPath,omitempty"`
	ResourceArtifactIDs       []string                               `json:"resourceArtifactIds,omitempty"`
	ResourceArtifactPaths     []string                               `json:"resourceArtifactPaths,omitempty"`
	ManifestArtifactIDs       []string                               `json:"manifestArtifactIds,omitempty"`
	ManifestArtifactPaths     []string                               `json:"manifestArtifactPaths,omitempty"`
	BackingArtifactPath       string                                 `json:"backingArtifactPath,omitempty"`
	SigningStoreFile          string                                 `json:"signingStoreFile,omitempty"`
	SigningKeyAlias           string                                 `json:"signingKeyAlias,omitempty"`
	HasStorePassword          bool                                   `json:"hasStorePassword,omitempty"`
	HasKeyPassword            bool                                   `json:"hasKeyPassword,omitempty"`
	InstallTask               string                                 `json:"installTask,omitempty"`
	UninstallTask             string                                 `json:"uninstallTask,omitempty"`
	Compatibility             project.VariantCompatibility           `json:"compatibility"`
	Materialization           project.SemanticMaterializationSummary `json:"materialization"`
	Provenance                configmodel.ProvenanceSummary          `json:"provenance"`
}

type ArtifactsForVariantResult struct {
	Repo               string                        `json:"repo"`
	Module             string                        `json:"module"`
	Variant            string                        `json:"variant"`
	ModelCacheKey      string                        `json:"modelCacheKey,omitempty"`
	MaterializationID  string                        `json:"materializationId,omitempty"`
	ArtifactSnapshotID string                        `json:"artifactSnapshotId,omitempty"`
	Artifacts          []configmodel.ArtifactSummary `json:"artifacts,omitempty"`
}

type ArtifactsForModuleResult struct {
	Repo                string                        `json:"repo"`
	Module              string                        `json:"module"`
	ModelCacheKey       string                        `json:"modelCacheKey,omitempty"`
	VariantNames        []string                      `json:"variantNames,omitempty"`
	MaterializationIDs  []string                      `json:"materializationIds,omitempty"`
	ArtifactSnapshotIDs []string                      `json:"artifactSnapshotIds,omitempty"`
	Artifacts           []configmodel.ArtifactSummary `json:"artifacts,omitempty"`
}

type ModuleManifestResult struct {
	Repo          string                           `json:"repo"`
	ModelCacheKey string                           `json:"modelCacheKey,omitempty"`
	Manifest      integration.ModuleManifestResult `json:"manifest"`
}

type VariantManifestResult struct {
	Repo          string                            `json:"repo"`
	ModelCacheKey string                            `json:"modelCacheKey,omitempty"`
	Manifest      integration.VariantManifestResult `json:"manifest"`
}

type ArtifactSnapshotProvenanceResult struct {
	Repo          string                                       `json:"repo"`
	ModelCacheKey string                                       `json:"modelCacheKey,omitempty"`
	Provenance    integration.ArtifactSnapshotProvenanceResult `json:"provenance"`
}

type ArtifactSnapshotConsumersResult struct {
	Repo               string                                      `json:"repo"`
	ArtifactSnapshotID string                                      `json:"artifactSnapshotId"`
	ModelCacheKey      string                                      `json:"modelCacheKey,omitempty"`
	Consumers          integration.ArtifactSnapshotConsumersResult `json:"consumers"`
}

type ArtifactConsumersResult struct {
	Repo          string                              `json:"repo"`
	ArtifactID    string                              `json:"artifactId"`
	ModelCacheKey string                              `json:"modelCacheKey,omitempty"`
	Consumers     integration.ArtifactConsumersResult `json:"consumers"`
}

type VariantImpactResult struct {
	Repo          string       `json:"repo"`
	ModelCacheKey string       `json:"modelCacheKey,omitempty"`
	Module        string       `json:"module"`
	Variant       string       `json:"variant"`
	Dependents    []ImpactNode `json:"dependents,omitempty"`
}

type ModuleImpactResult struct {
	Repo          string       `json:"repo"`
	ModelCacheKey string       `json:"modelCacheKey,omitempty"`
	Module        string       `json:"module"`
	Dependents    []ImpactNode `json:"dependents,omitempty"`
}

type CleanupPlanResult struct {
	Repo          string                  `json:"repo"`
	ModelCacheKey string                  `json:"modelCacheKey,omitempty"`
	Plan          cachepolicy.CleanupPlan `json:"plan"`
}

type RunSummaryResult struct {
	Repo    string           `json:"repo"`
	Module  string           `json:"module"`
	Command string           `json:"command"`
	Path    string           `json:"path"`
	Summary RunSummaryRecord `json:"summary"`
}

type RunSummariesResult struct {
	Repo    string            `json:"repo"`
	Module  string            `json:"module,omitempty"`
	Entries []RunSummaryEntry `json:"entries,omitempty"`
}

type RunGraphSummaryResult struct {
	Repo    string          `json:"repo"`
	Module  string          `json:"module"`
	Command string          `json:"command"`
	Path    string          `json:"path"`
	Summary RunGraphSummary `json:"summary"`
}

type PlannedScheduleResult struct {
	Repo    string             `json:"repo"`
	Module  string             `json:"module"`
	Command string             `json:"command"`
	Path    string             `json:"path"`
	Summary PlanScheduleResult `json:"summary"`
}

type ScheduleDriftAction struct {
	ActionID           string `json:"actionId"`
	Name               string `json:"name,omitempty"`
	Operation          string `json:"operation,omitempty"`
	ModulePath         string `json:"modulePath,omitempty"`
	VariantName        string `json:"variantName,omitempty"`
	Planned            bool   `json:"planned"`
	Executed           bool   `json:"executed"`
	PlannedBatchIndex  int    `json:"plannedBatchIndex"`
	ExecutedBatchIndex int    `json:"executedBatchIndex"`
	BatchMismatch      bool   `json:"batchMismatch"`
	CriticalPath       bool   `json:"criticalPath,omitempty"`
	QueueWaitMs        int64  `json:"queueWaitMs,omitempty"`
	WaitReason         string `json:"waitReason,omitempty"`
	Status             string `json:"status,omitempty"`
}

type ScheduleDriftSummary struct {
	PlannedActionCount      int                   `json:"plannedActionCount"`
	ExecutedActionCount     int                   `json:"executedActionCount"`
	MatchedActionCount      int                   `json:"matchedActionCount"`
	PlannedOnlyCount        int                   `json:"plannedOnlyCount"`
	ExecutedOnlyCount       int                   `json:"executedOnlyCount"`
	BatchMismatchCount      int                   `json:"batchMismatchCount"`
	PlannedBatchCount       int                   `json:"plannedBatchCount,omitempty"`
	ExecutedBatchCount      int                   `json:"executedBatchCount,omitempty"`
	QueueWaitActions        int                   `json:"queueWaitActions,omitempty"`
	CriticalPathActions     int                   `json:"criticalPathActions,omitempty"`
	MaxQueueWaitMs          int64                 `json:"maxQueueWaitMs,omitempty"`
	EstimatedCriticalPathMs int64                 `json:"estimatedCriticalPathMs,omitempty"`
	PlannedOnlyActionIDs    []string              `json:"plannedOnlyActionIds,omitempty"`
	ExecutedOnlyActionIDs   []string              `json:"executedOnlyActionIds,omitempty"`
	BatchMismatchActionIDs  []string              `json:"batchMismatchActionIds,omitempty"`
	RepresentativeActionIDs []string              `json:"representativeActionIds,omitempty"`
	RootActionIDs           []string              `json:"rootActionIds,omitempty"`
	QueueWaitReasonCounts   map[string]int        `json:"queueWaitReasonCounts,omitempty"`
	Actions                 []ScheduleDriftAction `json:"actions,omitempty"`
}

type ScheduleDriftResult struct {
	Repo    string               `json:"repo"`
	Module  string               `json:"module"`
	Command string               `json:"command"`
	Path    string               `json:"path"`
	Summary ScheduleDriftSummary `json:"summary"`
}

type CriticalPathSummaryResult struct {
	Repo    string              `json:"repo"`
	Module  string              `json:"module"`
	Command string              `json:"command"`
	Path    string              `json:"path"`
	Summary CriticalPathSummary `json:"summary"`
}

type SchedulerSummaryResult struct {
	Repo    string           `json:"repo"`
	Module  string           `json:"module"`
	Command string           `json:"command"`
	Path    string           `json:"path"`
	Summary SchedulerSummary `json:"summary"`
}

type CacheSummaryResult struct {
	Repo    string       `json:"repo"`
	Module  string       `json:"module"`
	Command string       `json:"command"`
	Path    string       `json:"path"`
	Summary CacheSummary `json:"summary"`
}

type ToolSummaryResult struct {
	Repo    string      `json:"repo"`
	Module  string      `json:"module"`
	Command string      `json:"command"`
	Path    string      `json:"path"`
	Summary ToolSummary `json:"summary"`
}

type DiagnosticsResult struct {
	Repo        string             `json:"repo"`
	Module      string             `json:"module"`
	Command     string             `json:"command"`
	Path        string             `json:"path"`
	Diagnostics []DiagnosticRecord `json:"diagnostics,omitempty"`
}

type DiagnosticSummaryResult struct {
	Repo    string            `json:"repo"`
	Module  string            `json:"module"`
	Command string            `json:"command"`
	Path    string            `json:"path"`
	Summary DiagnosticSummary `json:"summary"`
}

type MaterializationsResult struct {
	Repo             string                                   `json:"repo"`
	Module           string                                   `json:"module"`
	Command          string                                   `json:"command"`
	Path             string                                   `json:"path"`
	Materializations []project.SemanticMaterializationSummary `json:"materializations,omitempty"`
}

type ActionTraceSubstep struct {
	Name        string `json:"name,omitempty"`
	Depth       int    `json:"depth,omitempty"`
	DurationMs  int64  `json:"durationMs,omitempty"`
	CacheResult string `json:"cacheResult,omitempty"`
	CacheBasis  string `json:"cacheBasis,omitempty"`
}

type ActionTraceEntry struct {
	ActionID       string               `json:"actionId"`
	Name           string               `json:"name,omitempty"`
	Operation      string               `json:"operation,omitempty"`
	ModulePath     string               `json:"modulePath,omitempty"`
	VariantName    string               `json:"variantName,omitempty"`
	BatchIndex     int                  `json:"batchIndex,omitempty"`
	CriticalPath   bool                 `json:"criticalPath,omitempty"`
	QueueWaitMs    int64                `json:"queueWaitMs,omitempty"`
	WaitReason     string               `json:"waitReason,omitempty"`
	WorkerClass    string               `json:"workerClass,omitempty"`
	ResourceClass  string               `json:"resourceClass,omitempty"`
	ResourceCost   int                  `json:"resourceCost,omitempty"`
	MaxParallelism int                  `json:"maxParallelism,omitempty"`
	CacheKey       string               `json:"cacheKey,omitempty"`
	Cacheable      bool                 `json:"cacheable,omitempty"`
	ProbeOrder     []string             `json:"probeOrder,omitempty"`
	ExecuteOnMiss  bool                 `json:"executeOnMiss,omitempty"`
	RetentionClass string               `json:"retentionClass,omitempty"`
	Shareability   string               `json:"shareability,omitempty"`
	Status         string               `json:"status,omitempty"`
	DurationMs     int64                `json:"durationMs,omitempty"`
	CacheResult    string               `json:"cacheResult,omitempty"`
	CacheBasis     string               `json:"cacheBasis,omitempty"`
	Substeps       []ActionTraceSubstep `json:"substeps,omitempty"`
	Timings        []perf.TimingEntry   `json:"timings,omitempty"`
}

type ActionTraceResult struct {
	Repo    string             `json:"repo"`
	Module  string             `json:"module"`
	Command string             `json:"command"`
	Path    string             `json:"path"`
	Actions []ActionTraceEntry `json:"actions,omitempty"`
}

type ActionExecutionResult struct {
	Repo      string          `json:"repo"`
	Module    string          `json:"module"`
	Command   string          `json:"command"`
	Path      string          `json:"path"`
	ActionID  string          `json:"actionId"`
	Execution ActionExecution `json:"execution"`
	Explain   *explain.Action `json:"explanation,omitempty"`
}

type ActionExplanationResult struct {
	Repo      string           `json:"repo"`
	Module    string           `json:"module"`
	Command   string           `json:"command"`
	Path      string           `json:"path"`
	ActionID  string           `json:"actionId"`
	Explain   explain.Action   `json:"explanation"`
	Execution *ActionExecution `json:"execution,omitempty"`
}

type ActionExecutionsResult struct {
	Repo       string            `json:"repo"`
	Module     string            `json:"module"`
	Command    string            `json:"command"`
	Path       string            `json:"path"`
	Executions []ActionExecution `json:"executions,omitempty"`
}

type ActionExplanationsResult struct {
	Repo         string           `json:"repo"`
	Module       string           `json:"module"`
	Command      string           `json:"command"`
	Path         string           `json:"path"`
	Explanations []explain.Action `json:"explanations,omitempty"`
}

type CacheProbesResult struct {
	Repo    string                       `json:"repo"`
	Module  string                       `json:"module"`
	Command string                       `json:"command"`
	Path    string                       `json:"path"`
	Probes  []responsepayload.CacheProbe `json:"probes,omitempty"`
}

type CacheProbeRecordsResult struct {
	Repo    string                             `json:"repo"`
	Module  string                             `json:"module"`
	Command string                             `json:"command"`
	Path    string                             `json:"path"`
	Records []responsepayload.CacheProbeRecord `json:"records,omitempty"`
}

type ReuseDecision struct {
	ActionID     string                             `json:"actionId"`
	Name         string                             `json:"name,omitempty"`
	Operation    string                             `json:"operation,omitempty"`
	ModulePath   string                             `json:"modulePath,omitempty"`
	VariantName  string                             `json:"variantName,omitempty"`
	CacheOutcome string                             `json:"cacheOutcome,omitempty"`
	CacheSource  string                             `json:"cacheSource,omitempty"`
	Basis        []string                           `json:"basis,omitempty"`
	Reasons      []string                           `json:"reasons,omitempty"`
	Probe        *responsepayload.CacheProbe        `json:"probe,omitempty"`
	ProbeRecords []responsepayload.CacheProbeRecord `json:"probeRecords,omitempty"`
	Execution    *ActionExecution                   `json:"execution,omitempty"`
	Explain      *explain.Action                    `json:"explanation,omitempty"`
}

type ReuseDecisionResult struct {
	Repo     string        `json:"repo"`
	Module   string        `json:"module"`
	Command  string        `json:"command"`
	Path     string        `json:"path"`
	ActionID string        `json:"actionId"`
	Decision ReuseDecision `json:"decision"`
}

type ReuseDecisionsResult struct {
	Repo      string          `json:"repo"`
	Module    string          `json:"module"`
	Command   string          `json:"command"`
	Path      string          `json:"path"`
	Decisions []ReuseDecision `json:"decisions,omitempty"`
}

type PerfTimingResult struct {
	Repo    string           `json:"repo"`
	Module  string           `json:"module"`
	Command string           `json:"command"`
	Path    string           `json:"path"`
	Timing  *perf.TimingData `json:"timing,omitempty"`
}

type ImpactNode struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	ModulePath  string `json:"modulePath,omitempty"`
	VariantName string `json:"variantName,omitempty"`
	Name        string `json:"name,omitempty"`
}

type InspectModule struct {
	Path                      string
	Dir                       string
	Type                      string
	Namespace                 string
	ApplicationID             string
	VersionCode               string
	VersionName               string
	CompileSDK                string
	BuildToolsVersion         string
	MinSDK                    string
	TargetSDK                 string
	TestInstrumentationRunner string
	SourceFiles               int
	TestFiles                 int
	AndroidTestFiles          int
	UsesCompose               bool
	UsesMetro                 bool
	UsesWire                  bool
	WireConfig                *project.WireConfig
	KotlinFreeArgs            []string
	LintDisabled              []string
	ConsumerProguardFiles     []string
	Variants                  []project.BuildType
	ResolvedVariants          []project.ResolvedVariant
	RequestedTasks            []string
	Tasks                     []project.Task
}

type TasksResult struct {
	Repo   string
	Module string
	Tasks  []project.Task
}

type SigningReportResult struct {
	Repo     string
	Module   string
	Variants []SigningReportVariant
}

type SigningReportVariant struct {
	Name             string
	SigningConfig    string
	ResolvedConfig   string
	StoreFile        string
	KeyAlias         string
	HasStorePassword bool
	HasKeyPassword   bool
}

type ProjectsResult struct {
	Repo    string
	Name    string
	Modules []string
}

type PropertiesResult struct {
	Repo             string
	Module           string
	Type             string
	Values           responsepayload.PropertiesValues
	Variants         []project.BuildType
	ResolvedVariants []project.ResolvedVariant
}

type DependenciesResult struct {
	Repo   string
	Module string
	Scopes map[string][]string
}

type BuildEnvironmentResult struct {
	Repo             string
	SettingsFile     string
	RootBuildFile    string
	Repositories     []project.Repository
	GradleProperties map[string]string
	VersionCatalogs  []string
}

type ArtifactTransformsResult struct {
	Repo       string
	Module     string
	Transforms []string
}

type DependencyInsightResult struct {
	Repo    string
	Module  string
	Query   string
	Scopes  map[string][]string
	Matches map[string][]string
}

type KotlinDslAccessorsReportResult struct {
	Repo      string
	Module    string
	Accessors []string
}

type OutgoingVariantsResult struct {
	Repo             string
	Module           string
	Variants         []project.BuildType
	ResolvedVariants []project.ResolvedVariant
}

type ResolvableConfigurationsResult struct {
	Repo           string
	Module         string
	Configurations map[string][]string
}

type ResolverReportResult struct {
	Repo         string
	Module       string
	CachePath    string
	ReportPath   string
	ReplayPath   string
	LockfilePath string
	Found        bool
	Topology     m2local.CacheTopology
	Inputs       m2local.ResolvedCacheInputs
	Summary      ResolverReportSummary
	Report       m2local.ResolutionReport
	Replay       m2local.ResolutionReplay
	Lockfile     m2local.ResolutionLockfile
}

type CacheTopologyResult struct {
	Repo     string                `json:"repo"`
	Topology m2local.CacheTopology `json:"topology"`
}

type ResolverReportSummary struct {
	CompileJarCount     int
	RuntimeJarCount     int
	TestJarCount        int
	AndroidLibraryCount int
	SelectionCount      int
	ConflictCount       int
	PinCount            int
}

func (s *Service) Inspect(prj *project.Project) InspectResult {
	model, err := s.LoadConfigurationModel(context.Background(), prj)
	summary := project.SemanticGraphSummary{}
	if err == nil && model != nil {
		summary = model.GraphSummary()
	} else {
		summary = configmodel.SummaryFromProject(prj)
	}
	result := InspectResult{
		Repo:            prj.RootDir,
		Name:            prj.Name,
		Backend:         prj.RecommendedBackend,
		RootBuildFile:   prj.RootBuildFile,
		SettingsFile:    prj.SettingsFile,
		VersionCatalog:  prj.VersionCatalog,
		VersionCatalogs: prj.VersionCatalogs,
		Repositories:    prj.Repositories,
		Plugins:         prj.RootPlugins,
		SemanticGraph:   summary,
		Modules:         make([]InspectModule, 0, len(prj.Modules)),
	}
	for _, mod := range prj.Modules {
		resolvedVariants := resolvedVariantsForModule(model, prj, &mod)
		result.Modules = append(result.Modules, InspectModule{
			Path:                      mod.Path,
			Dir:                       mod.Dir,
			Type:                      mod.Type,
			Namespace:                 mod.Namespace,
			ApplicationID:             mod.ApplicationID,
			VersionCode:               mod.VersionCode,
			VersionName:               mod.VersionName,
			CompileSDK:                mod.CompileSDK,
			BuildToolsVersion:         mod.BuildToolsVersion,
			MinSDK:                    mod.MinSDK,
			TargetSDK:                 mod.TargetSDK,
			TestInstrumentationRunner: mod.TestInstrumentationRunner,
			SourceFiles:               mod.SourceFileCount,
			TestFiles:                 mod.UnitTestFileCount,
			AndroidTestFiles:          mod.AndroidTestFileCount,
			UsesCompose:               mod.UsesCompose,
			UsesMetro:                 mod.UsesMetro,
			UsesWire:                  mod.UsesWire,
			WireConfig:                wireConfigPointer(mod),
			KotlinFreeArgs:            mod.KotlinFreeCompilerArgs,
			LintDisabled:              mod.LintDisabledChecks,
			ConsumerProguardFiles:     mod.ConsumerProguardFiles,
			Variants:                  mod.Variants(),
			ResolvedVariants:          resolvedVariants,
			RequestedTasks:            mod.DefaultTasks(),
			Tasks:                     mod.Tasks(),
		})
	}
	return result
}

func (s *Service) ExplainPlan(ctx context.Context, prj *project.Project, mod *project.Module, command string, requestedVariant string, variantExplicit bool) (PlanExplanationResult, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return PlanExplanationResult{}, err
	}
	plan, err := s.resolveExecutionPlanWithModel(ctx, model, prj, mod, command, requestedVariant, variantExplicit)
	if err != nil {
		return PlanExplanationResult{}, err
	}
	result := PlanExplanationResult{
		Repo:             prj.RootDir,
		Module:           mod.Path,
		Command:          command,
		RequestedVariant: requestedVariant,
		TargetVariant:    plan.TargetVariant,
		Variants:         append([]string(nil), plan.TargetVariants...),
		VariantExplicit:  variantExplicit,
		ModelCacheKey:    model.CacheKey(),
		Reasons:          planExplanationReasons(plan),
		Schedule:         toPlanScheduleResult(plan.Schedule),
		Actions:          make([]InspectPlannedAction, 0, len(plan.Schedule.Steps)),
	}
	for _, action := range plan.Actions {
		result.ActionIDs = append(result.ActionIDs, action.ID.String())
	}
	remoteDecisions := plannedRemoteProbeDecisions(plan.Schedule)
	for _, step := range plan.Schedule.Steps {
		remoteDecision := remoteDecisions[step.Action.ID.String()]
		result.Actions = append(result.Actions, InspectPlannedAction{
			ID:                   step.Action.ID.String(),
			Name:                 step.Action.Name,
			Operation:            step.Action.Attributes["operation"],
			ModulePath:           step.Action.Attributes["modulePath"],
			VariantName:          step.Action.Attributes["variantName"],
			WorkerClass:          step.WorkerClass,
			ResourceClass:        step.ResourceClass,
			ResourceCost:         step.ResourceCost,
			MaxParallelism:       step.MaxParallelism,
			CacheKey:             step.CacheKey,
			Cacheable:            step.Cacheable,
			ProbeOrder:           append([]string(nil), step.ProbeOrder...),
			ExecuteOnMiss:        step.ExecuteOnMiss,
			EstimatedBytes:       step.EstimatedBytes,
			DeferRemote:          remoteDecision.DeferRemote,
			RemoteProbeAdmission: toPlanRemoteProbeAdmission(remoteDecision),
			ProbeHint:            step.ProbeHint,
			RetentionClass:       step.RetentionClass,
			Shareability:         step.Shareability,
			Dependencies:         actionIDs(step.Dependencies),
			Inputs:               artifactIDs(step.Action.Inputs),
			Outputs:              artifactIDs(step.Action.Outputs),
		})
	}
	return result, nil
}

func toPlanScheduleResult(schedule configmodel.ActionSchedule) PlanScheduleResult {
	out := PlanScheduleResult{
		ResourceBudgets: make([]PlanResourceBudget, 0, len(schedule.ResourceBudgets)),
		Batches:         make([]PlanScheduleBatch, 0, len(schedule.Batches)),
	}
	remoteDecisions := plannedRemoteProbeDecisions(schedule)
	for _, budget := range schedule.ResourceBudgets {
		out.ResourceBudgets = append(out.ResourceBudgets, PlanResourceBudget{
			ResourceClass: budget.ResourceClass,
			Capacity:      budget.Capacity,
		})
	}
	if schedule.NetworkBudgetConfig != nil {
		out.NetworkBudgetConfig = &PlanNetworkBudget{
			CapacityBytes:     schedule.NetworkBudgetConfig.CapacityBytes,
			RefillBytesPerSec: schedule.NetworkBudgetConfig.RefillBytesPerSec,
		}
	}
	for batchIdx, batch := range schedule.Batches {
		stepResults := make([]InspectPlannedAction, 0, len(batch))
		for _, step := range batch {
			remoteDecision := remoteDecisions[step.Action.ID.String()]
			stepResults = append(stepResults, InspectPlannedAction{
				ID:                   step.Action.ID.String(),
				Name:                 step.Action.Name,
				Operation:            step.Action.Attributes["operation"],
				ModulePath:           step.Action.Attributes["modulePath"],
				VariantName:          step.Action.Attributes["variantName"],
				WorkerClass:          step.WorkerClass,
				ResourceClass:        step.ResourceClass,
				ResourceCost:         step.ResourceCost,
				MaxParallelism:       step.MaxParallelism,
				CacheKey:             step.CacheKey,
				Cacheable:            step.Cacheable,
				ProbeOrder:           append([]string(nil), step.ProbeOrder...),
				ExecuteOnMiss:        step.ExecuteOnMiss,
				EstimatedBytes:       step.EstimatedBytes,
				DeferRemote:          remoteDecision.DeferRemote,
				RemoteProbeAdmission: toPlanRemoteProbeAdmission(remoteDecision),
				ProbeHint:            step.ProbeHint,
				RetentionClass:       step.RetentionClass,
				Shareability:         step.Shareability,
				Dependencies:         actionIDs(step.Dependencies),
				Inputs:               artifactIDs(step.Action.Inputs),
				Outputs:              artifactIDs(step.Action.Outputs),
			})
		}
		resources := []PlanResourceUsage(nil)
		if batchIdx < len(schedule.BatchResources) {
			resources = make([]PlanResourceUsage, 0, len(schedule.BatchResources[batchIdx].Resources))
			for _, resource := range schedule.BatchResources[batchIdx].Resources {
				resources = append(resources, PlanResourceUsage{
					ResourceClass: resource.ResourceClass,
					Capacity:      resource.Capacity,
					Used:          resource.Used,
					Remaining:     resource.Remaining,
				})
			}
		}
		out.Batches = append(out.Batches, PlanScheduleBatch{
			Actions:   stepResults,
			Resources: resources,
		})
	}
	return out
}

func plannedRemoteProbeDecisions(schedule configmodel.ActionSchedule) map[string]admission.RemoteProbeDecision {
	if schedule.NetworkBudgetConfig == nil || (len(schedule.Batches) == 0 && len(schedule.Steps) == 0) {
		return nil
	}
	scheduler := execbackend.NewSchedulerFromSchedule(schedule)
	decisions := make(map[string]admission.RemoteProbeDecision, len(schedule.Steps))
	for {
		ready := scheduler.ReadyWithRemoteProbeDecisions()
		if len(ready) == 0 {
			break
		}
		for _, action := range ready {
			decisions[action.Step.Action.ID.String()] = action.RemoteProbeDecision
		}
		for _, action := range ready {
			if err := scheduler.Complete(action.Step.Action.ID); err != nil {
				return decisions
			}
		}
	}
	if len(decisions) == 0 {
		return nil
	}
	return decisions
}

func toPlanRemoteProbeAdmission(decision admission.RemoteProbeDecision) *PlanRemoteProbeAdmission {
	if !decision.Eligible {
		return nil
	}
	return &PlanRemoteProbeAdmission{
		Deferred:          decision.DeferRemote,
		BudgetBeforeBytes: decision.BudgetBeforeBytes,
		BudgetAfterBytes:  decision.BudgetAfterBytes,
	}
}

func (s *Service) VariantProvenance(ctx context.Context, prj *project.Project, modulePath, variantName string) (ProvenanceResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ProvenanceResult{}, err
	}
	provenance, ok := view.ProvenanceForVariant(modulePath, variantName)
	if !ok {
		return ProvenanceResult{}, os.ErrNotExist
	}
	return ProvenanceResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Provenance:    provenance,
	}, nil
}

func (s *Service) ActionProvenance(ctx context.Context, prj *project.Project, actionID string) (ProvenanceResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ProvenanceResult{}, err
	}
	provenance, ok := view.ProvenanceForAction(graph.ActionID(actionID))
	if !ok {
		return ProvenanceResult{}, os.ErrNotExist
	}
	return ProvenanceResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Provenance:    provenance,
	}, nil
}

func (s *Service) ArtifactProvenance(ctx context.Context, prj *project.Project, artifactID string) (ProvenanceResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ProvenanceResult{}, err
	}
	provenance, ok := view.ProvenanceForArtifact(graph.ArtifactID(artifactID))
	if !ok {
		return ProvenanceResult{}, os.ErrNotExist
	}
	return ProvenanceResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Provenance:    provenance,
	}, nil
}

func (s *Service) ClasspathProvenance(ctx context.Context, prj *project.Project, modulePath, variantName string) (ClasspathProvenanceResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ClasspathProvenanceResult{}, err
	}
	provenance, ok := view.ClasspathProvenanceForVariant(modulePath, variantName)
	if !ok {
		return ClasspathProvenanceResult{}, os.ErrNotExist
	}
	return ClasspathProvenanceResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Provenance:    provenance,
	}, nil
}

func (s *Service) ClasspathSnapshot(ctx context.Context, prj *project.Project, modulePath, variantName string) (ClasspathSnapshotResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ClasspathSnapshotResult{}, err
	}
	snapshot, ok := view.ClasspathSnapshot(modulePath, variantName)
	if !ok {
		return ClasspathSnapshotResult{}, os.ErrNotExist
	}
	return ClasspathSnapshotResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Snapshot:      snapshot,
	}, nil
}

func (s *Service) ClasspathSnapshotProvenance(ctx context.Context, prj *project.Project, snapshotID string) (ClasspathSnapshotProvenanceResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ClasspathSnapshotProvenanceResult{}, err
	}
	provenance, ok := view.ClasspathSnapshotProvenance(snapshotID)
	if !ok {
		return ClasspathSnapshotProvenanceResult{}, os.ErrNotExist
	}
	return ClasspathSnapshotProvenanceResult{
		Repo:                prj.RootDir,
		ClasspathSnapshotID: snapshotID,
		ModelCacheKey:       view.CacheKey(),
		Provenance:          provenance,
	}, nil
}

func (s *Service) ClasspathSnapshotConsumers(ctx context.Context, prj *project.Project, snapshotID string) (ClasspathSnapshotConsumersResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ClasspathSnapshotConsumersResult{}, err
	}
	consumers, ok := view.ClasspathSnapshotConsumers(snapshotID)
	if !ok {
		return ClasspathSnapshotConsumersResult{}, os.ErrNotExist
	}
	return ClasspathSnapshotConsumersResult{
		Repo:                prj.RootDir,
		ClasspathSnapshotID: snapshotID,
		ModelCacheKey:       view.CacheKey(),
		Consumers:           consumers,
	}, nil
}

func (s *Service) ClasspathEntryLookup(ctx context.Context, prj *project.Project, modulePath, variantName, path string) (ClasspathEntryLookupResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ClasspathEntryLookupResult{}, err
	}
	lookup, ok := view.ClasspathEntryLookup(modulePath, variantName, path)
	if !ok {
		return ClasspathEntryLookupResult{}, os.ErrNotExist
	}
	return ClasspathEntryLookupResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Lookup:        lookup,
	}, nil
}

func (s *Service) ClasspathPathConsumers(ctx context.Context, prj *project.Project, path string) (ClasspathPathConsumersResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ClasspathPathConsumersResult{}, err
	}
	consumers, ok := view.ClasspathPathConsumers(path)
	if !ok {
		return ClasspathPathConsumersResult{}, os.ErrNotExist
	}
	return ClasspathPathConsumersResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Consumers:     consumers,
	}, nil
}

func (s *Service) ArtifactOnClasspath(ctx context.Context, prj *project.Project, modulePath, variantName, artifactID string) (ArtifactOnClasspathResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ArtifactOnClasspathResult{}, err
	}
	lookup, ok := view.ArtifactOnClasspath(modulePath, variantName, graph.ArtifactID(artifactID))
	if !ok {
		return ArtifactOnClasspathResult{}, os.ErrNotExist
	}
	return ArtifactOnClasspathResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Lookup:        lookup,
	}, nil
}

func (s *Service) ArtifactClasspathConsumers(ctx context.Context, prj *project.Project, artifactID string) (ArtifactClasspathConsumersResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ArtifactClasspathConsumersResult{}, err
	}
	consumers, ok := view.ArtifactClasspathConsumers(graph.ArtifactID(artifactID))
	if !ok {
		return ArtifactClasspathConsumersResult{}, os.ErrNotExist
	}
	return ArtifactClasspathConsumersResult{
		Repo:          prj.RootDir,
		ArtifactID:    artifactID,
		ModelCacheKey: view.CacheKey(),
		Consumers:     consumers,
	}, nil
}

func (s *Service) FileOwners(ctx context.Context, prj *project.Project, path string) (FileOwnersResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return FileOwnersResult{}, err
	}
	return FileOwnersResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Owners:        view.FileOwners(path),
	}, nil
}

func (s *Service) ActionInputs(ctx context.Context, prj *project.Project, actionID string) (ActionInputsResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ActionInputsResult{}, err
	}
	inputs, ok := view.ActionInputsResult(graph.ActionID(actionID))
	if !ok {
		return ActionInputsResult{}, os.ErrNotExist
	}
	return ActionInputsResult{
		Repo:          prj.RootDir,
		ActionID:      actionID,
		ModelCacheKey: view.CacheKey(),
		Inputs:        inputs,
	}, nil
}

func (s *Service) ActionOutputs(ctx context.Context, prj *project.Project, actionID string) (ActionOutputsResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ActionOutputsResult{}, err
	}
	outputs, ok := view.ActionOutputsResult(graph.ActionID(actionID))
	if !ok {
		return ActionOutputsResult{}, os.ErrNotExist
	}
	return ActionOutputsResult{
		Repo:          prj.RootDir,
		ActionID:      actionID,
		ModelCacheKey: view.CacheKey(),
		Outputs:       outputs,
	}, nil
}

func (s *Service) ActionDependencies(ctx context.Context, prj *project.Project, actionID string) (ActionDependenciesResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ActionDependenciesResult{}, err
	}
	dependencies, ok := view.ActionDependenciesResult(graph.ActionID(actionID))
	if !ok {
		return ActionDependenciesResult{}, os.ErrNotExist
	}
	return ActionDependenciesResult{
		Repo:          prj.RootDir,
		ActionID:      actionID,
		ModelCacheKey: view.CacheKey(),
		Dependencies:  dependencies,
	}, nil
}

func (s *Service) ActionDependents(ctx context.Context, prj *project.Project, actionID string) (ActionDependentsResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ActionDependentsResult{}, err
	}
	dependents, ok := view.ActionDependentsResult(graph.ActionID(actionID))
	if !ok {
		return ActionDependentsResult{}, os.ErrNotExist
	}
	return ActionDependentsResult{
		Repo:          prj.RootDir,
		ActionID:      actionID,
		ModelCacheKey: view.CacheKey(),
		Dependents:    dependents,
	}, nil
}

func (s *Service) ActionsForModule(ctx context.Context, prj *project.Project, modulePath string) (ActionsForModuleResult, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return ActionsForModuleResult{}, err
	}
	var actions []configmodel.ActionSummary
	for _, action := range model.ActionSummaries {
		if action.ModulePath != modulePath {
			continue
		}
		actions = append(actions, action)
	}
	if len(actions) == 0 {
		return ActionsForModuleResult{}, os.ErrNotExist
	}
	return ActionsForModuleResult{
		Repo:          prj.RootDir,
		Module:        modulePath,
		ModelCacheKey: model.CacheKey(),
		Actions:       append([]configmodel.ActionSummary(nil), actions...),
	}, nil
}

func (s *Service) ActionsForVariant(ctx context.Context, prj *project.Project, modulePath, variantName string) (ActionsForVariantResult, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return ActionsForVariantResult{}, err
	}
	var actions []configmodel.ActionSummary
	for _, action := range model.ActionSummaries {
		if action.ModulePath != modulePath || action.VariantName != variantName {
			continue
		}
		actions = append(actions, action)
	}
	if len(actions) == 0 {
		return ActionsForVariantResult{}, os.ErrNotExist
	}
	return ActionsForVariantResult{
		Repo:          prj.RootDir,
		Module:        modulePath,
		Variant:       variantName,
		ModelCacheKey: model.CacheKey(),
		Actions:       append([]configmodel.ActionSummary(nil), actions...),
	}, nil
}

func (s *Service) AndroidCapabilities(ctx context.Context, prj *project.Project, modulePath string) (AndroidCapabilityReportResult, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return AndroidCapabilityReportResult{}, err
	}
	mod, err := s.RequireModule(prj, modulePath)
	if err != nil {
		return AndroidCapabilityReportResult{}, err
	}
	variants, err := model.ResolvedVariants(mod.Path)
	if err != nil {
		return AndroidCapabilityReportResult{}, err
	}
	report := AndroidCapabilityReportResult{
		Repo:     prj.RootDir,
		Module:   mod.Path,
		Variants: make([]AndroidCapabilityVariantResult, 0, len(variants)),
	}
	for _, variant := range variants {
		provenance, _ := model.ProvenanceSummaryForVariant(mod.Path, variant.Name)
		_, signing := resolveSigningForVariant(mod, variant.Config)
		report.Variants = append(report.Variants, AndroidCapabilityVariantResult{
			Name:                      variant.Name,
			DisplayName:               variant.DisplayName,
			BuildType:                 variant.Coordinate.BuildType,
			Flavors:                   append([]string(nil), variant.Coordinate.Flavors...),
			CompileSDK:                variant.CompileSDK,
			BuildToolsVersion:         variant.BuildToolsVersion,
			Namespace:                 variant.Namespace,
			ApplicationID:             variant.ApplicationID,
			ApplicationIDSuffix:       variant.ApplicationIDSuffix,
			VersionCode:               variant.VersionCode,
			VersionName:               variant.VersionName,
			VersionNameSuffix:         variant.VersionNameSuffix,
			MinSDK:                    variant.MinSDK,
			TargetSDK:                 variant.TargetSDK,
			TestInstrumentationRunner: variant.TestInstrumentationRunner,
			Optimization:              variant.Optimization,
			ProguardFiles:             append([]string(nil), variant.ProguardFiles...),
			ConsumerProguardFiles:     append([]string(nil), variant.ConsumerProguardFiles...),
			ManifestPaths:             append([]string(nil), provenance.ManifestPaths...),
			MaterializationID:         provenance.MaterializationID,
			ArtifactSnapshotID:        provenance.ArtifactSnapshotID,
			ClasspathSnapshotIDs:      append([]string(nil), provenance.ClasspathSnapshotIDs...),
			SourceRoots:               append([]string(nil), provenance.SourceRoots...),
			BackingArtifactID:         variant.BackingArtifactID,
			BackingArtifactPath:       firstNonEmpty(variant.BackingArtifactPath, backingArtifactPath(model, variant.BackingArtifactID)),
			ProducedArtifactIDs:       append([]string(nil), variant.ProducedArtifactIDs...),
			ProducedArtifactPaths:     append([]string(nil), variant.ProducedArtifactPaths...),
			ProducedArtifacts:         append([]project.ResolvedVariantArtifact(nil), variant.ProducedArtifacts...),
			ProducedArtifactKinds:     producedArtifactKinds(variant.ProducedArtifacts),
			InstallArtifactID:         firstNonEmpty(variant.InstallArtifactID, firstProducedArtifactByKind(variant.ProducedArtifacts, "apk")),
			InstallArtifactPath:       firstNonEmpty(variant.InstallArtifactPath, firstProducedArtifactPathByKind(variant.ProducedArtifacts, "apk")),
			ResourceArtifactIDs:       producedArtifactIDsByKind(variant.ProducedArtifacts, "resources"),
			ResourceArtifactPaths:     append([]string(nil), firstNonEmptyStrings(variant.ResourceArtifactPaths, producedArtifactPathsByKind(variant.ProducedArtifacts, "resources"))...),
			ManifestArtifactIDs:       producedArtifactIDsByKind(variant.ProducedArtifacts, "manifest"),
			ManifestArtifactPaths:     append([]string(nil), firstNonEmptyStrings(variant.ManifestArtifactPaths, producedArtifactPathsByKind(variant.ProducedArtifacts, "manifest"))...),
			Installable:               variant.Installable,
			Testable:                  variant.Testable,
			Debuggable:                variant.Debuggable,
			SigningConfigured:         variant.SigningConfigured,
			SigningConfig:             variant.SigningConfig,
			SigningStoreFile:          signing.StoreFile,
			SigningKeyAlias:           signing.KeyAlias,
			HasStorePassword:          signing.StorePassword != "",
			HasKeyPassword:            signing.KeyPassword != "",
			DexMode:                   variant.DexMode,
			MinifyEnabled:             variant.MinifyEnabled,
			ShrinkResources:           variant.ShrinkResources,
			InstallTask:               variant.InstallTask,
			UninstallTask:             variant.UninstallTask,
			AndroidTestPackage:        androidTestPackageName(mod, variant.Name),
			AndroidTestManifest:       androidTestManifestOutputPath(prj.RootDir, mod.Path, variant.Name),
			AndroidTestInstallTask:    androidTestTaskAlias(variant.TaskAliases, "install"),
			AndroidTestUninstallTask:  androidTestTaskAlias(variant.TaskAliases, "uninstall"),
		})
	}
	return report, nil
}

func producedArtifactKinds(artifacts []project.ResolvedVariantArtifact) []string {
	if len(artifacts) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, artifact := range artifacts {
		kind := strings.TrimSpace(artifact.Kind)
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

func firstProducedArtifactByKind(artifacts []project.ResolvedVariantArtifact, kind string) string {
	kind = strings.TrimSpace(kind)
	for _, artifact := range artifacts {
		if artifact.Kind == kind && artifact.ID != "" {
			return artifact.ID
		}
	}
	return ""
}

func producedArtifactIDsByKind(artifacts []project.ResolvedVariantArtifact, kind string) []string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil
	}
	var out []string
	for _, artifact := range artifacts {
		if artifact.Kind == kind && artifact.ID != "" {
			out = append(out, artifact.ID)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func producedArtifactPathsByKind(artifacts []project.ResolvedVariantArtifact, kind string) []string {
	var out []string
	for _, artifact := range artifacts {
		if artifact.Kind != kind || strings.TrimSpace(artifact.Path) == "" {
			continue
		}
		out = append(out, artifact.Path)
	}
	return uniqueStringList(out)
}

func firstProducedArtifactPathByKind(artifacts []project.ResolvedVariantArtifact, kind string) string {
	for _, artifact := range artifacts {
		if artifact.Kind == kind && strings.TrimSpace(artifact.Path) != "" {
			return artifact.Path
		}
	}
	return ""
}

func batchIndexForAction(schedule PlanScheduleResult, actionID string) int {
	for idx, batch := range schedule.Batches {
		for _, action := range batch.Actions {
			if action.ID == actionID {
				return idx
			}
		}
	}
	return 0
}

func plannedPoliciesFromSchedule(schedule PlanScheduleResult) []PlannedActionPolicy {
	var out []PlannedActionPolicy
	for idx, batch := range schedule.Batches {
		for _, action := range batch.Actions {
			out = append(out, toPlannedActionPolicy(action, idx))
		}
	}
	return out
}

func scheduleDriftSummaryFromRunSummary(summary RunSummaryRecord) *ScheduleDriftSummary {
	if summary.PlannedSchedule == nil && len(summary.ActionExecutions) == 0 && summary.RunGraphSummary == nil && summary.SchedulerSummary == nil && summary.CriticalPathSummary == nil {
		return nil
	}
	rows := map[string]*ScheduleDriftAction{}
	addRow := func(actionID string) *ScheduleDriftAction {
		actionID = strings.TrimSpace(actionID)
		if actionID == "" {
			return nil
		}
		if existing, ok := rows[actionID]; ok {
			return existing
		}
		entry := &ScheduleDriftAction{
			ActionID:           actionID,
			PlannedBatchIndex:  -1,
			ExecutedBatchIndex: -1,
		}
		rows[actionID] = entry
		return entry
	}

	if summary.RunGraphSummary != nil {
		for _, actionID := range summary.RunGraphSummary.PlannedActionIDs {
			if row := addRow(actionID); row != nil {
				row.Planned = true
			}
		}
		for _, actionID := range summary.RunGraphSummary.ExecutedActionIDs {
			if row := addRow(actionID); row != nil {
				row.Executed = true
			}
		}
	}
	if summary.PlannedSchedule != nil {
		for batchIdx, batch := range summary.PlannedSchedule.Batches {
			for _, action := range batch.Actions {
				row := addRow(action.ID)
				if row == nil {
					continue
				}
				row.Planned = true
				row.Name = firstNonEmpty(row.Name, action.Name)
				row.Operation = firstNonEmpty(row.Operation, action.Operation)
				row.ModulePath = firstNonEmpty(row.ModulePath, action.ModulePath)
				row.VariantName = firstNonEmpty(row.VariantName, action.VariantName)
				row.PlannedBatchIndex = batchIdx
			}
		}
	}
	for _, execution := range summary.ActionExecutions {
		row := addRow(execution.ActionID)
		if row == nil {
			continue
		}
		row.Executed = true
		row.Name = firstNonEmpty(row.Name, execution.Name)
		row.Operation = firstNonEmpty(row.Operation, execution.Operation)
		row.ModulePath = firstNonEmpty(row.ModulePath, execution.ModulePath)
		row.VariantName = firstNonEmpty(row.VariantName, execution.VariantName)
		row.ExecutedBatchIndex = execution.BatchIndex
		row.CriticalPath = execution.CriticalPath
		row.QueueWaitMs = execution.QueueWaitMs
		row.WaitReason = execution.WaitReason
		row.Status = execution.Status
	}

	order := make([]string, 0, len(rows))
	for actionID := range rows {
		order = append(order, actionID)
	}
	sort.Slice(order, func(i, j int) bool {
		left := rows[order[i]]
		right := rows[order[j]]
		leftBatch := left.PlannedBatchIndex
		if leftBatch < 0 {
			leftBatch = left.ExecutedBatchIndex
		}
		rightBatch := right.PlannedBatchIndex
		if rightBatch < 0 {
			rightBatch = right.ExecutedBatchIndex
		}
		if leftBatch != rightBatch {
			if leftBatch < 0 {
				return false
			}
			if rightBatch < 0 {
				return true
			}
			return leftBatch < rightBatch
		}
		return left.ActionID < right.ActionID
	})

	out := &ScheduleDriftSummary{
		PlannedBatchCount:       plannedBatchCount(summary.PlannedSchedule),
		QueueWaitReasonCounts:   map[string]int{},
		RepresentativeActionIDs: driftRepresentativeActionIDs(summary.CriticalPathSummary),
		RootActionIDs:           driftRootActionIDs(summary.RunGraphSummary),
	}
	if summary.CriticalPathSummary != nil {
		out.EstimatedCriticalPathMs = summary.CriticalPathSummary.EstimatedDurationMs
	}
	if summary.SchedulerSummary != nil {
		out.ExecutedBatchCount = summary.SchedulerSummary.ExecutedBatchCount
		out.QueueWaitActions = summary.SchedulerSummary.QueueWaitActions
		out.CriticalPathActions = summary.SchedulerSummary.CriticalPathActions
		out.MaxQueueWaitMs = summary.SchedulerSummary.MaxQueueWaitMs
		for key, count := range summary.SchedulerSummary.WaitReasonCounts {
			if strings.TrimSpace(key) == "" || count == 0 {
				continue
			}
			out.QueueWaitReasonCounts[key] = count
		}
	}

	for _, actionID := range order {
		row := *rows[actionID]
		if row.Planned {
			out.PlannedActionCount++
		}
		if row.Executed {
			out.ExecutedActionCount++
		}
		if row.Planned && row.Executed {
			out.MatchedActionCount++
			if row.PlannedBatchIndex >= 0 && row.ExecutedBatchIndex >= 0 && row.PlannedBatchIndex != row.ExecutedBatchIndex {
				row.BatchMismatch = true
				out.BatchMismatchCount++
				out.BatchMismatchActionIDs = append(out.BatchMismatchActionIDs, row.ActionID)
			}
		}
		if row.Planned && !row.Executed {
			out.PlannedOnlyCount++
			out.PlannedOnlyActionIDs = append(out.PlannedOnlyActionIDs, row.ActionID)
		}
		if row.Executed && !row.Planned {
			out.ExecutedOnlyCount++
			out.ExecutedOnlyActionIDs = append(out.ExecutedOnlyActionIDs, row.ActionID)
		}
		if row.QueueWaitMs > out.MaxQueueWaitMs {
			out.MaxQueueWaitMs = row.QueueWaitMs
		}
		if row.QueueWaitMs > 0 && row.WaitReason != "" {
			out.QueueWaitReasonCounts[row.WaitReason]++
		}
		out.Actions = append(out.Actions, row)
	}
	if len(out.QueueWaitReasonCounts) == 0 {
		out.QueueWaitReasonCounts = nil
	}
	return out
}

func plannedBatchCount(schedule *PlanScheduleResult) int {
	if schedule == nil {
		return 0
	}
	return len(schedule.Batches)
}

func driftRepresentativeActionIDs(summary *CriticalPathSummary) []string {
	if summary == nil {
		return nil
	}
	return uniqueStringList(summary.RepresentativeAction)
}

func driftRootActionIDs(summary *RunGraphSummary) []string {
	if summary == nil {
		return nil
	}
	return uniqueStringList(summary.RootActionIDs)
}

func backingArtifactPath(model *configmodel.Model, artifactID string) string {
	if model == nil || strings.TrimSpace(artifactID) == "" {
		return ""
	}
	artifact, ok := model.ArtifactSummary(graph.ArtifactID(artifactID))
	if !ok {
		return ""
	}
	return artifact.Path
}

func uniqueStringList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func (s *Service) VariantMaterialization(ctx context.Context, prj *project.Project, modulePath, variantName string) (VariantMaterializationResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return VariantMaterializationResult{}, err
	}
	provenance, ok := view.VariantMaterialization(modulePath, variantName)
	if !ok {
		return VariantMaterializationResult{}, os.ErrNotExist
	}
	return VariantMaterializationResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Provenance:    provenance,
	}, nil
}

func (s *Service) VariantSourceSetModel(ctx context.Context, prj *project.Project, modulePath, variantName string) (VariantSourceSetModelResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return VariantSourceSetModelResult{}, err
	}
	sourceSetModel, ok := view.VariantSourceSetModel(modulePath, variantName)
	if !ok {
		return VariantSourceSetModelResult{}, os.ErrNotExist
	}
	return VariantSourceSetModelResult{
		Repo:           prj.RootDir,
		ModelCacheKey:  view.CacheKey(),
		SourceSetModel: sourceSetModel,
	}, nil
}

func (s *Service) DependencyBindingsForVariant(ctx context.Context, prj *project.Project, modulePath, variantName string) (DependencyBindingsForVariantResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return DependencyBindingsForVariantResult{}, err
	}
	bindings, ok := view.DependencyBindingsForVariant(modulePath, variantName)
	if !ok {
		return DependencyBindingsForVariantResult{}, os.ErrNotExist
	}
	return DependencyBindingsForVariantResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Bindings:      bindings,
	}, nil
}

func (s *Service) DependencyBindingsForModule(ctx context.Context, prj *project.Project, modulePath string) (DependencyBindingsForModuleResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return DependencyBindingsForModuleResult{}, err
	}
	bindings, ok := view.DependencyBindingsForModule(modulePath)
	if !ok {
		return DependencyBindingsForModuleResult{}, os.ErrNotExist
	}
	return DependencyBindingsForModuleResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Bindings:      bindings,
	}, nil
}

func (s *Service) DependencyRealizationsForVariant(ctx context.Context, prj *project.Project, modulePath, variantName string) (DependencyRealizationsForVariantResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return DependencyRealizationsForVariantResult{}, err
	}
	realizations, ok := view.DependencyRealizationsForVariant(modulePath, variantName)
	if !ok {
		return DependencyRealizationsForVariantResult{}, os.ErrNotExist
	}
	return DependencyRealizationsForVariantResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Realizations:  realizations,
	}, nil
}

func (s *Service) DependencyRealizationsForModule(ctx context.Context, prj *project.Project, modulePath string) (DependencyRealizationsForModuleResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return DependencyRealizationsForModuleResult{}, err
	}
	realizations, ok := view.DependencyRealizationsForModule(modulePath)
	if !ok {
		return DependencyRealizationsForModuleResult{}, os.ErrNotExist
	}
	return DependencyRealizationsForModuleResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Realizations:  realizations,
	}, nil
}

func (s *Service) PlannedActionPolicy(ctx context.Context, prj *project.Project, mod *project.Module, command, requestedVariant, actionID string, variantExplicit bool) (PlannedActionPolicyResult, error) {
	plan, err := s.ExplainPlan(ctx, prj, mod, command, requestedVariant, variantExplicit)
	if err != nil {
		return PlannedActionPolicyResult{}, err
	}
	for _, action := range plan.Actions {
		if action.ID != actionID {
			continue
		}
		return PlannedActionPolicyResult{
			Repo:             prj.RootDir,
			Module:           mod.Path,
			Command:          command,
			RequestedVariant: requestedVariant,
			TargetVariant:    plan.TargetVariant,
			VariantExplicit:  variantExplicit,
			ActionID:         actionID,
			ModelCacheKey:    plan.ModelCacheKey,
			Policy:           toPlannedActionPolicy(action, batchIndexForAction(plan.Schedule, action.ID)),
		}, nil
	}
	return PlannedActionPolicyResult{}, os.ErrNotExist
}

func (s *Service) PlannedActionPolicies(ctx context.Context, prj *project.Project, mod *project.Module, command, requestedVariant string, variantExplicit bool) (PlannedActionPoliciesResult, error) {
	plan, err := s.ExplainPlan(ctx, prj, mod, command, requestedVariant, variantExplicit)
	if err != nil {
		return PlannedActionPoliciesResult{}, err
	}
	return PlannedActionPoliciesResult{
		Repo:             prj.RootDir,
		Module:           mod.Path,
		Command:          command,
		RequestedVariant: requestedVariant,
		TargetVariant:    plan.TargetVariant,
		VariantExplicit:  variantExplicit,
		ModelCacheKey:    plan.ModelCacheKey,
		Policies:         plannedPoliciesFromSchedule(plan.Schedule),
	}, nil
}

func (s *Service) ModuleByID(ctx context.Context, prj *project.Project, moduleID string) (ModuleByIDResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ModuleByIDResult{}, err
	}
	result, ok := view.ModuleByID(graph.LogicalModuleID(moduleID))
	if !ok {
		return ModuleByIDResult{}, os.ErrNotExist
	}
	return ModuleByIDResult{
		Repo:          prj.RootDir,
		ModuleID:      moduleID,
		ModelCacheKey: view.CacheKey(),
		Result:        result,
	}, nil
}

func (s *Service) VariantByID(ctx context.Context, prj *project.Project, variantID string) (VariantByIDResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return VariantByIDResult{}, err
	}
	result, ok := view.VariantByID(graph.VariantID(variantID))
	if !ok {
		return VariantByIDResult{}, os.ErrNotExist
	}
	return VariantByIDResult{
		Repo:          prj.RootDir,
		VariantID:     variantID,
		ModelCacheKey: view.CacheKey(),
		Result:        result,
	}, nil
}

func (s *Service) ActionByID(ctx context.Context, prj *project.Project, actionID string) (ActionByIDResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ActionByIDResult{}, err
	}
	result, ok := view.ActionByID(graph.ActionID(actionID))
	if !ok {
		return ActionByIDResult{}, os.ErrNotExist
	}
	return ActionByIDResult{
		Repo:          prj.RootDir,
		ActionID:      actionID,
		ModelCacheKey: view.CacheKey(),
		Result:        result,
	}, nil
}

func (s *Service) ArtifactByID(ctx context.Context, prj *project.Project, artifactID string) (ArtifactByIDResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ArtifactByIDResult{}, err
	}
	result, ok := view.ArtifactByID(graph.ArtifactID(artifactID))
	if !ok {
		return ArtifactByIDResult{}, os.ErrNotExist
	}
	return ArtifactByIDResult{
		Repo:          prj.RootDir,
		ArtifactID:    artifactID,
		ModelCacheKey: view.CacheKey(),
		Result:        result,
	}, nil
}

func (s *Service) MaterializationByID(ctx context.Context, prj *project.Project, materializationID string) (MaterializationByIDResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return MaterializationByIDResult{}, err
	}
	result, ok := view.MaterializationByID(graph.MaterializationID(materializationID))
	if !ok {
		return MaterializationByIDResult{}, os.ErrNotExist
	}
	return MaterializationByIDResult{
		Repo:              prj.RootDir,
		MaterializationID: materializationID,
		ModelCacheKey:     view.CacheKey(),
		Result:            result,
	}, nil
}

func (s *Service) MaterializationConsumers(ctx context.Context, prj *project.Project, materializationID string) (MaterializationConsumersResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return MaterializationConsumersResult{}, err
	}
	consumers, ok := view.MaterializationConsumers(graph.MaterializationID(materializationID))
	if !ok {
		return MaterializationConsumersResult{}, os.ErrNotExist
	}
	return MaterializationConsumersResult{
		Repo:              prj.RootDir,
		MaterializationID: materializationID,
		ModelCacheKey:     view.CacheKey(),
		Consumers:         consumers,
	}, nil
}

func (s *Service) ClasspathSnapshotByID(ctx context.Context, prj *project.Project, snapshotID string) (ClasspathSnapshotByIDResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ClasspathSnapshotByIDResult{}, err
	}
	result, ok := view.ClasspathSnapshotByID(snapshotID)
	if !ok {
		return ClasspathSnapshotByIDResult{}, os.ErrNotExist
	}
	return ClasspathSnapshotByIDResult{
		Repo:                prj.RootDir,
		ClasspathSnapshotID: snapshotID,
		ModelCacheKey:       view.CacheKey(),
		Result:              result,
	}, nil
}

func (s *Service) ClasspathSnapshotConsumersByID(ctx context.Context, prj *project.Project, snapshotID string) (ClasspathSnapshotConsumersByIDResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ClasspathSnapshotConsumersByIDResult{}, err
	}
	consumers, ok := view.ClasspathSnapshotConsumersByID(snapshotID)
	if !ok {
		return ClasspathSnapshotConsumersByIDResult{}, os.ErrNotExist
	}
	return ClasspathSnapshotConsumersByIDResult{
		Repo:                prj.RootDir,
		ClasspathSnapshotID: snapshotID,
		ModelCacheKey:       view.CacheKey(),
		Consumers:           consumers,
	}, nil
}

func (s *Service) MaterializationProvenance(ctx context.Context, prj *project.Project, materializationID string) (MaterializationProvenanceResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return MaterializationProvenanceResult{}, err
	}
	provenance, ok := view.MaterializationProvenance(graph.MaterializationID(materializationID))
	if !ok {
		return MaterializationProvenanceResult{}, os.ErrNotExist
	}
	return MaterializationProvenanceResult{
		Repo:              prj.RootDir,
		MaterializationID: materializationID,
		ModelCacheKey:     view.CacheKey(),
		Provenance:        provenance,
	}, nil
}

func (s *Service) VariantCompatibility(ctx context.Context, prj *project.Project, modulePath, variantName string) (VariantCompatibilityResult, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return VariantCompatibilityResult{}, err
	}
	variants, err := model.ResolvedVariants(modulePath)
	if err != nil {
		return VariantCompatibilityResult{}, err
	}
	var variant project.ResolvedVariant
	found := false
	for _, candidate := range variants {
		if candidate.Name != variantName {
			continue
		}
		variant = candidate
		found = true
		break
	}
	if !found {
		return VariantCompatibilityResult{}, os.ErrNotExist
	}
	provenance, _ := model.ProvenanceSummaryForVariant(modulePath, variantName)
	semanticVariant, ok := model.Variant(modulePath, variantName)
	if !ok {
		return VariantCompatibilityResult{}, os.ErrNotExist
	}
	mod, err := s.RequireModule(prj, modulePath)
	if err != nil {
		return VariantCompatibilityResult{}, err
	}
	_, signing := resolveSigningForVariant(mod, variant.Config)
	return VariantCompatibilityResult{
		Repo:                      prj.RootDir,
		ModelCacheKey:             model.CacheKey(),
		ModulePath:                modulePath,
		VariantName:               variantName,
		DeclaredName:              variant.DeclaredName,
		CoordinateName:            variant.CoordinateName,
		VariantID:                 semanticVariant.ID,
		DisplayName:               variant.DisplayName,
		BuildType:                 variant.Coordinate.BuildType,
		Flavors:                   append([]string(nil), variant.Coordinate.Flavors...),
		CompileSDK:                variant.CompileSDK,
		BuildToolsVersion:         variant.BuildToolsVersion,
		Namespace:                 variant.Namespace,
		ApplicationID:             variant.ApplicationID,
		ApplicationIDSuffix:       variant.ApplicationIDSuffix,
		VersionCode:               variant.VersionCode,
		VersionName:               variant.VersionName,
		VersionNameSuffix:         variant.VersionNameSuffix,
		MinSDK:                    variant.MinSDK,
		TargetSDK:                 variant.TargetSDK,
		TestInstrumentationRunner: variant.TestInstrumentationRunner,
		Optimization:              variant.Optimization,
		ProguardFiles:             append([]string(nil), variant.ProguardFiles...),
		ConsumerProguardFiles:     append([]string(nil), variant.ConsumerProguardFiles...),
		Installable:               variant.Installable,
		Testable:                  variant.Testable,
		Debuggable:                variant.Debuggable,
		SigningConfigured:         variant.SigningConfigured,
		SigningConfig:             variant.SigningConfig,
		DexMode:                   variant.DexMode,
		MinifyEnabled:             variant.MinifyEnabled,
		ShrinkResources:           variant.ShrinkResources,
		MaterializationID:         provenance.MaterializationID,
		ArtifactSnapshotID:        provenance.ArtifactSnapshotID,
		SourceRoots:               append([]string(nil), provenance.SourceRoots...),
		ManifestPaths:             append([]string(nil), provenance.ManifestPaths...),
		ClasspathSnapshotIDs:      append([]string(nil), provenance.ClasspathSnapshotIDs...),
		ProducedArtifactPaths:     append([]string(nil), variant.ProducedArtifactPaths...),
		ProducedArtifactKinds:     append([]string(nil), variant.ProducedArtifactKinds...),
		InstallArtifactID:         variant.InstallArtifactID,
		InstallArtifactPath:       firstNonEmpty(variant.InstallArtifactPath, firstProducedArtifactPathByKind(variant.ProducedArtifacts, "apk")),
		ResourceArtifactIDs:       append([]string(nil), variant.ResourceArtifactIDs...),
		ResourceArtifactPaths:     append([]string(nil), firstNonEmptyStrings(variant.ResourceArtifactPaths, producedArtifactPathsByKind(variant.ProducedArtifacts, "resources"))...),
		ManifestArtifactIDs:       append([]string(nil), variant.ManifestArtifactIDs...),
		ManifestArtifactPaths:     append([]string(nil), firstNonEmptyStrings(variant.ManifestArtifactPaths, producedArtifactPathsByKind(variant.ProducedArtifacts, "manifest"))...),
		BackingArtifactPath:       firstNonEmpty(variant.BackingArtifactPath, backingArtifactPath(model, variant.BackingArtifactID)),
		SigningStoreFile:          signing.StoreFile,
		SigningKeyAlias:           signing.KeyAlias,
		HasStorePassword:          signing.StorePassword != "",
		HasKeyPassword:            signing.KeyPassword != "",
		InstallTask:               variant.InstallTask,
		UninstallTask:             variant.UninstallTask,
		Compatibility:             variant.Compatibility,
		Materialization:           semanticVariant.Materialization,
		Provenance:                provenance,
	}, nil
}

func (s *Service) ArtifactsForVariant(ctx context.Context, prj *project.Project, modulePath, variantName string) (ArtifactsForVariantResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ArtifactsForVariantResult{}, err
	}
	materialization, ok := view.VariantMaterialization(modulePath, variantName)
	if !ok {
		return ArtifactsForVariantResult{}, os.ErrNotExist
	}
	return ArtifactsForVariantResult{
		Repo:               prj.RootDir,
		Module:             modulePath,
		Variant:            variantName,
		ModelCacheKey:      view.CacheKey(),
		MaterializationID:  materialization.Materialization.MaterializationID,
		ArtifactSnapshotID: materialization.Materialization.ArtifactSnapshotID,
		Artifacts:          append([]configmodel.ArtifactSummary(nil), view.ArtifactSummariesForVariant(modulePath, variantName)...),
	}, nil
}

func (s *Service) ArtifactsForModule(ctx context.Context, prj *project.Project, modulePath string) (ArtifactsForModuleResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ArtifactsForModuleResult{}, err
	}
	manifest, ok := view.ModuleManifest(modulePath)
	if !ok {
		return ArtifactsForModuleResult{}, os.ErrNotExist
	}
	return ArtifactsForModuleResult{
		Repo:                prj.RootDir,
		Module:              modulePath,
		ModelCacheKey:       view.CacheKey(),
		VariantNames:        append([]string(nil), manifest.VariantNames...),
		MaterializationIDs:  append([]string(nil), manifest.MaterializationIDs...),
		ArtifactSnapshotIDs: append([]string(nil), manifest.ArtifactSnapshotIDs...),
		Artifacts:           append([]configmodel.ArtifactSummary(nil), view.ArtifactSummariesForModule(modulePath)...),
	}, nil
}

func (s *Service) ModuleManifest(ctx context.Context, prj *project.Project, modulePath string) (ModuleManifestResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ModuleManifestResult{}, err
	}
	manifest, ok := view.ModuleManifest(modulePath)
	if !ok {
		return ModuleManifestResult{}, os.ErrNotExist
	}
	return ModuleManifestResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Manifest:      manifest,
	}, nil
}

func (s *Service) VariantManifest(ctx context.Context, prj *project.Project, modulePath, variantName string) (VariantManifestResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return VariantManifestResult{}, err
	}
	manifest, ok := view.VariantManifest(modulePath, variantName)
	if !ok {
		return VariantManifestResult{}, os.ErrNotExist
	}
	return VariantManifestResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Manifest:      manifest,
	}, nil
}

func (s *Service) ArtifactSnapshotProvenance(ctx context.Context, prj *project.Project, snapshotID string) (ArtifactSnapshotProvenanceResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ArtifactSnapshotProvenanceResult{}, err
	}
	provenance, ok := view.ArtifactSnapshotProvenance(snapshotID)
	if !ok {
		return ArtifactSnapshotProvenanceResult{}, os.ErrNotExist
	}
	return ArtifactSnapshotProvenanceResult{
		Repo:          prj.RootDir,
		ModelCacheKey: view.CacheKey(),
		Provenance:    provenance,
	}, nil
}

func (s *Service) ArtifactSnapshotConsumers(ctx context.Context, prj *project.Project, snapshotID string) (ArtifactSnapshotConsumersResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ArtifactSnapshotConsumersResult{}, err
	}
	consumers, ok := view.ArtifactSnapshotConsumers(snapshotID)
	if !ok {
		return ArtifactSnapshotConsumersResult{}, os.ErrNotExist
	}
	return ArtifactSnapshotConsumersResult{
		Repo:               prj.RootDir,
		ArtifactSnapshotID: snapshotID,
		ModelCacheKey:      view.CacheKey(),
		Consumers:          consumers,
	}, nil
}

func (s *Service) ArtifactConsumers(ctx context.Context, prj *project.Project, artifactID string) (ArtifactConsumersResult, error) {
	view, err := s.IntegrationView(ctx, prj)
	if err != nil {
		return ArtifactConsumersResult{}, err
	}
	consumers, ok := view.ArtifactConsumers(graph.ArtifactID(artifactID))
	if !ok {
		return ArtifactConsumersResult{}, os.ErrNotExist
	}
	return ArtifactConsumersResult{
		Repo:          prj.RootDir,
		ArtifactID:    artifactID,
		ModelCacheKey: view.CacheKey(),
		Consumers:     consumers,
	}, nil
}

func (s *Service) VariantImpact(ctx context.Context, prj *project.Project, modulePath, variantName string) (VariantImpactResult, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return VariantImpactResult{}, err
	}
	g, err := model.Graph()
	if err != nil {
		return VariantImpactResult{}, err
	}
	variant, ok := model.Variant(modulePath, variantName)
	if !ok {
		return VariantImpactResult{}, os.ErrNotExist
	}
	start := graph.NodeRef{Kind: graph.NodeKindVariant, ID: variant.ID}
	seen := map[graph.NodeRef]struct{}{start: {}}
	queue := []graph.NodeRef{start}
	var dependents []ImpactNode
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dep := range g.DependentsOf(current) {
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			queue = append(queue, dep)
			if node, ok := impactNodeForRef(g, dep); ok {
				dependents = append(dependents, node)
			}
		}
	}
	return VariantImpactResult{
		Repo:          prj.RootDir,
		ModelCacheKey: model.CacheKey(),
		Module:        modulePath,
		Variant:       variantName,
		Dependents:    dependents,
	}, nil
}

func (s *Service) ModuleImpact(ctx context.Context, prj *project.Project, modulePath string) (ModuleImpactResult, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return ModuleImpactResult{}, err
	}
	g, err := model.Graph()
	if err != nil {
		return ModuleImpactResult{}, err
	}
	mod, ok := model.Module(modulePath)
	if !ok {
		return ModuleImpactResult{}, os.ErrNotExist
	}
	start := graph.NodeRef{Kind: graph.NodeKindLogicalModule, ID: mod.ID}
	seen := map[graph.NodeRef]struct{}{start: {}}
	queue := []graph.NodeRef{start}
	var dependents []ImpactNode
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dep := range g.DependentsOf(current) {
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			queue = append(queue, dep)
			if node, ok := impactNodeForRef(g, dep); ok {
				dependents = append(dependents, node)
			}
		}
	}
	return ModuleImpactResult{
		Repo:          prj.RootDir,
		ModelCacheKey: model.CacheKey(),
		Module:        modulePath,
		Dependents:    dependents,
	}, nil
}

func (s *Service) CleanupPlan(ctx context.Context, prj *project.Project) (CleanupPlanResult, error) {
	model, err := s.LoadConfigurationModel(ctx, prj)
	if err != nil {
		return CleanupPlanResult{}, err
	}
	plan := model.DryRunCleanupPlan()
	return CleanupPlanResult{
		Repo:          prj.RootDir,
		ModelCacheKey: model.CacheKey(),
		Plan:          plan,
	}, nil
}

func (s *Service) RunSummary(_ context.Context, prj *project.Project, modulePath, command string) (RunSummaryResult, error) {
	if prj == nil {
		return RunSummaryResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return RunSummaryResult{}, err
	}
	return RunSummaryResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: summary,
	}, nil
}

func (s *Service) RunSummaries(_ context.Context, prj *project.Project, modulePath string) (RunSummariesResult, error) {
	if prj == nil {
		return RunSummariesResult{}, os.ErrInvalid
	}
	entries, err := listRunSummaryEntries(prj.RootDir, modulePath)
	if err != nil {
		return RunSummariesResult{}, err
	}
	return RunSummariesResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Entries: entries,
	}, nil
}

func (s *Service) RunGraphSummary(_ context.Context, prj *project.Project, modulePath, command string) (RunGraphSummaryResult, error) {
	if prj == nil {
		return RunGraphSummaryResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return RunGraphSummaryResult{}, err
	}
	if summary.RunGraphSummary == nil {
		return RunGraphSummaryResult{}, os.ErrNotExist
	}
	return RunGraphSummaryResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: *cloneRunGraphSummary(summary.RunGraphSummary),
	}, nil
}

func (s *Service) PlannedSchedule(_ context.Context, prj *project.Project, modulePath, command string) (PlannedScheduleResult, error) {
	if prj == nil {
		return PlannedScheduleResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return PlannedScheduleResult{}, err
	}
	if summary.PlannedSchedule == nil {
		return PlannedScheduleResult{}, os.ErrNotExist
	}
	return PlannedScheduleResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: *clonePlanScheduleResult(summary.PlannedSchedule),
	}, nil
}

func (s *Service) ScheduleDrift(_ context.Context, prj *project.Project, modulePath, command string) (ScheduleDriftResult, error) {
	if prj == nil {
		return ScheduleDriftResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ScheduleDriftResult{}, err
	}
	drift := scheduleDriftSummaryFromRunSummary(summary)
	if drift == nil {
		return ScheduleDriftResult{}, os.ErrNotExist
	}
	return ScheduleDriftResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: *drift,
	}, nil
}

func (s *Service) CriticalPathSummary(_ context.Context, prj *project.Project, modulePath, command string) (CriticalPathSummaryResult, error) {
	if prj == nil {
		return CriticalPathSummaryResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return CriticalPathSummaryResult{}, err
	}
	if summary.CriticalPathSummary == nil {
		return CriticalPathSummaryResult{}, os.ErrNotExist
	}
	return CriticalPathSummaryResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: *cloneCriticalPathSummary(summary.CriticalPathSummary),
	}, nil
}

func (s *Service) SchedulerSummary(_ context.Context, prj *project.Project, modulePath, command string) (SchedulerSummaryResult, error) {
	if prj == nil {
		return SchedulerSummaryResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return SchedulerSummaryResult{}, err
	}
	if summary.SchedulerSummary == nil {
		return SchedulerSummaryResult{}, os.ErrNotExist
	}
	return SchedulerSummaryResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: *cloneSchedulerSummary(summary.SchedulerSummary),
	}, nil
}

func (s *Service) CacheSummary(_ context.Context, prj *project.Project, modulePath, command string) (CacheSummaryResult, error) {
	if prj == nil {
		return CacheSummaryResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return CacheSummaryResult{}, err
	}
	if summary.CacheSummary == nil {
		return CacheSummaryResult{}, os.ErrNotExist
	}
	return CacheSummaryResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: *cloneCacheSummary(summary.CacheSummary),
	}, nil
}

func (s *Service) ToolSummary(_ context.Context, prj *project.Project, modulePath, command string) (ToolSummaryResult, error) {
	if prj == nil {
		return ToolSummaryResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ToolSummaryResult{}, err
	}
	toolSummary := cloneToolSummary(summary.ToolSummary)
	if toolSummary == nil {
		toolSummary = summarizeTooling(summary.ActionExecutions, summary.ActionExplanations)
	}
	if toolSummary == nil {
		return ToolSummaryResult{}, os.ErrNotExist
	}
	return ToolSummaryResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: *toolSummary,
	}, nil
}

func (s *Service) Diagnostics(_ context.Context, prj *project.Project, modulePath, command string) (DiagnosticsResult, error) {
	if prj == nil {
		return DiagnosticsResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return DiagnosticsResult{}, err
	}
	diagnostics := append([]DiagnosticRecord(nil), summary.Diagnostics...)
	if len(diagnostics) == 0 {
		diagnostics = collectDiagnostics(summary.ActionExecutions, errString(summary.Error))
	}
	diagnostics = normalizeDiagnostics(diagnostics)
	if len(diagnostics) == 0 {
		return DiagnosticsResult{}, os.ErrNotExist
	}
	return DiagnosticsResult{
		Repo:        prj.RootDir,
		Module:      modulePath,
		Command:     command,
		Path:        path,
		Diagnostics: diagnostics,
	}, nil
}

func (s *Service) DiagnosticSummary(_ context.Context, prj *project.Project, modulePath, command string) (DiagnosticSummaryResult, error) {
	if prj == nil {
		return DiagnosticSummaryResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return DiagnosticSummaryResult{}, err
	}
	diagnostics := normalizeDiagnostics(append([]DiagnosticRecord(nil), summary.Diagnostics...))
	diagnosticSummary := cloneDiagnosticSummary(summary.DiagnosticSummary)
	if len(diagnostics) != 0 {
		diagnosticSummary = summarizeNormalizedDiagnostics(diagnostics)
	} else if len(summary.ActionExecutions) != 0 || strings.TrimSpace(summary.Error) != "" {
		diagnosticSummary = summarizeDiagnostics(summary.ActionExecutions, errString(summary.Error))
	}
	if diagnosticSummary == nil {
		return DiagnosticSummaryResult{}, os.ErrNotExist
	}
	return DiagnosticSummaryResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Summary: *diagnosticSummary,
	}, nil
}

func (s *Service) Materializations(_ context.Context, prj *project.Project, modulePath, command string) (MaterializationsResult, error) {
	if prj == nil {
		return MaterializationsResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return MaterializationsResult{}, err
	}
	if len(summary.Materializations) == 0 {
		return MaterializationsResult{}, os.ErrNotExist
	}
	return MaterializationsResult{
		Repo:             prj.RootDir,
		Module:           modulePath,
		Command:          command,
		Path:             path,
		Materializations: append([]project.SemanticMaterializationSummary(nil), summary.Materializations...),
	}, nil
}

func (s *Service) ActionExecution(_ context.Context, prj *project.Project, modulePath, command, actionID string) (ActionExecutionResult, error) {
	if prj == nil {
		return ActionExecutionResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ActionExecutionResult{}, err
	}
	for _, execution := range summary.ActionExecutions {
		if execution.ActionID != actionID {
			continue
		}
		result := ActionExecutionResult{
			Repo:      prj.RootDir,
			Module:    modulePath,
			Command:   command,
			Path:      path,
			ActionID:  actionID,
			Execution: execution,
		}
		if explanation, ok := actionExplanationForID(summary.ActionExplanations, actionID); ok {
			cloned := explanation
			result.Explain = &cloned
		}
		return result, nil
	}
	return ActionExecutionResult{}, os.ErrNotExist
}

func (s *Service) ActionExplanation(_ context.Context, prj *project.Project, modulePath, command, actionID string) (ActionExplanationResult, error) {
	if prj == nil {
		return ActionExplanationResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ActionExplanationResult{}, err
	}
	explanation, ok := actionExplanationForID(summary.ActionExplanations, actionID)
	if !ok {
		return ActionExplanationResult{}, os.ErrNotExist
	}
	result := ActionExplanationResult{
		Repo:     prj.RootDir,
		Module:   modulePath,
		Command:  command,
		Path:     path,
		ActionID: actionID,
		Explain:  explanation,
	}
	if execution, ok := actionExecutionForID(summary.ActionExecutions, actionID); ok {
		cloned := execution
		result.Execution = &cloned
	}
	return result, nil
}

func (s *Service) ActionExecutions(_ context.Context, prj *project.Project, modulePath, command string) (ActionExecutionsResult, error) {
	if prj == nil {
		return ActionExecutionsResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ActionExecutionsResult{}, err
	}
	return ActionExecutionsResult{
		Repo:       prj.RootDir,
		Module:     modulePath,
		Command:    command,
		Path:       path,
		Executions: append([]ActionExecution(nil), summary.ActionExecutions...),
	}, nil
}

func (s *Service) ActionExplanations(_ context.Context, prj *project.Project, modulePath, command string) (ActionExplanationsResult, error) {
	if prj == nil {
		return ActionExplanationsResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ActionExplanationsResult{}, err
	}
	return ActionExplanationsResult{
		Repo:         prj.RootDir,
		Module:       modulePath,
		Command:      command,
		Path:         path,
		Explanations: append([]explain.Action(nil), summary.ActionExplanations...),
	}, nil
}

func (s *Service) CacheProbes(_ context.Context, prj *project.Project, modulePath, command string) (CacheProbesResult, error) {
	if prj == nil {
		return CacheProbesResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return CacheProbesResult{}, err
	}
	return CacheProbesResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Probes:  append([]responsepayload.CacheProbe(nil), summary.CacheProbes...),
	}, nil
}

func (s *Service) CacheProbeRecords(_ context.Context, prj *project.Project, modulePath, command string) (CacheProbeRecordsResult, error) {
	if prj == nil {
		return CacheProbeRecordsResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return CacheProbeRecordsResult{}, err
	}
	return CacheProbeRecordsResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Records: append([]responsepayload.CacheProbeRecord(nil), summary.CacheProbeRecords...),
	}, nil
}

func (s *Service) ReuseDecision(_ context.Context, prj *project.Project, modulePath, command, actionID string) (ReuseDecisionResult, error) {
	if prj == nil {
		return ReuseDecisionResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ReuseDecisionResult{}, err
	}
	decision, ok := reuseDecisionForID(summary, actionID)
	if !ok {
		return ReuseDecisionResult{}, os.ErrNotExist
	}
	return ReuseDecisionResult{
		Repo:     prj.RootDir,
		Module:   modulePath,
		Command:  command,
		Path:     path,
		ActionID: actionID,
		Decision: decision,
	}, nil
}

func (s *Service) ReuseDecisions(_ context.Context, prj *project.Project, modulePath, command string) (ReuseDecisionsResult, error) {
	if prj == nil {
		return ReuseDecisionsResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ReuseDecisionsResult{}, err
	}
	return ReuseDecisionsResult{
		Repo:      prj.RootDir,
		Module:    modulePath,
		Command:   command,
		Path:      path,
		Decisions: reuseDecisions(summary),
	}, nil
}

func (s *Service) ActionTrace(_ context.Context, prj *project.Project, modulePath, command string) (ActionTraceResult, error) {
	if prj == nil {
		return ActionTraceResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return ActionTraceResult{}, err
	}
	if len(summary.ActionExecutions) == 0 {
		return ActionTraceResult{}, os.ErrNotExist
	}
	explanations := make(map[string]explain.Action, len(summary.ActionExplanations))
	for _, item := range summary.ActionExplanations {
		explanations[item.ActionID] = item
	}
	trace := make([]ActionTraceEntry, 0, len(summary.ActionExecutions))
	for _, execution := range summary.ActionExecutions {
		entry := ActionTraceEntry{
			ActionID:       execution.ActionID,
			Name:           execution.Name,
			Operation:      execution.Operation,
			ModulePath:     execution.ModulePath,
			VariantName:    execution.VariantName,
			BatchIndex:     execution.BatchIndex,
			CriticalPath:   execution.CriticalPath,
			QueueWaitMs:    execution.QueueWaitMs,
			WaitReason:     execution.WaitReason,
			WorkerClass:    execution.WorkerClass,
			ResourceClass:  execution.ResourceClass,
			ResourceCost:   execution.ResourceCost,
			MaxParallelism: execution.MaxParallelism,
			CacheKey:       execution.CacheKey,
			Cacheable:      execution.Cacheable,
			ProbeOrder:     append([]string(nil), execution.ProbeOrder...),
			ExecuteOnMiss:  execution.ExecuteOnMiss,
			RetentionClass: execution.RetentionClass,
			Shareability:   execution.Shareability,
			Status:         execution.Status,
			DurationMs:     execution.DurationMs,
			Substeps:       flattenActionTraceSubsteps(execution.Timings),
			Timings:        cloneTimingEntries(timingEntries(execution.Timings)),
		}
		if execution.CacheProbe != nil {
			entry.CacheResult = execution.CacheProbe.State
			entry.CacheBasis = execution.CacheProbe.Basis
		} else if explanation, ok := explanations[execution.ActionID]; ok && explanation.Cache != nil {
			entry.CacheResult = string(explanation.Cache.State)
			entry.CacheBasis = explanation.Cache.Basis
		}
		trace = append(trace, entry)
	}
	return ActionTraceResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Actions: trace,
	}, nil
}

func (s *Service) PerfTiming(_ context.Context, prj *project.Project, modulePath, command string) (PerfTimingResult, error) {
	if prj == nil {
		return PerfTimingResult{}, os.ErrInvalid
	}
	path := runSummaryPath(prj.RootDir, modulePath, command)
	summary, err := readRunSummary(path)
	if err != nil {
		return PerfTimingResult{}, err
	}
	return PerfTimingResult{
		Repo:    prj.RootDir,
		Module:  modulePath,
		Command: command,
		Path:    path,
		Timing:  summary.PerfTiming,
	}, nil
}

func cloneTimingData(data *perf.TimingData) *perf.TimingData {
	if data == nil {
		return nil
	}
	return perf.List(cloneTimingEntries(data.Entries()))
}

func timingEntries(data *perf.TimingData) []perf.TimingEntry {
	if data == nil {
		return nil
	}
	return data.Entries()
}

func cloneTimingEntries(entries []perf.TimingEntry) []perf.TimingEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]perf.TimingEntry, 0, len(entries))
	for _, entry := range entries {
		cloned := perf.TimingEntry{
			Name:       entry.Name,
			DurationMs: entry.DurationMs,
		}
		if entry.Children != nil {
			cloned.Children = cloneTimingData(entry.Children)
		}
		if entry.Explanation != nil {
			explainCopy := *entry.Explanation
			cloned.Explanation = &explainCopy
		}
		out = append(out, cloned)
	}
	return out
}

func flattenActionTraceSubsteps(data *perf.TimingData) []ActionTraceSubstep {
	if data == nil {
		return nil
	}
	var out []ActionTraceSubstep
	var walk func(entries []perf.TimingEntry, depth int)
	walk = func(entries []perf.TimingEntry, depth int) {
		for _, entry := range entries {
			substep := ActionTraceSubstep{
				Name:       entry.Name,
				Depth:      depth,
				DurationMs: entry.DurationMs,
			}
			if entry.Explanation != nil {
				substep.CacheResult = string(entry.Explanation.State)
				substep.CacheBasis = entry.Explanation.Basis
			}
			out = append(out, substep)
			if entry.Children != nil {
				walk(entry.Children.Entries(), depth+1)
			}
		}
	}
	walk(data.Entries(), 0)
	if len(out) == 0 {
		return nil
	}
	return out
}

func actionExplanationForID(items []explain.Action, actionID string) (explain.Action, bool) {
	for _, item := range items {
		if item.ActionID == actionID {
			return item, true
		}
	}
	return explain.Action{}, false
}

func actionExecutionForID(items []ActionExecution, actionID string) (ActionExecution, bool) {
	for _, item := range items {
		if item.ActionID == actionID {
			return item, true
		}
	}
	return ActionExecution{}, false
}

func reuseDecisionForID(summary RunSummaryRecord, actionID string) (ReuseDecision, bool) {
	for _, item := range reuseDecisions(summary) {
		if item.ActionID == actionID {
			return item, true
		}
	}
	return ReuseDecision{}, false
}

func reuseDecisions(summary RunSummaryRecord) []ReuseDecision {
	ids := map[string]struct{}{}
	for _, item := range summary.ActionExecutions {
		if item.ActionID != "" {
			ids[item.ActionID] = struct{}{}
		}
	}
	for _, item := range summary.ActionExplanations {
		if item.ActionID != "" {
			ids[item.ActionID] = struct{}{}
		}
	}
	for _, item := range summary.CacheProbes {
		if item.ActionID != "" {
			ids[item.ActionID] = struct{}{}
		}
	}
	for _, item := range summary.CacheProbeRecords {
		if item.ActionID != "" {
			ids[item.ActionID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	actionIDs := make([]string, 0, len(ids))
	for actionID := range ids {
		actionIDs = append(actionIDs, actionID)
	}
	sort.Strings(actionIDs)
	out := make([]ReuseDecision, 0, len(actionIDs))
	for _, actionID := range actionIDs {
		out = append(out, buildReuseDecision(summary, actionID))
	}
	return out
}

func buildReuseDecision(summary RunSummaryRecord, actionID string) ReuseDecision {
	decision := ReuseDecision{ActionID: actionID}
	if execution, ok := actionExecutionForID(summary.ActionExecutions, actionID); ok {
		cloned := execution
		decision.Execution = &cloned
		decision.Name = execution.Name
		decision.Operation = execution.Operation
		decision.ModulePath = execution.ModulePath
		decision.VariantName = execution.VariantName
		if execution.CacheProbe != nil {
			probe := *execution.CacheProbe
			decision.Probe = &probe
			decision.CacheOutcome = execution.CacheProbe.State
			decision.CacheSource = "execution-cache-probe"
			appendUniqueString(&decision.Basis, execution.CacheProbe.Basis)
			appendUniqueString(&decision.Reasons, execution.CacheProbe.Detail)
		}
		if len(execution.CacheProbeTrail) != 0 {
			decision.ProbeRecords = append([]responsepayload.CacheProbeRecord(nil), execution.CacheProbeTrail...)
			for _, record := range execution.CacheProbeTrail {
				appendUniqueString(&decision.Basis, record.Basis)
				appendUniqueString(&decision.Reasons, record.Detail)
			}
		}
		if decision.CacheOutcome == "" && execution.Status != "" {
			decision.CacheOutcome = execution.Status
			decision.CacheSource = "execution-status"
		}
	}
	if explanation, ok := actionExplanationForID(summary.ActionExplanations, actionID); ok {
		cloned := explanation
		decision.Explain = &cloned
		if decision.Name == "" {
			decision.Name = explanation.Name
		}
		if decision.Operation == "" {
			decision.Operation = explanation.Operation
		}
		if explanation.Cache != nil {
			if decision.CacheOutcome == "" {
				decision.CacheOutcome = string(explanation.Cache.State)
				decision.CacheSource = "action-explanation"
			}
			appendUniqueString(&decision.Basis, explanation.Cache.Basis)
			appendUniqueString(&decision.Reasons, explanation.Cache.Detail)
		}
	}
	if decision.Probe == nil {
		if probe, ok := cacheProbeForID(summary.CacheProbes, actionID); ok {
			cloned := probe
			decision.Probe = &cloned
			decision.CacheOutcome = probe.State
			decision.CacheSource = "summary-cache-probe"
			appendUniqueString(&decision.Basis, probe.Basis)
			appendUniqueString(&decision.Reasons, probe.Detail)
		}
	}
	if len(decision.ProbeRecords) == 0 {
		records := cacheProbeRecordsForID(summary.CacheProbeRecords, actionID)
		if len(records) != 0 {
			decision.ProbeRecords = records
			for _, record := range records {
				appendUniqueString(&decision.Basis, record.Basis)
				appendUniqueString(&decision.Reasons, record.Detail)
			}
		}
	}
	return decision
}

func cacheProbeForID(items []responsepayload.CacheProbe, actionID string) (responsepayload.CacheProbe, bool) {
	for _, item := range items {
		if item.ActionID == actionID {
			return item, true
		}
	}
	return responsepayload.CacheProbe{}, false
}

func cacheProbeRecordsForID(items []responsepayload.CacheProbeRecord, actionID string) []responsepayload.CacheProbeRecord {
	var out []responsepayload.CacheProbeRecord
	for _, item := range items {
		if item.ActionID == actionID {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		if out[i].StepName != out[j].StepName {
			return out[i].StepName < out[j].StepName
		}
		if out[i].Basis != out[j].Basis {
			return out[i].Basis < out[j].Basis
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

func appendUniqueString(items *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range *items {
		if existing == value {
			return
		}
	}
	*items = append(*items, value)
}

func planExplanationReasons(plan BuildPlan) []string {
	var reasons []string
	if plan.VariantExplicit {
		reasons = append(reasons, "requested variant was explicit")
	} else {
		reasons = append(reasons, "variant was derived from command semantics")
	}
	if len(plan.TargetResolvedVariants) > 1 {
		reasons = append(reasons, "command targets multiple resolved variants")
	}
	if len(plan.Schedule.Batches) > 0 {
		reasons = append(reasons, "actions were scheduled from persisted graph state")
	}
	return reasons
}

func impactNodeForRef(g *graph.Graph, ref graph.NodeRef) (ImpactNode, bool) {
	switch ref.Kind {
	case graph.NodeKindLogicalModule:
		mod, ok := g.LogicalModule(graph.LogicalModuleID(ref.ID))
		if !ok {
			return ImpactNode{}, false
		}
		return ImpactNode{Kind: string(ref.Kind), ID: ref.ID, ModulePath: mod.Path, Name: mod.Name}, true
	case graph.NodeKindVariant:
		variant, ok := g.Variant(graph.VariantID(ref.ID))
		if !ok {
			return ImpactNode{}, false
		}
		return ImpactNode{Kind: string(ref.Kind), ID: ref.ID, VariantName: variant.Name, Name: variant.Name}, true
	case graph.NodeKindAction:
		action, ok := g.Action(graph.ActionID(ref.ID))
		if !ok {
			return ImpactNode{}, false
		}
		return ImpactNode{Kind: string(ref.Kind), ID: ref.ID, ModulePath: action.Attributes["modulePath"], VariantName: action.Attributes["variantName"], Name: action.Name}, true
	case graph.NodeKindArtifact:
		artifact, ok := g.Artifact(graph.ArtifactID(ref.ID))
		if !ok {
			return ImpactNode{}, false
		}
		return ImpactNode{Kind: string(ref.Kind), ID: ref.ID, Name: artifact.Path}, true
	case graph.NodeKindMaterialization:
		mat, ok := g.Materialization(graph.MaterializationID(ref.ID))
		if !ok {
			return ImpactNode{}, false
		}
		return ImpactNode{Kind: string(ref.Kind), ID: ref.ID, Name: mat.Note}, true
	default:
		return ImpactNode{}, false
	}
}

func actionIDs(ids []graph.ActionID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func artifactIDs(ids []graph.ArtifactID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func semanticVariantNames(variants []project.SemanticVariantSummary) []string {
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		if name := strings.TrimSpace(variant.Name); name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func errString(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	return errors.New(message)
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmptyStrings(values ...[]string) []string {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		return value
	}
	return nil
}

func androidTestPackageName(mod *project.Module, variantName string) string {
	if mod == nil {
		return ""
	}
	resolved := mod.ResolveVariant(variantName)
	base := strings.TrimSpace(resolved.ApplicationID)
	if base == "" {
		base = strings.TrimSpace(mod.ApplicationID)
	}
	if base == "" {
		base = strings.TrimSpace(mod.Namespace)
	}
	if base == "" {
		return ""
	}
	if strings.HasPrefix(base, ".") && strings.TrimSpace(mod.Namespace) != "" {
		base = strings.TrimSpace(mod.Namespace) + base
	}
	return base + ".test"
}

func androidTestManifestOutputPath(rootDir, modulePath, variantName string) string {
	if strings.TrimSpace(rootDir) == "" || strings.TrimSpace(modulePath) == "" || strings.TrimSpace(variantName) == "" {
		return ""
	}
	rel := strings.TrimPrefix(strings.ReplaceAll(modulePath, ":", string(os.PathSeparator)), string(os.PathSeparator))
	return filepath.Join(rootDir, "build", "grit", rel, variantName+"AndroidTest", "AndroidManifest.xml")
}

func androidTestTaskAlias(aliases []string, prefix string) string {
	suffix := "AndroidTest"
	for _, alias := range aliases {
		if strings.HasPrefix(alias, prefix) && strings.HasSuffix(alias, suffix) {
			return alias
		}
	}
	return ""
}

func (s *Service) Tasks(mod *project.Module, prj *project.Project) TasksResult {
	return TasksResult{Repo: prj.RootDir, Module: mod.Path, Tasks: mod.Tasks()}
}

func (s *Service) SigningReport(mod *project.Module, prj *project.Project) SigningReportResult {
	variants := make([]SigningReportVariant, 0, len(moduleVariantNames(mod)))
	for _, variant := range moduleVariantNames(mod) {
		buildType := mod.Variant(variant)
		resolvedName, signing := resolveSigningForVariant(mod, buildType)
		variants = append(variants, SigningReportVariant{
			Name:             variant,
			SigningConfig:    buildType.SigningConfig,
			ResolvedConfig:   resolvedName,
			StoreFile:        signing.StoreFile,
			KeyAlias:         signing.KeyAlias,
			HasStorePassword: signing.StorePassword != "",
			HasKeyPassword:   signing.KeyPassword != "",
		})
	}
	return SigningReportResult{Repo: prj.RootDir, Module: mod.Path, Variants: variants}
}

func (s *Service) Projects(prj *project.Project) ProjectsResult {
	modules := make([]string, 0, len(prj.Modules))
	for _, mod := range prj.Modules {
		modules = append(modules, mod.Path)
	}
	return ProjectsResult{Repo: prj.RootDir, Name: prj.Name, Modules: modules}
}

func (s *Service) Properties(mod *project.Module, prj *project.Project) PropertiesResult {
	model, _ := s.LoadConfigurationModel(context.Background(), prj)
	return PropertiesResult{
		Repo:   prj.RootDir,
		Module: mod.Path,
		Type:   mod.Type,
		Values: responsepayload.PropertiesValues{
			Namespace:                 mod.Namespace,
			ApplicationID:             mod.ApplicationID,
			VersionCode:               mod.VersionCode,
			VersionName:               mod.VersionName,
			CompileSDK:                mod.CompileSDK,
			BuildToolsVersion:         mod.BuildToolsVersion,
			MinSDK:                    mod.MinSDK,
			TargetSDK:                 mod.TargetSDK,
			TestInstrumentationRunner: mod.TestInstrumentationRunner,
			UsesCompose:               mod.UsesCompose,
			UsesMetro:                 mod.UsesMetro,
			UsesWire:                  mod.UsesWire,
			KotlinFreeCompilerArgs:    mod.KotlinFreeCompilerArgs,
			LintDisabledChecks:        mod.LintDisabledChecks,
			ConsumerProguardFiles:     mod.ConsumerProguardFiles,
			RequestedTasks:            mod.DefaultTasks(),
		},
		Variants:         mod.Variants(),
		ResolvedVariants: resolvedVariantsForModule(model, prj, mod),
	}
}

func (s *Service) Dependencies(mod *project.Module, prj *project.Project) (DependenciesResult, error) {
	deps, err := modulebuild.ParseDependencies(mod.BuildFile)
	if err != nil {
		return DependenciesResult{}, err
	}
	return DependenciesResult{
		Repo:   prj.RootDir,
		Module: mod.Path,
		Scopes: map[string][]string{
			"main":                  refsToStrings(deps.Main),
			"debug":                 refsToStrings(deps.Debug),
			"test":                  refsToStrings(deps.Test),
			"compileOnly":           refsToStrings(deps.CompileOnly),
			"runtimeOnly":           refsToStrings(deps.RuntimeOnly),
			"testCompileOnly":       refsToStrings(deps.TestCompileOnly),
			"testRuntimeOnly":       refsToStrings(deps.TestRuntimeOnly),
			"coreLibraryDesugaring": refsToStrings(deps.CoreLibraryDesugaring),
		},
	}, nil
}

func (s *Service) BuildEnvironment(prj *project.Project) BuildEnvironmentResult {
	return BuildEnvironmentResult{
		Repo:             prj.RootDir,
		SettingsFile:     prj.SettingsFile,
		RootBuildFile:    prj.RootBuildFile,
		Repositories:     prj.Repositories,
		GradleProperties: prj.GradleProperties,
		VersionCatalogs:  prj.VersionCatalogs,
	}
}

func (s *Service) ArtifactTransforms(mod *project.Module, prj *project.Project) ArtifactTransformsResult {
	transforms := []string{"maven-resolution", "aar-extraction", "android-resource-compilation", "classpath-normalization", "dex-merging", "apk-signing"}
	if strings.HasPrefix(mod.Type, "jvm-") {
		transforms = []string{"maven-resolution", "classpath-normalization", "jar-packaging"}
	}
	return ArtifactTransformsResult{Repo: prj.RootDir, Module: mod.Path, Transforms: transforms}
}

func (s *Service) DependencyInsight(mod *project.Module, prj *project.Project, query string) (DependencyInsightResult, error) {
	deps, err := modulebuild.ParseDependencies(mod.BuildFile)
	if err != nil {
		return DependencyInsightResult{}, err
	}
	scopes := map[string][]string{
		"main":                  refsToStrings(deps.Main),
		"debug":                 refsToStrings(deps.Debug),
		"test":                  refsToStrings(deps.Test),
		"compileOnly":           refsToStrings(deps.CompileOnly),
		"runtimeOnly":           refsToStrings(deps.RuntimeOnly),
		"testCompileOnly":       refsToStrings(deps.TestCompileOnly),
		"testRuntimeOnly":       refsToStrings(deps.TestRuntimeOnly),
		"coreLibraryDesugaring": refsToStrings(deps.CoreLibraryDesugaring),
	}
	matches := map[string][]string{}
	if query != "" {
		lower := strings.ToLower(query)
		for scope, refs := range scopes {
			for _, ref := range refs {
				if strings.Contains(strings.ToLower(ref), lower) {
					matches[scope] = append(matches[scope], ref)
				}
			}
		}
	}
	return DependencyInsightResult{
		Repo:    prj.RootDir,
		Module:  mod.Path,
		Query:   query,
		Scopes:  scopes,
		Matches: matches,
	}, nil
}

func (s *Service) ResolverReport(mod *project.Module, prj *project.Project) (ResolverReportResult, error) {
	deps, err := modulebuild.ParseDependencies(mod.BuildFile)
	if err != nil {
		return ResolverReportResult{}, err
	}
	product, err := dependencywiring.LoadCachedResolvedProduct(prj, deps)
	if err != nil {
		return ResolverReportResult{}, err
	}
	return ResolverReportResult{
		Repo:         prj.RootDir,
		Module:       mod.Path,
		CachePath:    product.CachePath,
		ReportPath:   product.ReportPath,
		ReplayPath:   product.ReplayPath,
		LockfilePath: product.LockfilePath,
		Found:        product.Found,
		Topology:     product.Topology,
		Inputs:       product.Inputs,
		Summary: ResolverReportSummary{
			CompileJarCount:     len(product.Resolved.CompileJars),
			RuntimeJarCount:     len(product.Resolved.RuntimeJars),
			TestJarCount:        len(product.Resolved.TestJars),
			AndroidLibraryCount: len(product.Resolved.AndroidLibraries),
			SelectionCount:      len(product.Resolved.Report.Selections),
			ConflictCount:       len(product.Resolved.Report.Conflicts),
			PinCount:            len(product.Resolved.Replay.Pins),
		},
		Report:   product.Resolved.Report,
		Replay:   product.Resolved.Replay,
		Lockfile: product.Resolved.Lockfile,
	}, nil
}

func (s *Service) CacheTopology(prj *project.Project) (CacheTopologyResult, error) {
	if prj == nil {
		return CacheTopologyResult{}, os.ErrInvalid
	}
	topology, err := dependencywiring.CacheTopology(prj)
	if err != nil {
		return CacheTopologyResult{}, err
	}
	return CacheTopologyResult{
		Repo:     prj.RootDir,
		Topology: topology,
	}, nil
}

func (s *Service) KotlinDslAccessorsReport(mod *project.Module, prj *project.Project) KotlinDslAccessorsReportResult {
	accessors := []string{}
	if mod.Type == "android-application" {
		accessors = append(accessors, "android", "defaultConfig", "buildTypes", "signingConfigs")
	} else if mod.Type == "android-library" {
		accessors = append(accessors, "android", "defaultConfig", "buildTypes")
	}
	if mod.UsesCompose {
		accessors = append(accessors, "buildFeatures.compose")
	}
	if len(mod.KotlinFreeCompilerArgs) > 0 {
		accessors = append(accessors, "tasks.withType<KotlinCompile>().compilerOptions")
	}
	return KotlinDslAccessorsReportResult{Repo: prj.RootDir, Module: mod.Path, Accessors: accessors}
}

func (s *Service) OutgoingVariants(mod *project.Module, prj *project.Project) OutgoingVariantsResult {
	model, _ := s.LoadConfigurationModel(context.Background(), prj)
	return OutgoingVariantsResult{
		Repo:             prj.RootDir,
		Module:           mod.Path,
		Variants:         mod.Variants(),
		ResolvedVariants: resolvedVariantsForModule(model, prj, mod),
	}
}

func (s *Service) ResolvableConfigurations(mod *project.Module, prj *project.Project) ResolvableConfigurationsResult {
	configs := map[string][]string{
		"compileClasspath":     nil,
		"runtimeClasspath":     nil,
		"testCompileClasspath": nil,
		"testRuntimeClasspath": nil,
	}
	if mod.Type == "android-application" || mod.Type == "android-library" {
		for _, variant := range resolvedVariantsForModule(nil, prj, mod) {
			name := strings.TrimSpace(variant.Name)
			if name == "" {
				continue
			}
			configs[name+"CompileClasspath"] = nil
			configs[name+"RuntimeClasspath"] = nil
		}
	}
	return ResolvableConfigurationsResult{Repo: prj.RootDir, Module: mod.Path, Configurations: configs}
}

func moduleVariantNames(mod *project.Module) []string {
	variants := mod.Variants()
	if len(variants) == 0 {
		return []string{"debug"}
	}
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		out = append(out, variant.Name)
	}
	return out
}

func resolvedVariantsForModule(model *configmodel.Model, prj *project.Project, mod *project.Module) []project.ResolvedVariant {
	if model != nil {
		if variants, err := model.ResolvedVariants(mod.Path); err == nil && len(variants) > 0 {
			return variants
		}
	}
	if mod == nil {
		return nil
	}
	if variants := mod.ResolvedVariants(); len(variants) > 0 {
		return variants
	}
	return nil
}

func resolveSigningForVariant(mod *project.Module, variant project.BuildType) (string, project.SigningConfig) {
	for _, candidate := range strings.Split(variant.SigningConfig, "|") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if cfg, ok := mod.SigningConfigs[candidate]; ok && cfg.StoreFile != "" {
			return candidate, cfg
		}
		if candidate == "debug" {
			return "debug", project.SigningConfig{
				Name:          "debug",
				StoreFile:     filepath.Join(os.Getenv("HOME"), ".android", "debug.keystore"),
				StorePassword: "android",
				KeyAlias:      "androiddebugkey",
				KeyPassword:   "android",
			}
		}
	}
	return "", project.SigningConfig{}
}

func refsToStrings(refs []modulebuild.Ref) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Kind+":"+ref.Value)
	}
	return out
}

func wireConfigPointer(mod project.Module) *project.WireConfig {
	if !mod.UsesWire {
		return nil
	}
	cfg := mod.WireConfig
	return &cfg
}
