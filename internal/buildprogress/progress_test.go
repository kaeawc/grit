package buildprogress

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestDisabledReporterIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, false)
	r.Phase("resolve", 10)
	r.Item("resolve", "a:b:1")
	r.PhaseDone("resolve")
	if buf.Len() != 0 {
		t.Fatalf("expected no output when disabled, got %q", buf.String())
	}
	if r.Enabled() {
		t.Fatal("disabled reporter should report Enabled()=false")
	}
}

func TestNilReceiverIsNoOp(t *testing.T) {
	var r *Reporter
	// Must not panic.
	r.Phase("resolve", 1)
	r.Item("resolve", "a")
	r.PhaseDone("resolve")
	if r.Enabled() {
		t.Fatal("nil reporter should report Enabled()=false")
	}
}

func TestPhaseEmitsStartAndDone(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, true)
	r.Phase("resolve", 3)
	r.PhaseDone("resolve")
	out := buf.String()
	if !strings.Contains(out, "phase: resolve  count=3") {
		t.Fatalf("missing phase marker: %q", out)
	}
	if !strings.Contains(out, "phase done: resolve") {
		t.Fatalf("missing phase done marker: %q", out)
	}
}

func TestItemRespectsRateLimitAndFinalsAlwaysEmit(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, true)
	r.Phase("materialize", 5)
	// 5 items in a tight loop. The first emits (count%stride==0 false but
	// it's also the first emit after Phase so lastEmit is zero), and the
	// final one always emits because count==total. Middle items might be
	// suppressed under the rate limit.
	for i := 0; i < 5; i++ {
		r.Item("materialize", "")
	}
	r.PhaseDone("materialize")
	out := buf.String()
	if !strings.Contains(out, "item materialize 5/5") {
		t.Fatalf("final item should always emit, got:\n%s", out)
	}
	if !strings.Contains(out, "phase done: materialize  elapsed=") {
		t.Fatalf("missing phase done marker:\n%s", out)
	}
}

func TestItemWithoutPhaseStillEmits(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, true)
	r.Item("orphan", "x")
	r.PhaseDone("orphan")
	out := buf.String()
	if !strings.Contains(out, "item orphan 1 x") {
		t.Fatalf("expected orphan item to emit anyway: %q", out)
	}
}

func TestItemsAcrossGoroutinesAreSerialized(t *testing.T) {
	var buf bytes.Buffer
	r := NewReporter(&buf, true)
	r.Phase("parallel", 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Item("parallel", "x")
		}()
	}
	wg.Wait()
	r.PhaseDone("parallel")
	// The mutex prevents interleaved fragments. Every emitted line
	// should start with "[", and the phase done line should declare
	// items=100.
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if !strings.HasPrefix(line, "[") {
			t.Fatalf("interleaved output: %q", line)
		}
	}
	if !strings.Contains(buf.String(), "items=100") {
		t.Fatalf("expected items=100 in phase done, got:\n%s", buf.String())
	}
}

func TestEnvEnabled(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "Yes", "on"} {
		t.Setenv("GRIT_PROGRESS", v)
		if !envEnabled() {
			t.Errorf("GRIT_PROGRESS=%q should enable", v)
		}
	}
	for _, v := range []string{"", "0", "false", "no", "off"} {
		t.Setenv("GRIT_PROGRESS", v)
		if envEnabled() {
			t.Errorf("GRIT_PROGRESS=%q should not enable", v)
		}
	}
}
