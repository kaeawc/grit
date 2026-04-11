package nativecompile

import (
	"strings"

	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/project"
)

func filterResolvedForProject(prj *project.Project, resolved *m2local.Resolved) *m2local.Resolved {
	if resolved == nil || prj == nil {
		return resolved
	}
	if !strings.EqualFold(strings.TrimSpace(prj.GradleProperties["android.useAndroidX"]), "true") {
		return resolved
	}
	filtered := *resolved
	filtered.CompileJars = filterSupportArtifacts(resolved.CompileJars)
	filtered.RuntimeJars = filterSupportArtifacts(resolved.RuntimeJars)
	filtered.TestJars = filterSupportArtifacts(resolved.TestJars)
	filtered.AndroidLibraries = filterSupportAndroidLibraries(resolved.AndroidLibraries)
	return &filtered
}

func filterSupportArtifacts(paths []string) []string {
	var out []string
	for _, path := range paths {
		if isLegacySupportArtifact(path) {
			continue
		}
		out = append(out, path)
	}
	return out
}

func filterSupportAndroidLibraries(libs []m2local.AndroidLibrary) []m2local.AndroidLibrary {
	var out []m2local.AndroidLibrary
	for _, lib := range libs {
		if strings.HasPrefix(lib.ID, "maven:com.android.support:") {
			continue
		}
		out = append(out, lib)
	}
	return out
}

func isLegacySupportArtifact(path string) bool {
	return strings.Contains(path, "/com.android.support/") || strings.Contains(path, "\\com.android.support\\")
}
