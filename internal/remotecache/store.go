package remotecache

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/kaeawc/grit/internal/cas"
)

// Store adapts a Client to the cas.Store interface so a remote cache can
// participate in tiered composition alongside local stores.
//
// The adapter has three inherent limitations that callers need to know:
//
//  1. Remote stores do not track provenance. The provenance argument to
//     Put, PutBytes, PutExpected, and PutBytesExpected is accepted for
//     interface compatibility but never transmitted. Provenance queries
//     always return cas.ErrNotFound.
//  2. Stat returns Size: 0 for present blobs because the wire protocol's
//     HEAD response does not advertise size. Callers that need the size
//     must Get the blob and measure its bytes.
//  3. Put and PutExpected buffer the reader fully in memory before
//     uploading so the hash can be verified. Streaming uploads of very
//     large artifacts are a future optimization on the underlying
//     Client, not on this adapter.
type Store struct {
	client *Client
}

// NewStore returns a Store backed by client.
func NewStore(client *Client) *Store {
	return &Store{client: client}
}

// Put implements cas.Store. Provenance is accepted for interface
// compatibility but not transmitted to the server.
func (s *Store) Put(ctx context.Context, r io.Reader, _ cas.Provenance) (cas.BlobInfo, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return cas.BlobInfo{}, err
	}
	hash := cas.HashBytes(data)
	if err := s.client.PutBlob(ctx, hash, data); err != nil {
		return cas.BlobInfo{}, err
	}
	return cas.BlobInfo{Hash: hash, Size: int64(len(data))}, nil
}

// PutBytes implements cas.Store.
func (s *Store) PutBytes(ctx context.Context, data []byte, _ cas.Provenance) (cas.BlobInfo, error) {
	hash := cas.HashBytes(data)
	if err := s.client.PutBlob(ctx, hash, data); err != nil {
		return cas.BlobInfo{}, err
	}
	return cas.BlobInfo{Hash: hash, Size: int64(len(data))}, nil
}

// PutExpected implements cas.Store. Returns cas.ErrHashMismatch if the
// read bytes do not hash to expected; in that case no network request is
// made.
func (s *Store) PutExpected(ctx context.Context, r io.Reader, expected cas.Hash, _ cas.Provenance) (cas.BlobInfo, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return cas.BlobInfo{}, err
	}
	if got := cas.HashBytes(data); got != expected {
		return cas.BlobInfo{}, fmt.Errorf("%w: got %s want %s", cas.ErrHashMismatch, got, expected)
	}
	if err := s.client.PutBlob(ctx, expected, data); err != nil {
		return cas.BlobInfo{}, err
	}
	return cas.BlobInfo{Hash: expected, Size: int64(len(data))}, nil
}

// PutBytesExpected implements cas.Store.
func (s *Store) PutBytesExpected(ctx context.Context, data []byte, expected cas.Hash, _ cas.Provenance) (cas.BlobInfo, error) {
	if got := cas.HashBytes(data); got != expected {
		return cas.BlobInfo{}, fmt.Errorf("%w: got %s want %s", cas.ErrHashMismatch, got, expected)
	}
	if err := s.client.PutBlob(ctx, expected, data); err != nil {
		return cas.BlobInfo{}, err
	}
	return cas.BlobInfo{Hash: expected, Size: int64(len(data))}, nil
}

// Get implements cas.Store. The returned ReadCloser holds the full blob
// in memory; callers that read partial content still pay the full
// download cost.
func (s *Store) Get(ctx context.Context, h cas.Hash) (io.ReadCloser, error) {
	data, err := s.client.GetBlob(ctx, h)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Stat implements cas.Store. Present blobs return Size: 0 because the
// remote-cache wire protocol does not advertise size via HEAD.
func (s *Store) Stat(ctx context.Context, h cas.Hash) (cas.BlobInfo, error) {
	has, err := s.client.HasBlob(ctx, h)
	if err != nil {
		return cas.BlobInfo{}, err
	}
	if !has {
		return cas.BlobInfo{}, cas.ErrNotFound
	}
	return cas.BlobInfo{Hash: h, Size: 0}, nil
}

// Has implements cas.Store.
func (s *Store) Has(ctx context.Context, h cas.Hash) (bool, error) {
	return s.client.HasBlob(ctx, h)
}

// HasActionResult returns true if the remote cache claims to have the
// action result identified by actionHash.
func (s *Store) HasActionResult(ctx context.Context, actionHash cas.Hash) (bool, error) {
	return s.client.HasActionResult(ctx, actionHash)
}

// Provenance implements cas.Store. Remote stores do not track provenance,
// so this method always returns cas.ErrNotFound.
func (s *Store) Provenance(_ context.Context, _ cas.Hash) (cas.Provenance, error) {
	return cas.Provenance{}, cas.ErrNotFound
}

// PutActionResult implements cas.Store.
func (s *Store) PutActionResult(ctx context.Context, result cas.ActionResult) error {
	return s.client.PutActionResult(ctx, result)
}

// GetActionResult implements cas.Store.
func (s *Store) GetActionResult(ctx context.Context, actionHash cas.Hash) (cas.ActionResult, error) {
	return s.client.GetActionResult(ctx, actionHash)
}

// Compile-time assertion that *Store satisfies cas.Store.
var _ cas.Store = (*Store)(nil)
