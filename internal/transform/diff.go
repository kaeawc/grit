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

	deltas = append(deltas, diffEnv(old.Env, new.Env)...)
	deltas = append(deltas, diffInputs(old.Inputs, new.Inputs)...)
	deltas = append(deltas, diffOrderedInputs(old.OrderedInputs, new.OrderedInputs)...)
	deltas = append(deltas, diffOutputs(old.Outputs, new.Outputs)...)

	return deltas
}

// diffEnv reports per-key Env deltas (Env[KEY]) so a single divergent
// environment variable doesn't smear the whole map into one opaque delta.
func diffEnv(oldEnv, newEnv map[string]string) []FieldDelta {
	if len(oldEnv) == 0 && len(newEnv) == 0 {
		return nil
	}
	keys := map[string]struct{}{}
	for k := range oldEnv {
		keys[k] = struct{}{}
	}
	for k := range newEnv {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var deltas []FieldDelta
	for _, k := range sorted {
		ov, oOK := oldEnv[k]
		nv, nOK := newEnv[k]
		if oOK == nOK && ov == nv {
			continue
		}
		deltas = append(deltas, FieldDelta{"Env[" + k + "]", ov, nv})
	}
	return deltas
}

// diffInputs reports per-role Input deltas (Inputs[role]) so the field name
// names the specific input that changed.
func diffInputs(oldInputs, newInputs []Input) []FieldDelta {
	if len(oldInputs) == 0 && len(newInputs) == 0 {
		return nil
	}
	roles := map[string]struct{}{}
	oldByRole := map[string]string{}
	newByRole := map[string]string{}
	for _, in := range oldInputs {
		oldByRole[in.Role] = in.Hash.String()
		roles[in.Role] = struct{}{}
	}
	for _, in := range newInputs {
		newByRole[in.Role] = in.Hash.String()
		roles[in.Role] = struct{}{}
	}
	sorted := make([]string, 0, len(roles))
	for r := range roles {
		sorted = append(sorted, r)
	}
	sort.Strings(sorted)
	var deltas []FieldDelta
	for _, r := range sorted {
		ov := oldByRole[r]
		nv := newByRole[r]
		if ov == nv {
			continue
		}
		deltas = append(deltas, FieldDelta{"Inputs[" + r + "]", ov, nv})
	}
	return deltas
}

// diffOrderedInputs reports per-position input deltas. OrderedInputs preserve
// caller-provided order because classpath-like inputs can shadow each other.
func diffOrderedInputs(oldInputs, newInputs []Input) []FieldDelta {
	if len(oldInputs) == 0 && len(newInputs) == 0 {
		return nil
	}
	maxLen := len(oldInputs)
	if len(newInputs) > maxLen {
		maxLen = len(newInputs)
	}
	var deltas []FieldDelta
	for i := 0; i < maxLen; i++ {
		var oldInput, newInput Input
		if i < len(oldInputs) {
			oldInput = oldInputs[i]
		}
		if i < len(newInputs) {
			newInput = newInputs[i]
		}
		ov := formatInput(oldInput)
		nv := formatInput(newInput)
		if ov == nv {
			continue
		}
		deltas = append(deltas, FieldDelta{fmt.Sprintf("OrderedInputs[%d]", i), ov, nv})
	}
	return deltas
}

func formatInput(input Input) string {
	if input.Role == "" && input.Hash.IsZero() {
		return ""
	}
	if input.Role == "" {
		return input.Hash.String()
	}
	return input.Role + ":" + input.Hash.String()
}

// diffOutputs reports per-role Output deltas mirroring diffInputs.
func diffOutputs(oldOutputs, newOutputs []OutputDecl) []FieldDelta {
	if len(oldOutputs) == 0 && len(newOutputs) == 0 {
		return nil
	}
	roles := map[string]struct{}{}
	oldByRole := map[string]string{}
	newByRole := map[string]string{}
	render := func(o OutputDecl) string {
		if o.Kind != "" {
			return o.Kind
		}
		return o.Role
	}
	for _, o := range oldOutputs {
		oldByRole[o.Role] = render(o)
		roles[o.Role] = struct{}{}
	}
	for _, o := range newOutputs {
		newByRole[o.Role] = render(o)
		roles[o.Role] = struct{}{}
	}
	sorted := make([]string, 0, len(roles))
	for r := range roles {
		sorted = append(sorted, r)
	}
	sort.Strings(sorted)
	var deltas []FieldDelta
	for _, r := range sorted {
		ov := oldByRole[r]
		nv := newByRole[r]
		if ov == nv {
			continue
		}
		deltas = append(deltas, FieldDelta{"Outputs[" + r + "]", ov, nv})
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
