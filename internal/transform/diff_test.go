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
	if len(deltas) != 7 {
		t.Fatalf("expected 7 deltas, got %d: %+v", len(deltas), deltas)
	}
	names := make(map[string]bool)
	for _, d := range deltas {
		names[d.FieldName] = true
	}
	for _, want := range []string{"Kind", "Tool", "ToolVersion", "Args", "Env", "Inputs", "Outputs"} {
		if !names[want] {
			t.Errorf("missing delta for field %q", want)
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
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	if deltas[0].FieldName != "Env" {
		t.Errorf("expected field Env, got %s", deltas[0].FieldName)
	}
}
