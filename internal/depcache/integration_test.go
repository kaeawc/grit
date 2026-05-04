package depcache_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader/gradlecache"
	mavenread "github.com/kaeawc/grit/internal/downloader/mavenlocal"
	"github.com/kaeawc/grit/internal/lockfile"
	mavenpublish "github.com/kaeawc/grit/internal/publish/mavenlocal"
	"github.com/kaeawc/grit/internal/remotecache"
	"github.com/kaeawc/grit/internal/transform/aarextract"
)

// TestEndToEndFullStack wires every layer of the dependency-cache
// architecture into a single flow:
//
//	gradle-cache downloader
//	  → CAS A
//	      → aar-extract transform
//	          → CAS A (outputs + action result)
//	              → Maven Local publisher
//	                  → Maven filesystem layout
//	                      → Maven Local downloader
//	                          → CAS B
//	                              → aar-extract transform (re-verified)
//
// Along the way a remote-cache client round-trips one of the blobs and
// the action result through a fake server to prove the wire protocol
// speaks the same CAS types as the local path, and the pin travels
// through a lockfile encode/decode cycle to prove the schema survives
// serialization.
func TestEndToEndFullStack(t *testing.T) {
	ctx := context.Background()

	// ------------------------------------------------------------------
	// Stage 1: synthesize an AAR fixture.
	// ------------------------------------------------------------------
	classesBody := []byte("pretend classes.jar bytes")
	manifestBody := []byte(`<?xml version="1.0"?><manifest package="com.example"/>`)
	aarBytes := buildAAR(t, map[string][]byte{
		"classes.jar":            classesBody,
		"AndroidManifest.xml":    manifestBody,
		"res/values/strings.xml": []byte(`<resources><string name="app">x</string></resources>`),
		"R.txt":                  []byte("int id foo 0x7f010000"),
	})
	aarHash := cas.HashBytes(aarBytes)
	coord := lockfile.Coordinate{Group: "org.example.e2e", Artifact: "demo", Version: "1.2.3"}

	// ------------------------------------------------------------------
	// Stage 2: stage the AAR in a Gradle cache layout.
	// ------------------------------------------------------------------
	gradleRoot := t.TempDir()
	writeGradleCacheFile(t, gradleRoot, coord, "sha1-of-url", "demo-1.2.3.aar", aarBytes)

	// ------------------------------------------------------------------
	// Stage 3: build a lockfile pin and round-trip it through the schema.
	// ------------------------------------------------------------------
	pin := lockfile.Pin{
		Coordinate:   coord,
		RepositoryID: "central",
		Files: []lockfile.PinFile{
			{
				Kind: lockfile.FileKindPrimary,
				Name: "demo-1.2.3.aar",
				Size: int64(len(aarBytes)),
				Hash: aarHash,
				URL:  "https://repo.example/org/example/e2e/demo/1.2.3/demo-1.2.3.aar",
			},
		},
	}

	lf := lockfile.Lockfile{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
		GritVersion:   "e2e-test",
		Pins:          []lockfile.Pin{pin},
	}
	var lfBuf bytes.Buffer
	if err := lf.Encode(&lfBuf); err != nil {
		t.Fatalf("lockfile encode: %v", err)
	}
	decodedLockfile, err := lockfile.Decode(&lfBuf)
	if err != nil {
		t.Fatalf("lockfile decode: %v", err)
	}
	if len(decodedLockfile.Pins) != 1 {
		t.Fatalf("lockfile round trip lost pins: %d", len(decodedLockfile.Pins))
	}
	if decodedLockfile.Pins[0].Files[0].Hash != aarHash {
		t.Fatalf("lockfile round trip lost hash")
	}
	// Use the round-tripped pin for the rest of the flow.
	pin = decodedLockfile.Pins[0]

	// ------------------------------------------------------------------
	// Stage 4: fetch the AAR via the gradle-cache downloader into CAS A.
	// ------------------------------------------------------------------
	casA := cas.NewFilesystemStore(t.TempDir())
	if err := gradlecache.New(gradleRoot).Fetch(ctx, pin, casA); err != nil {
		t.Fatalf("gradle-cache fetch: %v", err)
	}
	if has, _ := casA.Has(ctx, aarHash); !has {
		t.Fatalf("AAR not present in CAS A after gradle-cache fetch")
	}
	aarProv, err := casA.Provenance(ctx, aarHash)
	if err != nil {
		t.Fatalf("provenance aar: %v", err)
	}
	if aarProv.Source.Download == nil || aarProv.Source.Download.Downloader != gradlecache.ID {
		t.Fatalf("unexpected aar provenance: %+v", aarProv.Source)
	}
	if aarProv.Source.Download.Coordinate != coord.String() {
		t.Fatalf("coordinate lost in provenance: %s", aarProv.Source.Download.Coordinate)
	}

	// ------------------------------------------------------------------
	// Stage 5: run the aar-extract transform against the CAS blob.
	// ------------------------------------------------------------------
	extractResult, err := aarextract.Extract(ctx, casA, aarHash)
	if err != nil {
		t.Fatalf("aarextract.Extract: %v", err)
	}

	classesOut, ok := extractResult.Output(aarextract.RoleClassesJar)
	if !ok {
		t.Fatalf("classes-jar missing from extract result")
	}
	if classesOut.Blob.Hash != cas.HashBytes(classesBody) {
		t.Fatalf("classes-jar hash mismatch")
	}
	manifestOut, ok := extractResult.Output(aarextract.RoleAndroidManifest)
	if !ok {
		t.Fatalf("android-manifest missing from extract result")
	}
	if manifestOut.Blob.Hash != cas.HashBytes(manifestBody) {
		t.Fatalf("android-manifest hash mismatch")
	}

	// Transform provenance must link every output back to the original AAR.
	classesProv, err := casA.Provenance(ctx, classesOut.Blob.Hash)
	if err != nil {
		t.Fatalf("classes provenance: %v", err)
	}
	if classesProv.Source.Transform == nil {
		t.Fatalf("classes-jar missing transform provenance")
	}
	if len(classesProv.Source.Transform.Inputs) != 1 || classesProv.Source.Transform.Inputs[0].Hash != aarHash {
		t.Fatalf("transform inputs not linked back to AAR: %+v", classesProv.Source.Transform.Inputs)
	}

	// Running Extract again must hit the action-result cache.
	cachedResult, err := aarextract.Extract(ctx, casA, aarHash)
	if err != nil {
		t.Fatalf("cached Extract: %v", err)
	}
	if cachedResult.ActionHash != extractResult.ActionHash {
		t.Fatalf("action hash drifted on second call: %s vs %s", cachedResult.ActionHash, extractResult.ActionHash)
	}

	// ------------------------------------------------------------------
	// Stage 6: round-trip a blob and the action result through the
	// remote-cache wire protocol.
	// ------------------------------------------------------------------
	remoteClient, remoteClose := startFakeRemoteCache(t)
	defer remoteClose()

	classesBytes, err := readBlob(ctx, casA, classesOut.Blob.Hash)
	if err != nil {
		t.Fatalf("read classes blob from CAS A: %v", err)
	}
	if err := remoteClient.PutBlob(ctx, classesOut.Blob.Hash, classesBytes); err != nil {
		t.Fatalf("remote PutBlob: %v", err)
	}
	roundTripBytes, err := remoteClient.GetBlob(ctx, classesOut.Blob.Hash)
	if err != nil {
		t.Fatalf("remote GetBlob: %v", err)
	}
	if !bytes.Equal(classesBytes, roundTripBytes) {
		t.Fatalf("remote blob round trip corrupted bytes")
	}
	if err := remoteClient.PutActionResult(ctx, extractResult); err != nil {
		t.Fatalf("remote PutActionResult: %v", err)
	}
	remoteResult, err := remoteClient.GetActionResult(ctx, extractResult.ActionHash)
	if err != nil {
		t.Fatalf("remote GetActionResult: %v", err)
	}
	if remoteResult.ActionHash != extractResult.ActionHash {
		t.Fatalf("remote action result hash drift")
	}
	if len(remoteResult.Outputs) != len(extractResult.Outputs) {
		t.Fatalf("remote action result output count mismatch")
	}

	// ------------------------------------------------------------------
	// Stage 7: publish the AAR pin through the Maven Local publisher.
	// ------------------------------------------------------------------
	mavenRoot := t.TempDir()
	publishPin := lockfile.Pin{
		Coordinate:   coord,
		RepositoryID: "local",
		Files: []lockfile.PinFile{
			{
				Kind: lockfile.FileKindPrimary,
				Name: "demo-1.2.3.aar",
				Size: int64(len(aarBytes)),
				Hash: aarHash,
			},
		},
	}
	if err := mavenpublish.New(mavenRoot).PublishPin(ctx, publishPin, casA); err != nil {
		t.Fatalf("PublishPin: %v", err)
	}

	publishedPath := filepath.Join(mavenRoot, "org", "example", "e2e", "demo", "1.2.3", "demo-1.2.3.aar")
	publishedBytes, err := os.ReadFile(publishedPath)
	if err != nil {
		t.Fatalf("read published file: %v", err)
	}
	if !bytes.Equal(publishedBytes, aarBytes) {
		t.Fatalf("published AAR bytes differ from original")
	}
	assertMavenSidecars(t, publishedPath, aarBytes)

	// ------------------------------------------------------------------
	// Stage 8: read the published artifact back through the Maven Local
	// downloader into a fresh CAS B.
	// ------------------------------------------------------------------
	casB := cas.NewFilesystemStore(t.TempDir())
	if err := mavenread.New(mavenRoot).Fetch(ctx, publishPin, casB); err != nil {
		t.Fatalf("maven-local fetch: %v", err)
	}
	hasAARInB, err := casB.Has(ctx, aarHash)
	if err != nil || !hasAARInB {
		t.Fatalf("AAR missing from CAS B: has=%v err=%v", hasAARInB, err)
	}
	provB, err := casB.Provenance(ctx, aarHash)
	if err != nil {
		t.Fatalf("CAS B provenance: %v", err)
	}
	if provB.Source.Download == nil || provB.Source.Download.Downloader != mavenread.ID {
		t.Fatalf("unexpected CAS B provenance: %+v", provB.Source)
	}

	// ------------------------------------------------------------------
	// Stage 9: run aar-extract against CAS B. The action hash and the
	// output blob hashes must match CAS A exactly — this is the
	// end-to-end proof that content addressing is transparent across
	// independent CAS instances.
	// ------------------------------------------------------------------
	resultB, err := aarextract.Extract(ctx, casB, aarHash)
	if err != nil {
		t.Fatalf("Extract against CAS B: %v", err)
	}
	if resultB.ActionHash != extractResult.ActionHash {
		t.Fatalf("action hash differs across CAS instances: %s vs %s", resultB.ActionHash, extractResult.ActionHash)
	}
	classesOutB, ok := resultB.Output(aarextract.RoleClassesJar)
	if !ok {
		t.Fatalf("classes-jar missing from CAS B extract result")
	}
	if classesOutB.Blob.Hash != classesOut.Blob.Hash {
		t.Fatalf("classes-jar hash differs across CAS instances")
	}
	manifestOutB, ok := resultB.Output(aarextract.RoleAndroidManifest)
	if !ok {
		t.Fatalf("android-manifest missing from CAS B extract result")
	}
	if manifestOutB.Blob.Hash != manifestOut.Blob.Hash {
		t.Fatalf("android-manifest hash differs across CAS instances")
	}

	// Sanity check: reading the classes blob back out of CAS B yields the
	// exact same bytes as CAS A.
	classesBytesB, err := readBlob(ctx, casB, classesOutB.Blob.Hash)
	if err != nil {
		t.Fatalf("read classes blob from CAS B: %v", err)
	}
	if !bytes.Equal(classesBytes, classesBytesB) {
		t.Fatalf("classes bytes differ across CAS instances")
	}
}

// TestEndToEndGradleCacheThenMavenLocalReader confirms the two Layer 2
// adapters agree on semantics when reading the same content from
// different ecosystem layouts.
func TestEndToEndGradleCacheAndMavenLocalAgreeOnContent(t *testing.T) {
	ctx := context.Background()

	payload := []byte("agreement test bytes")
	hash := cas.HashBytes(payload)
	coord := lockfile.Coordinate{Group: "com.example.agree", Artifact: "lib", Version: "0.1.0"}

	gradleRoot := t.TempDir()
	writeGradleCacheFile(t, gradleRoot, coord, "subdir", "lib-0.1.0.jar", payload)

	mavenRoot := t.TempDir()
	mavenDir := filepath.Join(mavenRoot, "com", "example", "agree", "lib", "0.1.0")
	if err := os.MkdirAll(mavenDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mavenDir, "lib-0.1.0.jar"), payload, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pin := lockfile.Pin{
		Coordinate:   coord,
		RepositoryID: "agree",
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "lib-0.1.0.jar", Size: int64(len(payload)), Hash: hash},
		},
	}

	gradleCAS := cas.NewFilesystemStore(t.TempDir())
	if err := gradlecache.New(gradleRoot).Fetch(ctx, pin, gradleCAS); err != nil {
		t.Fatalf("gradle-cache fetch: %v", err)
	}

	mavenCAS := cas.NewFilesystemStore(t.TempDir())
	if err := mavenread.New(mavenRoot).Fetch(ctx, pin, mavenCAS); err != nil {
		t.Fatalf("maven-local fetch: %v", err)
	}

	gradleBytes, err := readBlob(ctx, gradleCAS, hash)
	if err != nil {
		t.Fatal(err)
	}
	mavenBytes, err := readBlob(ctx, mavenCAS, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gradleBytes, mavenBytes) {
		t.Fatalf("adapters disagree on content for identical hash")
	}
}

// ---------- helpers ----------

func buildAAR(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip.Create %s: %v", name, err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatalf("zip.Write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	return buf.Bytes()
}

// writeGradleCacheFile creates <root>/<group-dotted>/<artifact>/<version>/<sub>/<name>
// matching Gradle's files-2.1 layout.
func writeGradleCacheFile(t *testing.T, root string, coord lockfile.Coordinate, sub, name string, data []byte) {
	t.Helper()
	dir := filepath.Join(root, coord.Group, coord.Artifact, coord.Version, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func readBlob(ctx context.Context, store cas.Store, h cas.Hash) ([]byte, error) {
	rc, err := store.Get(ctx, h)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func assertMavenSidecars(t *testing.T, target string, data []byte) {
	t.Helper()

	s1 := sha1.Sum(data)
	wantSha1 := hex.EncodeToString(s1[:])
	got, err := os.ReadFile(target + ".sha1")
	if err != nil || string(got) != wantSha1 {
		t.Fatalf("sha1 sidecar: got=%q want=%q err=%v", got, wantSha1, err)
	}

	m := md5.Sum(data)
	wantMd5 := hex.EncodeToString(m[:])
	got, err = os.ReadFile(target + ".md5")
	if err != nil || string(got) != wantMd5 {
		t.Fatalf("md5 sidecar: got=%q want=%q err=%v", got, wantMd5, err)
	}
}

// ---------- fake remote cache server ----------

type fakeRemote struct {
	mu      sync.Mutex
	blobs   map[cas.Hash][]byte
	actions map[cas.Hash]cas.ActionResult
}

func newFakeRemote() *fakeRemote {
	return &fakeRemote{
		blobs:   map[cas.Hash][]byte{},
		actions: map[cas.Hash]cas.ActionResult{},
	}
}

func (f *fakeRemote) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cas/"):
			hexHash := strings.TrimPrefix(r.URL.Path, "/cas/")
			h, err := cas.ParseHash(hexHash)
			if err != nil {
				http.Error(w, "bad hash", http.StatusBadRequest)
				return
			}
			f.handleBlob(w, r, h)
		case strings.HasPrefix(r.URL.Path, "/action/"):
			hexHash := strings.TrimPrefix(r.URL.Path, "/action/")
			h, err := cas.ParseHash(hexHash)
			if err != nil {
				http.Error(w, "bad hash", http.StatusBadRequest)
				return
			}
			f.handleAction(w, r, h)
		default:
			http.NotFound(w, r)
		}
	})
}

func (f *fakeRemote) handleBlob(w http.ResponseWriter, r *http.Request, hash cas.Hash) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		body, ok := f.blobs[hash]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	case http.MethodHead:
		if _, ok := f.blobs[hash]; !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if cas.HashBytes(body) != hash {
			http.Error(w, "hash mismatch", http.StatusBadRequest)
			return
		}
		f.blobs[hash] = body
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fakeRemote) handleAction(w http.ResponseWriter, r *http.Request, hash cas.Hash) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		result, ok := f.actions[hash]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	case http.MethodPut:
		var result cas.ActionResult
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if result.ActionHash != hash {
			http.Error(w, "action hash mismatch", http.StatusBadRequest)
			return
		}
		f.actions[hash] = result
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func startFakeRemoteCache(t *testing.T) (*remotecache.Client, func()) {
	t.Helper()
	fake := newFakeRemote()
	ts := httptest.NewServer(fake.handler())
	client, err := remotecache.New(ts.URL, "")
	if err != nil {
		ts.Close()
		t.Fatalf("remotecache.New: %v", err)
	}
	return client, ts.Close
}
