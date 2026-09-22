package rolemonitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Dependencies struct {
	// Callbacks must honor context cancellation. Never return secrets to clients.
	DB, Redis func(context.Context) error
	Sample    func(context.Context) (Snapshot, error)
}
type loopState struct {
	last     time.Time
	success  bool
	failures uint64
}
type Monitor struct {
	cfg                   Config
	deps                  Dependencies
	registry              *prometheus.Registry
	now                   func() time.Time
	mu                    sync.RWMutex
	loops                 map[string]loopState
	snapshot              Snapshot
	sampledAt             time.Time
	sampleOK              bool
	sampleFailures        uint64
	stopping              bool
	started               bool
	probeGate, sampleGate chan struct{}
	desc                  map[string]*prometheus.Desc
}

func New(cfg Config, role, provider string, loops map[string]LoopBudget, reg *prometheus.Registry, deps Dependencies) (*Monitor, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := cfg.ValidateLoopBudgets(loops); err != nil {
		return nil, err
	}
	if reg == nil || deps.DB == nil || deps.Redis == nil || deps.Sample == nil || len(loops) == 0 {
		return nil, fmt.Errorf("monitor requires registry, dependency checks, sampler and loops")
	}
	m := &Monitor{cfg: cfg, deps: deps, registry: reg, now: time.Now, loops: make(map[string]loopState), probeGate: make(chan struct{}, 1), sampleGate: make(chan struct{}, 1), desc: make(map[string]*prometheus.Desc)}
	for name := range loops {
		if name == "" {
			return nil, fmt.Errorf("monitor loop name is empty")
		}
		m.loops[name] = loopState{}
	}
	labels := prometheus.Labels{"role": role, "provider": provider}
	for name, help := range map[string]string{
		"queue_depth":                           "Last successful MySQL active queue depth; includes delayed entries. Gate on sample freshness.",
		"queue_oldest_wait_seconds":             "Age of oldest queued entry at sample time, using MySQL clock.",
		"outbox_pending":                        "Global pending outbox count, duplicated across role instances; do not sum.",
		"sample_success":                        "Whether the latest complete sample succeeded; zero before first sample.",
		"sample_last_success_timestamp_seconds": "Unix time of last successful complete sample; zero before first success.",
		"sample_failures_total":                 "Failed complete sampling attempts.",
		"stale_after_seconds":                   "Configured maximum age of successful loop progress or sample.",
		"loop_last_success_timestamp_seconds":   "Unix time of last real successful loop pass, including empty scans.",
		"loop_success":                          "Whether the most recent loop pass succeeded; zero before first pass.",
		"loop_failures_total":                   "Failed loop passes.",
		"provider_capacity":                     "Last successful MySQL capacity sample; observations, not admission decisions.",
	} {
		var variable []string
		if name == "provider_capacity" {
			variable = []string{"kind"}
		} else if name == "loop_last_success_timestamp_seconds" || name == "loop_success" || name == "loop_failures_total" {
			variable = []string{"loop"}
		}
		m.desc[name] = prometheus.NewDesc("studio_role_"+name, help, variable, labels)
	}
	if err := reg.Register(m); err != nil {
		var duplicate prometheus.AlreadyRegisteredError
		if errors.As(err, &duplicate) {
			return nil, fmt.Errorf("role monitor already registered: use the original monitor and callbacks")
		}
		return nil, fmt.Errorf("register role monitor: %w", err)
	}
	return m, nil
}

// ObserveLoop is a cheap, bounded in-memory callback. Install before Run. It must
// only be called after real work/IO completes, never from a heartbeat timer.
func (m *Monitor) ObserveLoop(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.loops[name]
	if !ok {
		return
	}
	s.success = err == nil
	if err == nil {
		s.last = m.now()
	} else {
		s.failures++
	}
	m.loops[name] = s
}
func (m *Monitor) SampleNow(ctx context.Context) bool {
	select {
	case m.sampleGate <- struct{}{}:
		defer func() { <-m.sampleGate }()
	default:
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, m.cfg.SampleTimeout)
	defer cancel()
	value, err := m.deps.Sample(ctx)
	if err == nil {
		err = ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sampleOK = err == nil
	if err != nil {
		m.sampleFailures++
		return false
	}
	m.snapshot = value
	m.sampledAt = m.now()
	return true
}
func (m *Monitor) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range m.desc {
		ch <- d
	}
}
func (m *Monitor) Collect(ch chan<- prometheus.Metric) {
	// Release the mutex before sending to the Prometheus channel. Slow scrapers
	// must never hold up a business-loop callback.
	m.mu.RLock()
	snapshot, at, ok, failures := m.snapshot, m.sampledAt, m.sampleOK, m.sampleFailures
	loops := make(map[string]loopState, len(m.loops))
	for k, v := range m.loops {
		loops[k] = v
	}
	m.mu.RUnlock()
	emit := func(name string, kind prometheus.ValueType, value float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(m.desc[name], kind, value, labels...)
	}
	emit("sample_success", prometheus.GaugeValue, boolean(ok))
	emit("sample_last_success_timestamp_seconds", prometheus.GaugeValue, unix(at))
	emit("sample_failures_total", prometheus.CounterValue, float64(failures))
	emit("stale_after_seconds", prometheus.GaugeValue, m.cfg.StaleAfter.Seconds())
	for name, s := range loops {
		emit("loop_last_success_timestamp_seconds", prometheus.GaugeValue, unix(s.last), name)
		emit("loop_success", prometheus.GaugeValue, boolean(s.success), name)
		emit("loop_failures_total", prometheus.CounterValue, float64(s.failures), name)
	}
	if at.IsZero() {
		return
	}
	emit("queue_depth", prometheus.GaugeValue, float64(snapshot.Queued))
	emit("queue_oldest_wait_seconds", prometheus.GaugeValue, snapshot.OldestWaitSeconds)
	emit("outbox_pending", prometheus.GaugeValue, float64(snapshot.PendingOutbox))
	if snapshot.HasCapacity {
		for kind, value := range map[string]int64{"controlled": snapshot.Controlled, "uncontrolled": snapshot.Uncontrolled, "effective": snapshot.Effective} {
			emit("provider_capacity", prometheus.GaugeValue, float64(value), kind)
		}
	}
}
func boolean(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
func unix(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}
func (m *Monitor) fresh() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := m.now()
	if m.stopping || !m.sampleOK || m.sampledAt.IsZero() || now.Sub(m.sampledAt) > m.cfg.StaleAfter {
		return false
	}
	for _, s := range m.loops {
		if !s.success || s.last.IsZero() || now.Sub(s.last) > m.cfg.StaleAfter {
			return false
		}
	}
	return true
}
func (m *Monitor) ready(w http.ResponseWriter, r *http.Request) {
	// At most one dependency probe is outstanding, even if a callback ignores its
	// deadline. The outer HTTP timeout bounds responses without spawning retries.
	select {
	case m.probeGate <- struct{}{}:
		defer func() { <-m.probeGate }()
	default:
		status(w, false)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), m.cfg.ProbeTimeout)
	defer cancel()
	dbErr := m.deps.DB(ctx)
	redisErr := m.deps.Redis(ctx)
	status(w, dbErr == nil && redisErr == nil && ctx.Err() == nil && m.fresh())
}
func status(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready"}`))
		return
	}
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
func (m *Monitor) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Timeout: m.cfg.RequestTimeout, MaxRequestsInFlight: 2}))
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		stopping := m.stopping
		m.mu.RUnlock()
		status(w, !stopping)
	})
	mux.HandleFunc("GET /health/ready", m.ready)
	return http.TimeoutHandler(mux, m.cfg.RequestTimeout, `{"status":"timeout"}`)
}

// Start binds synchronously (bad/occupied addresses are startup errors), then
// serves and samples until cancellation. Disabled monitoring does no extra IO.
// Runtime listener errors are reported via errors; callers should stop the role.
func (m *Monitor) Start(ctx context.Context) (<-chan error, error) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil, fmt.Errorf("monitor already started")
	}
	m.started = true
	m.mu.Unlock()
	errs := make(chan error, 1)
	if m.cfg.Address == "" {
		close(errs)
		return errs, nil
	}
	listener, err := net.Listen("tcp", m.cfg.Address)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: m.cfg.RequestTimeout, ReadTimeout: m.cfg.RequestTimeout, WriteTimeout: m.cfg.RequestTimeout + time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	monitorCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(errs)
		defer cancel()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	go m.sampleLoop(monitorCtx, waitForSampleDelay)
	go func() {
		<-monitorCtx.Done()
		m.mu.Lock()
		m.stopping = true
		m.mu.Unlock()
		shutdown, c := context.WithTimeout(context.Background(), m.cfg.RequestTimeout)
		defer c()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	return errs, nil
}
