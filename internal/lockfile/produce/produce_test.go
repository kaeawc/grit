package produce_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/lockfile/produce"
)

func TestProduceHashesFileFromDisk(t *testing.T) {
	dir := t.TempDir()
	body := []byte("test jar body")
	jarPath := writeFile(t, dir, "alpha-1.0.jar", body)

	lf, err := produce.Produce([]produce.Input{
		{
			Coordinate:   lockfile.Coordinate{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
			RepositoryID: "test",
			Files: []produce.FileInput{
				{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Path: jarPath},
			},
		},
	}, produce.Options{
		GeneratedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
		GritVersion: "test",
	})
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	if lf.SchemaVersion != lockfile.CurrentSchemaVersion {
		t.Fatalf("schema version: %d", lf.SchemaVersion)
	}
	if lf.GritVersion != "test" {
		t.Fatalf("grit version not preserved")
	}
	if !lf.GeneratedAt.Equal(time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("generated at mismatch")
	}
	if len(lf.Pins) != 1 {
		t.Fatalf("pin count: %d", len(lf.Pins))
	}
	pin := lf.Pins[0]
	if pin.Coordinate.Artifact != "alpha" {
		t.Fatalf("coordinate lost")
	}
	if pin.RepositoryID != "test" {
		t.Fatalf("repository lost")
	}
	if len(pin.Files) != 1 {
		t.Fatalf("file count: %d", len(pin.Files))
	}
	if pin.Files[0].Hash != cas.HashBytes(body) {
		t.Fatalf("hash mismatch: got %s want %s", pin.Files[0].Hash, cas.HashBytes(body))
	}
	if pin.Files[0].Size != int64(len(body)) {
		t.Fatalf("size mismatch: got %d want %d", pin.Files[0].Size, len(body))
	}
}

func TestProduceNameDefaultsToBasename(t *testing.T) {
	dir := t.TempDir()
	body := []byte("x")
	path := writeFile(t, dir, "default-name.jar", body)

	lf, err := produce.Produce([]produce.Input{
		{
			Coordinate: lockfile.Coordinate{Group: "g", Artifact: "a", Version: "1"},
			Files: []produce.FileInput{
				{Kind: lockfile.FileKindPrimary, Path: path}, // Name omitted
			},
		},
	}, produce.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if lf.Pins[0].Files[0].Name != "default-name.jar" {
		t.Fatalf("name not defaulted to basename: %q", lf.Pins[0].Files[0].Name)
	}
}

func TestProduceRejectsEmptyInputs(t *testing.T) {
	if _, err := produce.Produce(nil, produce.Options{}); err == nil {
		t.Fatalf("expected error for empty inputs")
	}
}

func TestProduceRejectsIncompleteCoordinate(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "x.jar", []byte("x"))

	cases := []lockfile.Coordinate{
		{Group: "", Artifact: "a", Version: "1"},
		{Group: "g", Artifact: "", Version: "1"},
		{Group: "g", Artifact: "a", Version: ""},
	}
	for _, coord := range cases {
		_, err := produce.Produce([]produce.Input{
			{
				Coordinate: coord,
				Files:      []produce.FileInput{{Kind: lockfile.FileKindPrimary, Path: path}},
			},
		}, produce.Options{})
		if err == nil {
			t.Fatalf("expected error for coord %+v", coord)
		}
	}
}

func TestProduceRejectsMissingFile(t *testing.T) {
	_, err := produce.Produce([]produce.Input{
		{
			Coordinate: lockfile.Coordinate{Group: "g", Artifact: "a", Version: "1"},
			Files: []produce.FileInput{
				{Kind: lockfile.FileKindPrimary, Name: "gone.jar", Path: "/definitely/not/here"},
			},
		},
	}, produce.Options{})
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestProduceRejectsDirectoryAsFile(t *testing.T) {
	dir := t.TempDir()
	_, err := produce.Produce([]produce.Input{
		{
			Coordinate: lockfile.Coordinate{Group: "g", Artifact: "a", Version: "1"},
			Files: []produce.FileInput{
				{Kind: lockfile.FileKindPrimary, Name: "dir.jar", Path: dir},
			},
		},
	}, produce.Options{})
	if err == nil {
		t.Fatalf("expected error for directory input")
	}
}

func TestProduceRejectsEmptyFileList(t *testing.T) {
	_, err := produce.Produce([]produce.Input{
		{
			Coordinate: lockfile.Coordinate{Group: "g", Artifact: "a", Version: "1"},
		},
	}, produce.Options{})
	if err == nil {
		t.Fatalf("expected error for input with no files")
	}
}

func TestProduceCanonicalizesPinOrder(t *testing.T) {
	dir := t.TempDir()
	aPath := writeFile(t, dir, "a-1.jar", []byte("a"))
	bPath := writeFile(t, dir, "b-1.jar", []byte("b"))

	// Pins provided in reverse coordinate order.
	lf, err := produce.Produce([]produce.Input{
		{
			Coordinate: lockfile.Coordinate{Group: "org.ex", Artifact: "zeta", Version: "1.0"},
			Files:      []produce.FileInput{{Kind: lockfile.FileKindPrimary, Name: "b-1.jar", Path: bPath}},
		},
		{
			Coordinate: lockfile.Coordinate{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
			Files:      []produce.FileInput{{Kind: lockfile.FileKindPrimary, Name: "a-1.jar", Path: aPath}},
		},
	}, produce.Options{GeneratedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if lf.Pins[0].Coordinate.Artifact != "alpha" {
		t.Fatalf("pins not canonicalized: %+v", lf.Pins)
	}
}

func TestProduceMultipleFilesPerPin(t *testing.T) {
	dir := t.TempDir()
	jarPath := writeFile(t, dir, "multi-3.0.jar", []byte("jar body"))
	pomPath := writeFile(t, dir, "multi-3.0.pom", []byte("<pom/>"))

	lf, err := produce.Produce([]produce.Input{
		{
			Coordinate:   lockfile.Coordinate{Group: "org.ex", Artifact: "multi", Version: "3.0"},
			RepositoryID: "central",
			Files: []produce.FileInput{
				{Kind: lockfile.FileKindPrimary, Name: "multi-3.0.jar", Path: jarPath},
				{Kind: lockfile.FileKindPOM, Name: "multi-3.0.pom", Path: pomPath},
			},
		},
	}, produce.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Pins[0].Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(lf.Pins[0].Files))
	}
}

func TestProducePreservesMetadata(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "meta-1.jar", []byte("meta"))

	lf, err := produce.Produce([]produce.Input{
		{
			Coordinate:   lockfile.Coordinate{Group: "g", Artifact: "meta", Version: "1"},
			RepositoryID: "central",
			Files: []produce.FileInput{
				{Kind: lockfile.FileKindPrimary, Name: "meta-1.jar", Path: path, URL: "https://example/meta-1.jar"},
			},
			Attributes:   map[string]string{"platform": "jvm"},
			Capabilities: []string{"core", "optional"},
			Dependencies: []lockfile.Coordinate{
				{Group: "g", Artifact: "dep", Version: "2"},
			},
		},
	}, produce.Options{})
	if err != nil {
		t.Fatal(err)
	}
	pin := lf.Pins[0]
	if pin.Files[0].URL != "https://example/meta-1.jar" {
		t.Fatalf("URL lost: %s", pin.Files[0].URL)
	}
	if pin.Attributes["platform"] != "jvm" {
		t.Fatalf("attributes lost")
	}
	// Capabilities are sorted by canonicalization.
	if len(pin.Capabilities) != 2 || pin.Capabilities[0] != "core" || pin.Capabilities[1] != "optional" {
		t.Fatalf("capabilities not preserved and sorted: %+v", pin.Capabilities)
	}
	if len(pin.Dependencies) != 1 || pin.Dependencies[0].Artifact != "dep" {
		t.Fatalf("dependencies not preserved")
	}
}

func TestProduceEncodeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "rt-1.0.jar", []byte("round trip body"))

	lf, err := produce.Produce([]produce.Input{
		{
			Coordinate:   lockfile.Coordinate{Group: "org.ex", Artifact: "rt", Version: "1.0"},
			RepositoryID: "test",
			Files: []produce.FileInput{
				{Kind: lockfile.FileKindPrimary, Name: "rt-1.0.jar", Path: path, URL: "https://example/rt-1.0.jar"},
			},
		},
	}, produce.Options{
		GeneratedAt: time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		GritVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := lf.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := lockfile.Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Pins) != 1 {
		t.Fatalf("pin count")
	}
	if decoded.Pins[0].Files[0].Hash != lf.Pins[0].Files[0].Hash {
		t.Fatalf("hash did not survive round trip")
	}
	if decoded.Pins[0].Files[0].URL != "https://example/rt-1.0.jar" {
		t.Fatalf("URL not preserved")
	}
}

func TestProduceDeterministicAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "det.jar", []byte("deterministic"))

	inputs := []produce.Input{
		{
			Coordinate: lockfile.Coordinate{Group: "g", Artifact: "det", Version: "1"},
			Files:      []produce.FileInput{{Kind: lockfile.FileKindPrimary, Name: "det-1.jar", Path: path}},
		},
	}
	opts := produce.Options{
		GeneratedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
	}

	lf1, err := produce.Produce(inputs, opts)
	if err != nil {
		t.Fatal(err)
	}
	lf2, err := produce.Produce(inputs, opts)
	if err != nil {
		t.Fatal(err)
	}

	var a, b bytes.Buffer
	if err := lf1.Encode(&a); err != nil {
		t.Fatal(err)
	}
	if err := lf2.Encode(&b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatalf("Produce not deterministic")
	}
}

func TestProduceAttributesAreDefensiveCopy(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.jar", []byte("a"))

	attrs := map[string]string{"k": "v1"}
	lf, err := produce.Produce([]produce.Input{
		{
			Coordinate: lockfile.Coordinate{Group: "g", Artifact: "a", Version: "1"},
			Files:      []produce.FileInput{{Kind: lockfile.FileKindPrimary, Name: "a.jar", Path: path}},
			Attributes: attrs,
		},
	}, produce.Options{})
	if err != nil {
		t.Fatal(err)
	}
	attrs["k"] = "MUTATED"
	if lf.Pins[0].Attributes["k"] != "v1" {
		t.Fatalf("caller mutation leaked into lockfile: %q", lf.Pins[0].Attributes["k"])
	}
}

func writeFile(t *testing.T, dir, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
