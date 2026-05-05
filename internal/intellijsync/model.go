package intellijsync

import "strings"

type Model struct {
	Repo        string   `json:"repo"`
	ProjectName string   `json:"projectName"`
	CacheKey    string   `json:"cacheKey,omitempty"`
	Project     Project  `json:"project"`
	Modules     []Module `json:"modules,omitempty"`
}

type Project struct {
	Name               string            `json:"name"`
	RootDir            string            `json:"rootDir"`
	SettingsFile       string            `json:"settingsFile,omitempty"`
	RootBuildFile      string            `json:"rootBuildFile,omitempty"`
	GradleProperties   map[string]string `json:"gradleProperties,omitempty"`
	VersionCatalogs    []string          `json:"versionCatalogs,omitempty"`
	Repositories       []Repository      `json:"repositories,omitempty"`
	RootPlugins        []string          `json:"rootPlugins,omitempty"`
	RecommendedBackend string            `json:"recommendedBackend,omitempty"`
}

type Repository struct {
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	URL               string   `json:"url,omitempty"`
	Scope             string   `json:"scope"`
	Exclusive         bool     `json:"exclusive,omitempty"`
	Priority          int      `json:"priority,omitempty"`
	Origin            string   `json:"origin,omitempty"`
	OfflineAllowed    bool     `json:"offlineAllowed,omitempty"`
	IncludeGroups     []string `json:"includeGroups,omitempty"`
	IncludeGroupRegex []string `json:"includeGroupRegex,omitempty"`
	ExcludeGroups     []string `json:"excludeGroups,omitempty"`
	ExcludeGroupRegex []string `json:"excludeGroupRegex,omitempty"`
	IncludeModules    []string `json:"includeModules,omitempty"`
	ExcludeModules    []string `json:"excludeModules,omitempty"`
}

type Module struct {
	Path                      string        `json:"path"`
	Name                      string        `json:"name,omitempty"`
	Identity                  Identity      `json:"identity,omitempty"`
	Dir                       string        `json:"dir,omitempty"`
	BuildFile                 string        `json:"buildFile,omitempty"`
	Kind                      string        `json:"kind,omitempty"`
	GraphKind                 string        `json:"graphKind,omitempty"`
	Namespace                 string        `json:"namespace,omitempty"`
	ApplicationID             string        `json:"applicationId,omitempty"`
	CompileSDK                string        `json:"compileSdk,omitempty"`
	BuildToolsVersion         string        `json:"buildToolsVersion,omitempty"`
	MinSDK                    string        `json:"minSdk,omitempty"`
	TargetSDK                 string        `json:"targetSdk,omitempty"`
	TestInstrumentationRunner string        `json:"testInstrumentationRunner,omitempty"`
	SourceFileCount           int           `json:"sourceFileCount,omitempty"`
	UnitTestFileCount         int           `json:"unitTestFileCount,omitempty"`
	AndroidTestFileCount      int           `json:"androidTestFileCount,omitempty"`
	UsesCompose               bool          `json:"usesCompose,omitempty"`
	UsesKotlinSerialization   bool          `json:"usesKotlinSerialization,omitempty"`
	UsesMetro                 bool          `json:"usesMetro,omitempty"`
	UsesWire                  bool          `json:"usesWire,omitempty"`
	KotlinFreeCompilerArgs    []string      `json:"kotlinFreeCompilerArgs,omitempty"`
	LintDisabledChecks        []string      `json:"lintDisabledChecks,omitempty"`
	ConsumerProguardFiles     []string      `json:"consumerProguardFiles,omitempty"`
	DefaultTasks              []string      `json:"defaultTasks,omitempty"`
	Tasks                     []Task        `json:"tasks,omitempty"`
	TaskCatalog               []TaskCatalog `json:"taskCatalog,omitempty"`
	Dependencies              []Dependency  `json:"dependencies,omitempty"`
	Variants                  []Variant     `json:"variants,omitempty"`
}

type Dependency struct {
	Kind                    string `json:"kind,omitempty"`
	Level                   string `json:"level,omitempty"`
	TargetNodeID            string `json:"targetNodeId,omitempty"`
	TargetModulePath        string `json:"targetModulePath,omitempty"`
	TargetVariantName       string `json:"targetVariantName,omitempty"`
	TargetMaterializationID string `json:"targetMaterializationId,omitempty"`
}

type Variant struct {
	ID                        string          `json:"id,omitempty"`
	Name                      string          `json:"name,omitempty"`
	Identity                  Identity        `json:"identity,omitempty"`
	DeclaredName              string          `json:"declaredName,omitempty"`
	CoordinateName            string          `json:"coordinateName,omitempty"`
	DisplayName               string          `json:"displayName,omitempty"`
	Compatibility             Compatibility   `json:"compatibility,omitempty"`
	BuildType                 string          `json:"buildType,omitempty"`
	Flavors                   []string        `json:"flavors,omitempty"`
	CompileSDK                string          `json:"compileSdk,omitempty"`
	ApplicationID             string          `json:"applicationId,omitempty"`
	ApplicationIDSuffix       string          `json:"applicationIdSuffix,omitempty"`
	VersionName               string          `json:"versionName,omitempty"`
	VersionNameSuffix         string          `json:"versionNameSuffix,omitempty"`
	MinSDK                    string          `json:"minSdk,omitempty"`
	TargetSDK                 string          `json:"targetSdk,omitempty"`
	TestInstrumentationRunner string          `json:"testInstrumentationRunner,omitempty"`
	ProguardFiles             []string        `json:"proguardFiles,omitempty"`
	ConsumerProguardFiles     []string        `json:"consumerProguardFiles,omitempty"`
	SourceSetOrder            []string        `json:"sourceSetOrder,omitempty"`
	SourceSetNames            []string        `json:"sourceSetNames,omitempty"`
	TaskAliases               []string        `json:"taskAliases,omitempty"`
	TaskCatalog               []TaskCatalog   `json:"taskCatalog,omitempty"`
	ModelSelectors            []string        `json:"modelSelectors,omitempty"`
	SyncFragments             []string        `json:"syncFragments,omitempty"`
	ContentRoots              []ContentRoot   `json:"contentRoots,omitempty"`
	Materialization           Materialization `json:"materialization,omitempty"`
	Dependencies              []Dependency    `json:"dependencies,omitempty"`
	OrderEntries              []OrderEntry    `json:"orderEntries,omitempty"`
	Actions                   []Action        `json:"actions,omitempty"`
	Targets                   []Target        `json:"targets,omitempty"`
}

type Compatibility struct {
	VariantName    string   `json:"variantName,omitempty"`
	CoordinateName string   `json:"coordinateName,omitempty"`
	DisplayName    string   `json:"displayName,omitempty"`
	SourceSetOrder []string `json:"sourceSetOrder,omitempty"`
	SourceSetNames []string `json:"sourceSetNames,omitempty"`
	TaskAliases    []string `json:"taskAliases,omitempty"`
	ModelSelectors []string `json:"modelSelectors,omitempty"`
	SyncFragments  []string `json:"syncFragments,omitempty"`
}

type Identity struct {
	GraphModuleID   string   `json:"graphModuleId,omitempty"`
	GraphVariantID  string   `json:"graphVariantId,omitempty"`
	ModulePath      string   `json:"modulePath,omitempty"`
	VariantName     string   `json:"variantName,omitempty"`
	DeclaredName    string   `json:"declaredName,omitempty"`
	CoordinateName  string   `json:"coordinateName,omitempty"`
	IDEModuleID     string   `json:"ideModuleId,omitempty"`
	IDEVariantID    string   `json:"ideVariantId,omitempty"`
	IDESourceSetIDs []string `json:"ideSourceSetIds,omitempty"`
	ModelSelectors  []string `json:"modelSelectors,omitempty"`
	SyncFragments   []string `json:"syncFragments,omitempty"`
}

type Materialization struct {
	ID                   string     `json:"id,omitempty"`
	Mode                 string     `json:"mode,omitempty"`
	ArtifactSnapshotID   string     `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs []string   `json:"classpathSnapshotIds,omitempty"`
	SourceRoots          []string   `json:"sourceRoots,omitempty"`
	ManifestPaths        []string   `json:"manifestPaths,omitempty"`
	BackingArtifactID    string     `json:"backingArtifactId,omitempty"`
	ProducedArtifactIDs  []string   `json:"producedArtifactIds,omitempty"`
	ProducedArtifacts    []Artifact `json:"producedArtifacts,omitempty"`
}

type Artifact struct {
	ID                 string `json:"id,omitempty"`
	Kind               string `json:"kind,omitempty"`
	Path               string `json:"path,omitempty"`
	ProducedByActionID string `json:"producedByActionId,omitempty"`
}

type ContentRoot struct {
	Path    string         `json:"path"`
	Entries []ContentEntry `json:"entries,omitempty"`
}

type ContentEntry struct {
	Path        string `json:"path"`
	Kind        string `json:"kind,omitempty"`
	Generated   bool   `json:"generated,omitempty"`
	SourceSet   string `json:"sourceSet,omitempty"`
	VariantName string `json:"variantName,omitempty"`
}

type Action struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Operation   string   `json:"operation,omitempty"`
	ModulePath  string   `json:"modulePath,omitempty"`
	VariantName string   `json:"variantName,omitempty"`
	Inputs      []string `json:"inputs,omitempty"`
	Outputs     []string `json:"outputs,omitempty"`
	Note        string   `json:"note,omitempty"`
}

type Target struct {
	Kind         string   `json:"kind,omitempty"`
	Name         string   `json:"name,omitempty"`
	TaskName     string   `json:"taskName,omitempty"`
	TaskNames    []string `json:"taskNames,omitempty"`
	ActionID     string   `json:"actionId,omitempty"`
	ActionName   string   `json:"actionName,omitempty"`
	ActionKind   string   `json:"actionKind,omitempty"`
	ArtifactIDs  []string `json:"artifactIds,omitempty"`
	ArtifactPath string   `json:"artifactPath,omitempty"`
	PackageName  string   `json:"packageName,omitempty"`
	ManifestPath string   `json:"manifestPath,omitempty"`
	Supported    bool     `json:"supported,omitempty"`
}

type Task struct {
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Supported   bool   `json:"supported"`
}

type TaskCatalog struct {
	RawName           string `json:"rawName"`
	NormalizedCommand string `json:"normalizedCommand,omitempty"`
	Category          string `json:"category,omitempty"`
	Kind              string `json:"kind,omitempty"`
	TargetVariant     string `json:"targetVariant,omitempty"`
	Supported         bool   `json:"supported,omitempty"`
	Runnable          bool   `json:"runnable,omitempty"`
	Test              bool   `json:"test,omitempty"`
	Install           bool   `json:"install,omitempty"`
}

func (m *Model) Module(path string) (Module, bool) {
	if m == nil {
		return Module{}, false
	}
	for _, mod := range m.Modules {
		if mod.Path == path {
			return cloneModule(mod), true
		}
	}
	return Module{}, false
}

func (m Module) Variant(name string) (Variant, bool) {
	name = strings.TrimSpace(name)
	for _, variant := range m.Variants {
		if variant.Name == name {
			return cloneVariant(variant), true
		}
	}
	return Variant{}, false
}

func cloneModule(mod Module) Module {
	mod.Identity = cloneIdentity(mod.Identity)
	mod.KotlinFreeCompilerArgs = cloneStringSlice(mod.KotlinFreeCompilerArgs)
	mod.LintDisabledChecks = cloneStringSlice(mod.LintDisabledChecks)
	mod.ConsumerProguardFiles = cloneStringSlice(mod.ConsumerProguardFiles)
	mod.DefaultTasks = cloneStringSlice(mod.DefaultTasks)
	mod.Tasks = append([]Task(nil), mod.Tasks...)
	mod.TaskCatalog = append([]TaskCatalog(nil), mod.TaskCatalog...)
	mod.Dependencies = append([]Dependency(nil), mod.Dependencies...)
	mod.Variants = cloneVariants(mod.Variants)
	return mod
}

func cloneVariants(variants []Variant) []Variant {
	if len(variants) == 0 {
		return nil
	}
	out := make([]Variant, 0, len(variants))
	for _, variant := range variants {
		out = append(out, cloneVariant(variant))
	}
	return out
}

func cloneVariant(variant Variant) Variant {
	variant.Identity = cloneIdentity(variant.Identity)
	variant.Compatibility = cloneCompatibility(variant.Compatibility)
	variant.Flavors = cloneStringSlice(variant.Flavors)
	variant.ProguardFiles = cloneStringSlice(variant.ProguardFiles)
	variant.ConsumerProguardFiles = cloneStringSlice(variant.ConsumerProguardFiles)
	variant.SourceSetOrder = cloneStringSlice(variant.SourceSetOrder)
	variant.SourceSetNames = cloneStringSlice(variant.SourceSetNames)
	variant.TaskAliases = cloneStringSlice(variant.TaskAliases)
	variant.TaskCatalog = append([]TaskCatalog(nil), variant.TaskCatalog...)
	variant.ModelSelectors = cloneStringSlice(variant.ModelSelectors)
	variant.SyncFragments = cloneStringSlice(variant.SyncFragments)
	variant.ContentRoots = cloneContentRoots(variant.ContentRoots)
	variant.Materialization = cloneMaterialization(variant.Materialization)
	variant.Dependencies = append([]Dependency(nil), variant.Dependencies...)
	variant.OrderEntries = append([]OrderEntry(nil), variant.OrderEntries...)
	variant.Actions = cloneActions(variant.Actions)
	variant.Targets = cloneTargets(variant.Targets)
	return variant
}

func cloneIdentity(identity Identity) Identity {
	identity.IDESourceSetIDs = cloneStringSlice(identity.IDESourceSetIDs)
	identity.ModelSelectors = cloneStringSlice(identity.ModelSelectors)
	identity.SyncFragments = cloneStringSlice(identity.SyncFragments)
	return identity
}

func cloneCompatibility(compatibility Compatibility) Compatibility {
	compatibility.SourceSetOrder = cloneStringSlice(compatibility.SourceSetOrder)
	compatibility.SourceSetNames = cloneStringSlice(compatibility.SourceSetNames)
	compatibility.TaskAliases = cloneStringSlice(compatibility.TaskAliases)
	compatibility.ModelSelectors = cloneStringSlice(compatibility.ModelSelectors)
	compatibility.SyncFragments = cloneStringSlice(compatibility.SyncFragments)
	return compatibility
}

func cloneContentRoots(roots []ContentRoot) []ContentRoot {
	if len(roots) == 0 {
		return nil
	}
	out := make([]ContentRoot, 0, len(roots))
	for _, root := range roots {
		root.Entries = append([]ContentEntry(nil), root.Entries...)
		out = append(out, root)
	}
	return out
}

func cloneMaterialization(materialization Materialization) Materialization {
	materialization.ClasspathSnapshotIDs = cloneStringSlice(materialization.ClasspathSnapshotIDs)
	materialization.SourceRoots = cloneStringSlice(materialization.SourceRoots)
	materialization.ManifestPaths = cloneStringSlice(materialization.ManifestPaths)
	materialization.ProducedArtifactIDs = cloneStringSlice(materialization.ProducedArtifactIDs)
	materialization.ProducedArtifacts = append([]Artifact(nil), materialization.ProducedArtifacts...)
	return materialization
}

func cloneActions(actions []Action) []Action {
	if len(actions) == 0 {
		return nil
	}
	out := make([]Action, 0, len(actions))
	for _, action := range actions {
		action.Inputs = cloneStringSlice(action.Inputs)
		action.Outputs = cloneStringSlice(action.Outputs)
		out = append(out, action)
	}
	return out
}

func cloneTargets(targets []Target) []Target {
	if len(targets) == 0 {
		return nil
	}
	out := make([]Target, 0, len(targets))
	for _, target := range targets {
		target.TaskNames = cloneStringSlice(target.TaskNames)
		target.ArtifactIDs = cloneStringSlice(target.ArtifactIDs)
		out = append(out, target)
	}
	return out
}

func cloneStringSlice(values []string) []string {
	return append([]string(nil), values...)
}

func (m Module) HasTask(name string) bool {
	for _, task := range m.Tasks {
		if task.Name == name {
			return true
		}
	}
	return false
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
