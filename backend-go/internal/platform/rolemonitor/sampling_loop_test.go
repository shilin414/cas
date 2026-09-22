package rolemonitor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestLoopBudgetsRejectImpossibleStaleWindow(t *testing.T) {
	c := probeConfig()
	c.StaleAfter = 10 * time.Second
	loops := map[string]LoopBudget{"delivery_reclaim": {Interval: 20 * time.Second, OperationBudget: 5 * time.Second}}
	if err := c.ValidateLoopBudgets(loops); err == nil {
		t.Fatal("reclaim interval exceeds accepted stale window")
	}
	c.StaleAfter = 50 * time.Second
	if err := c.ValidateLoopBudgets(loops); err == nil {
		t.Fatal("no margin beyond interval+work budget")
	}
	c.StaleAfter = 51 * time.Second
	if err := c.ValidateLoopBudgets(loops); err != nil {
		t.Fatal(err)
	}
	loops["disabled"] = LoopBudget{}
	if err := c.ValidateLoopBudgets(loops); err == nil {
		t.Fatal("required loop never runs")
	}
	c.Address = ""
	if err := c.ValidateLoopBudgets(loops); err != nil {
		t.Fatal("disabled monitoring rejected inactive loops")
	}
}
func TestSamplingBackoffIsBoundedAndResetsAfterSuccess(t *testing.T) {
	m := fixture(t, goodDeps())
	attempt := 0
	completed := false
	m.deps.Sample = func(ctx context.Context) (Snapshot, error) {
		attempt++
		completed = true
		if attempt == 6 {
			return Snapshot{}, nil
		}
		return Snapshot{}, errors.New("DB unavailable")
	}
	var delays []time.Duration
	m.sampleLoop(context.Background(), func(ctx context.Context, d time.Duration) bool {
		if !completed {
			t.Fatal("delay was not after completed sample")
		}
		completed = false
		delays = append(delays, d)
		return len(delays) < 7
	})
	base := m.cfg.SampleInterval
	want := []time.Duration{2 * base, 4 * base, 8 * base, 16 * base, 32 * base, base, 2 * base}
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays=%v want=%v", delays, want)
	}
	for _, n := range []uint{20, 100, 10000} {
		if got := samplingDelay(base, n); got != 5*time.Minute {
			t.Fatalf("unbounded backoff %s", got)
		}
	}
}
func TestSamplingLoopStopsBeforeQueryOrAfterCancelledDelay(t *testing.T) {
	calls := 0
	m := fixture(t, goodDeps())
	m.deps.Sample = func(context.Context) (Snapshot, error) { calls++; return Snapshot{}, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.sampleLoop(ctx, waitForSampleDelay)
	if calls != 0 {
		t.Fatal("cancelled loop sampled DB")
	}
	ctx, cancel = context.WithCancel(context.Background())
	m.sampleLoop(ctx, func(ctx context.Context, d time.Duration) bool { cancel(); return waitForSampleDelay(ctx, d) })
	if calls != 1 {
		t.Fatalf("retry after shutdown: %d", calls)
	}
}

func TestSamplingWaitsFullDelayAfterSlowCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := fixture(t, goodDeps())
		m.cfg.SampleTimeout = 2 * time.Second
		var started []time.Time
		m.deps.Sample = func(context.Context) (Snapshot, error) {
			started = append(started, time.Now())
			time.Sleep(time.Second)
			if len(started) < 3 {
				return Snapshot{}, errors.New("temporary sample failure")
			}
			return Snapshot{}, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go m.sampleLoop(ctx, waitForSampleDelay)
		// Production intervals stay >=5s; fake time avoids weakening that contract.
		time.Sleep(34 * time.Second)
		synctest.Wait()
		cancel()
		synctest.Wait()
		if len(started) != 3 {
			t.Fatalf("samples=%d, want only starts at t=0,11,32", len(started))
		}
		if started[1].Sub(started[0]) != 11*time.Second || started[2].Sub(started[1]) != 21*time.Second {
			t.Fatalf("did not wait after completion: %v", started)
		}
	})
}
