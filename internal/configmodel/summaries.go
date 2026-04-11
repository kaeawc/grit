package configmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/cachepolicy"
	"github.com/kaeawc/grit/internal/graph"
)

type ActionSummary struct {
	ID             string   `json:"id"`
	ModuleID       string   `json:"moduleId,omitempty"`
	ModulePath     string   `json:"modulePath,omitempty"`
	VariantID      string   `json:"variantId,omitempty"`
	VariantName    string   `json:"variantName,omitempty"`
	Name           string   `json:"name,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	Operation      string   `json:"operation,omitempty"`
	Inputs         []string `json:"inputs,omitempty"`
	Outputs        []string `json:"outputs,omitempty"`
	Note           string   `json:"note,omitempty"`
	CacheKey       string   `json:"cacheKey,omitempty"`
	WorkerClass    string   `json:"workerClass,omitempty"`
	ResourceClass  string   `json:"resourceClass,omitempty"`
	ResourceCost   int      `json:"resourceCost,omitempty"`
	MaxParallelism int      `json:"maxParallelism,omitempty"`
	RetentionClass string   `json:"retentionClass,omitempty"`
	Shareability   string   `json:"shareability,omitempty"`
}

type ArtifactSummary struct {
	ID                 string `json:"id"`
	ModuleID           string `json:"moduleId,omitempty"`
	ModulePath         string `json:"modulePath,omitempty"`
	VariantID          string `json:"variantId,omitempty"`
	VariantName        string `json:"variantName,omitempty"`
	MaterializationID  string `json:"materializationId,omitempty"`
	ProducedByActionID string `json:"producedByActionId,omitempty"`
	Kind               string `json:"kind,omitempty"`
	Path               string `json:"path,omitempty"`
	Digest             string `json:"digest,omitempty"`
	Note               string `json:"note,omitempty"`
	RetentionClass     string `json:"retentionClass,omitempty"`
	Shareability       string `json:"shareability,omitempty"`
}

type ProvenanceSummary struct {
	MaterializationID    string   `json:"materializationId"`
	ModuleID             string   `json:"moduleId,omitempty"`
	ModulePath           string   `json:"modulePath,omitempty"`
	VariantID            string   `json:"variantId,omitempty"`
	VariantName          string   `json:"variantName,omitempty"`
	Mode                 string   `json:"mode,omitempty"`
	ArtifactSnapshotID   string   `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs []string `json:"classpathSnapshotIds,omitempty"`
	SourceRoots          []string `json:"sourceRoots,omitempty"`
	ManifestPaths        []string `json:"manifestPaths,omitempty"`
	BackingArtifactID    string   `json:"backingArtifactId,omitempty"`
	ProducedArtifactIDs  []string `json:"producedArtifactIds,omitempty"`
	ConsumingActionIDs   []string `json:"consumingActionIds,omitempty"`
	Note                 string   `json:"note,omitempty"`
	RetentionClass       string   `json:"retentionClass,omitempty"`
	Shareability         string   `json:"shareability,omitempty"`
}

func (m *Model) ActionSummary(id graph.ActionID) (ActionSummary, bool) {
	for _, summary := range m.ActionSummaries {
		if summary.ID == id.String() {
			return summary, true
		}
	}
	return ActionSummary{}, false
}

func (m *Model) ArtifactSummary(id graph.ArtifactID) (ArtifactSummary, bool) {
	for _, summary := range m.ArtifactSummaries {
		if summary.ID == id.String() {
			return summary, true
		}
	}
	return ArtifactSummary{}, false
}

func (m *Model) ProvenanceSummaryByMaterialization(id graph.MaterializationID) (ProvenanceSummary, bool) {
	for _, summary := range m.ProvenanceSummaries {
		if summary.MaterializationID == id.String() {
			return summary, true
		}
	}
	return ProvenanceSummary{}, false
}

func (m *Model) ProvenanceSummaryForArtifact(id graph.ArtifactID) (ProvenanceSummary, bool) {
	artifact, ok := m.ArtifactSummary(id)
	if !ok || artifact.MaterializationID == "" {
		return ProvenanceSummary{}, false
	}
	return m.ProvenanceSummaryByMaterialization(graph.MaterializationID(artifact.MaterializationID))
}

func (m *Model) ProvenanceSummaryByArtifact(id graph.ArtifactID) (ProvenanceSummary, bool) {
	artifact, ok := m.ArtifactSummary(id)
	if !ok || artifact.MaterializationID == "" {
		return ProvenanceSummary{}, false
	}
	return m.ProvenanceSummaryByMaterialization(graph.MaterializationID(artifact.MaterializationID))
}

func (m *Model) SourceRootsForArtifact(id graph.ArtifactID) ([]string, bool) {
	summary, ok := m.ProvenanceSummaryByArtifact(id)
	if !ok {
		return nil, false
	}
	return append([]string(nil), summary.SourceRoots...), true
}

func (m *Model) ActionSummariesForModule(path string) []ActionSummary {
	var out []ActionSummary
	for _, summary := range m.ActionSummaries {
		if summary.ModulePath == path {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ArtifactSummariesForModule(path string) []ArtifactSummary {
	var out []ArtifactSummary
	for _, summary := range m.ArtifactSummaries {
		if summary.ModulePath == path {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ArtifactSummariesByMaterialization(id graph.MaterializationID) []ArtifactSummary {
	var out []ArtifactSummary
	for _, summary := range m.ArtifactSummaries {
		if summary.MaterializationID == id.String() {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ProvenanceSummariesForModule(path string) []ProvenanceSummary {
	var out []ProvenanceSummary
	for _, summary := range m.ProvenanceSummaries {
		if summary.ModulePath == path {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MaterializationID < out[j].MaterializationID })
	return out
}

func (m *Model) ActionSummariesForVariant(modulePath, variantName string) []ActionSummary {
	var out []ActionSummary
	for _, summary := range m.ActionSummaries {
		if summary.ModulePath == modulePath && summary.VariantName == variantName {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ArtifactSummariesForVariant(modulePath, variantName string) []ArtifactSummary {
	var out []ArtifactSummary
	for _, summary := range m.ArtifactSummaries {
		if summary.ModulePath == modulePath && summary.VariantName == variantName {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ActionSummariesByIDs(ids []string) []ActionSummary {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []ActionSummary
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		for _, summary := range m.ActionSummaries {
			if summary.ID != id {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, summary)
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ArtifactSummariesByIDs(ids []string) []ArtifactSummary {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []ArtifactSummary
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if summary, ok := m.ArtifactSummary(graph.ArtifactID(id)); ok {
			seen[id] = struct{}{}
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ProvenanceSummaryForVariant(modulePath, variantName string) (ProvenanceSummary, bool) {
	for _, summary := range m.ProvenanceSummaries {
		if summary.ModulePath == modulePath && summary.VariantName == variantName {
			return summary, true
		}
	}
	return ProvenanceSummary{}, false
}

func (m *Model) ProvenanceSummariesByArtifactSnapshot(snapshotID string) []ProvenanceSummary {
	var out []ProvenanceSummary
	for _, summary := range m.ProvenanceSummaries {
		if summary.ArtifactSnapshotID == snapshotID {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModulePath != out[j].ModulePath {
			return out[i].ModulePath < out[j].ModulePath
		}
		if out[i].VariantName != out[j].VariantName {
			return out[i].VariantName < out[j].VariantName
		}
		return out[i].MaterializationID < out[j].MaterializationID
	})
	return out
}

func (m *Model) ProvenanceSummariesByClasspathSnapshot(snapshotID string) []ProvenanceSummary {
	var out []ProvenanceSummary
	for _, summary := range m.ProvenanceSummaries {
		for _, candidate := range summary.ClasspathSnapshotIDs {
			if candidate == snapshotID {
				out = append(out, summary)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModulePath != out[j].ModulePath {
			return out[i].ModulePath < out[j].ModulePath
		}
		if out[i].VariantName != out[j].VariantName {
			return out[i].VariantName < out[j].VariantName
		}
		return out[i].MaterializationID < out[j].MaterializationID
	})
	return out
}

func (m *Model) ArtifactSummariesByArtifactSnapshot(snapshotID string) []ArtifactSummary {
	if strings.TrimSpace(snapshotID) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []ArtifactSummary
	for _, summary := range m.ProvenanceSummariesByArtifactSnapshot(snapshotID) {
		for _, artifactID := range summary.ProducedArtifactIDs {
			if _, ok := seen[artifactID]; ok {
				continue
			}
			artifact, ok := m.ArtifactSummary(graph.ArtifactID(artifactID))
			if !ok {
				continue
			}
			seen[artifactID] = struct{}{}
			out = append(out, artifact)
		}
		if backingID := strings.TrimSpace(summary.BackingArtifactID); backingID != "" {
			if _, ok := seen[backingID]; ok {
				continue
			}
			artifact, ok := m.ArtifactSummary(graph.ArtifactID(backingID))
			if !ok {
				continue
			}
			seen[backingID] = struct{}{}
			out = append(out, artifact)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ArtifactSummariesByClasspathSnapshot(snapshotID string) []ArtifactSummary {
	if strings.TrimSpace(snapshotID) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []ArtifactSummary
	for _, summary := range m.ProvenanceSummariesByClasspathSnapshot(snapshotID) {
		for _, artifactID := range summary.ProducedArtifactIDs {
			if _, ok := seen[artifactID]; ok {
				continue
			}
			artifact, ok := m.ArtifactSummary(graph.ArtifactID(artifactID))
			if !ok {
				continue
			}
			seen[artifactID] = struct{}{}
			out = append(out, artifact)
		}
		if backingID := strings.TrimSpace(summary.BackingArtifactID); backingID != "" {
			if _, ok := seen[backingID]; ok {
				continue
			}
			artifact, ok := m.ArtifactSummary(graph.ArtifactID(backingID))
			if !ok {
				continue
			}
			seen[backingID] = struct{}{}
			out = append(out, artifact)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) ActionSummariesByClasspathSnapshot(snapshotID string) []ActionSummary {
	if strings.TrimSpace(snapshotID) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []ActionSummary
	for _, summary := range m.ProvenanceSummariesByClasspathSnapshot(snapshotID) {
		for _, actionID := range summary.ConsumingActionIDs {
			if _, ok := seen[actionID]; ok {
				continue
			}
			action, ok := m.ActionSummary(graph.ActionID(actionID))
			if !ok {
				continue
			}
			seen[actionID] = struct{}{}
			out = append(out, action)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func buildActionSummaries(g *graph.Graph) []ActionSummary {
	if g == nil {
		return nil
	}
	modulePaths := modulePathsByID(g)
	actions := g.Actions()
	out := make([]ActionSummary, 0, len(actions))
	for _, action := range actions {
		workerClass := workerClassForAction(action)
		out = append(out, ActionSummary{
			ID:             action.ID.String(),
			ModuleID:       action.ModuleID.String(),
			ModulePath:     modulePaths[action.ModuleID],
			VariantID:      action.VariantID.String(),
			VariantName:    action.Attributes["variantName"],
			Name:           action.Name,
			Kind:           string(action.Kind),
			Operation:      action.Attributes["operation"],
			Inputs:         artifactIDs(action.Inputs),
			Outputs:        artifactIDs(action.Outputs),
			Note:           action.Note,
			CacheKey:       actionCacheKey(action),
			WorkerClass:    workerClass,
			ResourceClass:  resourceClassForWorkerClass(workerClass),
			ResourceCost:   resourceCostForWorkerClass(workerClass),
			MaxParallelism: maxParallelismForWorkerClass(workerClass),
			RetentionClass: string(retentionClassForAction(action)),
			Shareability:   string(shareabilityForAction(action)),
		})
	}
	return out
}

func buildArtifactSummaries(g *graph.Graph) []ArtifactSummary {
	if g == nil {
		return nil
	}
	modulePaths := modulePathsByID(g)
	artifacts := g.Artifacts()
	out := make([]ArtifactSummary, 0, len(artifacts))
	for _, artifact := range artifacts {
		moduleID := ""
		modulePath := ""
		variantID := ""
		variantName := ""
		if materialization, ok := g.Materialization(artifact.MaterializationID); ok {
			moduleID = materialization.ModuleID.String()
			modulePath = modulePaths[materialization.ModuleID]
			variantID = materialization.VariantID.String()
			if variant, ok := g.Variant(materialization.VariantID); ok {
				variantName = variant.Name
			}
		}
		out = append(out, ArtifactSummary{
			ID:                 artifact.ID.String(),
			ModuleID:           moduleID,
			ModulePath:         modulePath,
			VariantID:          variantID,
			VariantName:        variantName,
			MaterializationID:  artifact.MaterializationID.String(),
			ProducedByActionID: artifact.ProducedByActionID.String(),
			Kind:               string(artifact.Kind),
			Path:               artifact.Path,
			Digest:             artifact.Digest,
			Note:               artifact.Note,
			RetentionClass:     string(retentionClassForArtifact(artifact)),
			Shareability:       string(shareabilityForArtifact(artifact)),
		})
	}
	return out
}

func buildProvenanceSummaries(g *graph.Graph) []ProvenanceSummary {
	if g == nil {
		return nil
	}
	modulePaths := modulePathsByID(g)
	materializations := g.Materializations()
	out := make([]ProvenanceSummary, 0, len(materializations))
	for _, materialization := range materializations {
		variantName := ""
		if variant, ok := g.Variant(materialization.VariantID); ok {
			variantName = variant.Name
		}
		var producedArtifactIDs []string
		var consumingActionIDs []string
		for _, artifact := range g.MaterializationArtifacts(materialization.ID) {
			producedArtifactIDs = append(producedArtifactIDs, artifact.ID.String())
			for _, action := range g.ActionsConsumingArtifact(artifact.ID) {
				consumingActionIDs = append(consumingActionIDs, action.ID.String())
			}
		}
		if materialization.BackingArtifactID != "" {
			for _, action := range g.ActionsConsumingArtifact(materialization.BackingArtifactID) {
				consumingActionIDs = append(consumingActionIDs, action.ID.String())
			}
		}
		out = append(out, ProvenanceSummary{
			MaterializationID:    materialization.ID.String(),
			ModuleID:             materialization.ModuleID.String(),
			ModulePath:           modulePaths[materialization.ModuleID],
			VariantID:            materialization.VariantID.String(),
			VariantName:          variantName,
			Mode:                 string(materialization.Kind),
			ArtifactSnapshotID:   materialization.ArtifactSnapshotID,
			ClasspathSnapshotIDs: append([]string(nil), materialization.ClasspathSnapshotIDs...),
			SourceRoots:          append([]string(nil), materialization.SourceRoots...),
			ManifestPaths:        manifestPathsForSourceRoots(materialization.SourceRoots),
			BackingArtifactID:    materialization.BackingArtifactID.String(),
			ProducedArtifactIDs:  uniqueStrings(producedArtifactIDs),
			ConsumingActionIDs:   uniqueStrings(consumingActionIDs),
			Note:                 materialization.Note,
			RetentionClass:       string(retentionClassForMaterialization(materialization)),
			Shareability:         string(shareabilityForMaterialization(materialization)),
		})
	}
	return out
}

func retentionClassForAction(action graph.Action) cachepolicy.RetentionClass {
	switch action.Kind {
	case graph.ActionKindTest:
		return cachepolicy.RetentionClassDiagnostic
	default:
		return cachepolicy.RetentionClassMachineShareable
	}
}

func shareabilityForAction(action graph.Action) cachepolicy.Shareability {
	switch action.Attributes["operation"] {
	case "install":
		return cachepolicy.ShareabilityWorktreeOnly
	default:
		return cachepolicy.ShareabilityMachine
	}
}

func retentionClassForArtifact(artifact graph.Artifact) cachepolicy.RetentionClass {
	switch artifact.Kind {
	case graph.ArtifactKindProvenance:
		return cachepolicy.RetentionClassIndex
	default:
		return cachepolicy.RetentionClassMachineShareable
	}
}

func shareabilityForArtifact(artifact graph.Artifact) cachepolicy.Shareability {
	return cachepolicy.ShareabilityMachine
}

func retentionClassForMaterialization(materialization graph.Materialization) cachepolicy.RetentionClass {
	switch materialization.Kind {
	case graph.MaterializationKindSourceBacked:
		return cachepolicy.RetentionClassWorktreeEphemeral
	default:
		return cachepolicy.RetentionClassMachineShareable
	}
}

func shareabilityForMaterialization(materialization graph.Materialization) cachepolicy.Shareability {
	switch materialization.Kind {
	case graph.MaterializationKindSourceBacked:
		return cachepolicy.ShareabilityWorktreeOnly
	default:
		return cachepolicy.ShareabilityMachine
	}
}

func modulePathsByID(g *graph.Graph) map[graph.LogicalModuleID]string {
	out := make(map[graph.LogicalModuleID]string, len(g.LogicalModules()))
	for _, module := range g.LogicalModules() {
		out[module.ID] = module.Path
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
	sort.Strings(out)
	return out
}

func manifestPathsForSourceRoots(sourceRoots []string) []string {
	if len(sourceRoots) == 0 {
		return nil
	}
	out := make([]string, 0, len(sourceRoots))
	for _, root := range sourceRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		out = append(out, filepath.Join(root, "AndroidManifest.xml"))
	}
	return uniqueStrings(out)
}

func actionCacheKey(action graph.Action) string {
	sum := sha256.New()
	parts := []string{
		action.ID.String(),
		action.ModuleID.String(),
		action.VariantID.String(),
		action.Name,
		string(action.Kind),
		action.Attributes["operation"],
		action.Attributes["variantName"],
		strings.Join(artifactIDs(action.Inputs), ","),
		strings.Join(artifactIDs(action.Outputs), ","),
		action.Note,
	}
	for _, part := range parts {
		fmt.Fprint(sum, part)
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}
