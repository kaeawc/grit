package transform

import (
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func TestDiffActionHash_Identical(t *testing.T) {
	a := Action{
		Kind:        "dex",
		Tool:        "d8",
		ToolVersion: "1.0",
		Args:        []string{"--min-api", "21"},
		Env:         map[string]string{"JAVA_HOME": "/jdk"},
		Inputs:      []Input{{Role: "classpath", Hash: cas.HashBytes([]byte("lib"))}},
		Outputs:     []OutputDecl{{Role: "dex", Kind: "file"}},
	}
	deltas := DiffActionHash(a, a)
	if len(deltas) != 0 {
		t.Fatalf("identical actions should produce no deltas, got %d", len(deltas))
	}
}

func TestDiffActionHash_AllFieldsDiffer(t *testing.T) {
	old := Action{
		Kind:        "aar-extract",
		Tool:        "unzip",
		ToolVersion: "1.0",
		Args:        []string{"-o"},
		Env:         map[string]string{"HOME": "/a"},
		Inputs:      []Input{{Role: "archive", Hash: cas.HashBytes([]byte("old"))}},
		Outputs:     []OutputDecl{{Role: "classes", Kind: "jar"}},
	}
	new := Action{
		Kind:        "dex",
		Tool:        "d8",
		ToolVersion: "2.0",
		Args:        []string{"--release"},
		Env:         map[string]string{"JAVA_HOME": "/jdk"},
		Inputs:      []Input{{Role: "classpath", Hash: cas.HashBytes([]byte("new"))}},
		Outputs:     []OutputDecl{{Role: "dex", Kind: "file"}},
	}
	deltas := DiffActionHash(old, new)
	names := make(map[string]bool)
	for _, d := range deltas {
		names[d.FieldName] = true
	}
	// Env, Inputs and Outputs are now reported per-key/per-role rather
	// than as a single concatenated delta.
	for _, want := range []string{
		"Kind", "Tool", "ToolVersion", "Args",
		"Env[HOME]", "Env[JAVA_HOME]",
		"Inputs[archive]", "Inputs[classpath]",
		"Outputs[classes]", "Outputs[dex]",
	} {
		if !names[want] {
			t.Errorf("missing delta for field %q (got %+v)", want, deltas)
		}
	}
}

func TestDiffActionHash_SingleFieldDiffers(t *testing.T) {
	base := Action{
		Kind:        "dex",
		Tool:        "d8",
		ToolVersion: "1.0",
		Args:        []string{"--min-api", "21"},
	}
	modified := base
	modified.ToolVersion = "2.0"

	deltas := DiffActionHash(base, modified)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d: %+v", len(deltas), deltas)
	}
	d := deltas[0]
	if d.FieldName != "ToolVersion" {
		t.Errorf("expected field ToolVersion, got %s", d.FieldName)
	}
	if d.OldValue != "1.0" || d.NewValue != "2.0" {
		t.Errorf("unexpected values: old=%q new=%q", d.OldValue, d.NewValue)
	}
}

func TestDiffActionHash_InputOrderIrrelevant(t *testing.T) {
	h1 := cas.HashBytes([]byte("a"))
	h2 := cas.HashBytes([]byte("b"))
	a := Action{
		Inputs: []Input{{Role: "src", Hash: h1}, {Role: "lib", Hash: h2}},
	}
	b := Action{
		Inputs: []Input{{Role: "lib", Hash: h2}, {Role: "src", Hash: h1}},
	}
	deltas := DiffActionHash(a, b)
	if len(deltas) != 0 {
		t.Fatalf("reordered inputs should produce no deltas, got %d: %+v", len(deltas), deltas)
	}
}

func TestDiffActionHash_SameRoleInputOrderIrrelevant(t *testing.T) {
	h1 := cas.HashBytes([]byte("a"))
	h2 := cas.HashBytes([]byte("b"))
	old := Action{
		Inputs: []Input{{Role: "src", Hash: h1}, {Role: "src", Hash: h2}},
	}
	new := Action{
		Inputs: []Input{{Role: "src", Hash: h2}, {Role: "src", Hash: h1}},
	}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 0 {
		t.Fatalf("reordered same-role inputs should produce no deltas, got %d: %+v", len(deltas), deltas)
	}
}

func TestDiffActionHash_SameRoleInputMultisetDiffers(t *testing.T) {
	h1 := cas.HashBytes([]byte("a"))
	h2 := cas.HashBytes([]byte("b"))
	h3 := cas.HashBytes([]byte("c"))
	old := Action{
		Inputs: []Input{{Role: "src", Hash: h1}, {Role: "src", Hash: h2}},
	}
	new := Action{
		Inputs: []Input{{Role: "src", Hash: h1}, {Role: "src", Hash: h3}},
	}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 same-role input delta, got %d: %+v", len(deltas), deltas)
	}
	if deltas[0].FieldName != "Inputs[src]" {
		t.Fatalf("expected Inputs[src], got %s", deltas[0].FieldName)
	}
	if deltas[0].OldValue != formatInputHashes([]string{h1.String(), h2.String()}) ||
		deltas[0].NewValue != formatInputHashes([]string{h1.String(), h3.String()}) {
		t.Fatalf("unexpected same-role input values: %+v", deltas[0])
	}
}

func TestDiffActionHash_OrderedInputOrderReported(t *testing.T) {
	h1 := cas.HashBytes([]byte("a.jar"))
	h2 := cas.HashBytes([]byte("b.jar"))
	old := Action{
		OrderedInputs: []Input{{Role: "classpath", Hash: h1}, {Role: "classpath", Hash: h2}},
	}
	new := Action{
		OrderedInputs: []Input{{Role: "classpath", Hash: h2}, {Role: "classpath", Hash: h1}},
	}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 2 {
		t.Fatalf("expected 2 ordered input deltas, got %d: %+v", len(deltas), deltas)
	}
	if deltas[0].FieldName != "OrderedInputs[0]" || deltas[1].FieldName != "OrderedInputs[1]" {
		t.Fatalf("unexpected ordered input fields: %+v", deltas)
	}
	if deltas[0].OldValue != "classpath:"+h1.String() || deltas[0].NewValue != "classpath:"+h2.String() {
		t.Fatalf("unexpected first ordered input delta: %+v", deltas[0])
	}
	if deltas[1].OldValue != "classpath:"+h2.String() || deltas[1].NewValue != "classpath:"+h1.String() {
		t.Fatalf("unexpected second ordered input delta: %+v", deltas[1])
	}
}

func TestDiffActionHash_OrderedInputAddedAndRemoved(t *testing.T) {
	h := cas.HashBytes([]byte("a.jar"))
	old := Action{OrderedInputs: []Input{{Role: "classpath", Hash: h}}}
	new := Action{}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 ordered input delta, got %d: %+v", len(deltas), deltas)
	}
	if deltas[0].FieldName != "OrderedInputs[0]" || deltas[0].OldValue != "classpath:"+h.String() || deltas[0].NewValue != "" {
		t.Fatalf("unexpected ordered input removal delta: %+v", deltas[0])
	}
}

func TestDiffActionHash_EmptyActions(t *testing.T) {
	deltas := DiffActionHash(Action{}, Action{})
	if len(deltas) != 0 {
		t.Fatalf("two empty actions should produce no deltas, got %d", len(deltas))
	}
}

func TestDiffActionHash_EnvKeyDiffers(t *testing.T) {
	old := Action{Env: map[string]string{"A": "1", "B": "2"}}
	new := Action{Env: map[string]string{"A": "1", "B": "3"}}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d: %+v", len(deltas), deltas)
	}
	if deltas[0].FieldName != "Env[B]" {
		t.Errorf("expected field Env[B], got %s", deltas[0].FieldName)
	}
	if deltas[0].OldValue != "2" || deltas[0].NewValue != "3" {
		t.Errorf("expected old=2 new=3, got old=%q new=%q", deltas[0].OldValue, deltas[0].NewValue)
	}
}

func TestDiffActionHash_EnvKeyAddedAndRemoved(t *testing.T) {
	old := Action{Env: map[string]string{"A": "1", "GONE": "x"}}
	new := Action{Env: map[string]string{"A": "1", "ADDED": "y"}}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d: %+v", len(deltas), deltas)
	}
	got := map[string]FieldDelta{}
	for _, d := range deltas {
		got[d.FieldName] = d
	}
	if d, ok := got["Env[ADDED]"]; !ok || d.OldValue != "" || d.NewValue != "y" {
		t.Errorf("expected Env[ADDED] empty→y, got %+v", d)
	}
	if d, ok := got["Env[GONE]"]; !ok || d.OldValue != "x" || d.NewValue != "" {
		t.Errorf("expected Env[GONE] x→empty, got %+v", d)
	}
}

func TestDiffActionHash_InputRoleGranularity(t *testing.T) {
	h1 := cas.HashBytes([]byte("a"))
	h2 := cas.HashBytes([]byte("b"))
	h3 := cas.HashBytes([]byte("c"))
	old := Action{Inputs: []Input{{Role: "src", Hash: h1}, {Role: "lib", Hash: h2}}}
	new := Action{Inputs: []Input{{Role: "src", Hash: h3}, {Role: "lib", Hash: h2}}}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta (only src changed), got %d: %+v", len(deltas), deltas)
	}
	if deltas[0].FieldName != "Inputs[src]" {
		t.Errorf("expected Inputs[src], got %s", deltas[0].FieldName)
	}
	if deltas[0].OldValue != h1.String() || deltas[0].NewValue != h3.String() {
		t.Errorf("hash values not surfaced: %+v", deltas[0])
	}
}

func TestDiffActionHash_SameRoleOutputOrderIrrelevant(t *testing.T) {
	old := Action{
		Outputs: []OutputDecl{{Role: "classes", Kind: "jar"}, {Role: "classes", Kind: "directory"}},
	}
	new := Action{
		Outputs: []OutputDecl{{Role: "classes", Kind: "directory"}, {Role: "classes", Kind: "jar"}},
	}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 0 {
		t.Fatalf("reordered same-role outputs should produce no deltas, got %d: %+v", len(deltas), deltas)
	}
}

func TestDiffActionHash_SameRoleOutputMultisetDiffers(t *testing.T) {
	old := Action{
		Outputs: []OutputDecl{{Role: "classes", Kind: "jar"}, {Role: "classes", Kind: "directory"}},
	}
	new := Action{
		Outputs: []OutputDecl{{Role: "classes", Kind: "jar"}, {Role: "classes", Kind: "metadata"}},
	}
	deltas := DiffActionHash(old, new)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 same-role output delta, got %d: %+v", len(deltas), deltas)
	}
	if deltas[0].FieldName != "Outputs[classes]" {
		t.Fatalf("expected Outputs[classes], got %s", deltas[0].FieldName)
	}
	if deltas[0].OldValue != formatOutputDecls([]string{"jar", "directory"}) ||
		deltas[0].NewValue != formatOutputDecls([]string{"jar", "metadata"}) {
		t.Fatalf("unexpected same-role output values: %+v", deltas[0])
	}
}
