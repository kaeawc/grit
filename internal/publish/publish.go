// Package publish defines the Layer 5 contract for publish adapters.
//
// A publisher is a one-way projection from the CAS to a target sink
// (a local directory, a remote repository, a registry). Publishers never
// read from their output sink as authoritative state: the CAS is always
// the source of truth. See roadmap/planning/dependency-cache-architecture.md
// for the architectural role.
package publish

import (
	"context"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

// Publisher is one output target for materializing CAS blobs as
// externally-visible artifacts.
//
// Implementations must be idempotent: publishing the same pin twice
// against the same sink must leave it in the same state. Publishers may
// overwrite existing files at the target path, but must do so atomically
// where the underlying sink supports it.
type Publisher interface {
	// ID returns a stable identifier for this publisher implementation.
	ID() string

	// PublishPin materializes every file named by pin into the publisher's
	// sink, reading bytes from store. Returns a non-nil error if any blob
	// referenced by pin is missing from the store or any target write
	// fails.
	PublishPin(ctx context.Context, pin lockfile.Pin, store cas.Store) error
}
