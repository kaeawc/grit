// Package depcache wires the dependency-cache subsystem: a CAS store
// (single-tier or tiered), a downloader chain over configured
// repositories, a Maven Local publisher, and a transform runner for
// AAR extraction.
//
// Callers compose a Stack once and use it to fetch lockfile pins into
// the CAS, publish them into the Maven Local layout, and run cached
// transforms on extracted artifacts. Project-layout concerns (where
// the worktree CAS lives, where AAR projections are materialized) are
// the caller's responsibility — Stack only deals in roots passed via
// Config.
package depcache

import (
	"context"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/downloader/chain"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/project"
	mavenpublish "github.com/kaeawc/grit/internal/publish/mavenlocal"
	"github.com/kaeawc/grit/internal/tieredcas"
	"github.com/kaeawc/grit/internal/transform/aarextract"
)

type Stack struct {
	Store      cas.Store
	Tiered     *tieredcas.Store
	Downloader downloader.Downloader
	Publisher  *mavenpublish.Publisher
}

type Config struct {
	WorktreeCASRoot string
	SharedCASRoot   string
	PublishRoot     string
	GradleCacheRoot string
	Repositories    []project.Repository
}

func New(cfg Config) (*Stack, error) {
	worktreeStore := cas.NewFilesystemStore(cfg.WorktreeCASRoot)
	tiers := []cas.Store{worktreeStore}
	if cfg.SharedCASRoot != "" {
		tiers = append(tiers, cas.NewFilesystemStore(cfg.SharedCASRoot))
	}
	tieredStore, err := tieredcas.New(tiers...)
	if err != nil {
		tieredStore = nil
	}
	var store cas.Store
	if tieredStore != nil {
		store = tieredStore
	} else {
		store = worktreeStore
	}
	chainDownloader, err := chain.New(SourceDownloaders(cfg.Repositories, cfg.GradleCacheRoot))
	if err != nil {
		chainDownloader = nil
	}
	return &Stack{
		Store:      store,
		Tiered:     tieredStore,
		Downloader: asDownloader(chainDownloader),
		Publisher:  mavenpublish.New(cfg.PublishRoot),
	}, nil
}

func (s *Stack) Fetch(ctx context.Context, pin lockfile.Pin) error {
	if s == nil || s.Downloader == nil || s.Store == nil {
		return nil
	}
	return s.Downloader.Fetch(ctx, pin, s.Store)
}

func (s *Stack) Publish(ctx context.Context, pin lockfile.Pin) error {
	if s == nil || s.Publisher == nil || s.Store == nil {
		return nil
	}
	return s.Publisher.PublishPin(ctx, pin, s.Store)
}

// Extract runs the aar-extract action through the tiered cache when one
// is configured (every extract gets a CacheSummary sidecar and probes/
// promotes via the tier chain), and falls back to a direct extract
// against the underlying store otherwise.
func (s *Stack) Extract(ctx context.Context, primaryHash cas.Hash) (cas.ActionResult, error) {
	if s.Tiered != nil {
		runner := &aarextract.CachedRunner{Store: s.Tiered}
		return runner.Run(ctx, primaryHash)
	}
	return aarextract.Extract(ctx, s.Store, primaryHash)
}

func asDownloader(c *chain.Downloader) downloader.Downloader {
	if c == nil {
		return nil
	}
	return c
}
