package configmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/cachepolicy"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
)

const schemaVersion = 5

type Builder interface {
	Build(*project.Project) (*Model, error)
}

type Hook interface {
	BeforeBuild(context.Context, *project.Project) error
	AfterBuild(context.Context, *Model) error
}

type Store struct {
	builder Builder
	hooks   []Hook
}

type Model struct {
	SchemaVersion       int                          `json:"schemaVersion"`
	Repo                string                       `json:"repo"`
	ProjectName         string                       `json:"projectName"`
	CacheKeyValue       string                       `json:"cacheKey"`
	Inputs              []string                     `json:"inputs,omitempty"`
	CachePolicy         cachepolicy.Policy           `json:"cachePolicy"`
	Summary             project.SemanticGraphSummary `json:"summary"`
	ActionSummaries     []ActionSummary              `json:"actionSummaries,omitempty"`
	ArtifactSummaries   []ArtifactSummary            `json:"artifactSummaries,omitempty"`
	ProvenanceSummaries []ProvenanceSummary          `json:"provenanceSummaries,omitempty"`
	Snapshot            graph.Snapshot               `json:"snapshot"`
}

type DefaultBuilder struct{}

func NewStore(builder Builder) *Store {
	if builder == nil {
		builder = DefaultBuilder{}
	}
	return &Store{builder: builder}
}

func (s *Store) RegisterHook(h Hook) {
	if h == nil {
		return
	}
	s.hooks = append(s.hooks, h)
}

func (s *Store) LoadOrBuild(ctx context.Context, prj *project.Project) (*Model, error) {
	if prj == nil {
		return nil, fmt.Errorf("project is nil")
	}
	key, inputs, err := CacheKey(prj)
	if err != nil {
		return nil, err
	}
	path := cacheFilePath(prj.RootDir, key)
	if data, err := os.ReadFile(path); err == nil {
		var model Model
		if err := json.Unmarshal(data, &model); err == nil && model.SchemaVersion == schemaVersion {
			if state, stateErr := loadRuntimeState(prj.RootDir, key); stateErr == nil {
				model.Summary = enrichSummaryWithRuntime(model.Summary, state)
			}
			return &model, nil
		}
	}
	for _, hook := range s.hooks {
		if err := hook.BeforeBuild(ctx, prj); err != nil {
			return nil, err
		}
	}
	model, err := s.builder.Build(prj)
	if err != nil {
		return nil, err
	}
	model.SchemaVersion = schemaVersion
	model.Repo = prj.RootDir
	model.ProjectName = prj.Name
	model.CacheKeyValue = key
	model.Inputs = inputs
	model.CachePolicy = cachepolicy.DefaultPolicy()
	if err := writeModel(path, model); err != nil {
		return nil, err
	}
	if state, stateErr := loadRuntimeState(prj.RootDir, key); stateErr == nil {
		model.Summary = enrichSummaryWithRuntime(model.Summary, state)
	}
	for _, hook := range s.hooks {
		if err := hook.AfterBuild(ctx, model); err != nil {
			return nil, err
		}
	}
	return model, nil
}

func (DefaultBuilder) Build(prj *project.Project) (*Model, error) {
	g := prj.SemanticGraphDetailed()
	actionSummaries := buildActionSummaries(g)
	artifactSummaries := buildArtifactSummaries(g)
	provenanceSummaries := buildProvenanceSummaries(g)
	return &Model{
		Summary:             enrichSummary(prj.SemanticGraphSummary(), actionSummaries, artifactSummaries, provenanceSummaries),
		ActionSummaries:     actionSummaries,
		ArtifactSummaries:   artifactSummaries,
		ProvenanceSummaries: provenanceSummaries,
		CachePolicy:         cachepolicy.DefaultPolicy(),
		Snapshot:            g.Snapshot(),
	}, nil
}

func (m *Model) CacheKey() string {
	if m == nil {
		return ""
	}
	return m.CacheKeyValue
}

func (m *Model) Graph() (*graph.Graph, error) {
	if m == nil {
		return graph.New(), nil
	}
	return graph.FromSnapshot(m.Snapshot)
}

func (m *Model) GraphSummary() project.SemanticGraphSummary {
	if m == nil {
		return project.SemanticGraphSummary{}
	}
	return m.Summary
}

func (m *Model) Action(id graph.ActionID) (graph.Action, bool) {
	g, err := m.Graph()
	if err != nil {
		return graph.Action{}, false
	}
	return g.Action(id)
}

func (m *Model) ActionInputs(id graph.ActionID) []graph.Artifact {
	g, err := m.Graph()
	if err != nil {
		return nil
	}
	return g.ActionInputs(id)
}

func (m *Model) ActionOutputs(id graph.ActionID) []graph.Artifact {
	g, err := m.Graph()
	if err != nil {
		return nil
	}
	return g.ActionOutputs(id)
}

func (m *Model) ActionsForModule(path string) []graph.Action {
	mod, ok := m.Module(path)
	if !ok {
		return nil
	}
	g, err := m.Graph()
	if err != nil {
		return nil
	}
	return g.ActionsForModule(graph.LogicalModuleID(mod.ID))
}

func (m *Model) Module(path string) (project.SemanticModuleSummary, bool) {
	if m == nil {
		return project.SemanticModuleSummary{}, false
	}
	for _, mod := range m.Summary.Modules {
		if mod.Path == path {
			return mod, true
		}
	}
	return project.SemanticModuleSummary{}, false
}

func (m *Model) Variant(modulePath, variantName string) (project.SemanticVariantSummary, bool) {
	mod, ok := m.Module(modulePath)
	if !ok {
		return project.SemanticVariantSummary{}, false
	}
	for _, variant := range mod.Variants {
		if variant.Name == variantName {
			return variant, true
		}
	}
	return project.SemanticVariantSummary{}, false
}

func (m *Model) LastCacheProbeForAction(id graph.ActionID) (responsepayload.CacheProbe, bool) {
	if m == nil {
		return responsepayload.CacheProbe{}, false
	}
	for _, mod := range m.Summary.Modules {
		for _, variant := range mod.Variants {
			for _, action := range variant.Actions {
				if action.ID == id.String() && action.LastCacheProbe != nil && action.LastCacheProbe.State != "" {
					return *action.LastCacheProbe, true
				}
			}
		}
	}
	return responsepayload.CacheProbe{}, false
}

func (m *Model) ResolvedVariant(modulePath, variantName string) (project.ResolvedVariant, bool) {
	mod, ok := m.Module(modulePath)
	if !ok {
		return project.ResolvedVariant{}, false
	}
	for _, variant := range mod.Variants {
		if variant.Name != variantName {
			continue
		}
		provenance, _ := m.ProvenanceSummaryForVariant(modulePath, variantName)
		return project.ResolvedVariant{
			Name:           variant.Name,
			DeclaredName:   variant.DeclaredName,
			CoordinateName: variant.CoordinateName,
			ModulePath:     modulePath,
			DisplayName:    variant.DisplayName,
			Compatibility: project.VariantCompatibility{
				VariantName:    variant.Compatibility.VariantName,
				CoordinateName: variant.Compatibility.CoordinateName,
				DisplayName:    variant.Compatibility.DisplayName,
				SourceSetOrder: append([]string(nil), variant.Compatibility.SourceSetOrder...),
				SourceSetNames: append([]string(nil), variant.Compatibility.SourceSetNames...),
				TaskAliases:    append([]string(nil), variant.Compatibility.TaskAliases...),
				ModelSelectors: append([]string(nil), variant.Compatibility.ModelSelectors...),
				SyncFragments:  append([]string(nil), variant.Compatibility.SyncFragments...),
			},
			Coordinate: project.VariantCoordinate{
				ModulePath: modulePath,
				Name:       variant.Name,
				BuildType:  variant.BuildType,
				Flavors:    append([]string(nil), variant.Flavors...),
			},
			Config: project.BuildType{
				Name:                variant.Name,
				BaseBuildType:       variant.BuildType,
				Flavors:             append([]string(nil), variant.Flavors...),
				ApplicationID:       variant.ApplicationID,
				ApplicationIDSuffix: variant.ApplicationIDSuffix,
				VersionCode:         variant.VersionCode,
				VersionName:         variant.VersionName,
				VersionNameSuffix:   variant.VersionNameSuffix,
				MinSDK:              variant.MinSDK,
				TargetSDK:           variant.TargetSDK,
				Optimization:        variant.Optimization,
				SigningConfig:       variant.SigningConfig,
				ProguardFiles:       append([]string(nil), variant.ProguardFiles...),
			},
			ModuleType:                mod.Kind,
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
			MissingDimensions:         cloneStrategies(variant.MissingDimensions),
			Optimization:              variant.Optimization,
			ProguardFiles:             append([]string(nil), variant.ProguardFiles...),
			ConsumerProguardFiles:     append([]string(nil), mod.ConsumerProguardFiles...),
			SigningConfig:             variant.SigningConfig,
			SigningConfigured:         variant.SigningConfigured,
			MinifyEnabled:             variant.MinifyEnabled,
			ShrinkResources:           variant.ShrinkResources,
			Installable:               variant.Installable,
			Testable:                  variant.Testable,
			Debuggable:                variant.Debuggable,
			MaterializationID:         variant.Materialization.ID,
			ArtifactSnapshotID:        variant.Materialization.ArtifactSnapshotID,
			ClasspathSnapshotIDs:      append([]string(nil), variant.Materialization.ClasspathSnapshotIDs...),
			SourceRoots:               append([]string(nil), variant.Materialization.SourceRoots...),
			ManifestPaths:             append([]string(nil), provenance.ManifestPaths...),
			BackingArtifactID:         variant.Materialization.BackingArtifactID,
			BackingArtifactPath:       variant.Materialization.BackingArtifactPath,
			ProducedArtifactIDs:       append([]string(nil), variant.Materialization.ProducedArtifactIDs...),
			ProducedArtifactPaths:     append([]string(nil), variant.Materialization.ProducedArtifactPaths...),
			ProducedArtifacts:         toResolvedVariantArtifacts(variant.Materialization.Artifacts),
			ProducedArtifactKinds:     append([]string(nil), variant.Materialization.ProducedArtifactKinds...),
			InstallArtifactID:         variant.Materialization.InstallArtifactID,
			InstallArtifactPath:       variant.Materialization.InstallArtifactPath,
			ResourceArtifactIDs:       append([]string(nil), variant.Materialization.ResourceArtifactIDs...),
			ResourceArtifactPaths:     append([]string(nil), variant.Materialization.ResourceArtifactPaths...),
			ManifestArtifactIDs:       append([]string(nil), variant.Materialization.ManifestArtifactIDs...),
			ManifestArtifactPaths:     append([]string(nil), variant.Materialization.ManifestArtifactPaths...),
			SourceSetOrder:            append([]string(nil), variant.SourceSetOrder...),
			SourceSetNames:            append([]string(nil), variant.SourceSetNames...),
			TaskAliases:               append([]string(nil), variant.TaskAliases...),
			ModelSelectors:            append([]string(nil), variant.ModelSelectors...),
			SyncFragments:             append([]string(nil), variant.SyncFragments...),
			InstallTask:               variant.InstallTask,
			UninstallTask:             variant.UninstallTask,
		}, true
	}
	return project.ResolvedVariant{}, false
}

func (m *Model) ResolvedVariants(modulePath string) ([]project.ResolvedVariant, error) {
	mod, ok := m.Module(modulePath)
	if !ok {
		return nil, fmt.Errorf("module %s not found", modulePath)
	}
	if len(mod.Variants) == 0 {
		taskAliases := []string{"assembleDebug", "compileDebugSources", "testDebugUnitTest", "compileDebugUnitTestSources", "compileDebugAndroidTestSources", "assembleDebugAndroidTest"}
		if mod.Kind == "android-application" {
			taskAliases = []string{"assembleDebug", "compileDebugSources", "installDebug", "assembleDebugAndroidTest", "compileDebugAndroidTestSources", "compileDebugUnitTestSources", "testDebugUnitTest"}
		}
		return []project.ResolvedVariant{{
			Name:           "debug",
			CoordinateName: "debug",
			ModulePath:     modulePath,
			DisplayName:    "Debug",
			Compatibility: project.VariantCompatibility{
				VariantName:    "debug",
				CoordinateName: "debug",
				DisplayName:    "Debug",
				SourceSetOrder: []string{"main", "debug"},
				SourceSetNames: []string{"main", "debug"},
				TaskAliases:    append([]string(nil), taskAliases...),
				ModelSelectors: []string{modulePath, "debug", modulePath + "#debug", "buildType:debug"},
				SyncFragments:  []string{"module:" + modulePath, "variant:debug", "buildType:debug", "sourceSet:main", "sourceSet:debug"},
			},
			Coordinate: project.VariantCoordinate{
				ModulePath: modulePath,
				Name:       "debug",
				BuildType:  "debug",
			},
			Config:         project.BuildType{Name: "debug", BaseBuildType: "debug"},
			Debuggable:     true,
			SourceSetOrder: []string{"main", "debug"},
			TaskAliases:    taskAliases,
			ModelSelectors: []string{modulePath, "debug", modulePath + "#debug", "buildType:debug"},
			InstallTask:    resolvedVariantPrimaryTaskAlias(mod.Kind, "debug", "install"),
			UninstallTask:  resolvedVariantPrimaryTaskAlias(mod.Kind, "debug", "uninstall"),
		}}, nil
	}
	out := make([]project.ResolvedVariant, 0, len(mod.Variants))
	for _, variant := range mod.Variants {
		provenance, _ := m.ProvenanceSummaryForVariant(modulePath, variant.Name)
		out = append(out, project.ResolvedVariant{
			Name:           variant.Name,
			DeclaredName:   variant.DeclaredName,
			CoordinateName: variant.CoordinateName,
			ModulePath:     modulePath,
			DisplayName:    variant.DisplayName,
			Compatibility: project.VariantCompatibility{
				VariantName:    variant.Compatibility.VariantName,
				CoordinateName: variant.Compatibility.CoordinateName,
				DisplayName:    variant.Compatibility.DisplayName,
				SourceSetOrder: append([]string(nil), variant.Compatibility.SourceSetOrder...),
				SourceSetNames: append([]string(nil), variant.Compatibility.SourceSetNames...),
				TaskAliases:    append([]string(nil), variant.Compatibility.TaskAliases...),
				ModelSelectors: append([]string(nil), variant.Compatibility.ModelSelectors...),
				SyncFragments:  append([]string(nil), variant.Compatibility.SyncFragments...),
			},
			Coordinate: project.VariantCoordinate{
				ModulePath: modulePath,
				Name:       variant.Name,
				BuildType:  variant.BuildType,
				Flavors:    append([]string(nil), variant.Flavors...),
			},
			Config: project.BuildType{
				Name:                variant.Name,
				BaseBuildType:       variant.BuildType,
				Flavors:             append([]string(nil), variant.Flavors...),
				ApplicationID:       variant.ApplicationID,
				ApplicationIDSuffix: variant.ApplicationIDSuffix,
				VersionCode:         variant.VersionCode,
				VersionName:         variant.VersionName,
				VersionNameSuffix:   variant.VersionNameSuffix,
				MinSDK:              variant.MinSDK,
				TargetSDK:           variant.TargetSDK,
				Optimization:        variant.Optimization,
				SigningConfig:       variant.SigningConfig,
				ProguardFiles:       append([]string(nil), variant.ProguardFiles...),
			},
			ModuleType:                mod.Kind,
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
			MissingDimensions:         cloneStrategies(variant.MissingDimensions),
			Optimization:              variant.Optimization,
			ProguardFiles:             append([]string(nil), variant.ProguardFiles...),
			ConsumerProguardFiles:     append([]string(nil), mod.ConsumerProguardFiles...),
			SigningConfig:             variant.SigningConfig,
			SigningConfigured:         variant.SigningConfigured,
			MinifyEnabled:             variant.MinifyEnabled,
			ShrinkResources:           variant.ShrinkResources,
			Installable:               variant.Installable,
			Testable:                  variant.Testable,
			Debuggable:                variant.Debuggable,
			MaterializationID:         variant.Materialization.ID,
			ArtifactSnapshotID:        variant.Materialization.ArtifactSnapshotID,
			ClasspathSnapshotIDs:      append([]string(nil), variant.Materialization.ClasspathSnapshotIDs...),
			SourceRoots:               append([]string(nil), variant.Materialization.SourceRoots...),
			ManifestPaths:             append([]string(nil), provenance.ManifestPaths...),
			BackingArtifactID:         variant.Materialization.BackingArtifactID,
			BackingArtifactPath:       variant.Materialization.BackingArtifactPath,
			ProducedArtifactIDs:       append([]string(nil), variant.Materialization.ProducedArtifactIDs...),
			ProducedArtifactPaths:     append([]string(nil), variant.Materialization.ProducedArtifactPaths...),
			ProducedArtifacts:         toResolvedVariantArtifacts(variant.Materialization.Artifacts),
			ProducedArtifactKinds:     append([]string(nil), variant.Materialization.ProducedArtifactKinds...),
			InstallArtifactID:         variant.Materialization.InstallArtifactID,
			InstallArtifactPath:       variant.Materialization.InstallArtifactPath,
			ResourceArtifactIDs:       append([]string(nil), variant.Materialization.ResourceArtifactIDs...),
			ResourceArtifactPaths:     append([]string(nil), variant.Materialization.ResourceArtifactPaths...),
			ManifestArtifactIDs:       append([]string(nil), variant.Materialization.ManifestArtifactIDs...),
			ManifestArtifactPaths:     append([]string(nil), variant.Materialization.ManifestArtifactPaths...),
			SourceSetOrder:            append([]string(nil), variant.SourceSetOrder...),
			SourceSetNames:            append([]string(nil), variant.SourceSetNames...),
			TaskAliases:               append([]string(nil), variant.TaskAliases...),
			ModelSelectors:            append([]string(nil), variant.ModelSelectors...),
			SyncFragments:             append([]string(nil), variant.SyncFragments...),
			InstallTask:               variant.InstallTask,
			UninstallTask:             variant.UninstallTask,
		})
	}
	return out, nil
}

func toResolvedVariantArtifacts(artifacts []project.SemanticArtifactSummary) []project.ResolvedVariantArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]project.ResolvedVariantArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, project.ResolvedVariantArtifact{
			ID:                 artifact.ID,
			Kind:               artifact.Kind,
			Path:               artifact.Path,
			ProducedByActionID: artifact.ProducedByActionID,
		})
	}
	return out
}

func resolvedVariantPrimaryTaskAlias(moduleType, variantName, prefix string) string {
	if moduleType != "android-application" {
		return ""
	}
	suffix := resolvedVariantTaskNameSuffix(variantName)
	switch strings.TrimSpace(prefix) {
	case "install":
		return "install" + suffix
	case "uninstall":
		return "uninstall" + suffix
	default:
		return ""
	}
}

func resolvedVariantTaskNameSuffix(variantName string) string {
	if variantName == "" {
		return ""
	}
	if len(variantName) == 1 {
		return strings.ToUpper(variantName)
	}
	return strings.ToUpper(variantName[:1]) + variantName[1:]
}

func (m *Model) VariantNames(modulePath string) ([]string, error) {
	mod, ok := m.Module(modulePath)
	if !ok {
		return nil, fmt.Errorf("module %s not found", modulePath)
	}
	if len(mod.Variants) == 0 {
		return []string{"debug"}, nil
	}
	out := make([]string, 0, len(mod.Variants))
	for _, variant := range mod.Variants {
		if name := strings.TrimSpace(variant.Name); name != "" {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return []string{"debug"}, nil
	}
	return out, nil
}

func CacheKey(prj *project.Project) (string, []string, error) {
	inputs := modelInputs(prj)
	sum := sha256.New()
	sum.Write([]byte(strings.TrimSpace(prj.Name)))
	sum.Write([]byte{0})
	sum.Write([]byte(strings.TrimSpace(prj.RootDir)))
	sum.Write([]byte{0})
	for _, mod := range prj.Modules {
		sum.Write([]byte(mod.Path))
		sum.Write([]byte{0})
		sum.Write([]byte(mod.Type))
		sum.Write([]byte{0})
		var variants []string
		for name := range mod.BuildTypes {
			variants = append(variants, name)
		}
		sort.Strings(variants)
		for _, name := range variants {
			sum.Write([]byte(name))
			sum.Write([]byte{0})
		}
	}
	for _, input := range inputs {
		sum.Write([]byte(input))
		sum.Write([]byte{0})
		data, err := os.ReadFile(input)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", nil, err
		}
		sum.Write(data)
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil)), inputs, nil
}

func cacheFilePath(root, key string) string {
	return filepath.Join(root, ".grit", "configmodel", key+".json")
}

func cloneStrategies(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func writeModel(path string, model *Model) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func modelInputs(prj *project.Project) []string {
	if prj == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var inputs []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		inputs = append(inputs, path)
	}
	add(prj.SettingsFile)
	add(prj.RootBuildFile)
	for _, path := range prj.VersionCatalogs {
		add(path)
	}
	for _, mod := range prj.Modules {
		add(mod.BuildFile)
	}
	sort.Strings(inputs)
	return inputs
}

func semanticGraphSummary(g *graph.Graph) project.SemanticGraphSummary {
	summary := project.SemanticGraphSummary{
		NodeCount: g.NodeCount(),
		EdgeCount: g.EdgeCount(),
	}
	for _, module := range g.LogicalModules() {
		moduleSummary := project.SemanticModuleSummary{
			ID:                module.ID.String(),
			Name:              module.Name,
			Path:              module.Path,
			Dir:               module.Dir,
			Kind:              string(module.Kind),
			Tasks:             moduleTaskNames(g, module.ID),
			DependsOn:         moduleDependencyPathsFromGraph(g, module.ID),
			DependencyClosure: moduleDependencyClosureFromGraph(g, module.ID),
		}
		for _, variant := range g.ModuleVariants(module.ID) {
			variantSummary := project.SemanticVariantSummary{
				ID:                   variant.ID.String(),
				Name:                 variant.Name,
				BuildType:            variant.BuildType,
				Flavors:              append([]string(nil), variant.Flavors...),
				DependsOnVariants:    variantDependencyNamesFromGraph(g, variant.ID),
				DependencyProvenance: variantDependencyProvenanceFromGraph(g, variant.ID),
				TaskProjections:      variantTaskNames(g, variant.ID),
				Actions:              variantActionSummariesFromGraph(g, variant.ID),
			}
			materializations := g.VariantMaterializations(variant.ID)
			if len(materializations) > 0 {
				m := materializations[0]
				variantSummary.Materialization = project.SemanticMaterializationSummary{
					ID:                   m.ID.String(),
					Mode:                 m.Attributes["mode"],
					ArtifactSnapshotID:   m.ArtifactSnapshotID,
					ClasspathSnapshotIDs: append([]string(nil), m.ClasspathSnapshotIDs...),
					SourceRoots:          append([]string(nil), m.SourceRoots...),
					BackingArtifactID:    m.BackingArtifactID.String(),
					ProducedArtifactIDs:  materializationProducedArtifactIDs(g, m.ID),
					ConsumingActionIDs:   materializationConsumingActionIDs(g, m.ID),
					Artifacts:            materializationArtifactSummaries(g, m.ID),
				}
			}
			moduleSummary.Variants = append(moduleSummary.Variants, variantSummary)
		}
		summary.Modules = append(summary.Modules, moduleSummary)
	}
	sort.Slice(summary.Modules, func(i, j int) bool { return summary.Modules[i].ID < summary.Modules[j].ID })
	return summary
}

func enrichSummary(summary project.SemanticGraphSummary, actions []ActionSummary, artifacts []ArtifactSummary, provenance []ProvenanceSummary) project.SemanticGraphSummary {
	actionByVariant := map[string][]ActionSummary{}
	for _, action := range actions {
		actionByVariant[action.VariantID] = append(actionByVariant[action.VariantID], action)
	}
	artifactByMaterialization := map[string][]ArtifactSummary{}
	for _, artifact := range artifacts {
		artifactByMaterialization[artifact.MaterializationID] = append(artifactByMaterialization[artifact.MaterializationID], artifact)
	}
	provenanceByMaterialization := map[string]ProvenanceSummary{}
	for _, item := range provenance {
		provenanceByMaterialization[item.MaterializationID] = item
	}
	for mi := range summary.Modules {
		for vi := range summary.Modules[mi].Variants {
			variant := &summary.Modules[mi].Variants[vi]
			if actionItems := actionByVariant[variant.ID]; len(actionItems) > 0 {
				variant.Actions = make([]project.SemanticActionSummary, 0, len(actionItems))
				for _, action := range actionItems {
					variant.Actions = append(variant.Actions, project.SemanticActionSummary{
						ID:            action.ID,
						Name:          action.Name,
						Operation:     action.Operation,
						WorkerClass:   action.WorkerClass,
						ResourceClass: action.ResourceClass,
						CacheKey:      action.CacheKey,
						Inputs:        append([]string(nil), action.Inputs...),
						Outputs:       append([]string(nil), action.Outputs...),
					})
				}
			}
			mat := &summary.Modules[mi].Variants[vi].Materialization
			if mat.ID == "" {
				continue
			}
			if prov, ok := provenanceByMaterialization[mat.ID]; ok {
				mat.BackingArtifactID = prov.BackingArtifactID
				mat.ProducedArtifactIDs = append([]string(nil), prov.ProducedArtifactIDs...)
				mat.ConsumingActionIDs = append([]string(nil), prov.ConsumingActionIDs...)
			}
			if artifactItems := artifactByMaterialization[mat.ID]; len(artifactItems) > 0 {
				mat.Artifacts = make([]project.SemanticArtifactSummary, 0, len(artifactItems))
				for _, artifact := range artifactItems {
					mat.Artifacts = append(mat.Artifacts, project.SemanticArtifactSummary{
						ID:                 artifact.ID,
						Kind:               artifact.Kind,
						Path:               artifact.Path,
						ProducedByActionID: artifact.ProducedByActionID,
					})
				}
			}
		}
	}
	return summary
}

func enrichSummaryWithRuntime(summary project.SemanticGraphSummary, state *RuntimeState) project.SemanticGraphSummary {
	if state == nil || len(state.ActionCacheProbes) == 0 {
		return summary
	}
	for mi := range summary.Modules {
		for vi := range summary.Modules[mi].Variants {
			for ai := range summary.Modules[mi].Variants[vi].Actions {
				action := &summary.Modules[mi].Variants[vi].Actions[ai]
				if probe, ok := state.ActionCacheProbes[action.ID]; ok {
					clone := probe
					action.LastCacheProbe = &clone
				}
			}
		}
	}
	return summary
}

func moduleTaskNames(g *graph.Graph, moduleID graph.LogicalModuleID) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, action := range g.ActionsForModule(moduleID) {
		if strings.TrimSpace(action.Name) == "" {
			continue
		}
		if _, ok := seen[action.Name]; ok {
			continue
		}
		seen[action.Name] = struct{}{}
		out = append(out, action.Name)
	}
	sort.Strings(out)
	return out
}

func moduleDependencyPathsFromGraph(g *graph.Graph, moduleID graph.LogicalModuleID) []string {
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

func moduleDependencyClosureFromGraph(g *graph.Graph, moduleID graph.LogicalModuleID) []string {
	seen := map[graph.NodeRef]struct{}{}
	queue := []graph.NodeRef{moduleID.Ref()}
	var out []string
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
			mod, ok := g.LogicalModule(graph.LogicalModuleID(dep.ID))
			if !ok || strings.TrimSpace(mod.Path) == "" {
				continue
			}
			out = append(out, mod.Path)
		}
	}
	sort.Strings(out)
	return out
}

func variantDependencyNamesFromGraph(g *graph.Graph, variantID graph.VariantID) []string {
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

func variantDependencyProvenanceFromGraph(g *graph.Graph, variantID graph.VariantID) []project.SemanticDependencyProvenance {
	if g == nil {
		return nil
	}
	var out []project.SemanticDependencyProvenance
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
		if module, ok := g.LogicalModule(upstream.ModuleID); ok {
			modulePath = module.Path
		}
		out = append(out, project.SemanticDependencyProvenance{
			ModulePath:      modulePath,
			VariantName:     upstream.Name,
			DependencyLevel: edge.Attributes["dependencyLevel"],
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

func uniqueDependencyProvenance(values []project.SemanticDependencyProvenance) []project.SemanticDependencyProvenance {
	if len(values) == 0 {
		return nil
	}
	out := make([]project.SemanticDependencyProvenance, 0, len(values))
	for _, value := range values {
		if len(out) > 0 && out[len(out)-1] == value {
			continue
		}
		out = append(out, value)
	}
	return out
}

func variantTaskNames(g *graph.Graph, variantID graph.VariantID) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, action := range g.ActionsForVariant(variantID) {
		if strings.TrimSpace(action.Name) == "" {
			continue
		}
		if _, ok := seen[action.Name]; ok {
			continue
		}
		seen[action.Name] = struct{}{}
		out = append(out, action.Name)
	}
	sort.Strings(out)
	return out
}

func variantActionSummariesFromGraph(g *graph.Graph, variantID graph.VariantID) []project.SemanticActionSummary {
	var out []project.SemanticActionSummary
	for _, action := range g.ActionsForVariant(variantID) {
		out = append(out, project.SemanticActionSummary{
			ID:        action.ID.String(),
			Name:      action.Name,
			Operation: action.Attributes["operation"],
			Inputs:    artifactIDs(action.Inputs),
			Outputs:   artifactIDs(action.Outputs),
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

func materializationArtifactSummaries(g *graph.Graph, materializationID graph.MaterializationID) []project.SemanticArtifactSummary {
	var out []project.SemanticArtifactSummary
	for _, artifact := range g.MaterializationArtifacts(materializationID) {
		out = append(out, project.SemanticArtifactSummary{
			ID:                 artifact.ID.String(),
			Kind:               string(artifact.Kind),
			Path:               artifact.Path,
			ProducedByActionID: artifact.ProducedByActionID.String(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func materializationProducedArtifactIDs(g *graph.Graph, materializationID graph.MaterializationID) []string {
	var out []string
	for _, artifact := range g.MaterializationArtifacts(materializationID) {
		out = append(out, artifact.ID.String())
	}
	sort.Strings(out)
	return out
}

func materializationConsumingActionIDs(g *graph.Graph, materializationID graph.MaterializationID) []string {
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
