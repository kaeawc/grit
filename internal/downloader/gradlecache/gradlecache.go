// Package gradlecache implements a Layer 2 downloader that reads artifacts
// from an existing Gradle dependency cache.
//
// The source is the Gradle dependency cache tree, typically at
// ~/.gradle/caches/modules-2/files-2.1. Its layout is:
//
//	<root>/<group>/<artifact>/<version>/<sha1>/<file>
//
// The <sha1> subdirectory is Gradle's SHA-1 of the origin URL and is
// unrelated to content. gradlecache walks all subdirectories looking for a
// file whose name matches the one declared in a lockfile pin, then hands
// the bytes to the CAS via PutExpected so the declared content hash is
// verified before anything lands in the store.
//
// This package is read-only. It never writes to the Gradle cache.
package gradlecache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
)

// ID is the stable identifier recorded in provenance for blobs sourced
// through this downloader.
const ID = "gradle-cache"

// DefaultRoot returns the conventional Gradle cache path under the user's
// home directory, or an empty string if HOME is unset.
func DefaultRoot() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".gradle", "caches", "modules-2", "files-2.1")
}

// Downloader reads artifact files from a Gradle cache directory.
type Downloader struct {
	root string
}

// New returns a Downloader rooted at dir. The directory should point at a
// Gradle files-2.1 tree. It is not required to exist at construction time;
// missing module directories surface as per-pin errors.
func New(dir string) *Downloader {
	return &Downloader{root: dir}
}

// Root returns the cache root this downloader reads from.
func (d *Downloader) Root() string { return d.root }

// ID implements downloader.Downloader.
func (d *Downloader) ID() string { return ID }

// Fetch locates every file declared in pin under the Gradle cache root,
// verifies the content hash via store.PutExpected, and stores the bytes.
// Fetch is idempotent: files already present in store are not re-read.
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
		path, err := d.locate(base, file.Name)
		if err != nil {
			return fmt.Errorf("gradlecache: locate %s for %s: %w", file.Name, pin.Coordinate, err)
		}
		if err := d.ingest(ctx, store, pin, file, path); err != nil {
			return err
		}
	}
	return nil
}

func (d *Downloader) moduleBasePath(coord lockfile.Coordinate) string {
	return filepath.Join(d.root, coord.Group, coord.Artifact, coord.Version)
}

// locate returns the filesystem path of the first regular file under
// base/<sub>/name whose name matches. gradlecache does not care which
// Gradle <sub> (SHA-1-of-URL) holds the file; the downstream hash check
// rejects any file whose bytes do not match the pin.
//
// Missing modules and missing files are reported as errors wrapping
// downloader.ErrNotFound so a chain aggregator can fall through to the
// next source.
func (d *Downloader) locate(base, name string) (string, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: gradlecache module directory missing: %s", downloader.ErrNotFound, base)
		}
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(base, e.Name(), name)
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: gradlecache file %q under %s", downloader.ErrNotFound, name, base)
}

func (d *Downloader) ingest(ctx context.Context, store cas.Store, pin lockfile.Pin, file lockfile.PinFile, path string) error {
	f, err := os.Open(path)
	if err != nil {
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
		return fmt.Errorf("gradlecache: ingest %s: %w", path, err)
	}
	return nil
}

// Compile-time assertion that Downloader satisfies downloader.Downloader.
var _ downloader.Downloader = (*Downloader)(nil)
