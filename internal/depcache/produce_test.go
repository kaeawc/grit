package depcache_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	mavenread "github.com/kaeawc/grit/internal/downloader/mavenlocal"
	"github.com/kaeawc/grit/internal/lockfile"
	"github.com/kaeawc/grit/internal/lockfile/produce"
	"github.com/kaeawc/grit/internal/transform/aarextract"
)

// TestProduceLockfileDrivesRealDownloader is the end-to-end proof that
// the producer output is consumable by the new Layer 2 adapters
// without manual glue.
//
// The flow:
//
//  1. Stage two files (an AAR and a POM) on disk in a Maven local
//     layout.
//  2. Build []produce.Input records pointing at those files.
//  3. Run produce.Produce to compute content hashes and assemble a
//     lockfile.Lockfile.
//  4. Encode the lockfile to JSON and decode it back, proving the
//     produced output survives serialization.
//  5. Use the decoded pin to drive a mavenlocal.Downloader, which
//     fetches the files into a fresh CAS keyed by those hashes.
//  6. Run aar-extract on the fetched AAR, verify the action hash is
//     stable, and the output blobs match.
//
// This is the full loop: resolution → lockfile → downloader → CAS →
// transform, with the content hashes flowing unchanged through every
// step. No step needs to know how any other step was wired up.
func TestProduceLockfileDrivesRealDownloader(t *testing.T) {
	ctx := context.Background()

	// ---- Stage 1: stage files in Maven local layout on disk ----
	mavenRoot := t.TempDir()
	coord := lockfile.Coordinate{Group: "org.example.produce", Artifact: "demo", Version: "7.7.7"}
	moduleDir := filepath.Join(mavenRoot, "org", "example", "produce", "demo", "7.7.7")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	classesBody := []byte("produced-lockfile classes")
	manifestBody := []byte(`<?xml version="1.0"?><manifest package="com.example.produce"/>`)
	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":         classesBody,
		"AndroidManifest.xml": manifestBody,
	})
	pomBytes := []byte(`<project><modelVersion>4.0.0</modelVersion></project>`)

	aarPath := filepath.Join(moduleDir, "demo-7.7.7.aar")
	pomPath := filepath.Join(moduleDir, "demo-7.7.7.pom")
	if err := os.WriteFile(aarPath, aarBytes, 0o644); err != nil {
		t.Fatalf("write aar: %v", err)
	}
	if err := os.WriteFile(pomPath, pomBytes, 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}

	// ---- Stage 2: build produce.Input records ----
	inputs := []produce.Input{
		{
			Coordinate:   coord,
			RepositoryID: "maven-local",
			Files: []produce.FileInput{
				{Kind: lockfile.FileKindPrimary, Name: "demo-7.7.7.aar", Path: aarPath},
				{Kind: lockfile.FileKindPOM, Name: "demo-7.7.7.pom", Path: pomPath},
			},
		},
	}

	// ---- Stage 3: run the producer ----
	lf, err := produce.Produce(inputs, produce.Options{
		GeneratedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
		GritVersion: "e2e",
	})
	if err != nil {
		t.Fatalf("produce.Produce: %v", err)
	}

	// Sanity check: hashes in the lockfile must match the on-disk bytes.
	if len(lf.Pins) != 1 {
		t.Fatalf("pin count: %d", len(lf.Pins))
	}
	pin := lf.Pins[0]
	if len(pin.Files) != 2 {
		t.Fatalf("file count: %d", len(pin.Files))
	}
	var aarPinHash, pomPinHash cas.Hash
	for _, f := range pin.Files {
		switch f.Kind {
		case lockfile.FileKindPrimary:
			aarPinHash = f.Hash
		case lockfile.FileKindPOM:
			pomPinHash = f.Hash
		}
	}
	if aarPinHash != cas.HashBytes(aarBytes) {
		t.Fatalf("AAR hash mismatch between producer and on-disk bytes")
	}
	if pomPinHash != cas.HashBytes(pomBytes) {
		t.Fatalf("POM hash mismatch between producer and on-disk bytes")
	}

	// ---- Stage 4: encode + decode the lockfile ----
	var buf bytes.Buffer
	if err := lf.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := lockfile.Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded.Pins) != 1 {
		t.Fatalf("decoded pin count: %d", len(decoded.Pins))
	}
	decodedPin := decoded.Pins[0]

	// ---- Stage 5: drive a real mavenlocal downloader with the decoded pin ----
	store := cas.NewFilesystemStore(t.TempDir())
	if err := mavenread.New(mavenRoot).Fetch(ctx, decodedPin, store); err != nil {
		t.Fatalf("mavenlocal Fetch: %v", err)
	}

	// Both files must be present in the CAS under the hashes the
	// producer computed.
	if has, _ := store.Has(ctx, aarPinHash); !has {
		t.Fatalf("AAR not in CAS after Fetch")
	}
	if has, _ := store.Has(ctx, pomPinHash); !has {
		t.Fatalf("POM not in CAS after Fetch")
	}

	// Provenance must link back to the maven-local downloader and the
	// coordinate the producer recorded.
	prov, err := store.Provenance(ctx, aarPinHash)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if prov.Source.Download == nil || prov.Source.Download.Downloader != mavenread.ID {
		t.Fatalf("unexpected downloader provenance: %+v", prov.Source.Download)
	}
	if prov.Source.Download.Coordinate != coord.String() {
		t.Fatalf("coordinate lost: %s", prov.Source.Download.Coordinate)
	}

	// ---- Stage 6: run the AAR transform on the fetched blob ----
	result, err := aarextract.Extract(ctx, store, aarPinHash)
	if err != nil {
		t.Fatalf("aarextract.Extract: %v", err)
	}
	classesOut, ok := result.Output(aarextract.RoleClassesJar)
	if !ok {
		t.Fatalf("classes-jar missing")
	}
	if classesOut.Blob.Hash != cas.HashBytes(classesBody) {
		t.Fatalf("classes-jar hash drifted after producer pipeline")
	}
	manifestOut, ok := result.Output(aarextract.RoleAndroidManifest)
	if !ok {
		t.Fatalf("android-manifest missing")
	}
	if manifestOut.Blob.Hash != cas.HashBytes(manifestBody) {
		t.Fatalf("manifest hash drifted after producer pipeline")
	}
}

// TestProduceOutputIsStableAcrossRuns asserts that a second Produce
// call on unchanged files emits byte-identical canonicalized output.
// This is the reproducibility guarantee the architecture doc calls for.
func TestProduceOutputIsStableAcrossRuns(t *testing.T) {
	dir := t.TempDir()

	jarPath := filepath.Join(dir, "stable-1.0.jar")
	if err := os.WriteFile(jarPath, []byte("stable payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	inputs := []produce.Input{
		{
			Coordinate: lockfile.Coordinate{Group: "org.ex", Artifact: "stable", Version: "1.0"},
			Files:      []produce.FileInput{{Kind: lockfile.FileKindPrimary, Name: "stable-1.0.jar", Path: jarPath}},
		},
	}
	opts := produce.Options{
		GeneratedAt: time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		GritVersion: "e2e-stable",
	}

	lf1, err := produce.Produce(inputs, opts)
	if err != nil {
		t.Fatal(err)
	}
	lf2, err := produce.Produce(inputs, opts)
	if err != nil {
		t.Fatal(err)
	}

	var a, b bytes.Buffer
	if err := lf1.Encode(&a); err != nil {
		t.Fatal(err)
	}
	if err := lf2.Encode(&b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatalf("Produce output differs across runs")
	}
}
