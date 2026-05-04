package depcache_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader/gradlecache"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/lockfile/produce"
	"github.com/kaeawc/grit/internal/m2local"
	"github.com/kaeawc/grit/internal/m2localbridge"
	"github.com/kaeawc/grit/internal/transform/aarextract"
)

// TestM2LocalBridgeEndToEnd is the full proof that the existing
// internal/m2local resolver's output (a Resolved struct) can flow
// through the new tree via the bridge and produce a lockfile whose
// pins are directly consumable by the new Layer 2 adapters.
//
// The flow:
//
//  1. Stage an AAR and a POM in a Gradle cache layout on disk.
//  2. Synthesize a *m2local.Resolved pointing at those paths. (We do
//     not call the real m2local.Resolver here — testing the resolver
//     is m2local's own job; testing the *bridge* is this test's job.)
//  3. Run m2localbridge.FromResolved to convert Resolved →
//     []produce.Input. The bridge parses each path back to a Maven
//     coordinate and groups by coordinate.
//  4. Run produce.Produce to compute SHA-256 hashes for every file
//     and assemble a canonicalized lockfile.Lockfile.
//  5. Encode + decode the lockfile to prove it survives JSON
//     serialization.
//  6. Use the decoded pin to drive a real gradlecache.Downloader,
//     which fetches the files back into a fresh CAS keyed by content
//     hash.
//  7. Run aarextract.Extract against the fetched AAR and confirm the
//     outputs match the staged classes.jar and manifest bytes.
//
// Every step is content-addressed. The lockfile is the only state
// that passes between m2local's world and the new tree's world.
func TestM2LocalBridgeEndToEnd(t *testing.T) {
	ctx := context.Background()

	// ---- Stage 1: build a Gradle cache directory on disk ----
	gradleRoot := t.TempDir()

	classesBody := []byte("m2local-bridge classes")
	manifestBody := []byte(`<?xml version="1.0"?><manifest package="com.example.bridge"/>`)
	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":         classesBody,
		"AndroidManifest.xml": manifestBody,
	})
	pomBytes := []byte(`<project><modelVersion>4.0.0</modelVersion></project>`)

	// Gradle cache layout: <root>/files-2.1/<group>/<artifact>/<version>/<sha1>/<file>.
	coord := lockfile.Coordinate{Group: "org.example.bridge", Artifact: "widget", Version: "9.9.9"}
	aarPath := stageGradleCache(t, gradleRoot, coord, "sha1-aar", "widget-9.9.9.aar", aarBytes)
	pomPath := stageGradleCache(t, gradleRoot, coord, "sha1-pom", "widget-9.9.9.pom", pomBytes)

	// ---- Stage 2: synthesize a m2local.Resolved ----
	resolved := &m2local.Resolved{
		CompileJars: []string{aarPath, pomPath},
		RuntimeJars: []string{aarPath},
	}

	// ---- Stage 3: bridge to []produce.Input ----
	inputs, err := m2localbridge.FromResolved(resolved, "gradle-cache")
	if err != nil {
		t.Fatalf("FromResolved: %v", err)
	}
	if len(inputs) != 1 {
		t.Fatalf("expected 1 input (one coordinate, two files), got %d", len(inputs))
	}

	in := inputs[0]
	if in.Coordinate != coord {
		t.Fatalf("coordinate mismatch: %+v", in.Coordinate)
	}
	if len(in.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(in.Files))
	}

	// ---- Stage 4: run the producer ----
	lf, err := produce.Produce(inputs, produce.Options{
		GeneratedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
		GritVersion: "bridge-e2e",
	})
	if err != nil {
		t.Fatalf("produce.Produce: %v", err)
	}
	if len(lf.Pins) != 1 {
		t.Fatalf("expected 1 pin in lockfile, got %d", len(lf.Pins))
	}

	// Find the AAR and POM hashes from the produced lockfile for later assertions.
	var aarPinHash, pomPinHash cas.Hash
	for _, f := range lf.Pins[0].Files {
		switch f.Kind {
		case lockfile.FileKindPrimary:
			aarPinHash = f.Hash
		case lockfile.FileKindPOM:
			pomPinHash = f.Hash
		}
	}
	if aarPinHash != cas.HashBytes(aarBytes) {
		t.Fatalf("AAR hash drifted: producer=%s want=%s", aarPinHash, cas.HashBytes(aarBytes))
	}
	if pomPinHash != cas.HashBytes(pomBytes) {
		t.Fatalf("POM hash drifted")
	}

	// ---- Stage 5: encode + decode ----
	var buf bytes.Buffer
	if err := lf.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := lockfile.Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Pins) != 1 {
		t.Fatalf("decoded pin count mismatch")
	}

	// ---- Stage 6: fetch the decoded pin via gradlecache downloader ----
	// gradlecache expects its root to be the files-2.1 directory, so
	// point it at the subdirectory where stageGradleCache actually
	// writes the fixture tree.
	filesRoot := filepath.Join(gradleRoot, "files-2.1")
	store := cas.NewFilesystemStore(t.TempDir())
	if err := gradlecache.New(filesRoot).Fetch(ctx, decoded.Pins[0], store); err != nil {
		t.Fatalf("gradlecache Fetch: %v", err)
	}

	if has, _ := store.Has(ctx, aarPinHash); !has {
		t.Fatalf("AAR not in CAS after fetch")
	}
	if has, _ := store.Has(ctx, pomPinHash); !has {
		t.Fatalf("POM not in CAS after fetch")
	}

	// Provenance on the AAR should name the gradlecache downloader.
	prov, err := store.Provenance(ctx, aarPinHash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Download == nil || prov.Source.Download.Downloader != gradlecache.ID {
		t.Fatalf("unexpected downloader provenance: %+v", prov.Source.Download)
	}
	if prov.Source.Download.Coordinate != coord.String() {
		t.Fatalf("coordinate lost in provenance: %s", prov.Source.Download.Coordinate)
	}

	// ---- Stage 7: run the AAR transform on the fetched blob ----
	result, err := aarextract.Extract(ctx, store, aarPinHash)
	if err != nil {
		t.Fatalf("aarextract.Extract: %v", err)
	}
	classesOut, ok := result.Output(aarextract.RoleClassesJar)
	if !ok {
		t.Fatalf("classes-jar output missing")
	}
	if classesOut.Blob.Hash != cas.HashBytes(classesBody) {
		t.Fatalf("classes hash drift: got %s want %s", classesOut.Blob.Hash, cas.HashBytes(classesBody))
	}
	manifestOut, ok := result.Output(aarextract.RoleAndroidManifest)
	if !ok {
		t.Fatalf("android-manifest output missing")
	}
	if manifestOut.Blob.Hash != cas.HashBytes(manifestBody) {
		t.Fatalf("manifest hash drift")
	}
}

// stageGradleCache writes data at
// <root>/files-2.1/<group>/<artifact>/<version>/<sha1>/<name> and
// returns the path, matching how Gradle's dependency cache organizes
// files on disk.
func stageGradleCache(t *testing.T, root string, coord lockfile.Coordinate, sha1, name string, data []byte) string {
	t.Helper()
	dir := filepath.Join(root, "files-2.1", coord.Group, coord.Artifact, coord.Version, sha1)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
