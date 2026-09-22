package execution

import (
	"context"
	"testing"
	"time"
)

func TestLeaseWatchdogStopsExecutionWithoutHeartbeatRoundTrip(t *testing.T) {
	w := &Worker{Lease: 40 * time.Millisecond}
	own := ownershipFixture(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.trackInflight(own, cancel, time.Now())
	defer w.trackInflight(own, nil, time.Time{})
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("execution survived lease expiry without DB heartbeat returning")
	}
}

func TestLeaseWatchdogUsesRenewedDeadline(t *testing.T) {
	w := &Worker{Lease: 250 * time.Millisecond}
	own := ownershipFixture(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.trackInflight(own, cancel, time.Now())
	defer w.trackInflight(own, nil, time.Time{})
	w.mu.Lock()
	ctl := w.inflight[own.RunID]
	w.mu.Unlock()
	// Extend from a monotonic future baseline to test timer rearming without sleeps.
	w.recordHeartbeatSuccess(own.RunID, ctl, time.Now().Add(time.Second))
	select {
	case <-ctx.Done():
		t.Fatal("old timer cancelled renewed ownership")
	case <-time.After(350 * time.Millisecond):
	}
	// With no more renewals the independently armed deadline must still fire.
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expired safety deadline did not cancel")
	}
}

func TestExpiredLocalLeaseNeverRenewsAgain(t *testing.T) {
	w := &Worker{Lease: 20 * time.Millisecond}
	own := ownershipFixture(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.trackInflight(own, cancel, time.Now())
	defer w.trackInflight(own, nil, time.Time{})
	w.mu.Lock()
	ctl := w.inflight[own.RunID]
	w.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop execution")
	}
	// Svc is deliberately nil: an expired control must never reach database IO.
	w.heartbeatOne(context.Background(), own.RunID, inflightSnapshot{ctl: ctl, leaseRenewedAt: time.Now()})
}

func TestLateOldRegistrationCannotCancelReplacement(t *testing.T) {
	w := &Worker{}
	old := ownershipFixture(1)
	current := old
	current.LeaseEpoch++
	currentCtx, currentCancel := context.WithCancel(context.Background())
	defer currentCancel()
	oldCtx, oldCancel := context.WithCancel(context.Background())
	defer oldCancel()
	w.trackInflight(current, currentCancel, time.Now())
	defer w.trackInflight(current, nil, time.Time{})
	w.trackInflight(old, oldCancel, time.Now())
	if currentCtx.Err() != nil {
		t.Fatal("late old registration cancelled the current execution")
	}
	if oldCtx.Err() == nil {
		t.Fatal("late old registration was not rejected")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inflight[current.RunID].own != current {
		t.Fatal("late old registration replaced current ownership")
	}
}
