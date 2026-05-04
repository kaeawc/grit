package dependencywiring

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kaeawc/grit/internal/fsutil"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/m2local"
)

// lockfileDir returns the directory where the persisted lockfile lives.
func lockfileDir(workRoot string) string {
	return filepath.Join(workRoot, ".grit", "worktree")
}

// lockfilePath returns the filesystem path for the persisted lockfile.
func lockfilePath(workRoot string) string {
	return filepath.Join(lockfileDir(workRoot), "dependencies.lock.json")
}

// lockfileMetaPath returns the path to the companion metadata file that
// records the input digest used to produce the lockfile.
func lockfileMetaPath(workRoot string) string {
	return filepath.Join(lockfileDir(workRoot), "dependencies.lock.meta.json")
}

// lockfileMeta is the companion metadata persisted alongside the lockfile
// so we can validate whether the lockfile is still current without
// re-hashing every file on disk.
type lockfileMeta struct {
	// InputDigest is a SHA-256 hex digest over the sorted jar paths and
	// android library IDs from the m2local.Resolved output that produced
	// this lockfile. If the resolved output changes, the digest changes,
	// and the cached lockfile is stale.
	InputDigest string    `json:"inputDigest"`
	SavedAt     time.Time `json:"savedAt"`
}

// resolvedInputDigest computes a deterministic digest over the paths and
// library IDs from a Resolved value. This captures the shape of the
// resolution so downstream code can detect when the lockfile is stale.
func resolvedInputDigest(resolved *m2local.Resolved) string {
	if resolved == nil {
		return ""
	}
	h := sha256.New()
	paths := make([]string, 0, len(resolved.CompileJars)+len(resolved.RuntimeJars)+len(resolved.TestJars)+len(resolved.AndroidLibraries))
	paths = append(paths, resolved.CompileJars...)
	paths = append(paths, resolved.RuntimeJars...)
	paths = append(paths, resolved.TestJars...)
	for _, lib := range resolved.AndroidLibraries {
		paths = append(paths, lib.ID)
	}
	slices.Sort(paths)
	for _, p := range paths {
		_, _ = fmt.Fprintf(h, "%s\n", p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// saveLockfile atomically writes a lockfile and its companion metadata.
func saveLockfile(workRoot string, lf lockfile.Lockfile, resolved *m2local.Resolved) error {
	dir := lockfileDir(workRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Write lockfile atomically.
	var lfBuf bytes.Buffer
	if err := lf.Encode(&lfBuf); err != nil {
		return fmt.Errorf("encode lockfile: %w", err)
	}
	if err := atomicWriteFile(lockfilePath(workRoot), lfBuf.Bytes()); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}

	// Write companion metadata.
	meta := lockfileMeta{
		InputDigest: resolvedInputDigest(resolved),
		SavedAt:     time.Now().UTC(),
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal lockfile meta: %w", err)
	}
	if err := atomicWriteFile(lockfileMetaPath(workRoot), metaBytes); err != nil {
		return fmt.Errorf("write lockfile meta: %w", err)
	}
	return nil
}

// loadCachedLockfile loads a previously persisted lockfile if the input
// digest matches the current resolved output. Returns the lockfile and
// true on cache hit, or a zero lockfile and false on miss.
func loadCachedLockfile(workRoot string, resolved *m2local.Resolved) (lockfile.Lockfile, bool) {
	metaData, err := os.ReadFile(lockfileMetaPath(workRoot))
	if err != nil {
		return lockfile.Lockfile{}, false
	}
	var meta lockfileMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return lockfile.Lockfile{}, false
	}
	if meta.InputDigest == "" || meta.InputDigest != resolvedInputDigest(resolved) {
		return lockfile.Lockfile{}, false
	}
	lfData, err := os.Open(lockfilePath(workRoot))
	if err != nil {
		return lockfile.Lockfile{}, false
	}
	defer func() { _ = lfData.Close() }()
	lf, err := lockfile.Decode(lfData)
	if err != nil {
		return lockfile.Lockfile{}, false
	}
	if len(lf.Pins) == 0 {
		return lockfile.Lockfile{}, false
	}
	return lf, true
}

// atomicWriteFile writes data to path via a temporary file and rename.
func atomicWriteFile(path string, data []byte) error {
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

// LockfilePath returns the persisted lockfile path for external consumers
// (service introspection, CLI).
func LockfilePath(workRoot string) string {
	if strings.TrimSpace(workRoot) == "" {
		return ""
	}
	return lockfilePath(workRoot)
}
