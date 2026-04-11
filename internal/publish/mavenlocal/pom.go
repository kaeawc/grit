package mavenlocal

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/kaeawc/grit/internal/lockfile"
)

type mavenPomProject struct {
	XMLName      xml.Name             `xml:"project"`
	Xmlns        string               `xml:"xmlns,attr,omitempty"`
	Xsi          string               `xml:"xmlns:xsi,attr,omitempty"`
	Schema       string               `xml:"xsi:schemaLocation,attr,omitempty"`
	Model        string               `xml:"modelVersion"`
	GroupID      string               `xml:"groupId"`
	ArtifactID   string               `xml:"artifactId"`
	Version      string               `xml:"version"`
	Packaging    string               `xml:"packaging,omitempty"`
	Dependencies []mavenPomDependency `xml:"dependencies>dependency,omitempty"`
}

type mavenPomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope,omitempty"`
}

func (p *Publisher) publishGeneratedPom(pin lockfile.Pin) error {
	if hasPinFileKind(pin, lockfile.FileKindPOM) {
		return nil
	}
	payload, ok, err := generatedPomPayload(pin)
	if err != nil || !ok {
		return err
	}
	path := filepath.Join(p.moduleBasePath(pin.Coordinate), pomFileName(pin.Coordinate))
	return writeBytesWithSidecars(path, payload)
}

func pomFileName(coord lockfile.Coordinate) string {
	return coord.Artifact + "-" + coord.Version + ".pom"
}

func generatedPomPayload(pin lockfile.Pin) ([]byte, bool, error) {
	if pin.Coordinate.Group == "" || pin.Coordinate.Artifact == "" || pin.Coordinate.Version == "" {
		return nil, false, fmt.Errorf("incomplete coordinate: %+v", pin.Coordinate)
	}
	deps := make([]lockfile.Coordinate, 0, len(pin.Dependencies))
	for _, dep := range pin.Dependencies {
		if dep.Group == "" || dep.Artifact == "" || dep.Version == "" {
			continue
		}
		deps = append(deps, dep)
	}
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].Group != deps[j].Group {
			return deps[i].Group < deps[j].Group
		}
		if deps[i].Artifact != deps[j].Artifact {
			return deps[i].Artifact < deps[j].Artifact
		}
		if deps[i].Version != deps[j].Version {
			return deps[i].Version < deps[j].Version
		}
		return deps[i].Classifier < deps[j].Classifier
	})
	project := mavenPomProject{
		Xmlns:      "http://maven.apache.org/POM/4.0.0",
		Xsi:        "http://www.w3.org/2001/XMLSchema-instance",
		Schema:     "http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd",
		Model:      "4.0.0",
		GroupID:    pin.Coordinate.Group,
		ArtifactID: pin.Coordinate.Artifact,
		Version:    pin.Coordinate.Version,
		Packaging:  "jar",
	}
	if len(deps) > 0 {
		project.Dependencies = make([]mavenPomDependency, 0, len(deps))
		for _, dep := range deps {
			project.Dependencies = append(project.Dependencies, mavenPomDependency{
				GroupID:    dep.Group,
				ArtifactID: dep.Artifact,
				Version:    dep.Version,
			})
		}
	}
	payload, err := xml.MarshalIndent(project, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(append([]byte(xml.Header), payload...), '\n'), true, nil
}
