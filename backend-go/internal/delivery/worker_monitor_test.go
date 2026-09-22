package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	goredis "github.com/redis/go-redis/v9"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
	"github.com/shilin414/cas/backend-go/internal/platform/rolemonitor"
)

func TestDeliveryRecoveryProgress(t *testing.T) {
	for _, fail := range []string{"", "scan", "reclaim", "fail_stuck"} {
		t.Run(fail, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			scan := m.ExpectQuery("ListDueDeliveries")
			if fail == "scan" {
				scan.WillReturnError(errors.New("unavailable"))
			} else {
				scan.WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			reclaim := m.ExpectExec("ReclaimStuckDeliveries")
			if fail == "reclaim" {
				reclaim.WillReturnError(errors.New("unavailable"))
			} else {
				reclaim.WillReturnResult(sqlmock.NewResult(0, 0))
			}
			terminal := m.ExpectExec("FailStuckDeliveries")
			if fail == "fail_stuck" {
				terminal.WillReturnError(errors.New("unavailable"))
			} else {
				terminal.WillReturnResult(sqlmock.NewResult(0, 0))
			}
			seen := map[string]error{}
			w := NewWorker(conn, nil, "unit", nil, nil, nil, nil)
			w.ObserveLoop = func(name string, e error) { seen[name] = e }
			w.dueScanOnce(context.Background())
			w.reclaimOnce(context.Background())
			if len(seen) != 2 || (seen["delivery_scan"] != nil) != (fail == "scan") || (seen["delivery_reclaim"] != nil) != (fail == "reclaim" || fail == "fail_stuck") {
				t.Fatalf("%v", seen)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type monitoringEnqueueHook struct {
	*combinedACK
	failure  error
	enqueues int
}

func (h *monitoringEnqueueHook) ProcessHook(goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if cmd.Name() != "xadd" {
			h.t.Errorf("unexpected Redis command %s", cmd.Name())
			return errors.New("unexpected command")
		}
		if _, ok := ctx.Deadline(); !ok {
			h.t.Error("recovery enqueue has no timeout")
		}
		h.enqueues++
		if h.failure != nil {
			return h.failure
		}
		cmd.(*goredis.StringCmd).SetVal("1-0")
		return nil
	}
}
func TestDeliveryScanEnqueueFailureIsNotHealthy(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "enqueue_error"}[fail], func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			row := db.DeliveryExecution{ID: ids.New().Bytes(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
			m.ExpectQuery("ListDueDeliveries").WillReturnRows(previewRow(t, row))
			hook := &monitoringEnqueueHook{combinedACK: &combinedACK{t: t}}
			if fail {
				hook.failure = errors.New("Redis unavailable")
			}
			client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
			client.AddHook(hook)
			defer client.Close()
			rdb := redisx.NewWithPrefix("monitor-test")
			rdb.UniversalClient = client
			called := false
			var observed error
			w := NewWorker(conn, rdb, "unit", nil, nil, nil, nil)
			w.ObserveLoop = func(name string, e error) { called = true; observed = e }
			w.dueScanOnce(context.Background())
			if !called || hook.enqueues != 1 || (observed != nil) != fail {
				t.Fatalf("called=%v enqueues=%d err=%v", called, hook.enqueues, observed)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeliveryMonitoringBudgetUsesActualLoopConstants(t *testing.T) {
	w := &Worker{}
	loops := w.MonitoringLoopBudgets()
	if loops["delivery_scan"].Interval != scanEvery || loops["delivery_reclaim"].Interval != reclaimEvery {
		t.Fatal("monitor periods drifted from recovery loops")
	}
	if loops["delivery_scan"].OperationBudget != recoveryOperationTimeout || loops["delivery_reclaim"].OperationBudget != recoveryOperationTimeout {
		t.Fatal("monitor operation budget drifted")
	}
	c, err := rolemonitor.ParseEnv("WORKER", func(k string) string {
		switch k {
		case "WORKER_MONITOR_ADDR":
			return "127.0.0.1:9092"
		case "WORKER_MONITOR_SAMPLE_INTERVAL":
			return "5s"
		case "WORKER_MONITOR_STALE_AFTER":
			return "10s"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ValidateLoopBudgets(loops); err == nil {
		t.Fatal("healthy delivery would regularly expire under accepted config")
	}
	c.StaleAfter = 2*(reclaimEvery+recoveryOperationTimeout) + time.Second
	if err = c.ValidateLoopBudgets(loops); err != nil {
		t.Fatal(err)
	}
}
