package rolemonitor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
)

func configForTest() Config {
	return Config{ProbeTimeout: 30 * time.Millisecond, SampleTimeout: 30 * time.Millisecond, RequestTimeout: 100 * time.Millisecond, SampleInterval: 5 * time.Second, StaleAfter: time.Minute}
}
func TestParseEnv(t *testing.T) {
	c, err := ParseEnv("WORKER", func(string) string { return "" })
	if err != nil || c.Address != "" {
		t.Fatalf("default must disable listener: %+v %v", c, err)
	}
	for _, tt := range []struct {
		name  string
		env   map[string]string
		valid bool
	}{
		{"loopback", map[string]string{"WORKER_MONITOR_ADDR": "127.0.0.1:9091"}, true},
		{"ipv6", map[string]string{"WORKER_MONITOR_ADDR": "[::1]:9091"}, true},
		{"wildcard forbidden", map[string]string{"WORKER_MONITOR_ADDR": ":9091"}, false},
		{"remote explicit", map[string]string{"WORKER_MONITOR_ADDR": "0.0.0.0:9091", "WORKER_MONITOR_ALLOW_REMOTE": "true"}, true},
		{"hostname forbidden", map[string]string{"WORKER_MONITOR_ADDR": "example.org:9091", "WORKER_MONITOR_ALLOW_REMOTE": "true"}, false},
		{"bad port", map[string]string{"WORKER_MONITOR_ADDR": "127.0.0.1:0"}, false},
		{"bad duration", map[string]string{"WORKER_MONITOR_PROBE_TIMEOUT": "forever"}, false},
		{"negative", map[string]string{"WORKER_MONITOR_SAMPLE_INTERVAL": "-1s"}, false},
		{"long timeout", map[string]string{"WORKER_MONITOR_SAMPLE_TIMEOUT": "1h"}, false},
		{"stale too soon", map[string]string{"WORKER_MONITOR_STALE_AFTER": "1s"}, false},
		{"bad bool", map[string]string{"WORKER_MONITOR_ALLOW_REMOTE": "yes"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEnv("WORKER", func(k string) string { return tt.env[k] })
			if (err == nil) != tt.valid {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
func fixture(t *testing.T, d Dependencies) *Monitor {
	t.Helper()
	m, err := New(configForTest(), "worker", "test", map[string]LoopBudget{"maintenance": {Interval: time.Second}}, prometheus.NewRegistry(), d)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func request(m *Monitor, path string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	m.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
	return r
}
func goodDeps() Dependencies {
	return Dependencies{DB: func(context.Context) error { return nil }, Redis: func(context.Context) error { return nil }, Sample: func(context.Context) (Snapshot, error) {
		return Snapshot{Queued: 7, OldestWaitSeconds: 12, PendingOutbox: 3}, nil
	}}
}
func TestReadinessRequiresRealProgressAndDependencies(t *testing.T) {
	m := fixture(t, goodDeps())
	if request(m, "/health/live").Code != 200 || request(m, "/health/ready").Code != 503 {
		t.Fatal("startup must be live but not ready")
	}
	m.SampleNow(context.Background())
	m.ObserveLoop("maintenance", nil)
	if r := request(m, "/health/ready"); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	m.ObserveLoop("maintenance", errors.New("password=secret"))
	r := request(m, "/health/ready")
	if r.Code != 503 || strings.Contains(r.Body.String(), "secret") {
		t.Fatal(r.Body.String())
	}
	m.ObserveLoop("maintenance", nil)
	m.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("stalled loop reported ready")
	}
	for _, dependency := range []string{"db", "redis"} {
		t.Run(dependency, func(t *testing.T) {
			deps := goodDeps()
			fail := func(context.Context) error { return errors.New("secret") }
			if dependency == "db" {
				deps.DB = fail
			} else {
				deps.Redis = fail
			}
			mon := fixture(t, deps)
			mon.SampleNow(context.Background())
			mon.ObserveLoop("maintenance", nil)
			r := request(mon, "/health/ready")
			if r.Code != 503 || strings.Contains(r.Body.String(), "secret") {
				t.Fatal(r.Body.String())
			}
		})
	}
}
func TestProbeDeadlineAndNoSampleOnRequests(t *testing.T) {
	d := goodDeps()
	calls := 0
	d.Sample = func(context.Context) (Snapshot, error) { calls++; return Snapshot{}, nil }
	d.DB = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	m := fixture(t, d)
	m.SampleNow(context.Background())
	m.ObserveLoop("maintenance", nil)
	start := time.Now()
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("blocked DB reported ready")
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Fatal("probe unbounded")
	}
	request(m, "/metrics")
	request(m, "/health/live")
	if calls != 1 {
		t.Fatal("HTTP request triggered sampling")
	}
}
func TestMetricsKeepLastGoodValueAndExposeFailure(t *testing.T) {
	d := goodDeps()
	fail := false
	d.Sample = func(ctx context.Context) (Snapshot, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing sampling deadline")
		}
		if fail {
			return Snapshot{}, errors.New("credentials")
		}
		return Snapshot{Queued: 7, HasCapacity: true, Controlled: 2, Uncontrolled: 1, Effective: 3}, nil
	}
	m := fixture(t, d)
	if strings.Contains(request(m, "/metrics").Body.String(), "studio_role_queue_depth{") {
		t.Fatal("invented initial zero")
	}
	m.SampleNow(context.Background())
	fail = true
	m.SampleNow(context.Background())
	body := request(m, "/metrics").Body.String()
	for _, want := range []string{"studio_role_queue_depth{provider=\"test\",role=\"worker\"} 7", "studio_role_sample_success{provider=\"test\",role=\"worker\"} 0", "studio_role_provider_capacity{kind=\"uncontrolled\",provider=\"test\",role=\"worker\"} 1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	if strings.Contains(body, "credentials") {
		t.Fatal("leaked error")
	}
	m.ObserveLoop("maintenance", nil)
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("failed sampling reported ready")
	}
}
func TestDuplicateRegistrationAndUnknownLoop(t *testing.T) {
	reg := prometheus.NewRegistry()
	cfg := configForTest()
	d := goodDeps()
	m, err := New(cfg, "worker", "p", map[string]LoopBudget{"maintenance": {Interval: time.Second}}, reg, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = New(cfg, "worker", "p", map[string]LoopBudget{"maintenance": {Interval: time.Second}}, reg, d); err == nil {
		t.Fatal("duplicate monitor must return error, not panic or bind different hooks")
	}
	m.SampleNow(context.Background())
	m.ObserveLoop("typo", nil)
	if request(m, "/health/ready").Code != 503 {
		t.Fatal("unknown loop satisfied readiness")
	}
}
func TestSQLSamplerActiveRowsAndErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sampler := SQLSampler{DB: db, Provider: "aily"}
	mock.ExpectQuery("SELECT COUNT.*FROM runs.*status = 'queued'.*provider = \\?").WithArgs("aily").WillReturnRows(sqlmock.NewRows([]string{"depth", "age"}).AddRow(2, 14))
	mock.ExpectQuery("SELECT COUNT.*FROM outbox_events.*status = 'pending'").WillReturnRows(sqlmock.NewRows([]string{"depth"}).AddRow(4))
	s, err := sampler.Sample(context.Background())
	if err != nil || s.Queued != 2 || s.OldestWaitSeconds != 14 || s.PendingOutbox != 4 {
		t.Fatalf("%+v %v", s, err)
	}
	mock.ExpectQuery("SELECT COUNT").WillReturnError(errors.New("unavailable"))
	if _, err = sampler.Sample(context.Background()); err == nil {
		t.Fatal("DB error must not become zero")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
