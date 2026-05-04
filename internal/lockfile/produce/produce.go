// Package produce builds an internal/lockfile.Lockfile from a set of
// resolved dependency records by hashing their filesystem paths.
//
// The producer is the join point between Layer 1 (dependency resolution)
// and every Layer 2+ consumer in grit's dependency-cache architecture.
// Callers pass a list of Input records describing each pinned
// coordinate and the filesystem paths of the files that make it up.
// The producer reads each file, computes its SHA-256 and size, and
// assembles a Lockfile whose pins name the same files by content hash.
//
// Produce is a pure function of its inputs: given the same Input slice,
// the same Options, and the same bytes on disk, Produce emits a
// byte-identical canonicalized Lockfile. Tests inject a fixed
// GeneratedAt for full determinism.
//
// The producer deliberately does not depend on any resolver
// implementation (m2local or otherwise). Callers that have a concrete
// resolver build their own Input slice and hand it to Produce. This
// keeps the layer boundary clean and avoids coupling the new tree to
// the existing internal/m2local package. See
// roadmap/planning/dependency-cache-architecture.md for the
// architectural role.
package produce

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

// Input describes one resolved dependency to be recorded in the
// lockfile. Every field except Coordinate and Files is optional.
type Input struct {
	// Coordinate is the Maven-style group/artifact/version triple that
	// identifies this dependency. Must be fully populated.
	Coordinate lockfile.Coordinate
	// RepositoryID is the identifier of the repository that served
	// this dependency at resolution time. Recorded in the pin so
	// downstream tooling can explain why a file was reached.
	RepositoryID string
	// Files is the list of filesystem paths that should be hashed and
	// recorded under this pin. At least one file is required.
	Files []FileInput
	// Attributes is a free-form map of selection attributes (variant
	// hints, platform hints) preserved verbatim in the pin.
	Attributes map[string]string
	// Capabilities is the set of declared capabilities for this
	// dependency. Copied into the pin.
	Capabilities []string
	// Dependencies is the declared transitive edge set for this pin.
	// Copied into the pin so resolution can be replayed without
	// re-walking the upstream graph.
	Dependencies []lockfile.Coordinate
}

// FileInput names one filesystem file that should be hashed and
// recorded under a pin.
type FileInput struct {
	Kind lockfile.FileKind
	// Name is the lockfile-visible file name (e.g. "alpha-1.0.jar").
	// If empty, Produce uses filepath.Base(Path).
	Name string
	// Path is the absolute or relative filesystem path to the file
	// whose bytes will be hashed. Must reference a regular file, not
	// a directory.
	Path string
	// URL is the origin URL of the file if known, preserved verbatim
	// in the pin for provenance and remote re-fetch.
	URL string
}

// Options configures the producer. The zero value is valid: GeneratedAt
// defaults to time.Now().UTC() and GritVersion to the empty string.
type Options struct {
	// GeneratedAt is the timestamp embedded in the Lockfile. Use a
	// fixed value in tests for deterministic output.
	GeneratedAt time.Time
	// GritVersion is an optional version stamp embedded in the
	// Lockfile. Empty by default.
	GritVersion string
}

// Produce reads every file named in every input, computes its SHA-256
// and size, and assembles a canonicalized lockfile. If any file is
// missing, unreadable, or a directory, Produce returns an error
// without emitting a partial lockfile.
//
// The returned Lockfile is always pre-canonicalized: pins are sorted
// by coordinate, files within each pin are sorted by kind then name,
// and dependency/capability slices are sorted. Callers can Encode the
// result directly without worrying about input ordering.
func Produce(inputs []Input, opts Options) (lockfile.Lockfile, error) {
	if len(inputs) == 0 {
		return lockfile.Lockfile{}, errors.New("produce: at least one input required")
	}
	pins := make([]lockfile.Pin, 0, len(inputs))
	for _, in := range inputs {
		pin, err := buildPin(in)
		if err != nil {
			return lockfile.Lockfile{}, err
		}
		pins = append(pins, pin)
	}
	generated := opts.GeneratedAt
	if generated.IsZero() {
		generated = time.Now().UTC()
	}
	lf := lockfile.Lockfile{
		SchemaVersion: lockfile.CurrentSchemaVersion,
		GeneratedAt:   generated,
		GritVersion:   opts.GritVersion,
		Pins:          pins,
	}
	return lf.Canonicalize(), nil
}

func buildPin(in Input) (lockfile.Pin, error) {
	if in.Coordinate.Group == "" || in.Coordinate.Artifact == "" || in.Coordinate.Version == "" {
		return lockfile.Pin{}, fmt.Errorf("produce: incomplete coordinate: %+v", in.Coordinate)
	}
	if len(in.Files) == 0 {
		return lockfile.Pin{}, fmt.Errorf("produce: %s has no files", in.Coordinate)
	}
	pinFiles := make([]lockfile.PinFile, 0, len(in.Files))
	for _, f := range in.Files {
		pf, err := hashFile(f)
		if err != nil {
			return lockfile.Pin{}, fmt.Errorf("produce: %s: %w", in.Coordinate, err)
		}
		pinFiles = append(pinFiles, pf)
	}
	var attrs map[string]string
	if len(in.Attributes) > 0 {
		attrs = make(map[string]string, len(in.Attributes))
		for k, v := range in.Attributes {
			attrs[k] = v
		}
	}
	return lockfile.Pin{
		Coordinate:   in.Coordinate,
		RepositoryID: in.RepositoryID,
		Files:        pinFiles,
		Attributes:   attrs,
		Capabilities: append([]string(nil), in.Capabilities...),
		Dependencies: append([]lockfile.Coordinate(nil), in.Dependencies...),
	}, nil
}

func hashFile(f FileInput) (lockfile.PinFile, error) {
	if f.Path == "" {
		return lockfile.PinFile{}, fmt.Errorf("empty path for %s", f.Name)
	}
	info, err := os.Stat(f.Path)
	if err != nil {
		return lockfile.PinFile{}, fmt.Errorf("stat %s: %w", f.Path, err)
	}
	if info.IsDir() {
		return lockfile.PinFile{}, fmt.Errorf("%s is a directory, not a regular file", f.Path)
	}
	file, err := os.Open(f.Path)
	if err != nil {
		return lockfile.PinFile{}, fmt.Errorf("open %s: %w", f.Path, err)
	}
	defer func() { _ = file.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, file)
	if err != nil {
		return lockfile.PinFile{}, fmt.Errorf("read %s: %w", f.Path, err)
	}
	var hash cas.Hash
	copy(hash[:], h.Sum(nil))

	name := f.Name
	if name == "" {
		name = filepath.Base(f.Path)
	}
	return lockfile.PinFile{
		Kind: f.Kind,
		Name: name,
		Size: n,
		Hash: hash,
		URL:  f.URL,
	}, nil
}
