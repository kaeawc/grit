package admission

import (
	"sync"
	"testing"
	"time"
)

func TestNetworkBudgetAdmitBasic(t *testing.T) {
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     1000,
		RefillBytesPerSec: 0,
	})
	if !nb.Admit(500) {
		t.Fatal("expected admission of 500 bytes from 1000 capacity")
	}
	if !nb.Admit(500) {
		t.Fatal("expected admission of second 500 bytes")
	}
	if nb.Admit(1) {
		t.Fatal("expected denial when budget exhausted")
	}
}

func TestNetworkBudgetAdmitZeroBytes(t *testing.T) {
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	})
	if !nb.Admit(0) {
		t.Fatal("zero-byte admission should always succeed")
	}
	if !nb.Admit(-10) {
		t.Fatal("negative-byte admission should always succeed")
	}
}

func TestNetworkBudgetRefill(t *testing.T) {
	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     1000,
		RefillBytesPerSec: 100,
	})
	nb.now = func() time.Time { return fakeNow }
	nb.lastRefill = fakeNow

	// Drain the bucket.
	nb.Admit(1000)
	if nb.Admit(1) {
		t.Fatal("expected denial after draining")
	}

	// Advance 5 seconds -> refill 500 bytes.
	fakeNow = fakeNow.Add(5 * time.Second)
	if !nb.Admit(500) {
		t.Fatal("expected admission after 5s refill at 100 B/s")
	}
	if nb.Admit(1) {
		t.Fatal("expected denial after consuming refilled tokens")
	}
}

func TestNetworkBudgetRefillCapsAtCapacity(t *testing.T) {
	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     1000,
		RefillBytesPerSec: 500,
	})
	nb.now = func() time.Time { return fakeNow }
	nb.lastRefill = fakeNow

	// Drain 200 bytes.
	nb.Admit(200)

	// Advance 10 seconds -> would refill 5000, but capped at 1000.
	fakeNow = fakeNow.Add(10 * time.Second)
	snap := nb.Snapshot()
	if snap.Available != 1000 {
		t.Fatalf("expected available capped at 1000, got %d", snap.Available)
	}
}

func TestNetworkBudgetReturn(t *testing.T) {
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     1000,
		RefillBytesPerSec: 0,
	})
	nb.Admit(800)

	// Return 300 -> available should be 200+300 = 500.
	nb.Return(300)
	snap := nb.Snapshot()
	if snap.Available != 500 {
		t.Fatalf("expected 500 available after return, got %d", snap.Available)
	}

	// Return a huge amount -> capped at capacity.
	nb.Return(999999)
	snap = nb.Snapshot()
	if snap.Available != 1000 {
		t.Fatalf("expected available capped at capacity, got %d", snap.Available)
	}
}

func TestNetworkBudgetReturnZeroOrNegative(t *testing.T) {
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	})
	nb.Admit(50)
	nb.Return(0)
	nb.Return(-10)
	snap := nb.Snapshot()
	if snap.Available != 50 {
		t.Fatalf("expected 50 unchanged, got %d", snap.Available)
	}
}

func TestNetworkBudgetSnapshot(t *testing.T) {
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     2000,
		RefillBytesPerSec: 100,
	})
	nb.now = func() time.Time { return nb.lastRefill } // freeze time

	nb.Admit(600)  // admitted
	nb.Admit(5000) // denied (not enough)

	snap := nb.Snapshot()
	if snap.Capacity != 2000 {
		t.Fatalf("expected capacity 2000, got %d", snap.Capacity)
	}
	if snap.Available != 1400 {
		t.Fatalf("expected 1400 available, got %d", snap.Available)
	}
	if snap.TotalAdmitted != 600 {
		t.Fatalf("expected 600 admitted, got %d", snap.TotalAdmitted)
	}
	if snap.TotalDenied != 5000 {
		t.Fatalf("expected 5000 denied, got %d", snap.TotalDenied)
	}
	if snap.RefillRate != 100 {
		t.Fatalf("expected refill rate 100, got %d", snap.RefillRate)
	}
}

func TestNetworkBudgetDefaultCapacity(t *testing.T) {
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     0,
		RefillBytesPerSec: -5,
	})
	if nb.capacity != 1 {
		t.Fatalf("expected default capacity 1, got %d", nb.capacity)
	}
	if nb.refillRate != 0 {
		t.Fatalf("expected clamped refill rate 0, got %d", nb.refillRate)
	}
}

func TestNetworkBudgetConcurrency(t *testing.T) {
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     10000,
		RefillBytesPerSec: 0,
	})

	var wg sync.WaitGroup
	var admitted, denied int64
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if nb.Admit(100) {
				mu.Lock()
				admitted += 100
				mu.Unlock()
			} else {
				mu.Lock()
				denied += 100
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Exactly 10000 bytes should have been admitted.
	if admitted != 10000 {
		t.Fatalf("expected exactly 10000 bytes admitted, got %d", admitted)
	}
	if denied != 0 {
		t.Fatalf("expected 0 denied, got %d", denied)
	}
}
