// Package cas is the Layer 3 content-addressable store for grit.
//
// Blobs are keyed by the SHA-256 of their content. Coordinates, URLs, and
// Maven-style directory trees never appear in storage paths — they live in
// provenance records and in higher-layer indexes. See
// roadmap/planning/dependency-cache-architecture.md for the architectural
// contract this package implements.
package cas

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound indicates a requested blob or provenance record does not exist.
var ErrNotFound = errors.New("cas: not found")

// ErrHashMismatch indicates bytes presented to PutExpected did not hash to
// the expected value. When PutExpected returns this error, no blob and no
// provenance are written to the store.
var ErrHashMismatch = errors.New("cas: hash mismatch")

// BlobInfo is the minimal identity of a stored blob.
type BlobInfo struct {
	Hash Hash  `json:"hash"`
	Size int64 `json:"size"`
}

// ActionResult is the persisted outcome of a Layer 4 transform action.
// It is keyed by the action hash and names the output blobs by role so
// later callers that recompute the same action hash can reuse the same
// output blobs without re-executing the action.
type ActionResult struct {
	ActionHash Hash          `json:"actionHash"`
	Outputs    []NamedOutput `json:"outputs"`
}

// NamedOutput is one role-labelled output blob of an action.
type NamedOutput struct {
	Role string   `json:"role"`
	Blob BlobInfo `json:"blob"`
}

// Output returns the first output whose Role matches role, and a bool
// reporting whether one was found. It does not allocate.
func (r ActionResult) Output(role string) (NamedOutput, bool) {
	for _, out := range r.Outputs {
		if out.Role == role {
			return out, true
		}
	}
	return NamedOutput{}, false
}

// Provenance records why a blob was written into the store.
//
// Provenance is first-writer-wins: once a blob has a provenance record, later
// writes for the same content preserve the original record. Higher layers
// that need to track multiple producers should record that separately.
type Provenance struct {
	Source     Source            `json:"source"`
	CreatedAt  time.Time         `json:"createdAt"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// SourceKind is the category of work that produced a blob.
type SourceKind string

const (
	SourceDownload  SourceKind = "download"
	SourceTransform SourceKind = "transform"
	SourceImport    SourceKind = "import"
)

// Source is a tagged union describing the primary reason a blob exists.
// Exactly one of the non-Kind fields must be populated for a valid source.
type Source struct {
	Kind      SourceKind       `json:"kind"`
	Download  *DownloadSource  `json:"download,omitempty"`
	Transform *TransformSource `json:"transform,omitempty"`
	Import    *ImportSource    `json:"import,omitempty"`
}

// DownloadSource captures an external fetch by a Layer 2 downloader.
type DownloadSource struct {
	Downloader   string `json:"downloader"`
	RepositoryID string `json:"repositoryId,omitempty"`
	URL          string `json:"url,omitempty"`
	Coordinate   string `json:"coordinate,omitempty"`
}

// TransformSource captures a Layer 4 transform action that produced a blob.
type TransformSource struct {
	ActionHash  Hash             `json:"actionHash"`
	ActionKind  string           `json:"actionKind"`
	Tool        string           `json:"tool,omitempty"`
	ToolVersion string           `json:"toolVersion,omitempty"`
	Inputs      []TransformInput `json:"inputs,omitempty"`
}

// TransformInput names one input blob to a transform action along with the
// role label that the action used to consume it.
type TransformInput struct {
	Role string `json:"role"`
	Hash Hash   `json:"hash"`
}

// ImportSource captures a direct import of user-supplied bytes that did not
// originate from a download or a transform.
type ImportSource struct {
	Path string `json:"path,omitempty"`
	Note string `json:"note,omitempty"`
}

// Store is the Layer 3 content-addressable storage boundary.
//
// Implementations must guarantee that a blob, once written, is immutable and
// identifiable solely by its content hash. They must verify content hashes on
// write and reject corrupted writes rather than quarantining them.
type Store interface {
	// Put reads r to EOF, stores the bytes under their content hash, and
	// records the given provenance if this is the first writer for the hash.
	Put(ctx context.Context, r io.Reader, prov Provenance) (BlobInfo, error)

	// PutBytes is a convenience that calls Put with an in-memory buffer.
	PutBytes(ctx context.Context, data []byte, prov Provenance) (BlobInfo, error)

	// PutExpected is like Put but verifies that the computed content hash
	// matches expected before committing the blob. If the hashes disagree,
	// PutExpected returns ErrHashMismatch and writes neither blob nor
	// provenance to the store.
	PutExpected(ctx context.Context, r io.Reader, expected Hash, prov Provenance) (BlobInfo, error)

	// PutBytesExpected is a convenience that calls PutExpected with an
	// in-memory buffer.
	PutBytesExpected(ctx context.Context, data []byte, expected Hash, prov Provenance) (BlobInfo, error)

	// Get opens a reader for the blob identified by h. Returns ErrNotFound if
	// the blob is not stored.
	Get(ctx context.Context, h Hash) (io.ReadCloser, error)

	// Stat returns blob identity without opening the bytes.
	Stat(ctx context.Context, h Hash) (BlobInfo, error)

	// Has reports whether the blob is present in the store.
	Has(ctx context.Context, h Hash) (bool, error)

	// Provenance returns the first-writer provenance record for a blob.
	// Returns ErrNotFound if the blob is not stored.
	Provenance(ctx context.Context, h Hash) (Provenance, error)

	// PutActionResult records the outputs of a Layer 4 transform action so
	// later lookups by the same action hash can skip re-execution.
	// Later writes for the same action hash overwrite earlier results:
	// callers should only call PutActionResult after they have verified
	// the result is correct, and actions that are not deterministic should
	// not be cached.
	PutActionResult(ctx context.Context, result ActionResult) error

	// GetActionResult returns the cached result for an action hash, or
	// ErrNotFound if no result has been recorded.
	GetActionResult(ctx context.Context, actionHash Hash) (ActionResult, error)
}
