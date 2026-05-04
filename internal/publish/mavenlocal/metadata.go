package mavenlocal

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	readadapter "github.com/kaeawc/grit/internal/downloader/mavenlocal"

	"github.com/kaeawc/grit/internal/lockfile"
)

type artifactMetadata struct {
	XMLName    xml.Name                 `xml:"metadata"`
	GroupID    string                   `xml:"groupId"`
	ArtifactID string                   `xml:"artifactId"`
	Versioning artifactMetadataVersions `xml:"versioning"`
}

type artifactMetadataVersions struct {
	Latest   string   `xml:"latest,omitempty"`
	Release  string   `xml:"release,omitempty"`
	Versions []string `xml:"versions>version,omitempty"`
}

func (p *Publisher) publishArtifactMetadata(coord lockfile.Coordinate) error {
	artifactDir := filepath.Join(p.root, readadapter.GroupPath(coord.Group), coord.Artifact)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	metadataPath := filepath.Join(artifactDir, "maven-metadata-local.xml")
	existing, err := os.ReadFile(metadataPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payload, err := mergeArtifactMetadata(existing, coord)
	if err != nil {
		return err
	}
	return writeBytesWithSidecars(metadataPath, payload)
}

func mergeArtifactMetadata(existing []byte, coord lockfile.Coordinate) ([]byte, error) {
	metadata, err := decodeArtifactMetadata(existing)
	if err != nil {
		return nil, err
	}
	if metadata.GroupID != "" && metadata.GroupID != coord.Group {
		return nil, fmt.Errorf("metadata group mismatch: %q != %q", metadata.GroupID, coord.Group)
	}
	if metadata.ArtifactID != "" && metadata.ArtifactID != coord.Artifact {
		return nil, fmt.Errorf("metadata artifact mismatch: %q != %q", metadata.ArtifactID, coord.Artifact)
	}
	metadata.GroupID = coord.Group
	metadata.ArtifactID = coord.Artifact

	versionSet := map[string]struct{}{}
	for _, version := range metadata.Versioning.Versions {
		if version == "" {
			continue
		}
		versionSet[version] = struct{}{}
	}
	if coord.Version != "" {
		versionSet[coord.Version] = struct{}{}
	}
	versions := make([]string, 0, len(versionSet))
	for version := range versionSet {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	metadata.Versioning.Versions = versions
	if len(versions) != 0 {
		metadata.Versioning.Latest = versions[len(versions)-1]
		metadata.Versioning.Release = versions[len(versions)-1]
	}

	payload, err := xml.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(append([]byte(xml.Header), payload...), '\n'), nil
}

func decodeArtifactMetadata(existing []byte) (artifactMetadata, error) {
	if len(bytes.TrimSpace(existing)) == 0 {
		return artifactMetadata{}, nil
	}
	var metadata artifactMetadata
	if err := xml.Unmarshal(existing, &metadata); err != nil {
		return artifactMetadata{}, err
	}
	return metadata, nil
}
