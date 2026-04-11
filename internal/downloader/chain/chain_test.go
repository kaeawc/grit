package chain

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
)

// stubDownloader is a test double that records call counts and returns
// either a configured error or a set of blobs.
type stubDownloader struct {
	id    string
	err   error
	calls int32
	// data is the set of pre-staged blobs this stub serves. A Fetch call
	// for a pin file whose hash matches an entry here will land the
	// bytes in the target store. A miss returns err (which defaults to
	// an ErrNotFound wrap so untyped stubs exercise fall-through).
	data map[cas.Hash][]byte
}

func (s *stubDownloader) ID() string { return s.id }

func (s *stubDownloader) Fetch(ctx context.Context, pin lockfile.Pin, store cas.Store) error {
	atomic.AddInt32(&s.calls, 1)
	if s.err != nil {
		return s.err
	}
	for _, file := range pin.Files {
		data, ok := s.data[file.Hash]
		if !ok {
			return fmt.Errorf("%w: stub %s does not have %s", downloader.ErrNotFound, s.id, file.Hash)
		}
		prov := cas.Provenance{
			Source: cas.Source{
				Kind: cas.SourceDownload,
				Download: &cas.DownloadSource{
					Downloader:   s.id,
					RepositoryID: pin.RepositoryID,
					Coordinate:   pin.Coordinate.String(),
				},
			},
		}
		if _, err := store.PutBytesExpected(ctx, data, file.Hash, prov); err != nil {
			return err
		}
	}
	return nil
}

func (s *stubDownloader) callCount() int32 {
	return atomic.LoadInt32(&s.calls)
}

func newStub(id string, blobs ...[]byte) *stubDownloader {
	data := map[cas.Hash][]byte{}
	for _, b := range blobs {
		data[cas.HashBytes(b)] = b
	}
	return &stubDownloader{id: id, data: data}
}

// --- tests ---

func TestNewRejectsEmpty(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatalf("expected error for zero sources")
	}
}

func TestIDDefaultsAndOverride(t *testing.T) {
	d, err := New([]downloader.Downloader{newStub("a")})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID() != DefaultID {
		t.Fatalf("default ID: got %q", d.ID())
	}

	d2, err := New([]downloader.Downloader{newStub("a")}, WithID("custom"))
	if err != nil {
		t.Fatal(err)
	}
	if d2.ID() != "custom" {
		t.Fatalf("override ID: got %q", d2.ID())
	}
}

func TestFetchFirstSourceWins(t *testing.T) {
	ctx := context.Background()
	payload := []byte("first source payload")
	hash := cas.HashBytes(payload)

	first := newStub("first", payload)
	second := newStub("second", payload) // also has it, should not be called

	c, _ := New([]downloader.Downloader{first, second})
	store := cas.NewFilesystemStore(t.TempDir())
	pin := testPin(hash)

	records, err := c.FetchWithRecords(ctx, pin, store)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(records) != 1 || records[0].Order != 0 || records[0].SourceID != "first" || records[0].Outcome != FetchOutcomeSuccess {
		t.Fatalf("unexpected fetch records: %#v", records)
	}
	if first.callCount() != 1 {
		t.Fatalf("first source call count: %d", first.callCount())
	}
	if second.callCount() != 0 {
		t.Fatalf("second source should not have been called, got %d", second.callCount())
	}
	if has, _ := store.Has(ctx, hash); !has {
		t.Fatalf("blob not present after Fetch")
	}
}

func TestFetchFallsThroughOnNotFound(t *testing.T) {
	ctx := context.Background()
	payload := []byte("fall-through payload")
	hash := cas.HashBytes(payload)

	empty := newStub("empty")              // returns ErrNotFound for everything
	stocked := newStub("stocked", payload) // has the blob

	c, _ := New([]downloader.Downloader{empty, stocked})
	store := cas.NewFilesystemStore(t.TempDir())

	records, err := c.FetchWithRecords(ctx, testPin(hash), store)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(records) != 2 || records[0].SourceID != "empty" || records[0].Outcome != FetchOutcomeNotFound || records[1].SourceID != "stocked" || records[1].Outcome != FetchOutcomeSuccess {
		t.Fatalf("unexpected fetch records: %#v", records)
	}
	if empty.callCount() != 1 || stocked.callCount() != 1 {
		t.Fatalf("unexpected call counts: empty=%d stocked=%d", empty.callCount(), stocked.callCount())
	}
	if has, _ := store.Has(ctx, hash); !has {
		t.Fatalf("blob not present after fall-through")
	}
}

func TestFetchReturnsNotFoundAfterAllSourcesExhausted(t *testing.T) {
	ctx := context.Background()
	hash := cas.HashBytes([]byte("nowhere"))

	a := newStub("a")
	b := newStub("b")
	c, _ := New([]downloader.Downloader{a, b})

	records, err := c.FetchWithRecords(ctx, testPin(hash), cas.NewFilesystemStore(t.TempDir()))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, downloader.ErrNotFound) {
		t.Fatalf("expected wrapped downloader.ErrNotFound, got %v", err)
	}
	if len(records) != 2 || records[0].SourceID != "a" || records[0].Outcome != FetchOutcomeNotFound || records[1].SourceID != "b" || records[1].Outcome != FetchOutcomeNotFound {
		t.Fatalf("unexpected fetch records: %#v", records)
	}
}

func TestFetchDoesNotFallThroughOnHardError(t *testing.T) {
	ctx := context.Background()

	// A hard error (not an ErrNotFound wrap) must short-circuit the chain.
	hard := &stubDownloader{id: "hard", err: errors.New("auth failed")}
	fallback := newStub("fallback", []byte("would have worked"))

	c, _ := New([]downloader.Downloader{hard, fallback})
	pin := testPin(cas.HashBytes([]byte("would have worked")))

	records, err := c.FetchWithRecords(ctx, pin, cas.NewFilesystemStore(t.TempDir()))
	if err == nil {
		t.Fatalf("expected hard error to surface")
	}
	if errors.Is(err, downloader.ErrNotFound) {
		t.Fatalf("hard error must not be reported as ErrNotFound")
	}
	if len(records) != 1 || records[0].SourceID != "hard" || records[0].Outcome != FetchOutcomeError {
		t.Fatalf("unexpected fetch records: %#v", records)
	}
	if fallback.callCount() != 0 {
		t.Fatalf("fallback should not be called after hard error, got %d calls", fallback.callCount())
	}
}

func TestFetchPartialPopulationAcceptedByNextSource(t *testing.T) {
	// A pin with two files. The first source has only one. Since Fetch
	// takes the whole pin, the first source errors out with ErrNotFound
	// after writing the one file it has. The chain falls through to the
	// second source, which sees the first file already present (via
	// store.Has) and only needs to serve the second. The end state: both
	// files present in the store.
	ctx := context.Background()

	jarBytes := []byte("jar body")
	pomBytes := []byte("pom body")
	jarHash := cas.HashBytes(jarBytes)
	pomHash := cas.HashBytes(pomBytes)

	firstStub := newStub("first-partial", jarBytes) // has only the jar
	secondStub := newStub("second-full", jarBytes, pomBytes)

	c, _ := New([]downloader.Downloader{firstStub, secondStub})
	store := cas.NewFilesystemStore(t.TempDir())
	pin := lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.ex", Artifact: "multi", Version: "1.0"},
		RepositoryID: "chain-test",
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "multi-1.0.jar", Hash: jarHash},
			{Kind: lockfile.FileKindPOM, Name: "multi-1.0.pom", Hash: pomHash},
		},
	}

	records, err := c.FetchWithRecords(ctx, pin, store)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(records) != 2 || records[0].SourceID != "first-partial" || records[0].Outcome != FetchOutcomeNotFound || records[1].SourceID != "second-full" || records[1].Outcome != FetchOutcomeSuccess {
		t.Fatalf("unexpected fetch records: %#v", records)
	}

	// Both files must be in the store.
	for _, h := range []cas.Hash{jarHash, pomHash} {
		if has, _ := store.Has(ctx, h); !has {
			t.Fatalf("missing blob after chain fetch: %s", h)
		}
	}
	// Both stubs must have been called (first returned ErrNotFound on pom).
	if firstStub.callCount() != 1 || secondStub.callCount() != 1 {
		t.Fatalf("unexpected call counts: first=%d second=%d", firstStub.callCount(), secondStub.callCount())
	}
}

func TestFetchPropagatesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c, _ := New([]downloader.Downloader{newStub("s", []byte("x"))})
	err := c.Fetch(ctx, testPin(cas.HashBytes([]byte("x"))), cas.NewFilesystemStore(t.TempDir()))
	if err == nil {
		t.Fatalf("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestSourcesReturnsDefensiveCopy(t *testing.T) {
	a := newStub("a")
	b := newStub("b")
	c, _ := New([]downloader.Downloader{a, b})

	snap := c.Sources()
	if len(snap) != 2 {
		t.Fatalf("Sources: expected 2, got %d", len(snap))
	}
	snap[0] = nil

	// Original chain must still work after caller mutates the snapshot.
	payload := []byte("still works")
	hash := cas.HashBytes(payload)
	a.data[hash] = payload

	if err := c.Fetch(context.Background(), testPin(hash), cas.NewFilesystemStore(t.TempDir())); err != nil {
		t.Fatalf("Sources() mutation leaked into chain: %v", err)
	}
}

func TestChainOfChains(t *testing.T) {
	// Aggregators should compose transparently: chain(chain(A, B), C)
	// should produce the same fetch semantics as chain(A, B, C).
	ctx := context.Background()
	payload := []byte("nested payload")
	hash := cas.HashBytes(payload)

	a := newStub("a")             // empty
	b := newStub("b")             // empty
	cSrc := newStub("c", payload) // has it

	innerAB, err := New([]downloader.Downloader{a, b})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := New([]downloader.Downloader{innerAB, cSrc})
	if err != nil {
		t.Fatal(err)
	}

	store := cas.NewFilesystemStore(t.TempDir())
	if err := outer.Fetch(ctx, testPin(hash), store); err != nil {
		t.Fatalf("Fetch through nested chain: %v", err)
	}
	if has, _ := store.Has(ctx, hash); !has {
		t.Fatalf("blob not present after nested chain fetch")
	}
}

func testPin(h cas.Hash) lockfile.Pin {
	return lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.ex", Artifact: "alpha", Version: "1.0"},
		RepositoryID: "chain-test",
		Files: []lockfile.PinFile{
			{Kind: lockfile.FileKindPrimary, Name: "alpha-1.0.jar", Hash: h},
		},
	}
}
