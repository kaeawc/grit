// Package retention implements age-based, size-based, and free-space-based
// eviction for the CAS.
package retention

import (
	"context"
	"sort"
	"time"

	"github.com/kaeawc/grit/internal/cas"
)

// Policy controls which blobs are eligible for eviction.
type Policy struct {
	// MaxAge evicts blobs whose provenance CreatedAt is older than this duration.
	// Zero means no age limit.
	MaxAge time.Duration

	// MaxSize evicts the oldest blobs until total store size is at or below this
	// byte count. Zero means no size limit.
	MaxSize int64

	// MinFreeSpace evicts the oldest blobs until the filesystem has at least this
	// many free bytes available. Zero means no free-space target. The available
	// disk space is provided by FreeSpaceFn; when nil this dimension is skipped.
	MinFreeSpace int64

	// FreeSpaceFn returns the number of free bytes on the filesystem backing the
	// store. Injected so tests don't need a real disk. When nil and MinFreeSpace
	// is set, MinFreeSpace is silently skipped.
	FreeSpaceFn func() (int64, error)
}

// EvictionReport summarises the result of a Sweep.
type EvictionReport struct {
	BlobsRemoved int   `json:"blobsRemoved"`
	BytesFreed   int64 `json:"bytesFreed"`
}

// Sweeper lists and removes blobs from a store. FilesystemStore implements this.
type Sweeper interface {
	ListBlobs(ctx context.Context) ([]cas.BlobEntry, error)
	RemoveBlob(ctx context.Context, h cas.Hash) error
}

// Sweep removes blobs that violate the given policy from the store.
// It processes age-based eviction first, then free-space-based eviction, then
// size-based eviction on whatever remains. Blobs are evicted in oldest-first
// order at each phase.
func Sweep(ctx context.Context, sw Sweeper, policy Policy, now time.Time) (EvictionReport, error) {
	blobs, err := sw.ListBlobs(ctx)
	if err != nil {
		return EvictionReport{}, err
	}

	// Sort oldest first.
	sort.Slice(blobs, func(i, j int) bool {
		return blobs[i].CreatedAt.Before(blobs[j].CreatedAt)
	})

	var report EvictionReport
	evicted := make(map[cas.Hash]bool)

	// Phase 1: age-based eviction.
	if policy.MaxAge > 0 {
		cutoff := now.Add(-policy.MaxAge)
		for _, b := range blobs {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			if b.CreatedAt.Before(cutoff) {
				if err := sw.RemoveBlob(ctx, b.Hash); err != nil {
					return report, err
				}
				evicted[b.Hash] = true
				report.BlobsRemoved++
				report.BytesFreed += b.Size
			}
		}
	}

	// Phase 2: free-space-based eviction. Run before size-based eviction so a
	// MinFreeSpace shortfall is corrected first; remaining MaxSize trimming
	// then operates on whatever survives.
	if policy.MinFreeSpace > 0 && policy.FreeSpaceFn != nil {
		free, freeErr := policy.FreeSpaceFn()
		if freeErr != nil {
			return report, freeErr
		}
		for _, b := range blobs {
			if free >= policy.MinFreeSpace {
				break
			}
			if err := ctx.Err(); err != nil {
				return report, err
			}
			if evicted[b.Hash] {
				continue
			}
			if err := sw.RemoveBlob(ctx, b.Hash); err != nil {
				return report, err
			}
			evicted[b.Hash] = true
			report.BlobsRemoved++
			report.BytesFreed += b.Size
			free += b.Size
		}
	}

	// Phase 3: size-based eviction.
	if policy.MaxSize > 0 {
		var totalSize int64
		for _, b := range blobs {
			if !evicted[b.Hash] {
				totalSize += b.Size
			}
		}
		for _, b := range blobs {
			if totalSize <= policy.MaxSize {
				break
			}
			if err := ctx.Err(); err != nil {
				return report, err
			}
			if evicted[b.Hash] {
				continue
			}
			if err := sw.RemoveBlob(ctx, b.Hash); err != nil {
				return report, err
			}
			evicted[b.Hash] = true
			report.BlobsRemoved++
			report.BytesFreed += b.Size
			totalSize -= b.Size
		}
	}

	return report, nil
}
