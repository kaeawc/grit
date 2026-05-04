package cas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/kaeawc/grit/internal/clock"
	"github.com/kaeawc/grit/internal/fsutil"
)

// FilesystemStore is a Store backed by a local directory.
//
// Layout:
//
//	<root>/blobs/<hh>/<remaining>          — blob bytes
//	<root>/provenance/<hh>/<remaining>.json — provenance record
//
// where <hh> is the first two hex characters of the blob hash and
// <remaining> is the remaining 62 characters.
type FilesystemStore struct {
	root  string
	clock clock.Clock
}

// NewFilesystemStore returns a store rooted at dir. The directory is created
// on first write; callers do not need to pre-create it. Uses clock.System
// for provenance timestamps; tests should set Clock via NewFilesystemStoreWithClock.
func NewFilesystemStore(dir string) *FilesystemStore {
	return NewFilesystemStoreWithClock(dir, clock.System{})
}

// NewFilesystemStoreWithClock returns a FilesystemStore that uses c for
// provenance and summary timestamps. Tests pass clock.NewFake.
func NewFilesystemStoreWithClock(dir string, c clock.Clock) *FilesystemStore {
	if c == nil {
		c = clock.System{}
	}
	return &FilesystemStore{root: dir, clock: c}
}

// now returns the store's current time. Used for provenance defaulting and
// summary timestamps; preserved as a method to keep call sites short.
func (s *FilesystemStore) now() time.Time { return s.clock.Now() }

// Root returns the filesystem root of the store.
func (s *FilesystemStore) Root() string { return s.root }

func (s *FilesystemStore) blobPath(h Hash) string {
	hex := h.String()
	return filepath.Join(s.root, "blobs", hex[:2], hex[2:])
}

func (s *FilesystemStore) provenancePath(h Hash) string {
	hex := h.String()
	return filepath.Join(s.root, "provenance", hex[:2], hex[2:]+".json")
}

func (s *FilesystemStore) actionResultPath(h Hash) string {
	hex := h.String()
	return filepath.Join(s.root, "actions", hex[:2], hex[2:]+".json")
}

func (s *FilesystemStore) Put(ctx context.Context, r io.Reader, prov Provenance) (BlobInfo, error) {
	return s.put(ctx, r, nil, prov)
}

func (s *FilesystemStore) PutBytes(ctx context.Context, data []byte, prov Provenance) (BlobInfo, error) {
	return s.put(ctx, bytes.NewReader(data), nil, prov)
}

func (s *FilesystemStore) PutExpected(ctx context.Context, r io.Reader, expected Hash, prov Provenance) (BlobInfo, error) {
	return s.put(ctx, r, &expected, prov)
}

func (s *FilesystemStore) PutBytesExpected(ctx context.Context, data []byte, expected Hash, prov Provenance) (BlobInfo, error) {
	return s.put(ctx, bytes.NewReader(data), &expected, prov)
}

func (s *FilesystemStore) put(ctx context.Context, r io.Reader, expected *Hash, prov Provenance) (BlobInfo, error) {
	if err := ctx.Err(); err != nil {
		return BlobInfo{}, err
	}
	blobRoot := filepath.Join(s.root, "blobs")
	if err := os.MkdirAll(blobRoot, 0o755); err != nil {
		return BlobInfo{}, err
	}
	tmp, err := os.CreateTemp(blobRoot, ".put-*")
	if err != nil {
		return BlobInfo{}, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	h, n, copyErr := copyAndHash(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil {
		return BlobInfo{}, copyErr
	}
	if closeErr != nil {
		return BlobInfo{}, closeErr
	}
	if expected != nil && h != *expected {
		return BlobInfo{}, fmt.Errorf("%w: got %s want %s", ErrHashMismatch, h, *expected)
	}

	dest := s.blobPath(h)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return BlobInfo{}, err
	}
	if existing, err := os.Stat(dest); err == nil {
		if err := s.writeProvenanceIfMissing(h, prov); err != nil {
			return BlobInfo{}, err
		}
		return BlobInfo{Hash: h, Size: existing.Size()}, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return BlobInfo{}, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return BlobInfo{}, err
	}
	if err := s.writeProvenanceIfMissing(h, prov); err != nil {
		return BlobInfo{}, err
	}
	return BlobInfo{Hash: h, Size: n}, nil
}

func (s *FilesystemStore) Get(ctx context.Context, h Hash) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(s.blobPath(h))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (s *FilesystemStore) Stat(ctx context.Context, h Hash) (BlobInfo, error) {
	if err := ctx.Err(); err != nil {
		return BlobInfo{}, err
	}
	info, err := os.Stat(s.blobPath(h))
	if errors.Is(err, fs.ErrNotExist) {
		return BlobInfo{}, ErrNotFound
	}
	if err != nil {
		return BlobInfo{}, err
	}
	return BlobInfo{Hash: h, Size: info.Size()}, nil
}

func (s *FilesystemStore) Has(ctx context.Context, h Hash) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := os.Stat(s.blobPath(h))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (s *FilesystemStore) Provenance(ctx context.Context, h Hash) (Provenance, error) {
	if err := ctx.Err(); err != nil {
		return Provenance{}, err
	}
	data, err := os.ReadFile(s.provenancePath(h))
	if errors.Is(err, fs.ErrNotExist) {
		return Provenance{}, ErrNotFound
	}
	if err != nil {
		return Provenance{}, err
	}
	var prov Provenance
	if err := json.Unmarshal(data, &prov); err != nil {
		return Provenance{}, fmt.Errorf("cas: decode provenance for %s: %w", h, err)
	}
	return prov, nil
}

func (s *FilesystemStore) writeProvenanceIfMissing(h Hash, prov Provenance) error {
	path := s.provenancePath(h)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if prov.CreatedAt.IsZero() {
		prov.CreatedAt = s.now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(prov, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, encoded, 0o644)
}

func (s *FilesystemStore) PutActionResult(ctx context.Context, result ActionResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if result.ActionHash.IsZero() {
		return fmt.Errorf("cas: PutActionResult: zero action hash")
	}
	path := s.actionResultPath(result.ActionHash)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, encoded, 0o644)
}

func (s *FilesystemStore) GetActionResult(ctx context.Context, actionHash Hash) (ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return ActionResult{}, err
	}
	data, err := os.ReadFile(s.actionResultPath(actionHash))
	if errors.Is(err, fs.ErrNotExist) {
		return ActionResult{}, ErrNotFound
	}
	if err != nil {
		return ActionResult{}, err
	}
	var result ActionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return ActionResult{}, fmt.Errorf("cas: decode action result for %s: %w", actionHash, err)
	}
	return result, nil
}

// ListBlobs returns every blob in the store along with its provenance
// timestamp. Blobs whose provenance cannot be read use the zero time.
func (s *FilesystemStore) ListBlobs(ctx context.Context) ([]BlobEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	blobRoot := filepath.Join(s.root, "blobs")
	var entries []BlobEntry
	err := filepath.WalkDir(blobRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip temp files created during put.
		if name := d.Name(); len(name) > 0 && name[0] == '.' {
			return nil
		}
		rel, err := filepath.Rel(blobRoot, path)
		if err != nil {
			return err
		}
		// rel is "<hh>/<remaining>" — reconstruct the 64-char hex hash.
		dir := filepath.Dir(rel)
		base := filepath.Base(rel)
		hexStr := dir + base
		h, err := ParseHash(hexStr)
		if err != nil {
			return nil // skip unparseable entries
		}
		info, err := d.Info()
		if err != nil {
			return nil // skip unreadable entries
		}
		var createdAt time.Time
		prov, provErr := s.Provenance(ctx, h)
		if provErr == nil {
			createdAt = prov.CreatedAt
		}
		entries = append(entries, BlobEntry{Hash: h, Size: info.Size(), CreatedAt: createdAt})
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

// BlobEntry is a blob with its metadata, used for listing and retention.
type BlobEntry struct {
	Hash      Hash
	Size      int64
	CreatedAt time.Time
}

// RemoveBlob removes a blob and its provenance record from the store.
func (s *FilesystemStore) RemoveBlob(ctx context.Context, h Hash) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	blobPath := s.blobPath(h)
	if err := os.Remove(blobPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	provPath := s.provenancePath(h)
	if err := os.Remove(provPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func copyAndHash(w io.Writer, r io.Reader) (Hash, int64, error) {
	h := sha256.New()
	tee := io.TeeReader(r, h)
	n, err := io.Copy(w, tee)
	if err != nil {
		return Hash{}, 0, err
	}
	var out Hash
	copy(out[:], h.Sum(nil))
	return out, n, nil
}
