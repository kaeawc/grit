package project

import (
	"reflect"
	"sort"
	"testing"
)

func TestSimpleClassNameStripsPackage(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"Foo":             "Foo",
		"com.example.Foo": "Foo",
		"com.squareup.anvil.conventions.LibraryPlugin": "LibraryPlugin",
		"single.":  "",
		".leading": "leading",
	}
	for in, want := range cases {
		if got := simpleClassName(in); got != want {
			t.Errorf("simpleClassName(%q): got %q want %q", in, got, want)
		}
	}
}

func TestResolvePluginAccessorMatchesCatalogAlias(t *testing.T) {
	aliases := map[string]string{
		"kotlin.jvm":          "org.jetbrains.kotlin.jvm",
		"android.application": "com.android.application",
		"foundry.base":        "foundry.base",
	}
	cases := []struct {
		accessor string
		want     string
	}{
		{"target.libs.plugins.kotlin.jvm", "org.jetbrains.kotlin.jvm"},
		{"libs.plugins.kotlin.jvm", "org.jetbrains.kotlin.jvm"},
		{"libs.plugins.android.application", "com.android.application"},
		{"libs.plugins.foundry.base", "foundry.base"},
		{"libs.plugins.kotlin", ""},    // no "kotlin" alias
		{"libs.plugins", ""},           // no segments after plugins
		{"target.versions.kotlin", ""}, // wrong segment family
		{"randomReceiver.libs.plugins.kotlin.jvm", "org.jetbrains.kotlin.jvm"},
	}
	for _, tc := range cases {
		t.Run(tc.accessor, func(t *testing.T) {
			if got := resolvePluginAccessor(tc.accessor, aliases); got != tc.want {
				t.Errorf("resolvePluginAccessor(%q): got %q want %q", tc.accessor, got, tc.want)
			}
		})
	}
}

func TestParseAppliedPluginIDsCombinesLiteralAndAccessorForms(t *testing.T) {
	body := `
class LibraryPlugin : Plugin<Project> {
    override fun apply(target: Project) {
        target.plugins.apply("com.android.lint")
        target.plugins.apply(target.libs.plugins.kotlin.jvm.pluginId)
        target.plugins.apply(target.libs.plugins.kotlinx.binaryCompatibility.pluginId)
    }
}
`
	aliases := map[string]string{
		"kotlin.jvm":                  "org.jetbrains.kotlin.jvm",
		"kotlinx.binaryCompatibility": "org.jetbrains.kotlinx.binary-compatibility-validator",
	}
	got := parseAppliedPluginIDs(body, aliases)
	sort.Strings(got)
	want := []string{
		"com.android.lint",
		"org.jetbrains.kotlin.jvm",
		"org.jetbrains.kotlinx.binary-compatibility-validator",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("applied ids:\n got  %v\n want %v", got, want)
	}
}

func TestParseAppliedPluginIDsHandlesNilAliasMap(t *testing.T) {
	body := `target.plugins.apply("com.example.foo")` + "\n" + `target.plugins.apply(libs.plugins.kotlin.jvm.pluginId)`
	got := parseAppliedPluginIDs(body, nil)
	want := []string{"com.example.foo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("with nil aliases:\n got  %v\n want %v", got, want)
	}
}
