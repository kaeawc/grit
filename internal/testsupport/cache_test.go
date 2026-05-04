package testsupport

import "testing"

func TestMemoryCacheRecordsHitsAndMisses(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	if _, ok := cache.Load("missing"); ok {
		t.Fatal("expected miss")
	}
	cache.Store("key", []byte("value"))
	value, ok := cache.Load("key")
	if !ok || string(value) != "value" {
		t.Fatalf("unexpected cache hit: %q %v", value, ok)
	}
	if got, want := cache.Hits(), 1; got != want {
		t.Fatalf("unexpected hit count: got %d want %d", got, want)
	}
	if got, want := cache.Misses(), 1; got != want {
		t.Fatalf("unexpected miss count: got %d want %d", got, want)
	}
}
