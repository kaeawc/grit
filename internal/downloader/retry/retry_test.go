package retry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kaeawc/grit/internal/cas"
	"github.com/kaeawc/grit/internal/downloader"
	"github.com/kaeawc/grit/internal/lockfile"
)

type stubDownloader struct {
	id    string
	mu    sync.Mutex
	calls int
	errs  []error
	data  map[cas.Hash][]byte
}

func (s *stubDownloader) ID() string { return s.id }

func (s *stubDownloader) Fetch(ctx context.Context, pin lockfile.Pin, store cas.Store) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	var err error
	if call <= len(s.errs) {
		err = s.errs[call-1]
	}
	s.mu.Unlock()

	if err != nil {
		return err
	}
	for _, file := range pin.Files {
		payload, ok := s.data[file.Hash]
		if !ok {
			return fmt.Errorf("%w: stub %s missing %s", downloader.ErrNotFound, s.id, file.Hash)
		}
		if _, err := store.PutBytesExpected(ctx, payload, file.Hash, cas.Provenance{}); err != nil {
			return err
		}
	}
	return nil
}

func (s *stubDownloader) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type fakeSleeper struct {
	mu    sync.Mutex
	delta []time.Duration
}

func (s *fakeSleeper) Sleep(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delta = append(s.delta, d)
	return nil
}

func (s *fakeSleeper) delays() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.delta...)
}

func testPin(hash cas.Hash) lockfile.Pin {
	return lockfile.Pin{
		Coordinate:   lockfile.Coordinate{Group: "org.ex", Artifact: "demo", Version: "1.0"},
		RepositoryID: "retry-test",
		Files: []lockfile.PinFile{
			{Name: "demo.jar", Kind: lockfile.FileKindPrimary, Hash: hash},
		},
	}
}

func TestNewRejectsNilInner(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatalf("expected error for nil inner downloader")
	}
}

func TestFetchRetriesAndSucceeds(t *testing.T) {
	ctx := context.Background()
	payload := []byte("retry payload")
	hash := cas.HashBytes(payload)

	inner := &stubDownloader{
		id:   "flaky",
		errs: []error{errors.New("transient 1"), errors.New("transient 2")},
		data: map[cas.Hash][]byte{hash: payload},
	}
	sleeper := &fakeSleeper{}
	d, err := New(inner, WithAttempts(3), WithBackoff(func(attempt int) time.Duration {
		return time.Duration(attempt) * time.Millisecond
	}), WithSleeper(sleeper.Sleep))
	if err != nil {
		t.Fatal(err)
	}

	store := cas.NewFilesystemStore(t.TempDir())
	if err := d.Fetch(ctx, testPin(hash), store); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := inner.callCount(); got != 3 {
		t.Fatalf("call count: got %d, want 3", got)
	}
	delays := sleeper.delays()
	if len(delays) != 2 || delays[0] != time.Millisecond || delays[1] != 2*time.Millisecond {
		t.Fatalf("unexpected sleep delays: %#v", delays)
	}
	if has, _ := store.Has(ctx, hash); !has {
		t.Fatalf("payload not stored after retries")
	}
}

func TestFetchReturnsNotFoundWithoutRetry(t *testing.T) {
	ctx := context.Background()
	inner := &stubDownloader{
		id:   "missing",
		errs: []error{fmt.Errorf("%w: not present", downloader.ErrNotFound)},
	}
	sleeper := &fakeSleeper{}
	d, err := New(inner, WithAttempts(5), WithBackoff(func(int) time.Duration { return time.Millisecond }), WithSleeper(sleeper.Sleep))
	if err != nil {
		t.Fatal(err)
	}

	err = d.Fetch(ctx, testPin(cas.HashBytes([]byte("x"))), cas.NewFilesystemStore(t.TempDir()))
	if !errors.Is(err, downloader.ErrNotFound) {
		t.Fatalf("expected wrapped downloader.ErrNotFound, got %v", err)
	}
	if got := inner.callCount(); got != 1 {
		t.Fatalf("call count: got %d, want 1", got)
	}
	if len(sleeper.delays()) != 0 {
		t.Fatalf("expected no sleeps for not-found errors, got %#v", sleeper.delays())
	}
}

func TestFetchUsesCustomRetryPredicate(t *testing.T) {
	ctx := context.Background()
	inner := &stubDownloader{
		id:   "hard",
		errs: []error{errors.New("permanent")},
	}
	d, err := New(inner,
		WithAttempts(4),
		WithRetryable(func(error) bool { return false }),
		WithBackoff(func(int) time.Duration { return time.Millisecond }),
		WithSleeper(func(context.Context, time.Duration) error {
			t.Fatalf("sleep should not be called when retry predicate rejects the error")
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	err = d.Fetch(ctx, testPin(cas.HashBytes([]byte("x"))), cas.NewFilesystemStore(t.TempDir()))
	if err == nil {
		t.Fatalf("expected error")
	}
	if errors.Is(err, downloader.ErrNotFound) {
		t.Fatalf("hard error must not masquerade as ErrNotFound")
	}
	if got := inner.callCount(); got != 1 {
		t.Fatalf("call count: got %d, want 1", got)
	}
}

func TestFetchPropagatesContextCancellationDuringSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inner := &stubDownloader{
		id:   "flaky",
		errs: []error{errors.New("transient")},
	}
	sleeper := func(ctx context.Context, d time.Duration) error {
		cancel()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}
	d, err := New(inner,
		WithAttempts(3),
		WithBackoff(func(int) time.Duration { return time.Millisecond }),
		WithSleeper(sleeper),
	)
	if err != nil {
		t.Fatal(err)
	}

	err = d.Fetch(ctx, testPin(cas.HashBytes([]byte("x"))), cas.NewFilesystemStore(t.TempDir()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if got := inner.callCount(); got != 1 {
		t.Fatalf("call count: got %d, want 1", got)
	}
}
