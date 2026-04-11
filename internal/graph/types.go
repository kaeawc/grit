package graph

type GraphNode interface {
	Ref() NodeRef
}

type VariantCoordinate struct {
	ModuleID LogicalModuleID `json:"moduleId"`
	Name     string          `json:"name"`
}

type ResolvedVariant struct {
	Coordinate VariantCoordinate `json:"coordinate"`
	Variant    Variant           `json:"variant"`
}

type ModuleKind string

const (
	ModuleKindUnknown            ModuleKind = "unknown"
	ModuleKindAndroidApplication ModuleKind = "android_application"
	ModuleKindAndroidLibrary     ModuleKind = "android_library"
	ModuleKindJvmLibrary         ModuleKind = "jvm_library"
)

type MaterializationKind string

const (
	MaterializationKindSourceBacked   MaterializationKind = "source_backed"
	MaterializationKindArtifactBacked MaterializationKind = "artifact_backed"
)

type ArtifactKind string

const (
	ArtifactKindUnknown    ArtifactKind = "unknown"
	ArtifactKindDirectory  ArtifactKind = "directory"
	ArtifactKindJar        ArtifactKind = "jar"
	ArtifactKindAar        ArtifactKind = "aar"
	ArtifactKindDex        ArtifactKind = "dex"
	ArtifactKindApk        ArtifactKind = "apk"
	ArtifactKindManifest   ArtifactKind = "manifest"
	ArtifactKindResources  ArtifactKind = "resources"
	ArtifactKindClasspath  ArtifactKind = "classpath"
	ArtifactKindProvenance ArtifactKind = "provenance"
	ArtifactKindOther      ArtifactKind = "other"
)

type ActionKind string

const (
	ActionKindUnknown   ActionKind = "unknown"
	ActionKindResolve   ActionKind = "resolve"
	ActionKindCompile   ActionKind = "compile"
	ActionKindLink      ActionKind = "link"
	ActionKindDex       ActionKind = "dex"
	ActionKindPackage   ActionKind = "package"
	ActionKindSign      ActionKind = "sign"
	ActionKindTest      ActionKind = "test"
	ActionKindTransform ActionKind = "transform"
	ActionKindCustom    ActionKind = "custom"
)

type EdgeKind string

const (
	EdgeKindDependsOn EdgeKind = "depends_on"
	EdgeKindContains  EdgeKind = "contains"
	EdgeKindRealizes  EdgeKind = "realizes"
	EdgeKindProduces  EdgeKind = "produces"
	EdgeKindConsumes  EdgeKind = "consumes"
	EdgeKindBacks     EdgeKind = "backs"
	EdgeKindRelatesTo EdgeKind = "relates_to"
)

type LogicalModule struct {
	ID         LogicalModuleID   `json:"id"`
	Name       string            `json:"name,omitempty"`
	Path       string            `json:"path,omitempty"`
	Dir        string            `json:"dir,omitempty"`
	Kind       ModuleKind        `json:"kind"`
	Note       string            `json:"note,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func (m LogicalModule) Ref() NodeRef { return m.ID.Ref() }

type Variant struct {
	ID         VariantID         `json:"id"`
	ModuleID   LogicalModuleID   `json:"moduleId"`
	Name       string            `json:"name,omitempty"`
	BuildType  string            `json:"buildType,omitempty"`
	Flavors    []string          `json:"flavors,omitempty"`
	Note       string            `json:"note,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func (v Variant) Ref() NodeRef { return v.ID.Ref() }

type Materialization struct {
	ID                   MaterializationID   `json:"id"`
	ModuleID             LogicalModuleID     `json:"moduleId"`
	VariantID            VariantID           `json:"variantId"`
	Kind                 MaterializationKind `json:"kind"`
	BackingArtifactID    ArtifactID          `json:"backingArtifactId,omitempty"`
	SourceRoots          []string            `json:"sourceRoots,omitempty"`
	ArtifactSnapshotID   string              `json:"artifactSnapshotId,omitempty"`
	ClasspathSnapshotIDs []string            `json:"classpathSnapshotIds,omitempty"`
	Note                 string              `json:"note,omitempty"`
	Attributes           map[string]string   `json:"attributes,omitempty"`
}

func (m Materialization) Ref() NodeRef { return m.ID.Ref() }

type Artifact struct {
	ID                 ArtifactID        `json:"id"`
	MaterializationID  MaterializationID `json:"materializationId,omitempty"`
	ProducedByActionID ActionID          `json:"producedByActionId,omitempty"`
	Kind               ArtifactKind      `json:"kind"`
	Path               string            `json:"path,omitempty"`
	Digest             string            `json:"digest,omitempty"`
	Note               string            `json:"note,omitempty"`
	Attributes         map[string]string `json:"attributes,omitempty"`
}

func (a Artifact) Ref() NodeRef { return a.ID.Ref() }

type Action struct {
	ID         ActionID          `json:"id"`
	ModuleID   LogicalModuleID   `json:"moduleId,omitempty"`
	VariantID  VariantID         `json:"variantId,omitempty"`
	Name       string            `json:"name,omitempty"`
	Kind       ActionKind        `json:"kind"`
	Inputs     []ArtifactID      `json:"inputs,omitempty"`
	Outputs    []ArtifactID      `json:"outputs,omitempty"`
	Note       string            `json:"note,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func (a Action) Ref() NodeRef { return a.ID.Ref() }

type Edge struct {
	ID         EdgeID            `json:"id"`
	From       NodeRef           `json:"from"`
	To         NodeRef           `json:"to"`
	Kind       EdgeKind          `json:"kind"`
	Note       string            `json:"note,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}
