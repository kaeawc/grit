package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
)

func TestGCCommandRequiresCacheDir(t *testing.T) {
	var stdout, stderr strings.Builder
	exit := Run(context.Background(), []string{"gc"}, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("expected non-zero exit, got 0; stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String()+stdout.String(), "cache-dir") {
		t.Fatalf("expected cache-dir error, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestGCCommandEvictsByMaxAge(t *testing.T) {
	dir := t.TempDir()
	store := cas.NewFilesystemStore(dir)
	ctx := context.Background()

	old, err := store.PutBytes(ctx, []byte("stale"), cas.Provenance{
		Source:    cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "old"}},
		CreatedAt: time.Now().Add(-30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("PutBytes old: %v", err)
	}
	fresh, err := store.PutBytes(ctx, []byte("fresh"), cas.Provenance{
		Source:    cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "fresh"}},
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("PutBytes fresh: %v", err)
	}

	var stdout, stderr strings.Builder
	exit := Run(ctx, []string{"gc", "--cache-dir", dir, "--max-age", "168h"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("gc exited %d: stderr=%q", exit, stderr.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			BlobsRemoved int   `json:"blobsRemoved"`
			BytesFreed   int64 `json:"bytesFreed"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &resp); err != nil {
		t.Fatalf("decode response: %v\n%s", err, stdout.String())
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got %s", stdout.String())
	}
	if resp.Result.BlobsRemoved != 1 {
		t.Fatalf("BlobsRemoved=%d want 1", resp.Result.BlobsRemoved)
	}
	if resp.Result.BytesFreed != int64(len("stale")) {
		t.Fatalf("BytesFreed=%d want %d", resp.Result.BytesFreed, len("stale"))
	}

	if has, err := store.Has(ctx, old.Hash); err != nil || has {
		t.Fatalf("old blob should be gone: has=%v err=%v", has, err)
	}
	if has, err := store.Has(ctx, fresh.Hash); err != nil || !has {
		t.Fatalf("fresh blob should remain: has=%v err=%v", has, err)
	}
}
