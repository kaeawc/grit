package tooldiag

import (
	"context"
	"sync"
)

type Record struct {
	Tool              string
	Severity          string
	Code              string
	Category          string
	Message           string
	File              string
	Line              int
	Column            int
	SourceKind        string
	Stream            string
	RelatedDependency string
}

type collectorKey struct{}

type Collector struct {
	mu      sync.Mutex
	records []Record
}

func WithCollector(ctx context.Context, collector *Collector) context.Context {
	if collector == nil {
		return ctx
	}
	return context.WithValue(ctx, collectorKey{}, collector)
}

func RecordAll(ctx context.Context, records []Record) {
	collector, _ := ctx.Value(collectorKey{}).(*Collector)
	if collector == nil || len(records) == 0 {
		return
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.records = append(collector.records, records...)
}

func (c *Collector) Records() []Record {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Record, len(c.records))
	copy(out, c.records)
	return out
}
