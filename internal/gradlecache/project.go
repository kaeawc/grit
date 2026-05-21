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
// with a fallback to the default user cache and a fetch-on-miss path
// pointing at the public Maven repos. When prj is nil or has no root
// the function returns DefaultProbe so callers don't have to
// special-case the early-startup case. Set GRIT_OFFLINE=1 to disable
// the fetch step (the fallback chain still runs). Test binaries
// default to offline so tests don't accidentally hit the network.
func ProjectProbe(prj *project.Project) *Probe {
	staging := ProjectStagingRoot(prj)
	if staging == "" {
		return DefaultProbe()
	}
	probe := NewProbe(staging).WithStaging(StageByHardlink).WithFallback(DefaultProbe())
	if fetcher := defaultFetcher(); fetcher != nil {
		probe = probe.WithFetcher(fetcher)
	}
	return probe
}
