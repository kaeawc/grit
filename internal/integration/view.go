package integration

import (
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/kaeawc/grit/internal/classpath"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/materialization"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

type ModelView struct {
	model       *configmodel.Model
	graphOnce   sync.Once
	cachedGraph *graph.Graph
	graphErr    error
}

var _ ReadOnlyModel = (*ModelView)(nil)

type Provenance struct {
	Module             project.SemanticModuleSummary                `json:"module"`
	Variant            project.SemanticVariantSummary               `json:"variant"`
	Materialization    project.SemanticMaterializationSummary       `json:"materialization"`
	ClasspathSnapshots []materialization.ClasspathSnapshotReference `json:"classpathSnapshots,omitempty"`
	CacheProbe         *responsepayload.CacheProbe                  `json:"cacheProbe,omitempty"`
	Action             graph.Action                                 `json:"action,omitempty"`
	Producer           graph.Action                                 `json:"producer,omitempty"`
	Actions            []graph.Action                               `json:"actions,omitempty"`
	Inputs             []graph.Artifact                             `json:"inputs,omitempty"`
	Outputs            []graph.Artifact                             `json:"outputs,omitempty"`
	Artifacts          []graph.Artifact                             `json:"artifacts,omitempty"`
	Consumers          []graph.Action                               `json:"consumers,omitempty"`
}

func NewModelView(model *configmodel.Model) *ModelView {
	return &ModelView{model: model}
}

func (v *ModelView) CacheKey() string {
	if v == nil || v.model == nil {
		return ""
	}
	return v.model.CacheKey()
}

func (v *ModelView) GraphSummary() project.SemanticGraphSummary {
	if v == nil || v.model == nil {
		return project.SemanticGraphSummary{}
	}
	return v.model.GraphSummary()
}

func (v *ModelView) Modules() []project.SemanticModuleSummary {
	if v == nil || v.model == nil {
		return nil
	}
	summary := v.model.GraphSummary()
	return append([]project.SemanticModuleSummary(nil), summary.Modules...)
}

func (v *ModelView) Module(path string) (project.SemanticModuleSummary, bool) {
	if v == nil || v.model == nil {
		return project.SemanticModuleSummary{}, false
	}
	return v.model.Module(path)
}

func (v *ModelView) Variants(modulePath string) []project.SemanticVariantSummary {
	mod, ok := v.Module(modulePath)
	if !ok {
		return nil
	}
	return append([]project.SemanticVariantSummary(nil), mod.Variants...)
}

func (v *ModelView) Variant(modulePath, variantName string) (project.SemanticVariantSummary, bool) {
	if v == nil || v.model == nil {
		return project.SemanticVariantSummary{}, false
	}
	return v.model.Variant(modulePath, variantName)
}

func (v *ModelView) ModuleByID(id graph.LogicalModuleID) (ModuleByIDResult, bool) {
	if v == nil || v.model == nil {
		return ModuleByIDResult{}, false
	}
	g, err := v.graph()
	if err != nil {
		return ModuleByIDResult{}, false
	}
	mod, ok := g.LogicalModule(id)
	if !ok {
		return ModuleByIDResult{}, false
	}
	summary, ok := v.model.Module(mod.Path)
	if !ok {
		return ModuleByIDResult{}, false
	}
	materializations := g.ModuleMaterializations(id)
	artifacts := make([]graph.Artifact, 0)
	for _, mat := range materializations {
		artifacts = append(artifacts, g.MaterializationArtifacts(mat.ID)...)
	}
	return ModuleByIDResult{
		Module:           mod,
		Summary:          summary,
		Variants:         append([]project.SemanticVariantSummary(nil), summary.Variants...),
		Materializations: materializations,
		Actions:          g.ActionsForModule(id),
		Artifacts:        dedupeArtifactsByID(artifacts),
	}, true
}

func (v *ModelView) VariantByID(id graph.VariantID) (VariantByIDResult, bool) {
	if v == nil || v.model == nil {
		return VariantByIDResult{}, false
	}
	g, err := v.graph()
	if err != nil {
		return VariantByIDResult{}, false
	}
	variant, ok := g.Variant(id)
	if !ok {
		return VariantByIDResult{}, false
	}
	mod, ok := g.LogicalModule(variant.ModuleID)
	if !ok {
		return VariantByIDResult{}, false
	}
	summary, ok := v.model.Variant(mod.Path, variant.Name)
	if !ok {
		return VariantByIDResult{}, false
	}
	materializations := g.VariantMaterializations(id)
	artifacts := make([]graph.Artifact, 0)
	for _, mat := range materializations {
		artifacts = append(artifacts, g.MaterializationArtifacts(mat.ID)...)
	}
	return VariantByIDResult{
		Module:           mod,
		Variant:          variant,
		Summary:          summary,
		Materializations: materializations,
		Actions:          g.ActionsForVariant(id),
		Artifacts:        dedupeArtifactsByID(artifacts),
	}, true
}

func (v *ModelView) ActionByID(id graph.ActionID) (ActionByIDResult, bool) {
	if v == nil || v.model == nil {
		return ActionByIDResult{}, false
	}
	action, ok := v.Action(id)
	if !ok {
		return ActionByIDResult{}, false
	}
	modulePath, variantName := v.actionCoordinates(action)
	summary, _ := v.model.ActionSummary(id)
	dependencies, _ := v.ActionDependenciesResult(id)
	dependents, _ := v.ActionDependentsResult(id)
	return ActionByIDResult{
		Action:       action,
		ModulePath:   modulePath,
		VariantName:  variantName,
		Summary:      summary,
		Inputs:       v.ActionInputs(id),
		Outputs:      v.ActionOutputs(id),
		Dependencies: dependencies.Dependencies,
		Dependents:   dependents.Dependents,
	}, true
}

func (v *ModelView) PlannedActionPolicy(id graph.ActionID) (PlannedActionPolicyResult, bool) {
	if v == nil || v.model == nil {
		return PlannedActionPolicyResult{}, false
	}
	action, ok := v.Action(id)
	if !ok {
		return PlannedActionPolicyResult{}, false
	}
	summary, ok := v.model.ActionSummary(id)
	if !ok {
		return PlannedActionPolicyResult{}, false
	}
	modulePath, variantName := v.actionCoordinates(action)
	return PlannedActionPolicyResult{
		Action:      action,
		ModulePath:  modulePath,
		VariantName: variantName,
		Policy:      summary,
	}, true
}

func (v *ModelView) PlannedActionPolicies(modulePath string) (PlannedActionPoliciesResult, bool) {
	if v == nil || v.model == nil {
		return PlannedActionPoliciesResult{}, false
	}
	mod, ok := v.Module(modulePath)
	if !ok {
		return PlannedActionPoliciesResult{}, false
	}
	result := PlannedActionPoliciesResult{ModulePath: modulePath}
	policyMap := map[string]configmodel.ActionSummary{}
	for _, variant := range mod.Variants {
		policies := v.model.ActionSummariesForVariant(modulePath, variant.Name)
		if len(policies) == 0 {
			continue
		}
		result.VariantNames = append(result.VariantNames, variant.Name)
		result.Variants = append(result.Variants, PlannedActionPoliciesVariantResult{
			ModulePath:  modulePath,
			VariantName: variant.Name,
			VariantID:   variant.ID,
			Policies:    append([]configmodel.ActionSummary(nil), policies...),
		})
		for _, policy := range policies {
			policyMap[policy.ID] = policy
		}
	}
	result.VariantNames = uniqueStrings(result.VariantNames)
	result.Policies = sortedActionSummaries(policyMap)
	sort.Slice(result.Variants, func(i, j int) bool { return result.Variants[i].VariantName < result.Variants[j].VariantName })
	return result, len(result.Variants) > 0
}

func (v *ModelView) ArtifactByID(id graph.ArtifactID) (ArtifactByIDResult, bool) {
	if v == nil || v.model == nil {
		return ArtifactByIDResult{}, false
	}
	artifact, ok := v.Artifact(id)
	if !ok {
		return ArtifactByIDResult{}, false
	}
	summary, _ := v.model.ArtifactSummary(id)
	provenance, _ := v.model.ProvenanceSummaryByArtifact(id)
	result := ArtifactByIDResult{
		Artifact:           artifact,
		ModulePath:         provenance.ModulePath,
		VariantName:        provenance.VariantName,
		MaterializationID:  provenance.MaterializationID,
		ArtifactSnapshotID: provenance.ArtifactSnapshotID,
		Summary:            summary,
		Provenance:         provenance,
		ClasspathSnapshots: classpathSnapshotReferences(provenance.MaterializationID, provenance.ClasspathSnapshotIDs, provenance.SourceRoots),
		Consumers:          v.consumerActions(id),
	}
	if artifact.ProducedByActionID != "" {
		if producer, ok := v.Action(artifact.ProducedByActionID); ok {
			result.Producer = producer
		}
	}
	if artifact.MaterializationID != "" {
		result.SiblingArtifacts = materializationArtifactsExcluding(v.ArtifactsForMaterialization(artifact.MaterializationID), id)
	}
	return result, true
}

func (v *ModelView) MaterializationByID(id graph.MaterializationID) (MaterializationByIDResult, bool) {
	if v == nil || v.model == nil {
		return MaterializationByIDResult{}, false
	}
	mat, ok := v.Materialization(id)
	if !ok {
		return MaterializationByIDResult{}, false
	}
	provenance, _ := v.model.ProvenanceSummaryByMaterialization(id)
	return MaterializationByIDResult{
		Materialization:    mat,
		ModulePath:         provenance.ModulePath,
		VariantName:        provenance.VariantName,
		ArtifactSnapshotID: provenance.ArtifactSnapshotID,
		Provenance:         provenance,
		ClasspathSnapshots: classpathSnapshotReferencesFromGraphMaterialization(mat),
		Artifacts:          v.model.ArtifactSummariesByMaterialization(id),
		Actions:            v.model.ActionSummariesByIDs(provenance.ConsumingActionIDs),
	}, true
}

func (v *ModelView) MaterializationConsumers(id graph.MaterializationID) (MaterializationConsumersResult, bool) {
	if v == nil || v.model == nil {
		return MaterializationConsumersResult{}, false
	}
	mat, ok := v.Materialization(id)
	if !ok {
		return MaterializationConsumersResult{}, false
	}
	provenance, ok := v.model.ProvenanceSummaryByMaterialization(id)
	if !ok {
		return MaterializationConsumersResult{}, false
	}
	return MaterializationConsumersResult{
		MaterializationID:  mat.ID.String(),
		ModulePath:         provenance.ModulePath,
		VariantName:        provenance.VariantName,
		ArtifactSnapshotID: provenance.ArtifactSnapshotID,
		Provenance:         provenance,
		Actions:            v.model.ActionSummariesByIDs(provenance.ConsumingActionIDs),
		Artifacts:          v.model.ArtifactSummariesByMaterialization(id),
		ManifestPaths:      append([]string(nil), provenance.ManifestPaths...),
		ClasspathSnapshots: classpathSnapshotReferencesFromGraphMaterialization(mat),
	}, true
}

func (v *ModelView) Actions() []graph.Action {
	g, err := v.graph()
	if err != nil {
		return nil
	}
	return g.Actions()
}

func (v *ModelView) Action(id graph.ActionID) (graph.Action, bool) {
	if v == nil || v.model == nil {
		return graph.Action{}, false
	}
	return v.model.Action(id)
}

func (v *ModelView) ActionInputs(id graph.ActionID) []graph.Artifact {
	if v == nil || v.model == nil {
		return nil
	}
	return v.model.ActionInputs(id)
}

func (v *ModelView) ActionOutputs(id graph.ActionID) []graph.Artifact {
	if v == nil || v.model == nil {
		return nil
	}
	return v.model.ActionOutputs(id)
}

func (v *ModelView) ActionInputsResult(id graph.ActionID) (ActionInputsResult, bool) {
	action, ok := v.Action(id)
	if !ok {
		return ActionInputsResult{}, false
	}
	modulePath, variantName := v.actionCoordinates(action)
	return ActionInputsResult{
		Action:      action,
		ModulePath:  modulePath,
		VariantName: variantName,
		Inputs:      v.ActionInputs(id),
	}, true
}

func (v *ModelView) ActionOutputsResult(id graph.ActionID) (ActionOutputsResult, bool) {
	action, ok := v.Action(id)
	if !ok {
		return ActionOutputsResult{}, false
	}
	modulePath, variantName := v.actionCoordinates(action)
	return ActionOutputsResult{
		Action:      action,
		ModulePath:  modulePath,
		VariantName: variantName,
		Outputs:     v.ActionOutputs(id),
	}, true
}

func (v *ModelView) ActionDependenciesResult(id graph.ActionID) (ActionDependenciesResult, bool) {
	action, ok := v.Action(id)
	if !ok {
		return ActionDependenciesResult{}, false
	}
	g, err := v.graph()
	if err != nil {
		return ActionDependenciesResult{}, false
	}
	modulePath, variantName := v.actionCoordinates(action)
	dependencyIDs := make([]string, 0, len(g.ActionDependencies(id)))
	for _, depID := range g.ActionDependencies(id) {
		dependencyIDs = append(dependencyIDs, depID.String())
	}
	return ActionDependenciesResult{
		Action:       action,
		ModulePath:   modulePath,
		VariantName:  variantName,
		Dependencies: v.model.ActionSummariesByIDs(dependencyIDs),
	}, true
}

func (v *ModelView) ActionDependentsResult(id graph.ActionID) (ActionDependentsResult, bool) {
	action, ok := v.Action(id)
	if !ok {
		return ActionDependentsResult{}, false
	}
	g, err := v.graph()
	if err != nil {
		return ActionDependentsResult{}, false
	}
	modulePath, variantName := v.actionCoordinates(action)
	var dependentIDs []string
	for _, ref := range g.DependentsOf(action.Ref()) {
		if ref.Kind != graph.NodeKindAction {
			continue
		}
		dependentIDs = append(dependentIDs, ref.ID)
	}
	return ActionDependentsResult{
		Action:      action,
		ModulePath:  modulePath,
		VariantName: variantName,
		Dependents:  v.model.ActionSummariesByIDs(dependentIDs),
	}, true
}

func (v *ModelView) ActionsForModule(path string) []graph.Action {
	if v == nil || v.model == nil {
		return nil
	}
	return v.model.ActionsForModule(path)
}

func (v *ModelView) ArtifactSummariesForVariant(modulePath, variantName string) []configmodel.ArtifactSummary {
	if v == nil || v.model == nil {
		return nil
	}
	return v.model.ArtifactSummariesForVariant(modulePath, variantName)
}

func (v *ModelView) ArtifactSummariesForModule(path string) []configmodel.ArtifactSummary {
	if v == nil || v.model == nil {
		return nil
	}
	return v.model.ArtifactSummariesForModule(path)
}

func (v *ModelView) VariantMaterialization(modulePath, variantName string) (VariantMaterializationResult, bool) {
	if v == nil || v.model == nil {
		return VariantMaterializationResult{}, false
	}
	provenance, ok := v.model.ProvenanceSummaryForVariant(modulePath, variantName)
	if !ok {
		return VariantMaterializationResult{}, false
	}
	return VariantMaterializationResult{
		ModulePath:      modulePath,
		VariantName:     variantName,
		VariantID:       provenance.VariantID,
		Materialization: provenance,
		Actions:         v.model.ActionSummariesForVariant(modulePath, variantName),
		Artifacts:       v.model.ArtifactSummariesForVariant(modulePath, variantName),
	}, true
}

func (v *ModelView) MaterializationProvenance(id graph.MaterializationID) (MaterializationProvenanceResult, bool) {
	if v == nil || v.model == nil {
		return MaterializationProvenanceResult{}, false
	}
	mat, ok := v.Materialization(id)
	if !ok {
		return MaterializationProvenanceResult{}, false
	}
	provenance, ok := v.model.ProvenanceSummaryByMaterialization(id)
	if !ok {
		return MaterializationProvenanceResult{}, false
	}
	return MaterializationProvenanceResult{
		Materialization:    mat,
		ModulePath:         provenance.ModulePath,
		VariantName:        provenance.VariantName,
		ArtifactSnapshotID: provenance.ArtifactSnapshotID,
		Provenance:         provenance,
		Actions:            v.model.ActionSummariesByIDs(provenance.ConsumingActionIDs),
		Artifacts:          v.model.ArtifactSummariesByMaterialization(id),
	}, true
}

func (v *ModelView) VariantManifest(modulePath, variantName string) (VariantManifestResult, bool) {
	if v == nil || v.model == nil {
		return VariantManifestResult{}, false
	}
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return VariantManifestResult{}, false
	}
	provenance, ok := v.model.ProvenanceSummaryForVariant(modulePath, variantName)
	if !ok {
		return VariantManifestResult{}, false
	}
	classpathSnapshots := classpathSnapshotReferencesFromSummary(variant.Materialization)
	producedArtifacts := v.model.ArtifactSummariesByIDs(provenance.ProducedArtifactIDs)
	actions := v.model.ActionSummariesByIDs(provenance.ConsumingActionIDs)
	var backingArtifact *configmodel.ArtifactSummary
	if backingID := strings.TrimSpace(provenance.BackingArtifactID); backingID != "" {
		if artifact, ok := v.model.ArtifactSummary(graph.ArtifactID(backingID)); ok {
			backingArtifact = &artifact
		}
	}
	return VariantManifestResult{
		ModulePath:           modulePath,
		VariantName:          variantName,
		VariantID:            variant.ID,
		MaterializationID:    provenance.MaterializationID,
		ArtifactSnapshotID:   provenance.ArtifactSnapshotID,
		SourceRoots:          append([]string(nil), provenance.SourceRoots...),
		ManifestPaths:        append([]string(nil), provenance.ManifestPaths...),
		ClasspathSnapshotIDs: append([]string(nil), provenance.ClasspathSnapshotIDs...),
		ClasspathSnapshots:   classpathSnapshots,
		ActionIDs:            append([]string(nil), provenance.ConsumingActionIDs...),
		Actions:              actions,
		ProducedArtifactIDs:  append([]string(nil), provenance.ProducedArtifactIDs...),
		ProducedArtifacts:    producedArtifacts,
		BackingArtifactID:    provenance.BackingArtifactID,
		BackingArtifact:      backingArtifact,
		Materialization:      variant.Materialization,
		Provenance:           provenance,
	}, true
}

func (v *ModelView) VariantSourceSetModel(modulePath, variantName string) (VariantSourceSetModelResult, bool) {
	if v == nil || v.model == nil {
		return VariantSourceSetModelResult{}, false
	}
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return VariantSourceSetModelResult{}, false
	}
	provenance, ok := v.model.ProvenanceSummaryForVariant(modulePath, variantName)
	if !ok {
		return VariantSourceSetModelResult{}, false
	}
	return VariantSourceSetModelResult{
		ModulePath:           modulePath,
		VariantName:          variantName,
		VariantID:            variant.ID,
		DisplayName:          variant.DisplayName,
		CoordinateName:       variant.CoordinateName,
		BuildType:            variant.BuildType,
		Flavors:              append([]string(nil), variant.Flavors...),
		SourceSetOrder:       append([]string(nil), variant.SourceSetOrder...),
		SourceSetNames:       append([]string(nil), variant.SourceSetNames...),
		SourceRoots:          append([]string(nil), provenance.SourceRoots...),
		ManifestPaths:        append([]string(nil), provenance.ManifestPaths...),
		ClasspathSnapshotIDs: append([]string(nil), provenance.ClasspathSnapshotIDs...),
		ClasspathSnapshots:   classpathSnapshotReferencesFromSummary(variant.Materialization),
		Materialization:      variant.Materialization,
		Provenance:           provenance,
	}, true
}

func (v *ModelView) DependencyBindingsForVariant(modulePath, variantName string) (DependencyBindingsForVariantResult, bool) {
	if v == nil || v.model == nil {
		return DependencyBindingsForVariantResult{}, false
	}
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return DependencyBindingsForVariantResult{}, false
	}
	return DependencyBindingsForVariantResult{
		ModulePath:   modulePath,
		VariantName:  variantName,
		VariantID:    variant.ID,
		BuildType:    variant.BuildType,
		Flavors:      append([]string(nil), variant.Flavors...),
		Dependencies: append([]project.SemanticDependencyProvenance(nil), variant.DependencyProvenance...),
	}, true
}

func (v *ModelView) DependencyBindingsForModule(modulePath string) (DependencyBindingsForModuleResult, bool) {
	if v == nil || v.model == nil {
		return DependencyBindingsForModuleResult{}, false
	}
	mod, ok := v.Module(modulePath)
	if !ok {
		return DependencyBindingsForModuleResult{}, false
	}
	result := DependencyBindingsForModuleResult{ModulePath: modulePath}
	for _, variant := range mod.Variants {
		result.Variants = append(result.Variants, DependencyBindingsForVariantResult{
			ModulePath:   modulePath,
			VariantName:  variant.Name,
			VariantID:    variant.ID,
			BuildType:    variant.BuildType,
			Flavors:      append([]string(nil), variant.Flavors...),
			Dependencies: append([]project.SemanticDependencyProvenance(nil), variant.DependencyProvenance...),
		})
	}
	sort.Slice(result.Variants, func(i, j int) bool { return result.Variants[i].VariantName < result.Variants[j].VariantName })
	return result, true
}

func (v *ModelView) DependencyRealizationsForVariant(modulePath, variantName string) (DependencyRealizationsForVariantResult, bool) {
	if v == nil || v.model == nil {
		return DependencyRealizationsForVariantResult{}, false
	}
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return DependencyRealizationsForVariantResult{}, false
	}
	result := DependencyRealizationsForVariantResult{
		ModulePath:  modulePath,
		VariantName: variantName,
		VariantID:   variant.ID,
		BuildType:   variant.BuildType,
		Flavors:     append([]string(nil), variant.Flavors...),
	}
	for _, dep := range variant.DependencyProvenance {
		item := DependencyRealization{
			ModulePath:        dep.ModulePath,
			VariantName:       dep.VariantName,
			DependencyLevel:   dep.DependencyLevel,
			RealizationKind:   dep.RealizationKind,
			LogicalModuleKind: dep.LogicalModuleKind,
			SelectionReason:   "semantic dependency provenance",
		}
		if strings.TrimSpace(dep.DependencyLevel) != "" {
			item.SelectionReasons = []string{"dependency level: " + dep.DependencyLevel}
		}
		if depVariant, ok := v.Variant(dep.ModulePath, dep.VariantName); ok {
			item.ModuleID = moduleIDForPath(v.model, dep.ModulePath)
			item.VariantID = depVariant.ID
			item.Mode = depVariant.Materialization.Mode
			item.MaterializationID = depVariant.Materialization.ID
			item.ArtifactSnapshotID = depVariant.Materialization.ArtifactSnapshotID
			item.ClasspathSnapshotIDs = append([]string(nil), depVariant.Materialization.ClasspathSnapshotIDs...)
			item.SourceRoots = append([]string(nil), depVariant.Materialization.SourceRoots...)
			item.BackingArtifactID = depVariant.Materialization.BackingArtifactID
			item.BackingArtifactPath = depVariant.Materialization.BackingArtifactPath
			item.ProducedArtifactPaths = append([]string(nil), depVariant.Materialization.ProducedArtifactPaths...)
			item.ProducedArtifactKinds = append([]string(nil), depVariant.Materialization.ProducedArtifactKinds...)
			item.ProducedArtifactIDs = append([]string(nil), depVariant.Materialization.ProducedArtifactIDs...)
			item.ProducedArtifacts = artifactSummariesFromSemantic(depVariant.Materialization.Artifacts)
			if provenance, ok := v.model.ProvenanceSummaryForVariant(dep.ModulePath, dep.VariantName); ok {
				if item.MaterializationID == "" {
					item.MaterializationID = provenance.MaterializationID
				}
				if item.ArtifactSnapshotID == "" {
					item.ArtifactSnapshotID = provenance.ArtifactSnapshotID
				}
				if len(item.ClasspathSnapshotIDs) == 0 {
					item.ClasspathSnapshotIDs = append([]string(nil), provenance.ClasspathSnapshotIDs...)
				}
				if len(item.SourceRoots) == 0 {
					item.SourceRoots = append([]string(nil), provenance.SourceRoots...)
				}
				if item.BackingArtifactID == "" {
					item.BackingArtifactID = provenance.BackingArtifactID
				}
				if len(item.ProducedArtifactIDs) == 0 {
					item.ProducedArtifactIDs = append([]string(nil), provenance.ProducedArtifactIDs...)
				}
				item.ManifestPaths = append([]string(nil), provenance.ManifestPaths...)
			}
			if backingID := strings.TrimSpace(item.BackingArtifactID); backingID != "" {
				if backingArtifact, ok := v.model.ArtifactSummary(graph.ArtifactID(backingID)); ok {
					item.BackingArtifact = &backingArtifact
					if item.BackingArtifactPath == "" {
						item.BackingArtifactPath = backingArtifact.Path
					}
					if item.BackingArtifactKind == "" {
						item.BackingArtifactKind = backingArtifact.Kind
					}
				}
			}
			if len(item.ProducedArtifacts) == 0 && len(item.ProducedArtifactIDs) > 0 {
				item.ProducedArtifacts = v.model.ArtifactSummariesByIDs(item.ProducedArtifactIDs)
			}
			if len(item.ProducedArtifactPaths) == 0 && len(item.ProducedArtifacts) > 0 {
				for _, artifact := range item.ProducedArtifacts {
					if path := strings.TrimSpace(artifact.Path); path != "" {
						item.ProducedArtifactPaths = append(item.ProducedArtifactPaths, path)
					}
					if kind := strings.TrimSpace(artifact.Kind); kind != "" {
						item.ProducedArtifactKinds = append(item.ProducedArtifactKinds, kind)
					}
				}
			}
			item.ProducedArtifactPaths = uniqueStrings(item.ProducedArtifactPaths)
			item.ProducedArtifactKinds = uniqueStrings(item.ProducedArtifactKinds)
		}
		result.Dependencies = append(result.Dependencies, item)
	}
	return result, true
}

func (v *ModelView) DependencyRealizationsForModule(modulePath string) (DependencyRealizationsForModuleResult, bool) {
	if v == nil || v.model == nil {
		return DependencyRealizationsForModuleResult{}, false
	}
	mod, ok := v.Module(modulePath)
	if !ok {
		return DependencyRealizationsForModuleResult{}, false
	}
	result := DependencyRealizationsForModuleResult{ModulePath: modulePath}
	for _, variant := range mod.Variants {
		item, ok := v.DependencyRealizationsForVariant(modulePath, variant.Name)
		if !ok {
			continue
		}
		result.Variants = append(result.Variants, item)
	}
	sort.Slice(result.Variants, func(i, j int) bool { return result.Variants[i].VariantName < result.Variants[j].VariantName })
	return result, true
}

func (v *ModelView) ModuleManifest(modulePath string) (ModuleManifestResult, bool) {
	if v == nil || v.model == nil {
		return ModuleManifestResult{}, false
	}
	mod, ok := v.Module(modulePath)
	if !ok {
		return ModuleManifestResult{}, false
	}
	result := ModuleManifestResult{ModulePath: modulePath}
	actionMap := map[string]configmodel.ActionSummary{}
	artifactMap := map[string]configmodel.ArtifactSummary{}
	for _, variant := range mod.Variants {
		manifest, ok := v.VariantManifest(modulePath, variant.Name)
		if !ok {
			continue
		}
		result.Variants = append(result.Variants, manifest)
		result.VariantNames = append(result.VariantNames, manifest.VariantName)
		result.MaterializationIDs = append(result.MaterializationIDs, manifest.MaterializationID)
		result.ArtifactSnapshotIDs = append(result.ArtifactSnapshotIDs, manifest.ArtifactSnapshotID)
		result.ManifestPaths = append(result.ManifestPaths, manifest.ManifestPaths...)
		result.SourceRoots = append(result.SourceRoots, manifest.SourceRoots...)
		result.ClasspathSnapshotIDs = append(result.ClasspathSnapshotIDs, manifest.ClasspathSnapshotIDs...)
		result.ActionIDs = append(result.ActionIDs, manifest.ActionIDs...)
		result.ProducedArtifactIDs = append(result.ProducedArtifactIDs, manifest.ProducedArtifactIDs...)
		if backingID := strings.TrimSpace(manifest.BackingArtifactID); backingID != "" {
			result.BackingArtifactIDs = append(result.BackingArtifactIDs, backingID)
		}
		for _, action := range manifest.Actions {
			actionMap[action.ID] = action
		}
		for _, artifact := range manifest.ProducedArtifacts {
			artifactMap[artifact.ID] = artifact
		}
		if manifest.BackingArtifact != nil {
			artifactMap[manifest.BackingArtifact.ID] = *manifest.BackingArtifact
		}
	}
	result.VariantNames = uniqueStrings(result.VariantNames)
	result.MaterializationIDs = uniqueStrings(result.MaterializationIDs)
	result.ArtifactSnapshotIDs = uniqueStrings(result.ArtifactSnapshotIDs)
	result.ManifestPaths = uniqueStrings(result.ManifestPaths)
	result.SourceRoots = uniqueStrings(result.SourceRoots)
	result.ClasspathSnapshotIDs = uniqueStrings(result.ClasspathSnapshotIDs)
	result.ActionIDs = uniqueStrings(result.ActionIDs)
	result.ProducedArtifactIDs = uniqueStrings(result.ProducedArtifactIDs)
	result.BackingArtifactIDs = uniqueStrings(result.BackingArtifactIDs)
	result.Actions = sortedActionSummaries(actionMap)
	result.Artifacts = sortedArtifactSummaries(artifactMap)
	return result, true
}

func (v *ModelView) ArtifactSnapshotProvenance(snapshotID string) (ArtifactSnapshotProvenanceResult, bool) {
	if v == nil || v.model == nil {
		return ArtifactSnapshotProvenanceResult{}, false
	}
	variants := v.model.ProvenanceSummariesByArtifactSnapshot(snapshotID)
	if len(variants) == 0 {
		return ArtifactSnapshotProvenanceResult{}, false
	}
	manifestPaths := make([]string, 0, len(variants))
	for _, variant := range variants {
		manifestPaths = append(manifestPaths, variant.ManifestPaths...)
	}
	return ArtifactSnapshotProvenanceResult{
		ArtifactSnapshotID: snapshotID,
		Variants:           variants,
		Artifacts:          v.model.ArtifactSummariesByArtifactSnapshot(snapshotID),
		ManifestPaths:      uniqueStrings(manifestPaths),
	}, true
}

func (v *ModelView) ArtifactSnapshotConsumers(snapshotID string) (ArtifactSnapshotConsumersResult, bool) {
	if v == nil || v.model == nil {
		return ArtifactSnapshotConsumersResult{}, false
	}
	variants := v.model.ProvenanceSummariesByArtifactSnapshot(snapshotID)
	if len(variants) == 0 {
		return ArtifactSnapshotConsumersResult{}, false
	}
	manifestPaths := make([]string, 0, len(variants))
	actionIDs := make([]string, 0, len(variants))
	for _, variant := range variants {
		manifestPaths = append(manifestPaths, variant.ManifestPaths...)
		actionIDs = append(actionIDs, variant.ConsumingActionIDs...)
	}
	return ArtifactSnapshotConsumersResult{
		ArtifactSnapshotID: snapshotID,
		Variants:           variants,
		Actions:            v.model.ActionSummariesByIDs(actionIDs),
		Artifacts:          v.model.ArtifactSummariesByArtifactSnapshot(snapshotID),
		ManifestPaths:      uniqueStrings(manifestPaths),
	}, true
}

func (v *ModelView) ActionsForVariant(modulePath, variantName string) []graph.Action {
	g, err := v.graph()
	if err != nil {
		return nil
	}
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return nil
	}
	return g.ActionsForVariant(graph.VariantID(variant.ID))
}

func (v *ModelView) actionCoordinates(action graph.Action) (string, string) {
	if v == nil || v.model == nil {
		return "", ""
	}
	for _, mod := range v.model.GraphSummary().Modules {
		if mod.ID != action.ModuleID.String() {
			continue
		}
		for _, variant := range mod.Variants {
			if variant.ID == action.VariantID.String() {
				return mod.Path, variant.Name
			}
		}
		return mod.Path, ""
	}
	return "", ""
}

func (v *ModelView) Artifacts() []graph.Artifact {
	g, err := v.graph()
	if err != nil {
		return nil
	}
	return g.Artifacts()
}

func (v *ModelView) Artifact(id graph.ArtifactID) (graph.Artifact, bool) {
	g, err := v.graph()
	if err != nil {
		return graph.Artifact{}, false
	}
	return g.Artifact(id)
}

func (v *ModelView) Materialization(id graph.MaterializationID) (graph.Materialization, bool) {
	g, err := v.graph()
	if err != nil {
		return graph.Materialization{}, false
	}
	return g.Materialization(id)
}

func (v *ModelView) MaterializationsForVariant(modulePath, variantName string) []graph.Materialization {
	g, err := v.graph()
	if err != nil {
		return nil
	}
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return nil
	}
	return g.VariantMaterializations(graph.VariantID(variant.ID))
}

func (v *ModelView) ArtifactsForVariant(modulePath, variantName string) []graph.Artifact {
	g, err := v.graph()
	if err != nil {
		return nil
	}
	materials := v.MaterializationsForVariant(modulePath, variantName)
	if len(materials) == 0 {
		return nil
	}
	seen := map[graph.ArtifactID]struct{}{}
	var out []graph.Artifact
	for _, mat := range materials {
		for _, artifact := range g.MaterializationArtifacts(mat.ID) {
			if _, ok := seen[artifact.ID]; ok {
				continue
			}
			seen[artifact.ID] = struct{}{}
			out = append(out, artifact)
		}
	}
	return out
}

func (v *ModelView) ArtifactsForModule(path string) []graph.Artifact {
	mod, ok := v.Module(path)
	if !ok {
		return nil
	}
	seen := map[graph.ArtifactID]struct{}{}
	var out []graph.Artifact
	for _, variant := range mod.Variants {
		for _, artifact := range v.ArtifactsForVariant(path, variant.Name) {
			if _, ok := seen[artifact.ID]; ok {
				continue
			}
			seen[artifact.ID] = struct{}{}
			out = append(out, artifact)
		}
	}
	return out
}

func (v *ModelView) ArtifactsForMaterialization(id graph.MaterializationID) []graph.Artifact {
	g, err := v.graph()
	if err != nil {
		return nil
	}
	return g.MaterializationArtifacts(id)
}

func (v *ModelView) ProvenanceForVariant(modulePath, variantName string) (Provenance, bool) {
	mod, ok := v.Module(modulePath)
	if !ok {
		return Provenance{}, false
	}
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return Provenance{}, false
	}
	return Provenance{
		Module:             mod,
		Variant:            variant,
		Materialization:    variant.Materialization,
		ClasspathSnapshots: classpathSnapshotReferencesFromSummary(variant.Materialization),
		Actions:            v.ActionsForVariant(modulePath, variantName),
		Artifacts:          v.ArtifactsForVariant(modulePath, variantName),
	}, true
}

func (v *ModelView) ProvenanceForAction(id graph.ActionID) (Provenance, bool) {
	action, ok := v.Action(id)
	if !ok {
		return Provenance{}, false
	}
	g, err := v.graph()
	if err != nil {
		return Provenance{}, false
	}
	modNode, ok := g.LogicalModule(action.ModuleID)
	if !ok {
		return Provenance{}, false
	}
	variantNode, ok := g.Variant(action.VariantID)
	if !ok {
		return Provenance{}, false
	}
	mod, ok := v.Module(modNode.Path)
	if !ok {
		return Provenance{}, false
	}
	variant, ok := v.Variant(modNode.Path, variantNode.Name)
	if !ok {
		return Provenance{}, false
	}
	materialization := variant.Materialization
	if materializationID := action.Attributes["materialization"]; materializationID != "" {
		if mat, ok := g.Materialization(graph.MaterializationID(materializationID)); ok {
			materialization = materializationSummary(mat)
		}
	}
	provenance := Provenance{
		Module:             mod,
		Variant:            variant,
		Materialization:    materialization,
		ClasspathSnapshots: classpathSnapshotReferencesFromSummary(materialization),
		Action:             action,
		Inputs:             v.ActionInputs(id),
		Outputs:            v.ActionOutputs(id),
		Artifacts:          v.ArtifactsForVariant(modNode.Path, variantNode.Name),
		Actions:            v.ActionsForVariant(modNode.Path, variantNode.Name),
	}
	if actionSummary, ok := actionSummaryByID(variant, action.ID.String()); ok {
		provenance.CacheProbe = actionSummary.LastCacheProbe
	}
	return provenance, true
}

func (v *ModelView) ProvenanceForArtifact(id graph.ArtifactID) (Provenance, bool) {
	artifact, ok := v.Artifact(id)
	if !ok {
		return Provenance{}, false
	}
	g, err := v.graph()
	if err != nil {
		return Provenance{}, false
	}
	mat, ok := g.Materialization(artifact.MaterializationID)
	if !ok {
		return Provenance{}, false
	}
	modNode, ok := g.LogicalModule(mat.ModuleID)
	if !ok {
		return Provenance{}, false
	}
	variantNode, ok := g.Variant(mat.VariantID)
	if !ok {
		return Provenance{}, false
	}
	mod, ok := v.Module(modNode.Path)
	if !ok {
		return Provenance{}, false
	}
	variant, ok := v.Variant(modNode.Path, variantNode.Name)
	if !ok {
		return Provenance{}, false
	}
	provenance := Provenance{
		Module:             mod,
		Variant:            variant,
		Materialization:    materializationSummary(mat),
		ClasspathSnapshots: classpathSnapshotReferencesFromGraphMaterialization(mat),
		Artifacts:          v.ArtifactsForVariant(modNode.Path, variantNode.Name),
		Actions:            v.ActionsForVariant(modNode.Path, variantNode.Name),
		Consumers:          v.consumerActions(id),
	}
	if producer, ok := g.Action(artifact.ProducedByActionID); ok {
		provenance.Producer = producer
		if actionSummary, ok := actionSummaryByID(variant, producer.ID.String()); ok {
			provenance.CacheProbe = actionSummary.LastCacheProbe
		}
	}
	return provenance, true
}

func (v *ModelView) ClasspathProvenanceForVariant(modulePath, variantName string) (ClasspathProvenanceResult, bool) {
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return ClasspathProvenanceResult{}, false
	}
	return ClasspathProvenanceResult{
		ModulePath:         modulePath,
		VariantName:        variantName,
		MaterializationID:  variant.Materialization.ID,
		ArtifactSnapshotID: variant.Materialization.ArtifactSnapshotID,
		SourceRoots:        append([]string(nil), variant.Materialization.SourceRoots...),
		ClasspathSnapshots: classpathSnapshotReferencesFromSummary(variant.Materialization),
		Artifacts:          v.ArtifactsForVariant(modulePath, variantName),
		Actions:            v.ActionsForVariant(modulePath, variantName),
	}, true
}

func (v *ModelView) ClasspathSnapshot(modulePath, variantName string) (ClasspathSnapshotResult, bool) {
	record, variant, ok := v.classpathRecordForVariant(modulePath, variantName)
	if !ok {
		return ClasspathSnapshotResult{}, false
	}
	return ClasspathSnapshotResult{
		ModulePath:         modulePath,
		VariantName:        variantName,
		MaterializationID:  variant.Materialization.ID,
		ArtifactSnapshotID: variant.Materialization.ArtifactSnapshotID,
		Snapshot:           record,
	}, true
}

func (v *ModelView) ClasspathSnapshotByID(snapshotID string) (ClasspathSnapshotByIDResult, bool) {
	lookupID, canonicalID, result, ok := v.resolveClasspathSnapshotByID(snapshotID)
	if !ok {
		return ClasspathSnapshotByIDResult{}, false
	}
	return ClasspathSnapshotByIDResult{
		LookupID:    lookupID,
		CanonicalID: canonicalID,
		Result:      result,
	}, true
}

func (v *ModelView) ClasspathSnapshotProvenance(snapshotID string) (ClasspathSnapshotProvenanceResult, bool) {
	if v == nil || v.model == nil || strings.TrimSpace(snapshotID) == "" {
		return ClasspathSnapshotProvenanceResult{}, false
	}
	variants := v.model.ProvenanceSummariesByClasspathSnapshot(snapshotID)
	if len(variants) == 0 {
		return ClasspathSnapshotProvenanceResult{}, false
	}
	artifactIDs := map[string]struct{}{}
	manifestPaths := make([]string, 0, len(variants))
	for _, variant := range variants {
		manifestPaths = append(manifestPaths, variant.ManifestPaths...)
		if strings.TrimSpace(variant.BackingArtifactID) != "" {
			artifactIDs[variant.BackingArtifactID] = struct{}{}
		}
		for _, artifactID := range variant.ProducedArtifactIDs {
			if strings.TrimSpace(artifactID) != "" {
				artifactIDs[artifactID] = struct{}{}
			}
		}
	}
	ids := sortedStringSet(artifactIDs)
	return ClasspathSnapshotProvenanceResult{
		ClasspathSnapshotID: snapshotID,
		Variants:            append([]configmodel.ProvenanceSummary(nil), variants...),
		Artifacts:           append([]configmodel.ArtifactSummary(nil), v.model.ArtifactSummariesByIDs(ids)...),
		ManifestPaths:       uniqueStrings(manifestPaths),
	}, true
}

func (v *ModelView) ClasspathSnapshotConsumers(snapshotID string) (ClasspathSnapshotConsumersResult, bool) {
	if v == nil || v.model == nil || strings.TrimSpace(snapshotID) == "" {
		return ClasspathSnapshotConsumersResult{}, false
	}
	variants := v.model.ProvenanceSummariesByClasspathSnapshot(snapshotID)
	if len(variants) == 0 {
		return ClasspathSnapshotConsumersResult{}, false
	}
	actionIDs := map[string]struct{}{}
	artifactIDs := map[string]struct{}{}
	manifestPaths := make([]string, 0, len(variants))
	for _, variant := range variants {
		manifestPaths = append(manifestPaths, variant.ManifestPaths...)
		for _, actionID := range variant.ConsumingActionIDs {
			if strings.TrimSpace(actionID) != "" {
				actionIDs[actionID] = struct{}{}
			}
		}
		if strings.TrimSpace(variant.BackingArtifactID) != "" {
			artifactIDs[variant.BackingArtifactID] = struct{}{}
		}
		for _, artifactID := range variant.ProducedArtifactIDs {
			if strings.TrimSpace(artifactID) != "" {
				artifactIDs[artifactID] = struct{}{}
			}
		}
	}
	actionIDList := sortedStringSet(actionIDs)
	artifactIDList := sortedStringSet(artifactIDs)
	return ClasspathSnapshotConsumersResult{
		ClasspathSnapshotID: snapshotID,
		Variants:            append([]configmodel.ProvenanceSummary(nil), variants...),
		Actions:             append([]configmodel.ActionSummary(nil), v.model.ActionSummariesByIDs(actionIDList)...),
		Artifacts:           append([]configmodel.ArtifactSummary(nil), v.model.ArtifactSummariesByIDs(artifactIDList)...),
		ManifestPaths:       uniqueStrings(manifestPaths),
	}, true
}

func (v *ModelView) ClasspathSnapshotConsumersByID(snapshotID string) (ClasspathSnapshotConsumersByIDResult, bool) {
	lookupID, canonicalID, _, ok := v.resolveClasspathSnapshotByID(snapshotID)
	if !ok {
		return ClasspathSnapshotConsumersByIDResult{}, false
	}
	consumers, ok := v.ClasspathSnapshotConsumers(canonicalID)
	if !ok {
		return ClasspathSnapshotConsumersByIDResult{}, false
	}
	return ClasspathSnapshotConsumersByIDResult{
		LookupID:    lookupID,
		CanonicalID: canonicalID,
		Consumers:   consumers,
	}, true
}

func (v *ModelView) ClasspathEntryLookup(modulePath, variantName, path string) (ClasspathEntryLookupResult, bool) {
	record, variant, ok := v.classpathRecordForVariant(modulePath, variantName)
	if !ok {
		return ClasspathEntryLookupResult{}, false
	}
	entry, ok := record.Lookup(path)
	if !ok {
		return ClasspathEntryLookupResult{}, false
	}
	var decisions []classpath.NormalizationDecision
	for _, decision := range record.Decisions {
		if decision.OutputPath == entry.NormalizedPath || decision.InputPath == entry.Path {
			decisions = append(decisions, decision)
		}
	}
	return ClasspathEntryLookupResult{
		ModulePath:         modulePath,
		VariantName:        variantName,
		MaterializationID:  variant.Materialization.ID,
		ArtifactSnapshotID: variant.Materialization.ArtifactSnapshotID,
		Path:               path,
		Entry:              entry,
		Decisions:          decisions,
	}, true
}

func (v *ModelView) ArtifactOnClasspath(modulePath, variantName string, artifactID graph.ArtifactID) (ArtifactOnClasspathResult, bool) {
	record, variant, ok := v.classpathRecordForVariant(modulePath, variantName)
	if !ok {
		return ArtifactOnClasspathResult{}, false
	}
	var target graph.Artifact
	found := false
	for _, artifact := range v.ArtifactsForVariant(modulePath, variantName) {
		if artifact.ID == artifactID {
			target = artifact
			found = true
			break
		}
	}
	if !found {
		return ArtifactOnClasspathResult{}, false
	}
	result := ArtifactOnClasspathResult{
		ModulePath:         modulePath,
		VariantName:        variantName,
		MaterializationID:  variant.Materialization.ID,
		ArtifactSnapshotID: variant.Materialization.ArtifactSnapshotID,
		Artifact:           target,
	}
	for _, entry := range record.Entries {
		if entry.ArtifactID == target.ID.String() || (target.Path != "" && entry.Path == target.Path) || (target.Path != "" && entry.NormalizedPath == target.Path) {
			result.Present = true
			result.Entry = entry
			break
		}
	}
	return result, true
}

func (v *ModelView) ArtifactClasspathConsumers(id graph.ArtifactID) (ArtifactClasspathConsumersResult, bool) {
	artifact, ok := v.Artifact(id)
	if !ok {
		return ArtifactClasspathConsumersResult{}, false
	}
	result := ArtifactClasspathConsumersResult{Artifact: artifact}
	for _, mod := range v.Modules() {
		for _, variant := range mod.Variants {
			lookup, ok := v.ArtifactOnClasspath(mod.Path, variant.Name, id)
			if !ok || !lookup.Present {
				continue
			}
			result.Consumers = append(result.Consumers, lookup)
		}
	}
	sort.Slice(result.Consumers, func(i, j int) bool {
		if result.Consumers[i].ModulePath != result.Consumers[j].ModulePath {
			return result.Consumers[i].ModulePath < result.Consumers[j].ModulePath
		}
		return result.Consumers[i].VariantName < result.Consumers[j].VariantName
	})
	return result, len(result.Consumers) > 0
}

func (v *ModelView) ClasspathPathConsumers(path string) (ClasspathPathConsumersResult, bool) {
	result := ClasspathPathConsumersResult{Path: path}
	target := strings.TrimSpace(path)
	if target == "" || v == nil || v.model == nil {
		return result, false
	}
	for _, mod := range v.Modules() {
		for _, variant := range mod.Variants {
			lookup, ok := v.ClasspathEntryLookup(mod.Path, variant.Name, target)
			if !ok {
				continue
			}
			result.Consumers = append(result.Consumers, lookup)
		}
	}
	sort.Slice(result.Consumers, func(i, j int) bool {
		if result.Consumers[i].ModulePath != result.Consumers[j].ModulePath {
			return result.Consumers[i].ModulePath < result.Consumers[j].ModulePath
		}
		return result.Consumers[i].VariantName < result.Consumers[j].VariantName
	})
	return result, len(result.Consumers) > 0
}

func (v *ModelView) FileOwners(path string) FileOwnersResult {
	result := FileOwnersResult{Path: path}
	target := strings.TrimSpace(path)
	if target == "" || v == nil || v.model == nil {
		return result
	}
	for _, mod := range v.Modules() {
		for _, variant := range mod.Variants {
			manifest, _ := v.VariantManifest(mod.Path, variant.Name)
			var owners []string
			for _, root := range variant.Materialization.SourceRoots {
				if pathWithinRoot(target, root) {
					owners = append(owners, root)
				}
			}
			if len(owners) > 0 {
				result.Owners = append(result.Owners, FileOwner{
					ModulePath:  mod.Path,
					VariantName: variant.Name,
					Kind:        "source-root",
					Paths:       uniqueSortedStrings(owners),
				})
			}
			owners = nil
			for _, manifestPath := range manifest.ManifestPaths {
				if samePath(target, manifestPath) {
					owners = append(owners, manifestPath)
				}
			}
			if len(owners) > 0 {
				result.Owners = append(result.Owners, FileOwner{
					ModulePath:  mod.Path,
					VariantName: variant.Name,
					Kind:        "manifest",
					Paths:       uniqueSortedStrings(owners),
				})
			}
			owners = nil
			for _, artifact := range variant.Materialization.Artifacts {
				if samePath(target, artifact.Path) {
					owners = append(owners, artifact.Path)
				}
			}
			if len(owners) > 0 {
				result.Owners = append(result.Owners, FileOwner{
					ModulePath:  mod.Path,
					VariantName: variant.Name,
					Kind:        "artifact",
					Paths:       uniqueSortedStrings(owners),
				})
			}
		}
	}
	return result
}

func (v *ModelView) ArtifactProvenance(id graph.ArtifactID) (ArtifactProvenanceResult, bool) {
	artifact, ok := v.Artifact(id)
	if !ok {
		return ArtifactProvenanceResult{}, false
	}
	g, err := v.graph()
	if err != nil {
		return ArtifactProvenanceResult{}, false
	}
	mat, ok := g.Materialization(artifact.MaterializationID)
	if !ok {
		return ArtifactProvenanceResult{}, false
	}
	modNode, ok := g.LogicalModule(mat.ModuleID)
	if !ok {
		return ArtifactProvenanceResult{}, false
	}
	variantNode, ok := g.Variant(mat.VariantID)
	if !ok {
		return ArtifactProvenanceResult{}, false
	}
	siblings := materializationArtifactsExcluding(g.MaterializationArtifacts(mat.ID), artifact.ID)
	result := ArtifactProvenanceResult{
		Artifact:           artifact,
		ModulePath:         modNode.Path,
		VariantName:        variantNode.Name,
		MaterializationID:  mat.ID.String(),
		ArtifactSnapshotID: mat.ArtifactSnapshotID,
		ClasspathSnapshots: classpathSnapshotReferencesFromGraphMaterialization(mat),
		Consumers:          v.consumerActions(id),
		SiblingArtifacts:   siblings,
	}
	if producer, ok := g.Action(artifact.ProducedByActionID); ok {
		result.Producer = producer
	}
	return result, true
}

func (v *ModelView) ArtifactConsumers(id graph.ArtifactID) (ArtifactConsumersResult, bool) {
	result, ok := v.ArtifactProvenance(id)
	if !ok {
		return ArtifactConsumersResult{}, false
	}
	return ArtifactConsumersResult{
		Artifact:           result.Artifact,
		ModulePath:         result.ModulePath,
		VariantName:        result.VariantName,
		MaterializationID:  result.MaterializationID,
		ArtifactSnapshotID: result.ArtifactSnapshotID,
		ClasspathSnapshots: append([]materialization.ClasspathSnapshotReference(nil), result.ClasspathSnapshots...),
		Producer:           result.Producer,
		Consumers:          append([]graph.Action(nil), result.Consumers...),
		SiblingArtifacts:   append([]graph.Artifact(nil), result.SiblingArtifacts...),
	}, true
}

func actionSummaryByID(variant project.SemanticVariantSummary, actionID string) (project.SemanticActionSummary, bool) {
	for _, action := range variant.Actions {
		if action.ID == actionID {
			return action, true
		}
	}
	return project.SemanticActionSummary{}, false
}

func (v *ModelView) consumerActions(id graph.ArtifactID) []graph.Action {
	g, err := v.graph()
	if err != nil {
		return nil
	}
	return g.ActionsConsumingArtifact(id)
}

func (v *ModelView) classpathRecordForVariant(modulePath, variantName string) (classpath.Record, project.SemanticVariantSummary, bool) {
	mod, ok := v.Module(modulePath)
	if !ok {
		return classpath.Record{}, project.SemanticVariantSummary{}, false
	}
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return classpath.Record{}, project.SemanticVariantSummary{}, false
	}
	artifacts := v.ArtifactsForVariant(modulePath, variantName)
	entries := make([]classpath.Entry, 0, len(variant.Materialization.SourceRoots)+len(artifacts))
	for _, root := range variant.Materialization.SourceRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		entries = append(entries, classpath.Entry{
			Path:            root,
			NormalizedPath:  root,
			Origin:          classpath.OriginSource,
			ModuleID:        mod.ID,
			VariantID:       variant.ID,
			SelectionReason: "semantic source root",
			Provenance: materialization.Provenance{
				Producer: "integration.ModelView",
				Subject:  variant.Materialization.ID,
				Reasons:  []string{"variant source root"},
			},
		})
	}
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Path) == "" {
			continue
		}
		entries = append(entries, classpath.Entry{
			Path:            artifact.Path,
			NormalizedPath:  artifact.Path,
			Origin:          classpath.OriginArtifact,
			ArtifactID:      artifact.ID.String(),
			ModuleID:        mod.ID,
			VariantID:       variant.ID,
			FamilyKey:       string(artifact.Kind),
			SelectionReason: "semantic produced artifact",
			Provenance: materialization.Provenance{
				Producer: artifact.ProducedByActionID.String(),
				Subject:  artifact.ID.String(),
				Reasons:  []string{"variant artifact"},
			},
		})
	}
	record := classpath.Normalize(classpath.ScopeCompile, mod.ID, variant.ID, "integration", entries, materialization.Provenance{
		Producer: "integration.ModelView",
		Subject:  variant.Materialization.ID,
		Reasons:  []string{"variant classpath snapshot"},
	}).Record()
	if snapshotIDs := variant.Materialization.ClasspathSnapshotIDs; len(snapshotIDs) > 0 && strings.TrimSpace(snapshotIDs[0]) != "" {
		record.ID = snapshotIDs[0]
	}
	return record, variant, true
}

func (v *ModelView) graph() (*graph.Graph, error) {
	if v == nil {
		return graph.New(), nil
	}
	v.graphOnce.Do(func() {
		if v.model == nil {
			v.cachedGraph = graph.New()
			return
		}
		v.cachedGraph, v.graphErr = v.model.Graph()
	})
	if v.graphErr != nil {
		return nil, v.graphErr
	}
	if v.cachedGraph == nil {
		return graph.New(), nil
	}
	return v.cachedGraph, nil
}

func materializationSummary(mat graph.Materialization) project.SemanticMaterializationSummary {
	mode := ""
	switch mat.Kind {
	case graph.MaterializationKindSourceBacked:
		mode = string(materialization.ModeSourceBacked)
	case graph.MaterializationKindArtifactBacked:
		mode = string(materialization.ModeArtifactBacked)
	}
	return project.SemanticMaterializationSummary{
		ID:                   mat.ID.String(),
		Mode:                 mode,
		ArtifactSnapshotID:   mat.ArtifactSnapshotID,
		ClasspathSnapshotIDs: append([]string(nil), mat.ClasspathSnapshotIDs...),
		SourceRoots:          append([]string(nil), mat.SourceRoots...),
	}
}

func materializationArtifactsExcluding(artifacts []graph.Artifact, exclude graph.ArtifactID) []graph.Artifact {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]graph.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.ID == exclude {
			continue
		}
		out = append(out, artifact)
	}
	return out
}

func (v *ModelView) ClasspathSnapshotsForVariant(modulePath, variantName string) []materialization.ClasspathSnapshotReference {
	variant, ok := v.Variant(modulePath, variantName)
	if !ok {
		return nil
	}
	return classpathSnapshotReferencesFromSummary(variant.Materialization)
}

func classpathSnapshotReferencesFromSummary(summary project.SemanticMaterializationSummary) []materialization.ClasspathSnapshotReference {
	return classpathSnapshotReferences(summary.ID, summary.ClasspathSnapshotIDs, summary.SourceRoots)
}

func classpathSnapshotReferencesFromGraphMaterialization(mat graph.Materialization) []materialization.ClasspathSnapshotReference {
	return classpathSnapshotReferences(mat.ID.String(), mat.ClasspathSnapshotIDs, mat.SourceRoots)
}

func classpathSnapshotReferences(materializationID string, ids []string, sourceRoots []string) []materialization.ClasspathSnapshotReference {
	if len(ids) == 0 {
		return nil
	}
	refs := make([]materialization.ClasspathSnapshotReference, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, materialization.ClasspathSnapshotReference{ID: id})
	}
	if len(ids) == 1 && len(sourceRoots) > 0 {
		entries := make([]classpath.Entry, 0, len(sourceRoots))
		for _, root := range sourceRoots {
			entries = append(entries, classpath.Entry{
				Path:            root,
				NormalizedPath:  root,
				Origin:          classpath.OriginSource,
				SelectionReason: "integration materialization source root",
			})
		}
		record := classpath.Normalize(classpath.ScopeCompile, "", "", "integration", entries, materialization.Provenance{
			Producer: "integration.ModelView",
			Subject:  materializationID,
			Reasons:  []string{"materialization classpath reference"},
		}).Record()
		refs[0].NormalizedID = record.NormalizedID
		refs[0].OrderedEntriesID = record.OrderedEntriesID
		refs[0].EntriesDigest = record.EntriesDigest
		refs[0].EntryCount = len(record.Entries)
		refs[0].Entries = append([]string(nil), record.NormalizedEntries...)
	}
	return refs
}

func (v *ModelView) resolveClasspathSnapshotByID(snapshotID string) (string, string, ClasspathSnapshotResult, bool) {
	if v == nil || v.model == nil || strings.TrimSpace(snapshotID) == "" {
		return "", "", ClasspathSnapshotResult{}, false
	}
	lookupID := strings.TrimSpace(snapshotID)
	for _, mod := range v.Modules() {
		for _, variant := range mod.Variants {
			result, ok := v.ClasspathSnapshot(mod.Path, variant.Name)
			if !ok {
				continue
			}
			record := result.Snapshot
			switch lookupID {
			case record.ID, record.NormalizedID, record.OrderedEntriesID:
				return lookupID, record.ID, result, true
			}
			for _, ref := range classpathSnapshotReferencesFromSummary(variant.Materialization) {
				if lookupID == ref.ID || lookupID == ref.NormalizedID || lookupID == ref.OrderedEntriesID {
					return lookupID, record.ID, result, true
				}
			}
		}
	}
	return "", "", ClasspathSnapshotResult{}, false
}

func dedupeArtifactsByID(artifacts []graph.Artifact) []graph.Artifact {
	if len(artifacts) == 0 {
		return nil
	}
	seen := map[graph.ArtifactID]struct{}{}
	out := make([]graph.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if _, ok := seen[artifact.ID]; ok {
			continue
		}
		seen[artifact.ID] = struct{}{}
		out = append(out, artifact)
	}
	return out
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sortedStringSet(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func samePath(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.TrimSpace(b) != "" && a == b
}

func pathWithinRoot(path, root string) bool {
	path = strings.TrimSpace(path)
	root = strings.TrimSpace(root)
	if path == "" || root == "" {
		return false
	}
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func sortedActionSummaries(values map[string]configmodel.ActionSummary) []configmodel.ActionSummary {
	if len(values) == 0 {
		return nil
	}
	out := make([]configmodel.ActionSummary, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func moduleIDForPath(model *configmodel.Model, modulePath string) string {
	if model == nil {
		return ""
	}
	mod, ok := model.Module(modulePath)
	if !ok {
		return ""
	}
	return mod.ID
}

func sortedArtifactSummaries(values map[string]configmodel.ArtifactSummary) []configmodel.ArtifactSummary {
	if len(values) == 0 {
		return nil
	}
	out := make([]configmodel.ArtifactSummary, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func artifactSummariesFromSemantic(values []project.SemanticArtifactSummary) []configmodel.ArtifactSummary {
	if len(values) == 0 {
		return nil
	}
	out := make([]configmodel.ArtifactSummary, 0, len(values))
	for _, value := range values {
		out = append(out, configmodel.ArtifactSummary{
			ID:                 value.ID,
			Kind:               value.Kind,
			Path:               value.Path,
			ProducedByActionID: value.ProducedByActionID,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func uniqueSortedStrings(values []string) []string {
	out := uniqueStrings(values)
	sort.Strings(out)
	return out
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
