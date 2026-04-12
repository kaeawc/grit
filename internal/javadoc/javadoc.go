// Package javadoc defines the javadoc-jar action descriptor.
//
// A javadoc jar packages HTML API documentation into a jar with the
// -javadoc classifier, as required for Maven Central publication.
// The tool selector chooses Dokka when Kotlin sources are present,
// falling back to javadoc for pure-Java modules.
package javadoc

import (
	"path/filepath"
	"strings"
)

// ToolKind identifies the documentation generator to invoke.
type ToolKind string

const (
	// ToolKindDokka selects the Dokka documentation engine (Kotlin).
	ToolKindDokka ToolKind = "dokka"
	// ToolKindJavadoc selects the standard javadoc tool (Java).
	ToolKindJavadoc ToolKind = "javadoc"
)

// ToolDescriptor captures everything needed to produce a -javadoc.jar
// for a single module variant.
type ToolDescriptor struct {
	// Tool is the documentation generator to invoke.
	Tool ToolKind `json:"tool"`
	// SourceRoots lists directories containing source files to document.
	SourceRoots []string `json:"sourceRoots"`
	// Classpath entries required to resolve types during doc generation.
	Classpath []string `json:"classpath,omitempty"`
	// OutputPath is the path where the -javadoc.jar will be written.
	OutputPath string `json:"outputPath"`
}

// Classifier returns the Maven artifact classifier for javadoc jars.
func Classifier() string {
	return "javadoc"
}

// OutputFileName returns the conventional -javadoc.jar file name for
// the given artifact ID and version.
func OutputFileName(artifactID, version string) string {
	return artifactID + "-" + version + "-javadoc.jar"
}

// OutputPathForModule returns the default output path for a javadoc jar
// within the module's build directory.
func OutputPathForModule(moduleDir, artifactID, version string) string {
	return filepath.Join(moduleDir, "build", "libs", OutputFileName(artifactID, version))
}

// SelectTool inspects sourceRoots for .kt files and returns ToolKindDokka
// if any are found, otherwise ToolKindJavadoc.
func SelectTool(sourceRoots []string) ToolKind {
	for _, root := range sourceRoots {
		if hasKotlinSuffix(root) {
			return ToolKindDokka
		}
	}
	return ToolKindJavadoc
}

// NewToolDescriptor creates a ToolDescriptor with the tool auto-selected
// from the source roots.
func NewToolDescriptor(sourceRoots, classpath []string, outputPath string) ToolDescriptor {
	return ToolDescriptor{
		Tool:        SelectTool(sourceRoots),
		SourceRoots: sourceRoots,
		Classpath:   classpath,
		OutputPath:  outputPath,
	}
}

// hasKotlinSuffix returns true if the path ends with .kt or contains
// /kotlin/ as a directory component, which is the conventional Kotlin
// source root layout.
func hasKotlinSuffix(path string) bool {
	if strings.HasSuffix(path, ".kt") || strings.HasSuffix(path, ".kts") {
		return true
	}
	// Conventional source root: src/main/kotlin
	return strings.Contains(path, string(filepath.Separator)+"kotlin"+string(filepath.Separator)) ||
		strings.HasSuffix(path, string(filepath.Separator)+"kotlin")
}
