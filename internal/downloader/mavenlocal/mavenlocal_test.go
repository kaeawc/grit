package mavenlocal

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

func TestGroupPathSlashesDottedGroup(t *testing.T) {
	got := GroupPath("org.jetbrains.kotlin")
	want := filepath.Join("org", "jetbrains", "kotlin")
	if got != want {
		t.Fatalf("GroupPath: got %q want %q", got, want)
	}
	if GroupPath("") != "" {
		t.Fatalf("empty group should produce empty path")
	}
	if GroupPath("single") != "single" {
		t.Fatalf("single-segment group should be unchanged")
	}
}

func TestFetchRoundTrip(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()

	payload := []byte("pretend jar bytes")
	hash := cas.HashBytes(payload)
	writeMavenFile(t, root, "org.example", "alpha", "1.0", "alpha-1.0.jar", payload)

	d := New(root)
	if d.ID() != ID {
		t.Fatalf("ID mismatch: %s", d.ID())
	}

	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.example", Artifact: "alpha", Version: "1.0"},
		RepositoryID: "local",
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
		t.Fatalf("unexpected provenance: %+v", prov.Source)
	}
	if prov.Source.Download.Coordinate != "org.example:alpha:1.0" {
		t.Fatalf("coordinate not preserved: %s", prov.Source.Download.Coordinate)
	}
}

func TestFetchFindsPomAndPrimarySiblings(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()

	jarBytes := []byte("jar body")
	pomBytes := []byte(`<project><modelVersion>4.0.0</modelVersion></project>`)
	jarHash := cas.HashBytes(jarBytes)
	pomHash := cas.HashBytes(pomBytes)

	writeMavenFile(t, root, "org.example", "multi", "3.0", "multi-3.0.jar", jarBytes)
	writeMavenFile(t, root, "org.example", "multi", "3.0", "multi-3.0.pom", pomBytes)

	d := New(root)
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
			t.Fatalf("missing blob for %s", h)
		}
	}
}

func TestFetchRejectsHashMismatch(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()

	actual := []byte("real content")
	wrongHash := cas.HashBytes([]byte("tampered"))
	writeMavenFile(t, root, "org.example", "beta", "2.0", "beta-2.0.jar", actual)

	d := New(root)
	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "beta", Version: "2.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "beta-2.0.jar", Hash: wrongHash},
		},
	}
	err := d.Fetch(context.Background(), pin, store)
	if !errors.Is(err, cas.ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
}

func TestFetchMissingFile(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()

	d := New(root)
	store := cas.NewFilesystemStore(casRoot)
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "missing", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "missing-1.0.jar", Hash: cas.HashBytes([]byte("x"))},
		},
	}
	err := d.Fetch(context.Background(), pin, store)
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	if !errors.Is(err, downloader.ErrNotFound) {
		t.Fatalf("expected wrapped downloader.ErrNotFound, got %v", err)
	}
}

func TestFetchIdempotent(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()
	payload := []byte("idem payload")
	hash := cas.HashBytes(payload)
	writeMavenFile(t, root, "org.example", "alpha", "1.0", "alpha-1.0.jar", payload)

	d := New(root)
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
		t.Fatalf("second Fetch: %v", err)
	}
}

func TestDefaultRootHonorsHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/fake-home")
	got := DefaultRoot()
	want := filepath.Join("/tmp/fake-home", ".m2", "repository")
	if got != want {
		t.Fatalf("DefaultRoot: got %q want %q", got, want)
	}
}

func TestModulePathUsesSlashedGroup(t *testing.T) {
	root := t.TempDir()
	d := New(root)
	got := d.ModulePath(lockfile.Coordinate{Group: "org.jetbrains.kotlin", Artifact: "kotlin-stdlib", Version: "2.0.0"})
	want := filepath.Join(root, "org", "jetbrains", "kotlin", "kotlin-stdlib", "2.0.0")
	if got != want {
		t.Fatalf("ModulePath: got %q want %q", got, want)
	}
}

// writeMavenFile creates a file at <root>/<group-slashed>/<artifact>/<version>/<name>
// for tests that need to stage fixture data in Maven layout.
func writeMavenFile(t *testing.T, root, group, artifact, version, name string, data []byte) {
	t.Helper()
	dir := filepath.Join(root, GroupPath(group), artifact, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
