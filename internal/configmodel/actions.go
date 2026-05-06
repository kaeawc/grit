package configmodel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/graph"
	"github.com/kaeawc/grit/internal/project"
	"github.com/kaeawc/grit/internal/responsepayload"
	"github.com/kaeawc/grit/internal/tieredcas"
)

type ActionSchedule struct {
	Steps               []ActionScheduleStep
	Batches             [][]ActionScheduleStep
	ResourceBudgets     []ResourceBudget
	NetworkBudgetConfig *ScheduleNetworkBudget
	BatchResources      []BatchResourceUsage
	Dependencies        map[graph.ActionID][]graph.ActionID
	Dependents          map[graph.ActionID][]graph.ActionID
}

// ScheduleNetworkBudget holds the bandwidth-aware admission parameters derived
// from the action schedule. When present, the service layer uses these to
// create a NetworkBudget and attach it to the admission controller.
type ScheduleNetworkBudget struct {
	// CapacityBytes is the maximum burst size in bytes.
	CapacityBytes int64 `json:"capacityBytes"`

	// RefillBytesPerSec is the steady-state bandwidth allowance.
	RefillBytesPerSec int64 `json:"refillBytesPerSec"`
}

type ActionScheduleStep struct {
	Action         graph.Action
	Dependencies   []graph.ActionID
	WorkerClass    string
	MaxParallelism int
	ResourceClass  string
	ResourceCost   int
	CacheKey       string
	Cacheable      bool
	ProbeOrder     []string
	ExecuteOnMiss  bool
	ProbeHint      *projectProbeHint
	EstimatedBytes int64
	RetentionClass string
	Shareability   string
}

const (
	estimatedProbeBytesCompile      int64 = 8 * 1024 * 1024
	estimatedProbeBytesCompileTests int64 = 6 * 1024 * 1024
	estimatedProbeBytesLint         int64 = 4 * 1024 * 1024
	estimatedProbeBytesAssemble     int64 = 16 * 1024 * 1024
	estimatedProbeBytesTest         int64 = 1 * 1024 * 1024
	estimatedProbeBytesJavadoc      int64 = 4 * 1024 * 1024
)

type projectProbeHint = responsepayload.CacheProbe

type ResourceBudget struct {
	ResourceClass string `json:"resourceClass"`
	Capacity      int    `json:"capacity"`
}

type ResourceUsage struct {
	ResourceClass string `json:"resourceClass"`
	Capacity      int    `json:"capacity"`
	Used          int    `json:"used"`
	Remaining     int    `json:"remaining"`
}

type BatchResourceUsage struct {
	Resources []ResourceUsage `json:"resources,omitempty"`
}

func (s ActionSchedule) OrderedActions() []graph.Action {
	if len(s.Steps) == 0 {
		return nil
	}
	out := make([]graph.Action, 0, len(s.Steps))
	for _, step := range s.Steps {
		out = append(out, cloneScheduledAction(step.Action))
	}
	return out
}

func (s ActionSchedule) StepFor(id graph.ActionID) (ActionScheduleStep, bool) {
	for _, step := range s.Steps {
		if step.Action.ID == id {
			return cloneActionScheduleStep(step), true
		}
	}
	return ActionScheduleStep{}, false
}

func (m *Model) ActionsForCommand(modulePath, moduleKind, command string, requestedVariants []string) ([]graph.Action, error) {
	resolved := make([]project.ResolvedVariant, 0, len(requestedVariants))
	for _, name := range requestedVariants {
		resolved = append(resolved, project.ResolvedVariant{
			Name:       name,
			ModulePath: modulePath,
			Coordinate: project.VariantCoordinate{Name: name},
		})
	}
	return m.ActionsForResolvedCommand(modulePath, moduleKind, command, resolved)
}

func (m *Model) ActionsForResolvedCommand(modulePath, moduleKind, command string, requestedVariants []project.ResolvedVariant) ([]graph.Action, error) {
	g, err := m.Graph()
	if err != nil {
		return nil, err
	}
	switch moduleKind {
	case "jvm-library":
		return m.jvmActionsForResolvedCommand(g, modulePath, command, requestedVariants)
	default:
		return m.androidActionsForResolvedCommand(g, modulePath, command, requestedVariants)
	}
}

func (m *Model) DependentModules(target string) ([]string, error) {
	g, err := m.Graph()
	if err != nil {
		return nil, err
	}
	mod, ok := m.Module(target)
	if !ok {
		return nil, fmt.Errorf("module %s not found", target)
	}
	targetRef := graph.NodeRef{Kind: graph.NodeKindLogicalModule, ID: mod.ID}
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
			if module, ok := g.LogicalModule(graph.LogicalModuleID(dependent.ID)); ok && module.Path != "" {
				out = append(out, module.Path)
			}
		}
	}
	return out, nil
}

func (m *Model) PlanActions(actions []graph.Action) []graph.Action {
	return m.ScheduleActions(actions).OrderedActions()
}

func (m *Model) ScheduleActions(actions []graph.Action) ActionSchedule {
	schedule := ActionSchedule{
		ResourceBudgets:     defaultResourceBudgets(),
		NetworkBudgetConfig: defaultNetworkBudgetConfig(actions),
		Dependencies:        map[graph.ActionID][]graph.ActionID{},
		Dependents:          map[graph.ActionID][]graph.ActionID{},
	}
	if len(actions) == 0 {
		return schedule
	}
	if len(actions) == 1 {
		step := m.scheduleStepForAction(actions[0], nil)
		schedule.Steps = append(schedule.Steps, step)
		schedule.Batches = append(schedule.Batches, []ActionScheduleStep{cloneActionScheduleStep(step)})
		schedule.BatchResources = append(schedule.BatchResources, batchResourceUsage([]ActionScheduleStep{step}))
		return schedule
	}
	g, err := m.Graph()
	if err != nil {
		schedule.Steps = append(schedule.Steps, m.toScheduleSteps(actions)...)
		return schedule
	}
	actions = expandActionDependencies(g, actions)
	selected := map[graph.ActionID]graph.Action{}
	indegree := map[graph.ActionID]int{}
	for _, action := range actions {
		selected[action.ID] = action
		indegree[action.ID] = 0
	}
	for _, action := range actions {
		deps := g.ActionDependencies(action.ID)
		if len(deps) == 0 {
			continue
		}
		for _, dep := range deps {
			if _, ok := selected[dep]; !ok {
				continue
			}
			indegree[action.ID]++
			schedule.Dependencies[action.ID] = append(schedule.Dependencies[action.ID], dep)
			schedule.Dependents[dep] = append(schedule.Dependents[dep], action.ID)
		}
	}
	ready := make([]graph.ActionID, 0)
	for id, deg := range indegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return compareReadyActions(selected, ready[i], ready[j]) })
	for len(ready) > 0 {
		current, leftover := selectReadyBatch(selected, ready)
		ready = append([]graph.ActionID(nil), leftover...)
		batch := make([]ActionScheduleStep, 0, len(current))
		for _, id := range current {
			step := m.scheduleStepForAction(selected[id], schedule.Dependencies[id])
			schedule.Steps = append(schedule.Steps, step)
			batch = append(batch, cloneActionScheduleStep(step))
			for _, dep := range schedule.Dependents[id] {
				indegree[dep]--
				if indegree[dep] == 0 {
					ready = append(ready, dep)
				}
			}
		}
		schedule.Batches = append(schedule.Batches, batch)
		schedule.BatchResources = append(schedule.BatchResources, batchResourceUsage(batch))
		sort.Slice(ready, func(i, j int) bool { return compareReadyActions(selected, ready[i], ready[j]) })
	}
	if len(schedule.Steps) != len(actions) {
		schedule.Steps = m.toScheduleSteps(actions)
		schedule.Batches = make([][]ActionScheduleStep, 0, len(schedule.Steps))
		schedule.BatchResources = make([]BatchResourceUsage, 0, len(schedule.Steps))
		for _, step := range schedule.Steps {
			schedule.Batches = append(schedule.Batches, []ActionScheduleStep{cloneActionScheduleStep(step)})
			schedule.BatchResources = append(schedule.BatchResources, batchResourceUsage([]ActionScheduleStep{step}))
		}
	}
	return schedule
}

func expandActionDependencies(g *graph.Graph, actions []graph.Action) []graph.Action {
	if len(actions) < 2 {
		expanded := append([]graph.Action(nil), actions...)
		seen := map[graph.ActionID]struct{}{}
		queue := make([]graph.ActionID, 0, len(actions))
		for _, action := range actions {
			seen[action.ID] = struct{}{}
			queue = append(queue, action.ID)
		}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			for _, dep := range g.ActionDependencies(id) {
				if _, ok := seen[dep]; ok {
					continue
				}
				action, ok := g.Action(dep)
				if !ok {
					continue
				}
				seen[dep] = struct{}{}
				expanded = append(expanded, action)
				queue = append(queue, dep)
			}
		}
		return expanded
	}
	expanded := append([]graph.Action(nil), actions...)
	seen := map[graph.ActionID]struct{}{}
	queue := make([]graph.ActionID, 0, len(actions))
	for _, action := range actions {
		seen[action.ID] = struct{}{}
		queue = append(queue, action.ID)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, dep := range g.ActionDependencies(id) {
			if _, ok := seen[dep]; ok {
				continue
			}
			action, ok := g.Action(dep)
			if !ok {
				continue
			}
			seen[dep] = struct{}{}
			expanded = append(expanded, action)
			queue = append(queue, dep)
		}
	}
	return expanded
}

func compareReadyActions(actions map[graph.ActionID]graph.Action, left, right graph.ActionID) bool {
	lhs := actionPriority(actions[left])
	rhs := actionPriority(actions[right])
	if lhs == rhs {
		return left < right
	}
	return lhs < rhs
}

func actionPriority(action graph.Action) int {
	switch action.Attributes["operation"] {
	case "compile":
		return 0
	case "compile-tests":
		return 1
	case "lint":
		return 2
	case "assemble":
		return 3
	case "install":
		return 4
	case "test":
		return 5
	case "javadoc-jar":
		return 6
	default:
		return 100
	}
}

func actionCacheable(action graph.Action) bool {
	switch action.Attributes["operation"] {
	case "compile", "compile-tests", "lint", "assemble", "test", "javadoc-jar":
		return true
	case "install":
		return false
	default:
		return false
	}
}

func probeOrderForAction(action graph.Action) []string {
	if !actionCacheable(action) {
		return nil
	}
	switch action.Attributes["operation"] {
	case "compile", "compile-tests", "lint", "assemble", "test", "javadoc-jar":
		return []string{"local-overlay", "shared-machine"}
	default:
		return []string{"local-overlay"}
	}
}

// estimatedBytesForAction returns a coarse remote-probe size estimate for
// cacheable actions. These values are intentionally conservative and give the
// bandwidth-aware admission controller a non-zero cost to reason about even
// before we have artifact-size history for a specific action.
func estimatedBytesForAction(action graph.Action) int64 {
	if !actionCacheable(action) {
		return 0
	}
	if len(probeOrderForAction(action)) <= 1 {
		return 0
	}
	switch action.Attributes["operation"] {
	case "compile":
		return estimatedProbeBytesCompile
	case "compile-tests":
		return estimatedProbeBytesCompileTests
	case "lint":
		return estimatedProbeBytesLint
	case "assemble":
		return estimatedProbeBytesAssemble
	case "test":
		return estimatedProbeBytesTest
	case "javadoc-jar":
		return estimatedProbeBytesJavadoc
	default:
		return 0
	}
}

func workerClassForAction(action graph.Action) string {
	switch action.Attributes["operation"] {
	case "compile":
		return "kotlin-compile"
	case "compile-tests":
		return "test-compile"
	case "lint":
		return "lint"
	case "assemble":
		return "android-package"
	case "install":
		return "adb-install"
	case "test":
		return "junit"
	case "javadoc-jar":
		return "javadoc"
	default:
		return "default"
	}
}

func resourceClassForWorkerClass(workerClass string) string {
	switch workerClass {
	case "kotlin-compile", "test-compile", "lint", "junit", "javadoc":
		return "jvm-process"
	case "android-package":
		return "android-tools"
	case "adb-install":
		return "device"
	default:
		return "default"
	}
}

func resourceCostForWorkerClass(workerClass string) int {
	switch workerClass {
	case "kotlin-compile", "test-compile", "lint", "junit", "android-package", "adb-install", "javadoc":
		return 1
	default:
		return 1
	}
}

func resourceBudgetForClass(resourceClass string) int {
	switch resourceClass {
	case "jvm-process":
		return 2
	case "android-tools", "device", "default":
		return 1
	default:
		return 1
	}
}

func defaultResourceBudgets() []ResourceBudget {
	budgets := []ResourceBudget{
		{ResourceClass: "android-tools", Capacity: resourceBudgetForClass("android-tools")},
		{ResourceClass: "default", Capacity: resourceBudgetForClass("default")},
		{ResourceClass: "device", Capacity: resourceBudgetForClass("device")},
		{ResourceClass: "jvm-process", Capacity: resourceBudgetForClass("jvm-process")},
	}
	sort.Slice(budgets, func(i, j int) bool {
		return budgets[i].ResourceClass < budgets[j].ResourceClass
	})
	return budgets
}

// defaultNetworkBudgetConfig returns a network budget config only when the
// schedule contains cacheable actions with true remote probe tiers. Local CAS
// tiers such as worktree overlay and shared-machine storage should not enable
// bandwidth admission on their own. Defaults: 50 MiB burst, 10 MiB/s refill.
func defaultNetworkBudgetConfig(actions []graph.Action) *ScheduleNetworkBudget {
	for _, a := range actions {
		if actionCacheable(a) && tieredcas.HasRemoteProbeTier(probeOrderForAction(a)) {
			return &ScheduleNetworkBudget{
				CapacityBytes:     50 * 1024 * 1024,
				RefillBytesPerSec: 10 * 1024 * 1024,
			}
		}
	}
	return nil
}

func maxParallelismForWorkerClass(workerClass string) int {
	switch workerClass {
	case "kotlin-compile", "test-compile":
		return 2
	case "lint", "android-package", "adb-install", "junit", "javadoc":
		return 1
	default:
		return 1
	}
}

func (m *Model) toScheduleSteps(actions []graph.Action) []ActionScheduleStep {
	if len(actions) == 0 {
		return nil
	}
	out := make([]ActionScheduleStep, 0, len(actions))
	for _, action := range actions {
		out = append(out, m.scheduleStepForAction(action, nil))
	}
	return out
}

func (m *Model) scheduleStepForAction(action graph.Action, deps []graph.ActionID) ActionScheduleStep {
	workerClass := workerClassForAction(action)
	step := ActionScheduleStep{
		Action:         cloneScheduledAction(action),
		Dependencies:   append([]graph.ActionID(nil), deps...),
		WorkerClass:    workerClass,
		MaxParallelism: maxParallelismForWorkerClass(workerClass),
		ResourceClass:  resourceClassForWorkerClass(workerClass),
		ResourceCost:   resourceCostForWorkerClass(workerClass),
		CacheKey:       actionCacheKeyForModel(m, action),
		Cacheable:      actionCacheable(action),
		ProbeOrder:     probeOrderForAction(action),
		ExecuteOnMiss:  true,
		EstimatedBytes: estimatedBytesForAction(action),
		RetentionClass: string(retentionClassForAction(action)),
		Shareability:   string(shareabilityForAction(action)),
	}
	if m != nil {
		if summary, ok := m.ActionSummary(action.ID); ok {
			if summary.WorkerClass != "" {
				step.WorkerClass = summary.WorkerClass
			}
			if summary.MaxParallelism != 0 {
				step.MaxParallelism = summary.MaxParallelism
			}
			if summary.ResourceClass != "" {
				step.ResourceClass = summary.ResourceClass
			}
			if summary.ResourceCost != 0 {
				step.ResourceCost = summary.ResourceCost
			}
			step.CacheKey = summary.CacheKey
			step.RetentionClass = summary.RetentionClass
			step.Shareability = summary.Shareability
		}
		if probe, ok := m.LastCacheProbeForAction(action.ID); ok {
			clone := probe
			step.ProbeHint = &clone
		}
		if observed, ok := m.LastRemoteBytesForAction(action.ID); ok && observed > step.EstimatedBytes {
			step.EstimatedBytes = observed
		}
	}
	return step
}

func cloneActionScheduleStep(step ActionScheduleStep) ActionScheduleStep {
	step.Action = cloneScheduledAction(step.Action)
	step.Dependencies = append([]graph.ActionID(nil), step.Dependencies...)
	step.ProbeOrder = append([]string(nil), step.ProbeOrder...)
	if step.ProbeHint != nil {
		probe := *step.ProbeHint
		step.ProbeHint = &probe
	}
	return step
}

func cloneScheduledAction(action graph.Action) graph.Action {
	action.Inputs = append([]graph.ArtifactID(nil), action.Inputs...)
	action.Outputs = append([]graph.ArtifactID(nil), action.Outputs...)
	if len(action.Attributes) > 0 {
		attrs := make(map[string]string, len(action.Attributes))
		for key, value := range action.Attributes {
			attrs[key] = value
		}
		action.Attributes = attrs
	}
	return action
}

func batchResourceUsage(batch []ActionScheduleStep) BatchResourceUsage {
	if len(batch) == 0 {
		return BatchResourceUsage{}
	}
	used := map[string]int{}
	for _, step := range batch {
		used[step.ResourceClass] += step.ResourceCost
	}
	classes := make([]string, 0, len(used))
	for class := range used {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	out := BatchResourceUsage{Resources: make([]ResourceUsage, 0, len(classes))}
	for _, class := range classes {
		capacity := resourceBudgetForClass(class)
		usedUnits := used[class]
		out.Resources = append(out.Resources, ResourceUsage{
			ResourceClass: class,
			Capacity:      capacity,
			Used:          usedUnits,
			Remaining:     capacity - usedUnits,
		})
	}
	return out
}

func selectReadyBatch(actions map[graph.ActionID]graph.Action, ready []graph.ActionID) ([]graph.ActionID, []graph.ActionID) {
	if len(ready) <= 1 {
		return append([]graph.ActionID(nil), ready...), nil
	}
	used := map[string]int{}
	current := make([]graph.ActionID, 0, len(ready))
	leftover := make([]graph.ActionID, 0, len(ready))
	for _, id := range ready {
		action, ok := actions[id]
		if !ok {
			continue
		}
		workerClass := workerClassForAction(action)
		resourceClass := resourceClassForWorkerClass(workerClass)
		resourceCost := resourceCostForWorkerClass(workerClass)
		if used[resourceClass]+resourceCost > resourceBudgetForClass(resourceClass) {
			leftover = append(leftover, id)
			continue
		}
		used[resourceClass] += resourceCost
		current = append(current, id)
	}
	if len(current) == 0 && len(ready) > 0 {
		return []graph.ActionID{ready[0]}, append([]graph.ActionID(nil), ready[1:]...)
	}
	return current, leftover
}

func (m *Model) androidActionsForResolvedCommand(g *graph.Graph, modulePath, command string, requestedVariants []project.ResolvedVariant) ([]graph.Action, error) {
	names := resolvedVariantNames(requestedVariants)
	switch command {
	case "compile-debug", "compileDebugSources", "compileReleaseSources":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, names, "compile")), nil
	case "lint-debug", "lintDebug", "lint-release", "lintRelease", "lint":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, names, "lint")), nil
	case "install", "install-debug", "installDebug", "installRelease":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, names, "install")), nil
	case "assemble-debug", "assembleDebug", "assemble-release", "assembleRelease", "assemble":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, names, "assemble")), nil
	case "build", "buildNeeded":
		actions := m.actionsForVariants(g, modulePath, names, "assemble")
		actions = append(actions, m.actionsForVariants(g, modulePath, debugVariantNames(requestedVariants), "test")...)
		return expandActionDependencies(g, actions), nil
	case "buildDependents":
		modulePaths, err := m.DependentModules(modulePath)
		if err != nil {
			return nil, err
		}
		var out []graph.Action
		for _, depPath := range modulePaths {
			variants, err := m.VariantNames(depPath)
			if err != nil {
				return nil, err
			}
			out = append(out, m.actionsForVariants(g, depPath, variants, "assemble")...)
		}
		return expandActionDependencies(g, out), nil
	case "test-debug-unit", "testDebugUnitTest", "test", "check":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, debugVariantNames(requestedVariants), "test")), nil
	case "compileDebugUnitTestSources", "assembleUnitTest":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, debugVariantNames(requestedVariants), "compile-tests")), nil
	case "compileDebugAndroidTestSources", "assembleAndroidTest":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, debugVariantNames(requestedVariants), "compile-android-tests")), nil
	default:
		return nil, nil
	}
}

func (m *Model) jvmActionsForResolvedCommand(g *graph.Graph, modulePath, command string, requestedVariants []project.ResolvedVariant) ([]graph.Action, error) {
	variants := resolvedVariantNames(requestedVariants)
	if len(variants) == 0 {
		variants = m.requestedVariants(modulePath, nil)
	}
	switch command {
	case "compile", "compile-debug", "compileDebugSources", "compileReleaseSources", "assemble", "assembleDebug", "assembleRelease":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, variants, "compile")), nil
	case "build", "buildNeeded":
		actions := m.actionsForVariants(g, modulePath, variants, "compile")
		actions = append(actions, m.actionsForVariants(g, modulePath, variants, "test")...)
		return expandActionDependencies(g, actions), nil
	case "test", "check":
		actions := m.actionsForVariants(g, modulePath, variants, "compile")
		actions = append(actions, m.actionsForVariants(g, modulePath, variants, "test")...)
		return expandActionDependencies(g, actions), nil
	case "compileDebugUnitTestSources", "assembleUnitTest":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, variants, "compile-tests")), nil
	case "compileDebugAndroidTestSources", "assembleAndroidTest":
		return expandActionDependencies(g, m.actionsForVariants(g, modulePath, variants, "compile-android-tests")), nil
	case "buildDependents":
		modulePaths, err := m.DependentModules(modulePath)
		if err != nil {
			return nil, err
		}
		var out []graph.Action
		for _, depPath := range modulePaths {
			mod, ok := m.Module(depPath)
			if !ok {
				continue
			}
			depVariants := m.requestedVariants(depPath, nil)
			operation := "assemble"
			if mod.Kind == string(graph.ModuleKindJvmLibrary) {
				operation = "compile"
			}
			out = append(out, m.actionsForVariants(g, depPath, depVariants, operation)...)
		}
		return expandActionDependencies(g, out), nil
	default:
		return nil, nil
	}
}

func resolvedVariantNames(requested []project.ResolvedVariant) []string {
	out := make([]string, 0, len(requested))
	for _, variant := range requested {
		name := strings.TrimSpace(variant.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func debugVariantNames(requested []project.ResolvedVariant) []string {
	if len(requested) == 0 {
		return []string{"debug"}
	}
	out := make([]string, 0, len(requested))
	for _, variant := range requested {
		buildType := strings.TrimSpace(variant.Coordinate.BuildType)
		if buildType == "" {
			buildType = strings.TrimSpace(variant.Config.BaseBuildType)
		}
		if buildType != "debug" {
			continue
		}
		name := strings.TrimSpace(variant.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	if len(out) > 0 {
		return out
	}
	names := resolvedVariantNames(requested)
	if len(names) > 0 {
		return names
	}
	return []string{"debug"}
}

func (m *Model) actionsForVariants(g *graph.Graph, modulePath string, requestedVariants []string, operation string) []graph.Action {
	mod, ok := m.Module(modulePath)
	if !ok {
		return nil
	}
	variantNames := m.requestedVariants(modulePath, requestedVariants)
	if len(requestedVariants) > 0 && len(variantNames) == 0 {
		return nil
	}
	variantSet := map[string]struct{}{}
	for _, variant := range variantNames {
		variantSet[variant] = struct{}{}
	}
	var out []graph.Action
	for _, variant := range mod.Variants {
		if len(variantSet) > 0 {
			if _, ok := variantSet[variant.Name]; !ok {
				continue
			}
		}
		for _, action := range g.ActionsForVariant(graph.VariantID(variant.ID)) {
			if action.Attributes["operation"] == operation {
				out = append(out, action)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Model) requestedVariants(modulePath string, requested []string) []string {
	names, err := m.VariantNames(modulePath)
	if err != nil {
		return nil
	}
	available := map[string]struct{}{}
	for _, name := range names {
		available[strings.TrimSpace(name)] = struct{}{}
	}
	var out []string
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if _, ok := available[name]; ok {
			out = append(out, name)
		}
	}
	if len(requested) > 0 {
		if len(out) == 0 && len(names) == 1 && names[0] == "main" {
			return names
		}
		return out
	}
	if len(out) > 0 {
		return out
	}
	return names
}

func SummaryFromProject(prj *project.Project) project.SemanticGraphSummary {
	if prj == nil {
		return project.SemanticGraphSummary{}
	}
	return semanticGraphSummary(prj.SemanticGraphDetailed())
}
