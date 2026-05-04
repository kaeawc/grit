// Package downloader defines the Layer 2 contract for ecosystem-coupled
// artifact fetchers.
//
// A downloader reads a lockfile pin, fetches every file it names, verifies
// the content against the pin's declared SHA-256, and lands the bytes in a
// content-addressable store. Downloaders never write to coordinate-shaped
// directory trees and never own cache layout beyond the CAS they were
// handed. See roadmap/planning/dependency-cache-architecture.md for the
// full layer contract.
package downloader

import (
	"context"
	"errors"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

// ErrNotFound is the shared sentinel for "this source does not have the
// requested file". Layer 2 adapters wrap it so compositions higher up
// (e.g. internal/downloader/chain) can use errors.Is for fall-through
// semantics. A Fetch call that returns ErrNotFound is an invitation to
// try the next source; any other error is a hard stop.
var ErrNotFound = errors.New("downloader: file not found")

// Downloader is one ecosystem-coupled source of artifact bytes.
//
// Implementations fetch every file declared in a lockfile pin, verify the
// bytes against the pin's declared SHA-256, and land them in the given
// store. Fetch must be idempotent: a call in which every pinned file is
// already present in the store must be a no-op that returns nil.
type Downloader interface {
	// ID returns a stable identifier for this downloader implementation.
	// The ID is recorded in provenance so cache-debugging can explain
	// which downloader served a particular blob.
	ID() string

	// Fetch materializes every file named by pin into store. Returns a
	// non-nil error if any file cannot be located, is missing from the
	// source, or fails hash verification.
	Fetch(ctx context.Context, pin lockfile.Pin, store cas.Store) error
}
