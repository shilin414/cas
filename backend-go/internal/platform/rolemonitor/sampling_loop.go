package rolemonitor

import (
	"context"
	"time"
)

const minSampleInterval = 5 * time.Second
const maxSampleBackoff = 5 * time.Minute

// samplingDelay is measured AFTER completion, so slow samples never leave a
// buffered ticker event ready to cause an immediate second database scan.
func samplingDelay(base time.Duration, failures uint) time.Duration {
	delay := base
	for failures > 0 && delay < maxSampleBackoff {
		delay *= 2
		failures--
	}
	if delay > maxSampleBackoff {
		return maxSampleBackoff
	}
	return delay
}
func waitForSampleDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

// wait is an internal deterministic test seam. Production always uses the real
// cancellation-aware timer; tests do not relax config validation to run quickly.
func (m *Monitor) sampleLoop(ctx context.Context, wait func(context.Context, time.Duration) bool) {
	var failures uint
	for ctx.Err() == nil {
		if m.SampleNow(ctx) {
			failures = 0
		} else if failures < 64 {
			failures++
		}
		if !wait(ctx, samplingDelay(m.cfg.SampleInterval, failures)) {
			return
		}
	}
}
