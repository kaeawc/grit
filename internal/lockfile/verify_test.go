package lockfile

import (
	"strings"
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
	if !result.Match || len(result.Issues) != 0 {
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
	joined := strings.Join(result.Issues, "\n")
	for _, want := range []string{
		"missing pin: org.ex:beta:2.0|central",
		"unexpected pin: org.ex:gamma:3.0|central",
		"hash mismatch for org.ex:alpha:1.0|central file primary|alpha-1.0.jar",
		"size mismatch for org.ex:alpha:1.0|central file primary|alpha-1.0.jar",
		"url mismatch for org.ex:alpha:1.0|central file primary|alpha-1.0.jar",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected issue containing %q, got %s", want, joined)
		}
	}
}
