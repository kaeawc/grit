// Package m2localbridge converts output from the existing
// internal/m2local resolver into input records for the new
// internal/lockfile/produce package.
//
// This is the bridge that lets the existing Gradle-cache-backed
// resolver feed the new content-addressed Layer 2+ stack without
// modifying m2local itself. The bridge is purely additive: it imports
// m2local only for the Resolved type, and m2local has no knowledge of
// this package.
//
// Every filesystem path in a m2local.Resolved that came from the
// Gradle dependency cache (under "<root>/files-2.1/...") is parsed
// back to a Maven coordinate via the stable Gradle cache layout, then
// grouped by coordinate into a produce.Input record. Files are
// classified into lockfile.FileKind values by filename extension:
//
//	.pom         → FileKindPOM
//	-sources.jar → FileKindSources
//	-javadoc.jar → FileKindJavadoc
//	.module      → FileKindModule
//	anything else (.jar, .aar) → FileKindPrimary
//
// AndroidLibraries is deliberately not processed. Those entries hold
// *extracted* classes.jar paths that live under ~/.grit/aar/ rather
// than the Gradle cache, and the original AAR already appears in
// CompileJars. The new aarextract transform reproduces the extraction
// deterministically from the primary AAR content hash, so there is no
// value in recording the extraction outputs in the lockfile.
//
// See roadmap/planning/dependency-cache-architecture.md for the
// architectural role the bridge plays.
package m2localbridge

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/lockfile/produce"
	"github.com/kaeawc/grit/internal/m2local"
)

// gradleCacheMarker is the last stable directory segment before the
// coordinate-structured part of a Gradle dependency cache path.
const gradleCacheMarker = "files-2.1"

// FromResolved converts a m2local.Resolved into a set of produce.Input
// records, grouped by Maven coordinate.
//
// All paths from CompileJars, RuntimeJars, and TestJars are processed.
// Each file is added exactly once (the bridge deduplicates across the
// three lists, which typically overlap). Files whose path does not
// match the Gradle cache layout cause FromResolved to return an error.
//
// repositoryID is recorded on every pin as the source repository. For
// an m2local resolver rooted at ~/.gradle/caches/modules-2/files-2.1,
// a reasonable value is "gradle-cache".
func FromResolved(resolved *m2local.Resolved, repositoryID string) ([]produce.Input, error) {
	if resolved == nil {
		return nil, fmt.Errorf("m2localbridge: nil resolved")
	}

	// groups maps coordinate → partial Input being built up.
	groups := map[lockfile.Coordinate]*produce.Input{}
	seen := map[string]bool{}

	add := func(path string) error {
		if path == "" || seen[path] {
			return nil
		}
		seen[path] = true
		coord, err := coordinateFromPath(path)
		if err != nil {
			return err
		}
		in, ok := groups[coord]
		if !ok {
			in = &produce.Input{
				Coordinate:   coord,
				RepositoryID: repositoryID,
			}
			groups[coord] = in
		}
		name := filepath.Base(path)
		in.Files = append(in.Files, produce.FileInput{
			Kind: classifyFile(name),
			Name: name,
			Path: path,
		})
		return nil
	}

	for _, p := range resolved.CompileJars {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	for _, p := range resolved.RuntimeJars {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	for _, p := range resolved.TestJars {
		if err := add(p); err != nil {
			return nil, err
		}
	}

	// Emit inputs in a deterministic order (sorted by coordinate) so
	// the resulting lockfile is stable even before produce.Produce
	// runs its own canonicalization.
	inputs := make([]produce.Input, 0, len(groups))
	for _, in := range groups {
		inputs = append(inputs, *in)
	}
	sort.Slice(inputs, func(i, j int) bool {
		return coordinateLess(inputs[i].Coordinate, inputs[j].Coordinate)
	})
	return inputs, nil
}

// coordinateFromPath extracts a Maven coordinate from a Gradle cache
// filesystem path. The expected layout is:
//
//	<anything>/files-2.1/<group>/<artifact>/<version>/<sha1>/<file>
//
// Only indices 0..2 after the files-2.1 marker are used; the <sha1>
// subdir and filename are ignored. Paths that do not contain the
// marker or have fewer than four segments after it are rejected.
func coordinateFromPath(path string) (lockfile.Coordinate, error) {
	marker := gradleCacheMarker + string(filepath.Separator)
	idx := strings.Index(path, marker)
	if idx < 0 {
		return lockfile.Coordinate{}, fmt.Errorf("m2localbridge: path %q is not in Gradle cache layout (missing %q marker)", path, gradleCacheMarker)
	}
	rest := strings.Split(path[idx+len(marker):], string(filepath.Separator))
	if len(rest) < 4 {
		return lockfile.Coordinate{}, fmt.Errorf("m2localbridge: path %q does not contain group/module/version", path)
	}
	if rest[0] == "" || rest[1] == "" || rest[2] == "" {
		return lockfile.Coordinate{}, fmt.Errorf("m2localbridge: path %q has empty coordinate segment", path)
	}
	return lockfile.Coordinate{
		Group:    rest[0],
		Artifact: rest[1],
		Version:  rest[2],
	}, nil
}

// classifyFile maps a filename to a lockfile.FileKind via its suffix.
func classifyFile(name string) lockfile.FileKind {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".pom"):
		return lockfile.FileKindPOM
	case strings.HasSuffix(lower, ".module"):
		return lockfile.FileKindModule
	case strings.HasSuffix(lower, "-sources.jar"):
		return lockfile.FileKindSources
	case strings.HasSuffix(lower, "-javadoc.jar"):
		return lockfile.FileKindJavadoc
	default:
		return lockfile.FileKindPrimary
	}
}

func coordinateLess(a, b lockfile.Coordinate) bool {
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
