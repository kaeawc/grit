package graph

import "strings"

type LogicalModuleID string
type VariantID string
type MaterializationID string
type ArtifactID string
type ActionID string
type EdgeID string

type NodeKind string

const (
	NodeKindLogicalModule   NodeKind = "logical_module"
	NodeKindVariant         NodeKind = "variant"
	NodeKindMaterialization NodeKind = "materialization"
	NodeKindArtifact        NodeKind = "artifact"
	NodeKindAction          NodeKind = "action"
)

type NodeRef struct {
	Kind NodeKind `json:"kind"`
	ID   string   `json:"id"`
}

func (id LogicalModuleID) String() string   { return string(id) }
func (id VariantID) String() string         { return string(id) }
func (id MaterializationID) String() string { return string(id) }
func (id ArtifactID) String() string        { return string(id) }
func (id ActionID) String() string          { return string(id) }
func (id EdgeID) String() string            { return string(id) }

func (id LogicalModuleID) Ref() NodeRef {
	return NodeRef{Kind: NodeKindLogicalModule, ID: id.String()}
}

func (id VariantID) Ref() NodeRef {
	return NodeRef{Kind: NodeKindVariant, ID: id.String()}
}

func (id MaterializationID) Ref() NodeRef {
	return NodeRef{Kind: NodeKindMaterialization, ID: id.String()}
}

func (id ArtifactID) Ref() NodeRef {
	return NodeRef{Kind: NodeKindArtifact, ID: id.String()}
}

func (id ActionID) Ref() NodeRef {
	return NodeRef{Kind: NodeKindAction, ID: id.String()}
}

func (ref NodeRef) Valid() bool {
	return strings.TrimSpace(string(ref.Kind)) != "" && strings.TrimSpace(ref.ID) != ""
}

func (ref NodeRef) String() string {
	if ref.Kind == "" {
		return ref.ID
	}
	if ref.ID == "" {
		return string(ref.Kind)
	}
	return string(ref.Kind) + ":" + ref.ID
}
