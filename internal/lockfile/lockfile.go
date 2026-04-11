// Package lockfile defines the deterministic on-disk format for the
// resolved set of external dependencies pinned by content hash.
//
// A lockfile is produced by dependency resolution and consumed by Layer 2
// downloaders. Every pin names a coordinate, a source repository, and the
// SHA-256 hashes of every file fetched for that coordinate. Lockfiles are
// the source of truth for reproducible builds: a downloader must never
// return content whose hash disagrees with the pin.
//
// See roadmap/planning/dependency-cache-architecture.md and
// roadmap/planning/remote-artifact-fetch.md for the architectural contract.
package lockfile

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/kaeawc/grit/internal/cas"
)

// CurrentSchemaVersion is the lockfile schema version this package writes.
const CurrentSchemaVersion = 1

// Lockfile is the on-disk format produced by dependency resolution.
type Lockfile struct {
	SchemaVersion int       `json:"schemaVersion"`
	GeneratedAt   time.Time `json:"generatedAt"`
	GritVersion   string    `json:"gritVersion,omitempty"`
	Pins          []Pin     `json:"pins"`
}

// Pin is one resolved dependency along with the content hashes of every
// file fetched for it.
type Pin struct {
	Coordinate   Coordinate        `json:"coordinate"`
	RepositoryID string            `json:"repositoryId"`
	Files        []PinFile         `json:"files"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	// Dependencies records the declared transitive edge set so resolution
	// can be replayed without re-walking the upstream graph.
	Dependencies []Coordinate `json:"dependencies,omitempty"`
}

// Coordinate is a Maven-style group/artifact/version triple plus optional
// classifier. Coordinates are value types and compare by field equality.
type Coordinate struct {
	Group      string `json:"group"`
	Artifact   string `json:"artifact"`
	Version    string `json:"version"`
	Classifier string `json:"classifier,omitempty"`
}

// String returns a Maven-style coordinate string.
func (c Coordinate) String() string {
	if c.Classifier != "" {
		return fmt.Sprintf("%s:%s:%s:%s", c.Group, c.Artifact, c.Version, c.Classifier)
	}
	return fmt.Sprintf("%s:%s:%s", c.Group, c.Artifact, c.Version)
}

// PinFile is one fetched file recorded under a pinned coordinate.
type PinFile struct {
	Kind FileKind `json:"kind"`
	Name string   `json:"name"`
	Size int64    `json:"size"`
	Hash cas.Hash `json:"hash"`
	URL  string   `json:"url,omitempty"`
}

// FileKind is the role of a fetched file within a pinned dependency.
type FileKind string

const (
	FileKindPrimary   FileKind = "primary"
	FileKindPOM       FileKind = "pom"
	FileKindModule    FileKind = "module"
	FileKindSources   FileKind = "sources"
	FileKindJavadoc   FileKind = "javadoc"
	FileKindChecksum  FileKind = "checksum"
	FileKindSignature FileKind = "signature"
)

// Encode writes lf to w as deterministic JSON. Pins, files, capabilities,
// and dependency edges are canonicalized before encoding so serialized
// output does not depend on insertion order.
func (lf Lockfile) Encode(w io.Writer) error {
	normalized := lf.Canonicalize()
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(normalized)
}

// Decode reads a Lockfile from r. Unknown top-level fields are rejected so
// schema drift surfaces loudly at load time.
func Decode(r io.Reader) (Lockfile, error) {
	var lf Lockfile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lf); err != nil {
		return Lockfile{}, fmt.Errorf("lockfile: %w", err)
	}
	if lf.SchemaVersion == 0 {
		return Lockfile{}, fmt.Errorf("lockfile: missing schemaVersion")
	}
	if lf.SchemaVersion > CurrentSchemaVersion {
		return Lockfile{}, fmt.Errorf("lockfile: schemaVersion %d is newer than supported %d", lf.SchemaVersion, CurrentSchemaVersion)
	}
	return lf, nil
}

// Canonicalize returns a copy of lf with pins and inner slices sorted into
// a stable order. Callers never need to invoke Canonicalize directly;
// Encode does it automatically. It is exported so tests and tooling can
// compare two lockfiles for semantic equality.
func (lf Lockfile) Canonicalize() Lockfile {
	out := lf
	if out.SchemaVersion == 0 {
		out.SchemaVersion = CurrentSchemaVersion
	}
	pins := make([]Pin, len(lf.Pins))
	for i, p := range lf.Pins {
		pins[i] = canonicalizePin(p)
	}
	sort.Slice(pins, func(i, j int) bool {
		return pinLess(pins[i], pins[j])
	})
	out.Pins = pins
	return out
}

func canonicalizePin(p Pin) Pin {
	files := append([]PinFile(nil), p.Files...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].Kind != files[j].Kind {
			return files[i].Kind < files[j].Kind
		}
		return files[i].Name < files[j].Name
	})
	deps := append([]Coordinate(nil), p.Dependencies...)
	sort.Slice(deps, func(i, j int) bool {
		return coordinateLess(deps[i], deps[j])
	})
	caps := append([]string(nil), p.Capabilities...)
	sort.Strings(caps)
	return Pin{
		Coordinate:   p.Coordinate,
		RepositoryID: p.RepositoryID,
		Files:        files,
		Attributes:   p.Attributes,
		Capabilities: caps,
		Dependencies: deps,
	}
}

func pinLess(a, b Pin) bool {
	if a.Coordinate != b.Coordinate {
		return coordinateLess(a.Coordinate, b.Coordinate)
	}
	return a.RepositoryID < b.RepositoryID
}

func coordinateLess(a, b Coordinate) bool {
	if a.Group != b.Group {
		return a.Group < b.Group
	}
	if a.Artifact != b.Artifact {
		return a.Artifact < b.Artifact
	}
	if a.Version != b.Version {
		return a.Version < b.Version
	}
	return a.Classifier < b.Classifier
}
