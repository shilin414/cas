package rolemonitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
)

func TestFreshSampleDoesNotHideStalledLoop(t *testing.T) {
	m := fixture(t, goodDeps())
	clock := time.Now()
	m.now = func() time.Time { return clock }
	m.ObserveLoop("maintenance", nil)
	clock = clock.Add(2 * time.Minute)
	m.SampleNow(context.Background())
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("fresh sample disguised stalled loop")
	}
	m.ObserveLoop("maintenance", nil)
	if request(m, "/health/ready").Code != 200 {
		t.Fatal("completed empty pass did not recover readiness")
	}
	clock = clock.Add(2 * time.Minute)
	m.ObserveLoop("maintenance", nil)
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("fresh loop disguised stalled sampler")
	}
}
func TestAllRequiredLoopsAndCancellation(t *testing.T) {
	m, err := New(configForTest(), "worker", "p", map[string]LoopBudget{"maintenance": {Interval: time.Second}, "fallback_scan": {Interval: time.Second}}, prometheus.NewRegistry(), goodDeps())
	if err != nil {
		t.Fatal(err)
	}
	m.SampleNow(context.Background())
	m.ObserveLoop("maintenance", nil)
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("missing scan progress ignored")
	}
	m.ObserveLoop("fallback_scan", nil)
	if request(m, "/health/ready").Code != 200 {
		t.Fatal("healthy loops not ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.SampleNow(ctx)
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("cancelled sample refreshed health")
	}
	m.mu.Lock()
	m.stopping = true
	m.mu.Unlock()
	if request(m, "/health/live").Code != 503 {
		t.Fatal("shutdown reported live")
	}
	if request(m, "/no-such-endpoint").Code != 404 {
		t.Fatal("unexpected catch-all endpoint")
	}
}
func TestIgnoredDeadlineHasBoundedResponseAndProbeConcurrency(t *testing.T) {
	blocked, entered := make(chan struct{}), make(chan struct{}, 1)
	var calls atomic.Int32
	d := goodDeps()
	d.DB = func(context.Context) error { calls.Add(1); entered <- struct{}{}; <-blocked; return nil }
	m := fixture(t, d)
	m.SampleNow(context.Background())
	m.ObserveLoop("maintenance", nil)
	done := make(chan int, 1)
	go func() { done <- request(m, "/health/ready").Code }()
	<-entered
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("overlapping probe admitted")
	}
	select {
	case code := <-done:
		if code != 503 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP timeout ineffective")
	}
	if calls.Load() != 1 {
		t.Fatal("unbounded dependency goroutines")
	}
	close(blocked)
}
func TestConcurrentSamplingAndCollection(t *testing.T) {
	m := fixture(t, goodDeps())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 10; n++ {
				m.ObserveLoop("maintenance", nil)
				m.SampleNow(context.Background())
				if _, err := m.registry.Gather(); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
func TestSampleFailureCounterAndNoPartialSnapshot(t *testing.T) {
	d := goodDeps()
	d.Sample = func(ctx context.Context) (Snapshot, error) { <-ctx.Done(); return Snapshot{Queued: 1000}, ctx.Err() }
	m := fixture(t, d)
	m.SampleNow(context.Background())
	body := request(m, "/metrics").Body.String()
	if strings.Contains(body, "studio_role_queue_depth{") || !strings.Contains(body, `studio_role_sample_failures_total{provider="test",role="worker"} 1`) {
		t.Fatal(body)
	}
}
func TestMonitorStartDisabledAndBindFailure(t *testing.T) {
	var calls atomic.Int32
	d := goodDeps()
	d.Sample = func(context.Context) (Snapshot, error) { calls.Add(1); return Snapshot{}, nil }
	m := fixture(t, d)
	ch, err := m.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := <-ch; e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 0 {
		t.Fatal("disabled monitor sampled")
	}
	if _, err = m.Start(context.Background()); err == nil {
		t.Fatal("started twice")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	m = fixture(t, d)
	m.cfg.Address = ln.Addr().String()
	if _, err = m.Start(context.Background()); err == nil {
		t.Fatal("occupied listener silently ignored")
	}
}
func TestMonitorHTTPStartAndShutdown(t *testing.T) {
	// Only a loopback test listener and fake callbacks, never a business process.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	m := fixture(t, goodDeps())
	m.cfg.Address = addr
	m.ObserveLoop("maintenance", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := m.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	r, err := client.Get("http://" + addr + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	cancel()
	select {
	case err := <-ch:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("listener did not stop")
	}
}

func TestSQLSamplerEmptyDeliveryGlobalAndCapacityFailures(t *testing.T) {
	for _, mode := range []string{"global", "delivery", "capacity", "capacity_error", "clock_error", "clock_zero", "outbox"} {
		t.Run(mode, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			s := SQLSampler{DB: conn}
			capacity := strings.HasPrefix(mode, "capacity") || strings.HasPrefix(mode, "clock")
			if capacity {
				s.IncludeCapacity = true
				s.Provider = "p"
			}
			table, index := "runs", "idx_runs_claim"
			if mode == "delivery" {
				s.Delivery = true
				table, index = "delivery_executions", "idx_deliveries_pending"
			}
			queue := mock.ExpectQuery(fmt.Sprintf(`SELECT COUNT.*FROM %s FORCE INDEX \(%s\) WHERE status`, table, index))
			if capacity {
				queue.WithArgs("p")
			}
			queue.WillReturnRows(sqlmock.NewRows([]string{"n", "age"}).AddRow(0, 0))
			box := mock.ExpectQuery(`FROM outbox_events FORCE INDEX \(idx_outbox_dispatch\) WHERE status = 'pending'`)
			if mode == "outbox" {
				box.WillReturnError(errors.New("down"))
			} else {
				box.WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
			}
			if capacity {
				// Deliberately far from local wall time: every expiry decision must use
				// exactly this DB clock value, passed to the single shared capacity SQL.
				at := time.Date(2040, 1, 2, 3, 4, 5, 6000000, time.UTC)
				clock := mock.ExpectQuery("CurrentDBTime")
				switch mode {
				case "clock_error":
					clock.WillReturnError(errors.New("clock down"))
				case "clock_zero":
					clock.WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(time.Time{}))
				default:
					clock.WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(at))
					read := mock.ExpectQuery("SELECT COUNT.*effective").WithArgs("p", at, "p")
					if mode == "capacity_error" {
						read.WillReturnError(errors.New("capacity down"))
					} else {
						read.WillReturnRows(sqlmock.NewRows([]string{"effective", "controlled", "uncontrolled"}).AddRow(5, 3, 2))
					}
				}
			}
			value, err := s.Sample(context.Background())
			bad := mode == "outbox" || (capacity && mode != "capacity")
			if (err != nil) != bad {
				t.Fatalf("value=%+v err=%v", value, err)
			}
			if !bad && (value.Queued != 0 || value.OldestWaitSeconds != 0 || value.PendingOutbox != 0) {
				t.Fatalf("empty queue not zero: %+v", value)
			}
			if mode == "capacity" && (!value.HasCapacity || value.Effective != 5 || value.Controlled != 3 || value.Uncontrolled != 2) {
				t.Fatalf("%+v", value)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := (SQLSampler{}).Sample(context.Background()); err == nil {
		t.Fatal("nil DB accepted")
	}
}
func TestConfigValidationAndRegistrationErrors(t *testing.T) {
	if _, err := ParseEnv("API", func(string) string { return "" }); err == nil {
		t.Fatal("unknown prefix accepted")
	}
	for _, env := range []map[string]string{
		{"ADDR": "not-an-address"}, {"ADDR": "127.0.0.1:65536"}, {"ADDR": "192.0.2.1:9091"},
		{"PROBE_TIMEOUT": "3s", "REQUEST_TIMEOUT": "2s"}, {"SAMPLE_INTERVAL": "10m"}, {"STALE_AFTER": "31m"},
	} {
		if _, err := ParseEnv("WORKER", func(k string) string { return env[strings.TrimPrefix(k, "WORKER_MONITOR_")] }); err == nil {
			t.Fatalf("accepted %v", env)
		}
	}
	if _, err := New(configForTest(), "w", "p", nil, prometheus.NewRegistry(), goodDeps()); err == nil {
		t.Fatal("empty loops accepted")
	}
	if _, err := New(configForTest(), "w", "p", map[string]LoopBudget{"": {Interval: time.Second}}, prometheus.NewRegistry(), goodDeps()); err == nil {
		t.Fatal("blank loop accepted")
	}
	cfg := configForTest()
	cfg.ProbeTimeout = 0
	if _, err := New(cfg, "w", "p", map[string]LoopBudget{"loop": {Interval: time.Second}}, prometheus.NewRegistry(), goodDeps()); err == nil {
		t.Fatal("invalid config accepted")
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGauge(prometheus.GaugeOpts{Name: "studio_role_queue_depth", Help: "incompatible collector"}))
	if _, err := New(configForTest(), "w", "p", map[string]LoopBudget{"loop": {Interval: time.Second}}, reg, goodDeps()); err == nil {
		t.Fatal("conflicting collector ignored")
	}
}
