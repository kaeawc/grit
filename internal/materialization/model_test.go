package materialization

import "testing"

func TestArtifactSnapshotFingerprintCanonicalizesInputs(t *testing.T) {
	a := NewArtifactSnapshot(
		":app",
		"debug",
		[]Reference{
			{Kind: "jar", ID: "b", Path: "/tmp/b.jar"},
			{Kind: "jar", ID: "a", Path: "/tmp/a.jar"},
		},
		[]Reference{
			{Kind: "input", ID: "dep-b"},
			{Kind: "input", ID: "dep-a"},
		},
		Provenance{
			Producer: "resolver",
			Subject:  ":app@debug",
			Inputs: []Reference{
				{Kind: "module", ID: ":app"},
				{Kind: "artifact", ID: "artifact-2"},
				{Kind: "artifact", ID: "artifact-1"},
			},
			Reasons: []string{"selected-artifact", "local-invalidations-absent"},
		},
	)
	b := NewArtifactSnapshot(
		":app",
		"debug",
		[]Reference{
			{Kind: "jar", ID: "a", Path: "/tmp/a.jar"},
			{Kind: "jar", ID: "b", Path: "/tmp/b.jar"},
		},
		[]Reference{
			{Kind: "input", ID: "dep-a"},
			{Kind: "input", ID: "dep-b"},
		},
		Provenance{
			Producer: "resolver",
			Subject:  ":app@debug",
			Inputs: []Reference{
				{Kind: "artifact", ID: "artifact-1"},
				{Kind: "artifact", ID: "artifact-2"},
				{Kind: "module", ID: ":app"},
			},
			Reasons: []string{"local-invalidations-absent", "selected-artifact"},
		},
	)
	if a.ID == "" {
		t.Fatalf("expected artifact snapshot id")
	}
	if a.ID != b.ID {
		t.Fatalf("expected canonical fingerprint to ignore input ordering: %s != %s", a.ID, b.ID)
	}
}

func TestMaterializationFingerprintIncludesBindingsAndSnapshots(t *testing.T) {
	m := NewMaterialization(
		":app",
		"release",
		ModeArtifactBacked,
		[]string{"/src/main", "/src/generated"},
		"snapshot-2",
		[]string{"classpath-2", "classpath-1"},
		[]BindingDecision{
			{
				EdgeID:                     "edge-2",
				UpstreamModuleID:           ":lib",
				UpstreamVariantID:          "release",
				SelectedMode:               ModeArtifactBacked,
				Reason:                     BindingReasonArtifactSnapshot,
				Detail:                     "selected published snapshot",
				SelectedArtifactSnapshotID: "snapshot-2",
			},
			{
				EdgeID:                     "edge-1",
				UpstreamModuleID:           ":util",
				UpstreamVariantID:          "release",
				SelectedMode:               ModeSourceBacked,
				Reason:                     BindingReasonLocalInvalidation,
				LocalInvalidation:          true,
				Detail:                     "local edits override artifact reuse",
				SelectedArtifactSnapshotID: "snapshot-1",
			},
		},
		Provenance{
			Producer: "planner",
			Subject:  ":app@release",
			Inputs: []Reference{
				{Kind: "classpath-snapshot", ID: "classpath-2"},
				{Kind: "classpath-snapshot", ID: "classpath-1"},
			},
			Reasons: []string{"artifact-backed", "local-invalidations-checked"},
		},
	)
	if !m.IsArtifactBacked() {
		t.Fatalf("expected artifact-backed materialization")
	}
	if m.ID == "" {
		t.Fatalf("expected materialization id")
	}
	if got, want := len(m.ClasspathSnapshotIDs), 2; got != want {
		t.Fatalf("unexpected classpath snapshot count: got %d want %d", got, want)
	}
	again := NewMaterialization(
		":app",
		"release",
		ModeArtifactBacked,
		[]string{"/src/generated", "/src/main"},
		"snapshot-2",
		[]string{"classpath-1", "classpath-2"},
		[]BindingDecision{
			{
				EdgeID:                     "edge-1",
				UpstreamModuleID:           ":util",
				UpstreamVariantID:          "release",
				SelectedMode:               ModeSourceBacked,
				Reason:                     BindingReasonLocalInvalidation,
				LocalInvalidation:          true,
				Detail:                     "local edits override artifact reuse",
				SelectedArtifactSnapshotID: "snapshot-1",
			},
			{
				EdgeID:                     "edge-2",
				UpstreamModuleID:           ":lib",
				UpstreamVariantID:          "release",
				SelectedMode:               ModeArtifactBacked,
				Reason:                     BindingReasonArtifactSnapshot,
				Detail:                     "selected published snapshot",
				SelectedArtifactSnapshotID: "snapshot-2",
			},
		},
		Provenance{
			Producer: "planner",
			Subject:  ":app@release",
			Inputs: []Reference{
				{Kind: "classpath-snapshot", ID: "classpath-1"},
				{Kind: "classpath-snapshot", ID: "classpath-2"},
			},
			Reasons: []string{"local-invalidations-checked", "artifact-backed"},
		},
	)
	if m.ID != again.ID {
		t.Fatalf("expected canonical fingerprint to ignore ordering: %s != %s", m.ID, again.ID)
	}
}

func TestMaterializationWithClasspathSnapshotsCanonicalizesAndIndexes(t *testing.T) {
	m := NewMaterialization(
		":app",
		"debug",
		ModeSourceBacked,
		[]string{"/repo/app/src/main"},
		"artifact-snapshot",
		nil,
		nil,
		Provenance{Producer: "planner", Subject: ":app@debug"},
	).WithClasspathSnapshots([]ClasspathSnapshotReference{
		{
			ID:               "cp-b",
			NormalizedID:     "normalized-b",
			OrderedEntriesID: "ordered-b",
			EntriesDigest:    "digest-b",
			Entries:          []string{"/tmp/b", "/tmp/a"},
		},
		{
			ID:               "cp-a",
			NormalizedID:     "normalized-a",
			OrderedEntriesID: "ordered-a",
			EntriesDigest:    "digest-a",
			Entries:          []string{"/tmp/d", "/tmp/c"},
		},
	})

	if got, want := m.ClasspathSnapshotIDs, []string{"cp-a", "cp-b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected classpath snapshot ids: got %#v want %#v", got, want)
	}
	if got, want := len(m.ClasspathSnapshots), 2; got != want {
		t.Fatalf("unexpected classpath snapshot ref count: got %d want %d", got, want)
	}
	if got, want := m.ClasspathSnapshots[0].Entries, []string{"/tmp/c", "/tmp/d"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected canonicalized entries: got %#v want %#v", got, want)
	}
	if got, want := m.ClasspathSnapshots[0].EntryCount, 2; got != want {
		t.Fatalf("unexpected entry count: got %d want %d", got, want)
	}
	ref, ok := m.ClasspathSnapshot("cp-b")
	if !ok {
		t.Fatalf("expected classpath snapshot lookup")
	}
	if got, want := ref.OrderedEntriesID, "ordered-b"; got != want {
		t.Fatalf("unexpected classpath snapshot ref: %#v", ref)
	}
}

func TestMaterializationClasspathSnapshotCopiesEntries(t *testing.T) {
	m := NewMaterialization(
		":app",
		"debug",
		ModeSourceBacked,
		nil,
		"",
		nil,
		nil,
		Provenance{},
	).WithClasspathSnapshots([]ClasspathSnapshotReference{
		{ID: "cp", Entries: []string{"/tmp/a", "/tmp/b"}},
	})

	ref, ok := m.ClasspathSnapshot("cp")
	if !ok {
		t.Fatal("expected classpath snapshot lookup")
	}
	ref.Entries[0] = "/tmp/mutated"

	refAgain, ok := m.ClasspathSnapshot("cp")
	if !ok {
		t.Fatal("expected second classpath snapshot lookup")
	}
	if got := refAgain.Entries[0]; got != "/tmp/a" {
		t.Fatalf("lookup result entries alias stored materialization: got %q", got)
	}
}
