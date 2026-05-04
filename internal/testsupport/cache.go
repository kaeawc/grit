package testsupport

import (
	"sync"

	"github.com/kaeawc/grit/internal/explain"
)

type CacheProbeRecord struct {
	Key   string
	Probe explain.CacheProbe
}

type MemoryCache struct {
	mu      sync.Mutex
	entries map[string][]byte
	probes  []CacheProbeRecord
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		entries: map[string][]byte{},
	}
}

func (c *MemoryCache) Store(key string, value []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = append([]byte(nil), value...)
}

func (c *MemoryCache) Load(key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.entries[key]
	if !ok {
		c.probes = append(c.probes, CacheProbeRecord{
			Key:   key,
			Probe: explain.CacheMiss("memory-cache", "no entry stored for key"),
		})
		return nil, false
	}
	c.probes = append(c.probes, CacheProbeRecord{
		Key:   key,
		Probe: explain.CacheHit("memory-cache", "entry restored from memory"),
	})
	return append([]byte(nil), value...), true
}

func (c *MemoryCache) LoadOrStore(key string, value []byte) ([]byte, bool) {
	if existing, ok := c.Load(key); ok {
		return existing, true
	}
	c.Store(key, value)
	return append([]byte(nil), value...), false
}

func (c *MemoryCache) ProbeRecords() []CacheProbeRecord {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]CacheProbeRecord(nil), c.probes...)
}

func (c *MemoryCache) Hits() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	hits := 0
	for _, probe := range c.probes {
		if probe.Probe.State == explain.CacheProbeHit {
			hits++
		}
	}
	return hits
}

func (c *MemoryCache) Misses() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	misses := 0
	for _, probe := range c.probes {
		if probe.Probe.State == explain.CacheProbeMiss {
			misses++
		}
	}
	return misses
}
