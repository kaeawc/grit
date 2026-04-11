package mavenlocal

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kaeawc/grit/internal/lockfile"
)

type gradleModuleMetadata struct {
	FormatVersion string                `json:"formatVersion"`
	Component     gradleModuleComponent `json:"component"`
	Variants      []gradleModuleVariant `json:"variants"`
}

type gradleModuleComponent struct {
	Group   string `json:"group"`
	Module  string `json:"module"`
	Version string `json:"version"`
}

type gradleModuleVariant struct {
	Name         string                   `json:"name"`
	Attributes   map[string]string        `json:"attributes,omitempty"`
	Capabilities []gradleModuleCapability `json:"capabilities,omitempty"`
	Dependencies []gradleModuleDependency `json:"dependencies,omitempty"`
	Files        []gradleModuleFile       `json:"files,omitempty"`
}

type gradleModuleCapability struct {
	Group   string `json:"group"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type gradleModuleDependency struct {
	Group   string `json:"group"`
	Module  string `json:"module"`
	Version struct {
		Requires string `json:"requires"`
	} `json:"version"`
}

type gradleModuleFile struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url"`
}

func (p *Publisher) publishGeneratedModule(pin lockfile.Pin) error {
	if hasPinFileKind(pin, lockfile.FileKindModule) {
		return nil
	}
	payload, ok, err := generatedModulePayload(pin)
	if err != nil || !ok {
		return err
	}
	path := filepath.Join(p.moduleBasePath(pin.Coordinate), moduleFileName(pin.Coordinate))
	return writeBytesWithSidecars(path, payload)
}

func hasPinFileKind(pin lockfile.Pin, kind lockfile.FileKind) bool {
	for _, file := range pin.Files {
		if file.Kind == kind {
			return true
		}
	}
	return false
}

func moduleFileName(coord lockfile.Coordinate) string {
	return coord.Artifact + "-" + coord.Version + ".module"
}

func generatedModulePayload(pin lockfile.Pin) ([]byte, bool, error) {
	primaryFiles := gradleModuleFilesForKinds(pin.Files, lockfile.FileKindPrimary)
	if len(primaryFiles) == 0 {
		return nil, false, nil
	}
	mainAttrs := defaultRuntimeAttributes(pin.Attributes, pin.Files)
	variants := []gradleModuleVariant{{
		Name:         runtimeVariantName(mainAttrs),
		Attributes:   mainAttrs,
		Capabilities: gradleModuleCapabilities(pin),
		Dependencies: gradleModuleDependencies(pin.Dependencies),
		Files:        primaryFiles,
	}}
	if sources := gradleModuleFilesForKinds(pin.Files, lockfile.FileKindSources); len(sources) != 0 {
		variants = append(variants, gradleModuleVariant{
			Name:       "sourcesElements",
			Attributes: map[string]string{"org.gradle.category": "documentation", "org.gradle.docstype": "sources"},
			Files:      sources,
		})
	}
	if javadoc := gradleModuleFilesForKinds(pin.Files, lockfile.FileKindJavadoc); len(javadoc) != 0 {
		variants = append(variants, gradleModuleVariant{
			Name:       "javadocElements",
			Attributes: map[string]string{"org.gradle.category": "documentation", "org.gradle.docstype": "javadoc"},
			Files:      javadoc,
		})
	}
	payload, err := json.MarshalIndent(gradleModuleMetadata{
		FormatVersion: "1.1",
		Component: gradleModuleComponent{
			Group:   pin.Coordinate.Group,
			Module:  pin.Coordinate.Artifact,
			Version: pin.Coordinate.Version,
		},
		Variants: variants,
	}, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(payload, '\n'), true, nil
}

func gradleModuleFilesForKinds(files []lockfile.PinFile, kinds ...lockfile.FileKind) []gradleModuleFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]gradleModuleFile, 0, len(files))
	for _, file := range files {
		if !slices.Contains(kinds, file.Kind) {
			continue
		}
		out = append(out, gradleModuleFile{Name: file.Name, URL: file.Name})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func defaultRuntimeAttributes(attrs map[string]string, files []lockfile.PinFile) map[string]string {
	out := map[string]string{}
	for key, value := range attrs {
		out[key] = value
	}
	if out["org.gradle.usage"] == "" {
		out["org.gradle.usage"] = "java-runtime"
	}
	if out["org.jetbrains.kotlin.platform.type"] == "" {
		switch primaryArtifactExtension(files) {
		case ".aar":
			out["org.jetbrains.kotlin.platform.type"] = "androidJvm"
		case ".jar":
			out["org.jetbrains.kotlin.platform.type"] = "jvm"
		}
	}
	return out
}

func primaryArtifactExtension(files []lockfile.PinFile) string {
	for _, file := range files {
		if file.Kind != lockfile.FileKindPrimary {
			continue
		}
		return strings.ToLower(filepath.Ext(file.Name))
	}
	return ""
}

func runtimeVariantName(attrs map[string]string) string {
	if attrs["org.gradle.usage"] == "java-api" {
		return "apiElements"
	}
	return "runtimeElements"
}

func gradleModuleCapabilities(pin lockfile.Pin) []gradleModuleCapability {
	if len(pin.Capabilities) == 0 {
		return nil
	}
	out := make([]gradleModuleCapability, 0, len(pin.Capabilities))
	for _, capability := range pin.Capabilities {
		parts := strings.Split(capability, ":")
		if len(parts) != 3 {
			continue
		}
		out = append(out, gradleModuleCapability{
			Group:   strings.TrimSpace(parts[0]),
			Name:    strings.TrimSpace(parts[1]),
			Version: strings.TrimSpace(parts[2]),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gradleModuleDependencies(deps []lockfile.Coordinate) []gradleModuleDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]gradleModuleDependency, 0, len(deps))
	for _, dep := range deps {
		if dep.Group == "" || dep.Artifact == "" || dep.Version == "" {
			continue
		}
		var item gradleModuleDependency
		item.Group = dep.Group
		item.Module = dep.Artifact
		item.Version.Requires = dep.Version
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
