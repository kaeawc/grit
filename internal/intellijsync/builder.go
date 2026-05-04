package intellijsync

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/classpath"
	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/intellijtask"
	"github.com/kaeawc/grit/internal/materialization"
	"github.com/kaeawc/grit/internal/project"
)

type Builder struct{}

func (Builder) Build(cfg *configmodel.Model, prj *project.Project) (*Model, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config model is nil")
	}
	if prj == nil {
		return nil, fmt.Errorf("project is nil")
	}
	g, err := cfg.Graph()
	if err != nil {
		return nil, err
	}
	out := &Model{
		Repo:        prj.RootDir,
		ProjectName: prj.Name,
		CacheKey:    cfg.CacheKey(),
		Project:     buildProject(prj),
	}
	for _, mod := range prj.Modules {
		out.Modules = append(out.Modules, buildModule(cfg, g, mod))
	}
	sort.Slice(out.Modules, func(i, j int) bool {
		return out.Modules[i].Path < out.Modules[j].Path
	})
	return out, nil
}

func buildProject(prj *project.Project) Project {
	if prj == nil {
		return Project{}
	}
	out := Project{
		Name:               prj.Name,
		RootDir:            prj.RootDir,
		SettingsFile:       prj.SettingsFile,
		RootBuildFile:      prj.RootBuildFile,
		GradleProperties:   cloneStringMap(prj.GradleProperties),
		VersionCatalogs:    append([]string(nil), prj.VersionCatalogs...),
		RecommendedBackend: prj.RecommendedBackend,
	}
	for _, repo := range prj.Repositories {
		out.Repositories = append(out.Repositories, buildRepository(repo))
	}
	sort.Slice(out.Repositories, func(i, j int) bool {
		return out.Repositories[i].Name < out.Repositories[j].Name
	})
	sort.Strings(out.RootPlugins)
	out.RootPlugins = append(out.RootPlugins, prj.RootPlugins...)
	sort.Strings(out.RootPlugins)
	return out
}

func buildRepository(repo project.Repository) Repository {
	return Repository{
		Name:              repo.Name,
		Kind:              repo.Kind,
		URL:               repo.URL,
		Scope:             repo.Scope,
		Exclusive:         repo.Exclusive,
		Priority:          repo.Priority,
		Origin:            repo.Origin,
		OfflineAllowed:    repo.OfflineAllowed,
		IncludeGroups:     append([]string(nil), repo.IncludeGroups...),
		IncludeGroupRegex: append([]string(nil), repo.IncludeGroupRegex...),
		ExcludeGroups:     append([]string(nil), repo.ExcludeGroups...),
		ExcludeGroupRegex: append([]string(nil), repo.ExcludeGroupRegex...),
		IncludeModules:    append([]string(nil), repo.IncludeModules...),
		ExcludeModules:    append([]string(nil), repo.ExcludeModules...),
	}
}

func buildModule(cfg *configmodel.Model, g *graph.Graph, mod project.Module) Module {
	out := Module{
		Path: mod.Path,
		Name: strings.TrimPrefix(mod.Path, ":"),
		Identity: Identity{
			ModulePath:     mod.Path,
			IDEModuleID:    ideModuleID(mod.Path),
			ModelSelectors: []string{mod.Path},
		},
		Dir:                       mod.Dir,
		BuildFile:                 mod.BuildFile,
		Kind:                      mod.Type,
		Namespace:                 mod.Namespace,
		ApplicationID:             mod.ApplicationID,
		CompileSDK:                mod.CompileSDK,
		BuildToolsVersion:         mod.BuildToolsVersion,
		MinSDK:                    mod.MinSDK,
		TargetSDK:                 mod.TargetSDK,
		TestInstrumentationRunner: mod.TestInstrumentationRunner,
		SourceFileCount:           mod.SourceFileCount,
		UnitTestFileCount:         mod.UnitTestFileCount,
		AndroidTestFileCount:      mod.AndroidTestFileCount,
		UsesCompose:               mod.UsesCompose,
		UsesKotlinSerialization:   mod.UsesKotlinSerialization,
		UsesMetro:                 mod.UsesMetro,
		UsesWire:                  mod.UsesWire,
		KotlinFreeCompilerArgs:    append([]string(nil), mod.KotlinFreeCompilerArgs...),
		LintDisabledChecks:        append([]string(nil), mod.LintDisabledChecks...),
		ConsumerProguardFiles:     append([]string(nil), mod.ConsumerProguardFiles...),
		DefaultTasks:              append([]string(nil), mod.DefaultTasks()...),
		Tasks:                     buildTasks(mod.Tasks()),
	}
	moduleID, ok := logicalModuleIDForPath(g, mod.Path)
	if !ok {
		if variants := mod.Variants(); len(variants) > 0 {
			for _, variant := range variants {
				out.Variants = append(out.Variants, buildVariantFromProject(cfg, mod, variant))
			}
		}
		sort.Slice(out.Variants, func(i, j int) bool { return out.Variants[i].Name < out.Variants[j].Name })
		out.TaskCatalog = buildModuleTaskCatalog(mod, out.Tasks)
		return out
	}
	out.Identity.GraphModuleID = moduleID.String()
	if logical, ok := g.LogicalModule(moduleID); ok {
		out.GraphKind = string(logical.Kind)
	}
	out.Dependencies = buildModuleDependencies(g, moduleID)
	for _, variant := range g.ModuleVariants(moduleID) {
		out.Variants = append(out.Variants, buildVariant(cfg, g, mod, variant))
	}
	sort.Slice(out.Variants, func(i, j int) bool { return out.Variants[i].Name < out.Variants[j].Name })
	out.TaskCatalog = buildModuleTaskCatalog(mod, out.Tasks)
	return out
}

func buildTasks(tasks []project.Task) []Task {
	out := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, Task{
			Name:        task.Name,
			Category:    task.Category,
			Description: task.Description,
			Supported:   task.Supported,
		})
	}
	return out
}

func buildModuleTaskCatalog(mod project.Module, tasks []Task) []TaskCatalog {
	settings := intellijtask.Settings{
		ModulePath: mod.Path,
		ModuleKind: moduleKindForTaskCatalog(mod.Type),
	}
	taskMeta := taskCatalogMetadata(tasks)
	seen := map[string]struct{}{}
	var out []TaskCatalog
	for _, rawTask := range mod.DefaultTasks() {
		_, taskName := splitCatalogTaskName(rawTask)
		if taskName == "" {
			continue
		}
		seen[taskName] = struct{}{}
		out = append(out, buildTaskCatalogEntry(settings, taskName, "", taskMeta[taskName]))
	}
	for _, task := range tasks {
		if _, ok := seen[task.Name]; ok {
			continue
		}
		out = append(out, buildTaskCatalogEntry(settings, task.Name, "", task))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RawName < out[j].RawName })
	return out
}

func buildVariantTaskCatalog(mod project.Module, variantName string, aliases []string, tasks []Task) []TaskCatalog {
	settings := intellijtask.Settings{
		ModulePath: mod.Path,
		ModuleKind: moduleKindForTaskCatalog(mod.Type),
	}
	taskMeta := taskCatalogMetadata(tasks)
	seen := map[string]struct{}{}
	var out []TaskCatalog
	for _, rawTask := range aliases {
		rawTask = strings.TrimSpace(rawTask)
		if rawTask == "" {
			continue
		}
		if _, ok := seen[rawTask]; ok {
			continue
		}
		seen[rawTask] = struct{}{}
		out = append(out, buildTaskCatalogEntry(settings, rawTask, variantName, taskMeta[rawTask]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RawName < out[j].RawName })
	return out
}

func taskCatalogMetadata(tasks []Task) map[string]Task {
	out := make(map[string]Task, len(tasks))
	for _, task := range tasks {
		out[task.Name] = task
	}
	return out
}

func buildTaskCatalogEntry(settings intellijtask.Settings, rawTask, fallbackVariant string, task Task) TaskCatalog {
	entry := TaskCatalog{
		RawName:   rawTask,
		Category:  firstNonEmptyString(task.Category, inferTaskCatalogCategory(rawTask)),
		Supported: task.Supported,
	}
	resolved, err := (intellijtask.Request{Settings: intellijtask.Settings{
		ModulePath: settings.ModulePath,
		ModuleKind: settings.ModuleKind,
		TaskNames:  []string{rawTask},
	}}).Resolve()
	if err == nil && len(resolved) > 0 {
		entry.NormalizedCommand = resolved[0].Command
		entry.TargetVariant = firstNonEmptyString(resolved[0].RequestedVariant, fallbackVariant)
		entry.Supported = true
		entry.Runnable = true
	}
	if entry.TargetVariant == "" {
		entry.TargetVariant = strings.TrimSpace(fallbackVariant)
	}
	entry.Kind = inferTaskCatalogKind(rawTask, entry.NormalizedCommand)
	entry.Test = taskCatalogIsTest(entry.Kind, rawTask, entry.NormalizedCommand)
	entry.Install = taskCatalogIsInstall(entry.Kind, rawTask, entry.NormalizedCommand)
	return entry
}

func inferTaskCatalogCategory(rawTask string) string {
	lower := strings.ToLower(strings.TrimSpace(rawTask))
	switch {
	case strings.HasPrefix(lower, "test") || strings.Contains(lower, "unittest") || strings.Contains(lower, "androidtest"):
		return "verification"
	case strings.HasPrefix(lower, "install") || strings.HasPrefix(lower, "uninstall"):
		return "install"
	default:
		return "build"
	}
}

func inferTaskCatalogKind(rawTask, command string) string {
	lowerRaw := strings.ToLower(strings.TrimSpace(rawTask))
	lowerCommand := strings.ToLower(strings.TrimSpace(command))
	switch {
	case strings.Contains(lowerRaw, "androidtest") || strings.Contains(lowerCommand, "android-tests"):
		if strings.HasPrefix(lowerRaw, "install") || strings.HasPrefix(lowerCommand, "install") {
			return "android-test-install"
		}
		if strings.HasPrefix(lowerRaw, "uninstall") || strings.HasPrefix(lowerCommand, "uninstall") {
			return "android-test-uninstall"
		}
		return "android-test"
	case strings.Contains(lowerRaw, "unittest") || strings.Contains(lowerCommand, "unit"):
		if strings.HasPrefix(lowerRaw, "compile") || strings.HasPrefix(lowerCommand, "compile") {
			return "unit-test-compile"
		}
		return "unit-test"
	case strings.HasPrefix(lowerRaw, "install") || strings.HasPrefix(lowerCommand, "install"):
		return "install"
	case strings.HasPrefix(lowerRaw, "uninstall") || strings.HasPrefix(lowerCommand, "uninstall"):
		return "uninstall"
	case strings.HasPrefix(lowerRaw, "assemble") || strings.HasPrefix(lowerCommand, "assemble"):
		return "assemble"
	case strings.HasPrefix(lowerRaw, "compile") || strings.HasPrefix(lowerCommand, "compile"):
		return "compile"
	case strings.HasPrefix(lowerRaw, "test") || strings.HasPrefix(lowerCommand, "test"):
		return "test"
	case lowerRaw == "build" || lowerCommand == "build":
		return "build"
	case lowerRaw == "check" || lowerCommand == "check":
		return "check"
	default:
		return "task"
	}
}

func taskCatalogIsTest(kind, rawTask, command string) bool {
	lowerRaw := strings.ToLower(strings.TrimSpace(rawTask))
	lowerCommand := strings.ToLower(strings.TrimSpace(command))
	return strings.Contains(kind, "test") || strings.HasPrefix(lowerRaw, "test") || strings.Contains(lowerRaw, "unittest") || strings.Contains(lowerRaw, "androidtest") || strings.HasPrefix(lowerCommand, "test")
}

func taskCatalogIsInstall(kind, rawTask, command string) bool {
	lowerRaw := strings.ToLower(strings.TrimSpace(rawTask))
	lowerCommand := strings.ToLower(strings.TrimSpace(command))
	return strings.Contains(kind, "install") || strings.HasPrefix(lowerRaw, "install") || strings.HasPrefix(lowerCommand, "install")
}

func splitCatalogTaskName(task string) (string, string) {
	task = strings.TrimSpace(task)
	if task == "" {
		return "", ""
	}
	parts := strings.Split(task, ":")
	var filtered []string
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		return "", ""
	}
	if len(filtered) == 1 {
		return "", filtered[0]
	}
	return ":" + strings.Join(filtered[:len(filtered)-1], ":"), filtered[len(filtered)-1]
}

func buildVariantFromProject(cfg *configmodel.Model, mod project.Module, bt project.BuildType) Variant {
	buildType := bt.BaseBuildType
	if strings.TrimSpace(buildType) == "" {
		buildType = bt.Name
	}
	resolved := resolvedVariantForSync(cfg, mod, bt.Name)
	identity := buildIdentityMapping("", "", mod.Path, resolved)
	out := Variant{
		Name:           bt.Name,
		Identity:       identity,
		DeclaredName:   resolved.DeclaredName,
		CoordinateName: resolved.CoordinateName,
		DisplayName:    resolved.DisplayName,
		Compatibility: Compatibility{
			VariantName:    resolved.Compatibility.VariantName,
			CoordinateName: resolved.Compatibility.CoordinateName,
			DisplayName:    resolved.Compatibility.DisplayName,
			SourceSetOrder: append([]string(nil), resolved.Compatibility.SourceSetOrder...),
			SourceSetNames: append([]string(nil), resolved.Compatibility.SourceSetNames...),
			TaskAliases:    append([]string(nil), resolved.Compatibility.TaskAliases...),
			ModelSelectors: append([]string(nil), resolved.Compatibility.ModelSelectors...),
			SyncFragments:  append([]string(nil), resolved.Compatibility.SyncFragments...),
		},
		BuildType:                 buildType,
		Flavors:                   append([]string(nil), bt.Flavors...),
		CompileSDK:                resolved.CompileSDK,
		ApplicationID:             resolved.ApplicationID,
		ApplicationIDSuffix:       resolved.ApplicationIDSuffix,
		VersionName:               resolved.VersionName,
		VersionNameSuffix:         resolved.VersionNameSuffix,
		MinSDK:                    resolved.MinSDK,
		TargetSDK:                 resolved.TargetSDK,
		TestInstrumentationRunner: resolved.TestInstrumentationRunner,
		ProguardFiles:             append([]string(nil), resolved.ProguardFiles...),
		ConsumerProguardFiles:     append([]string(nil), resolved.ConsumerProguardFiles...),
		SourceSetOrder:            append([]string(nil), resolved.SourceSetOrder...),
		SourceSetNames:            append([]string(nil), resolved.SourceSetNames...),
		TaskAliases:               append([]string(nil), resolved.TaskAliases...),
		ModelSelectors:            append([]string(nil), resolved.ModelSelectors...),
		SyncFragments:             append([]string(nil), resolved.SyncFragments...),
		Materialization:           buildMaterializationFromResolved(resolved),
	}
	out.TaskCatalog = buildVariantTaskCatalog(mod, out.Name, out.TaskAliases, buildTasks(mod.Tasks()))
	out.ContentRoots = buildContentRoots(mod, out, resolved)
	out.OrderEntries = VariantOrderEntries(out)
	out.Targets = buildTargets(resolved, out.Materialization, nil)
	return out
}

func buildVariant(cfg *configmodel.Model, g *graph.Graph, mod project.Module, variant graph.Variant) Variant {
	resolved := resolvedVariantForSync(cfg, mod, variant.Name)
	moduleID, _ := logicalModuleIDForPath(g, mod.Path)
	identity := buildIdentityMapping(moduleID.String(), variant.ID.String(), mod.Path, resolved)
	out := Variant{
		ID:             variant.ID.String(),
		Name:           variant.Name,
		Identity:       identity,
		DeclaredName:   resolved.DeclaredName,
		CoordinateName: resolved.CoordinateName,
		DisplayName:    resolved.DisplayName,
		Compatibility: Compatibility{
			VariantName:    resolved.Compatibility.VariantName,
			CoordinateName: resolved.Compatibility.CoordinateName,
			DisplayName:    resolved.Compatibility.DisplayName,
			SourceSetOrder: append([]string(nil), resolved.Compatibility.SourceSetOrder...),
			SourceSetNames: append([]string(nil), resolved.Compatibility.SourceSetNames...),
			TaskAliases:    append([]string(nil), resolved.Compatibility.TaskAliases...),
			ModelSelectors: append([]string(nil), resolved.Compatibility.ModelSelectors...),
			SyncFragments:  append([]string(nil), resolved.Compatibility.SyncFragments...),
		},
		BuildType:                 variant.BuildType,
		Flavors:                   append([]string(nil), variant.Flavors...),
		CompileSDK:                resolved.CompileSDK,
		ApplicationID:             resolved.ApplicationID,
		ApplicationIDSuffix:       resolved.ApplicationIDSuffix,
		VersionName:               resolved.VersionName,
		VersionNameSuffix:         resolved.VersionNameSuffix,
		MinSDK:                    resolved.MinSDK,
		TargetSDK:                 resolved.TargetSDK,
		TestInstrumentationRunner: resolved.TestInstrumentationRunner,
		ProguardFiles:             append([]string(nil), resolved.ProguardFiles...),
		ConsumerProguardFiles:     append([]string(nil), resolved.ConsumerProguardFiles...),
		SourceSetOrder:            append([]string(nil), resolved.SourceSetOrder...),
		SourceSetNames:            append([]string(nil), resolved.SourceSetNames...),
		TaskAliases:               append([]string(nil), resolved.TaskAliases...),
		ModelSelectors:            append([]string(nil), resolved.ModelSelectors...),
		SyncFragments:             append([]string(nil), resolved.SyncFragments...),
		Materialization:           buildMaterializationFromResolved(resolved),
	}
	materials := g.VariantMaterializations(variant.ID)
	if len(materials) > 0 {
		out.Materialization = mergeMaterialization(out.Materialization, buildMaterialization(cfg, materials[0]))
	}
	out.Dependencies = buildVariantDependencies(g, variant)
	out.Actions = buildActionsForVariant(g, mod.Path, variant)
	out.TaskCatalog = buildVariantTaskCatalog(mod, out.Name, out.TaskAliases, buildTasks(mod.Tasks()))
	out.ContentRoots = buildContentRoots(mod, out, resolved)
	out.OrderEntries = buildVariantOrderEntries(g, variant, out)
	out.Targets = buildTargets(resolved, out.Materialization, out.Actions)
	return out
}

func buildVariantOrderEntries(g *graph.Graph, variant graph.Variant, projected Variant) []OrderEntry {
	fallback := VariantOrderEntries(projected)
	if record, ok := classpathRecordForGraphVariant(g, variant); ok {
		entries := ClasspathRecordToOrderEntries(record, ClasspathOrderEntryOptions{
			CompileSDK:      projected.CompileSDK,
			CurrentModuleID: variant.ModuleID.String(),
			ModulePaths:     graphModulePaths(g),
			VariantNames:    graphVariantNames(g),
		})
		if len(entries) > 0 {
			return mergeOrderEntries(fallback, entries)
		}
	}
	return fallback
}

func mergeOrderEntries(fallback, resolved []OrderEntry) []OrderEntry {
	if len(fallback) == 0 {
		return append([]OrderEntry(nil), resolved...)
	}
	if len(resolved) == 0 {
		return append([]OrderEntry(nil), fallback...)
	}

	resolvedByKey := make(map[string]OrderEntry, len(resolved))
	for _, entry := range resolved {
		resolvedByKey[orderEntryModelKey(entry)] = entry
	}

	out := make([]OrderEntry, 0, len(fallback)+len(resolved))
	seen := make(map[string]struct{}, len(fallback)+len(resolved))
	for _, entry := range fallback {
		key := orderEntryModelKey(entry)
		if resolvedEntry, ok := resolvedByKey[key]; ok {
			out = append(out, resolvedEntry)
		} else {
			out = append(out, entry)
		}
		seen[key] = struct{}{}
	}
	for _, entry := range resolved {
		key := orderEntryModelKey(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, entry)
		seen[key] = struct{}{}
	}
	return out
}

func classpathRecordForGraphVariant(g *graph.Graph, variant graph.Variant) (classpath.Record, bool) {
	if g == nil {
		return classpath.Record{}, false
	}

	entries := make([]classpath.Entry, 0)
	materializations := g.VariantMaterializations(variant.ID)
	for _, materialization := range materializations {
		for _, root := range materialization.SourceRoots {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			entries = append(entries, classpath.Entry{
				Path:            root,
				NormalizedPath:  root,
				Origin:          classpath.OriginSource,
				ModuleID:        materialization.ModuleID.String(),
				VariantID:       materialization.VariantID.String(),
				SelectionReason: "variant source root",
			})
		}
	}

	for _, action := range g.ActionsForVariant(variant.ID) {
		if !isOrderEntryClasspathAction(action) {
			continue
		}
		for _, artifact := range g.ActionInputs(action.ID) {
			if !isOrderEntryClasspathArtifact(artifact) {
				continue
			}
			entries = append(entries, classpathEntriesForActionInput(g, variant, artifact)...)
		}
	}

	if len(entries) == 0 {
		return classpath.Record{}, false
	}

	snapshot := classpath.Normalize(
		classpath.ScopeCompile,
		variant.ModuleID.String(),
		variant.ID.String(),
		"",
		entries,
		materialization.Provenance{
			Producer: "intellijsync.Builder",
			Subject:  variant.ID.String(),
			Reasons:  []string{"variant order entry projection"},
		},
	)
	return snapshot.Record(), true
}

func isOrderEntryClasspathAction(action graph.Action) bool {
	if action.Kind != graph.ActionKindCompile {
		return false
	}
	operation := strings.TrimSpace(action.Attributes["operation"])
	return operation == "" || operation == "compile"
}

func isOrderEntryClasspathArtifact(artifact graph.Artifact) bool {
	if artifact.MaterializationID != "" {
		return true
	}
	switch artifact.Kind {
	case graph.ArtifactKindJar, graph.ArtifactKindAar, graph.ArtifactKindDirectory, graph.ArtifactKindClasspath, graph.ArtifactKindUnknown:
		return true
	}
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(artifact.Path))) {
	case ".jar", ".aar":
		return true
	default:
		return false
	}
}

func classpathEntriesForActionInput(g *graph.Graph, variant graph.Variant, artifact graph.Artifact) []classpath.Entry {
	if g == nil {
		return nil
	}
	if materializationID := artifact.MaterializationID; materializationID != "" {
		if materialization, ok := g.Materialization(materializationID); ok {
			if materialization.ModuleID == variant.ModuleID {
				return nil
			}
			if len(materialization.SourceRoots) > 0 {
				entries := make([]classpath.Entry, 0, len(materialization.SourceRoots))
				for _, root := range materialization.SourceRoots {
					root = strings.TrimSpace(root)
					if root == "" {
						continue
					}
					entries = append(entries, classpath.Entry{
						Path:            root,
						NormalizedPath:  root,
						Origin:          classpath.OriginSource,
						ArtifactID:      artifact.ID.String(),
						ModuleID:        materialization.ModuleID.String(),
						VariantID:       materialization.VariantID.String(),
						SelectionReason: "variant dependency input",
					})
				}
				if len(entries) > 0 {
					return entries
				}
			}
		}
	}

	path := strings.TrimSpace(artifact.Path)
	if path == "" {
		return nil
	}
	return []classpath.Entry{{
		Path:            path,
		NormalizedPath:  path,
		Origin:          classpath.OriginArtifact,
		ArtifactID:      artifact.ID.String(),
		SelectionReason: "variant artifact input",
	}}
}

func graphModulePaths(g *graph.Graph) map[string]string {
	if g == nil {
		return nil
	}
	out := make(map[string]string, len(g.LogicalModules()))
	for _, module := range g.LogicalModules() {
		out[module.ID.String()] = module.Path
	}
	return out
}

func graphVariantNames(g *graph.Graph) map[string]string {
	if g == nil {
		return nil
	}
	out := map[string]string{}
	for _, module := range g.LogicalModules() {
		for _, variant := range g.ModuleVariants(module.ID) {
			out[variant.ID.String()] = variant.Name
		}
	}
	return out
}

func buildMaterialization(cfg *configmodel.Model, m graph.Materialization) Materialization {
	out := Materialization{
		ID:                   m.ID.String(),
		Mode:                 m.Attributes["mode"],
		ArtifactSnapshotID:   m.ArtifactSnapshotID,
		ClasspathSnapshotIDs: append([]string(nil), m.ClasspathSnapshotIDs...),
		SourceRoots:          append([]string(nil), m.SourceRoots...),
	}
	if cfg == nil {
		return out
	}
	if provenance, ok := cfg.ProvenanceSummaryByMaterialization(m.ID); ok {
		out.ManifestPaths = append([]string(nil), provenance.ManifestPaths...)
		out.BackingArtifactID = provenance.BackingArtifactID
		out.ProducedArtifactIDs = append([]string(nil), provenance.ProducedArtifactIDs...)
	}
	if artifacts := cfg.ArtifactSummariesByMaterialization(m.ID); len(artifacts) > 0 {
		out.ProducedArtifacts = make([]Artifact, 0, len(artifacts))
		for _, artifact := range artifacts {
			out.ProducedArtifacts = append(out.ProducedArtifacts, Artifact{
				ID:                 artifact.ID,
				Kind:               artifact.Kind,
				Path:               artifact.Path,
				ProducedByActionID: artifact.ProducedByActionID,
			})
		}
	}
	return out
}

func buildMaterializationFromResolved(resolved project.ResolvedVariant) Materialization {
	out := Materialization{
		ID:                   resolved.MaterializationID,
		ArtifactSnapshotID:   resolved.ArtifactSnapshotID,
		ClasspathSnapshotIDs: append([]string(nil), resolved.ClasspathSnapshotIDs...),
		SourceRoots:          append([]string(nil), resolved.SourceRoots...),
		ManifestPaths:        append([]string(nil), resolved.ManifestPaths...),
		BackingArtifactID:    resolved.BackingArtifactID,
		ProducedArtifactIDs:  append([]string(nil), resolved.ProducedArtifactIDs...),
	}
	if len(resolved.ProducedArtifacts) > 0 {
		out.ProducedArtifacts = make([]Artifact, 0, len(resolved.ProducedArtifacts))
		for _, artifact := range resolved.ProducedArtifacts {
			out.ProducedArtifacts = append(out.ProducedArtifacts, Artifact{
				ID:                 artifact.ID,
				Kind:               artifact.Kind,
				Path:               artifact.Path,
				ProducedByActionID: artifact.ProducedByActionID,
			})
		}
	}
	return out
}

func mergeMaterialization(base, overlay Materialization) Materialization {
	if base.ID == "" {
		base.ID = overlay.ID
	}
	if base.Mode == "" {
		base.Mode = overlay.Mode
	}
	if base.ArtifactSnapshotID == "" {
		base.ArtifactSnapshotID = overlay.ArtifactSnapshotID
	}
	if len(base.ClasspathSnapshotIDs) == 0 {
		base.ClasspathSnapshotIDs = append([]string(nil), overlay.ClasspathSnapshotIDs...)
	}
	if len(base.SourceRoots) == 0 {
		base.SourceRoots = append([]string(nil), overlay.SourceRoots...)
	}
	if len(base.ManifestPaths) == 0 {
		base.ManifestPaths = append([]string(nil), overlay.ManifestPaths...)
	}
	if base.BackingArtifactID == "" {
		base.BackingArtifactID = overlay.BackingArtifactID
	}
	if len(base.ProducedArtifactIDs) == 0 {
		base.ProducedArtifactIDs = append([]string(nil), overlay.ProducedArtifactIDs...)
	}
	if len(base.ProducedArtifacts) == 0 {
		base.ProducedArtifacts = append([]Artifact(nil), overlay.ProducedArtifacts...)
		return base
	}
	havePath := false
	for _, artifact := range base.ProducedArtifacts {
		if strings.TrimSpace(artifact.Path) != "" {
			havePath = true
			break
		}
	}
	if havePath {
		return base
	}
	byID := map[string]Artifact{}
	for _, artifact := range base.ProducedArtifacts {
		byID[artifact.ID] = artifact
	}
	for _, artifact := range overlay.ProducedArtifacts {
		if existing, ok := byID[artifact.ID]; ok {
			if existing.Path == "" {
				existing.Path = artifact.Path
			}
			if existing.Kind == "" {
				existing.Kind = artifact.Kind
			}
			if existing.ProducedByActionID == "" {
				existing.ProducedByActionID = artifact.ProducedByActionID
			}
			byID[artifact.ID] = existing
			continue
		}
		byID[artifact.ID] = artifact
	}
	base.ProducedArtifacts = base.ProducedArtifacts[:0]
	for _, artifact := range byID {
		base.ProducedArtifacts = append(base.ProducedArtifacts, artifact)
	}
	sort.Slice(base.ProducedArtifacts, func(i, j int) bool { return base.ProducedArtifacts[i].ID < base.ProducedArtifacts[j].ID })
	return base
}

func buildContentRoots(mod project.Module, variant Variant, resolved project.ResolvedVariant) []ContentRoot {
	grouped := map[string]map[string]ContentEntry{}
	add := func(path, kind, sourceSet string, generated bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		root := contentRootPath(mod.Dir, path, generated)
		if root == "" {
			return
		}
		if _, ok := grouped[root]; !ok {
			grouped[root] = map[string]ContentEntry{}
		}
		entry := ContentEntry{
			Path:        path,
			Kind:        kind,
			Generated:   generated,
			SourceSet:   sourceSet,
			VariantName: variant.Name,
		}
		key := strings.Join([]string{entry.Path, entry.Kind, entry.SourceSet, entry.VariantName, fmt.Sprintf("%t", entry.Generated)}, "|")
		grouped[root][key] = entry
	}
	for _, root := range variant.Materialization.SourceRoots {
		add(root, "source", sourceSetNameFromPath(root), false)
	}
	for _, sourceSet := range resolved.SourceSetOrder {
		if sourceSet == "" {
			continue
		}
		add(filepath.Join(mod.Dir, "src", sourceSet, "res"), "resource", sourceSet, false)
		add(filepath.Join(mod.Dir, "src", sourceSet, "AndroidManifest.xml"), "manifest", sourceSet, false)
	}
	if variant.TestInstrumentationRunner != "" || mod.Type == "android-application" || mod.Type == "android-library" {
		add(filepath.Join(mod.Dir, "src", "androidTest"), "androidTest", "androidTest", false)
		if variant.Name != "" && variant.Name != "main" {
			sourceSet := "androidTest" + titleCase(variant.Name)
			add(filepath.Join(mod.Dir, "src", sourceSet), "androidTest", sourceSet, false)
		}
	}
	if mod.Type == "jvm-library" || variant.BuildType != "" || variant.Name != "" {
		add(filepath.Join(mod.Dir, "src", "test"), "test", "test", false)
		if variant.Name != "" && variant.Name != "main" {
			sourceSet := "test" + titleCase(variant.Name)
			add(filepath.Join(mod.Dir, "src", sourceSet), "test", sourceSet, false)
		}
	}
	for _, manifestPath := range variant.Materialization.ManifestPaths {
		add(manifestPath, "manifest", sourceSetNameFromManifest(manifestPath), false)
	}
	for _, artifact := range variant.Materialization.ProducedArtifacts {
		if strings.TrimSpace(artifact.Path) == "" {
			continue
		}
		if !withinDir(artifact.Path, filepath.Join(mod.Dir, "build")) {
			continue
		}
		add(generatedContentPath(mod.Dir, artifact.Path), "generated", "", true)
	}
	add(filepath.Join(mod.Dir, "build"), "generated", "", true)
	out := make([]ContentRoot, 0, len(grouped))
	for root, entries := range grouped {
		contentRoot := ContentRoot{Path: root}
		for _, entry := range entries {
			contentRoot.Entries = append(contentRoot.Entries, entry)
		}
		sort.Slice(contentRoot.Entries, func(i, j int) bool {
			if contentRoot.Entries[i].Kind != contentRoot.Entries[j].Kind {
				return contentRoot.Entries[i].Kind < contentRoot.Entries[j].Kind
			}
			return contentRoot.Entries[i].Path < contentRoot.Entries[j].Path
		})
		out = append(out, contentRoot)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func contentRootPath(moduleDir, path string, generated bool) string {
	path = filepath.Clean(strings.TrimSpace(path))
	moduleDir = filepath.Clean(strings.TrimSpace(moduleDir))
	if path == "" || moduleDir == "" {
		return ""
	}
	if generated {
		buildDir := filepath.Join(moduleDir, "build")
		if withinDir(path, buildDir) {
			return buildDir
		}
	}
	if withinDir(path, moduleDir) {
		return moduleDir
	}
	if looksLikeFile(path) {
		return filepath.Dir(path)
	}
	return path
}

func generatedContentPath(moduleDir, path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	moduleDir = filepath.Clean(strings.TrimSpace(moduleDir))
	if path == "" || moduleDir == "" {
		return ""
	}
	buildDir := filepath.Join(moduleDir, "build")
	if withinDir(path, buildDir) {
		return buildDir
	}
	return ""
}

func sourceSetNameFromPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	if filepath.Base(filepath.Dir(path)) == "src" {
		return filepath.Base(path)
	}
	if filepath.Base(filepath.Dir(filepath.Dir(path))) == "src" {
		return filepath.Base(filepath.Dir(path))
	}
	return filepath.Base(path)
}

func sourceSetNameFromManifest(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	if filepath.Base(filepath.Dir(dir)) == "src" {
		return filepath.Base(dir)
	}
	return filepath.Base(dir)
}

func looksLikeFile(path string) bool {
	return strings.Contains(filepath.Base(path), ".")
}

func withinDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func titleCase(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func buildIdentityMapping(graphModuleID, graphVariantID, modulePath string, resolved project.ResolvedVariant) Identity {
	variantName := firstNonEmptyString(strings.TrimSpace(resolved.Name), strings.TrimSpace(resolved.DeclaredName))
	ideModule := ideModuleID(modulePath)
	ideVariantID := ""
	if variantName != "" {
		ideVariantID = ideModule + "/" + variantName
	}
	return Identity{
		GraphModuleID:   strings.TrimSpace(graphModuleID),
		GraphVariantID:  strings.TrimSpace(graphVariantID),
		ModulePath:      modulePath,
		VariantName:     variantName,
		DeclaredName:    strings.TrimSpace(resolved.DeclaredName),
		CoordinateName:  strings.TrimSpace(resolved.CoordinateName),
		IDEModuleID:     ideModule,
		IDEVariantID:    ideVariantID,
		IDESourceSetIDs: ideSourceSetIDs(ideVariantID, resolved.SourceSetNames),
		ModelSelectors:  append([]string(nil), resolved.ModelSelectors...),
		SyncFragments:   append([]string(nil), resolved.SyncFragments...),
	}
}

func buildTargets(resolved project.ResolvedVariant, materialization Materialization, actions []Action) []Target {
	var targets []Target
	if target, ok := buildTarget(resolved, materialization, actions, "build"); ok {
		targets = append(targets, target)
	}
	if target, ok := buildTarget(resolved, materialization, actions, "install"); ok {
		targets = append(targets, target)
	}
	if target, ok := buildTarget(resolved, materialization, actions, "uninstall"); ok {
		targets = append(targets, target)
	}
	if target, ok := buildTarget(resolved, materialization, actions, "unit-test"); ok {
		targets = append(targets, target)
	}
	if target, ok := buildTarget(resolved, materialization, actions, "android-test"); ok {
		targets = append(targets, target)
	}
	return targets
}

func buildTarget(resolved project.ResolvedVariant, materialization Materialization, actions []Action, kind string) (Target, bool) {
	target := Target{
		Kind:      kind,
		Name:      kind,
		Supported: true,
	}
	switch kind {
	case "build":
		action, ok := actionForOperation(actions, "assemble")
		target.TaskName = firstNonEmptyString(taskAlias(resolved.TaskAliases, "assemble"), taskAlias(resolved.TaskAliases, "build"), "assemble")
		target.TaskNames = compactStrings([]string{target.TaskName})
		if ok {
			target.ActionID = action.ID
			target.ActionName = action.Name
			target.ActionKind = action.Kind
			target.ArtifactIDs = append([]string(nil), action.Outputs...)
		}
		if len(target.ArtifactIDs) == 0 {
			target.ArtifactIDs = append([]string(nil), materialization.ProducedArtifactIDs...)
		}
		target.ArtifactPath = firstNonEmptyString(
			resolved.InstallArtifactPath,
			firstArtifactPathByKind(materialization.ProducedArtifacts, "apk"),
			firstArtifactPathByKind(materialization.ProducedArtifacts, "jar"),
			firstArtifactPath(materialization.ProducedArtifacts),
		)
		if target.TaskName == "" && target.ActionID == "" && target.ArtifactPath == "" && len(target.ArtifactIDs) == 0 {
			return Target{}, false
		}
	case "install":
		target.TaskName = firstNonEmptyString(resolved.InstallTask, taskAlias(resolved.TaskAliases, "install"))
		target.TaskNames = compactStrings([]string{target.TaskName})
		target.ArtifactIDs = compactStrings([]string{resolved.InstallArtifactID})
		target.ArtifactPath = firstNonEmptyString(resolved.InstallArtifactPath, firstArtifactPathByKind(materialization.ProducedArtifacts, "apk"), firstArtifactPath(materialization.ProducedArtifacts))
		if action, ok := actionForOperation(actions, "install"); ok {
			target.ActionID = action.ID
			target.ActionName = action.Name
			target.ActionKind = action.Kind
			if len(target.ArtifactIDs) == 0 {
				target.ArtifactIDs = append([]string(nil), action.Outputs...)
			}
		}
		if target.TaskName == "" {
			return Target{}, false
		}
	case "uninstall":
		target.TaskName = firstNonEmptyString(resolved.UninstallTask, taskAlias(resolved.TaskAliases, "uninstall"))
		target.TaskNames = compactStrings([]string{target.TaskName})
		target.PackageName = resolved.ApplicationID
		if action, ok := actionForOperation(actions, "uninstall"); ok {
			target.ActionID = action.ID
			target.ActionName = action.Name
			target.ActionKind = action.Kind
		}
		if target.TaskName == "" {
			return Target{}, false
		}
	case "unit-test":
		target.TaskName = firstNonEmptyString(taskAlias(resolved.TaskAliases, "test"), taskAlias(resolved.TaskAliases, "unit-test"))
		target.TaskNames = compactStrings([]string{
			target.TaskName,
			taskAlias(resolved.TaskAliases, "compile-unit-test"),
		})
		if action, ok := actionForOperation(actions, "test"); ok {
			target.ActionID = action.ID
			target.ActionName = action.Name
			target.ActionKind = action.Kind
			target.ArtifactIDs = append([]string(nil), action.Outputs...)
		}
		if target.TaskName == "" {
			return Target{}, false
		}
	case "android-test":
		target.TaskNames = compactStrings([]string{
			taskAlias(resolved.TaskAliases, "assemble-android-test"),
			taskAlias(resolved.TaskAliases, "compile-android-test"),
			taskAlias(resolved.TaskAliases, "install-android-test"),
			taskAlias(resolved.TaskAliases, "uninstall-android-test"),
		})
		target.TaskName = firstNonEmptyString(
			taskAlias(resolved.TaskAliases, "install-android-test"),
			taskAlias(resolved.TaskAliases, "assemble-android-test"),
			taskAlias(resolved.TaskAliases, "compile-android-test"),
		)
		target.PackageName = resolved.ApplicationID + ".test"
		target.ManifestPath = firstNonEmptyString(
			firstArtifactPathByKind(materialization.ProducedArtifacts, "manifest"),
			firstNonEmptyString(materialization.ManifestPaths...),
		)
		if action, ok := actionForOperation(actions, "install-android-tests"); ok {
			target.ActionID = action.ID
			target.ActionName = action.Name
			target.ActionKind = action.Kind
			target.ArtifactIDs = append([]string(nil), action.Outputs...)
		} else if action, ok := actionForOperation(actions, "compile-android-tests"); ok {
			target.ActionID = action.ID
			target.ActionName = action.Name
			target.ActionKind = action.Kind
			target.ArtifactIDs = append([]string(nil), action.Outputs...)
		}
		if len(target.TaskNames) == 0 && target.TaskName == "" {
			return Target{}, false
		}
	}
	target.ArtifactIDs = compactStrings(target.ArtifactIDs)
	target.TaskNames = compactStrings(target.TaskNames)
	return target, true
}

func actionForOperation(actions []Action, operation string) (Action, bool) {
	for _, action := range actions {
		if action.Operation == operation {
			return action, true
		}
	}
	return Action{}, false
}

func taskAlias(aliases []string, prefix string) string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for _, alias := range aliases {
		lower := strings.ToLower(strings.TrimSpace(alias))
		switch prefix {
		case "assemble":
			if strings.HasPrefix(lower, "assemble") && !strings.Contains(lower, "androidtest") {
				return alias
			}
		case "build":
			if lower == "build" {
				return alias
			}
		case "install":
			if strings.HasPrefix(lower, "install") && !strings.Contains(lower, "androidtest") {
				return alias
			}
		case "uninstall":
			if strings.HasPrefix(lower, "uninstall") && !strings.Contains(lower, "androidtest") {
				return alias
			}
		case "test", "unit-test":
			if strings.HasPrefix(lower, "test") && strings.Contains(lower, "unittest") || lower == "test" {
				return alias
			}
		case "compile-unit-test":
			if strings.HasPrefix(lower, "compile") && strings.Contains(lower, "unittest") {
				return alias
			}
		case "assemble-android-test":
			if strings.HasPrefix(lower, "assemble") && strings.Contains(lower, "androidtest") {
				return alias
			}
		case "compile-android-test":
			if strings.HasPrefix(lower, "compile") && strings.Contains(lower, "androidtest") {
				return alias
			}
		case "install-android-test":
			if strings.HasPrefix(lower, "install") && strings.Contains(lower, "androidtest") {
				return alias
			}
		case "uninstall-android-test":
			if strings.HasPrefix(lower, "uninstall") && strings.Contains(lower, "androidtest") {
				return alias
			}
		}
	}
	return ""
}

func firstArtifactPathByKind(artifacts []Artifact, kind string) string {
	for _, artifact := range artifacts {
		if artifact.Kind == kind && strings.TrimSpace(artifact.Path) != "" {
			return artifact.Path
		}
	}
	return ""
}

func firstArtifactPath(artifacts []Artifact) string {
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Path) != "" {
			return artifact.Path
		}
	}
	return ""
}

func compactStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
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
	return out
}

func ideModuleID(modulePath string) string {
	modulePath = strings.TrimSpace(modulePath)
	if modulePath == "" {
		return ""
	}
	modulePath = strings.TrimPrefix(modulePath, ":")
	modulePath = strings.ReplaceAll(modulePath, ":", ".")
	return modulePath
}

func moduleKindForTaskCatalog(kind string) intellijtask.ModuleKind {
	switch kind {
	case "android-application":
		return intellijtask.ModuleKindAndroidApplication
	case "android-library":
		return intellijtask.ModuleKindAndroidLibrary
	case "jvm-library":
		return intellijtask.ModuleKindJvmLibrary
	default:
		return intellijtask.ModuleKindUnknown
	}
}

func ideSourceSetIDs(ideVariantID string, sourceSetNames []string) []string {
	if ideVariantID == "" || len(sourceSetNames) == 0 {
		return nil
	}
	out := make([]string, 0, len(sourceSetNames))
	for _, name := range sourceSetNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, ideVariantID+"/sourceSet:"+name)
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func resolvedVariantForSync(cfg *configmodel.Model, mod project.Module, variantName string) project.ResolvedVariant {
	if cfg != nil {
		if resolved, ok := cfg.ResolvedVariant(mod.Path, variantName); ok {
			return resolved
		}
	}
	return mod.ResolveVariant(variantName)
}

func buildActionsForVariant(g *graph.Graph, modulePath string, variant graph.Variant) []Action {
	actions := g.ActionsForVariant(variant.ID)
	out := make([]Action, 0, len(actions))
	for _, action := range actions {
		out = append(out, Action{
			ID:          action.ID.String(),
			Name:        action.Name,
			Kind:        string(action.Kind),
			Operation:   action.Attributes["operation"],
			ModulePath:  modulePath,
			VariantName: action.Attributes["variantName"],
			Inputs:      artifactIDs(action.Inputs),
			Outputs:     artifactIDs(action.Outputs),
			Note:        action.Note,
		})
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

func buildModuleDependencies(g *graph.Graph, moduleID graph.LogicalModuleID) []Dependency {
	deps := g.DependenciesOf(moduleID.Ref())
	out := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		if dep.Kind != graph.NodeKindLogicalModule {
			continue
		}
		target, ok := g.LogicalModule(graph.LogicalModuleID(dep.ID))
		if !ok {
			continue
		}
		out = append(out, Dependency{
			Kind:             "module",
			Level:            "module",
			TargetNodeID:     dep.ID,
			TargetModulePath: target.Path,
		})
	}
	sortDependencies(out)
	return out
}

func buildVariantDependencies(g *graph.Graph, variant graph.Variant) []Dependency {
	deps := g.DependenciesOf(variant.Ref())
	out := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		switch dep.Kind {
		case graph.NodeKindLogicalModule:
			target, ok := g.LogicalModule(graph.LogicalModuleID(dep.ID))
			if !ok {
				continue
			}
			out = append(out, Dependency{
				Kind:             "module",
				Level:            "variant",
				TargetNodeID:     dep.ID,
				TargetModulePath: target.Path,
			})
		case graph.NodeKindVariant:
			target, ok := g.Variant(graph.VariantID(dep.ID))
			if !ok {
				continue
			}
			module, ok := g.LogicalModule(target.ModuleID)
			if !ok {
				continue
			}
			out = append(out, Dependency{
				Kind:              "variant",
				Level:             "variant",
				TargetNodeID:      dep.ID,
				TargetModulePath:  module.Path,
				TargetVariantName: target.Name,
			})
		case graph.NodeKindMaterialization:
			target, ok := g.Materialization(graph.MaterializationID(dep.ID))
			if !ok {
				continue
			}
			module, ok := g.LogicalModule(target.ModuleID)
			if !ok {
				continue
			}
			targetVariant, _ := g.Variant(target.VariantID)
			out = append(out, Dependency{
				Kind:                    "materialization",
				Level:                   "variant",
				TargetNodeID:            dep.ID,
				TargetModulePath:        module.Path,
				TargetVariantName:       targetVariant.Name,
				TargetMaterializationID: target.ID.String(),
			})
		}
	}
	sortDependencies(out)
	return out
}

func logicalModuleIDForPath(g *graph.Graph, path string) (graph.LogicalModuleID, bool) {
	for _, module := range g.LogicalModules() {
		if module.Path == path {
			return module.ID, true
		}
	}
	return "", false
}

func sortDependencies(deps []Dependency) {
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].TargetModulePath == deps[j].TargetModulePath {
			if deps[i].TargetVariantName == deps[j].TargetVariantName {
				return deps[i].Kind < deps[j].Kind
			}
			return deps[i].TargetVariantName < deps[j].TargetVariantName
		}
		return deps[i].TargetModulePath < deps[j].TargetModulePath
	})
}
