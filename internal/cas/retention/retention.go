// Package retention implements age-based and size-based eviction for the CAS.
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
// It processes age-based eviction first, then size-based eviction on whatever
// remains. Blobs are evicted in oldest-first order.
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

	// Phase 2: size-based eviction.
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
