package integration

import (
	"context"

	"github.com/kaeawc/grit/internal/classpath"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/materialization"
	"github.com/kaeawc/grit/internal/project"
)

type ReadOnlyModel interface {
	CacheKey() string
	GraphSummary() project.SemanticGraphSummary
	Module(path string) (project.SemanticModuleSummary, bool)
	Variant(modulePath, variantName string) (project.SemanticVariantSummary, bool)
	ModuleByID(id graph.LogicalModuleID) (ModuleByIDResult, bool)
	VariantByID(id graph.VariantID) (VariantByIDResult, bool)
	ActionByID(id graph.ActionID) (ActionByIDResult, bool)
	PlannedActionPolicy(id graph.ActionID) (PlannedActionPolicyResult, bool)
	PlannedActionPolicies(modulePath string) (PlannedActionPoliciesResult, bool)
	ArtifactByID(id graph.ArtifactID) (ArtifactByIDResult, bool)
	MaterializationByID(id graph.MaterializationID) (MaterializationByIDResult, bool)
	MaterializationConsumers(id graph.MaterializationID) (MaterializationConsumersResult, bool)
	Action(id graph.ActionID) (graph.Action, bool)
	ActionInputs(id graph.ActionID) []graph.Artifact
	ActionOutputs(id graph.ActionID) []graph.Artifact
	ActionInputsResult(id graph.ActionID) (ActionInputsResult, bool)
	ActionOutputsResult(id graph.ActionID) (ActionOutputsResult, bool)
	ActionDependenciesResult(id graph.ActionID) (ActionDependenciesResult, bool)
	ActionDependentsResult(id graph.ActionID) (ActionDependentsResult, bool)
	ActionsForModule(path string) []graph.Action
	ClasspathSnapshot(modulePath, variantName string) (ClasspathSnapshotResult, bool)
	ClasspathSnapshotByID(snapshotID string) (ClasspathSnapshotByIDResult, bool)
	ClasspathSnapshotProvenance(snapshotID string) (ClasspathSnapshotProvenanceResult, bool)
	ClasspathSnapshotConsumers(snapshotID string) (ClasspathSnapshotConsumersResult, bool)
	ClasspathSnapshotConsumersByID(snapshotID string) (ClasspathSnapshotConsumersByIDResult, bool)
	ClasspathEntryLookup(modulePath, variantName, path string) (ClasspathEntryLookupResult, bool)
	ClasspathPathConsumers(path string) (ClasspathPathConsumersResult, bool)
	ArtifactOnClasspath(modulePath, variantName string, artifactID graph.ArtifactID) (ArtifactOnClasspathResult, bool)
	FileOwners(path string) FileOwnersResult
	ArtifactSummariesForModule(path string) []configmodel.ArtifactSummary
	ArtifactSummariesForVariant(modulePath, variantName string) []configmodel.ArtifactSummary
	VariantMaterialization(modulePath, variantName string) (VariantMaterializationResult, bool)
	MaterializationProvenance(id graph.MaterializationID) (MaterializationProvenanceResult, bool)
	ModuleManifest(modulePath string) (ModuleManifestResult, bool)
	VariantManifest(modulePath, variantName string) (VariantManifestResult, bool)
	ArtifactSnapshotProvenance(snapshotID string) (ArtifactSnapshotProvenanceResult, bool)
	ArtifactSnapshotConsumers(snapshotID string) (ArtifactSnapshotConsumersResult, bool)
	ArtifactConsumers(id graph.ArtifactID) (ArtifactConsumersResult, bool)
	ArtifactClasspathConsumers(id graph.ArtifactID) (ArtifactClasspathConsumersResult, bool)
	VariantSourceSetModel(modulePath, variantName string) (VariantSourceSetModelResult, bool)
	DependencyBindingsForVariant(modulePath, variantName string) (DependencyBindingsForVariantResult, bool)
	DependencyBindingsForModule(modulePath string) (DependencyBindingsForModuleResult, bool)
	DependencyRealizationsForVariant(modulePath, variantName string) (DependencyRealizationsForVariantResult, bool)
	DependencyRealizationsForModule(modulePath string) (DependencyRealizationsForModuleResult, bool)
}

type ActionInputsResult struct {
	Action      graph.Action     `json:"action"`
	ModulePath  string           `json:"modulePath,omitempty"`
	VariantName string           `json:"variantName,omitempty"`
	Inputs      []graph.Artifact `json:"inputs,omitempty"`
}

type ActionOutputsResult struct {
	Action      graph.Action     `json:"action"`
	ModulePath  string           `json:"modulePath,omitempty"`
	VariantName string           `json:"variantName,omitempty"`
	Outputs     []graph.Artifact `json:"outputs,omitempty"`
}

type ActionDependenciesResult struct {
	Action       graph.Action                `json:"action"`
	ModulePath   string                      `json:"modulePath,omitempty"`
	VariantName  string                      `json:"variantName,omitempty"`
	Dependencies []configmodel.ActionSummary `json:"dependencies,omitempty"`
}

type ActionDependentsResult struct {
	Action      graph.Action                `json:"action"`
	ModulePath  string                      `json:"modulePath,omitempty"`
	VariantName string                      `json:"variantName,omitempty"`
	Dependents  []configmodel.ActionSummary `json:"dependents,omitempty"`
}

type PlanRequest struct {
	Command          string
	ModulePath       string
	RequestedVariant string
	VariantExplicit  bool
}

type PlanResult struct {
	Command       string
	ModulePath    string
	TargetVariant string
	Variants      []string
	Actions       []graph.Action
}

type ModuleByIDResult struct {
	Module           graph.LogicalModule              `json:"module"`
	Summary          project.SemanticModuleSummary    `json:"summary"`
	Variants         []project.SemanticVariantSummary `json:"variants,omitempty"`
	Materializations []graph.Materialization          `json:"materializations,omitempty"`
	Actions          []graph.Action                   `json:"actions,omitempty"`
	Artifacts        []graph.Artifact                 `json:"artifacts,omitempty"`
}

type VariantByIDResult struct {
	Module           graph.LogicalModule            `json:"module"`
	Variant          graph.Variant                  `json:"variant"`
	Summary          project.SemanticVariantSummary `json:"summary"`
	Materializations []graph.Materialization        `json:"materializations,omitempty"`
	Actions          []graph.Action                 `json:"actions,omitempty"`
	Artifacts        []graph.Artifact               `json:"artifacts,omitempty"`
}

type ActionByIDResult struct {
	Action       graph.Action                `json:"action"`
	ModulePath   string                      `json:"modulePath,omitempty"`
	VariantName  string                      `json:"variantName,omitempty"`
	Summary      configmodel.ActionSummary   `json:"summary"`
	Inputs       []graph.Artifact            `json:"inputs,omitempty"`
	Outputs      []graph.Artifact            `json:"outputs,omitempty"`
	Dependencies []configmodel.ActionSummary `json:"dependencies,omitempty"`
	Dependents   []configmodel.ActionSummary `json:"dependents,omitempty"`
}

type PlannedActionPolicyResult struct {
	Action      graph.Action              `json:"action"`
	ModulePath  string                    `json:"modulePath,omitempty"`
	VariantName string                    `json:"variantName,omitempty"`
	Policy      configmodel.ActionSummary `json:"policy"`
}

type PlannedActionPoliciesVariantResult struct {
	ModulePath  string                      `json:"modulePath,omitempty"`
	VariantName string                      `json:"variantName,omitempty"`
	VariantID   string                      `json:"variantId,omitempty"`
	Policies    []configmodel.ActionSummary `json:"policies,omitempty"`
}

type PlannedActionPoliciesResult struct {
	ModulePath   string                               `json:"modulePath,omitempty"`
	VariantNames []string                             `json:"variantNames,omitempty"`
	Policies     []configmodel.ActionSummary          `json:"policies,omitempty"`
	Variants     []PlannedActionPoliciesVariantResult `json:"variants,omitempty"`
}

type ArtifactByIDResult struct {
	Artifact           graph.Artifact                               `json:"artifact"`
	ModulePath         string                                       `json:"modulePath,omitempty"`
	VariantName        string                                       `json:"variantName,omitempty"`
	MaterializationID  string                                       `json:"materializationId,omitempty"`
	ArtifactSnapshotID string                                       `json:"artifactSnapshotId,omitempty"`
	Summary            configmodel.ArtifactSummary                  `json:"summary"`
	Provenance         configmodel.ProvenanceSummary                `json:"provenance"`
	ClasspathSnapshots []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	Producer           graph.Action                                 `json:"producer,omitempty"`
	Consumers          []graph.Action                               `json:"consumers,omitempty"`
	SiblingArtifacts   []graph.Artifact                             `json:"siblingArtifacts,omitempty"`
}

type MaterializationByIDResult struct {
	Materialization    graph.Materialization                        `json:"materialization"`
	ModulePath         string                                       `json:"modulePath,omitempty"`
	VariantName        string                                       `json:"variantName,omitempty"`
	ArtifactSnapshotID string                                       `json:"artifactSnapshotId,omitempty"`
	Provenance         configmodel.ProvenanceSummary                `json:"provenance"`
	ClasspathSnapshots []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	Artifacts          []configmodel.ArtifactSummary                `json:"artifacts,omitempty"`
	Actions            []configmodel.ActionSummary                  `json:"actions,omitempty"`
}

type MaterializationConsumersResult struct {
	MaterializationID  string                                       `json:"materializationId,omitempty"`
	ModulePath         string                                       `json:"modulePath,omitempty"`
	VariantName        string                                       `json:"variantName,omitempty"`
	ArtifactSnapshotID string                                       `json:"artifactSnapshotId,omitempty"`
	Provenance         configmodel.ProvenanceSummary                `json:"provenance"`
	Actions            []configmodel.ActionSummary                  `json:"actions,omitempty"`
	Artifacts          []configmodel.ArtifactSummary                `json:"artifacts,omitempty"`
	ManifestPaths      []string                                     `json:"manifestPaths,omitempty"`
	ClasspathSnapshots []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
}

type ClasspathProvenanceResult struct {
	ModulePath         string                                       `json:"modulePath,omitempty"`
	VariantName        string                                       `json:"variantName,omitempty"`
	MaterializationID  string                                       `json:"materializationId,omitempty"`
	ArtifactSnapshotID string                                       `json:"artifactSnapshotId,omitempty"`
	SourceRoots        []string                                     `json:"sourceRoots,omitempty"`
	ClasspathSnapshots []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	Artifacts          []graph.Artifact                             `json:"artifacts,omitempty"`
	Actions            []graph.Action                               `json:"actions,omitempty"`
}

type ClasspathSnapshotResult struct {
	ModulePath         string           `json:"modulePath,omitempty"`
	VariantName        string           `json:"variantName,omitempty"`
	MaterializationID  string           `json:"materializationId,omitempty"`
	ArtifactSnapshotID string           `json:"artifactSnapshotId,omitempty"`
	Snapshot           classpath.Record `json:"snapshot"`
}

type ClasspathSnapshotByIDResult struct {
	LookupID    string                  `json:"lookupId,omitempty"`
	CanonicalID string                  `json:"canonicalId,omitempty"`
	Result      ClasspathSnapshotResult `json:"result"`
}

type ClasspathSnapshotProvenanceResult struct {
	ClasspathSnapshotID string                          `json:"classpathSnapshotId,omitempty"`
	Variants            []configmodel.ProvenanceSummary `json:"variants,omitempty"`
	Artifacts           []configmodel.ArtifactSummary   `json:"artifacts,omitempty"`
	ManifestPaths       []string                        `json:"manifestPaths,omitempty"`
}

type ClasspathSnapshotConsumersResult struct {
	ClasspathSnapshotID string                          `json:"classpathSnapshotId,omitempty"`
	Variants            []configmodel.ProvenanceSummary `json:"variants,omitempty"`
	Actions             []configmodel.ActionSummary     `json:"actions,omitempty"`
	Artifacts           []configmodel.ArtifactSummary   `json:"artifacts,omitempty"`
	ManifestPaths       []string                        `json:"manifestPaths,omitempty"`
}

type ClasspathSnapshotConsumersByIDResult struct {
	LookupID    string                           `json:"lookupId,omitempty"`
	CanonicalID string                           `json:"canonicalId,omitempty"`
	Consumers   ClasspathSnapshotConsumersResult `json:"consumers"`
}

type ClasspathEntryLookupResult struct {
	ModulePath         string                            `json:"modulePath,omitempty"`
	VariantName        string                            `json:"variantName,omitempty"`
	MaterializationID  string                            `json:"materializationId,omitempty"`
	ArtifactSnapshotID string                            `json:"artifactSnapshotId,omitempty"`
	Path               string                            `json:"path,omitempty"`
	Entry              classpath.EntryRecord             `json:"entry"`
	Decisions          []classpath.NormalizationDecision `json:"decisions,omitempty"`
}

type ClasspathPathConsumersResult struct {
	Path      string                       `json:"path,omitempty"`
	Consumers []ClasspathEntryLookupResult `json:"consumers,omitempty"`
}

type ArtifactOnClasspathResult struct {
	ModulePath         string                `json:"modulePath,omitempty"`
	VariantName        string                `json:"variantName,omitempty"`
	MaterializationID  string                `json:"materializationId,omitempty"`
	ArtifactSnapshotID string                `json:"artifactSnapshotId,omitempty"`
	Artifact           graph.Artifact        `json:"artifact"`
	Present            bool                  `json:"present"`
	Entry              classpath.EntryRecord `json:"entry"`
}

type ArtifactClasspathConsumersResult struct {
	Artifact  graph.Artifact              `json:"artifact"`
	Consumers []ArtifactOnClasspathResult `json:"consumers,omitempty"`
}

type FileOwnersResult struct {
	Path   string      `json:"path"`
	Owners []FileOwner `json:"owners,omitempty"`
}

type FileOwner struct {
	ModulePath  string   `json:"modulePath,omitempty"`
	VariantName string   `json:"variantName,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Paths       []string `json:"paths,omitempty"`
}

type ArtifactProvenanceResult struct {
	Artifact           graph.Artifact                               `json:"artifact"`
	ModulePath         string                                       `json:"modulePath,omitempty"`
	VariantName        string                                       `json:"variantName,omitempty"`
	MaterializationID  string                                       `json:"materializationId,omitempty"`
	ArtifactSnapshotID string                                       `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshots []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	Producer           graph.Action                                 `json:"producer,omitempty"`
	Consumers          []graph.Action                               `json:"consumers,omitempty"`
	SiblingArtifacts   []graph.Artifact                             `json:"siblingArtifacts,omitempty"`
}

type ArtifactConsumersResult struct {
	Artifact           graph.Artifact                               `json:"artifact"`
	ModulePath         string                                       `json:"modulePath,omitempty"`
	VariantName        string                                       `json:"variantName,omitempty"`
	MaterializationID  string                                       `json:"materializationId,omitempty"`
	ArtifactSnapshotID string                                       `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshots []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	Producer           graph.Action                                 `json:"producer,omitempty"`
	Consumers          []graph.Action                               `json:"consumers,omitempty"`
	SiblingArtifacts   []graph.Artifact                             `json:"siblingArtifacts,omitempty"`
}

type VariantMaterializationResult struct {
	ModulePath      string                        `json:"modulePath,omitempty"`
	VariantName     string                        `json:"variantName,omitempty"`
	VariantID       string                        `json:"variantId,omitempty"`
	Materialization configmodel.ProvenanceSummary `json:"materialization"`
	Actions         []configmodel.ActionSummary   `json:"actions,omitempty"`
	Artifacts       []configmodel.ArtifactSummary `json:"artifacts,omitempty"`
}

type MaterializationProvenanceResult struct {
	Materialization    graph.Materialization         `json:"materialization"`
	ModulePath         string                        `json:"modulePath,omitempty"`
	VariantName        string                        `json:"variantName,omitempty"`
	ArtifactSnapshotID string                        `json:"artifactSnapshotId,omitempty"`
	Provenance         configmodel.ProvenanceSummary `json:"provenance"`
	Actions            []configmodel.ActionSummary   `json:"actions,omitempty"`
	Artifacts          []configmodel.ArtifactSummary `json:"artifacts,omitempty"`
}

type ModuleManifestResult struct {
	ModulePath           string                        `json:"modulePath,omitempty"`
	VariantNames         []string                      `json:"variantNames,omitempty"`
	MaterializationIDs   []string                      `json:"materializationIds,omitempty"`
	ArtifactSnapshotIDs  []string                      `json:"artifactSnapshotIds,omitempty"`
	ManifestPaths        []string                      `json:"manifestPaths,omitempty"`
	SourceRoots          []string                      `json:"sourceRoots,omitempty"`
	ClasspathSnapshotIDs []string                      `json:"classpathSnapshotIds,omitempty"`
	ActionIDs            []string                      `json:"actionIds,omitempty"`
	ProducedArtifactIDs  []string                      `json:"producedArtifactIds,omitempty"`
	BackingArtifactIDs   []string                      `json:"backingArtifactIds,omitempty"`
	Actions              []configmodel.ActionSummary   `json:"actions,omitempty"`
	Artifacts            []configmodel.ArtifactSummary `json:"artifacts,omitempty"`
	Variants             []VariantManifestResult       `json:"variants,omitempty"`
}

type VariantManifestResult struct {
	ModulePath           string                                       `json:"modulePath,omitempty"`
	VariantName          string                                       `json:"variantName,omitempty"`
	VariantID            string                                       `json:"variantId,omitempty"`
	MaterializationID    string                                       `json:"materializationId,omitempty"`
	ArtifactSnapshotID   string                                       `json:"artifactSnapshotId,omitempty"`
	SourceRoots          []string                                     `json:"sourceRoots,omitempty"`
	ManifestPaths        []string                                     `json:"manifestPaths,omitempty"`
	ClasspathSnapshotIDs []string                                     `json:"classpathSnapshotIds,omitempty"`
	ClasspathSnapshots   []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	ActionIDs            []string                                     `json:"actionIds,omitempty"`
	Actions              []configmodel.ActionSummary                  `json:"actions,omitempty"`
	ProducedArtifactIDs  []string                                     `json:"producedArtifactIds,omitempty"`
	ProducedArtifacts    []configmodel.ArtifactSummary                `json:"producedArtifacts,omitempty"`
	BackingArtifactID    string                                       `json:"backingArtifactId,omitempty"`
	BackingArtifact      *configmodel.ArtifactSummary                 `json:"backingArtifact,omitempty"`
	Materialization      project.SemanticMaterializationSummary       `json:"materialization"`
	Provenance           configmodel.ProvenanceSummary                `json:"provenance"`
}

type VariantSourceSetModelResult struct {
	ModulePath           string                                       `json:"modulePath,omitempty"`
	VariantName          string                                       `json:"variantName,omitempty"`
	VariantID            string                                       `json:"variantId,omitempty"`
	DisplayName          string                                       `json:"displayName,omitempty"`
	CoordinateName       string                                       `json:"coordinateName,omitempty"`
	BuildType            string                                       `json:"buildType,omitempty"`
	Flavors              []string                                     `json:"flavors,omitempty"`
	SourceSetOrder       []string                                     `json:"sourceSetOrder,omitempty"`
	SourceSetNames       []string                                     `json:"sourceSetNames,omitempty"`
	SourceRoots          []string                                     `json:"sourceRoots,omitempty"`
	ManifestPaths        []string                                     `json:"manifestPaths,omitempty"`
	ClasspathSnapshotIDs []string                                     `json:"classpathSnapshotIds,omitempty"`
	ClasspathSnapshots   []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	Materialization      project.SemanticMaterializationSummary       `json:"materialization"`
	Provenance           configmodel.ProvenanceSummary                `json:"provenance"`
}

type DependencyBindingsForVariantResult struct {
	ModulePath   string                                 `json:"modulePath,omitempty"`
	VariantName  string                                 `json:"variantName,omitempty"`
	VariantID    string                                 `json:"variantId,omitempty"`
	BuildType    string                                 `json:"buildType,omitempty"`
	Flavors      []string                               `json:"flavors,omitempty"`
	Dependencies []project.SemanticDependencyProvenance `json:"dependencies,omitempty"`
}

type DependencyBindingsForModuleResult struct {
	ModulePath string                               `json:"modulePath,omitempty"`
	Variants   []DependencyBindingsForVariantResult `json:"variants,omitempty"`
}

type DependencyRealization struct {
	ModulePath            string                        `json:"modulePath,omitempty"`
	VariantName           string                        `json:"variantName,omitempty"`
	DependencyLevel       string                        `json:"dependencyLevel,omitempty"`
	RealizationKind       string                        `json:"realizationKind,omitempty"`
	LogicalModuleKind     string                        `json:"logicalModuleKind,omitempty"`
	SelectionReason       string                        `json:"selectionReason,omitempty"`
	SelectionReasons      []string                      `json:"selectionReasons,omitempty"`
	ModuleID              string                        `json:"moduleId,omitempty"`
	VariantID             string                        `json:"variantId,omitempty"`
	Mode                  string                        `json:"mode,omitempty"`
	MaterializationID     string                        `json:"materializationId,omitempty"`
	ArtifactSnapshotID    string                        `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs  []string                      `json:"classpathSnapshotIds,omitempty"`
	SourceRoots           []string                      `json:"sourceRoots,omitempty"`
	ManifestPaths         []string                      `json:"manifestPaths,omitempty"`
	BackingArtifactID     string                        `json:"backingArtifactId,omitempty"`
	BackingArtifactPath   string                        `json:"backingArtifactPath,omitempty"`
	BackingArtifactKind   string                        `json:"backingArtifactKind,omitempty"`
	ProducedArtifactIDs   []string                      `json:"producedArtifactIds,omitempty"`
	ProducedArtifactPaths []string                      `json:"producedArtifactPaths,omitempty"`
	ProducedArtifactKinds []string                      `json:"producedArtifactKinds,omitempty"`
	BackingArtifact       *configmodel.ArtifactSummary  `json:"backingArtifact,omitempty"`
	ProducedArtifacts     []configmodel.ArtifactSummary `json:"producedArtifacts,omitempty"`
}

type DependencyRealizationsForVariantResult struct {
	ModulePath   string                  `json:"modulePath,omitempty"`
	VariantName  string                  `json:"variantName,omitempty"`
	VariantID    string                  `json:"variantId,omitempty"`
	BuildType    string                  `json:"buildType,omitempty"`
	Flavors      []string                `json:"flavors,omitempty"`
	Dependencies []DependencyRealization `json:"dependencies,omitempty"`
}

type DependencyRealizationsForModuleResult struct {
	ModulePath string                                   `json:"modulePath,omitempty"`
	Variants   []DependencyRealizationsForVariantResult `json:"variants,omitempty"`
}

type ArtifactSnapshotProvenanceResult struct {
	ArtifactSnapshotID string                          `json:"artifactSnapshotId,omitempty"`
	Variants           []configmodel.ProvenanceSummary `json:"variants,omitempty"`
	Artifacts          []configmodel.ArtifactSummary   `json:"artifacts,omitempty"`
	ManifestPaths      []string                        `json:"manifestPaths,omitempty"`
}

type ArtifactSnapshotConsumersResult struct {
	ArtifactSnapshotID string                          `json:"artifactSnapshotId,omitempty"`
	Variants           []configmodel.ProvenanceSummary `json:"variants,omitempty"`
	Actions            []configmodel.ActionSummary     `json:"actions,omitempty"`
	Artifacts          []configmodel.ArtifactSummary   `json:"artifacts,omitempty"`
	ManifestPaths      []string                        `json:"manifestPaths,omitempty"`
}

type Hook interface {
	BeforePlan(context.Context, PlanRequest, ReadOnlyModel) error
	AfterPlan(context.Context, PlanResult, ReadOnlyModel) error
}
