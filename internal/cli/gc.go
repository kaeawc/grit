package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/cas/retention"
	"github.com/kaeawc/grit/internal/perf"
)

func runGC(ctx context.Context, args []string, stdout, stderr io.Writer, tracker perf.Tracker, start time.Time) int {
	cmd := newCommandState("gc", stdout, stderr, tracker, start)
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cacheDir := fs.String("cache-dir", "", "Path to CAS cache directory (required)")
	maxAge := fs.Duration("max-age", 0, "Evict blobs older than this duration (e.g. 720h)")
	maxSize := fs.Int64("max-size", 0, "Evict oldest blobs until total size is at or below this byte count")
	if err := fs.Parse(args); err != nil {
		return cmd.fail(2, err)
	}
	if *cacheDir == "" {
		return cmd.fail(2, errors.New("--cache-dir is required"))
	}

	store := cas.NewFilesystemStore(*cacheDir)
	policy := retention.Policy{
		MaxAge:  *maxAge,
		MaxSize: *maxSize,
	}

	var report retention.EvictionReport
	if err := tracker.Track("gc", func() error {
		var sweepErr error
		report, sweepErr = retention.Sweep(ctx, store, policy, time.Now())
		return sweepErr
	}); err != nil {
		return cmd.fail(1, err)
	}

	return cmd.success(resultJSON(report))
}
