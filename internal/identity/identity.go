package identity

import "strings"

type ModuleID string
type VariantID string
type ArtifactID string
type ActionID string
type ClasspathSnapshotID string
type MaterializationID string

type MaterializationMode string

const (
	MaterializationSource   MaterializationMode = "source"
	MaterializationBinary   MaterializationMode = "binary"
	MaterializationArtifact MaterializationMode = "artifact"
)

func (id ModuleID) String() string            { return string(id) }
func (id VariantID) String() string           { return string(id) }
func (id ArtifactID) String() string          { return string(id) }
func (id ActionID) String() string            { return string(id) }
func (id ClasspathSnapshotID) String() string { return string(id) }
func (id MaterializationID) String() string   { return string(id) }

func NewModuleID(logicalName string) ModuleID {
	return ModuleID(Key("module", NormalizeLogicalParts(logicalName)...))
}

func NewVariantID(module ModuleID, coordinates ...string) VariantID {
	parts := append([]string{module.String()}, NormalizeLogicalParts(coordinates...)...)
	return VariantID(Key("variant", parts...))
}

func NewArtifactID(module ModuleID, variant VariantID, kind string, attributes ...string) ArtifactID {
	parts := []string{module.String(), variant.String()}
	parts = append(parts, NormalizeLogicalParts(kind)...)
	parts = append(parts, NormalizeLogicalParts(attributes...)...)
	return ArtifactID(Key("artifact", parts...))
}

func NewActionID(module ModuleID, variant VariantID, action string, inputs ...string) ActionID {
	parts := []string{module.String(), variant.String()}
	parts = append(parts, NormalizeLogicalParts(action)...)
	parts = append(parts, NormalizeLogicalParts(inputs...)...)
	return ActionID(Key("action", parts...))
}

func NewClasspathSnapshotID(entries ...string) ClasspathSnapshotID {
	return ClasspathSnapshotID(SetKey("classpath-snapshot", NormalizePathParts(entries...)...))
}

func NewClasspathSnapshotFromOrderedEntries(entries ...string) ClasspathSnapshotID {
	return ClasspathSnapshotID(Key("classpath-snapshot", NormalizePathParts(entries...)...))
}

func NewMaterializationID(module ModuleID, variant VariantID, mode MaterializationMode, source string) MaterializationID {
	parts := []string{module.String(), variant.String(), string(mode)}
	parts = append(parts, NormalizeLogicalParts(source)...)
	return MaterializationID(Key("materialization", parts...))
}

func NormalizeLogicalPart(value string) string {
	return strings.TrimSpace(value)
}

func NormalizeLogicalParts(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := NormalizeLogicalPart(value)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}
