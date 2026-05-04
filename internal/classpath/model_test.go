package classpath

import (
	"testing"

	"github.com/kaeawc/grit/internal/identity"
	"github.com/kaeawc/grit/internal/materialization"
)

func TestNormalizeDeduplicatesByFamilyAndPath(t *testing.T) {
	snapshot := Normalize(
		ScopeCompile,
		":app",
		"debug",
		"jvm-21",
		[]Entry{
			{
				Path:       "./libs/../libs/a.jar",
				Origin:     OriginArtifact,
				ArtifactID: "artifact-a",
				FamilyKey:  "androidx.collection",
				Provenance: materialization.Provenance{
					Producer: "resolver",
					Subject:  "artifact-a",
				},
			},
			{
				Path:       "/tmp/b.jar",
				Origin:     OriginArtifact,
				ArtifactID: "artifact-b",
				FamilyKey:  "androidx.collection",
				Provenance: materialization.Provenance{
					Producer: "resolver",
					Subject:  "artifact-b",
				},
			},
			{
				Path:            "/tmp/c.jar",
				Origin:          OriginSource,
				ModuleID:        ":app",
				SelectionReason: "local-source",
			},
			{
				Path:            "/tmp/c.jar",
				Origin:          OriginSource,
				ModuleID:        ":app",
				SelectionReason: "local-source",
			},
		},
		materialization.Provenance{
			Producer: "planner",
			Subject:  ":app@debug",
			Inputs: []materialization.Reference{
				{Kind: "artifact", ID: "artifact-b"},
				{Kind: "artifact", ID: "artifact-a"},
			},
		},
	)
	if got, want := len(snapshot.Entries), 2; got != want {
		t.Fatalf("unexpected normalized entry count: got %d want %d", got, want)
	}
	if got, want := snapshot.Entries[0].NormalizedPath, "libs/a.jar"; got != want {
		t.Fatalf("unexpected cleaned path: got %q want %q", got, want)
	}
	if got, want := len(snapshot.Decisions), 4; got != want {
		t.Fatalf("unexpected decision count: got %d want %d", got, want)
	}
	if !snapshot.Has("libs/a.jar") {
		t.Fatalf("expected snapshot to contain cleaned path")
	}
	if snapshot.Has("missing.jar") {
		t.Fatalf("did not expect missing path to be present")
	}
	dropped := 0
	hasDuplicateEntry := false
	hasFamilyCollapse := false
	for _, decision := range snapshot.Decisions {
		if decision.Dropped {
			dropped++
		}
		if decision.Dropped && decision.Reason == "duplicate-entry" {
			hasDuplicateEntry = true
		}
		if decision.Dropped && decision.FamilyKey == "androidx.collection" {
			hasFamilyCollapse = true
		}
	}
	if dropped != 2 {
		t.Fatalf("expected two dropped entries, got %d", dropped)
	}
	if !hasDuplicateEntry {
		t.Fatalf("expected duplicate entry decision")
	}
	if !hasFamilyCollapse {
		t.Fatalf("expected family collapse decision")
	}
}

func TestNormalizeFingerprintStableAcrossEquivalentInputs(t *testing.T) {
	base := []Entry{
		{
			Path:       "/tmp/a.jar",
			Origin:     OriginArtifact,
			ArtifactID: "artifact-a",
			FamilyKey:  "family-a",
			Provenance: materialization.Provenance{Producer: "resolver", Subject: "artifact-a"},
		},
		{
			Path:       "/tmp/b.jar",
			Origin:     OriginArtifact,
			ArtifactID: "artifact-b",
			FamilyKey:  "family-b",
			Provenance: materialization.Provenance{Producer: "resolver", Subject: "artifact-b"},
		},
	}
	a := Normalize(
		ScopeRuntime,
		":lib",
		"release",
		"jvm-21",
		base,
		materialization.Provenance{
			Producer: "planner",
			Subject:  ":lib@release",
			Reasons:  []string{"artifact-backed"},
			Inputs: []materialization.Reference{
				{Kind: "artifact", ID: "artifact-b"},
				{Kind: "artifact", ID: "artifact-a"},
			},
		},
	)
	b := Normalize(
		ScopeRuntime,
		":lib",
		"release",
		"jvm-21",
		base,
		materialization.Provenance{
			Producer: "planner",
			Subject:  ":lib@release",
			Reasons:  []string{"artifact-backed"},
			Inputs: []materialization.Reference{
				{Kind: "artifact", ID: "artifact-a"},
				{Kind: "artifact", ID: "artifact-b"},
			},
		},
	)
	if a.ID != b.ID {
		t.Fatalf("expected canonical fingerprint to ignore equivalent input ordering: %s != %s", a.ID, b.ID)
	}
}

func TestSnapshotRecordPreservesOrderedAndNormalizedIDs(t *testing.T) {
	snapshot := Normalize(
		ScopeCompile,
		":app",
		"freeDebug",
		"jvm-21",
		[]Entry{
			{
				Path:            "./src/freeDebug/../freeDebug",
				Origin:          OriginSource,
				ModuleID:        ":app",
				VariantID:       "freeDebug",
				SelectionReason: "semantic source root",
			},
			{
				Path:       "./libs/../libs/a.jar",
				Origin:     OriginArtifact,
				ArtifactID: "artifact-a",
				FamilyKey:  "artifact-a",
				Provenance: materialization.Provenance{
					Producer: "resolver",
					Subject:  "artifact-a",
				},
			},
		},
		materialization.Provenance{
			Producer: "semantic",
			Subject:  ":app@freeDebug",
		},
	)

	record := snapshot.Record()
	if got, want := record.ID, snapshot.ID; got != want {
		t.Fatalf("unexpected record snapshot id: got %q want %q", got, want)
	}
	if got, want := record.NormalizedEntries, []string{"src/freeDebug", "libs/a.jar"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected normalized entries: got %#v want %#v", got, want)
	}
	if got, want := record.NormalizedID, identity.NewClasspathSnapshotID("src/freeDebug", "libs/a.jar").String(); got != want {
		t.Fatalf("unexpected normalized id: got %q want %q", got, want)
	}
	if got, want := record.OrderedEntriesID, identity.NewClasspathSnapshotFromOrderedEntries("src/freeDebug", "libs/a.jar").String(); got != want {
		t.Fatalf("unexpected ordered entries id: got %q want %q", got, want)
	}
	if record.EntriesDigest == "" {
		t.Fatalf("expected record entries digest")
	}
	if got, want := len(record.Entries), 2; got != want {
		t.Fatalf("unexpected record entry count: got %d want %d", got, want)
	}
	for i, entry := range record.Entries {
		if entry.ID == "" {
			t.Fatalf("expected entry %d id", i)
		}
		if entry.Digest == "" {
			t.Fatalf("expected entry %d digest", i)
		}
		if entry.Order != i {
			t.Fatalf("unexpected entry order: got %d want %d", entry.Order, i)
		}
	}
}

func TestSnapshotRecordLookupUsesRawAndNormalizedPaths(t *testing.T) {
	snapshot := Normalize(
		ScopeRuntime,
		":lib",
		"release",
		"jvm-21",
		[]Entry{
			{
				Path:            "./build/../build/generated",
				Origin:          OriginGenerated,
				ModuleID:        ":lib",
				VariantID:       "release",
				SelectionReason: "generated runtime classpath",
			},
		},
		materialization.Provenance{Producer: "planner", Subject: ":lib@release"},
	)

	record := snapshot.Record()
	if !record.Has("build/generated") {
		t.Fatalf("expected normalized path lookup to succeed")
	}
	entry, ok := record.Lookup("./build/../build/generated")
	if !ok {
		t.Fatalf("expected raw path lookup to succeed")
	}
	if got, want := entry.NormalizedPath, "build/generated"; got != want {
		t.Fatalf("unexpected normalized path: got %q want %q", got, want)
	}
	if record.Has("missing") {
		t.Fatalf("did not expect missing path to be present")
	}
}

func TestSnapshotRecordOrderedIDChangesWithOrder(t *testing.T) {
	a := Normalize(
		ScopeCompile,
		":app",
		"debug",
		"semantic",
		[]Entry{
			{Path: "/tmp/a.jar", Origin: OriginArtifact, ArtifactID: "a", FamilyKey: "a"},
			{Path: "/tmp/b.jar", Origin: OriginArtifact, ArtifactID: "b", FamilyKey: "b"},
		},
		materialization.Provenance{Producer: "planner", Subject: ":app@debug"},
	).Record()
	b := Normalize(
		ScopeCompile,
		":app",
		"debug",
		"semantic",
		[]Entry{
			{Path: "/tmp/b.jar", Origin: OriginArtifact, ArtifactID: "b", FamilyKey: "b"},
			{Path: "/tmp/a.jar", Origin: OriginArtifact, ArtifactID: "a", FamilyKey: "a"},
		},
		materialization.Provenance{Producer: "planner", Subject: ":app@debug"},
	).Record()

	if a.NormalizedID != b.NormalizedID {
		t.Fatalf("expected normalized id to ignore ordering: %q != %q", a.NormalizedID, b.NormalizedID)
	}
	if a.OrderedEntriesID == b.OrderedEntriesID {
		t.Fatalf("expected ordered entries id to change with ordering")
	}
}
