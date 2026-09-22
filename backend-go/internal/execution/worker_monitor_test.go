package execution

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMaintenanceProgressReflectsActualScanResults(t *testing.T) {
	for _, fail := range []string{"", "lease", "parked", "slots"} {
		t.Run(fail, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			first := m.ExpectQuery("ListExpiredLeaseRunIDs")
			if fail == "lease" {
				first.WillReturnError(errors.New("db unavailable"))
			} else {
				first.WillReturnRows(sqlmock.NewRows([]string{"run_id"}))
			}
			clock := m.ExpectQuery("CurrentDBTime")
			if fail == "parked" {
				clock.WillReturnError(errors.New("clock unavailable"))
			} else {
				clock.WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(time.Now()))
				m.ExpectQuery("ListParkedWaitingExternalRunIDs").WillReturnRows(sqlmock.NewRows([]string{"run_id"}))
			}
			slots := m.ExpectExec("DeleteExpiredProviderSlotsAll")
			if fail == "slots" {
				slots.WillReturnError(errors.New("cleanup unavailable"))
			} else {
				slots.WillReturnResult(sqlmock.NewResult(0, 0))
			}
			var called bool
			var observed error
			w := &Worker{Svc: NewService(conn, nil, nil, nil), Log: slog.Default(), ProviderSlots: &ProviderSlots{DB: conn}, ObserveLoop: func(name string, e error) {
				if name != "maintenance" {
					t.Error(name)
				}
				called = true
				observed = e
			}}
			w.maintenanceOnce(context.Background())
			if !called || (observed != nil) != (fail != "") {
				t.Fatalf("called=%v err=%v", called, observed)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestFallbackProgressIncludesEmptyScans(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "error"}[fail], func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			q := m.ExpectQuery("ListQueuedRunIDs")
			if fail {
				q.WillReturnError(errors.New("db down"))
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var observed error
			called := false
			w := &Worker{Svc: NewService(conn, nil, nil, nil), Log: slog.Default(), ScanEvery: time.Millisecond, ObserveLoop: func(name string, e error) { called = true; observed = e; cancel() }}
			w.scanLoop(ctx)
			if !called || (observed != nil) != fail {
				t.Fatalf("called=%v err=%v", called, observed)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExecutionMonitoringBudgetTracksActualMaintenanceWork(t *testing.T) {
	w := &Worker{ScanEvery: 20 * time.Second, Lease: 8 * time.Second}
	loops := w.MonitoringLoopBudgets()
	if loops["fallback_scan"].Interval != w.ScanEvery || loops["fallback_scan"].OperationBudget != w.operationTimeout() {
		t.Fatal("fallback monitor budget drifted")
	}
	if loops["maintenance"].Interval != w.ScanEvery || loops["maintenance"].OperationBudget != 3*w.operationTimeout() {
		t.Fatal("maintenance sequential operation budget lost")
	}
}
