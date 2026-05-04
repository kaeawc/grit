package gradlecache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
)

func TestFetchRoundTrip(t *testing.T) {
	gradleRoot := t.TempDir()
	casRoot := t.TempDir()

	payload := []byte("pretend jar bytes")
	hash := cas.HashBytes(payload)

	writeCacheFile(t, gradleRoot, "org.example", "alpha", "1.0", "abc123", "alpha-1.0.jar", payload)

	d := New(gradleRoot)
	if d.ID() != ID {
		t.Fatalf("ID mismatch: %s", d.ID())
	}

	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		RepositoryID: "central",
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Size: int64(len(payload)), Hash: hash},
		},
	}
	if err := d.Fetch(context.Background(), pin, store); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	has, err := store.Has(context.Background(), hash)
	if err != nil || !has {
		t.Fatalf("blob not present after Fetch: has=%v err=%v", has, err)
	}

	prov, err := store.Provenance(context.Background(), hash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Download == nil || prov.Source.Download.Downloader != ID {
		t.Fatalf("unexpected provenance source: %+v", prov.Source)
	}
	if prov.Source.Download.Coordinate != "org.example:alpha:1.0" {
		t.Fatalf("coordinate not preserved: %s", prov.Source.Download.Coordinate)
	}
	if prov.Source.Download.RepositoryID != "central" {
		t.Fatalf("repository id not preserved: %s", prov.Source.Download.RepositoryID)
	}
	if prov.Attributes["file.kind"] != string(lockfile.FileKindPrimary) {
		t.Fatalf("file.kind attribute missing: %+v", prov.Attributes)
	}
}

func TestFetchRejectsHashMismatch(t *testing.T) {
	gradleRoot := t.TempDir()
	casRoot := t.TempDir()

	actual := []byte("real content")
	wrongHash := cas.HashBytes([]byte("tampered expectation"))
	writeCacheFile(t, gradleRoot, "org.example", "beta", "2.0", "sub", "beta-2.0.jar", actual)

	d := New(gradleRoot)
	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "beta", Version: "2.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "beta-2.0.jar", Size: int64(len(actual)), Hash: wrongHash},
		},
	}
	err := d.Fetch(context.Background(), pin, store)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, cas.ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}

	if has, _ := store.Has(context.Background(), wrongHash); has {
		t.Fatalf("wrong hash must not land in CAS")
	}
	if has, _ := store.Has(context.Background(), cas.HashBytes(actual)); has {
		t.Fatalf("real hash must not land in CAS on mismatch")
	}
}

func TestFetchMissingModule(t *testing.T) {
	gradleRoot := t.TempDir()
	casRoot := t.TempDir()

	d := New(gradleRoot)
	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "missing", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "missing-1.0.jar", Hash: cas.HashBytes([]byte("x"))},
		},
	}
	err := d.Fetch(context.Background(), pin, store)
	if err == nil {
		t.Fatalf("expected error for missing module directory")
	}
	if !errors.Is(err, downloader.ErrNotFound) {
		t.Fatalf("expected wrapped downloader.ErrNotFound, got %v", err)
	}
}

func TestFetchMissingFileInsideModule(t *testing.T) {
	gradleRoot := t.TempDir()
	casRoot := t.TempDir()

	// Create an empty <sha1> subdir for the coordinate but no files inside.
	dir := filepath.Join(gradleRoot, "org.example", "empty", "1.0", "sub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	d := New(gradleRoot)
	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "empty", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "empty-1.0.jar", Hash: cas.HashBytes([]byte("x"))},
		},
	}
	err := d.Fetch(context.Background(), pin, store)
	if err == nil {
		t.Fatalf("expected error when the named file is absent")
	}
	if !errors.Is(err, downloader.ErrNotFound) {
		t.Fatalf("expected wrapped downloader.ErrNotFound, got %v", err)
	}
}

func TestFetchIdempotent(t *testing.T) {
	gradleRoot := t.TempDir()
	casRoot := t.TempDir()

	payload := []byte("idem payload")
	hash := cas.HashBytes(payload)
	writeCacheFile(t, gradleRoot, "org.example", "alpha", "1.0", "sub", "alpha-1.0.jar", payload)

	d := New(gradleRoot)
	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Hash: hash},
		},
	}
	if err := d.Fetch(context.Background(), pin, store); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if err := d.Fetch(context.Background(), pin, store); err != nil {
		t.Fatalf("second Fetch (idempotent): %v", err)
	}
}

func TestFetchHandlesMultipleFiles(t *testing.T) {
	gradleRoot := t.TempDir()
	casRoot := t.TempDir()

	jarBytes := []byte("jar body")
	pomBytes := []byte("<pom/>")
	jarHash := cas.HashBytes(jarBytes)
	pomHash := cas.HashBytes(pomBytes)

	writeCacheFile(t, gradleRoot, "org.example", "multi", "3.0", "jsub", "multi-3.0.jar", jarBytes)
	writeCacheFile(t, gradleRoot, "org.example", "multi", "3.0", "psub", "multi-3.0.pom", pomBytes)

	d := New(gradleRoot)
	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "multi", Version: "3.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "multi-3.0.jar", Hash: jarHash},
			{Kind: lockfile.FileKindPOM, Name: "multi-3.0.pom", Hash: pomHash},
		},
	}
	if err := d.Fetch(context.Background(), pin, store); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, h := range []cas.Hash{jarHash, pomHash} {
		has, err := store.Has(context.Background(), h)
		if err != nil || !has {
			t.Fatalf("missing blob for %s: has=%v err=%v", h, has, err)
		}
	}
}

func TestFetchFindsFileAcrossSubdirs(t *testing.T) {
	gradleRoot := t.TempDir()
	casRoot := t.TempDir()

	// Write a decoy subdir that does not contain the requested file, then
	// write the real one in a different subdir. locate() should still find
	// it by scanning siblings.
	base := filepath.Join(gradleRoot, "org.example", "scan", "1.0")
	if err := os.MkdirAll(filepath.Join(base, "decoy"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	payload := []byte("scan payload")
	hash := cas.HashBytes(payload)
	writeCacheFile(t, gradleRoot, "org.example", "scan", "1.0", "real", "scan-1.0.jar", payload)

	d := New(gradleRoot)
	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "scan", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "scan-1.0.jar", Hash: hash},
		},
	}
	if err := d.Fetch(context.Background(), pin, store); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestDefaultRootHonorsHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/fake-home")
	got := DefaultRoot()
	want := filepath.Join("/tmp/fake-home", ".gradle", "caches", "modules-2", "files-2.1")
	if got != want {
		t.Fatalf("DefaultRoot mismatch: got %q want %q", got, want)
	}
}

func writeCacheFile(t *testing.T, root, group, artifact, version, sub, name string, data []byte) {
	t.Helper()
	dir := filepath.Join(root, group, artifact, version, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
