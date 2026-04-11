package mavenlocal

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	readadapter "github.com/kaeawc/grit/internal/downloader/mavenlocal"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/lockfile"
)

func TestPublishPinWritesMavenLayout(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()

	store := cas.NewFilesystemStore(casRoot)
	ctx := context.Background()

	jarBytes := []byte("jar body")
	pomBytes := []byte(`<project><modelVersion>4.0.0</modelVersion></project>`)

	jarInfo, err := store.PutBytes(ctx, jarBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "jar"}},
	})
	if err != nil {
		t.Fatalf("PutBytes jar: %v", err)
	}
	pomInfo, err := store.PutBytes(ctx, pomBytes, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "pom"}},
	})
	if err != nil {
		t.Fatalf("PutBytes pom: %v", err)
	}

	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.jetbrains.kotlin", Artifact: "kotlin-stdlib", Version: "2.0.0"},
		RepositoryID: "local",
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "kotlin-stdlib-2.0.0.jar", Hash: jarInfo.Hash, Size: jarInfo.Size},
			{Kind: lockfile.FileKindPOM, Name: "kotlin-stdlib-2.0.0.pom", Hash: pomInfo.Hash, Size: pomInfo.Size},
		},
	}

	p := New(root)
	if p.ID() != ID {
		t.Fatalf("ID mismatch: %s", p.ID())
	}

	if err := p.PublishPin(ctx, pin, store); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}

	baseDir := filepath.Join(root, "org", "jetbrains", "kotlin", "kotlin-stdlib", "2.0.0")
	jarPath := filepath.Join(baseDir, "kotlin-stdlib-2.0.0.jar")
	pomPath := filepath.Join(baseDir, "kotlin-stdlib-2.0.0.pom")

	if got, err := os.ReadFile(jarPath); err != nil || string(got) != string(jarBytes) {
		t.Fatalf("jar at %s: err=%v got=%q", jarPath, err, got)
	}
	if got, err := os.ReadFile(pomPath); err != nil || string(got) != string(pomBytes) {
		t.Fatalf("pom at %s: err=%v got=%q", pomPath, err, got)
	}

	// SHA-1 and MD5 sidecars must match the published bytes.
	assertSidecar(t, jarPath, jarBytes)
	assertSidecar(t, pomPath, pomBytes)
}

func TestPublishPinMissingBlob(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()
	store := cas.NewFilesystemStore(casRoot)

	// No bytes written to CAS — pin references a hash that isn't there.
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "lost", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "lost-1.0.jar", Hash: cas.HashBytes([]byte("never stored"))},
		},
	}

	p := New(root)
	err := p.PublishPin(context.Background(), pin, store)
	if err == nil {
		t.Fatalf("expected error for missing blob")
	}
	if !errors.Is(err, cas.ErrNotFound) {
		t.Fatalf("expected wrapped ErrNotFound, got %v", err)
	}
}

func TestPublishPinIdempotent(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()
	store := cas.NewFilesystemStore(casRoot)
	ctx := context.Background()

	payload := []byte("repeat content")
	info, err := store.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "t"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "dup", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "dup-1.0.jar", Hash: info.Hash},
		},
	}

	p := New(root)
	if err := p.PublishPin(ctx, pin, store); err != nil {
		t.Fatalf("first PublishPin: %v", err)
	}
	if err := p.PublishPin(ctx, pin, store); err != nil {
		t.Fatalf("second PublishPin: %v", err)
	}
	target := filepath.Join(root, "org", "example", "dup", "1.0", "dup-1.0.jar")
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("unexpected state after double publish: err=%v got=%q", err, got)
	}
}

func TestPublishAndReadRoundTrip(t *testing.T) {
	// End-to-end proof: bytes go from CAS A → Maven Local publish target →
	// CAS B via the read adapter → original bytes.
	publishRoot := t.TempDir()
	casA := cas.NewFilesystemStore(t.TempDir())
	casB := cas.NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	payload := []byte("round trip body")
	infoA, err := casA.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "origin"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.example", Artifact: "rt", Version: "1.0"},
		RepositoryID: "local",
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "rt-1.0.jar", Hash: infoA.Hash},
		},
	}

	if err := New(publishRoot).PublishPin(ctx, pin, casA); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}

	reader := readadapter.New(publishRoot)
	if err := reader.Fetch(ctx, pin, casB); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	hasB, err := casB.Has(ctx, infoA.Hash)
	if err != nil || !hasB {
		t.Fatalf("blob did not round-trip into casB: has=%v err=%v", hasB, err)
	}
}

// assertSidecar verifies that <target>.sha1 and <target>.md5 exist and
// contain the expected hex digests of data.
func assertSidecar(t *testing.T, target string, data []byte) {
	t.Helper()

	sh1 := sha1.Sum(data)
	wantSha1 := hex.EncodeToString(sh1[:])
	gotSha1, err := os.ReadFile(target + ".sha1")
	if err != nil {
		t.Fatalf("sha1 sidecar: %v", err)
	}
	if string(gotSha1) != wantSha1 {
		t.Fatalf("sha1 mismatch: got %s want %s", gotSha1, wantSha1)
	}

	m5 := md5.Sum(data)
	wantMd5 := hex.EncodeToString(m5[:])
	gotMd5, err := os.ReadFile(target + ".md5")
	if err != nil {
		t.Fatalf("md5 sidecar: %v", err)
	}
	if string(gotMd5) != wantMd5 {
		t.Fatalf("md5 mismatch: got %s want %s", gotMd5, wantMd5)
	}
}
