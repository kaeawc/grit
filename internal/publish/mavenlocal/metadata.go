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

// PublishArtifactMetadataVersions rewrites maven-metadata-local.xml for
// (group, artifact) so it lists every version in versions in addition to
// any versions the existing file already names.
func (p *Publisher) PublishArtifactMetadataVersions(group, artifact string, versions []string) error {
	artifactDir := filepath.Join(p.root, readadapter.GroupPath(group), artifact)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	metadataPath := filepath.Join(artifactDir, "maven-metadata-local.xml")
	existing, err := os.ReadFile(metadataPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	payload, err := mergeArtifactMetadataVersions(existing, group, artifact, versions)
	if err != nil {
		return err
	}
	return writeBytesWithSidecars(metadataPath, payload)
}

func mergeArtifactMetadataVersions(existing []byte, group, artifact string, versions []string) ([]byte, error) {
	metadata, err := decodeArtifactMetadata(existing)
	if err != nil {
		return nil, err
	}
	if metadata.GroupID != "" && metadata.GroupID != group {
		return nil, fmt.Errorf("metadata group mismatch: %q != %q", metadata.GroupID, group)
	}
	if metadata.ArtifactID != "" && metadata.ArtifactID != artifact {
		return nil, fmt.Errorf("metadata artifact mismatch: %q != %q", metadata.ArtifactID, artifact)
	}
	metadata.GroupID = group
	metadata.ArtifactID = artifact

	versionSet := map[string]struct{}{}
	for _, version := range metadata.Versioning.Versions {
		if version == "" {
			continue
		}
		versionSet[version] = struct{}{}
	}
	for _, version := range versions {
		if version == "" {
			continue
		}
		versionSet[version] = struct{}{}
	}
	merged := make([]string, 0, len(versionSet))
	for version := range versionSet {
		merged = append(merged, version)
	}
	sort.Strings(merged)
	metadata.Versioning.Versions = merged
	if len(merged) != 0 {
		metadata.Versioning.Latest = merged[len(merged)-1]
		metadata.Versioning.Release = merged[len(merged)-1]
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
