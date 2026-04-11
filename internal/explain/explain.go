package explain

import (
	"strings"

	"github.com/kaeawc/grit/internal/graph"
)

type State string

const (
	StateUnknown State = "unknown"
	StateReused  State = "reused"
	StateRebuilt State = "rebuilt"
)

type Timing struct {
	State  State  `json:"state"`
	Basis  string `json:"basis,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func InferTiming(actionName string, durationMs int64, err error) *Timing {
	if !isCacheableAction(actionName) {
		return nil
	}
	if err != nil {
		return &Timing{
			State:  StateUnknown,
			Basis:  "error",
			Detail: "action failed before cache status could be determined",
		}
	}
	if durationMs == 0 {
		return &Timing{
			State:  StateReused,
			Basis:  "perf-duration",
			Detail: "tracked action completed without observable work",
		}
	}
	return &Timing{
		State:  StateRebuilt,
		Basis:  "perf-duration",
		Detail: "tracked action recorded work",
	}
}

func isCacheableAction(actionName string) bool {
	switch actionName {
	case "aapt2Link",
		"addDexToAPK",
		"compileAndroidResources",
		"compileExternalAndroidLibraries",
		"compileKotlin",
		"compileTests",
		"copyUnsignedAPK",
		"jarClasses",
		"runD8",
		"runJavac",
		"runR8",
		"signAPK":
		return true
	default:
		return strings.HasPrefix(actionName, "compile")
	}
}

type Action struct {
	ActionID                    string           `json:"actionId"`
	Name                        string           `json:"name,omitempty"`
	Operation                   string           `json:"operation,omitempty"`
	VariantID                   string           `json:"variantId,omitempty"`
	ModuleID                    string           `json:"moduleId,omitempty"`
	InputArtifacts              []Artifact       `json:"inputArtifacts,omitempty"`
	VariantDependencies         []NodeDependency `json:"variantDependencies,omitempty"`
	MaterializationDependencies []NodeDependency `json:"materializationDependencies,omitempty"`
	Cache                       *Timing          `json:"cache,omitempty"`
}

type Artifact struct {
	ID                string `json:"id"`
	Kind              string `json:"kind,omitempty"`
	MaterializationID string `json:"materializationId,omitempty"`
	Path              string `json:"path,omitempty"`
	Digest            string `json:"digest,omitempty"`
}

type NodeDependency struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Path    string `json:"path,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Variant string `json:"variant,omitempty"`
}

func ForGraphAction(g *graph.Graph, action graph.Action) Action {
	out := Action{
		ActionID:  action.ID.String(),
		Name:      action.Name,
		Operation: action.Attributes["operation"],
		VariantID: action.VariantID.String(),
		ModuleID:  action.ModuleID.String(),
	}
	for _, artifact := range g.ActionInputs(action.ID) {
		out.InputArtifacts = append(out.InputArtifacts, Artifact{
			ID:                artifact.ID.String(),
			Kind:              string(artifact.Kind),
			MaterializationID: artifact.MaterializationID.String(),
			Path:              artifact.Path,
			Digest:            artifact.Digest,
		})
		if artifact.MaterializationID == "" {
			continue
		}
		materialization, ok := g.Materialization(artifact.MaterializationID)
		if !ok {
			continue
		}
		if variant, ok := g.Variant(materialization.VariantID); ok {
			out.VariantDependencies = appendIfMissingDependency(out.VariantDependencies, NodeDependency{
				ID:      variant.ID.String(),
				Name:    variant.Name,
				Kind:    "variant",
				Variant: variant.Name,
			})
		}
		out.MaterializationDependencies = appendIfMissingDependency(out.MaterializationDependencies, NodeDependency{
			ID:      materialization.ID.String(),
			Kind:    "materialization",
			Variant: materialization.VariantID.String(),
		})
	}
	return out
}

func appendIfMissingDependency(in []NodeDependency, dep NodeDependency) []NodeDependency {
	for _, existing := range in {
		if existing.ID == dep.ID {
			return in
		}
	}
	return append(in, dep)
}
