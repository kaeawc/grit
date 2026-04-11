package mavenlocal

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

func TestMergeArtifactMetadataAddsVersionAndUpdatesLatestRelease(t *testing.T) {
	existing := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>org.example</groupId>
  <artifactId>demo</artifactId>
  <versioning>
    <latest>1.0.0</latest>
    <release>1.0.0</release>
    <versions>
      <version>1.0.0</version>
    </versions>
  </versioning>
</metadata>
`)

	payload, err := mergeArtifactMetadata(existing, lockfile.Coordinate{
		Group:    "org.example",
		Artifact: "demo",
		Version:  "2.0.0",
	})
	if err != nil {
		t.Fatalf("mergeArtifactMetadata: %v", err)
	}

	metadata, err := decodeArtifactMetadata(payload)
	if err != nil {
		t.Fatalf("decodeArtifactMetadata: %v", err)
	}
	if metadata.GroupID != "org.example" || metadata.ArtifactID != "demo" {
		t.Fatalf("unexpected metadata identity: %#v", metadata)
	}
	if !slices.Equal(metadata.Versioning.Versions, []string{"1.0.0", "2.0.0"}) {
		t.Fatalf("unexpected versions: %#v", metadata.Versioning.Versions)
	}
	if metadata.Versioning.Latest != "2.0.0" || metadata.Versioning.Release != "2.0.0" {
		t.Fatalf("unexpected latest/release: %#v", metadata.Versioning)
	}
}

func TestMergeArtifactMetadataRejectsCoordinateMismatch(t *testing.T) {
	existing := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>org.example</groupId>
  <artifactId>other</artifactId>
</metadata>
`)
	_, err := mergeArtifactMetadata(existing, lockfile.Coordinate{
		Group:    "org.example",
		Artifact: "demo",
		Version:  "1.0.0",
	})
	if err == nil {
		t.Fatalf("expected mismatch error")
	}
}

func TestPublishPinUpdatesArtifactMetadataAcrossVersions(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()
	store := cas.NewFilesystemStore(casRoot)
	ctx := context.Background()
	info, err := store.PutBytes(ctx, []byte("jar body"), cas.Provenance{})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	p := New(root)
	if err := p.PublishPin(ctx, lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.0.jar", Hash: info.Hash},
		},
	}, store); err != nil {
		t.Fatalf("PublishPin 1.0.0: %v", err)
	}
	if err := p.PublishPin(ctx, lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "2.0.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "demo-2.0.0.jar", Hash: info.Hash},
		},
	}, store); err != nil {
		t.Fatalf("PublishPin 2.0.0: %v", err)
	}

	metadataPath := filepath.Join(root, "org", "example", "demo", "maven-metadata-local.xml")
	payload, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("ReadFile metadata: %v", err)
	}
	metadata, err := decodeArtifactMetadata(payload)
	if err != nil {
		t.Fatalf("decodeArtifactMetadata: %v", err)
	}
	if !slices.Equal(metadata.Versioning.Versions, []string{"1.0.0", "2.0.0"}) {
		t.Fatalf("unexpected versions after repeated publish: %#v", metadata.Versioning.Versions)
	}
	if metadata.Versioning.Latest != "2.0.0" || metadata.Versioning.Release != "2.0.0" {
		t.Fatalf("unexpected latest/release after repeated publish: %#v", metadata.Versioning)
	}
	assertSidecar(t, metadataPath, payload)
}
