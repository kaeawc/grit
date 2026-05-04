package tieredcas

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
)

func TestStandardTopologyTwoTier(t *testing.T) {
	repo := t.TempDir()
	userCache := t.TempDir()

	store, err := StandardTopology(repo, userCache, nil)
	if err != nil {
		t.Fatalf("StandardTopology: %v", err)
	}
	tiers := store.Tiers()
	if len(tiers) != 2 {
		t.Fatalf("expected 2 tiers when remoteClient is nil, got %d", len(tiers))
	}
	if _, err := os.Stat(filepath.Join(repo, ".grit", "cas")); err != nil {
		t.Errorf("worktree CAS dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(userCache, "grit", "cas")); err != nil {
		t.Errorf("shared CAS dir missing: %v", err)
	}
}

func TestStandardTopologyWritesGoToWorktreeTier(t *testing.T) {
	repo := t.TempDir()
	userCache := t.TempDir()

	store, err := StandardTopology(repo, userCache, nil)
	if err != nil {
		t.Fatalf("StandardTopology: %v", err)
	}
	ctx := context.Background()
	info, err := store.PutBytes(ctx, []byte("payload"), cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "test"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	worktreeRoot := filepath.Join(repo, ".grit", "cas")
	worktree := cas.NewFilesystemStore(worktreeRoot)
	if has, err := worktree.Has(ctx, info.Hash); err != nil || !has {
		t.Fatalf("expected blob in worktree CAS: has=%v err=%v", has, err)
	}

	sharedRoot := filepath.Join(userCache, "grit", "cas")
	shared := cas.NewFilesystemStore(sharedRoot)
	if has, _ := shared.Has(ctx, info.Hash); has {
		t.Fatal("write should not have promoted to shared tier on Put")
	}
}

func TestStandardTopologyRequiresRepoRoot(t *testing.T) {
	_, err := StandardTopology("", "", nil)
	if err == nil {
		t.Fatal("expected error when repoRoot is empty")
	}
}
