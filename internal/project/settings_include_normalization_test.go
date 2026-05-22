package project

import (
	"reflect"
	"testing"
)

func TestNormalizeIncludePathAddsLeadingColon(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"foo":              ":foo",
		":foo":             ":foo",
		"  foo  ":          ":foo",
		"core:data":        ":core:data",
		":core:data":       ":core:data",
		"samples:compose":  ":samples:compose",
		":samples:compose": ":samples:compose",
	}
	for in, want := range cases {
		if got := normalizeIncludePath(in); got != want {
			t.Errorf("normalizeIncludePath(%q): got %q want %q", in, got, want)
		}
	}
}

func TestParseSettingsKTSNormalizesIncludeArgs(t *testing.T) {
	body := `
rootProject.name = "demo"
include("core", ":app", "feature:foo", " spaced ", ":app")
`
	model := parseSettingsKTS(body)
	// mergeStrings dedupes and preserves insertion order.
	want := []string{":core", ":app", ":feature:foo", ":spaced"}
	got := append([]string(nil), model.Includes...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("includes:\n got  %v\n want %v", got, want)
	}
}
