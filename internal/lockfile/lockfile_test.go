package lockfile

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
)

func TestCoordinateString(t *testing.T) {
	c := Coordinate{Group: "org.ex", Artifact: "lib", Version: "1.0"}
	if got := c.String(); got != "org.ex:lib:1.0" {
		t.Fatalf("unexpected: %s", got)
	}
	c.Classifier = "sources"
	if got := c.String(); got != "org.ex:lib:1.0:sources" {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestCanonicalizeSortsPinsByCoordinate(t *testing.T) {
	lf := Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Pins: []Pin{
			{Coordinate: Coordinate{Group: "z.lib", Artifact: "late", Version: "1"}, RepositoryID: "r"},
			{Coordinate: Coordinate{Group: "a.lib", Artifact: "early", Version: "1"}, RepositoryID: "r"},
		},
	}
	out := lf.Canonicalize()
	if out.Pins[0].Coordinate.Group != "a.lib" {
		t.Fatalf("pins not sorted: %+v", out.Pins)
	}
}

func TestCanonicalizeSortsFilesWithinPin(t *testing.T) {
	h := cas.HashBytes([]byte("x"))
	lf := Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Pins: []Pin{{
			Coordinate:   Coordinate{Group: "a.lib", Artifact: "b", Version: "1"},
			RepositoryID: "r",
			Files: []PinFile{
				{Kind: FileKindPrimary, Name: "b-1.jar", Hash: h},
				{Kind: FileKindPOM, Name: "b-1.pom", Hash: h},
			},
		}},
	}
	out := lf.Canonicalize()
	if out.Pins[0].Files[0].Kind != FileKindPOM {
		t.Fatalf("files not sorted by kind: %+v", out.Pins[0].Files)
	}
}

func TestCanonicalizeSortsDependenciesAndCapabilities(t *testing.T) {
	lf := Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Pins: []Pin{{
			Coordinate:   Coordinate{Group: "a.lib", Artifact: "b", Version: "1"},
			RepositoryID: "r",
			Dependencies: []Coordinate{
				{Group: "z.dep", Artifact: "late", Version: "1"},
				{Group: "a.dep", Artifact: "early", Version: "1"},
			},
			Capabilities: []string{"zeta", "alpha"},
		}},
	}
	out := lf.Canonicalize()
	if out.Pins[0].Dependencies[0].Group != "a.dep" {
		t.Fatalf("dependencies not sorted: %+v", out.Pins[0].Dependencies)
	}
	if out.Pins[0].Capabilities[0] != "alpha" {
		t.Fatalf("capabilities not sorted: %+v", out.Pins[0].Capabilities)
	}
}

func TestCanonicalizeCopiesAttributes(t *testing.T) {
	lf := Lockfile{
		SchemaVersion: 1,
		Pins: []Pin{{
			Coordinate:   Coordinate{Group: "a.lib", Artifact: "b", Version: "1"},
			RepositoryID: "r",
			Attributes:   map[string]string{"platform": "jvm"},
		}},
	}
	out := lf.Canonicalize()
	out.Pins[0].Attributes["platform"] = "androidJvm"

	if got := lf.Pins[0].Attributes["platform"]; got != "jvm" {
		t.Fatalf("canonicalized attributes alias original map: got %q", got)
	}
}

func TestEncodeDeterministic(t *testing.T) {
	lf := sampleLockfile()
	var a, b bytes.Buffer
	if err := lf.Encode(&a); err != nil {
		t.Fatalf("Encode a: %v", err)
	}
	if err := lf.Encode(&b); err != nil {
		t.Fatalf("Encode b: %v", err)
	}
	if a.String() != b.String() {
		t.Fatalf("encode not deterministic:\n%s\n---\n%s", a.String(), b.String())
	}
}

func TestEncodeStableAcrossPinPermutations(t *testing.T) {
	lf1 := sampleLockfile()
	lf2 := sampleLockfile()
	lf2.Pins[0], lf2.Pins[1] = lf2.Pins[1], lf2.Pins[0]

	var a, b bytes.Buffer
	if err := lf1.Encode(&a); err != nil {
		t.Fatalf("Encode lf1: %v", err)
	}
	if err := lf2.Encode(&b); err != nil {
		t.Fatalf("Encode lf2: %v", err)
	}
	if a.String() != b.String() {
		t.Fatalf("pin order leaked into canonical output")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	lf := sampleLockfile()
	var buf bytes.Buffer
	if err := lf.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Pins) != len(lf.Pins) {
		t.Fatalf("pin count mismatch: %d vs %d", len(decoded.Pins), len(lf.Pins))
	}
	// Pins are in canonicalized order after decode; find the alpha pin.
	var alpha *Pin
	for i := range decoded.Pins {
		if decoded.Pins[i].Coordinate.Artifact == "alpha" {
			alpha = &decoded.Pins[i]
		}
	}
	if alpha == nil {
		t.Fatalf("alpha pin missing")
	}
	if len(alpha.Files) == 0 || alpha.Files[0].Hash.IsZero() {
		t.Fatalf("file hash not preserved")
	}
	if !decoded.GeneratedAt.Equal(lf.GeneratedAt) {
		t.Fatalf("generatedAt not preserved")
	}
}

func TestDecodeRejectsMissingSchemaVersion(t *testing.T) {
	_, err := Decode(strings.NewReader(`{"generatedAt": "2026-04-01T00:00:00Z", "pins": []}`))
	if err == nil {
		t.Fatalf("expected error for missing schemaVersion")
	}
}

func TestDecodeRejectsFutureSchemaVersion(t *testing.T) {
	body := `{"schemaVersion": 999, "generatedAt": "2026-04-01T00:00:00Z", "pins": []}`
	_, err := Decode(strings.NewReader(body))
	if err == nil {
		t.Fatalf("expected error for future schemaVersion")
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	body := `{"schemaVersion": 1, "generatedAt": "2026-04-01T00:00:00Z", "pins": [], "surprise": 1}`
	_, err := Decode(strings.NewReader(body))
	if err == nil {
		t.Fatalf("expected error for unknown field")
	}
}

func sampleLockfile() Lockfile {
	h1 := cas.HashBytes([]byte("one"))
	h2 := cas.HashBytes([]byte("two"))
	return Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		GritVersion:   "test",
		Pins: []Pin{
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "alpha-1.0.jar", Size: 100, Hash: h1, URL: "https://ex/alpha-1.0.jar"},
					{Kind: FileKindPOM, Name: "alpha-1.0.pom", Size: 50, Hash: h2, URL: "https://ex/alpha-1.0.pom"},
				},
			},
			{
				Coordinate:   Coordinate{Group: "org.ex", Artifact: "beta", Version: "2.0"},
				RepositoryID: "central",
				Files: []PinFile{
					{Kind: FileKindPrimary, Name: "beta-2.0.jar", Size: 200, Hash: h2},
				},
				Dependencies: []Coordinate{
					{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
				},
				Capabilities: []string{"core"},
			},
		},
	}
}
