package intellijsync

import (
	"testing"
)

func TestClasspathToOrderEntriesDeduplicatesAndSorts(t *testing.T) {
	entries := []ClasspathEntry{
		{Kind: OrderEntryKindLibrary, Name: "okhttp", Scope: "compile", Classes: "/repo/okhttp-4.12.0.jar"},
		{Kind: OrderEntryKindModule, Name: ":core", Scope: "compile", ModulePath: ":core", Exported: true},
		{Kind: OrderEntryKindSDK, Name: "Android API 34", Scope: "compile"},
		{Kind: OrderEntryKindLibrary, Name: "gson", Scope: "compile", Classes: "/repo/gson-2.10.jar"},
		// Duplicate module entry — should be deduplicated.
		{Kind: OrderEntryKindModule, Name: ":core", Scope: "compile", ModulePath: ":core", Exported: true},
		// Duplicate library entry — should be deduplicated.
		{Kind: OrderEntryKindLibrary, Name: "okhttp", Scope: "compile", Classes: "/repo/okhttp-4.12.0.jar"},
	}

	got := ClasspathToOrderEntries(entries)
	if len(got) != 4 {
		t.Fatalf("expected 4 deduplicated entries, got %d: %#v", len(got), got)
	}

	// SDK first.
	if got[0].Kind != OrderEntryKindSDK || got[0].Name != "Android API 34" {
		t.Fatalf("expected SDK entry first, got %#v", got[0])
	}
	// Then module.
	if got[1].Kind != OrderEntryKindModule || got[1].ModulePath != ":core" {
		t.Fatalf("expected module entry second, got %#v", got[1])
	}
	if !got[1].Exported {
		t.Fatalf("expected module entry to be exported, got %#v", got[1])
	}
	// Then libraries in alphabetical order.
	if got[2].Kind != OrderEntryKindLibrary || got[2].Name != "gson" {
		t.Fatalf("expected gson library third, got %#v", got[2])
	}
	if got[3].Kind != OrderEntryKindLibrary || got[3].Name != "okhttp" {
		t.Fatalf("expected okhttp library fourth, got %#v", got[3])
	}
}

func TestClasspathToOrderEntriesEmptyInput(t *testing.T) {
	got := ClasspathToOrderEntries(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty result for nil input, got %#v", got)
	}

	got = ClasspathToOrderEntries([]ClasspathEntry{})
	if len(got) != 0 {
		t.Fatalf("expected empty result for empty input, got %#v", got)
	}
}

func TestVariantOrderEntriesProjectsAllKinds(t *testing.T) {
	v := Variant{
		Name:       "debug",
		CompileSDK: "34",
		Dependencies: []Dependency{
			{Kind: "module", TargetModulePath: ":lib"},
			{Kind: "variant", TargetModulePath: ":core", TargetVariantName: "debug"},
		},
		Materialization: Materialization{
			ClasspathSnapshotIDs: []string{
				"/caches/transforms/okhttp-4.12.0.jar",
				"/caches/transforms/gson-2.10.jar",
			},
		},
	}

	got := VariantOrderEntries(v)

	// Expect: 1 SDK + 2 modules + 2 libraries = 5.
	if len(got) != 5 {
		t.Fatalf("expected 5 entries, got %d: %#v", len(got), got)
	}

	// SDK first.
	if got[0].Kind != OrderEntryKindSDK || got[0].Name != "Android API 34" {
		t.Fatalf("expected SDK entry, got %#v", got[0])
	}

	// Modules sorted alphabetically.
	if got[1].Kind != OrderEntryKindModule || got[1].ModulePath != ":core" {
		t.Fatalf("expected :core module entry, got %#v", got[1])
	}
	if got[1].Name != ":core/debug" {
		t.Fatalf("expected variant-qualified module name, got %q", got[1].Name)
	}
	if got[2].Kind != OrderEntryKindModule || got[2].ModulePath != ":lib" {
		t.Fatalf("expected :lib module entry, got %#v", got[2])
	}

	// Libraries sorted alphabetically by derived name.
	if got[3].Kind != OrderEntryKindLibrary || got[3].Name != "gson-2.10" {
		t.Fatalf("expected gson library entry, got %#v", got[3])
	}
	if got[3].Sources == "" || got[3].Javadoc == "" {
		t.Fatalf("expected inferred sources and javadoc for JAR library, got %#v", got[3])
	}
	if got[4].Kind != OrderEntryKindLibrary || got[4].Name != "okhttp-4.12.0" {
		t.Fatalf("expected okhttp library entry, got %#v", got[4])
	}
}

func TestVariantOrderEntriesNoCompileSDK(t *testing.T) {
	v := Variant{
		Name: "main",
		Dependencies: []Dependency{
			{Kind: "module", TargetModulePath: ":utils"},
		},
	}

	got := VariantOrderEntries(v)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry (no SDK), got %d: %#v", len(got), got)
	}
	if got[0].Kind != OrderEntryKindModule {
		t.Fatalf("expected module entry, got %#v", got[0])
	}
}

func TestVariantOrderEntriesDeduplicatesDependencies(t *testing.T) {
	v := Variant{
		Name: "debug",
		Dependencies: []Dependency{
			{Kind: "module", TargetModulePath: ":lib"},
			{Kind: "variant", TargetModulePath: ":lib", TargetVariantName: "debug"},
		},
	}

	got := VariantOrderEntries(v)
	// Both point to :lib — the module dep has key "module::lib" and the variant
	// dep has key "module::lib" too, so only one survives dedup.
	if len(got) != 1 {
		t.Fatalf("expected 1 deduplicated entry, got %d: %#v", len(got), got)
	}
}

func TestClasspathEntryFromSnapshotIDInfersCompanionJars(t *testing.T) {
	entry := classpathEntryFromSnapshotID("/repo/libs/retrofit-2.9.0.jar")
	if entry.Kind != OrderEntryKindLibrary {
		t.Fatalf("expected library kind, got %q", entry.Kind)
	}
	if entry.Name != "retrofit-2.9.0" {
		t.Fatalf("expected derived name, got %q", entry.Name)
	}
	if entry.Classes != "/repo/libs/retrofit-2.9.0.jar" {
		t.Fatalf("expected classes path, got %q", entry.Classes)
	}
	if entry.Sources != "/repo/libs/retrofit-2.9.0-sources.jar" {
		t.Fatalf("expected sources path, got %q", entry.Sources)
	}
	if entry.Javadoc != "/repo/libs/retrofit-2.9.0-javadoc.jar" {
		t.Fatalf("expected javadoc path, got %q", entry.Javadoc)
	}
}

func TestClasspathEntryFromSnapshotIDAar(t *testing.T) {
	entry := classpathEntryFromSnapshotID("/repo/libs/material-1.9.0.aar")
	if entry.Name != "material-1.9.0" {
		t.Fatalf("expected derived name, got %q", entry.Name)
	}
	if entry.Sources != "/repo/libs/material-1.9.0-sources.jar" {
		t.Fatalf("expected sources path for AAR, got %q", entry.Sources)
	}
}

func TestClasspathEntryFromSnapshotIDDirectory(t *testing.T) {
	entry := classpathEntryFromSnapshotID("/build/intermediates/classes/debug")
	if entry.Name != "debug" {
		t.Fatalf("expected derived name from directory, got %q", entry.Name)
	}
	if entry.Sources != "" || entry.Javadoc != "" {
		t.Fatalf("expected no companion inference for non-jar path, got sources=%q javadoc=%q", entry.Sources, entry.Javadoc)
	}
}

func TestLibraryNameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/repo/okhttp-4.12.0.jar", "okhttp-4.12.0"},
		{"/repo/material-1.9.0.aar", "material-1.9.0"},
		{"/build/classes/debug", "debug"},
		{"simple.jar", "simple"},
	}
	for _, tc := range tests {
		got := libraryNameFromPath(tc.path)
		if got != tc.want {
			t.Errorf("libraryNameFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
