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

func TestAdmitBatchWithDecisionsReturnsDeferRemote(t *testing.T) {
	c := NewController([]configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 10},
	})
	// Set a tight network budget: only 100 bytes capacity, no refill.
	c.SetNetworkBudget(NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	}))

	ready := []configmodel.ActionScheduleStep{
		{
			Action:         graph.Action{ID: "a1", Attributes: map[string]string{"operation": "compile"}},
			WorkerClass:    "kotlin-compile",
			ResourceClass:  "jvm-process",
			ResourceCost:   1,
			MaxParallelism: 10,
			Cacheable:      true,
			ProbeOrder:     []string{"local-overlay", "remote"},
			EstimatedBytes: 80,
		},
		{
			Action:         graph.Action{ID: "a2", Attributes: map[string]string{"operation": "compile"}},
			WorkerClass:    "kotlin-compile",
			ResourceClass:  "jvm-process",
			ResourceCost:   1,
			MaxParallelism: 10,
			Cacheable:      true,
			ProbeOrder:     []string{"local-overlay", "remote"},
			EstimatedBytes: 80,
		},
	}

	admitted, waiting := c.AdmitBatchWithDecisions(ready)
	if len(admitted) != 2 {
		t.Fatalf("expected 2 admitted, got %d", len(admitted))
	}
	if len(waiting) != 0 {
		t.Fatalf("expected 0 waiting, got %d", len(waiting))
	}

	// First action should consume 80 of 100 bytes — no DeferRemote.
	if admitted[0].Decision.DeferRemote {
		t.Fatal("first action should not have DeferRemote")
	}
	// Second action needs 80 bytes but only 20 remain — DeferRemote.
	if !admitted[1].Decision.DeferRemote {
		t.Fatal("second action should have DeferRemote set")
	}
}

func TestAdmitBatchWithDecisionsEmptyInput(t *testing.T) {
	c := NewController(budgets())
	admitted, waiting := c.AdmitBatchWithDecisions(nil)
	if len(admitted) != 0 || len(waiting) != 0 {
		t.Fatal("expected empty results for nil input")
	}
}

func TestAdmitRemoteProbeConsumesOnlyNetworkBudget(t *testing.T) {
	c := NewController([]configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 1},
	})
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	})
	c.SetNetworkBudget(nb)

	first := configmodel.ActionScheduleStep{
		Action:         graph.Action{ID: "a1", Attributes: map[string]string{"operation": "compile"}},
		WorkerClass:    "kotlin-compile",
		ResourceClass:  "jvm-process",
		ResourceCost:   1,
		MaxParallelism: 1,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote"},
		EstimatedBytes: 80,
	}
	second := first
	second.Action.ID = "a2"

	if decision := c.AdmitRemoteProbe(first); decision.ActionID != "a1" || decision.DeferRemote {
		t.Fatalf("expected first remote probe to consume budget without deferral, got %+v", decision)
	} else if !decision.Eligible || decision.EstimatedBytes != 80 || decision.BudgetBeforeBytes != 100 || decision.BudgetAfterBytes != 20 {
		t.Fatalf("expected detailed budget accounting on first probe, got %+v", decision)
	}
	if c.ActiveCount() != 0 {
		t.Fatalf("expected remote-probe admission to avoid action reservations, got %d active", c.ActiveCount())
	}
	snap := nb.Snapshot()
	if snap.Available != 20 {
		t.Fatalf("expected 20 bytes remaining after first remote probe, got %d", snap.Available)
	}

	if decision := c.AdmitRemoteProbe(second); decision.ActionID != "a2" || !decision.DeferRemote {
		t.Fatalf("expected second remote probe to defer, got %+v", decision)
	} else if !decision.Eligible || decision.EstimatedBytes != 80 || decision.BudgetBeforeBytes != 20 || decision.BudgetAfterBytes != 20 {
		t.Fatalf("expected denied probe to preserve before/after budget state, got %+v", decision)
	}
	snap = nb.Snapshot()
	if snap.Available != 20 {
		t.Fatalf("expected denied remote probe to leave budget unchanged, got %d", snap.Available)
	}
}

func TestAdmitRemoteProbeSkipsBudgetForSharedMachineTier(t *testing.T) {
	c := NewController([]configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 1},
	})
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	})
	c.SetNetworkBudget(nb)

	step := configmodel.ActionScheduleStep{
		Action:         graph.Action{ID: "a1", Attributes: map[string]string{"operation": "compile"}},
		WorkerClass:    "kotlin-compile",
		ResourceClass:  "jvm-process",
		ResourceCost:   1,
		MaxParallelism: 1,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "shared-machine"},
		EstimatedBytes: 80,
	}

	if decision := c.AdmitRemoteProbe(step); decision.ActionID != "a1" || decision.DeferRemote {
		t.Fatalf("expected shared-machine probe to bypass remote deferral, got %+v", decision)
	} else if decision.Eligible || decision.EstimatedBytes != 0 || decision.BudgetBeforeBytes != 0 || decision.BudgetAfterBytes != 0 {
		t.Fatalf("expected non-remote tier to skip bandwidth accounting, got %+v", decision)
	}
	if snap := nb.Snapshot(); snap.Available != 100 || snap.TotalAdmitted != 0 || snap.TotalDenied != 0 {
		t.Fatalf("expected shared-machine probe to leave network budget untouched, got %+v", snap)
	}
}

func TestReleaseWithActualReturnsSurplus(t *testing.T) {
	c := NewController([]configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 10},
	})
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     1000,
		RefillBytesPerSec: 0,
	})
	c.SetNetworkBudget(nb)

	s := configmodel.ActionScheduleStep{
		Action:         graph.Action{ID: "a1", Attributes: map[string]string{"operation": "compile"}},
		WorkerClass:    "kotlin-compile",
		ResourceClass:  "jvm-process",
		ResourceCost:   1,
		MaxParallelism: 10,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote"},
		EstimatedBytes: 400,
	}
	d := c.TryAdmit(s)
	if !d.Admitted || d.DeferRemote {
		t.Fatalf("expected admitted without defer, got %+v", d)
	}

	// Budget should have 600 remaining (1000 - 400).
	snap := nb.Snapshot()
	if snap.Available != 600 {
		t.Fatalf("expected 600 available after admit, got %d", snap.Available)
	}

	// Release reporting only 100 actual bytes used; surplus of 300 returned.
	if err := c.ReleaseWithActual("a1", 100); err != nil {
		t.Fatal(err)
	}
	snap = nb.Snapshot()
	if snap.Available != 900 {
		t.Fatalf("expected 900 available after surplus return, got %d", snap.Available)
	}
}

func TestReleaseDeferredActionDoesNotReturnTokens(t *testing.T) {
	c := NewController([]configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 10},
	})
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     100,
		RefillBytesPerSec: 0,
	})
	c.SetNetworkBudget(nb)

	s := configmodel.ActionScheduleStep{
		Action:         graph.Action{ID: "a1", Attributes: map[string]string{"operation": "compile"}},
		WorkerClass:    "kotlin-compile",
		ResourceClass:  "jvm-process",
		ResourceCost:   1,
		MaxParallelism: 10,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote"},
		EstimatedBytes: 200, // exceeds 100-byte budget
	}
	d := c.TryAdmit(s)
	if !d.Admitted {
		t.Fatal("expected admission")
	}
	if !d.DeferRemote {
		t.Fatal("expected DeferRemote=true for action exceeding budget")
	}

	// Budget was denied, so no tokens were taken — still at 100.
	snap := nb.Snapshot()
	if snap.Available != 100 {
		t.Fatalf("expected 100 available (denied actions don't consume), got %d", snap.Available)
	}

	// Release deferred action — budget should remain unchanged.
	if err := c.Release("a1"); err != nil {
		t.Fatal(err)
	}
	snap = nb.Snapshot()
	if snap.Available != 100 {
		t.Fatalf("expected 100 available after deferred release, got %d", snap.Available)
	}
}

func TestFullSnapshotIncludesNetworkBudget(t *testing.T) {
	c := NewController([]configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 4},
	})

	// Without network budget.
	snap := c.FullSnapshot()
	if snap.NetworkBudget != nil {
		t.Fatal("expected nil NetworkBudget when none attached")
	}
	if len(snap.Pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(snap.Pools))
	}

	// Attach a network budget.
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     5000,
		RefillBytesPerSec: 100,
	})
	c.SetNetworkBudget(nb)

	snap = c.FullSnapshot()
	if snap.NetworkBudget == nil {
		t.Fatal("expected non-nil NetworkBudget after attaching")
	}
	if snap.NetworkBudget.Capacity != 5000 {
		t.Fatalf("expected capacity 5000, got %d", snap.NetworkBudget.Capacity)
	}
	if snap.NetworkBudget.RefillRate != 100 {
		t.Fatalf("expected refill rate 100, got %d", snap.NetworkBudget.RefillRate)
	}
	if snap.Active != 0 {
		t.Fatalf("expected 0 active, got %d", snap.Active)
	}
}

func TestReleaseWithActualZeroBytesReturnsFullEstimate(t *testing.T) {
	c := NewController([]configmodel.ResourceBudget{
		{ResourceClass: "jvm-process", Capacity: 10},
	})
	nb := NewNetworkBudget(NetworkBudgetConfig{
		CapacityBytes:     1000,
		RefillBytesPerSec: 0,
	})
	c.SetNetworkBudget(nb)

	s := configmodel.ActionScheduleStep{
		Action:         graph.Action{ID: "a1", Attributes: map[string]string{"operation": "compile"}},
		WorkerClass:    "kotlin-compile",
		ResourceClass:  "jvm-process",
		ResourceCost:   1,
		MaxParallelism: 10,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote"},
		EstimatedBytes: 300,
	}
	c.TryAdmit(s)

	// Report zero actual bytes — full 300 returned.
	if err := c.ReleaseWithActual("a1", 0); err != nil {
		t.Fatal(err)
	}
	snap := nb.Snapshot()
	if snap.Available != 1000 {
		t.Fatalf("expected 1000 available after full return, got %d", snap.Available)
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

func TestNewControllerFromScheduleWithNetworkBudget(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{
			{ResourceClass: "jvm-process", Capacity: 4},
		},
		NetworkBudgetConfig: &configmodel.ScheduleNetworkBudget{
			CapacityBytes:     200,
			RefillBytesPerSec: 0,
		},
	}
	c := NewControllerFromSchedule(schedule)

	// Verify resource pool was created.
	s := step("a1", "compile", "kotlin-compile", "jvm-process", 1, 2)
	d := c.TryAdmit(s)
	if !d.Admitted {
		t.Fatalf("expected admission, got %+v", d)
	}
	_ = c.Release("a1")

	// Verify network budget was attached by admitting a cacheable action
	// with a remote tier and estimated bytes exceeding the budget.
	remoteStep := configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:         graph.ActionID("r1"),
			Attributes: map[string]string{"operation": "compile"},
		},
		WorkerClass:    "kotlin-compile",
		ResourceClass:  "jvm-process",
		ResourceCost:   1,
		MaxParallelism: 2,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote-tier1"},
		EstimatedBytes: 300, // exceeds 200-byte budget
	}
	d = c.TryAdmit(remoteStep)
	if !d.Admitted {
		t.Fatal("expected admission (network budget only defers, doesn't block)")
	}
	if !d.DeferRemote {
		t.Fatal("expected DeferRemote=true when estimated bytes exceed budget")
	}
}

func TestNewControllerFromScheduleWithoutNetworkBudget(t *testing.T) {
	schedule := configmodel.ActionSchedule{
		ResourceBudgets: []configmodel.ResourceBudget{
			{ResourceClass: "jvm-process", Capacity: 2},
		},
		// NetworkBudgetConfig is nil — no bandwidth constraint.
	}
	c := NewControllerFromSchedule(schedule)

	// Cacheable actions with remote tiers should NOT get DeferRemote since
	// no network budget is attached.
	remoteStep := configmodel.ActionScheduleStep{
		Action: graph.Action{
			ID:         graph.ActionID("r1"),
			Attributes: map[string]string{"operation": "compile"},
		},
		WorkerClass:    "kotlin-compile",
		ResourceClass:  "jvm-process",
		ResourceCost:   1,
		MaxParallelism: 2,
		Cacheable:      true,
		ProbeOrder:     []string{"local-overlay", "remote-tier1"},
		EstimatedBytes: 999999,
	}
	d := c.TryAdmit(remoteStep)
	if !d.Admitted {
		t.Fatalf("expected admission, got %+v", d)
	}
	if d.DeferRemote {
		t.Fatal("expected DeferRemote=false when no network budget is attached")
	}
}
