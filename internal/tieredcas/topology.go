package tieredcas

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/remotecache"
)

// StandardTopology builds the canonical 3-tier topology used by grit's
// production cache wiring:
//
//  1. worktree-local CAS at <repoRoot>/.grit/cas/
//  2. shared-local CAS at <userCacheDir>/grit/cas/
//  3. remote cache via remotecache.Client (omitted if remoteClient is nil)
//
// Promoting results from worktree-local up only on success keeps
// concurrent worktrees from colliding through the shared store.
//
// repoRoot must be non-empty. userCacheDir is resolved by the OS
// convention (os.UserCacheDir) when empty. Returns an error if the
// directories cannot be created.
func StandardTopology(repoRoot, userCacheDir string, remoteClient *remotecache.Client) (*Store, error) {
	if repoRoot == "" {
		return nil, errors.New("tieredcas: StandardTopology: repoRoot is required")
	}

	worktreeRoot := filepath.Join(repoRoot, ".grit", "cas")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		return nil, fmt.Errorf("tieredcas: prepare worktree CAS: %w", err)
	}
	worktree := cas.NewFilesystemStore(worktreeRoot)

	if userCacheDir == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("tieredcas: locate user cache dir: %w", err)
		}
		userCacheDir = dir
	}
	sharedRoot := filepath.Join(userCacheDir, "grit", "cas")
	if err := os.MkdirAll(sharedRoot, 0o755); err != nil {
		return nil, fmt.Errorf("tieredcas: prepare shared CAS: %w", err)
	}
	shared := cas.NewFilesystemStore(sharedRoot)

	if remoteClient == nil {
		return New(worktree, shared)
	}
	return New(worktree, shared, remotecache.NewStore(remoteClient))
}
