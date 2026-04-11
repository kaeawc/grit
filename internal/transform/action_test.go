package transform

import (
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func TestActionHashStable(t *testing.T) {
	a := sampleAction()
	if a.Hash() != a.Hash() {
		t.Fatalf("action hash not stable across calls")
	}
}

func TestActionHashDiffersOnKind(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Kind = "different-kind"
	if base.Hash() == mod.Hash() {
		t.Fatalf("expected different Kind to change hash")
	}
}

func TestActionHashDiffersOnTool(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Tool = "other-tool"
	if base.Hash() == mod.Hash() {
		t.Fatalf("expected different Tool to change hash")
	}
}

func TestActionHashDiffersOnToolVersion(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.ToolVersion = "0.2"
	if base.Hash() == mod.Hash() {
		t.Fatalf("expected different ToolVersion to change hash")
	}
}

func TestActionHashRespectsArgOrder(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Args = []string{base.Args[1], base.Args[0]}
	if base.Hash() == mod.Hash() {
		t.Fatalf("argument order must affect action hash")
	}
}

func TestActionHashInvariantToInputOrder(t *testing.T) {
	base := sampleAction()
	shuffled := base
	shuffled.Inputs = []Input{base.Inputs[1], base.Inputs[0]}
	if base.Hash() != shuffled.Hash() {
		t.Fatalf("input order must not affect action hash")
	}
}

func TestActionHashInvariantToOutputOrder(t *testing.T) {
	base := sampleAction()
	shuffled := base
	shuffled.Outputs = []OutputDecl{base.Outputs[1], base.Outputs[0]}
	if base.Hash() != shuffled.Hash() {
		t.Fatalf("output order must not affect action hash")
	}
}

func TestActionHashIncludesEnv(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Env = map[string]string{"LANG": "en_US", "TZ": "UTC"}
	if base.Hash() == mod.Hash() {
		t.Fatalf("env values must affect action hash")
	}
}

func TestActionHashIncludesNewEnvKey(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Env = map[string]string{"LANG": "C", "TZ": "UTC", "EXTRA": "1"}
	if base.Hash() == mod.Hash() {
		t.Fatalf("adding an env key must affect action hash")
	}
}

func TestActionHashIncludesInputHash(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Inputs = append([]Input(nil), base.Inputs...)
	mod.Inputs[0].Hash = cas.HashBytes([]byte("different-input"))
	if base.Hash() == mod.Hash() {
		t.Fatalf("input hash must affect action hash")
	}
}

func TestActionHashIncludesOutputShape(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Outputs = []OutputDecl{{Role: "something-else", Kind: "dex"}}
	if base.Hash() == mod.Hash() {
		t.Fatalf("output shape must affect action hash")
	}
}

func TestActionHashDifferentInputRoles(t *testing.T) {
	base := sampleAction()
	mod := base
	mod.Inputs = append([]Input(nil), base.Inputs...)
	mod.Inputs[0].Role = "renamed"
	if base.Hash() == mod.Hash() {
		t.Fatalf("input role must affect action hash")
	}
}

func sampleAction() Action {
	return Action{
		Kind:        "aar-extract",
		Tool:        "grit-aar-extract",
		ToolVersion: "0.1",
		Args:        []string{"--flat", "--verbose"},
		Env: map[string]string{
			"LANG": "C",
			"TZ":   "UTC",
		},
		Inputs: []Input{
			{Role: "aar", Hash: cas.HashBytes([]byte("aar-bytes"))},
			{Role: "manifest-stub", Hash: cas.HashBytes([]byte("stub"))},
		},
		Outputs: []OutputDecl{
			{Role: "classes-jar", Kind: "jar"},
			{Role: "android-manifest", Kind: "xml"},
		},
	}
}
