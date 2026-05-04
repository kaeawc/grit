package nativecompile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheAccountingForRootCountsKnownBuckets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "compile", "abc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "compile", "abc", "classes.jar"), []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "unit-test-run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unit-test-run", "result.stamp"), []byte("stamp"), 0o644); err != nil {
		t.Fatal(err)
	}

	accounting := cacheAccountingForRoot(root)
	if accounting.Root != root {
		t.Fatalf("unexpected cache root: %#v", accounting)
	}
	if accounting.Files != 2 || accounting.Bytes != int64(len("jar")+len("stamp")) {
		t.Fatalf("unexpected aggregate accounting: %#v", accounting)
	}
	if len(accounting.Buckets) != len(sharedNativeCacheBuckets) {
		t.Fatalf("unexpected bucket count: got %d want %d", len(accounting.Buckets), len(sharedNativeCacheBuckets))
	}
	var compileBucket, runBucket bool
	for _, bucket := range accounting.Buckets {
		switch bucket.Name {
		case "compile":
			compileBucket = true
			if bucket.Files != 1 || bucket.Bytes != int64(len("jar")) {
				t.Fatalf("unexpected compile bucket accounting: %#v", bucket)
			}
		case "unit-test-run":
			runBucket = true
			if bucket.Files != 1 || bucket.Bytes != int64(len("stamp")) {
				t.Fatalf("unexpected unit-test-run bucket accounting: %#v", bucket)
			}
		}
	}
	if !compileBucket || !runBucket {
		t.Fatalf("expected known buckets to be reported, got %#v", accounting.Buckets)
	}
}
