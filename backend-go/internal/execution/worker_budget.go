package execution

import (
	"context"
	"time"
)

func (w *Worker) localConcurrency() int {
	if w.Concurrency > 0 {
		return w.Concurrency
	}
	return 1
}
func (w *Worker) executionPermits() chan struct{} {
	w.permitOnce.Do(func() { w.permits = make(chan struct{}, w.localConcurrency()) })
	return w.permits
}
func (w *Worker) acquireExecution(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case w.executionPermits() <- struct{}{}:
		if ctx.Err() != nil {
			w.releaseExecution()
			return false
		}
		return true
	case <-ctx.Done():
		return false
	}
}
func (w *Worker) tryAcquireExecution() bool {
	select {
	case w.executionPermits() <- struct{}{}:
		return true
	default:
		return false
	}
}
func (w *Worker) releaseExecution() { <-w.executionPermits() }
func (w *Worker) operationTimeout() time.Duration {
	d := 5 * time.Second
	if w.Lease > 0 && w.Lease/4 < d {
		d = w.Lease / 4
	}
	if d <= 0 {
		return time.Nanosecond
	}
	return d
}

// Stop a little before the DB lease expires, independent of any stalled IO.
func (w *Worker) leaseSafetyWindow() time.Duration {
	margin := w.Lease / 10
	if margin > 5*time.Second {
		margin = 5 * time.Second
	}
	return w.Lease - margin
}
func (w *Worker) expireLocalLease(ctl *executionControl) {
	w.mu.Lock()
	if current := w.inflight[ctl.own.RunID]; current != ctl || ctl.leaseExpired {
		w.mu.Unlock()
		return
	}
	remaining := time.Until(ctl.leaseRenewedAt.Add(w.leaseSafetyWindow()))
	if remaining > 0 {
		ctl.leaseTimer.Reset(remaining)
		w.mu.Unlock()
		return
	}
	ctl.leaseExpired = true
	w.mu.Unlock()
	ctl.cancel()
	// Do not release capacity here: the handler may still be unwinding. Its
	// fenced cleanup and the external-submission reconciliation own that decision.
	if w.Log != nil {
		w.Log.Warn("local lease safety deadline reached", "run_id", ctl.own.RunID.String())
	}
}
