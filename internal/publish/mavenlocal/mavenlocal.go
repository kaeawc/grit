// Package mavenlocal implements a Layer 5 publish adapter that writes
// CAS blobs out to a standard Maven local repository layout.
//
// The target is a directory in Maven layout, typically at ~/.m2/repository.
// For each file named in a lockfile pin, the publisher materializes the
// CAS blob at
//
//	<root>/<group-slashed>/<artifact>/<version>/<name>
//
// and writes Maven-convention checksum sidecars (.sha1 and .md5) alongside.
// Every file write is atomic via temp-file-then-rename so concurrent Maven
// readers never see a half-written artifact.
//
// In addition to exact-GAV files, the publisher maintains
// maven-metadata-local.xml at the artifact directory so version-range and
// LATEST/RELEASE lookups can discover locally published versions.
//
// See roadmap/in-progress/maven-local-support.md and
// roadmap/in-progress/dependency-cache-architecture.md for the role this
// publisher plays in the overall layer contract.
package mavenlocal

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	readadapter "github.com/kaeawc/grit/internal/downloader/mavenlocal"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/mavenlocalroot"
	"github.com/kaeawc/grit/internal/publish"
)

// ID is the stable identifier for this publisher.
const ID = "maven-local"

// DefaultRoot returns the conventional Maven local repository path under
// the user's home directory, or an empty string if HOME is unset.
func DefaultRoot() string {
	return mavenlocalroot.Default()
}

// Publisher materializes CAS blobs as Maven-layout files under a local root.
type Publisher struct {
	root string
}

// New returns a Publisher rooted at dir. The directory is created on
// first publish; callers do not need to pre-create it.
func New(dir string) *Publisher {
	return &Publisher{root: dir}
}

// Root returns the repository root this publisher writes to.
func (p *Publisher) Root() string { return p.root }

// ID implements publish.Publisher.
func (p *Publisher) ID() string { return ID }

// PublishPin writes every file named in pin to the repository. Each file
// is written atomically (temp + rename) and accompanied by .sha1 and .md5
// checksum sidecars computed in the same streaming pass.
func (p *Publisher) PublishPin(ctx context.Context, pin lockfile.Pin, store cas.Store) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := p.moduleBasePath(pin.Coordinate)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mavenlocal publish: mkdir %s: %w", dir, err)
	}
	for _, file := range pin.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(dir, file.Name)
		if err := p.publishBlob(ctx, store, file.Hash, target); err != nil {
			return fmt.Errorf("mavenlocal publish %s: %w", pin.Coordinate, err)
		}
	}
	if err := p.publishGeneratedPom(pin); err != nil {
		return fmt.Errorf("mavenlocal publish pom %s: %w", pin.Coordinate, err)
	}
	if err := p.publishGeneratedModule(pin); err != nil {
		return fmt.Errorf("mavenlocal publish gradle module %s: %w", pin.Coordinate, err)
	}
	if err := p.publishArtifactMetadata(pin.Coordinate); err != nil {
		return fmt.Errorf("mavenlocal publish metadata %s: %w", pin.Coordinate, err)
	}
	if err := p.publishRemoteRepositoriesMarker(pin); err != nil {
		return fmt.Errorf("mavenlocal publish marker %s: %w", pin.Coordinate, err)
	}
	return nil
}

func (p *Publisher) moduleBasePath(coord lockfile.Coordinate) string {
	return filepath.Join(p.root, readadapter.GroupPath(coord.Group), coord.Artifact, coord.Version)
}

func (p *Publisher) publishBlob(ctx context.Context, store cas.Store, blobHash cas.Hash, target string) error {
	rc, err := store.Get(ctx, blobHash)
	if err != nil {
		return fmt.Errorf("get blob %s: %w", blobHash, err)
	}
	defer func() { _ = rc.Close() }()

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".publish-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	// Maven clients expect SHA-1 and MD5 checksum sidecars. These are
	// weak hashes by modern standards; grit does not rely on them for
	// trust (the real identity is the SHA-256 in the CAS). They are
	// written for ecosystem compatibility only.
	sh1 := sha1.New()
	m5 := md5.New()
	mw := io.MultiWriter(tmp, sh1, m5)

	if _, err := io.Copy(mw, rc); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy blob %s: %w", blobHash, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("rename %s: %w", target, err)
	}
	if err := writeSidecar(target+".sha1", sh1); err != nil {
		return err
	}
	if err := writeSidecar(target+".md5", m5); err != nil {
		return err
	}
	return nil
}

func writeFileAtomically(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".publish-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func writeBytesWithSidecars(path string, data []byte) error {
	if err := writeFileAtomically(path, data); err != nil {
		return err
	}
	sh1 := sha1.New()
	if _, err := sh1.Write(data); err != nil {
		return err
	}
	if err := writeSidecar(path+".sha1", sh1); err != nil {
		return err
	}
	m5 := md5.New()
	if _, err := m5.Write(data); err != nil {
		return err
	}
	if err := writeSidecar(path+".md5", m5); err != nil {
		return err
	}
	return nil
}

// writeSidecar writes the hex digest of h to path atomically. Maven
// checksum files contain just the hex digest, with no trailing newline.
func writeSidecar(path string, h hash.Hash) error {
	digest := hex.EncodeToString(h.Sum(nil))
	return writeFileAtomically(path, []byte(digest))
}

// Compile-time assertion that *Publisher satisfies publish.Publisher.
var _ publish.Publisher = (*Publisher)(nil)
