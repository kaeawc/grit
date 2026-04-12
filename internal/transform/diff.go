package transform

import (
	"fmt"
	"sort"
	"strings"
)

// FieldDelta describes a single field that differs between two Actions.
type FieldDelta struct {
	FieldName string
	OldValue  string
	NewValue  string
}

// DiffActionHash compares two Actions field by field (using the same
// canonical ordering that Hash uses) and returns one FieldDelta per
// divergent field. If the actions are identical the returned slice is nil.
func DiffActionHash(old, new Action) []FieldDelta {
	var deltas []FieldDelta

	if old.Kind != new.Kind {
		deltas = append(deltas, FieldDelta{"Kind", old.Kind, new.Kind})
	}
	if old.Tool != new.Tool {
		deltas = append(deltas, FieldDelta{"Tool", old.Tool, new.Tool})
	}
	if old.ToolVersion != new.ToolVersion {
		deltas = append(deltas, FieldDelta{"ToolVersion", old.ToolVersion, new.ToolVersion})
	}

	oldArgs := formatArgs(old.Args)
	newArgs := formatArgs(new.Args)
	if oldArgs != newArgs {
		deltas = append(deltas, FieldDelta{"Args", oldArgs, newArgs})
	}

	oldEnv := formatEnv(old.Env)
	newEnv := formatEnv(new.Env)
	if oldEnv != newEnv {
		deltas = append(deltas, FieldDelta{"Env", oldEnv, newEnv})
	}

	oldInputs := formatInputs(old.Inputs)
	newInputs := formatInputs(new.Inputs)
	if oldInputs != newInputs {
		deltas = append(deltas, FieldDelta{"Inputs", oldInputs, newInputs})
	}

	oldOutputs := formatOutputs(old.Outputs)
	newOutputs := formatOutputs(new.Outputs)
	if oldOutputs != newOutputs {
		deltas = append(deltas, FieldDelta{"Outputs", oldOutputs, newOutputs})
	}

	return deltas
}

// formatArgs returns a deterministic string representation of the Args slice.
func formatArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return strings.Join(args, ",")
}

// formatEnv returns a deterministic string representation of the Env map,
// with keys sorted alphabetically (matching canonical encoding).
func formatEnv(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%s", k, env[k])
	}
	return strings.Join(parts, ",")
}

// formatInputs returns a deterministic string representation of the Inputs
// slice, sorted by (role, hash) to match canonical ordering.
func formatInputs(inputs []Input) string {
	if len(inputs) == 0 {
		return ""
	}
	sorted := append([]Input(nil), inputs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Role != sorted[j].Role {
			return sorted[i].Role < sorted[j].Role
		}
		return sorted[i].Hash.String() < sorted[j].Hash.String()
	})
	parts := make([]string, len(sorted))
	for i, inp := range sorted {
		parts[i] = fmt.Sprintf("%s:%s", inp.Role, inp.Hash)
	}
	return strings.Join(parts, ",")
}

// formatOutputs returns a deterministic string representation of the Outputs
// slice, sorted by (role, kind) to match canonical ordering.
func formatOutputs(outputs []OutputDecl) string {
	if len(outputs) == 0 {
		return ""
	}
	sorted := append([]OutputDecl(nil), outputs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Role != sorted[j].Role {
			return sorted[i].Role < sorted[j].Role
		}
		return sorted[i].Kind < sorted[j].Kind
	})
	parts := make([]string, len(sorted))
	for i, o := range sorted {
		if o.Kind != "" {
			parts[i] = fmt.Sprintf("%s(%s)", o.Role, o.Kind)
		} else {
			parts[i] = o.Role
		}
	}
	return strings.Join(parts, ",")
}
