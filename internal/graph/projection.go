package graph

import "sort"

// ModuleDependency represents a directed dependency from one module to another,
// derived from action input/output artifact relationships that cross module boundaries.
type ModuleDependency struct {
	FromModuleID LogicalModuleID `json:"fromModuleId"`
	ToModuleID   LogicalModuleID `json:"toModuleId"`
	// ArtifactIDs lists the artifacts that establish this dependency edge.
	ArtifactIDs []ArtifactID `json:"artifactIds,omitempty"`
}

// ModuleDependencyGraph is an IDE-friendly projection of inter-module dependencies.
type ModuleDependencyGraph struct {
	Modules      []LogicalModuleID  `json:"modules"`
	Dependencies []ModuleDependency `json:"dependencies"`
}

// ProjectModuleDependencies derives a module-level dependency graph from action
// input/output relationships. When an action in module A consumes an artifact
// produced by an action in module B, this creates a dependency edge from A to B.
func (g *Graph) ProjectModuleDependencies() ModuleDependencyGraph {
	// Build a map: artifactID -> producing module
	artifactProducer := map[ArtifactID]LogicalModuleID{}
	for _, action := range g.actions {
		if action.ModuleID == "" {
			continue
		}
		for _, outputID := range action.Outputs {
			artifactProducer[outputID] = action.ModuleID
		}
	}

	// Also attribute artifacts to modules via materializations.
	for _, artifact := range g.artifacts {
		if artifact.MaterializationID == "" {
			continue
		}
		mat, ok := g.materializations[artifact.MaterializationID]
		if !ok {
			continue
		}
		if _, already := artifactProducer[artifact.ID]; !already {
			artifactProducer[artifact.ID] = mat.ModuleID
		}
	}

	type depKey struct {
		from, to LogicalModuleID
	}
	depArtifacts := map[depKey][]ArtifactID{}

	for _, action := range g.actions {
		if action.ModuleID == "" {
			continue
		}
		for _, inputID := range action.Inputs {
			producerModule, ok := artifactProducer[inputID]
			if !ok || producerModule == action.ModuleID {
				continue
			}
			key := depKey{from: action.ModuleID, to: producerModule}
			depArtifacts[key] = append(depArtifacts[key], inputID)
		}
	}

	// Also derive dependencies from edges of kind "depends_on" between modules.
	for _, edge := range g.edges {
		if edge.Kind != EdgeKindDependsOn {
			continue
		}
		if edge.From.Kind != NodeKindLogicalModule || edge.To.Kind != NodeKindLogicalModule {
			continue
		}
		key := depKey{
			from: LogicalModuleID(edge.From.ID),
			to:   LogicalModuleID(edge.To.ID),
		}
		if _, ok := depArtifacts[key]; !ok {
			depArtifacts[key] = nil
		}
	}

	deps := make([]ModuleDependency, 0, len(depArtifacts))
	for key, artifacts := range depArtifacts {
		sort.Slice(artifacts, func(i, j int) bool { return artifacts[i] < artifacts[j] })
		deps = append(deps, ModuleDependency{
			FromModuleID: key.from,
			ToModuleID:   key.to,
			ArtifactIDs:  artifacts,
		})
	}
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].FromModuleID == deps[j].FromModuleID {
			return deps[i].ToModuleID < deps[j].ToModuleID
		}
		return deps[i].FromModuleID < deps[j].FromModuleID
	})

	moduleSet := map[LogicalModuleID]struct{}{}
	for id := range g.modules {
		moduleSet[id] = struct{}{}
	}
	modules := make([]LogicalModuleID, 0, len(moduleSet))
	for id := range moduleSet {
		modules = append(modules, id)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i] < modules[j] })

	return ModuleDependencyGraph{
		Modules:      modules,
		Dependencies: deps,
	}
}

// TaskEntry represents a single available task (action) in a task catalog.
type TaskEntry struct {
	ActionID    ActionID        `json:"actionId"`
	Name        string          `json:"name"`
	Kind        ActionKind      `json:"kind"`
	ModuleID    LogicalModuleID `json:"moduleId"`
	VariantID   VariantID       `json:"variantId,omitempty"`
	InputCount  int             `json:"inputCount"`
	OutputCount int             `json:"outputCount"`
}

// TaskCatalog groups available tasks by module, suitable for IDE task menus.
type TaskCatalog struct {
	// Modules maps module ID to its available tasks, sorted by action name.
	Modules map[LogicalModuleID][]TaskEntry `json:"modules"`
}

// ProjectTaskCatalog derives a task catalog from the graph's actions.
// Each action becomes a task entry, grouped by its owning module.
func (g *Graph) ProjectTaskCatalog() TaskCatalog {
	catalog := TaskCatalog{
		Modules: make(map[LogicalModuleID][]TaskEntry, len(g.modules)),
	}
	for id := range g.modules {
		catalog.Modules[id] = nil
	}

	for _, action := range g.actions {
		entry := TaskEntry{
			ActionID:    action.ID,
			Name:        action.Name,
			Kind:        action.Kind,
			ModuleID:    action.ModuleID,
			VariantID:   action.VariantID,
			InputCount:  len(action.Inputs),
			OutputCount: len(action.Outputs),
		}
		catalog.Modules[action.ModuleID] = append(catalog.Modules[action.ModuleID], entry)
	}

	for moduleID, entries := range catalog.Modules {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Name == entries[j].Name {
				return entries[i].ActionID < entries[j].ActionID
			}
			return entries[i].Name < entries[j].Name
		})
		catalog.Modules[moduleID] = entries
	}
	return catalog
}

// ContentRoot represents a source or resource root for a variant.
type ContentRoot struct {
	Path                string              `json:"path"`
	MaterializationID   MaterializationID   `json:"materializationId"`
	MaterializationKind MaterializationKind `json:"materializationKind"`
}

// VariantContentRoots holds the content roots for a single variant.
type VariantContentRoots struct {
	ModuleID    LogicalModuleID `json:"moduleId"`
	VariantID   VariantID       `json:"variantId"`
	VariantName string          `json:"variantName"`
	Roots       []ContentRoot   `json:"roots"`
}

// ContentRootProjection maps each variant to its content roots.
type ContentRootProjection struct {
	Variants []VariantContentRoots `json:"variants"`
}

// ProjectContentRoots derives content roots per variant from materializations.
// Each materialization's source roots become content root entries, giving IDE
// sync the information it needs to set up project structure.
func (g *Graph) ProjectContentRoots() ContentRootProjection {
	type variantKey struct {
		moduleID  LogicalModuleID
		variantID VariantID
	}

	variantRoots := map[variantKey][]ContentRoot{}

	for _, mat := range g.materializations {
		key := variantKey{moduleID: mat.ModuleID, variantID: mat.VariantID}
		for _, root := range mat.SourceRoots {
			variantRoots[key] = append(variantRoots[key], ContentRoot{
				Path:                root,
				MaterializationID:   mat.ID,
				MaterializationKind: mat.Kind,
			})
		}
	}

	variants := make([]VariantContentRoots, 0, len(variantRoots))
	for key, roots := range variantRoots {
		variant, ok := g.variants[key.variantID]
		variantName := ""
		if ok {
			variantName = variant.Name
		}
		sort.Slice(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
		variants = append(variants, VariantContentRoots{
			ModuleID:    key.moduleID,
			VariantID:   key.variantID,
			VariantName: variantName,
			Roots:       roots,
		})
	}
	sort.Slice(variants, func(i, j int) bool {
		if variants[i].ModuleID == variants[j].ModuleID {
			return variants[i].VariantID < variants[j].VariantID
		}
		return variants[i].ModuleID < variants[j].ModuleID
	})

	return ContentRootProjection{Variants: variants}
}

// ActionDependencyChain represents the transitive chain of action dependencies
// for a given action, useful for critical-path analysis and scheduling.
type ActionDependencyChain struct {
	ActionID     ActionID   `json:"actionId"`
	Dependencies []ActionID `json:"dependencies"`
	Depth        int        `json:"depth"`
}

// ProjectActionDependencyChains computes the transitive dependency chain for
// every action in the graph. Each chain includes all transitive upstream actions
// and the depth (longest path from a root action). This is the substrate that
// schedulers and critical-path analyzers need.
func (g *Graph) ProjectActionDependencyChains() []ActionDependencyChain {
	// Memoize per-action transitive deps and depth.
	type memo struct {
		deps  []ActionID
		depth int
	}
	cache := map[ActionID]*memo{}

	var resolve func(id ActionID) *memo
	resolve = func(id ActionID) *memo {
		if m, ok := cache[id]; ok {
			return m
		}
		m := &memo{}
		cache[id] = m // place sentinel to break cycles

		directDeps := g.ActionDependencies(id)
		seen := map[ActionID]struct{}{}
		for _, dep := range directDeps {
			seen[dep] = struct{}{}
			upstream := resolve(dep)
			if upstream.depth+1 > m.depth {
				m.depth = upstream.depth + 1
			}
			for _, transitive := range upstream.deps {
				seen[transitive] = struct{}{}
			}
		}
		allDeps := make([]ActionID, 0, len(seen))
		for dep := range seen {
			allDeps = append(allDeps, dep)
		}
		sort.Slice(allDeps, func(i, j int) bool { return allDeps[i] < allDeps[j] })
		m.deps = allDeps
		return m
	}

	chains := make([]ActionDependencyChain, 0, len(g.actions))
	for id := range g.actions {
		m := resolve(id)
		chains = append(chains, ActionDependencyChain{
			ActionID:     id,
			Dependencies: m.deps,
			Depth:        m.depth,
		})
	}
	sort.Slice(chains, func(i, j int) bool { return chains[i].ActionID < chains[j].ActionID })
	return chains
}
