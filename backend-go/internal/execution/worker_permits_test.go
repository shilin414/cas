package execution

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAllWorkerEntrypointsShareOneExecutionBudget(t *testing.T) {
	w := &Worker{Concurrency: 2}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !w.acquireExecution(ctx) {
				return
			}
			defer w.releaseExecution()
			n := active.Add(1)
			for p := peak.Load(); n > p; p = peak.Load() {
				if peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
		}()
	}
	wg.Wait()
	if peak.Load() > 2 || peak.Load() == 0 {
		t.Fatalf("peak=%d", peak.Load())
	}
	if !w.acquireExecution(ctx) || !w.acquireExecution(ctx) {
		t.Fatal("budget leaked")
	}
	defer w.releaseExecution()
	defer w.releaseExecution()
	if w.tryAcquireExecution() {
		w.releaseExecution()
		t.Fatal("recovery bypassed saturated pool")
	}
	stopped, stop := context.WithCancel(ctx)
	stop()
	// Must exit before touching Svc/Redis when a normal consumer waits for a permit.
	w.claimAndExecute(stopped, ownershipFixture(1).RunID, nil)
}

func TestExecutionBudgetDefaultsAndCancelledWait(t *testing.T) {
	w := &Worker{}
	if w.localConcurrency() != 1 || w.operationTimeout() != 5*time.Second {
		t.Fatal("unsafe defaults")
	}
	if !w.tryAcquireExecution() {
		t.Fatal("empty pool rejected recovery")
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan bool, 1)
	go func() { result <- w.acquireExecution(ctx) }()
	cancel()
	select {
	case ok := <-result:
		if ok {
			t.Fatal("cancelled waiter admitted")
		}
	case <-time.After(time.Second):
		t.Fatal("waiter leaked")
	}
	w.releaseExecution()
	w.Lease = time.Nanosecond
	if w.operationTimeout() != time.Nanosecond {
		t.Fatal("timeout truncated to zero")
	}
	w.Lease = 2 * time.Minute
	if w.leaseSafetyWindow() != 115*time.Second {
		t.Fatal("incorrect safety margin")
	}
}
