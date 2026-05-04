package mavenlocal

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

type parsedPom struct {
	XMLName      xml.Name `xml:"project"`
	GroupID      string   `xml:"groupId"`
	ArtifactID   string   `xml:"artifactId"`
	Version      string   `xml:"version"`
	Packaging    string   `xml:"packaging"`
	Dependencies []struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
	} `xml:"dependencies>dependency"`
}

func TestGeneratedPomPayloadIncludesCoordinatesAndDependencies(t *testing.T) {
	payload, ok, err := generatedPomPayload(lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.2.3"},
		Dependencies: []lockfile.Coordinate{
			{Group: "org.dep", Artifact: "beta", Version: "2.0.0"},
			{Group: "org.dep", Artifact: "alpha", Version: "1.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("generatedPomPayload: %v", err)
	}
	if !ok {
		t.Fatalf("expected generated pom payload")
	}

	var got parsedPom
	if err := xml.Unmarshal(payload, &got); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if got.GroupID != "org.example" || got.ArtifactID != "demo" || got.Version != "1.2.3" {
		t.Fatalf("unexpected pom identity: %#v", got)
	}
	if got.Packaging != "jar" {
		t.Fatalf("unexpected packaging: %#v", got.Packaging)
	}
	if len(got.Dependencies) != 2 {
		t.Fatalf("unexpected dependencies: %#v", got.Dependencies)
	}
	if got.Dependencies[0].ArtifactID != "alpha" || got.Dependencies[1].ArtifactID != "beta" {
		t.Fatalf("dependencies not deterministically sorted: %#v", got.Dependencies)
	}
}

func TestGeneratedPomPayloadSkipsPinsWithoutDependencies(t *testing.T) {
	payload, ok, err := generatedPomPayload(lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("generatedPomPayload: %v", err)
	}
	if !ok {
		t.Fatalf("expected generated pom even with no dependencies")
	}
	var got parsedPom
	if err := xml.Unmarshal(payload, &got); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if len(got.Dependencies) != 0 {
		t.Fatalf("expected no dependencies, got %#v", got.Dependencies)
	}
}

func TestPublishPinWritesGeneratedPomWhenMissing(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()
	store := cas.NewFilesystemStore(casRoot)
	ctx := context.Background()

	info, err := store.PutBytes(ctx, []byte("jar body"), cas.Provenance{})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.0.jar", Hash: info.Hash},
		},
		Dependencies: []lockfile.Coordinate{
			{Group: "org.dep", Artifact: "alpha", Version: "1.0.0"},
		},
	}

	if err := New(root).PublishPin(ctx, pin, store); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}

	pomPath := filepath.Join(root, "org", "example", "demo", "1.0.0", "demo-1.0.0.pom")
	payload, err := os.ReadFile(pomPath)
	if err != nil {
		t.Fatalf("ReadFile pom: %v", err)
	}
	var got parsedPom
	if err := xml.Unmarshal(payload, &got); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if got.GroupID != "org.example" || got.ArtifactID != "demo" || got.Version != "1.0.0" {
		t.Fatalf("unexpected pom identity: %#v", got)
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0].ArtifactID != "alpha" {
		t.Fatalf("unexpected pom dependencies: %#v", got.Dependencies)
	}
	assertSidecar(t, pomPath, payload)
}

func TestPublishPinPreservesExplicitPomFile(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()
	store := cas.NewFilesystemStore(casRoot)
	ctx := context.Background()

	jarInfo, err := store.PutBytes(ctx, []byte("jar body"), cas.Provenance{})
	if err != nil {
		t.Fatalf("PutBytes jar: %v", err)
	}
	pomBytes := []byte(`<project><modelVersion>4.0.0</modelVersion><groupId>org.example</groupId><artifactId>demo</artifactId><version>1.0.0</version></project>`)
	pomInfo, err := store.PutBytes(ctx, pomBytes, cas.Provenance{})
	if err != nil {
		t.Fatalf("PutBytes pom: %v", err)
	}

	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "demo-1.0.0.jar", Hash: jarInfo.Hash},
			{Kind: lockfile.FileKindPOM, Name: "demo-1.0.0.pom", Hash: pomInfo.Hash},
		},
		Dependencies: []lockfile.Coordinate{
			{Group: "org.dep", Artifact: "alpha", Version: "1.0.0"},
		},
	}

	if err := New(root).PublishPin(ctx, pin, store); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}

	pomPath := filepath.Join(root, "org", "example", "demo", "1.0.0", "demo-1.0.0.pom")
	got, err := os.ReadFile(pomPath)
	if err != nil {
		t.Fatalf("ReadFile pom: %v", err)
	}
	if string(got) != string(pomBytes) {
		t.Fatalf("expected explicit pom bytes to be preserved, got %q", got)
	}
	assertSidecar(t, pomPath, got)
}

func TestGeneratedPomPayloadOrdersDependenciesDeterministically(t *testing.T) {
	payload, ok, err := generatedPomPayload(lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "demo", Version: "1.0.0"},
		Dependencies: []lockfile.Coordinate{
			{Group: "org.z", Artifact: "gamma", Version: "1.0.0"},
			{Group: "org.a", Artifact: "beta", Version: "1.0.0"},
			{Group: "org.a", Artifact: "alpha", Version: "1.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("generatedPomPayload: %v", err)
	}
	if !ok {
		t.Fatalf("expected generated pom payload")
	}
	var got parsedPom
	if err := xml.Unmarshal(payload, &got); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	names := []string{got.Dependencies[0].ArtifactID, got.Dependencies[1].ArtifactID, got.Dependencies[2].ArtifactID}
	if !slices.Equal(names, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("unexpected dependency order: %#v", names)
	}
}
