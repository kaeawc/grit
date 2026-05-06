package project

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/modulebuild"
)

type Project struct {
	Name               string
	RootDir            string
	SettingsFile       string
	RootBuildFile      string
	ModuleDirs         map[string]string
	GradleProperties   map[string]string
	VersionCatalog     string
	VersionCatalogs    []string
	VersionCatalogData map[string]string
	Repositories       []Repository
	RootPlugins        []string
	Modules            []Module
	RecommendedBackend string
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
	Path                      string
	Dir                       string
	BuildFile                 string
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
	SourceFileCount           int
	UnitTestFileCount         int
	AndroidTestFileCount      int
	AidlFiles                 []string
	UsesCompose               bool
	UsesKotlinSerialization   bool
	UsesMetro                 bool
	UsesWire                  bool
	WireConfig                WireConfig
	UsesKSP                   bool
	KSP                       modulebuild.KSPConfig
	Plugins                   []string
	BuildFeatures             BuildFeatures
	CompilerPlugins           *modulebuild.PluginRegistry
	KotlinFreeCompilerArgs    []string
	LintDisabledChecks        []string
	ConsumerProguardFiles     []string
	SigningConfigs            map[string]SigningConfig
	DefaultConfig             DefaultConfig
	FlavorDimensions          []string
	ProductFlavors            map[string]ProductFlavor
	BuildTypes                map[string]BuildType
	EnabledVariants           []string
}

// WireConfig captures the resolved settings from a `wire { }` block on a
// module that applies the `com.squareup.wire` Gradle plugin. Fields default
// to wire's documented defaults when the corresponding key is absent from
// the build script.
type WireConfig struct {
	// SourcePaths are proto source roots resolved relative to the module dir.
	// Defaults to "src/main/proto" when no `sourcePath { srcDir(...) }` is
	// declared (mirroring the wire plugin's default).
	SourcePaths []string
	// ProtoPaths are include-only proto roots that supply imported types but
	// do not produce generated sources.
	ProtoPaths []string
	// KotlinTarget is true when the wire { kotlin { } } block is present.
	KotlinTarget bool
	// JavaTarget is true when the wire { java { } } block is present.
	JavaTarget bool
	// JavaInterop mirrors `kotlin { javaInterop = true }`.
	JavaInterop bool
	// ProtoLibrary mirrors `protoLibrary = true`.
	ProtoLibrary bool
	// Includes / Excludes select / filter types within the proto roots.
	Includes []string
	Excludes []string
}

// BuildFeatures represents the android { buildFeatures { } } block.
type BuildFeatures struct {
	Compose     bool
	ViewBinding bool
	DataBinding bool
	BuildConfig bool
}

type VariantOptimization struct {
	MinifyEnabled        bool                  `json:"minifyEnabled"`
	ShrinkResources      bool                  `json:"shrinkResources"`
	PackageOptimizations []PackageOptimization `json:"packageOptimizations,omitempty"`
}

type PackageOptimization struct {
	PackageName     string `json:"packageName"`
	MinifyEnabled   *bool  `json:"minifyEnabled,omitempty"`
	ShrinkResources *bool  `json:"shrinkResources,omitempty"`
	Note            string `json:"note,omitempty"`
}

type SigningConfig struct {
	Name          string
	StoreFile     string
	StorePassword string
	KeyAlias      string
	KeyPassword   string
}

type ProductFlavor struct {
	Name                string              `json:"name"`
	Dimension           string              `json:"dimension,omitempty"`
	ApplicationID       string              `json:"applicationId,omitempty"`
	ApplicationIDSuffix string              `json:"applicationIdSuffix,omitempty"`
	VersionCode         string              `json:"versionCode,omitempty"`
	VersionName         string              `json:"versionName,omitempty"`
	VersionNameSuffix   string              `json:"versionNameSuffix,omitempty"`
	MinSDK              string              `json:"minSdk,omitempty"`
	TargetSDK           string              `json:"targetSdk,omitempty"`
	MatchingFallbacks   []string            `json:"matchingFallbacks,omitempty"`
	MissingDimensions   map[string][]string `json:"missingDimensionStrategies,omitempty"`
}

type DefaultConfig struct {
	ApplicationID     string              `json:"applicationId,omitempty"`
	VersionCode       string              `json:"versionCode,omitempty"`
	VersionName       string              `json:"versionName,omitempty"`
	MinSDK            string              `json:"minSdk,omitempty"`
	TargetSDK         string              `json:"targetSdk,omitempty"`
	MissingDimensions map[string][]string `json:"missingDimensionStrategies,omitempty"`
}

type BuildType struct {
	Name                string              `json:"name"`
	DeclaredName        string              `json:"declaredName,omitempty"`
	BaseBuildType       string              `json:"baseBuildType,omitempty"`
	Flavors             []string            `json:"flavors,omitempty"`
	ApplicationID       string              `json:"applicationId,omitempty"`
	ApplicationIDSuffix string              `json:"applicationIdSuffix,omitempty"`
	VersionCode         string              `json:"versionCode,omitempty"`
	VersionName         string              `json:"versionName,omitempty"`
	VersionNameSuffix   string              `json:"versionNameSuffix,omitempty"`
	MinSDK              string              `json:"minSdk,omitempty"`
	TargetSDK           string              `json:"targetSdk,omitempty"`
	MatchingFallbacks   []string            `json:"matchingFallbacks,omitempty"`
	Optimization        VariantOptimization `json:"optimization"`
	SigningConfig       string              `json:"signingConfig,omitempty"`
	ProguardFiles       []string            `json:"proguardFiles,omitempty"`
	IsMinifyEnabled     bool                `json:"-"`
	IsShrinkResources   bool                `json:"-"`
}

type Task struct {
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Supported   bool   `json:"supported"`
}

type VariantCompatibility struct {
	VariantName    string   `json:"variantName,omitempty"`
	CoordinateName string   `json:"coordinateName,omitempty"`
	DisplayName    string   `json:"displayName,omitempty"`
	SourceSetOrder []string `json:"sourceSetOrder,omitempty"`
	SourceSetNames []string `json:"sourceSetNames,omitempty"`
	TaskAliases    []string `json:"taskAliases,omitempty"`
	ModelSelectors []string `json:"modelSelectors,omitempty"`
	SyncFragments  []string `json:"syncFragments,omitempty"`
}

type ResolvedVariant struct {
	Name                      string                    `json:"name"`
	DeclaredName              string                    `json:"declaredName,omitempty"`
	CoordinateName            string                    `json:"coordinateName,omitempty"`
	ModulePath                string                    `json:"modulePath"`
	ModuleType                string                    `json:"moduleType,omitempty"`
	DisplayName               string                    `json:"displayName,omitempty"`
	Compatibility             VariantCompatibility      `json:"compatibility,omitempty"`
	Coordinate                VariantCoordinate         `json:"coordinate"`
	CompileSDK                string                    `json:"compileSdk,omitempty"`
	BuildToolsVersion         string                    `json:"buildToolsVersion,omitempty"`
	Namespace                 string                    `json:"namespace,omitempty"`
	Config                    BuildType                 `json:"config"`
	ApplicationID             string                    `json:"applicationId,omitempty"`
	ApplicationIDSuffix       string                    `json:"applicationIdSuffix,omitempty"`
	VersionCode               string                    `json:"versionCode,omitempty"`
	VersionName               string                    `json:"versionName,omitempty"`
	VersionNameSuffix         string                    `json:"versionNameSuffix,omitempty"`
	MinSDK                    string                    `json:"minSdk,omitempty"`
	TargetSDK                 string                    `json:"targetSdk,omitempty"`
	TestInstrumentationRunner string                    `json:"testInstrumentationRunner,omitempty"`
	MissingDimensions         map[string][]string       `json:"missingDimensionStrategies,omitempty"`
	Optimization              VariantOptimization       `json:"optimization"`
	ProguardFiles             []string                  `json:"proguardFiles,omitempty"`
	ConsumerProguardFiles     []string                  `json:"consumerProguardFiles,omitempty"`
	SigningConfig             string                    `json:"signingConfig,omitempty"`
	SigningConfigured         bool                      `json:"signingConfigured,omitempty"`
	DexMode                   string                    `json:"dexMode,omitempty"`
	MinifyEnabled             bool                      `json:"minifyEnabled,omitempty"`
	ShrinkResources           bool                      `json:"shrinkResources,omitempty"`
	Installable               bool                      `json:"installable,omitempty"`
	Testable                  bool                      `json:"testable,omitempty"`
	Debuggable                bool                      `json:"debuggable,omitempty"`
	MaterializationID         string                    `json:"materializationId,omitempty"`
	ArtifactSnapshotID        string                    `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs      []string                  `json:"classpathSnapshotIds,omitempty"`
	SourceRoots               []string                  `json:"sourceRoots,omitempty"`
	ManifestPaths             []string                  `json:"manifestPaths,omitempty"`
	BackingArtifactID         string                    `json:"backingArtifactId,omitempty"`
	BackingArtifactPath       string                    `json:"backingArtifactPath,omitempty"`
	ProducedArtifactIDs       []string                  `json:"producedArtifactIds,omitempty"`
	ProducedArtifactPaths     []string                  `json:"producedArtifactPaths,omitempty"`
	ProducedArtifacts         []ResolvedVariantArtifact `json:"producedArtifacts,omitempty"`
	ProducedArtifactKinds     []string                  `json:"producedArtifactKinds,omitempty"`
	InstallArtifactID         string                    `json:"installArtifactId,omitempty"`
	InstallArtifactPath       string                    `json:"installArtifactPath,omitempty"`
	ResourceArtifactIDs       []string                  `json:"resourceArtifactIds,omitempty"`
	ResourceArtifactPaths     []string                  `json:"resourceArtifactPaths,omitempty"`
	ManifestArtifactIDs       []string                  `json:"manifestArtifactIds,omitempty"`
	ManifestArtifactPaths     []string                  `json:"manifestArtifactPaths,omitempty"`
	SourceSetOrder            []string                  `json:"sourceSetOrder,omitempty"`
	SourceSetNames            []string                  `json:"sourceSetNames,omitempty"`
	TaskAliases               []string                  `json:"taskAliases,omitempty"`
	ModelSelectors            []string                  `json:"modelSelectors,omitempty"`
	SyncFragments             []string                  `json:"syncFragments,omitempty"`
	LintBaselinePath          string                    `json:"lintBaselinePath,omitempty"`
	InstallTask               string                    `json:"installTask,omitempty"`
	UninstallTask             string                    `json:"uninstallTask,omitempty"`
}

type ResolvedVariantArtifact struct {
	ID                 string `json:"id,omitempty"`
	Kind               string `json:"kind,omitempty"`
	Path               string `json:"path,omitempty"`
	ProducedByActionID string `json:"producedByActionId,omitempty"`
}

type VariantCoordinate struct {
	ModulePath string   `json:"modulePath,omitempty"`
	Name       string   `json:"name"`
	BuildType  string   `json:"buildType,omitempty"`
	Flavors    []string `json:"flavors,omitempty"`
}

func (m Module) DefaultTasks() []string {
	switch m.Type {
	case "android-application":
		variant := m.defaultAndroidTaskVariant()
		return []string{
			m.Path + ":assemble" + taskNameSuffix(variant),
			m.Path + ":install" + taskNameSuffix(variant),
			m.Path + ":test" + taskNameSuffix(variant) + "UnitTest",
		}
	case "android-library":
		variant := m.defaultAndroidTaskVariant()
		return []string{
			m.Path + ":assemble" + taskNameSuffix(variant),
			m.Path + ":test" + taskNameSuffix(variant) + "UnitTest",
		}
	case "jvm-library":
		return []string{m.Path + ":build", m.Path + ":test"}
	default:
		return nil
	}
}

func (m Module) ResolveVariant(name string) ResolvedVariant {
	if strings.TrimSpace(name) == "" {
		name = m.DefaultVariantName()
	}
	config := m.variantConfigForName(name)
	buildType := config.Name
	if strings.TrimSpace(config.BaseBuildType) != "" {
		buildType = config.BaseBuildType
	}
	resolvedName := m.variantResolvedName(config, buildType)
	coordinateName := m.variantCoordinateName(config, buildType)
	config.Name = resolvedName
	sourceSetOrder := m.variantSourceSetOrder(config, buildType)
	taskAliases := m.variantTaskAliases(config, buildType)
	modelSelectors := m.variantModelSelectors(config, buildType)
	sourceSetNames := m.variantCompatibilitySourceSetNames(sourceSetOrder)
	syncFragments := m.variantSyncFragments(config, buildType, coordinateName, sourceSetOrder)
	compatibility := VariantCompatibility{
		VariantName:    resolvedName,
		CoordinateName: coordinateName,
		DisplayName:    m.variantDisplayName(config, buildType),
		SourceSetOrder: append([]string(nil), sourceSetOrder...),
		SourceSetNames: append([]string(nil), sourceSetNames...),
		TaskAliases:    append([]string(nil), taskAliases...),
		ModelSelectors: append([]string(nil), modelSelectors...),
		SyncFragments:  append([]string(nil), syncFragments...),
	}
	return ResolvedVariant{
		Name:           config.Name,
		DeclaredName:   strings.TrimSpace(config.DeclaredName),
		CoordinateName: coordinateName,
		ModulePath:     m.Path,
		DisplayName:    compatibility.DisplayName,
		Compatibility:  compatibility,
		Coordinate: VariantCoordinate{
			ModulePath: m.Path,
			Name:       coordinateName,
			BuildType:  buildType,
			Flavors:    append([]string(nil), config.Flavors...),
		},
		ModuleType:                m.Type,
		CompileSDK:                m.CompileSDK,
		BuildToolsVersion:         m.BuildToolsVersion,
		Namespace:                 m.Namespace,
		Config:                    config,
		ApplicationID:             resolvedApplicationID(m.DefaultConfig.ApplicationID, config),
		ApplicationIDSuffix:       config.ApplicationIDSuffix,
		VersionCode:               firstNonEmpty(config.VersionCode, m.DefaultConfig.VersionCode, m.VersionCode),
		VersionName:               resolvedVersionName(m.DefaultConfig.VersionName, config),
		VersionNameSuffix:         config.VersionNameSuffix,
		MinSDK:                    firstNonEmpty(config.MinSDK, m.DefaultConfig.MinSDK, m.MinSDK),
		TargetSDK:                 firstNonEmpty(config.TargetSDK, m.DefaultConfig.TargetSDK, m.TargetSDK),
		TestInstrumentationRunner: m.TestInstrumentationRunner,
		MissingDimensions:         cloneStrategyMap(mergedMissingDimensions(m, config.Flavors)),
		Optimization:              config.Optimization,
		ProguardFiles:             append([]string(nil), config.ProguardFiles...),
		ConsumerProguardFiles:     append([]string(nil), m.ConsumerProguardFiles...),
		SigningConfig:             config.SigningConfig,
		SigningConfigured:         strings.TrimSpace(config.SigningConfig) != "",
		MinifyEnabled:             config.Optimization.MinifyEnabled,
		ShrinkResources:           config.Optimization.ShrinkResources,
		Installable:               m.Type == "android-application",
		Testable:                  m.IsJVM() || buildType == "debug",
		Debuggable:                buildType == "debug",
		SourceSetOrder:            sourceSetOrder,
		SourceSetNames:            sourceSetNames,
		TaskAliases:               taskAliases,
		ModelSelectors:            modelSelectors,
		SyncFragments:             syncFragments,
		InstallTask:               variantPrimaryTaskAlias(m.Type, resolvedName, "install"),
		UninstallTask:             variantPrimaryTaskAlias(m.Type, resolvedName, "uninstall"),
	}
}

func variantPrimaryTaskAlias(moduleType, variantName, prefix string) string {
	if moduleType != "android-application" {
		return ""
	}
	suffix := taskNameSuffix(variantName)
	switch strings.TrimSpace(prefix) {
	case "install":
		return "install" + suffix
	case "uninstall":
		return "uninstall" + suffix
	default:
		return ""
	}
}

func resolvedVariantArtifactKinds(artifacts []ResolvedVariantArtifact) []string {
	if len(artifacts) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		kind := strings.TrimSpace(artifact.Kind)
		if kind == "" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

func resolvedVariantFirstArtifactIDByKind(artifacts []ResolvedVariantArtifact, kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ""
	}
	for _, artifact := range artifacts {
		if artifact.Kind == kind && strings.TrimSpace(artifact.ID) != "" {
			return artifact.ID
		}
	}
	return ""
}

func resolvedVariantArtifactIDsByKind(artifacts []ResolvedVariantArtifact, kind string) []string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil
	}
	out := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Kind == kind && strings.TrimSpace(artifact.ID) != "" {
			out = append(out, artifact.ID)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func resolvedVariantArtifactPaths(artifacts []ResolvedVariantArtifact) []string {
	out := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if path := strings.TrimSpace(artifact.Path); path != "" {
			out = append(out, path)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func resolvedVariantArtifactPathsByKind(artifacts []ResolvedVariantArtifact, kind string) []string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil
	}
	out := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Kind == kind && strings.TrimSpace(artifact.Path) != "" {
			out = append(out, artifact.Path)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func resolvedVariantFirstArtifactPathByKind(artifacts []ResolvedVariantArtifact, kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ""
	}
	for _, artifact := range artifacts {
		if artifact.Kind == kind && strings.TrimSpace(artifact.Path) != "" {
			return artifact.Path
		}
	}
	return ""
}

func (m Module) ResolvedVariants() []ResolvedVariant {
	variants := m.Variants()
	if len(variants) == 0 {
		return []ResolvedVariant{m.ResolveVariant(m.DefaultVariantName())}
	}
	out := make([]ResolvedVariant, 0, len(variants))
	for _, variant := range variants {
		out = append(out, m.ResolveVariant(variant.Name))
	}
	return out
}

func (m Module) ResolveVariants(requested []string) []ResolvedVariant {
	if len(requested) == 0 {
		return m.ResolvedVariants()
	}
	available := map[string]struct{}{}
	for _, variant := range m.Variants() {
		available[variant.Name] = struct{}{}
	}
	var out []ResolvedVariant
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := available[name]; ok {
			out = append(out, m.ResolveVariant(name))
			continue
		}
		out = append(out, m.ResolveVariant(name))
	}
	if len(out) > 0 {
		return out
	}
	return nil
}

func (m Module) Tasks() []Task {
	switch m.Type {
	case "android-application":
		tasks := []Task{
			{Name: "assemble", Category: "build", Description: "Assemble main outputs for all variants.", Supported: true},
			{Name: "build", Category: "build", Description: "Assemble and run supported tests.", Supported: true},
			{Name: "clean", Category: "build", Description: "Delete grit build outputs.", Supported: true},
			{Name: "test", Category: "verification", Description: "Run supported unit tests.", Supported: true},
			{Name: "check", Category: "verification", Description: "Run supported checks.", Supported: true},
			{Name: "signingReport", Category: "android", Description: "Display signing info.", Supported: true},
			{Name: "tasks", Category: "help", Description: "Display runnable tasks.", Supported: true},
			{Name: "artifactTransforms", Category: "help", Description: "Display artifact transform behavior.", Supported: true},
			{Name: "dependencies", Category: "help", Description: "Display module dependencies.", Supported: true},
			{Name: "projects", Category: "help", Description: "Display project modules.", Supported: true},
			{Name: "properties", Category: "help", Description: "Display module properties.", Supported: true},
			{Name: "buildEnvironment", Category: "help", Description: "Display environment and repository configuration.", Supported: true},
			{Name: "dependencyInsight", Category: "help", Description: "Display resolved dependency information.", Supported: true},
			{Name: "javaToolchains", Category: "help", Description: "Display Java and Kotlin toolchain information.", Supported: true},
			{Name: "kotlinDslAccessorsReport", Category: "help", Description: "Display detected Kotlin DSL accessors.", Supported: true},
			{Name: "outgoingVariants", Category: "help", Description: "Display published variants for this module.", Supported: true},
			{Name: "resolvableConfigurations", Category: "help", Description: "Display supported resolvable configurations.", Supported: true},
			{Name: "bundle", Category: "build", Description: "Assemble bundles for all variants.", Supported: false},
			{Name: "assembleAndroidTest", Category: "build", Description: "Assemble androidTest outputs.", Supported: false},
			{Name: "assembleUnitTest", Category: "build", Description: "Assemble unit test outputs.", Supported: true},
			{Name: "buildDependents", Category: "build", Description: "Build projects depending on this module.", Supported: true},
			{Name: "buildNeeded", Category: "build", Description: "Build dependent project requirements.", Supported: true},
			{Name: "lint", Category: "verification", Description: "Run lint.", Supported: false},
			{Name: "lintDebug", Category: "verification", Description: "Run lint for debug.", Supported: false},
			{Name: "lintRelease", Category: "verification", Description: "Run lint for release.", Supported: false},
			{Name: "lintVitalRelease", Category: "verification", Description: "Run vital lint for release.", Supported: false},
			{Name: "lintFix", Category: "verification", Description: "Apply lint fixes.", Supported: false},
			{Name: "connectedAndroidTest", Category: "verification", Description: "Run connected instrumentation tests.", Supported: false},
			{Name: "connectedDebugAndroidTest", Category: "verification", Description: "Run debug connected instrumentation tests.", Supported: false},
			{Name: "connectedCheck", Category: "verification", Description: "Run connected checks.", Supported: false},
			{Name: "deviceAndroidTest", Category: "verification", Description: "Run device-provider tests.", Supported: false},
			{Name: "deviceCheck", Category: "verification", Description: "Run device-provider checks.", Supported: false},
			{Name: "installDebugAndroidTest", Category: "install", Description: "Install debug androidTest build.", Supported: true},
			{Name: "uninstallAll", Category: "install", Description: "Uninstall all application variants.", Supported: true},
			{Name: "uninstallDebug", Category: "install", Description: "Uninstall the debug build.", Supported: true},
			{Name: "uninstallDebugAndroidTest", Category: "install", Description: "Uninstall the debug androidTest build.", Supported: true},
			{Name: "uninstallRelease", Category: "install", Description: "Uninstall the release build.", Supported: true},
		}
		tasks = append(tasks, m.androidVariantTasks(true)...)
		return append(tasks, m.qualityTasks()...)
	case "android-library":
		tasks := []Task{
			{Name: "assemble", Category: "build", Description: "Assemble outputs for all variants.", Supported: true},
			{Name: "build", Category: "build", Description: "Assemble and run supported tests.", Supported: true},
			{Name: "clean", Category: "build", Description: "Delete grit build outputs.", Supported: true},
			{Name: "test", Category: "verification", Description: "Run supported unit tests.", Supported: true},
			{Name: "check", Category: "verification", Description: "Run supported checks.", Supported: true},
			{Name: "tasks", Category: "help", Description: "Display runnable tasks.", Supported: true},
			{Name: "artifactTransforms", Category: "help", Description: "Display artifact transform behavior.", Supported: true},
			{Name: "dependencies", Category: "help", Description: "Display module dependencies.", Supported: true},
			{Name: "projects", Category: "help", Description: "Display project modules.", Supported: true},
			{Name: "properties", Category: "help", Description: "Display module properties.", Supported: true},
			{Name: "buildEnvironment", Category: "help", Description: "Display environment and repository configuration.", Supported: true},
			{Name: "dependencyInsight", Category: "help", Description: "Display resolved dependency information.", Supported: true},
			{Name: "javaToolchains", Category: "help", Description: "Display Java and Kotlin toolchain information.", Supported: true},
			{Name: "kotlinDslAccessorsReport", Category: "help", Description: "Display detected Kotlin DSL accessors.", Supported: true},
			{Name: "outgoingVariants", Category: "help", Description: "Display published variants for this module.", Supported: true},
			{Name: "resolvableConfigurations", Category: "help", Description: "Display supported resolvable configurations.", Supported: true},
			{Name: "assembleUnitTest", Category: "build", Description: "Assemble unit test outputs.", Supported: true},
			{Name: "buildDependents", Category: "build", Description: "Build projects depending on this module.", Supported: true},
			{Name: "buildNeeded", Category: "build", Description: "Build dependent project requirements.", Supported: true},
			{Name: "lint", Category: "verification", Description: "Run lint.", Supported: false},
		}
		tasks = append(tasks, m.androidVariantTasks(false)...)
		return append(tasks, m.qualityTasks()...)
	case "jvm-library":
		tasks := []Task{
			{Name: "assemble", Category: "build", Description: "Compile the main JVM outputs.", Supported: true},
			{Name: "build", Category: "build", Description: "Compile the main JVM outputs and run supported tests.", Supported: true},
			{Name: "clean", Category: "build", Description: "Delete grit build outputs.", Supported: true},
			{Name: "compile", Category: "build", Description: "Compile JVM sources.", Supported: true},
			{Name: "test", Category: "verification", Description: "Run JVM unit tests.", Supported: true},
			{Name: "check", Category: "verification", Description: "Run supported checks.", Supported: true},
			{Name: "tasks", Category: "help", Description: "Display runnable tasks.", Supported: true},
			{Name: "artifactTransforms", Category: "help", Description: "Display artifact transform behavior.", Supported: true},
			{Name: "dependencies", Category: "help", Description: "Display module dependencies.", Supported: true},
			{Name: "projects", Category: "help", Description: "Display project modules.", Supported: true},
			{Name: "properties", Category: "help", Description: "Display module properties.", Supported: true},
			{Name: "buildEnvironment", Category: "help", Description: "Display environment and repository configuration.", Supported: true},
			{Name: "dependencyInsight", Category: "help", Description: "Display resolved dependency information.", Supported: true},
			{Name: "javaToolchains", Category: "help", Description: "Display Java and Kotlin toolchain information.", Supported: true},
			{Name: "kotlinDslAccessorsReport", Category: "help", Description: "Display detected Kotlin DSL accessors.", Supported: true},
			{Name: "outgoingVariants", Category: "help", Description: "Display published variants for this module.", Supported: true},
			{Name: "resolvableConfigurations", Category: "help", Description: "Display supported resolvable configurations.", Supported: true},
			{Name: "buildDependents", Category: "build", Description: "Build projects depending on this module.", Supported: true},
			{Name: "buildNeeded", Category: "build", Description: "Build dependent project requirements.", Supported: true},
		}
		return append(tasks, m.qualityTasks()...)
	default:
		return nil
	}
}

func (m Module) androidVariantTasks(includeInstall bool) []Task {
	variants := m.Variants()
	if len(variants) == 0 {
		return nil
	}
	out := make([]Task, 0, len(variants)*4)
	seen := map[string]struct{}{}
	for _, variant := range variants {
		name := strings.TrimSpace(variant.Name)
		if name == "" {
			continue
		}
		for _, task := range androidTasksForVariant(name, includeInstall) {
			if _, ok := seen[task.Name]; ok {
				continue
			}
			seen[task.Name] = struct{}{}
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func androidTasksForVariant(variantName string, includeInstall bool) []Task {
	suffix := taskNameSuffix(variantName)
	debugLike := androidVariantSupportsAndroidTestInstall(variantName)
	tasks := []Task{
		{Name: "assemble" + suffix, Category: "build", Description: "Assemble " + variantName + " outputs.", Supported: true},
		{Name: "compile" + suffix + "Sources", Category: "build", Description: "Compile " + variantName + " sources.", Supported: true},
		{Name: "compile" + suffix + "UnitTestSources", Category: "build", Description: "Compile " + variantName + " unit test sources.", Supported: debugLike},
		{Name: "compile" + suffix + "AndroidTestSources", Category: "build", Description: "Compile " + variantName + " androidTest sources.", Supported: debugLike},
		{Name: "assemble" + suffix + "AndroidTest", Category: "build", Description: "Assemble " + variantName + " androidTest outputs.", Supported: false},
		{Name: "test" + suffix + "UnitTest", Category: "verification", Description: "Run " + variantName + " unit tests.", Supported: debugLike},
	}
	if includeInstall {
		tasks = append(tasks, Task{Name: "install" + suffix, Category: "install", Description: "Install the " + variantName + " build.", Supported: true})
		if androidVariantSupportsAndroidTestInstall(variantName) {
			tasks = append(tasks,
				Task{Name: "install" + suffix + "AndroidTest", Category: "install", Description: "Install the " + variantName + " androidTest APK.", Supported: true},
				Task{Name: "uninstall" + suffix + "AndroidTest", Category: "install", Description: "Uninstall the " + variantName + " androidTest APK.", Supported: true},
			)
		}
	}
	return tasks
}

func androidVariantSupportsAndroidTestInstall(variantName string) bool {
	return strings.HasSuffix(strings.TrimSpace(variantName), "Debug") || strings.EqualFold(strings.TrimSpace(variantName), "debug")
}

func (m Module) defaultAndroidTaskVariant() string {
	if variants := m.Variants(); len(variants) > 0 {
		if name := strings.TrimSpace(variants[0].Name); name != "" {
			return name
		}
	}
	return m.DefaultVariantName()
}

func taskNameSuffix(variantName string) string {
	if variantName == "" {
		return ""
	}
	if len(variantName) == 1 {
		return strings.ToUpper(variantName)
	}
	return strings.ToUpper(variantName[:1]) + variantName[1:]
}

func (m Module) variantDisplayName(config BuildType, buildType string) string {
	parts := make([]string, 0, len(config.Flavors)+1)
	parts = append(parts, config.Flavors...)
	if strings.TrimSpace(buildType) != "" {
		parts = append(parts, buildType)
	} else if strings.TrimSpace(config.Name) != "" {
		parts = append(parts, config.Name)
	}
	if len(parts) == 0 {
		return "Main"
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, taskNameSuffix(part))
	}
	if len(out) == 0 {
		return "Main"
	}
	return strings.Join(out, " ")
}

func (m Module) variantCoordinateName(config BuildType, buildType string) string {
	if len(config.Flavors) > 0 {
		return variantNameFromFlavors(config.Flavors, buildType)
	}
	if strings.TrimSpace(buildType) != "" {
		return buildType
	}
	return firstNonEmpty(config.Name, m.DefaultVariantName())
}

func (m Module) variantResolvedName(config BuildType, buildType string) string {
	if declared := strings.TrimSpace(config.DeclaredName); declared != "" {
		return declared
	}
	if name := strings.TrimSpace(config.Name); name != "" {
		return name
	}
	return m.variantCoordinateName(config, buildType)
}

func (m Module) variantSourceSetOrder(config BuildType, buildType string) []string {
	if m.IsJVM() {
		return []string{"main"}
	}
	var out []string
	out = append(out, "main")
	out = append(out, config.Flavors...)
	if strings.TrimSpace(buildType) != "" {
		out = append(out, buildType)
	}
	if strings.TrimSpace(config.Name) != "" {
		out = append(out, config.Name)
	}
	return uniqueOrderedStrings(out)
}

func (m Module) variantTaskAliases(config BuildType, buildType string) []string {
	if m.IsJVM() {
		return []string{"build", "check", "compile", "test"}
	}
	suffix := taskNameSuffix(m.variantResolvedName(config, buildType))
	var out []string
	out = append(out, "assemble"+suffix)
	out = append(out, "compile"+suffix+"Sources")
	if m.Type == "android-application" {
		out = append(out, "install"+suffix)
	}
	if strings.EqualFold(strings.TrimSpace(buildType), "debug") {
		out = append(out,
			"assemble"+suffix+"AndroidTest",
			"compile"+suffix+"AndroidTestSources",
			"install"+suffix+"AndroidTest",
			"uninstall"+suffix+"AndroidTest",
			"compile"+suffix+"UnitTestSources",
			"test"+suffix+"UnitTest",
		)
	}
	return uniqueOrderedStrings(out)
}

func (m Module) variantModelSelectors(config BuildType, buildType string) []string {
	resolvedName := m.variantResolvedName(config, buildType)
	coordinateName := m.variantCoordinateName(config, buildType)
	out := []string{
		m.Path,
		resolvedName,
		m.Path + "#" + resolvedName,
	}
	if coordinateName != "" && coordinateName != resolvedName {
		out = append(out, coordinateName, "coordinate:"+coordinateName, m.Path+"#"+coordinateName)
	}
	if strings.TrimSpace(buildType) != "" {
		out = append(out, "buildType:"+buildType)
	}
	for _, flavor := range config.Flavors {
		flavor = strings.TrimSpace(flavor)
		if flavor == "" {
			continue
		}
		out = append(out, flavor, "flavor:"+flavor)
	}
	return uniqueOrderedStrings(out)
}

func (m Module) variantCompatibilitySourceSetNames(sourceSetOrder []string) []string {
	return uniqueOrderedStrings(append([]string(nil), sourceSetOrder...))
}

func (m Module) variantSyncFragments(config BuildType, buildType string, coordinateName string, sourceSetOrder []string) []string {
	resolvedName := m.variantResolvedName(config, buildType)
	out := []string{"module:" + m.Path, "variant:" + resolvedName}
	if coordinateName != "" && coordinateName != resolvedName {
		out = append(out, "coordinate:"+coordinateName)
	}
	if strings.TrimSpace(buildType) != "" {
		out = append(out, "buildType:"+buildType)
	}
	for _, flavor := range config.Flavors {
		flavor = strings.TrimSpace(flavor)
		if flavor == "" {
			continue
		}
		out = append(out, "flavor:"+flavor)
	}
	for _, sourceSet := range sourceSetOrder {
		sourceSet = strings.TrimSpace(sourceSet)
		if sourceSet == "" {
			continue
		}
		out = append(out, "sourceSet:"+sourceSet)
	}
	return uniqueOrderedStrings(out)
}

func uniqueOrderedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m Module) Variants() []BuildType {
	if m.IsJVM() {
		return []BuildType{cloneBuildType(m.ResolveVariant(m.DefaultVariantName()).Config)}
	}
	return cloneBuildTypes(m.allVariants())
}

func (m Module) Variant(name string) BuildType {
	return cloneBuildType(m.variantConfigForName(name))
}

func (m Module) IsAndroid() bool {
	return strings.HasPrefix(m.Type, "android-")
}

func (m Module) IsJVM() bool {
	return m.Type == "jvm-library"
}

func (m Module) DefaultVariantName() string {
	if m.IsJVM() {
		return "main"
	}
	variants := m.allVariants()
	if len(variants) > 0 {
		return variants[0].Name
	}
	return "debug"
}

func (m Module) variantConfigForName(name string) BuildType {
	if strings.TrimSpace(name) == "" {
		name = m.DefaultVariantName()
	}
	if buildType, ok := m.BuildTypes[name]; ok {
		if strings.TrimSpace(buildType.Name) == "" {
			buildType.Name = name
		}
		if strings.TrimSpace(buildType.BaseBuildType) != "" && strings.TrimSpace(buildType.DeclaredName) == "" {
			buildType.DeclaredName = buildType.Name
		}
		if strings.TrimSpace(buildType.BaseBuildType) == "" {
			buildType.BaseBuildType = name
		}
		baseName := buildType.Name
		if strings.TrimSpace(buildType.BaseBuildType) != "" {
			baseName = buildType.BaseBuildType
		}
		if buildType.SigningConfig == "" && baseName == "debug" {
			if _, ok := m.SigningConfigs["debug"]; ok {
				buildType.SigningConfig = "debug"
			}
		}
		buildType.Optimization.MinifyEnabled = buildType.IsMinifyEnabled
		buildType.Optimization.ShrinkResources = buildType.IsShrinkResources
		return cloneBuildType(buildType)
	}
	for _, variant := range m.allVariants() {
		if variant.Name == name {
			return cloneBuildType(variant)
		}
	}
	buildType := m.variantConfigForBaseBuildType(name)
	if buildType.Name == "" {
		buildType.Name = name
		buildType.BaseBuildType = name
		buildType.Optimization = VariantOptimization{
			MinifyEnabled:   false,
			ShrinkResources: false,
		}
	}
	return cloneBuildType(buildType)
}

func (m Module) allVariants() []BuildType {
	baseBuildTypes := m.baseBuildTypes()
	if len(baseBuildTypes) == 0 {
		return nil
	}
	combinations := m.flavorCombinations()
	if len(combinations) == 0 {
		names := make([]string, 0, len(baseBuildTypes))
		for name := range baseBuildTypes {
			names = append(names, name)
		}
		sort.Strings(names)
		out := make([]BuildType, 0, len(names))
		for _, name := range names {
			variant := m.variantConfigForBaseBuildType(name)
			if m.variantEnabled(variant.Name) {
				out = append(out, variant)
			}
		}
		return out
	}
	buildTypeNames := make([]string, 0, len(baseBuildTypes))
	for name := range baseBuildTypes {
		buildTypeNames = append(buildTypeNames, name)
	}
	sort.Strings(buildTypeNames)
	out := make([]BuildType, 0, len(buildTypeNames)*len(combinations))
	for _, combo := range combinations {
		for _, buildTypeName := range buildTypeNames {
			variant := m.mergeVariant(buildTypeName, combo)
			if m.variantEnabled(variant.Name) {
				out = append(out, variant)
			}
		}
	}
	return out
}

func (m Module) variantEnabled(name string) bool {
	if len(m.EnabledVariants) == 0 {
		return true
	}
	for _, enabled := range m.EnabledVariants {
		if strings.TrimSpace(enabled) == strings.TrimSpace(name) {
			return true
		}
	}
	return false
}

func (m Module) baseBuildTypes() map[string]BuildType {
	if m.IsAndroid() {
		out := map[string]BuildType{
			"debug":   {Name: "debug"},
			"release": {Name: "release"},
		}
		for name, buildType := range m.BuildTypes {
			if strings.TrimSpace(buildType.BaseBuildType) != "" {
				continue
			}
			buildType.Name = name
			out[name] = buildType
		}
		return out
	}
	if len(m.BuildTypes) == 0 {
		return map[string]BuildType{"debug": {Name: "debug"}}
	}
	out := make(map[string]BuildType, len(m.BuildTypes))
	for name, buildType := range m.BuildTypes {
		if strings.TrimSpace(buildType.BaseBuildType) != "" {
			continue
		}
		buildType.Name = name
		out[name] = buildType
	}
	if len(out) == 0 {
		out["debug"] = BuildType{Name: "debug"}
	}
	return out
}

func (m Module) variantConfigForBaseBuildType(name string) BuildType {
	buildType, ok := m.baseBuildTypes()[name]
	if !ok {
		buildType = BuildType{Name: name}
	}
	buildType.Name = name
	buildType.BaseBuildType = name
	if buildType.SigningConfig == "" && name == "debug" {
		if _, ok := m.SigningConfigs["debug"]; ok {
			buildType.SigningConfig = "debug"
		}
	}
	buildType.Optimization.MinifyEnabled = buildType.IsMinifyEnabled
	buildType.Optimization.ShrinkResources = buildType.IsShrinkResources
	return cloneBuildType(buildType)
}

func (m Module) mergeVariant(buildTypeName string, flavors []string) BuildType {
	cfg := BuildType{
		Name:          variantNameFromFlavors(flavors, buildTypeName),
		BaseBuildType: buildTypeName,
		Flavors:       append([]string(nil), flavors...),
	}

	for i := len(flavors) - 1; i >= 0; i-- {
		flavor, ok := m.ProductFlavors[flavors[i]]
		if !ok {
			continue
		}
		cfg = applyFlavorOverrides(cfg, flavor)
	}
	cfg = applyBuildTypeOverrides(cfg, m.variantConfigForBaseBuildType(buildTypeName))
	if custom, ok := m.customVariantOverride(buildTypeName, flavors); ok {
		cfg = applyBuildTypeOverrides(cfg, custom)
		cfg.DeclaredName = firstNonEmpty(custom.DeclaredName, custom.Name)
	}
	cfg.Name = firstNonEmpty(cfg.DeclaredName, variantNameFromFlavors(flavors, buildTypeName))
	cfg.BaseBuildType = buildTypeName
	cfg.Flavors = append([]string(nil), flavors...)
	return cloneBuildType(cfg)
}

func (m Module) customVariantOverride(buildTypeName string, flavors []string) (BuildType, bool) {
	for name, buildType := range m.BuildTypes {
		if strings.TrimSpace(buildType.BaseBuildType) != strings.TrimSpace(buildTypeName) {
			continue
		}
		if !sameOrderedStrings(buildType.Flavors, flavors) {
			continue
		}
		if strings.TrimSpace(buildType.Name) == "" {
			buildType.Name = name
		}
		return cloneBuildType(buildType), true
	}
	return BuildType{}, false
}

func cloneBuildTypes(values []BuildType) []BuildType {
	if len(values) == 0 {
		return nil
	}
	out := make([]BuildType, 0, len(values))
	for _, value := range values {
		out = append(out, cloneBuildType(value))
	}
	return out
}

func cloneBuildType(buildType BuildType) BuildType {
	buildType.Flavors = append([]string(nil), buildType.Flavors...)
	buildType.MatchingFallbacks = append([]string(nil), buildType.MatchingFallbacks...)
	buildType.Optimization = cloneVariantOptimization(buildType.Optimization)
	buildType.ProguardFiles = append([]string(nil), buildType.ProguardFiles...)
	return buildType
}

func cloneVariantOptimization(optimization VariantOptimization) VariantOptimization {
	if len(optimization.PackageOptimizations) == 0 {
		return optimization
	}
	items := optimization.PackageOptimizations
	optimization.PackageOptimizations = make([]PackageOptimization, 0, len(items))
	for _, item := range items {
		if item.MinifyEnabled != nil {
			value := *item.MinifyEnabled
			item.MinifyEnabled = &value
		}
		if item.ShrinkResources != nil {
			value := *item.ShrinkResources
			item.ShrinkResources = &value
		}
		optimization.PackageOptimizations = append(optimization.PackageOptimizations, item)
	}
	return optimization
}

func sameOrderedStrings(a, b []string) bool {
	a = uniqueOrderedStrings(a)
	b = uniqueOrderedStrings(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m Module) buildTypeFallbacks(name string) []string {
	buildType := m.variantConfigForBaseBuildType(name)
	out := []string{name}
	for _, fallback := range buildType.MatchingFallbacks {
		if !containsString(out, fallback) {
			out = append(out, fallback)
		}
	}
	return out
}

func (m Module) resolveDependencyVariant(requested ResolvedVariant) string {
	if requested.ModuleType == "" {
		requested.ModuleType = m.Type
	}
	if !m.IsAndroid() {
		if m.IsJVM() {
			return "main"
		}
		return m.DefaultVariantName()
	}
	targetVariants := m.Variants()
	if len(targetVariants) == 0 {
		return m.DefaultVariantName()
	}
	buildTypeCandidates := m.buildTypeFallbacks(firstNonEmpty(requested.Coordinate.BuildType, requested.Name))
	flavorChoices := m.resolveFlavorChoices(requested)
	for _, buildTypeName := range buildTypeCandidates {
		if candidate, ok := m.findVariantForFlavorChoices(buildTypeName, flavorChoices); ok {
			return candidate.Name
		}
	}
	return semanticResolvedVariantName(m, requested.Name)
}

func (m Module) flavorCombinations() [][]string {
	if len(m.ProductFlavors) == 0 {
		return nil
	}
	dimensions := append([]string(nil), m.FlavorDimensions...)
	if len(dimensions) == 0 {
		dimensionSet := map[string]struct{}{}
		for _, flavor := range m.ProductFlavors {
			dimension := strings.TrimSpace(flavor.Dimension)
			if dimension == "" {
				dimension = "default"
			}
			if _, ok := dimensionSet[dimension]; !ok {
				dimensions = append(dimensions, dimension)
				dimensionSet[dimension] = struct{}{}
			}
		}
		sort.Strings(dimensions)
	}
	flavorsByDimension := map[string][]string{}
	for name, flavor := range m.ProductFlavors {
		dimension := strings.TrimSpace(flavor.Dimension)
		if dimension == "" {
			dimension = "default"
		}
		flavorsByDimension[dimension] = append(flavorsByDimension[dimension], firstNonEmpty(flavor.Name, name))
	}
	for dimension := range flavorsByDimension {
		sort.Strings(flavorsByDimension[dimension])
	}
	out := [][]string{{}}
	for _, dimension := range dimensions {
		flavors := flavorsByDimension[dimension]
		if len(flavors) == 0 {
			continue
		}
		var next [][]string
		for _, prefix := range out {
			for _, flavor := range flavors {
				combo := append(append([]string(nil), prefix...), flavor)
				next = append(next, combo)
			}
		}
		out = next
	}
	if len(out) == 1 && len(out[0]) == 0 {
		return nil
	}
	return out
}

func variantNameFromFlavors(flavors []string, buildType string) string {
	if len(flavors) == 0 {
		return buildType
	}
	var b strings.Builder
	for i, flavor := range flavors {
		if i == 0 {
			b.WriteString(flavor)
			continue
		}
		if flavor == "" {
			continue
		}
		b.WriteString(strings.ToUpper(flavor[:1]))
		if len(flavor) > 1 {
			b.WriteString(flavor[1:])
		}
	}
	if buildType == "" {
		return b.String()
	}
	b.WriteString(strings.ToUpper(buildType[:1]))
	if len(buildType) > 1 {
		b.WriteString(buildType[1:])
	}
	return b.String()
}

func resolvedApplicationID(base string, cfg BuildType) string {
	appID := firstNonEmpty(cfg.ApplicationID, base)
	if appID == "" {
		appID = cfg.ApplicationIDSuffix
	}
	if appID == "" {
		return ""
	}
	if cfg.ApplicationIDSuffix != "" && !strings.HasSuffix(appID, cfg.ApplicationIDSuffix) {
		appID += cfg.ApplicationIDSuffix
	}
	return appID
}

func resolvedVersionName(base string, cfg BuildType) string {
	versionName := firstNonEmpty(cfg.VersionName, base)
	if versionName == "" {
		versionName = cfg.VersionNameSuffix
	}
	if versionName == "" {
		return ""
	}
	if cfg.VersionNameSuffix != "" && !strings.HasSuffix(versionName, cfg.VersionNameSuffix) {
		versionName += cfg.VersionNameSuffix
	}
	return versionName
}

func mergedMissingDimensions(m Module, selectedFlavors []string) map[string][]string {
	out := cloneStrategyMap(m.DefaultConfig.MissingDimensions)
	for i := len(selectedFlavors) - 1; i >= 0; i-- {
		flavor, ok := m.ProductFlavors[selectedFlavors[i]]
		if !ok {
			continue
		}
		for dimension, values := range flavor.MissingDimensions {
			out[dimension] = append([]string(nil), values...)
		}
	}
	return out
}

func cloneStrategyMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func applyFlavorOverrides(cfg BuildType, flavor ProductFlavor) BuildType {
	if flavor.ApplicationID != "" {
		cfg.ApplicationID = flavor.ApplicationID
	}
	if flavor.ApplicationIDSuffix != "" {
		cfg.ApplicationIDSuffix += flavor.ApplicationIDSuffix
	}
	if flavor.VersionCode != "" {
		cfg.VersionCode = flavor.VersionCode
	}
	if flavor.VersionName != "" {
		cfg.VersionName = flavor.VersionName
	}
	if flavor.VersionNameSuffix != "" {
		cfg.VersionNameSuffix += flavor.VersionNameSuffix
	}
	if flavor.MinSDK != "" {
		cfg.MinSDK = flavor.MinSDK
	}
	if flavor.TargetSDK != "" {
		cfg.TargetSDK = flavor.TargetSDK
	}
	if len(flavor.MatchingFallbacks) > 0 {
		cfg.MatchingFallbacks = append([]string(nil), flavor.MatchingFallbacks...)
	}
	return cfg
}

func applyBuildTypeOverrides(cfg BuildType, buildType BuildType) BuildType {
	if buildType.ApplicationIDSuffix != "" {
		cfg.ApplicationIDSuffix += buildType.ApplicationIDSuffix
	}
	if buildType.VersionNameSuffix != "" {
		cfg.VersionNameSuffix += buildType.VersionNameSuffix
	}
	if len(buildType.MatchingFallbacks) > 0 {
		cfg.MatchingFallbacks = append([]string(nil), buildType.MatchingFallbacks...)
	}
	cfg.SigningConfig = firstNonEmpty(buildType.SigningConfig, cfg.SigningConfig)
	cfg.Optimization = buildType.Optimization
	cfg.IsMinifyEnabled = buildType.IsMinifyEnabled
	cfg.IsShrinkResources = buildType.IsShrinkResources
	if len(buildType.ProguardFiles) > 0 {
		cfg.ProguardFiles = append([]string(nil), buildType.ProguardFiles...)
	}
	return cfg
}

func (m Module) resolveFlavorChoices(requested ResolvedVariant) map[string][]string {
	choices := map[string][]string{}
	selectedFlavorSet := map[string]struct{}{}
	for _, flavorName := range requested.Coordinate.Flavors {
		selectedFlavorSet[flavorName] = struct{}{}
		flavor, ok := m.ProductFlavors[flavorName]
		if !ok {
			continue
		}
		dimension := firstNonEmpty(flavor.Dimension, "default")
		candidates := []string{flavorName}
		for _, fallback := range flavor.MatchingFallbacks {
			if !containsString(candidates, fallback) {
				candidates = append(candidates, fallback)
			}
		}
		choices[dimension] = candidates
	}
	for dimension, fallbacks := range requested.MissingDimensions {
		if len(fallbacks) == 0 {
			continue
		}
		if _, ok := choices[dimension]; ok {
			continue
		}
		choices[dimension] = append([]string(nil), fallbacks...)
	}
	for name, flavor := range m.ProductFlavors {
		dimension := firstNonEmpty(flavor.Dimension, "default")
		if _, ok := choices[dimension]; ok {
			continue
		}
		if _, selected := selectedFlavorSet[name]; selected {
			choices[dimension] = []string{name}
		}
	}
	return choices
}

func (m Module) findVariantForFlavorChoices(buildType string, choices map[string][]string) (BuildType, bool) {
	for _, variant := range m.Variants() {
		if firstNonEmpty(variant.BaseBuildType, variant.Name) != buildType {
			continue
		}
		if !m.variantMatchesFlavorChoices(variant, choices) {
			continue
		}
		return variant, true
	}
	return BuildType{}, false
}

func (m Module) variantMatchesFlavorChoices(variant BuildType, choices map[string][]string) bool {
	flavorsByDimension := map[string]string{}
	for _, flavorName := range variant.Flavors {
		flavor, ok := m.ProductFlavors[flavorName]
		if !ok {
			continue
		}
		dimension := firstNonEmpty(flavor.Dimension, "default")
		flavorsByDimension[dimension] = flavorName
	}
	for dimension, targetFlavor := range flavorsByDimension {
		if candidates, ok := choices[dimension]; ok && len(candidates) > 0 {
			if !containsString(candidates, targetFlavor) {
				return false
			}
		}
	}
	for dimension, candidates := range choices {
		if len(candidates) == 0 {
			continue
		}
		if targetFlavor, ok := flavorsByDimension[dimension]; ok {
			if !containsString(candidates, targetFlavor) {
				return false
			}
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func RequireModule(prj *Project, path string) error {
	if prj.FindModule(path) != nil {
		return nil
	}
	return fmt.Errorf("module %s not found", path)
}

func (prj *Project) FindModule(path string) *Module {
	for i := range prj.Modules {
		if prj.Modules[i].Path == path {
			return &prj.Modules[i]
		}
	}
	return nil
}
