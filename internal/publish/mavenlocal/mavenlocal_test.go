package mavenlocal

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	artifactDir := filepath.Join(root, "org", "jetbrains", "kotlin", "kotlin-stdlib")
	jarPath := filepath.Join(baseDir, "kotlin-stdlib-2.0.0.jar")
	pomPath := filepath.Join(baseDir, "kotlin-stdlib-2.0.0.pom")
	modulePath := filepath.Join(baseDir, "kotlin-stdlib-2.0.0.module")
	metadataPath := filepath.Join(artifactDir, "maven-metadata-local.xml")
	markerPath := filepath.Join(baseDir, "_remote.repositories")

	if got, err := os.ReadFile(jarPath); err != nil || string(got) != string(jarBytes) {
		t.Fatalf("jar at %s: err=%v got=%q", jarPath, err, got)
	}
	if got, err := os.ReadFile(pomPath); err != nil || string(got) != string(pomBytes) {
		t.Fatalf("pom at %s: err=%v got=%q", pomPath, err, got)
	}
	modulePayload, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatalf("module at %s: %v", modulePath, err)
	}

	// SHA-1 and MD5 sidecars must match the published bytes.
	assertSidecar(t, jarPath, jarBytes)
	assertSidecar(t, pomPath, pomBytes)
	assertSidecar(t, modulePath, modulePayload)
	var gradleModule gradleModuleMetadata
	if err := json.Unmarshal(modulePayload, &gradleModule); err != nil {
		t.Fatalf("json.Unmarshal module: %v", err)
	}
	if gradleModule.Component.Group != "org.jetbrains.kotlin" || gradleModule.Component.Module != "kotlin-stdlib" || gradleModule.Component.Version != "2.0.0" {
		t.Fatalf("unexpected module component: %#v", gradleModule.Component)
	}
	if len(gradleModule.Variants) != 1 || gradleModule.Variants[0].Attributes["org.gradle.usage"] != "java-runtime" {
		t.Fatalf("unexpected generated module variants: %#v", gradleModule.Variants)
	}
	metadataPayload, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("metadata at %s: %v", metadataPath, err)
	}
	metadata, err := decodeArtifactMetadata(metadataPayload)
	if err != nil {
		t.Fatalf("decodeArtifactMetadata: %v", err)
	}
	if metadata.GroupID != "org.jetbrains.kotlin" || metadata.ArtifactID != "kotlin-stdlib" {
		t.Fatalf("unexpected metadata identity: %#v", metadata)
	}
	if len(metadata.Versioning.Versions) != 1 || metadata.Versioning.Versions[0] != "2.0.0" {
		t.Fatalf("unexpected metadata versions: %#v", metadata.Versioning)
	}
	if metadata.Versioning.Latest != "2.0.0" || metadata.Versioning.Release != "2.0.0" {
		t.Fatalf("unexpected metadata latest/release: %#v", metadata.Versioning)
	}
	assertSidecar(t, metadataPath, metadataPayload)

	markerPayload, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("marker at %s: %v", markerPath, err)
	}
	if got, want := string(markerPayload), "kotlin-stdlib-2.0.0.jar>local=\n"+"kotlin-stdlib-2.0.0.pom>local=\n"; got != want {
		t.Fatalf("unexpected marker payload: got=%q want=%q", got, want)
	}
}

func TestDefaultRootHonorsSettingsXML(t *testing.T) {
	home := t.TempDir()
	confDir := filepath.Join(home, ".m2")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	override := filepath.Join(home, "publisher-repo")
	if err := os.WriteFile(filepath.Join(confDir, "settings.xml"), []byte("<settings><localRepository>"+override+"</localRepository></settings>"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MAVEN_USER_HOME", "")

	if got := DefaultRoot(); got != override {
		t.Fatalf("DefaultRoot: got %q want %q", got, override)
	}
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
	oldTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(target, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if err := p.PublishPin(ctx, pin, store); err != nil {
		t.Fatalf("third PublishPin: %v", err)
	}
	statInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !statInfo.ModTime().Equal(oldTime) {
		t.Fatalf("idempotent publish should not rewrite target: got %v want %v", statInfo.ModTime(), oldTime)
	}
}

func TestPublishPinOmitsRemoteRepositoriesMarkerWithoutRepositoryID(t *testing.T) {
	root := t.TempDir()
	casRoot := t.TempDir()
	store := cas.NewFilesystemStore(casRoot)
	ctx := context.Background()

	payload := []byte("markerless content")
	info, err := store.PutBytes(ctx, payload, cas.Provenance{
		Source: cas.Source{Kind: cas.SourceImport, Import: &cas.ImportSource{Note: "t"}},
	})
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	pin := lockfile.Pin{
		Coordinate: lockfile.Coordinate{Group: "org.example", Artifact: "markerless", Version: "1.0"},
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "markerless-1.0.jar", Hash: info.Hash},
		},
	}

	p := New(root)
	if err := p.PublishPin(ctx, pin, store); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}
	markerPath := filepath.Join(root, "org", "example", "markerless", "1.0", "_remote.repositories")
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no marker file, stat err=%v", err)
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

// assertSidecar verifies that <target>.sha1, <target>.md5, and <target>.sha256 exist and
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

	sha := sha256.Sum256(data)
	wantSha256 := hex.EncodeToString(sha[:])
	gotSha256, err := os.ReadFile(target + ".sha256")
	if err != nil {
		t.Fatalf("sha256 sidecar: %v", err)
	}
	if string(gotSha256) != wantSha256 {
		t.Fatalf("sha256 mismatch: got %s want %s", gotSha256, wantSha256)
	}
}
