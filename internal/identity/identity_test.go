package identity

import "testing"

func TestKeyUsesLengthPrefixedParts(t *testing.T) {
	got := Key("ns", "a|b", "c:d")
	want := "ns|3:a|b|3:c:d"
	if got != want {
		t.Fatalf("unexpected key: got %q want %q", got, want)
	}
}

func TestSetKeyIsOrderIndependent(t *testing.T) {
	gotA := SetKey("set", "b", "a", "a")
	gotB := SetKey("set", "a", "b")
	want := "set|1:a|1:b"
	if gotA != want {
		t.Fatalf("unexpected set key: got %q want %q", gotA, want)
	}
	if gotB != want {
		t.Fatalf("unexpected set key: got %q want %q", gotB, want)
	}
}

func TestNormalizePathPartAndSnapshotIDs(t *testing.T) {
	got := NormalizePathPart(`  foo\bar/../baz  `)
	want := "foo/baz"
	if got != want {
		t.Fatalf("unexpected normalized path: got %q want %q", got, want)
	}

	snapshotA := NewClasspathSnapshotID(`foo\bar`, "baz")
	snapshotB := NewClasspathSnapshotID("baz", "foo/bar")
	if snapshotA != snapshotB {
		t.Fatalf("classpath snapshots should be order-independent: %q vs %q", snapshotA, snapshotB)
	}

	orderedA := NewClasspathSnapshotFromOrderedEntries("baz", "foo/bar")
	orderedB := NewClasspathSnapshotFromOrderedEntries("foo/bar", "baz")
	if orderedA == orderedB {
		t.Fatalf("ordered snapshots should preserve ordering: %q vs %q", orderedA, orderedB)
	}
}

func TestTypedIDConstructorsAreStable(t *testing.T) {
	moduleA := NewModuleID("  playground/app ")
	moduleB := NewModuleID("playground/app")
	if moduleA != moduleB {
		t.Fatalf("module ids should normalize logically: %q vs %q", moduleA, moduleB)
	}

	variantA := NewVariantID(moduleA, " debug ", "")
	variantB := NewVariantID(moduleB, "debug")
	if variantA != variantB {
		t.Fatalf("variant ids should normalize logically: %q vs %q", variantA, variantB)
	}

	artifactA := NewArtifactID(moduleA, variantA, " classes ", " jar ")
	artifactB := NewArtifactID(moduleB, variantB, "classes", "jar")
	if artifactA != artifactB {
		t.Fatalf("artifact ids should normalize logically: %q vs %q", artifactA, artifactB)
	}

	actionA := NewActionID(moduleA, variantA, " compile ", " source ")
	actionB := NewActionID(moduleB, variantB, "compile", "source")
	if actionA != actionB {
		t.Fatalf("action ids should normalize logically: %q vs %q", actionA, actionB)
	}

	materializationA := NewMaterializationID(moduleA, variantA, MaterializationSource, " src/main ")
	materializationB := NewMaterializationID(moduleB, variantB, MaterializationSource, "src/main")
	if materializationA != materializationB {
		t.Fatalf("materialization ids should normalize logically: %q vs %q", materializationA, materializationB)
	}
}
