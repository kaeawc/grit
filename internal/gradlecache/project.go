package gradlecache

import (
	"path/filepath"

	"github.com/kaeawc/grit/internal/project"
)

// ProjectStagingRoot returns the directory grit owns under a project
// tree for staging artifacts. The directory may or may not exist yet —
// callers should create it (and any intermediate ancestors) before
// writing into it.
func ProjectStagingRoot(prj *project.Project) string {
	if prj == nil {
		return ""
	}
	root := prj.RootDir
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".grit", "cache", "artifacts")
}

// ProjectProbe returns a probe rooted at the project's staging cache
// with a fallback to the default user cache. When prj is nil or has
// no root the function returns DefaultProbe so callers don't have to
// special-case the early-startup case. Callers that want fetch-on-miss
// (download from a remote Maven repo when the local chain misses)
// should chain WithFetcher onto the returned probe.
func ProjectProbe(prj *project.Project) *Probe {
	staging := ProjectStagingRoot(prj)
	if staging == "" {
		return DefaultProbe()
	}
	return NewProbe(staging).WithStaging(StageByHardlink).WithFallback(DefaultProbe())
}
