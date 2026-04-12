package lockfile_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/lockfile/produce"
)

func TestVerifyMatchesFreshlyProducedLockfile(t *testing.T) {
	dir := t.TempDir()
	jarPath := filepath.Join(dir, "demo-1.0.jar")
	if err := os.WriteFile(jarPath, []byte("verify me"), 0o644); err != nil {
		t.Fatal(err)
	}

	expected := lockfile.Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		GritVersion:   "old",
		Pins: []lockfile.Pin{
			{
				Coordinate:   lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0"},
				RepositoryID: "central",
				Files: []lockfile.PinFile{
					{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.jar", Size: int64(len("verify me")), Hash: cas.HashBytes([]byte("verify me"))},
				},
			},
		},
	}

	produced, err := produce.Produce([]produce.Input{
		{
			Coordinate:   expected.Pins[0].Coordinate,
			RepositoryID: expected.Pins[0].RepositoryID,
			Files:        []produce.FileInput{{Kind: lockfile.FileKindPrimary, Path: jarPath}},
		},
	}, produce.Options{
		GeneratedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
		GritVersion: "new",
	})
	if err != nil {
		t.Fatalf("produce.Produce: %v", err)
	}

	result := lockfile.Verify(expected, produced)
	if !result.Match {
		t.Fatalf("expected match against freshly produced lockfile, got %#v", result)
	}
}

func TestVerifyReportsStructuredMismatchForFreshlyProducedLockfile(t *testing.T) {
	dir := t.TempDir()
	jarPath := filepath.Join(dir, "demo-1.0.jar")
	if err := os.WriteFile(jarPath, []byte("verify me"), 0o644); err != nil {
		t.Fatal(err)
	}

	expected := lockfile.Lockfile{
		SchemaVersion: 1,
		Pins: []lockfile.Pin{
			{
				Coordinate:   lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0"},
				RepositoryID: "central",
				Files: []lockfile.PinFile{
					{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.jar", Size: int64(len("verify me")), Hash: cas.HashBytes([]byte("verify me"))},
				},
			},
		},
	}

	produced, err := produce.Produce([]produce.Input{
		{
			Coordinate:   expected.Pins[0].Coordinate,
			RepositoryID: expected.Pins[0].RepositoryID,
			Files:        []produce.FileInput{{Kind: lockfile.FileKindPrimary, Path: jarPath}},
		},
	}, produce.Options{})
	if err != nil {
		t.Fatalf("produce.Produce: %v", err)
	}
	produced.Pins[0].Files[0].Hash = cas.HashBytes([]byte("different"))

	result := lockfile.Verify(expected, produced)
	if result.Match {
		t.Fatalf("expected mismatch, got %#v", result)
	}
	if len(result.Mismatches) != 1 {
		t.Fatalf("expected one mismatch, got %#v", result)
	}
	mismatch := result.Mismatches[0]
	if mismatch.Kind != lockfile.MismatchKindField || mismatch.Field != "hash" {
		t.Fatalf("expected hash field mismatch, got %#v", mismatch)
	}
	if mismatch.FileKind != lockfile.FileKindPrimary || mismatch.FileName != "demo-1.0.jar" {
		t.Fatalf("expected file identity in mismatch, got %#v", mismatch)
	}
	if mismatch.Expected == "" || mismatch.Actual == "" || mismatch.Expected == mismatch.Actual {
		t.Fatalf("expected distinct structured values, got %#v", mismatch)
	}
}
