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

// TestActionHashGoldenDigest pins the canonical encoding of sampleAction
// to a specific hex digest. Any change to canonical encoding (field order,
// version constant, Input/Output ordering rules, JSON omitempty rules) will
// break this test, forcing a deliberate decision: bump canonicalVersion
// (and accept that every cached action result is now invalid) or revert.
//
// To regenerate after an intentional canonical change, run with -v and
// copy the first line printed. Do not regenerate casually.
func TestActionHashGoldenDigest(t *testing.T) {
	const want = "7848e77a29841e01d7d9f3a3543a7e6d8d1da6c516d1f3ff86a643f757754cdc"
	got := sampleAction().Hash().String()
	if got != want {
		t.Fatalf("canonical encoding drifted\n  got:  %s\n  want: %s\n\nIf this is intentional, bump canonicalVersion in action.go and update this constant.", got, want)
	}
}

// TestActionHashSameRoleClasspathOrderingGap documents a known limitation:
// when multiple Inputs share the same Role (e.g. fifty "classpath" entries
// for kotlinc), canonicalization sorts them by hash. That makes the action
// hash invariant to the *caller's* slice order, which is correct for
// most actions but wrong for tools like kotlinc/javac whose semantics
// depend on classpath shadowing order. Callers that need ordered inputs
// must use distinct role names ("classpath-0", "classpath-1", ...) until
// the canonical encoding is extended with an ordered-input mode.
//
// This test exists to make the limitation explicit: shuffling the slice
// today does NOT change the hash, even though for kotlinc it semantically
// should.
func TestActionHashSameRoleClasspathOrderingGap(t *testing.T) {
	a := Action{
		Kind: "compile",
		Tool: "kotlinc",
		Inputs: []Input{
			{Role: "classpath", Hash: cas.HashBytes([]byte("a.jar"))},
			{Role: "classpath", Hash: cas.HashBytes([]byte("b.jar"))},
		},
	}
	b := Action{
		Kind: "compile",
		Tool: "kotlinc",
		Inputs: []Input{
			{Role: "classpath", Hash: cas.HashBytes([]byte("b.jar"))},
			{Role: "classpath", Hash: cas.HashBytes([]byte("a.jar"))},
		},
	}
	if a.Hash() != b.Hash() {
		t.Fatalf("regression: same-role inputs are no longer order-invariant; update the audit comment if this is intentional")
	}
}

// TestActionHashEmptyAndNilSlicesAgree confirms that a caller building an
// Action with nil-vs-empty slice fields produces the same hash. Without
// this guarantee, two callers constructing semantically identical actions
// could miss each other's cache hits.
func TestActionHashEmptyAndNilSlicesAgree(t *testing.T) {
	withNils := Action{Kind: "noop", Tool: "true"}
	withEmpties := Action{
		Kind:    "noop",
		Tool:    "true",
		Args:    []string{},
		Env:     map[string]string{},
		Inputs:  []Input{},
		Outputs: []OutputDecl{},
	}
	if withNils.Hash() != withEmpties.Hash() {
		t.Fatalf("nil and empty collection fields hash differently:\n  nils:    %s\n  empties: %s", withNils.Hash(), withEmpties.Hash())
	}
}
