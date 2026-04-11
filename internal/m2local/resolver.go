package m2local

import (
	"sync"

	"github.com/kaeawc/grit/internal/catalog"
	"github.com/kaeawc/grit/internal/perf"
	"github.com/kaeawc/grit/internal/project"
)

type Resolver struct {
	CacheRoot    string
	WorkRoot     string
	Repositories []project.Repository
	Catalog      *catalog.Catalog
	Tracker      perf.Tracker
	mu           sync.Mutex
	inflight     map[string]*resolveCall
	fetched      map[string]ResolutionMetadataSource
	report       ResolutionReport
	replay       ResolutionReplay
}

type resolveCall struct {
	done   chan struct{}
	result resolveResult
}

type resolveResult struct {
	artifact       string
	androidLibrary *AndroidLibrary
	deps           []Coordinate
	err            error
}

type Coordinate struct {
	Group    string
	Module   string
	Version  string
	Excludes []Exclude
}

type Exclude struct {
	Group  string
	Module string
}

type AndroidLibrary struct {
	ID           string
	ClassesJar   string
	ManifestPath string
	ResDir       string
}

type Resolved struct {
	CompileJars      []string
	RuntimeJars      []string
	TestJars         []string
	AndroidLibraries []AndroidLibrary
	Report           ResolutionReport   `json:"report,omitempty"`
	Replay           ResolutionReplay   `json:"replay,omitempty"`
	Lockfile         ResolutionLockfile `json:"lockfile,omitempty"`
}

type ResolutionReport struct {
	Selections []ResolutionSelection `json:"selections,omitempty"`
	Conflicts  []ResolutionConflict  `json:"conflicts,omitempty"`
}

type ResolutionSelection struct {
	Kind           string                    `json:"kind,omitempty"`
	Coordinate     string                    `json:"coordinate,omitempty"`
	Chosen         string                    `json:"chosen,omitempty"`
	Reason         string                    `json:"reason,omitempty"`
	Binding        string                    `json:"binding,omitempty"`
	Alternates     []string                  `json:"alternates,omitempty"`
	Attributes     map[string]string         `json:"attributes,omitempty"`
	Capabilities   []string                  `json:"capabilities,omitempty"`
	MetadataSource *ResolutionMetadataSource `json:"metadataSource,omitempty"`
}

type ResolutionMetadataSource struct {
	Kind          string `json:"kind,omitempty"`
	Path          string `json:"path,omitempty"`
	RepositoryURL string `json:"repositoryUrl,omitempty"`
	Fetched       bool   `json:"fetched,omitempty"`
}

type ResolutionConflict struct {
	Kind        string   `json:"kind,omitempty"`
	Module      string   `json:"module,omitempty"`
	Selected    string   `json:"selected,omitempty"`
	Discarded   string   `json:"discarded,omitempty"`
	Coordinates []string `json:"coordinates,omitempty"`
}

type ResolutionReplay struct {
	Pins []ResolutionPin `json:"pins,omitempty"`
}

type ResolutionPin struct {
	Coordinate    string   `json:"coordinate,omitempty"`
	Variant       string   `json:"variant,omitempty"`
	Binding       string   `json:"binding,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
	RepositoryURL string   `json:"repositoryUrl,omitempty"`
}

type ResolutionLockfile struct {
	SchemaVersion int                   `json:"schemaVersion,omitempty"`
	Format        string                `json:"format,omitempty"`
	Pins          []ResolutionPin       `json:"pins,omitempty"`
	Selections    []ResolutionSelection `json:"selections,omitempty"`
	Conflicts     []ResolutionConflict  `json:"conflicts,omitempty"`
}

type ResolutionReportArtifact struct {
	SchemaVersion int              `json:"schemaVersion,omitempty"`
	Format        string           `json:"format,omitempty"`
	Report        ResolutionReport `json:"report,omitempty"`
}

type ResolutionReplayArtifact struct {
	SchemaVersion int              `json:"schemaVersion,omitempty"`
	Format        string           `json:"format,omitempty"`
	Replay        ResolutionReplay `json:"replay,omitempty"`
}

type ResolvedEnvelope struct {
	SchemaVersion int           `json:"schemaVersion"`
	Format        string        `json:"format"`
	Topology      CacheTopology `json:"topology"`
	Resolved      Resolved      `json:"resolved"`
}

func New(cacheRoot, workRoot string, repos []project.Repository, cat *catalog.Catalog) *Resolver {
	return &Resolver{
		CacheRoot:    cacheRoot,
		WorkRoot:     workRoot,
		Repositories: repos,
		Catalog:      cat,
		inflight:     map[string]*resolveCall{},
		fetched:      map[string]ResolutionMetadataSource{},
	}
}

func (r *Resolver) SetTracker(tracker perf.Tracker) {
	r.Tracker = tracker
}
