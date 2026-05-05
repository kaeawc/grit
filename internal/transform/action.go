// Package transform defines the canonical shape of Layer 4 transform actions
// in grit's content-addressable build model.
//
// A transform action is a deterministic function from a set of input CAS
// blobs plus tool and environment metadata to a set of output CAS blobs.
// The central output of this package is the action hash: a SHA-256 digest
// over the canonical encoding of an Action, used as the cache key for the
// action's outputs. See roadmap/planning/dependency-cache-architecture.md
// for the architectural contract.
package transform

import (
	"encoding/json"
	"sort"

	"github.com/kaeawc/grit/internal/cas"
)

// Action is the declared shape of a Layer 4 transform.
//
// Actions are value types. Two Actions that canonicalize to equal fields
// must produce the same Hash. The canonical encoding is stable and must
// not change silently: changes invalidate every cached action output.
type Action struct {
	// Kind identifies the transform family (e.g. "aar-extract", "dex-external-library").
	Kind string
	// Tool is a stable identifier for the tool implementation.
	Tool string
	// ToolVersion is the version of the tool binary or package.
	ToolVersion string
	// Args is the ordered argument list passed to the tool. Order is
	// preserved in the canonical encoding because argument order is
	// semantically meaningful for most tools.
	Args []string
	// Env is the declared environment contract for the tool. It holds only
	// the environment variables the action depends on, not the full process
	// environment. Keys are sorted during canonicalization.
	Env map[string]string
	// Inputs are the labelled input blobs whose order is not part of identity.
	// They are sorted by (role, hash) during canonicalization.
	Inputs []Input
	// OrderedInputs are labelled input blobs whose caller-provided order is
	// semantically meaningful, such as classpath entries for tools where
	// earlier jars shadow later jars. OrderedInputs are preserved in canonical
	// encoding and should be used instead of per-slot role names.
	OrderedInputs []Input
	// Outputs declares the expected output roles. Output blob hashes are
	// discovered by executing the action and are therefore not hashed into
	// the action key, but the declared output shape is hashed so that two
	// actions with identical inputs but different declared output roles do
	// not collide.
	Outputs []OutputDecl
}

// Input is one labelled input blob to a transform action.
type Input struct {
	Role string   `json:"role"`
	Hash cas.Hash `json:"hash"`
}

// OutputDecl declares one expected output role for a transform action.
type OutputDecl struct {
	Role string `json:"role"`
	Kind string `json:"kind,omitempty"`
}

// Hash computes the action hash for a. The canonical encoding sorts unordered
// inputs by role then hash, preserves OrderedInputs and argument order, sorts
// outputs by role then kind, and sorts environment variables by key.
func (a Action) Hash() cas.Hash {
	return cas.HashBytes(a.canonicalBytes())
}

// canonicalBytes returns the stable canonical encoding of a. It is
// unexported so higher layers treat Hash() as the only identity contract.
func (a Action) canonicalBytes() []byte {
	inputs := append([]Input(nil), a.Inputs...)
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Role != inputs[j].Role {
			return inputs[i].Role < inputs[j].Role
		}
		return inputs[i].Hash.String() < inputs[j].Hash.String()
	})
	outputs := append([]OutputDecl(nil), a.Outputs...)
	sort.Slice(outputs, func(i, j int) bool {
		if outputs[i].Role != outputs[j].Role {
			return outputs[i].Role < outputs[j].Role
		}
		return outputs[i].Kind < outputs[j].Kind
	})
	c := canonicalAction{
		Version:       canonicalVersion,
		Kind:          a.Kind,
		Tool:          a.Tool,
		ToolVersion:   a.ToolVersion,
		Args:          append([]string(nil), a.Args...),
		Env:           a.Env,
		Inputs:        inputs,
		OrderedInputs: append([]Input(nil), a.OrderedInputs...),
		Outputs:       outputs,
	}
	// encoding/json sorts map keys alphabetically, and slice order is
	// preserved, so c marshals deterministically.
	data, err := json.Marshal(c)
	if err != nil {
		// Action fields are simple and always marshalable. A failure here
		// is a programmer error, not a runtime condition.
		panic("transform: canonical action failed to marshal: " + err.Error())
	}
	return data
}

// canonicalVersion namespaces the canonical encoding. Bumping it is how we
// deliberately invalidate every cached action output across the fleet.
const canonicalVersion = 2

type canonicalAction struct {
	Version       int               `json:"version"`
	Kind          string            `json:"kind"`
	Tool          string            `json:"tool"`
	ToolVersion   string            `json:"toolVersion"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Inputs        []Input           `json:"inputs,omitempty"`
	OrderedInputs []Input           `json:"orderedInputs,omitempty"`
	Outputs       []OutputDecl      `json:"outputs,omitempty"`
}
