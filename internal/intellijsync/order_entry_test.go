package intellijsync

import (
	"testing"

	"github.com/kaeawc/grit/internal/classpath"
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

func TestVariantOrderEntriesIgnoresOpaqueSnapshotIDs(t *testing.T) {
	v := Variant{
		Name:       "debug",
		CompileSDK: "34",
		Dependencies: []Dependency{
			{Kind: "module", TargetModulePath: ":lib"},
		},
		Materialization: Materialization{
			ClasspathSnapshotIDs: []string{
				"classpath-snapshot-123",
				"ordered-entries-456",
				"/caches/transforms/material-1.9.0.aar",
			},
		},
	}

	got := VariantOrderEntries(v)
	if len(got) != 3 {
		t.Fatalf("expected sdk, module, and one library entry, got %d: %#v", len(got), got)
	}
	if got[2].Kind != OrderEntryKindLibrary || got[2].Classes != "/caches/transforms/material-1.9.0.aar" {
		t.Fatalf("expected only path-like snapshot ids to become library entries, got %#v", got)
	}
}

func TestVariantOrderEntriesIgnoresDocumentationSnapshotJars(t *testing.T) {
	v := Variant{
		Name:       "debug",
		CompileSDK: "34",
		Materialization: Materialization{
			ClasspathSnapshotIDs: []string{
				"/caches/transforms/okhttp-4.12.0-sources.jar",
				"/caches/transforms/okhttp-4.12.0-javadoc.jar",
				"/caches/transforms/okhttp-4.12.0.jar",
			},
		},
	}

	got := VariantOrderEntries(v)
	if len(got) != 2 {
		t.Fatalf("expected sdk + binary library entries, got %d: %#v", len(got), got)
	}
	if got[1].Kind != OrderEntryKindLibrary || got[1].Classes != "/caches/transforms/okhttp-4.12.0.jar" {
		t.Fatalf("expected only the binary jar to project as a library entry, got %#v", got[1])
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

func TestLooksLikeLibrarySnapshotPath(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"/repo/okhttp-4.12.0.jar", true},
		{"/repo/material-1.9.0.aar", true},
		{"classpath-snapshot-123", false},
		{"ordered-entries-456", false},
		{"/build/classes/debug", false},
	}
	for _, tc := range tests {
		if got := looksLikeLibrarySnapshotPath(tc.id); got != tc.want {
			t.Errorf("looksLikeLibrarySnapshotPath(%q) = %t, want %t", tc.id, got, tc.want)
		}
	}
}

func TestClasspathRecordToOrderEntriesProjectsRealClasspathModel(t *testing.T) {
	record := classpath.Record{
		Scope:       classpath.ScopeCompile,
		ToolchainID: "ignored-when-compile-sdk-present",
		Entries: []classpath.EntryRecord{
			{
				Order:          0,
				Path:           "/repo/app/src/main",
				NormalizedPath: "/repo/app/src/main",
				Origin:         classpath.OriginSource,
				ModuleID:       "module-app",
				VariantID:      "variant-debug",
			},
			{
				Order:          1,
				Path:           "/repo/lib/src/main",
				NormalizedPath: "/repo/lib/src/main",
				Origin:         classpath.OriginSource,
				ModuleID:       "module-lib",
				VariantID:      "variant-debug",
			},
			{
				Order:          2,
				Path:           "/repo/lib/src/debug",
				NormalizedPath: "/repo/lib/src/debug",
				Origin:         classpath.OriginSource,
				ModuleID:       "module-lib",
				VariantID:      "variant-debug",
			},
			{
				Order:          3,
				Path:           "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0.jar",
				NormalizedPath: "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0.jar",
				Origin:         classpath.OriginArtifact,
				FamilyKey:      "com.squareup.okhttp3:okhttp",
			},
			{
				Order:          4,
				Path:           "/repo/tools/build-logic.jar",
				NormalizedPath: "/repo/tools/build-logic.jar",
				Origin:         classpath.OriginGenerated,
			},
		},
	}

	got := ClasspathRecordToOrderEntries(record, ClasspathOrderEntryOptions{
		CompileSDK:      "34",
		CurrentModuleID: "module-app",
		ModulePaths: map[string]string{
			"module-app": ":app",
			"module-lib": ":lib",
		},
		VariantNames: map[string]string{
			"variant-debug": "debug",
		},
	})

	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d: %#v", len(got), got)
	}
	if got[0].Kind != OrderEntryKindSDK || got[0].Name != "Android API 34" {
		t.Fatalf("expected SDK entry first, got %#v", got[0])
	}
	if got[1].Kind != OrderEntryKindModule || got[1].ModulePath != ":lib" || got[1].Name != ":lib/debug" {
		t.Fatalf("expected deduplicated module entry, got %#v", got[1])
	}
	if got[2].Kind != OrderEntryKindLibrary || got[2].Name != "com.squareup.okhttp3:okhttp" {
		t.Fatalf("expected artifact-backed library entry, got %#v", got[2])
	}
	if got[2].Sources != "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0-sources.jar" || got[2].Javadoc != "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0-javadoc.jar" {
		t.Fatalf("expected inferred companion jars, got %#v", got[2])
	}
	if got[3].Kind != OrderEntryKindLibrary || got[3].Name != "build-logic" {
		t.Fatalf("expected generated classes entry to project as a library, got %#v", got[3])
	}
}

func TestClasspathSnapshotToOrderEntriesUsesToolchainFallback(t *testing.T) {
	snapshot := classpath.Snapshot{
		Scope:       classpath.ScopeRuntime,
		ToolchainID: "jvm-21",
		Entries: []classpath.Entry{
			{
				Path:           "/repo/out/runtime.jar",
				NormalizedPath: "/repo/out/runtime.jar",
				Origin:         classpath.OriginArtifact,
			},
		},
	}

	got := ClasspathSnapshotToOrderEntries(snapshot, ClasspathOrderEntryOptions{})
	if len(got) != 2 {
		t.Fatalf("expected SDK + library entries, got %d: %#v", len(got), got)
	}
	if got[0].Kind != OrderEntryKindSDK || got[0].Name != "jvm-21" {
		t.Fatalf("expected toolchain-based SDK fallback, got %#v", got[0])
	}
	if got[1].Kind != OrderEntryKindLibrary || got[1].Scope != "runtime" {
		t.Fatalf("expected runtime-scoped library entry, got %#v", got[1])
	}
}

func TestClasspathRecordToOrderEntriesIgnoresDocumentationArtifacts(t *testing.T) {
	record := classpath.Record{
		Scope: classpath.ScopeCompile,
		Entries: []classpath.EntryRecord{
			{
				Order:          0,
				Path:           "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0-sources.jar",
				NormalizedPath: "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0-sources.jar",
				Origin:         classpath.OriginArtifact,
			},
			{
				Order:          1,
				Path:           "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0-javadoc.jar",
				NormalizedPath: "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0-javadoc.jar",
				Origin:         classpath.OriginGenerated,
			},
			{
				Order:          2,
				Path:           "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0.jar",
				NormalizedPath: "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0.jar",
				Origin:         classpath.OriginArtifact,
			},
		},
	}

	got := ClasspathRecordToOrderEntries(record, ClasspathOrderEntryOptions{CompileSDK: "34"})
	if len(got) != 2 {
		t.Fatalf("expected sdk + binary library entries, got %d: %#v", len(got), got)
	}
	if got[1].Kind != OrderEntryKindLibrary || got[1].Classes != "/Users/jason/.gradle/caches/modules-2/files-2.1/okhttp-4.12.0.jar" {
		t.Fatalf("expected only the binary artifact to project as a library entry, got %#v", got[1])
	}
}

func TestClasspathRecordToOrderEntriesUsesExplicitCompanionArtifacts(t *testing.T) {
	record := classpath.Record{
		Scope: classpath.ScopeCompile,
		Entries: []classpath.EntryRecord{
			{
				Order:          0,
				Path:           "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-sources/okhttp-4.12.0-sources.jar",
				NormalizedPath: "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-sources/okhttp-4.12.0-sources.jar",
				Origin:         classpath.OriginArtifact,
				FamilyKey:      "com.squareup.okhttp3:okhttp",
			},
			{
				Order:          1,
				Path:           "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-javadoc/okhttp-4.12.0-javadoc.jar",
				NormalizedPath: "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-javadoc/okhttp-4.12.0-javadoc.jar",
				Origin:         classpath.OriginArtifact,
				FamilyKey:      "com.squareup.okhttp3:okhttp",
			},
			{
				Order:          2,
				Path:           "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-binary/okhttp-4.12.0.jar",
				NormalizedPath: "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-binary/okhttp-4.12.0.jar",
				Origin:         classpath.OriginArtifact,
				FamilyKey:      "com.squareup.okhttp3:okhttp",
			},
		},
	}

	got := ClasspathRecordToOrderEntries(record, ClasspathOrderEntryOptions{CompileSDK: "34"})
	if len(got) != 2 {
		t.Fatalf("expected sdk + binary library entries, got %d: %#v", len(got), got)
	}
	if got[1].Kind != OrderEntryKindLibrary || got[1].Classes != "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-binary/okhttp-4.12.0.jar" {
		t.Fatalf("expected only the binary artifact to project as a library entry, got %#v", got[1])
	}
	if got[1].Sources != "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-sources/okhttp-4.12.0-sources.jar" {
		t.Fatalf("expected explicit sources jar to override inferred convention, got %#v", got[1])
	}
	if got[1].Javadoc != "/Users/jason/.gradle/caches/modules-2/files-2.1/com.squareup.okhttp3/okhttp/4.12.0/hash-javadoc/okhttp-4.12.0-javadoc.jar" {
		t.Fatalf("expected explicit javadoc jar to override inferred convention, got %#v", got[1])
	}
}
