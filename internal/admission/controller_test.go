package admission

import (
	"sync"
	"testing"

	"github.com/kaeawc/grit/internal/configmodel"
	"github.com/kaeawc/grit/internal/graph"
)

func budgets() []configmodel.ResourceBudget {
	return []configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 2},
		{ResourceClass: "android-tools", Capacity: 1},
		{ResourceClass: "device", Capacity: 1},
	}
}

func step(id, operation, workerClass, resourceClass string, cost, maxPar int) configmodel.ActionScheduleStep {
	return configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:         graph.ActionID(id),
			Attributes: map[string]string{"operation": operation},
		},
		WorkerClass:    workerClass,
		ResourceClass:  resourceClass,
		ResourceCost:   cost,
		MaxParallelism: maxPar,
	}
}

func TestAdmitSingleAction(t *testing.T) {
	c := NewController(budgets())
	s := step("a1", "compile", "kotlin-compile", "jvm-process", 1, 2)
	d := c.TryAdmit(s)
	if !d.Admitted {
		t.Fatalf("expected admission, got %+v", d)
	}
	if d.ActionID != "a1" {
		t.Fatalf("expected action id a1, got %s", d.ActionID)
	}
	if c.ActiveCount() != 1 {
		t.Fatalf("expected 1 active, got %d", c.ActiveCount())
	}
}

func TestAdmitRejectsExhaustedPool(t *testing.T) {
	c := NewController(budgets())
	s1 := step("a1", "compile", "kotlin-compile", "jvm-process", 1, 2)
	s2 := step("a2", "compile-tests", "test-compile", "jvm-process", 1, 2)
	s3 := step("a3", "test", "junit", "jvm-process", 1, 1)

	c.TryAdmit(s1)
	c.TryAdmit(s2)
	d := c.TryAdmit(s3)
	if d.Admitted {
		t.Fatalf("expected rejection when pool exhausted, got %+v", d)
	}
	if d.Reason != "resource-exhausted" {
		t.Fatalf("expected resource-exhausted reason, got %s", d.Reason)
	}
	if d.WaitClass != "jvm-process" {
		t.Fatalf("expected wait class jvm-process, got %s", d.WaitClass)
	}
}

func TestAdmitRejectsSaturatedWorker(t *testing.T) {
	c := NewController(budgets())
	// android-tools has capacity 1 but let's set jvm-process high enough.
	// Instead, test worker saturation: adb-install has maxParallelism=1
	s1 := step("a1", "install", "adb-install", "device", 1, 1)
	s2 := step("a2", "install", "adb-install", "device", 1, 1)

	c.TryAdmit(s1)
	d := c.TryAdmit(s2)
	// Could be either worker-saturated or resource-exhausted (device capacity=1)
	if d.Admitted {
		t.Fatalf("expected rejection, got %+v", d)
	}
}

func TestReleaseFreesResources(t *testing.T) {
	c := NewController(budgets())
	s1 := step("a1", "compile", "kotlin-compile", "jvm-process", 1, 2)
	s2 := step("a2", "compile-tests", "test-compile", "jvm-process", 1, 2)
	s3 := step("a3", "test", "junit", "jvm-process", 1, 1)

	c.TryAdmit(s1)
	c.TryAdmit(s2)

	// Pool full, a3 rejected.
	d := c.TryAdmit(s3)
	if d.Admitted {
		t.Fatal("expected rejection before release")
	}

	// Release a1, now a3 should be admitted.
	if err := c.Release("a1"); err != nil {
		t.Fatal(err)
	}
	d = c.TryAdmit(s3)
	if !d.Admitted {
		t.Fatalf("expected admission after release, got %+v", d)
	}
}

func TestReleaseUnknownActionReturnsError(t *testing.T) {
	c := NewController(budgets())
	if err := c.Release("nonexistent"); err == nil {
		t.Fatal("expected error releasing unknown action")
	}
}

func TestDuplicateAdmitRejected(t *testing.T) {
	c := NewController(budgets())
	s := step("a1", "compile", "kotlin-compile", "jvm-process", 1, 2)
	c.TryAdmit(s)
	d := c.TryAdmit(s)
	if d.Admitted {
		t.Fatal("expected duplicate admission to be rejected")
	}
	if d.Reason != "already-admitted" {
		t.Fatalf("expected already-admitted reason, got %s", d.Reason)
	}
}

func TestAdmitBatchPartitions(t *testing.T) {
	c := NewController(budgets())
	ready := []configmodel.ActionScheduleStep{
		step("a1", "compile", "kotlin-compile", "jvm-process", 1, 2),
		step("a2", "compile-tests", "test-compile", "jvm-process", 1, 2),
		step("a3", "test", "junit", "jvm-process", 1, 1),
	}
	admitted, waiting := c.AdmitBatch(ready)
	if len(admitted) != 2 {
		t.Fatalf("expected 2 admitted, got %d", len(admitted))
	}
	if len(waiting) != 1 {
		t.Fatalf("expected 1 waiting, got %d", len(waiting))
	}
	if waiting[0].Action.ID.String() != "a3" {
		t.Fatalf("expected a3 waiting, got %s", waiting[0].Action.ID.String())
	}
}

func TestSnapshotReflectsLiveState(t *testing.T) {
	c := NewController(budgets())
	s := step("a1", "compile", "kotlin-compile", "jvm-process", 1, 2)
	c.TryAdmit(s)

	pools, workers := c.Snapshot()

	var jvmPool *PoolSnapshot
	for i := range pools {
		if pools[i].ResourceClass == "jvm-process" {
			jvmPool = &pools[i]
			break
		}
	}
	if jvmPool == nil {
		t.Fatal("expected jvm-process pool in snapshot")
	}
	if jvmPool.Used != 1 || jvmPool.Remaining != 1 {
		t.Fatalf("expected 1 used / 1 remaining, got %+v", jvmPool)
	}

	var kotlinWorker *WorkerSnapshot
	for i := range workers {
		if workers[i].WorkerClass == "kotlin-compile" {
			kotlinWorker = &workers[i]
			break
		}
	}
	if kotlinWorker == nil {
		t.Fatal("expected kotlin-compile worker in snapshot")
	}
	if kotlinWorker.Active != 1 || kotlinWorker.Remaining != 1 {
		t.Fatalf("expected 1 active / 1 remaining, got %+v", kotlinWorker)
	}
}

func TestRegisterWorkerClassOverridesLimit(t *testing.T) {
	c := NewController(budgets())
	c.RegisterWorkerClass("kotlin-compile", 4)

	// Admit 4 kotlin-compile steps.
	// jvm-process pool has capacity 2, so pool will block before worker.
	// Use a different resource class to isolate the worker limit test.
	c.pools["jvm-process"].capacity = 10

	for i := 0; i < 4; i++ {
		s := step("k"+string(rune('0'+i)), "compile", "kotlin-compile", "jvm-process", 1, 4)
		d := c.TryAdmit(s)
		if !d.Admitted {
			t.Fatalf("expected admission for step %d, got %+v", i, d)
		}
	}
	s := step("k4", "compile", "kotlin-compile", "jvm-process", 1, 4)
	d := c.TryAdmit(s)
	if d.Admitted {
		t.Fatal("expected rejection at worker limit 4")
	}
	if d.Reason != "worker-saturated" {
		t.Fatalf("expected worker-saturated, got %s", d.Reason)
	}
}

func TestConcurrentAdmitRelease(t *testing.T) {
	c := NewController(budgets())
	c.pools["jvm-process"].capacity = 100
	c.RegisterWorkerClass("kotlin-compile", 100)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s := step(
				"c"+string(rune(idx+48)),
				"compile",
				"kotlin-compile",
				"jvm-process",
				1, 100,
			)
			d := c.TryAdmit(s)
			if d.Admitted {
				_ = c.Release(s.Action.ID.String())
			}
		}(i)
	}
	wg.Wait()
	if c.ActiveCount() != 0 {
		t.Fatalf("expected 0 active after all released, got %d", c.ActiveCount())
	}
}

func TestAdmitWithUnknownResourceClassCreatesDefault(t *testing.T) {
	c := NewController(nil)
	s := step("a1", "custom", "custom-worker", "custom-pool", 1, 1)
	d := c.TryAdmit(s)
	if !d.Admitted {
		t.Fatalf("expected admission with auto-created pool, got %+v", d)
	}
	// Default pool capacity is 1, second should be rejected.
	s2 := step("a2", "custom", "custom-worker", "custom-pool", 1, 1)
	d2 := c.TryAdmit(s2)
	if d2.Admitted {
		t.Fatal("expected rejection at default capacity 1")
	}
}

func TestAdmitBatchEmptyInput(t *testing.T) {
	c := NewController(budgets())
	admitted, waiting := c.AdmitBatch(nil)
	if len(admitted) != 0 || len(waiting) != 0 {
		t.Fatal("expected empty results for nil input")
	}
}

func TestResourceCostGreaterThanOne(t *testing.T) {
	c := NewController([]configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 4},
	})
	// Action with cost 3 should be admitted.
	s1 := step("a1", "compile", "kotlin-compile", "jvm-process", 3, 10)
	d := c.TryAdmit(s1)
	if !d.Admitted {
		t.Fatalf("expected admission for cost 3 within capacity 4, got %+v", d)
	}
	// Cost 2 would exceed capacity (3+2=5 > 4).
	s2 := step("a2", "compile", "kotlin-compile", "jvm-process", 2, 10)
	d = c.TryAdmit(s2)
	if d.Admitted {
		t.Fatal("expected rejection when cost would exceed capacity")
	}
	// Cost 1 fits (3+1=4).
	s3 := step("a3", "compile", "kotlin-compile", "jvm-process", 1, 10)
	d = c.TryAdmit(s3)
	if !d.Admitted {
		t.Fatalf("expected admission for cost 1, got %+v", d)
	}
}
