package execution

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

type workerLifetimeHandler func(context.Context, *ClaimedRun) error

func (f workerLifetimeHandler) Execute(ctx context.Context, claimed *ClaimedRun) error {
	return f(ctx, claimed)
}

func TestWorkerExecuteCancelsContextOnReturn(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"handler_error", errors.New("handler failed")},
		{"ownership_lost", ErrLostOwnership},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			var child context.Context
			own := ExecutionOwnership{RunID: ids.New(), WorkerID: "worker", LeaseEpoch: 1, LeaseToken: ids.New()}
			w := &Worker{Log: slog.Default(), Handler: workerLifetimeHandler(func(ctx context.Context, _ *ClaimedRun) error { child = ctx; return tc.err })}
			w.execute(parent, &ClaimedRun{Run: &Run{ID: own.RunID}, Ownership: own}, time.Now())
			if child == nil || child.Err() != context.Canceled {
				t.Fatal("completed execution retained an uncancelled child context")
			}
			if parent.Err() != nil {
				t.Fatal("execution cleanup cancelled its parent")
			}
			if len(w.inflight) != 0 {
				t.Fatal("completed execution stayed registered for heartbeat")
			}
		})
	}
}

func TestWorkerStaleAttemptCannotChangeReplacementControl(t *testing.T) {
	for _, change := range []string{"epoch", "token", "worker"} {
		t.Run(change, func(t *testing.T) {
			old := ExecutionOwnership{RunID: ids.New(), WorkerID: "worker", LeaseEpoch: 1, LeaseToken: ids.New()}
			current := old
			current.LeaseEpoch++
			current.LeaseToken = ids.New()
			stale := current
			switch change {
			case "epoch":
				stale.LeaseEpoch--
			case "token":
				stale.LeaseToken = ids.New()
			case "worker":
				stale.WorkerID = "stale-worker"
			}
			w := &Worker{}
			_, oldCancel := context.WithCancel(context.Background())
			defer oldCancel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w.trackInflight(old, oldCancel, time.Now())
			w.trackInflight(current, cancel, time.Now())
			replacement := w.inflight[current.RunID]
			slot := newProviderSlot("test", current)
			w.attachProviderSlot(current, slot)
			w.attachProviderSlot(stale, newProviderSlot("test", stale))
			if replacement.slot != slot {
				t.Error("stale slot attachment overwrote replacement attempt's reservation")
			}
			w.trackInflight(stale, nil, time.Time{})
			if w.inflight[current.RunID] != replacement {
				t.Error("stale attempt cleanup removed replacement heartbeat registration")
			}
			if ctx.Err() != nil {
				t.Error("stale cleanup cancelled replacement execution")
			}
			// The current owner must still be able to unregister normally.
			w.trackInflight(current, nil, time.Time{})
			if len(w.inflight) != 0 {
				t.Error("current owner cleanup did not unregister")
			}
		})
	}
}

// Force the old handler to return only after its replacement is executing.
// This models recovery/deferral overlap without any sleeps or shared stores.
func TestWorkerOldExecutionCompletionKeepsNewAttemptAlive(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := ExecutionOwnership{RunID: ids.New(), WorkerID: "worker", LeaseEpoch: 1, LeaseToken: ids.New()}
	current := old
	current.LeaseEpoch++
	current.LeaseToken = ids.New()
	oldStarted, newStarted := make(chan context.Context, 1), make(chan context.Context, 1)
	releaseOld, releaseNew := make(chan struct{}), make(chan struct{})
	oldDone, newDone := make(chan struct{}), make(chan struct{})
	w := &Worker{Log: slog.Default(), Handler: workerLifetimeHandler(func(ctx context.Context, claimed *ClaimedRun) error {
		entered, release := oldStarted, releaseOld
		if claimed.Ownership == current {
			entered, release = newStarted, releaseNew
		}
		entered <- ctx
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})}
	waitDone := func(done <-chan struct{}) {
		t.Helper()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("execution did not finish")
		}
	}
	awaitContext := func(started <-chan context.Context) context.Context {
		t.Helper()
		select {
		case ctx := <-started:
			return ctx
		case <-time.After(2 * time.Second):
			t.Fatal("execution did not start")
			return nil
		}
	}
	go func() {
		defer close(oldDone)
		w.execute(parent, &ClaimedRun{Run: &Run{ID: old.RunID}, Ownership: old}, time.Now())
	}()
	t.Cleanup(func() { cancel(); waitDone(oldDone) })
	oldCtx := awaitContext(oldStarted)
	go func() {
		defer close(newDone)
		w.execute(parent, &ClaimedRun{Run: &Run{ID: current.RunID}, Ownership: current}, time.Now())
	}()
	t.Cleanup(func() { cancel(); waitDone(newDone) })
	newCtx := awaitContext(newStarted)
	close(releaseOld)
	waitDone(oldDone)
	w.mu.Lock()
	ctl := w.inflight[current.RunID]
	w.mu.Unlock()
	if ctl == nil || ctl.own != current {
		t.Error("old handler completion removed new attempt's heartbeat control")
	}
	if oldCtx.Err() != context.Canceled {
		t.Error("completed handler context was not cancelled")
	}
	if newCtx.Err() != nil {
		t.Error("old handler completion cancelled new attempt")
	}
	close(releaseNew)
	waitDone(newDone)
	if newCtx.Err() != context.Canceled {
		t.Error("replacement handler context was not cancelled on completion")
	}
}
