package project

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/classpath"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/identity"
	"github.com/kaeawc/grit/internal/materialization"
	"github.com/kaeawc/grit/internal/modulebuild"
	"github.com/kaeawc/grit/internal/responsepayload"
)

type SemanticGraphSummary struct {
	NodeCount int                     `json:"nodeCount"`
	EdgeCount int                     `json:"edgeCount"`
	Modules   []SemanticModuleSummary `json:"modules,omitempty"`
}

type SemanticModuleSummary struct {
	ID                        string                   `json:"id"`
	Name                      string                   `json:"name,omitempty"`
	Path                      string                   `json:"path,omitempty"`
	Dir                       string                   `json:"dir,omitempty"`
	Kind                      string                   `json:"kind,omitempty"`
	CompileSDK                string                   `json:"compileSdk,omitempty"`
	BuildToolsVersion         string                   `json:"buildToolsVersion,omitempty"`
	Namespace                 string                   `json:"namespace,omitempty"`
	ApplicationID             string                   `json:"applicationId,omitempty"`
	TestInstrumentationRunner string                   `json:"testInstrumentationRunner,omitempty"`
	ConsumerProguardFiles     []string                 `json:"consumerProguardFiles,omitempty"`
	Plugins                   []string                 `json:"plugins,omitempty"`
	Tasks                     []string                 `json:"tasks,omitempty"`
	DependsOn                 []string                 `json:"dependsOn,omitempty"`
	DependencyClosure         []string                 `json:"dependencyClosure,omitempty"`
	Variants                  []SemanticVariantSummary `json:"variants,omitempty"`
}

type SemanticVariantSummary struct {
	ID                        string                         `json:"id"`
	Name                      string                         `json:"name,omitempty"`
	DeclaredName              string                         `json:"declaredName,omitempty"`
	CoordinateName            string                         `json:"coordinateName,omitempty"`
	DisplayName               string                         `json:"displayName,omitempty"`
	Compatibility             VariantCompatibility           `json:"compatibility,omitempty"`
	BuildType                 string                         `json:"buildType,omitempty"`
	Flavors                   []string                       `json:"flavors,omitempty"`
	Coordinate                VariantCoordinate              `json:"coordinate,omitempty"`
	CompileSDK                string                         `json:"compileSdk,omitempty"`
	BuildToolsVersion         string                         `json:"buildToolsVersion,omitempty"`
	Namespace                 string                         `json:"namespace,omitempty"`
	ApplicationID             string                         `json:"applicationId,omitempty"`
	ApplicationIDSuffix       string                         `json:"applicationIdSuffix,omitempty"`
	VersionCode               string                         `json:"versionCode,omitempty"`
	VersionName               string                         `json:"versionName,omitempty"`
	VersionNameSuffix         string                         `json:"versionNameSuffix,omitempty"`
	MinSDK                    string                         `json:"minSdk,omitempty"`
	TargetSDK                 string                         `json:"targetSdk,omitempty"`
	TestInstrumentationRunner string                         `json:"testInstrumentationRunner,omitempty"`
	MissingDimensions         map[string][]string            `json:"missingDimensionStrategies,omitempty"`
	Optimization              VariantOptimization            `json:"optimization,omitempty"`
	ProguardFiles             []string                       `json:"proguardFiles,omitempty"`
	ConsumerProguardFiles     []string                       `json:"consumerProguardFiles,omitempty"`
	SigningConfig             string                         `json:"signingConfig,omitempty"`
	SigningConfigured         bool                           `json:"signingConfigured,omitempty"`
	MinifyEnabled             bool                           `json:"minifyEnabled,omitempty"`
	ShrinkResources           bool                           `json:"shrinkResources,omitempty"`
	Installable               bool                           `json:"installable,omitempty"`
	Testable                  bool                           `json:"testable,omitempty"`
	Debuggable                bool                           `json:"debuggable,omitempty"`
	InstallTask               string                         `json:"installTask,omitempty"`
	UninstallTask             string                         `json:"uninstallTask,omitempty"`
	SourceSetOrder            []string                       `json:"sourceSetOrder,omitempty"`
	SourceSetNames            []string                       `json:"sourceSetNames,omitempty"`
	TaskAliases               []string                       `json:"taskAliases,omitempty"`
	ModelSelectors            []string                       `json:"modelSelectors,omitempty"`
	SyncFragments             []string                       `json:"syncFragments,omitempty"`
	DependsOnVariants         []string                       `json:"dependsOnVariants,omitempty"`
	DependencyProvenance      []SemanticDependencyProvenance `json:"dependencyProvenance,omitempty"`
	TaskProjections           []string                       `json:"taskProjections,omitempty"`
	Actions                   []SemanticActionSummary        `json:"actions,omitempty"`
	Materialization           SemanticMaterializationSummary `json:"materialization,omitempty"`
}

type SemanticDependencyProvenance struct {
	ModulePath        string `json:"modulePath,omitempty"`
	VariantName       string `json:"variantName,omitempty"`
	DependencyLevel   string `json:"dependencyLevel,omitempty"`
	RealizationKind   string `json:"realizationKind,omitempty"`
	LogicalModuleKind string `json:"logicalModuleKind,omitempty"`
}

type SemanticMaterializationSummary struct {
	ID                    string                    `json:"id,omitempty"`
	Mode                  string                    `json:"mode,omitempty"`
	ArtifactSnapshotID    string                    `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs  []string                  `json:"classpathSnapshotIds,omitempty"`
	SourceRoots           []string                  `json:"sourceRoots,omitempty"`
	BackingArtifactID     string                    `json:"backingArtifactId,omitempty"`
	BackingArtifactPath   string                    `json:"backingArtifactPath,omitempty"`
	ProducedArtifactIDs   []string                  `json:"producedArtifactIds,omitempty"`
	ProducedArtifactPaths []string                  `json:"producedArtifactPaths,omitempty"`
	ProducedArtifactKinds []string                  `json:"producedArtifactKinds,omitempty"`
	InstallArtifactID     string                    `json:"installArtifactId,omitempty"`
	InstallArtifactPath   string                    `json:"installArtifactPath,omitempty"`
	ResourceArtifactIDs   []string                  `json:"resourceArtifactIds,omitempty"`
	ResourceArtifactPaths []string                  `json:"resourceArtifactPaths,omitempty"`
	ManifestArtifactIDs   []string                  `json:"manifestArtifactIds,omitempty"`
	ManifestArtifactPaths []string                  `json:"manifestArtifactPaths,omitempty"`
	ConsumingActionIDs    []string                  `json:"consumingActionIds,omitempty"`
	Artifacts             []SemanticArtifactSummary `json:"artifacts,omitempty"`
}

type SemanticActionSummary struct {
	ID             string                      `json:"id,omitempty"`
	Name           string                      `json:"name,omitempty"`
	Operation      string                      `json:"operation,omitempty"`
	WorkerClass    string                      `json:"workerClass,omitempty"`
	ResourceClass  string                      `json:"resourceClass,omitempty"`
	CacheKey       string                      `json:"cacheKey,omitempty"`
	LastCacheProbe *responsepayload.CacheProbe `json:"lastCacheProbe,omitempty"`
	Inputs         []string                    `json:"inputs,omitempty"`
	Outputs        []string                    `json:"outputs,omitempty"`
}

type SemanticArtifactSummary struct {
	ID                 string `json:"id,omitempty"`
	Kind               string `json:"kind,omitempty"`
	Path               string `json:"path,omitempty"`
	ProducedByActionID string `json:"producedByActionId,omitempty"`
}

func (prj *Project) SemanticModule(path string) (SemanticModuleSummary, bool) {
	if prj == nil {
		return SemanticModuleSummary{}, false
	}
	for _, mod := range prj.SemanticGraphSummary().Modules {
		if mod.Path == path {
			return mod, true
		}
	}
	return SemanticModuleSummary{}, false
}

func (prj *Project) SemanticVariantNames(path string) ([]string, error) {
	mod, ok := prj.SemanticModule(path)
	if !ok {
		return nil, fmt.Errorf("module %s not found", path)
	}
	if len(mod.Variants) == 0 {
		return []string{"debug"}, nil
	}
	out := make([]string, 0, len(mod.Variants))
	for _, variant := range mod.Variants {
		name := strings.TrimSpace(variant.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return []string{"debug"}, nil
	}
	return out, nil
}

func (prj *Project) SemanticVariant(path, name string) (SemanticVariantSummary, bool) {
	mod, ok := prj.SemanticModule(path)
	if !ok {
		return SemanticVariantSummary{}, false
	}
	name = strings.TrimSpace(name)
	for _, variant := range mod.Variants {
		if variant.Name == name {
			return variant, true
		}
	}
	return SemanticVariantSummary{}, false
}

func (prj *Project) SemanticGraphDetailed() *graph.Graph {
	g := graph.New()
	if prj == nil {
		return g
	}
	modulesByPath := make(map[string]Module, len(prj.Modules))
	for _, mod := range prj.Modules {
		modulesByPath[mod.Path] = mod
		moduleID := semanticModuleID(prj, mod)
		if err := g.AddLogicalModule(graph.LogicalModule{
			ID:   graph.LogicalModuleID(moduleID.String()),
			Name: semanticModuleName(prj, mod),
			Path: mod.Path,
			Dir:  mod.Dir,
			Kind: semanticModuleKind(mod.Type),
			Attributes: map[string]string{
				"project": prj.Name,
			},
		}); err != nil {
			continue
		}
		variants := semanticVariants(mod)
		for _, variant := range variants {
			variantID := identity.NewVariantID(moduleID, semanticVariantCoordinates(variant)...)
			sourceRoots := semanticSourceRoots(mod, variant.Name)
			cpSnapshot := semanticClasspathSnapshot(moduleID, variantID, mod, sourceRoots)
			artifactSnapshot := semanticArtifactSnapshot(moduleID, variantID, mod, sourceRoots, cpSnapshot)
			materializationID := identity.NewMaterializationID(moduleID, variantID, identity.MaterializationSource, artifactSnapshot.Fingerprint())
			buildTypeName := variant.Name
			if strings.TrimSpace(variant.BaseBuildType) != "" {
				buildTypeName = variant.BaseBuildType
			}
			if err := g.AddVariant(graph.Variant{
				ID:        graph.VariantID(variantID.String()),
				ModuleID:  graph.LogicalModuleID(moduleID.String()),
				Name:      variant.Name,
				BuildType: buildTypeName,
				Flavors:   semanticVariantFlavors(variant),
				Attributes: map[string]string{
					"classpathSnapshotId": cpSnapshot.ID,
				},
			}); err != nil {
				continue
			}
			backingArtifactID := identity.NewArtifactID(moduleID, variantID, "sources", strings.Join(sourceRoots, ","))
			if err := g.AddMaterialization(graph.Materialization{
				ID:                   graph.MaterializationID(materializationID.String()),
				ModuleID:             graph.LogicalModuleID(moduleID.String()),
				VariantID:            graph.VariantID(variantID.String()),
				Kind:                 graph.MaterializationKindSourceBacked,
				BackingArtifactID:    graph.ArtifactID(backingArtifactID.String()),
				SourceRoots:          sourceRoots,
				ArtifactSnapshotID:   artifactSnapshot.ID,
				ClasspathSnapshotIDs: []string{cpSnapshot.ID},
				Attributes: map[string]string{
					"materializationId": artifactSnapshot.ID,
					"mode":              string(materialization.ModeSourceBacked),
				},
			}); err != nil {
				continue
			}
			if err := g.AddArtifact(graph.Artifact{
				ID:                graph.ArtifactID(backingArtifactID.String()),
				MaterializationID: graph.MaterializationID(materializationID.String()),
				Kind:              graph.ArtifactKindDirectory,
				Path:              firstNonEmpty(sourceRoots...),
				Digest:            artifactSnapshot.ID,
				Note:              "semantic source root",
				Attributes: map[string]string{
					"artifactSnapshotId":  artifactSnapshot.ID,
					"classpathSnapshotId": cpSnapshot.ID,
				},
			}); err != nil {
				continue
			}
			_, _ = g.AddEdge(graph.Edge{
				From: graph.NodeRef{Kind: graph.NodeKindLogicalModule, ID: moduleID.String()},
				To:   graph.NodeRef{Kind: graph.NodeKindVariant, ID: variantID.String()},
				Kind: graph.EdgeKindContains,
			})
			_, _ = g.AddEdge(graph.Edge{
				From: graph.NodeRef{Kind: graph.NodeKindVariant, ID: variantID.String()},
				To:   graph.NodeRef{Kind: graph.NodeKindMaterialization, ID: materializationID.String()},
				Kind: graph.EdgeKindRealizes,
			})
			_, _ = g.AddEdge(graph.Edge{
				From: graph.NodeRef{Kind: graph.NodeKindMaterialization, ID: materializationID.String()},
				To:   graph.NodeRef{Kind: graph.NodeKindArtifact, ID: backingArtifactID.String()},
				Kind: graph.EdgeKindBacks,
			})
			addSemanticActions(
				g,
				graph.LogicalModuleID(moduleID.String()),
				graph.VariantID(variantID.String()),
				graph.MaterializationID(materializationID.String()),
				graph.ArtifactID(backingArtifactID.String()),
				mod,
				variant.Name,
			)
		}
	}
	for _, mod := range prj.Modules {
		deps, err := modulebuild.ParseDependencies(mod.BuildFile)
		if err != nil {
			continue
		}
		for _, dep := range semanticProjectDependencyRefs(deps) {
			target := semanticModuleIDByPath(prj, dep)
			if target == "" {
				continue
			}
			from := semanticModuleID(prj, mod)
			_, _ = g.AddEdge(graph.Edge{
				From: graph.NodeRef{Kind: graph.NodeKindLogicalModule, ID: from.String()},
				To:   graph.NodeRef{Kind: graph.NodeKindLogicalModule, ID: target.String()},
				Kind: graph.EdgeKindDependsOn,
			})
			targetModule, ok := modulesByPath[dep]
			if !ok {
				continue
			}
			for _, variant := range semanticVariants(mod) {
				connectSemanticVariantDependency(g, prj, mod, variant.Name, targetModule)
			}
		}
	}
	return g
}

func (prj *Project) SemanticGraphSummary() SemanticGraphSummary {
	g := prj.SemanticGraphDetailed()
	summary := SemanticGraphSummary{
		NodeCount: g.NodeCount(),
		EdgeCount: g.EdgeCount(),
	}
	for _, module := range g.LogicalModules() {
		moduleSummary := SemanticModuleSummary{
			ID:   module.ID.String(),
			Name: module.Name,
			Path: module.Path,
			Dir:  module.Dir,
			Kind: string(module.Kind),
		}
		mod := prj.FindModule(module.Path)
		if mod != nil {
			moduleSummary.CompileSDK = mod.CompileSDK
			moduleSummary.BuildToolsVersion = mod.BuildToolsVersion
			moduleSummary.Namespace = mod.Namespace
			moduleSummary.ApplicationID = mod.ApplicationID
			moduleSummary.TestInstrumentationRunner = mod.TestInstrumentationRunner
			moduleSummary.ConsumerProguardFiles = append([]string(nil), mod.ConsumerProguardFiles...)
			moduleSummary.Plugins = append([]string(nil), mod.Plugins...)
			moduleSummary.Tasks = taskNames(mod.Tasks())
		}
		moduleSummary.DependsOn = moduleDependencyPaths(g, module.ID)
		moduleSummary.DependencyClosure = moduleDependencyClosure(g, module.ID)
		for _, variant := range g.ModuleVariants(module.ID) {
			resolved := ResolvedVariant{}
			if mod != nil {
				resolved = mod.ResolveVariant(variant.Name)
			}
			variantSummary := SemanticVariantSummary{
				ID:             variant.ID.String(),
				Name:           variant.Name,
				DeclaredName:   resolved.DeclaredName,
				CoordinateName: resolved.CoordinateName,
				DisplayName:    resolved.DisplayName,
				Compatibility: VariantCompatibility{
					VariantName:    resolved.Compatibility.VariantName,
					CoordinateName: resolved.Compatibility.CoordinateName,
					DisplayName:    resolved.Compatibility.DisplayName,
					SourceSetOrder: append([]string(nil), resolved.Compatibility.SourceSetOrder...),
					SourceSetNames: append([]string(nil), resolved.Compatibility.SourceSetNames...),
					TaskAliases:    append([]string(nil), resolved.Compatibility.TaskAliases...),
					ModelSelectors: append([]string(nil), resolved.Compatibility.ModelSelectors...),
					SyncFragments:  append([]string(nil), resolved.Compatibility.SyncFragments...),
				},
				BuildType: variant.BuildType,
				Flavors:   append([]string(nil), variant.Flavors...),
				Coordinate: VariantCoordinate{
					ModulePath: resolved.Coordinate.ModulePath,
					Name:       resolved.Coordinate.Name,
					BuildType:  resolved.Coordinate.BuildType,
					Flavors:    append([]string(nil), resolved.Coordinate.Flavors...),
				},
				CompileSDK:                resolved.CompileSDK,
				BuildToolsVersion:         resolved.BuildToolsVersion,
				Namespace:                 resolved.Namespace,
				ApplicationID:             resolved.ApplicationID,
				ApplicationIDSuffix:       resolved.ApplicationIDSuffix,
				VersionCode:               resolved.VersionCode,
				VersionName:               resolved.VersionName,
				VersionNameSuffix:         resolved.VersionNameSuffix,
				MinSDK:                    resolved.MinSDK,
				TargetSDK:                 resolved.TargetSDK,
				TestInstrumentationRunner: resolved.TestInstrumentationRunner,
				MissingDimensions:         cloneStrategyMap(resolved.MissingDimensions),
				Optimization:              resolved.Optimization,
				ProguardFiles:             append([]string(nil), resolved.ProguardFiles...),
				ConsumerProguardFiles:     append([]string(nil), resolved.ConsumerProguardFiles...),
				SigningConfig:             resolved.SigningConfig,
				SigningConfigured:         resolved.SigningConfigured,
				MinifyEnabled:             resolved.MinifyEnabled,
				ShrinkResources:           resolved.ShrinkResources,
				Installable:               resolved.Installable,
				Testable:                  resolved.Testable,
				Debuggable:                resolved.Debuggable,
				InstallTask:               resolved.InstallTask,
				UninstallTask:             resolved.UninstallTask,
				SourceSetOrder:            append([]string(nil), resolved.SourceSetOrder...),
				SourceSetNames:            append([]string(nil), resolved.SourceSetNames...),
				TaskAliases:               append([]string(nil), resolved.TaskAliases...),
				ModelSelectors:            append([]string(nil), resolved.ModelSelectors...),
				SyncFragments:             append([]string(nil), resolved.SyncFragments...),
				DependsOnVariants:         variantDependencyNames(g, variant.ID),
				DependencyProvenance:      variantDependencyProvenance(g, variant.ID),
				TaskProjections:           semanticTaskProjections(module.Kind, variant.Name, variant.BuildType),
				Actions:                   semanticActionSummaries(g, variant.ID),
			}
			materializations := g.VariantMaterializations(variant.ID)
			if len(materializations) > 0 {
				m := materializations[0]
				mode := m.Attributes["mode"]
				if mode == "" {
					mode = string(materialization.ModeSourceBacked)
				}
				artifacts := semanticArtifactSummaries(g, m.ID)
				resolvedArtifacts := make([]ResolvedVariantArtifact, 0, len(artifacts))
				for _, artifact := range artifacts {
					resolvedArtifacts = append(resolvedArtifacts, ResolvedVariantArtifact(artifact))
				}
				variantSummary.Materialization = SemanticMaterializationSummary{
					ID:                    m.ID.String(),
					Mode:                  mode,
					ArtifactSnapshotID:    m.ArtifactSnapshotID,
					ClasspathSnapshotIDs:  append([]string(nil), m.ClasspathSnapshotIDs...),
					SourceRoots:           append([]string(nil), m.SourceRoots...),
					BackingArtifactID:     m.BackingArtifactID.String(),
					BackingArtifactPath:   semanticArtifactPath(g, m.BackingArtifactID),
					ProducedArtifactIDs:   semanticProducedArtifactIDs(g, m.ID),
					ProducedArtifactPaths: resolvedVariantArtifactPaths(resolvedArtifacts),
					ProducedArtifactKinds: resolvedVariantArtifactKinds(resolvedArtifacts),
					InstallArtifactID:     resolvedVariantFirstArtifactIDByKind(resolvedArtifacts, "apk"),
					InstallArtifactPath:   resolvedVariantFirstArtifactPathByKind(resolvedArtifacts, "apk"),
					ResourceArtifactIDs:   resolvedVariantArtifactIDsByKind(resolvedArtifacts, "resources"),
					ResourceArtifactPaths: resolvedVariantArtifactPathsByKind(resolvedArtifacts, "resources"),
					ManifestArtifactIDs:   resolvedVariantArtifactIDsByKind(resolvedArtifacts, "manifest"),
					ManifestArtifactPaths: resolvedVariantArtifactPathsByKind(resolvedArtifacts, "manifest"),
					ConsumingActionIDs:    semanticConsumingActionIDs(g, m.ID),
					Artifacts:             artifacts,
				}
			}
			moduleSummary.Variants = append(moduleSummary.Variants, variantSummary)
		}
		summary.Modules = append(summary.Modules, moduleSummary)
	}
	sort.Slice(summary.Modules, func(i, j int) bool {
		return summary.Modules[i].ID < summary.Modules[j].ID
	})
	return summary
}

func semanticArtifactPath(g *graph.Graph, id graph.ArtifactID) string {
	artifact, ok := g.Artifact(id)
	if !ok {
		return ""
	}
	return artifact.Path
}

func taskNames(tasks []Task) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(task.Name) == "" {
			continue
		}
		out = append(out, task.Name)
	}
	sort.Strings(out)
	return out
}

func moduleDependencyPaths(g *graph.Graph, moduleID graph.LogicalModuleID) []string {
	var out []string
	for _, dep := range g.DependenciesOf(moduleID.Ref()) {
		if dep.Kind != graph.NodeKindLogicalModule {
			continue
		}
		mod, ok := g.LogicalModule(graph.LogicalModuleID(dep.ID))
		if !ok || strings.TrimSpace(mod.Path) == "" {
			continue
		}
		out = append(out, mod.Path)
	}
	sort.Strings(out)
	return out
}

func moduleDependencyClosure(g *graph.Graph, moduleID graph.LogicalModuleID) []string {
	if g == nil || moduleID == "" {
		return nil
	}
	start := moduleID.Ref()
	seen := map[graph.NodeRef]struct{}{start: {}}
	queue := []graph.NodeRef{start}
	paths := map[string]struct{}{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dep := range g.DependenciesOf(current) {
			if dep.Kind != graph.NodeKindLogicalModule {
				continue
			}
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			queue = append(queue, dep)
			if mod, ok := g.LogicalModule(graph.LogicalModuleID(dep.ID)); ok && strings.TrimSpace(mod.Path) != "" {
				paths[mod.Path] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func variantDependencyNames(g *graph.Graph, variantID graph.VariantID) []string {
	var out []string
	for _, dep := range g.DependenciesOf(variantID.Ref()) {
		if dep.Kind != graph.NodeKindVariant {
			continue
		}
		variant, ok := g.Variant(graph.VariantID(dep.ID))
		if !ok || strings.TrimSpace(variant.Name) == "" {
			continue
		}
		out = append(out, variant.Name)
	}
	sort.Strings(out)
	return out
}

func variantDependencyProvenance(g *graph.Graph, variantID graph.VariantID) []SemanticDependencyProvenance {
	if g == nil {
		return nil
	}
	out := make([]SemanticDependencyProvenance, 0)
	for _, edge := range g.EdgesFrom(variantID.Ref()) {
		if edge.Kind != graph.EdgeKindDependsOn {
			continue
		}
		if edge.To.Kind != graph.NodeKindVariant {
			continue
		}
		upstream, ok := g.Variant(graph.VariantID(edge.To.ID))
		if !ok {
			continue
		}
		modulePath := ""
		moduleKind := ""
		if module, ok := g.LogicalModule(upstream.ModuleID); ok {
			modulePath = module.Path
			moduleKind = string(module.Kind)
		}
		realizationKind := ""
		for _, mat := range g.VariantMaterializations(graph.VariantID(edge.To.ID)) {
			realizationKind = string(mat.Kind)
			break
		}
		out = append(out, SemanticDependencyProvenance{
			ModulePath:        modulePath,
			VariantName:       upstream.Name,
			DependencyLevel:   edge.Attributes["dependencyLevel"],
			RealizationKind:   realizationKind,
			LogicalModuleKind: moduleKind,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModulePath == out[j].ModulePath {
			if out[i].VariantName == out[j].VariantName {
				return out[i].DependencyLevel < out[j].DependencyLevel
			}
			return out[i].VariantName < out[j].VariantName
		}
		return out[i].ModulePath < out[j].ModulePath
	})
	return uniqueDependencyProvenance(out)
}

func uniqueDependencyProvenance(values []SemanticDependencyProvenance) []SemanticDependencyProvenance {
	if len(values) == 0 {
		return nil
	}
	out := make([]SemanticDependencyProvenance, 0, len(values))
	for _, value := range values {
		if len(out) > 0 && out[len(out)-1] == value {
			continue
		}
		out = append(out, value)
	}
	return out
}

func semanticActionSummaries(g *graph.Graph, variantID graph.VariantID) []SemanticActionSummary {
	var out []SemanticActionSummary
	for _, action := range g.ActionsForVariant(variantID) {
		out = append(out, SemanticActionSummary{
			ID:        action.ID.String(),
			Name:      action.Name,
			Operation: action.Attributes["operation"],
			Inputs:    semanticArtifactIDs(action.Inputs),
			Outputs:   semanticArtifactIDs(action.Outputs),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Operation == out[j].Operation {
			return out[i].ID < out[j].ID
		}
		return out[i].Operation < out[j].Operation
	})
	return out
}

func semanticArtifactSummaries(g *graph.Graph, materializationID graph.MaterializationID) []SemanticArtifactSummary {
	var out []SemanticArtifactSummary
	for _, artifact := range g.MaterializationArtifacts(materializationID) {
		out = append(out, SemanticArtifactSummary{
			ID:                 artifact.ID.String(),
			Kind:               string(artifact.Kind),
			Path:               artifact.Path,
			ProducedByActionID: artifact.ProducedByActionID.String(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func semanticProducedArtifactIDs(g *graph.Graph, materializationID graph.MaterializationID) []string {
	var out []string
	for _, artifact := range g.MaterializationArtifacts(materializationID) {
		out = append(out, artifact.ID.String())
	}
	sort.Strings(out)
	return out
}

func semanticConsumingActionIDs(g *graph.Graph, materializationID graph.MaterializationID) []string {
	mat, ok := g.Materialization(materializationID)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	if mat.BackingArtifactID != "" {
		for _, action := range g.ActionsConsumingArtifact(mat.BackingArtifactID) {
			if _, ok := seen[action.ID.String()]; ok {
				continue
			}
			seen[action.ID.String()] = struct{}{}
			out = append(out, action.ID.String())
		}
	}
	for _, artifact := range g.MaterializationArtifacts(materializationID) {
		for _, action := range g.ActionsConsumingArtifact(artifact.ID) {
			if _, ok := seen[action.ID.String()]; ok {
				continue
			}
			seen[action.ID.String()] = struct{}{}
			out = append(out, action.ID.String())
		}
	}
	sort.Strings(out)
	return out
}

func semanticArtifactIDs(ids []graph.ArtifactID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	sort.Strings(out)
	return out
}

func semanticTaskProjections(kind graph.ModuleKind, variantName, buildType string) []string {
	var out []string
	switch kind {
	case graph.ModuleKindJvmLibrary:
		out = append(out, "build", "test", "check")
	default:
		out = append(out, semanticTaskNameForVariant("assemble", variantName))
		out = append(out, semanticTaskNameForVariant("compile", variantName)+"Sources")
		if kind == graph.ModuleKindAndroidApplication {
			out = append(out, semanticTaskNameForVariant("install", variantName))
		}
		if semanticBuildTypeIs(buildType, "debug") {
			out = append(out, semanticTaskNameForVariant("test", variantName)+"UnitTest")
			out = append(out, semanticTaskNameForVariant("compile", variantName)+"UnitTestSources")
			out = append(out, semanticTaskNameForVariant("compile", variantName)+"AndroidTestSources")
			out = append(out, semanticTaskNameForVariant("assemble", variantName)+"AndroidTest")
			if kind == graph.ModuleKindAndroidApplication {
				out = append(out,
					semanticTaskNameForVariant("install", variantName)+"AndroidTest",
					semanticTaskNameForVariant("uninstall", variantName)+"AndroidTest",
				)
			}
		}
	}
	sort.Strings(out)
	return uniqueSemanticStrings(out)
}

func uniqueSemanticStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == value {
			continue
		}
		out = append(out, value)
	}
	return out
}

func semanticTaskNameForVariant(prefix, variantName string) string {
	if variantName == "" {
		return prefix
	}
	if len(variantName) == 1 {
		return prefix + strings.ToUpper(variantName)
	}
	return prefix + strings.ToUpper(variantName[:1]) + variantName[1:]
}

func semanticBuildTypeIs(buildType, expected string) bool {
	return strings.EqualFold(strings.TrimSpace(buildType), strings.TrimSpace(expected))
}

func (prj *Project) SemanticDependentModules(target string) ([]string, error) {
	if prj == nil {
		return nil, nil
	}
	targetID := semanticModuleIDByPath(prj, target)
	if targetID == "" {
		return nil, fmt.Errorf("module %s not found", target)
	}
	g := prj.SemanticGraphDetailed()
	targetRef := graph.LogicalModuleID(targetID.String()).Ref()
	seen := map[graph.NodeRef]struct{}{targetRef: {}}
	queue := []graph.NodeRef{targetRef}
	out := []string{target}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dependent := range g.DependentsOf(current) {
			if dependent.Kind != graph.NodeKindLogicalModule {
				continue
			}
			if _, ok := seen[dependent]; ok {
				continue
			}
			seen[dependent] = struct{}{}
			queue = append(queue, dependent)
			module, ok := g.LogicalModule(graph.LogicalModuleID(dependent.ID))
			if !ok || strings.TrimSpace(module.Path) == "" {
				continue
			}
			out = append(out, module.Path)
		}
	}
	return out, nil
}

func (prj *Project) SemanticActionsForCommand(modulePath, command string, requestedVariants []string) ([]graph.Action, error) {
	if prj == nil {
		return nil, nil
	}
	mod := prj.FindModule(modulePath)
	if mod == nil {
		return nil, fmt.Errorf("module %s not found", modulePath)
	}
	g := prj.SemanticGraphDetailed()
	if mod.IsJVM() {
		return semanticJVMActionsForCommand(prj, g, mod, command, requestedVariants), nil
	}
	switch command {
	case "compile-debug", "compileDebugSources", "compileReleaseSources":
		return semanticActionsForVariants(prj, g, modulePath, requestedVariants, "compile"), nil
	case "lint-debug", "lintDebug", "lint-release", "lintRelease", "lint":
		return semanticActionsForVariants(prj, g, modulePath, requestedVariants, "lint"), nil
	case "install", "install-debug", "installDebug", "installRelease":
		return semanticActionsForVariants(prj, g, modulePath, requestedVariants, "install"), nil
	case "assemble-debug", "assembleDebug", "assemble-release", "assembleRelease", "assemble":
		return semanticActionsForVariants(prj, g, modulePath, requestedVariants, "assemble"), nil
	case "build", "buildNeeded":
		actions := semanticActionsForVariants(prj, g, modulePath, requestedVariants, "assemble")
		actions = append(actions, semanticActionsForVariants(prj, g, modulePath, semanticDebugVariantNames(mod, requestedVariants), "test")...)
		return actions, nil
	case "buildDependents":
		modulePaths, err := prj.SemanticDependentModules(modulePath)
		if err != nil {
			return nil, err
		}
		var out []graph.Action
		for _, depPath := range modulePaths {
			variants, err := prj.SemanticVariantNames(depPath)
			if err != nil {
				return nil, err
			}
			out = append(out, semanticActionsForVariants(prj, g, depPath, variants, "assemble")...)
		}
		return out, nil
	case "test-debug-unit", "testDebugUnitTest", "test", "check":
		return semanticActionsForVariants(prj, g, modulePath, semanticDebugVariantNames(mod, requestedVariants), "test"), nil
	case "compileDebugUnitTestSources", "assembleUnitTest":
		return semanticActionsForVariants(prj, g, modulePath, semanticDebugVariantNames(mod, requestedVariants), "compile-tests"), nil
	case "compileDebugAndroidTestSources", "assembleAndroidTest":
		return semanticActionsForVariants(prj, g, modulePath, semanticDebugVariantNames(mod, requestedVariants), "compile-android-tests"), nil
	case "installDebugAndroidTest", "install-android-tests":
		return semanticActionsForVariants(prj, g, modulePath, semanticDebugVariantNames(mod, requestedVariants), "install-android-tests"), nil
	case "uninstallDebugAndroidTest", "uninstall-android-tests":
		return semanticActionsForVariants(prj, g, modulePath, semanticDebugVariantNames(mod, requestedVariants), "uninstall-android-tests"), nil
	default:
		return nil, nil
	}
}

func semanticModuleID(prj *Project, mod Module) identity.ModuleID {
	projectName := strings.TrimSpace(prj.Name)
	if projectName == "" {
		projectName = filepath.Base(prj.RootDir)
	}
	return identity.NewModuleID(projectName + ":" + mod.Path)
}

func semanticModuleName(prj *Project, mod Module) string {
	if strings.TrimSpace(mod.Path) != "" {
		return strings.TrimPrefix(mod.Path, ":")
	}
	if strings.TrimSpace(prj.Name) != "" {
		return prj.Name
	}
	return filepath.Base(prj.RootDir)
}

func semanticModuleKind(moduleType string) graph.ModuleKind {
	switch moduleType {
	case "android-application":
		return graph.ModuleKindAndroidApplication
	case "android-library":
		return graph.ModuleKindAndroidLibrary
	case "jvm-library":
		return graph.ModuleKindJvmLibrary
	default:
		return graph.ModuleKindUnknown
	}
}

func semanticVariants(mod Module) []BuildType {
	variants := mod.Variants()
	if len(variants) > 0 {
		return variants
	}
	return []BuildType{mod.Variant(mod.DefaultVariantName())}
}

func semanticVariantCoordinates(variant BuildType) []string {
	coordinateName := variantNameFromFlavors(variant.Flavors, firstNonEmpty(variant.BaseBuildType, variant.Name))
	if coordinateName == "" {
		coordinateName = variant.Name
	}
	coords := []string{coordinateName}
	if strings.TrimSpace(variant.BaseBuildType) != "" {
		coords = append(coords, "buildType:"+variant.BaseBuildType)
	}
	for _, flavor := range variant.Flavors {
		coords = append(coords, "flavor:"+flavor)
	}
	if variant.SigningConfig != "" {
		coords = append(coords, "signing:"+variant.SigningConfig)
	}
	return coords
}

func semanticVariantFlavors(variant BuildType) []string {
	return append([]string(nil), variant.Flavors...)
}

func semanticProjectDependencyRefs(deps *modulebuild.Dependencies) []string {
	var refs []modulebuild.Ref
	refs = append(refs, deps.Main...)
	refs = append(refs, deps.Debug...)
	refs = append(refs, deps.Test...)
	refs = append(refs, deps.CompileOnly...)
	refs = append(refs, deps.TestCompileOnly...)
	seen := map[string]struct{}{}
	var out []string
	for _, ref := range refs {
		if ref.Kind != "project" {
			continue
		}
		if _, ok := seen[ref.Value]; ok {
			continue
		}
		seen[ref.Value] = struct{}{}
		out = append(out, ref.Value)
	}
	return out
}

func semanticModuleIDByPath(prj *Project, path string) identity.ModuleID {
	for _, mod := range prj.Modules {
		if mod.Path == path {
			return semanticModuleID(prj, mod)
		}
	}
	return ""
}

func semanticSourceRoots(mod Module, variantName string) []string {
	roots := []string{
		filepath.Join(mod.Dir, "src", "main"),
	}
	if variantName != "" && variantName != "main" {
		roots = append(roots, filepath.Join(mod.Dir, "src", variantName))
	}
	return roots
}

func semanticClasspathSnapshot(moduleID identity.ModuleID, variantID identity.VariantID, mod Module, sourceRoots []string) classpath.Snapshot {
	entries := make([]classpath.Entry, 0, len(sourceRoots))
	for _, root := range sourceRoots {
		entries = append(entries, classpath.Entry{
			Path:            root,
			NormalizedPath:  root,
			Origin:          classpath.OriginSource,
			ModuleID:        moduleID.String(),
			VariantID:       variantID.String(),
			FamilyKey:       "source-root",
			SelectionReason: "semantic source root",
		})
	}
	provenance := materialization.Provenance{
		Producer: "project.SemanticGraph",
		Subject:  mod.Path,
		Inputs: []materialization.Reference{
			{Kind: "build-file", Path: mod.BuildFile},
		},
		Reasons: []string{"semantic source classpath"},
	}
	return classpath.Normalize(classpath.ScopeCompile, moduleID.String(), variantID.String(), "semantic", entries, provenance)
}

func semanticArtifactSnapshot(moduleID identity.ModuleID, variantID identity.VariantID, mod Module, sourceRoots []string, cpSnapshot classpath.Snapshot) materialization.ArtifactSnapshot {
	refs := make([]materialization.Reference, 0, len(sourceRoots))
	for _, root := range sourceRoots {
		refs = append(refs, materialization.Reference{Kind: "source-root", Path: root})
	}
	return materialization.NewArtifactSnapshot(
		moduleID.String(),
		variantID.String(),
		refs,
		[]materialization.Reference{{Kind: "classpath-snapshot", ID: cpSnapshot.ID}},
		materialization.Provenance{
			Producer: "project.SemanticGraph",
			Subject:  mod.Path,
			Inputs: []materialization.Reference{
				{Kind: "build-file", Path: mod.BuildFile},
			},
			Reasons: []string{"semantic artifact snapshot"},
		},
	)
}

func addSemanticActions(g *graph.Graph, moduleID graph.LogicalModuleID, variantID graph.VariantID, materializationID graph.MaterializationID, backingArtifactID graph.ArtifactID, mod Module, variantName string) {
	if mod.IsJVM() {
		addJVMSemanticActions(g, moduleID, variantID, materializationID, backingArtifactID, mod, variantName)
		return
	}
	operations := []struct {
		op   string
		kind graph.ActionKind
		task string
		out  graph.ArtifactKind
	}{
		{op: "compile", kind: graph.ActionKindCompile, task: semanticTaskName("compile", variantName), out: graph.ArtifactKindJar},
		{op: "lint", kind: graph.ActionKindLint, task: semanticTaskName("lint", variantName), out: graph.ArtifactKindOther},
		{op: "assemble", kind: graph.ActionKindPackage, task: semanticTaskName("assemble", variantName), out: graph.ArtifactKindApk},
		{op: "install", kind: graph.ActionKindCustom, task: semanticTaskName("install", variantName), out: graph.ArtifactKindOther},
	}
	if semanticBuildTypeIs(mod.Variant(variantName).BaseBuildType, "debug") {
		operations = append(operations,
			struct {
				op   string
				kind graph.ActionKind
				task string
				out  graph.ArtifactKind
			}{op: "test", kind: graph.ActionKindTest, task: "testDebugUnitTest", out: graph.ArtifactKindOther},
			struct {
				op   string
				kind graph.ActionKind
				task string
				out  graph.ArtifactKind
			}{op: "compile-tests", kind: graph.ActionKindCompile, task: "compileDebugUnitTestSources", out: graph.ArtifactKindJar},
			struct {
				op   string
				kind graph.ActionKind
				task string
				out  graph.ArtifactKind
			}{op: "compile-android-tests", kind: graph.ActionKindCompile, task: "compileDebugAndroidTestSources", out: graph.ArtifactKindJar},
			struct {
				op   string
				kind graph.ActionKind
				task string
				out  graph.ArtifactKind
			}{op: "install-android-tests", kind: graph.ActionKindCustom, task: semanticTaskName("install", variantName) + "AndroidTest", out: graph.ArtifactKindOther},
			struct {
				op   string
				kind graph.ActionKind
				task string
				out  graph.ArtifactKind
			}{op: "uninstall-android-tests", kind: graph.ActionKindCustom, task: semanticTaskName("uninstall", variantName) + "AndroidTest", out: graph.ArtifactKindOther},
		)
	}
	actionIDs := map[string]graph.ActionID{}
	outputIDs := map[string]graph.ArtifactID{}
	for _, spec := range operations {
		actionID := graph.ActionID(identity.NewActionID(identity.ModuleID(moduleID.String()), identity.VariantID(variantID.String()), spec.op, mod.Path, variantName))
		outputID := graph.ArtifactID(identity.NewArtifactID(identity.ModuleID(moduleID.String()), identity.VariantID(variantID.String()), spec.op+"-output", mod.Path))
		actionIDs[spec.op] = actionID
		outputIDs[spec.op] = outputID
		action := graph.Action{
			ID:        actionID,
			ModuleID:  moduleID,
			VariantID: variantID,
			Name:      spec.task,
			Kind:      spec.kind,
			Outputs:   []graph.ArtifactID{outputID},
			Attributes: map[string]string{
				"operation":       spec.op,
				"taskName":        spec.task,
				"modulePath":      mod.Path,
				"variantName":     variantName,
				"materialization": materializationID.String(),
			},
		}
		_ = g.AddAction(action)
		_ = g.AddArtifact(graph.Artifact{
			ID:                 outputID,
			MaterializationID:  materializationID,
			ProducedByActionID: actionID,
			Kind:               spec.out,
			Attributes: map[string]string{
				"operation": spec.op,
				"taskName":  spec.task,
			},
		})
		_, _ = g.AddEdge(graph.Edge{
			From: actionID.Ref(),
			To:   outputID.Ref(),
			Kind: graph.EdgeKindProduces,
		})
		_, _ = g.AddEdge(graph.Edge{
			From: variantID.Ref(),
			To:   actionID.Ref(),
			Kind: graph.EdgeKindContains,
		})
		if spec.op == "compile" || spec.op == "compile-tests" || spec.op == "lint" {
			_ = g.SetActionInputs(actionID, []graph.ArtifactID{backingArtifactID})
			_, _ = g.AddEdge(graph.Edge{
				From: actionID.Ref(),
				To:   backingArtifactID.Ref(),
				Kind: graph.EdgeKindConsumes,
			})
		}
	}
	linkSemanticActionDependencies(g, actionIDs, outputIDs)
}

func addJVMSemanticActions(g *graph.Graph, moduleID graph.LogicalModuleID, variantID graph.VariantID, materializationID graph.MaterializationID, backingArtifactID graph.ArtifactID, mod Module, variantName string) {
	operations := []struct {
		op   string
		kind graph.ActionKind
		task string
		out  graph.ArtifactKind
	}{
		{op: "compile", kind: graph.ActionKindCompile, task: "compile", out: graph.ArtifactKindJar},
		{op: "test", kind: graph.ActionKindTest, task: "test", out: graph.ArtifactKindOther},
		{op: "compile-tests", kind: graph.ActionKindCompile, task: "compileTest", out: graph.ArtifactKindJar},
	}
	actionIDs := map[string]graph.ActionID{}
	outputIDs := map[string]graph.ArtifactID{}
	for _, spec := range operations {
		actionID := graph.ActionID(identity.NewActionID(identity.ModuleID(moduleID.String()), identity.VariantID(variantID.String()), spec.op, mod.Path, variantName))
		outputID := graph.ArtifactID(identity.NewArtifactID(identity.ModuleID(moduleID.String()), identity.VariantID(variantID.String()), spec.op+"-output", mod.Path))
		actionIDs[spec.op] = actionID
		outputIDs[spec.op] = outputID
		action := graph.Action{
			ID:        actionID,
			ModuleID:  moduleID,
			VariantID: variantID,
			Name:      spec.task,
			Kind:      spec.kind,
			Outputs:   []graph.ArtifactID{outputID},
			Attributes: map[string]string{
				"operation":       spec.op,
				"taskName":        spec.task,
				"modulePath":      mod.Path,
				"variantName":     variantName,
				"materialization": materializationID.String(),
			},
		}
		_ = g.AddAction(action)
		_ = g.AddArtifact(graph.Artifact{
			ID:                 outputID,
			MaterializationID:  materializationID,
			ProducedByActionID: actionID,
			Kind:               spec.out,
			Attributes: map[string]string{
				"operation": spec.op,
				"taskName":  spec.task,
			},
		})
		_, _ = g.AddEdge(graph.Edge{
			From: actionID.Ref(),
			To:   outputID.Ref(),
			Kind: graph.EdgeKindProduces,
		})
		_, _ = g.AddEdge(graph.Edge{
			From: variantID.Ref(),
			To:   actionID.Ref(),
			Kind: graph.EdgeKindContains,
		})
		if spec.op == "compile" || spec.op == "compile-tests" {
			_ = g.SetActionInputs(actionID, []graph.ArtifactID{backingArtifactID})
			_, _ = g.AddEdge(graph.Edge{
				From: actionID.Ref(),
				To:   backingArtifactID.Ref(),
				Kind: graph.EdgeKindConsumes,
			})
		}
	}
	linkSemanticActionDependencies(g, actionIDs, outputIDs)
}

func linkSemanticActionDependencies(g *graph.Graph, actionIDs map[string]graph.ActionID, outputIDs map[string]graph.ArtifactID) {
	compileActionID := actionIDs["compile"]
	compileOutputID := outputIDs["compile"]
	if compileActionID != "" && compileOutputID != "" {
		if compileTestsActionID := actionIDs["compile-tests"]; compileTestsActionID != "" {
			_ = g.SetActionInputs(compileTestsActionID, append(actionInputIDs(g, compileTestsActionID), compileOutputID))
			_, _ = g.AddEdge(graph.Edge{
				From: compileTestsActionID.Ref(),
				To:   compileOutputID.Ref(),
				Kind: graph.EdgeKindConsumes,
			})
			_, _ = g.AddEdge(graph.Edge{
				From: compileTestsActionID.Ref(),
				To:   compileActionID.Ref(),
				Kind: graph.EdgeKindDependsOn,
				Attributes: map[string]string{
					"dependencyLevel": "action",
				},
			})
		}
		if testActionID := actionIDs["test"]; testActionID != "" {
			testInputs := []graph.ArtifactID{compileOutputID}
			if compileTestsOutputID := outputIDs["compile-tests"]; compileTestsOutputID != "" {
				testInputs = append(testInputs, compileTestsOutputID)
			}
			_ = g.SetActionInputs(testActionID, dedupeArtifactIDs(append(actionInputIDs(g, testActionID), testInputs...)))
			for _, input := range testInputs {
				_, _ = g.AddEdge(graph.Edge{
					From: testActionID.Ref(),
					To:   input.Ref(),
					Kind: graph.EdgeKindConsumes,
				})
			}
			_, _ = g.AddEdge(graph.Edge{
				From: testActionID.Ref(),
				To:   compileActionID.Ref(),
				Kind: graph.EdgeKindDependsOn,
				Attributes: map[string]string{
					"dependencyLevel": "action",
				},
			})
			if compileTestsActionID := actionIDs["compile-tests"]; compileTestsActionID != "" {
				_, _ = g.AddEdge(graph.Edge{
					From: testActionID.Ref(),
					To:   compileTestsActionID.Ref(),
					Kind: graph.EdgeKindDependsOn,
					Attributes: map[string]string{
						"dependencyLevel": "action",
					},
				})
			}
		}
	}
}

func actionInputIDs(g *graph.Graph, actionID graph.ActionID) []graph.ArtifactID {
	action, ok := g.Action(actionID)
	if !ok {
		return nil
	}
	return append([]graph.ArtifactID(nil), action.Inputs...)
}

func semanticJVMActionsForCommand(prj *Project, g *graph.Graph, mod *Module, command string, requestedVariants []string) []graph.Action {
	variants := semanticRequestedVariants(mod, requestedVariants)
	switch command {
	case "compile-debug", "compileDebugSources", "compileReleaseSources", "assemble", "assembleDebug", "assembleRelease":
		return semanticActionsForVariants(prj, g, mod.Path, variants, "compile")
	case "build", "buildNeeded":
		actions := semanticActionsForVariants(prj, g, mod.Path, variants, "compile")
		actions = append(actions, semanticActionsForVariants(prj, g, mod.Path, variants, "test")...)
		return actions
	case "test", "check":
		actions := semanticActionsForVariants(prj, g, mod.Path, variants, "compile")
		actions = append(actions, semanticActionsForVariants(prj, g, mod.Path, variants, "test")...)
		return actions
	case "compileDebugUnitTestSources", "assembleUnitTest":
		return semanticActionsForVariants(prj, g, mod.Path, variants, "compile-tests")
	case "compileDebugAndroidTestSources", "assembleAndroidTest":
		return semanticActionsForVariants(prj, g, mod.Path, variants, "compile-android-tests")
	case "buildDependents":
		modulePaths, err := prj.SemanticDependentModules(mod.Path)
		if err != nil {
			return nil
		}
		var out []graph.Action
		for _, depPath := range modulePaths {
			dep := prj.FindModule(depPath)
			if dep == nil {
				continue
			}
			depVariants := variantsForModule(dep)
			operation := "assemble"
			if dep.IsJVM() {
				operation = "compile"
			}
			out = append(out, semanticActionsForVariants(prj, g, depPath, depVariants, operation)...)
		}
		return out
	default:
		return nil
	}
}

func semanticRequestedVariants(mod *Module, requested []string) []string {
	available := map[string]struct{}{}
	for _, variant := range mod.Variants() {
		name := strings.TrimSpace(variant.Name)
		if name == "" {
			continue
		}
		available[name] = struct{}{}
	}
	out := make([]string, 0, len(requested))
	for _, variant := range requested {
		name := strings.TrimSpace(variant)
		if name == "" {
			continue
		}
		if _, ok := available[name]; ok {
			out = append(out, name)
		}
	}
	if len(out) > 0 {
		return out
	}
	if len(available) == 0 {
		return []string{mod.DefaultVariantName()}
	}
	variants := mod.Variants()
	if len(variants) > 0 {
		out = make([]string, 0, len(variants))
		for _, variant := range variants {
			if name := strings.TrimSpace(variant.Name); name != "" {
				out = append(out, name)
			}
		}
	}
	if len(out) == 0 {
		return []string{mod.DefaultVariantName()}
	}
	return out
}

func connectSemanticVariantDependency(g *graph.Graph, prj *Project, from Module, fromVariantName string, target Module) {
	fromVariantID := semanticVariantID(prj, from, fromVariantName)
	targetVariantName := target.resolveDependencyVariant(from.ResolveVariant(fromVariantName))
	targetVariantID := semanticVariantID(prj, target, targetVariantName)
	fromMaterializationID := semanticMaterializationID(prj, from, fromVariantName)
	targetMaterializationID := semanticMaterializationID(prj, target, targetVariantName)

	if _, ok := g.Variant(graph.VariantID(fromVariantID.String())); !ok {
		return
	}
	if _, ok := g.Variant(graph.VariantID(targetVariantID.String())); !ok {
		return
	}
	_, _ = g.AddEdge(graph.Edge{
		From: graph.NodeRef{Kind: graph.NodeKindVariant, ID: fromVariantID.String()},
		To:   graph.NodeRef{Kind: graph.NodeKindVariant, ID: targetVariantID.String()},
		Kind: graph.EdgeKindDependsOn,
		Attributes: map[string]string{
			"dependencyLevel": "variant",
		},
	})
	if _, ok := g.Materialization(graph.MaterializationID(fromMaterializationID.String())); ok {
		if _, ok := g.Materialization(graph.MaterializationID(targetMaterializationID.String())); ok {
			_, _ = g.AddEdge(graph.Edge{
				From: graph.NodeRef{Kind: graph.NodeKindMaterialization, ID: fromMaterializationID.String()},
				To:   graph.NodeRef{Kind: graph.NodeKindMaterialization, ID: targetMaterializationID.String()},
				Kind: graph.EdgeKindDependsOn,
				Attributes: map[string]string{
					"dependencyLevel": "materialization",
				},
			})
		}
	}

	targetBackingArtifactID := semanticBackingArtifactID(prj, target, targetVariantName)
	if _, ok := g.Artifact(graph.ArtifactID(targetBackingArtifactID.String())); !ok {
		return
	}
	for _, operation := range []string{"compile", "lint", "assemble", "test", "compile-tests"} {
		actionID := semanticActionID(prj, from, fromVariantName, operation)
		action, ok := g.Action(graph.ActionID(actionID.String()))
		if !ok {
			continue
		}
		inputs := append([]graph.ArtifactID(nil), action.Inputs...)
		inputs = append(inputs, graph.ArtifactID(targetBackingArtifactID.String()))
		_ = g.SetActionInputs(action.ID, dedupeArtifactIDs(inputs))
		_, _ = g.AddEdge(graph.Edge{
			From: action.ID.Ref(),
			To:   graph.ArtifactID(targetBackingArtifactID.String()).Ref(),
			Kind: graph.EdgeKindConsumes,
			Attributes: map[string]string{
				"dependencyLevel": "variant",
				"upstreamVariant": targetVariantName,
			},
		})
	}
}

func semanticVariantID(prj *Project, mod Module, variantName string) identity.VariantID {
	return identity.NewVariantID(semanticModuleID(prj, mod), semanticVariantCoordinates(mod.Variant(variantName))...)
}

func semanticMaterializationID(prj *Project, mod Module, variantName string) identity.MaterializationID {
	variantID := semanticVariantID(prj, mod, variantName)
	sourceRoots := semanticSourceRoots(mod, variantName)
	cpSnapshot := semanticClasspathSnapshot(semanticModuleID(prj, mod), variantID, mod, sourceRoots)
	artifactSnapshot := semanticArtifactSnapshot(semanticModuleID(prj, mod), variantID, mod, sourceRoots, cpSnapshot)
	return identity.NewMaterializationID(semanticModuleID(prj, mod), variantID, identity.MaterializationSource, artifactSnapshot.Fingerprint())
}

func semanticBackingArtifactID(prj *Project, mod Module, variantName string) identity.ArtifactID {
	variantID := semanticVariantID(prj, mod, variantName)
	sourceRoots := semanticSourceRoots(mod, variantName)
	return identity.NewArtifactID(semanticModuleID(prj, mod), variantID, "sources", strings.Join(sourceRoots, ","))
}

func semanticActionID(prj *Project, mod Module, variantName, operation string) identity.ActionID {
	variantID := semanticVariantID(prj, mod, variantName)
	return identity.NewActionID(semanticModuleID(prj, mod), variantID, operation, mod.Path, variantName)
}

func semanticResolvedVariantName(mod Module, requested string) string {
	requested = strings.TrimSpace(requested)
	for _, variant := range semanticVariants(mod) {
		if variant.Name == requested {
			return requested
		}
	}
	variants := semanticVariants(mod)
	if len(variants) > 0 {
		return variants[0].Name
	}
	return "debug"
}

func dedupeArtifactIDs(ids []graph.ArtifactID) []graph.ArtifactID {
	seen := map[graph.ArtifactID]struct{}{}
	out := make([]graph.ArtifactID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func semanticDebugVariantNames(mod *Module, requested []string) []string {
	if mod == nil {
		return []string{"debug"}
	}
	names := semanticRequestedVariants(mod, requested)
	if len(names) == 0 {
		return []string{"debug"}
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if semanticBuildTypeIs(mod.Variant(name).BaseBuildType, "debug") {
			out = append(out, name)
		}
	}
	if len(out) > 0 {
		return out
	}
	return names
}

func semanticActionsForVariants(prj *Project, g *graph.Graph, modulePath string, variants []string, operation string) []graph.Action {
	modID := semanticModuleIDByPath(prj, modulePath)
	if modID == "" {
		return nil
	}
	wanted := make(map[string]struct{}, len(variants))
	for _, variant := range variants {
		wanted[strings.TrimSpace(variant)] = struct{}{}
	}
	actions := g.ActionsForModule(graph.LogicalModuleID(modID.String()))
	out := make([]graph.Action, 0, len(actions))
	for _, action := range actions {
		if action.Attributes["operation"] != operation {
			continue
		}
		if _, ok := wanted[action.Attributes["variantName"]]; !ok {
			continue
		}
		out = append(out, action)
	}
	return out
}

func variantsForModule(mod *Module) []string {
	if mod == nil {
		return nil
	}
	variants := mod.Variants()
	if len(variants) == 0 {
		return []string{mod.DefaultVariantName()}
	}
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		if strings.TrimSpace(variant.Name) == "" {
			continue
		}
		out = append(out, variant.Name)
	}
	if len(out) == 0 {
		return []string{mod.DefaultVariantName()}
	}
	return out
}

func semanticTaskName(prefix, variantName string) string {
	if variantName == "" {
		return prefix
	}
	if len(variantName) == 1 {
		return prefix + strings.ToUpper(variantName)
	}
	return prefix + strings.ToUpper(variantName[:1]) + variantName[1:]
}
