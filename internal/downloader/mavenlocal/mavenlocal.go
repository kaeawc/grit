// Package mavenlocal implements a Layer 2 downloader that reads artifacts
// from a standard Maven local repository.
//
// The source is a directory in Maven layout, typically at ~/.m2/repository.
// Its layout differs from Gradle's dependency cache in three important
// ways:
//
//  1. Group segments are slashed rather than dotted:
//     org.example → org/example
//  2. Files live directly in the version directory; there is no SHA-1
//     subdir between version and file.
//  3. The POM, primary artifact, sources, and javadoc jars all live as
//     siblings under the version directory.
//
// Example:
//
//	<root>/org/example/alpha/1.0/alpha-1.0.jar
//	<root>/org/example/alpha/1.0/alpha-1.0.pom
//	<root>/org/example/alpha/1.0/alpha-1.0-sources.jar
//
// This package is read-only. Publishing artifacts in Maven layout is the
// job of internal/publish/mavenlocal, which is a separate Layer 5
// adapter. See roadmap/planning/maven-local-support.md for the rationale
// for keeping those two roles in distinct packages.
package mavenlocal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/mavenlocalroot"
)

// ID is the stable identifier recorded in provenance for blobs sourced
// through this downloader.
const ID = "maven-local"

// DefaultRoot returns the conventional Maven local repository path under
// the user's home directory, or an empty string if HOME is unset.
func DefaultRoot() string {
	return mavenlocalroot.Default()
}

// Downloader reads artifact files from a Maven local repository.
type Downloader struct {
	root string
}

// New returns a Downloader rooted at dir. The directory should point at a
// Maven repository tree, typically ~/.m2/repository. It is not required
// to exist at construction time; missing module directories surface as
// per-pin errors.
func New(dir string) *Downloader {
	return &Downloader{root: dir}
}

// Root returns the repository root this downloader reads from.
func (d *Downloader) Root() string { return d.root }

// ID implements downloader.Downloader.
func (d *Downloader) ID() string { return ID }

// Fetch locates every file declared in pin under the Maven repository
// root, verifies the content hash via store.PutExpected, and stores the
// bytes in store. Fetch is idempotent.
func (d *Downloader) Fetch(ctx context.Context, pin lockfile.Pin, store cas.Store) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	base := d.moduleBasePath(pin.Coordinate)
	for _, file := range pin.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		has, err := store.Has(ctx, file.Hash)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		path := filepath.Join(base, file.Name)
		if err := d.ingest(ctx, store, pin, file, path); err != nil {
			return err
		}
	}
	return nil
}

// ModulePath returns the directory where files for coord live under the
// repository root. It is exported so the publish adapter can reuse the
// same layout rule.
func (d *Downloader) ModulePath(coord lockfile.Coordinate) string {
	return d.moduleBasePath(coord)
}

func (d *Downloader) moduleBasePath(coord lockfile.Coordinate) string {
	return filepath.Join(d.root, GroupPath(coord.Group), coord.Artifact, coord.Version)
}

// GroupPath converts a dotted Maven group ID to its slashed filesystem
// path. It is exported so sibling packages (most importantly the publish
// adapter) can share the layout rule without reimplementing it.
func GroupPath(group string) string {
	if group == "" {
		return ""
	}
	return strings.ReplaceAll(group, ".", string(filepath.Separator))
}

func (d *Downloader) ingest(ctx context.Context, store cas.Store, pin lockfile.Pin, file lockfile.PinFile, path string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: mavenlocal: %s", downloader.ErrNotFound, path)
		}
		return err
	}
	defer func() { _ = f.Close() }()
	prov := cas.Provenance{
		Source: cas.Source{
			Kind: cas.SourceDownload,
			Download: &cas.DownloadSource{
				Downloader:   ID,
				RepositoryID: pin.RepositoryID,
				Coordinate:   pin.Coordinate.String(),
				URL:          file.URL,
			},
		},
		Attributes: map[string]string{
			"file.kind":   string(file.Kind),
			"file.name":   file.Name,
			"source.path": path,
		},
	}
	if _, err := store.PutExpected(ctx, f, file.Hash, prov); err != nil {
		return fmt.Errorf("mavenlocal: ingest %s: %w", path, err)
	}
	return nil
}

// Compile-time assertion that *Downloader satisfies downloader.Downloader.
var _ downloader.Downloader = (*Downloader)(nil)
