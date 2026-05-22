package dependencywiring

import (
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kaeawc/grit/internal/buildprogress"
	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/depcache"
	"github.com/kaeawc/grit/internal/lockfile"
	mavenpublish "github.com/kaeawc/grit/internal/publish/mavenlocal"
)

// TestMaterializePinsBatchesArtifactMetadataAcrossVersions verifies the
// two-phase materializer writes maven-metadata-local.xml once per
// (group, artifact) with every batched version, even when multiple
// versions of the same artifact are materialized concurrently. Pre-2026
// the publisher rewrote metadata once per pin while holding a per-(group,
// artifact) mutex around Stack.Publish; this test pins down the new
// behavior where the metadata file is written exactly once per artifact
// in the batch.
func TestMaterializePinsBatchesArtifactMetadataAcrossVersions(t *testing.T) {
	casRoot := t.TempDir()
	publishRoot := t.TempDir()
	store := cas.NewFilesystemStore(casRoot)
	ctx := context.Background()

	// Three versions of the same (group, artifact) plus a sibling artifact
	// to make sure the per-artifact grouping is real and not accidental.
	versions := []string{"1.0.0", "1.5.0", "2.0.0"}
	pins := make([]lockfile.Pin, 0, len(versions)+1)
	for _, version := range versions {
		info, err := store.PutBytes(ctx, []byte("jar "+version), cas.Provenance{})
		if err != nil {
			t.Fatalf("PutBytes %s: %v", version, err)
		}
		pins = append(pins, lockfile.Pin{
			Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: version},
			Files: []lockfile.PinFile{
				{Kind: lockfile.FileKindPrimary, Name: "demo-" + version + ".jar", Hash: info.Hash},
			},
		})
	}
	siblingInfo, err := store.PutBytes(ctx, []byte("sibling jar"), cas.Provenance{})
	if err != nil {
		t.Fatalf("PutBytes sibling: %v", err)
	}
	pins = append(pins, lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "other", Version: "1.0.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "other-1.0.0.jar", Hash: siblingInfo.Hash},
		},
	})

	m := &stackMaterializer{
		stack: &depcache.Stack{
			Store:     store,
			Publisher: mavenpublish.New(publishRoot),
		},
	}

	progress := buildprogress.NewReporter(io.Discard, false)
	got, err := m.materializePins(ctx, pins, progress)
	if err != nil {
		t.Fatalf("materializePins: %v", err)
	}
	if len(got) != len(pins) {
		t.Fatalf("pinsByCoordinate has %d entries, want %d", len(got), len(pins))
	}

	// Every version directory and jar exists.
	for _, version := range versions {
		jarPath := filepath.Join(publishRoot, "org", "example", "demo", version, "demo-"+version+".jar")
		if _, err := os.Stat(jarPath); err != nil {
			t.Fatalf("missing jar %s: %v", jarPath, err)
		}
	}

	// demo/maven-metadata-local.xml lists all three versions in one file.
	demoMetadataPath := filepath.Join(publishRoot, "org", "example", "demo", "maven-metadata-local.xml")
	demoVersions := readMetadataVersions(t, demoMetadataPath)
	if !slices.Equal(demoVersions, []string{"1.0.0", "1.5.0", "2.0.0"}) {
		t.Fatalf("demo metadata versions: got %v want [1.0.0 1.5.0 2.0.0]", demoVersions)
	}

	// The sibling artifact got its own metadata file with just its version.
	otherMetadataPath := filepath.Join(publishRoot, "org", "example", "other", "maven-metadata-local.xml")
	otherVersions := readMetadataVersions(t, otherMetadataPath)
	if !slices.Equal(otherVersions, []string{"1.0.0"}) {
		t.Fatalf("other metadata versions: got %v want [1.0.0]", otherVersions)
	}
}

func readMetadataVersions(t *testing.T, path string) []string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	var parsed struct {
		Versioning struct {
			Versions []string `xml:"versions>version"`
		} `xml:"versioning"`
	}
	if err := xml.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("unmarshal metadata at %s: %v", path, err)
	}
	return parsed.Versioning.Versions
}
