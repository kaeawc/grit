package lockfile

import (
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
)

func TestVerifyIgnoresTimestampAndOrderingNoise(t *testing.T) {
	h1 := cas.HashBytes([]byte("one"))
	h2 := cas.HashBytes([]byte("two"))

	expected := Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
		GritVersion:   "alpha",
		Pins: []Pin{
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "beta", Version: "2.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "beta-2.0.jar", Size: 2, Hash: h2},
				},
				Capabilities: []string{"core", "optional"},
			},
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPOM, Name: "alpha-1.0.pom", Size: 1, Hash: h1},
					{Kind: FileKindPrimary, Name: "alpha-1.0.jar", Size: 1, Hash: h1},
				},
			},
		},
	}
	actual := Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
		GritVersion:   "beta",
		Pins: []Pin{
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "alpha-1.0.jar", Size: 1, Hash: h1},
					{Kind: FileKindPOM, Name: "alpha-1.0.pom", Size: 1, Hash: h1},
				},
			},
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "beta", Version: "2.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "beta-2.0.jar", Size: 2, Hash: h2},
				},
				Capabilities: []string{"optional", "core"},
			},
		},
	}

	result := Verify(expected, actual)
	if !result.Match || len(result.Mismatches) != 0 {
		t.Fatalf("expected semantic match, got %#v", result)
	}
}

func TestVerifyReportsMissingUnexpectedAndDriftedPins(t *testing.T) {
	h1 := cas.HashBytes([]byte("one"))
	h2 := cas.HashBytes([]byte("two"))
	h3 := cas.HashBytes([]byte("three"))

	expected := Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
		Pins: []Pin{
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "alpha-1.0.jar", Size: 1, Hash: h1, URL: "https://example/alpha-1.0.jar"},
				},
			},
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "beta", Version: "2.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "beta-2.0.jar", Size: 2, Hash: h2},
				},
			},
		},
	}
	actual := Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
		Pins: []Pin{
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "alpha-1.0.jar", Size: 99, Hash: h3, URL: "https://mirror/alpha-1.0.jar"},
				},
			},
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "gamma", Version: "3.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "gamma-3.0.jar", Size: 3, Hash: h3},
				},
			},
		},
	}

	result := Verify(expected, actual)
	if result.Match {
		t.Fatalf("expected mismatch, got %#v", result)
	}
	if len(result.Mismatches) != 5 {
		t.Fatalf("expected 5 mismatches, got %#v", result)
	}
	assertMismatch := func(kind MismatchKind, field string) Mismatch {
		t.Helper()
		for _, mismatch := range result.Mismatches {
			if mismatch.Kind == kind && mismatch.Field == field {
				return mismatch
			}
		}
		t.Fatalf("expected mismatch kind %q field %q, got %#v", kind, field, result.Mismatches)
		return Mismatch{}
	}
	missing := assertMismatch(MismatchKindMissingPin, "")
	if missing.Coordinate != (Coordinate{Group: "org.ex", Artifact: "beta", Version: "2.0"}) || missing.RepositoryID != "central" {
		t.Fatalf("unexpected missing pin mismatch: %#v", missing)
	}
	unexpected := assertMismatch(MismatchKindUnexpectedPin, "")
	if unexpected.Coordinate != (Coordinate{Group: "org.ex", Artifact: "gamma", Version: "3.0"}) || unexpected.RepositoryID != "central" {
		t.Fatalf("unexpected unexpected pin mismatch: %#v", unexpected)
	}
	hash := assertMismatch(MismatchKindField, "hash")
	if hash.FileKind != FileKindPrimary || hash.FileName != "alpha-1.0.jar" {
		t.Fatalf("unexpected hash mismatch: %#v", hash)
	}
	size := assertMismatch(MismatchKindField, "size")
	if size.Expected != "1" || size.Actual != "99" {
		t.Fatalf("unexpected size mismatch values: %#v", size)
	}
	url := assertMismatch(MismatchKindField, "url")
	if url.Expected != "https://example/alpha-1.0.jar" || url.Actual != "https://mirror/alpha-1.0.jar" {
		t.Fatalf("unexpected url mismatch values: %#v", url)
	}
}
