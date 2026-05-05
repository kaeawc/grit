package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSucceedsOnFirstAttempt(t *testing.T) {
	exec := New(&Instant{}, nil)
	calls := 0
	err := exec.Do(context.Background(), Policy{MaxAttempts: 3}, func(context.Context, int) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calls != 1 {
		t.Errorf("op called %d times, want 1", calls)
	}
}

func TestRetriesUntilSuccess(t *testing.T) {
	s := &Instant{}
	exec := New(s, nil)
	calls := 0
	err := exec.Do(context.Background(), Policy{MaxAttempts: 5}, func(_ context.Context, attempt int) error {
		calls++
		if attempt < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calls != 3 {
		t.Errorf("op called %d times, want 3", calls)
	}
	if len(s.Delays) != 2 {
		t.Errorf("recorded %d delays, want 2", len(s.Delays))
	}
}

func TestExhaustsMaxAttempts(t *testing.T) {
	exec := New(&Instant{}, nil)
	want := errors.New("permafail")
	calls := 0
	err := exec.Do(context.Background(), Policy{MaxAttempts: 3}, func(context.Context, int) error {
		calls++
		return want
	})
	if calls != 3 {
		t.Errorf("op called %d times, want 3", calls)
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want it to wrap %v", err, want)
	}
}

func TestRetryableFalseStopsImmediately(t *testing.T) {
	exec := New(&Instant{}, nil)
	want := errors.New("fatal")
	calls := 0
	err := exec.Do(context.Background(), Policy{
		MaxAttempts: 5,
		Retryable:   func(error) bool { return false },
	}, func(context.Context, int) error {
		calls++
		return want
	})
	if calls != 1 {
		t.Errorf("op called %d times, want 1", calls)
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestExponentialBackoff(t *testing.T) {
	s := &Instant{}
	exec := New(s, nil)
	_ = exec.Do(context.Background(), Policy{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		Multiplier:  2.0,
	}, func(context.Context, int) error {
		return errors.New("x")
	})
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}
	if len(s.Delays) != len(want) {
		t.Fatalf("got %d delays %v, want %d %v", len(s.Delays), s.Delays, len(want), want)
	}
	for i, d := range want {
		if s.Delays[i] != d {
			t.Errorf("delay[%d] = %v, want %v", i, s.Delays[i], d)
		}
	}
}

func TestMaxDelayCap(t *testing.T) {
	s := &Instant{}
	exec := New(s, nil)
	_ = exec.Do(context.Background(), Policy{
		MaxAttempts: 6,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    250 * time.Millisecond,
		Multiplier:  2.0,
	}, func(context.Context, int) error { return errors.New("x") })

	// 100, 200, 250 (capped), 250, 250
	want := []time.Duration{100, 200, 250, 250, 250}
	for i, ms := range want {
		got := s.Delays[i]
		if got != ms*time.Millisecond {
			t.Errorf("delay[%d] = %v, want %v", i, got, ms*time.Millisecond)
		}
	}
}

func TestJitterAppliesDeterministicOffset(t *testing.T) {
	s := &Instant{}
	exec := New(s, &fakeRandom{floats: []float64{0, 1, 0.5}})
	_ = exec.Do(context.Background(), Policy{
		MaxAttempts:    4,
		BaseDelay:      100 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.5,
	}, func(context.Context, int) error { return errors.New("x") })

	want := []time.Duration{50 * time.Millisecond, 300 * time.Millisecond, 400 * time.Millisecond}
	if len(s.Delays) != len(want) {
		t.Fatalf("got %d delays %v, want %d %v", len(s.Delays), s.Delays, len(want), want)
	}
	for i, d := range want {
		if s.Delays[i] != d {
			t.Errorf("delay[%d] = %v, want %v", i, s.Delays[i], d)
		}
	}
}

func TestJitterClampsNegativeDelay(t *testing.T) {
	s := &Instant{}
	exec := New(s, &fakeRandom{floats: []float64{0}})
	_ = exec.Do(context.Background(), Policy{
		MaxAttempts:    2,
		BaseDelay:      100 * time.Millisecond,
		JitterFraction: 2.0,
	}, func(context.Context, int) error { return errors.New("x") })

	if len(s.Delays) != 1 {
		t.Fatalf("got %d delays %v, want 1", len(s.Delays), s.Delays)
	}
	if s.Delays[0] != 0 {
		t.Fatalf("delay = %v, want 0", s.Delays[0])
	}
}

func TestContextCancelStopsRetry(t *testing.T) {
	exec := New(Real{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := exec.Do(ctx, Policy{MaxAttempts: 5, BaseDelay: time.Hour}, func(context.Context, int) error {
		return errors.New("x")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestOnRetryCallback(t *testing.T) {
	exec := New(&Instant{}, nil)
	var attempts []int
	_ = exec.Do(context.Background(), Policy{
		MaxAttempts: 3,
		OnRetry: func(attempt int, _ error, _ time.Duration) {
			attempts = append(attempts, attempt)
		},
	}, func(context.Context, int) error { return errors.New("x") })

	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 {
		t.Errorf("OnRetry calls = %v, want [1, 2]", attempts)
	}
}

type fakeRandom struct {
	floats []float64
}

func (f *fakeRandom) Float64() float64 {
	if len(f.floats) == 0 {
		panic("fakeRandom: no float values left")
	}
	out := f.floats[0]
	f.floats = f.floats[1:]
	return out
}

func (*fakeRandom) IntN(int) int {
	panic("fakeRandom: IntN not implemented")
}

func (*fakeRandom) IntRange(int, int) int {
	panic("fakeRandom: IntRange not implemented")
}

func (*fakeRandom) Bytes(int) ([]byte, error) {
	panic("fakeRandom: Bytes not implemented")
}

func (*fakeRandom) UUID() string {
	panic("fakeRandom: UUID not implemented")
}
