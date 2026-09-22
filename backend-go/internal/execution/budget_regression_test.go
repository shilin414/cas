package execution

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestManagedProviderCannotFallBackWhenPolicyUnavailable(t *testing.T) {
	for _, name := range []string{"error", "missing", "zero"} {
		t.Run(name, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			m.ExpectExec("EnsureProviderAdmissionLock").WithArgs("managed").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectBegin()
			m.ExpectExec("LockProviderAdmission").WithArgs("managed").WillReturnResult(sqlmock.NewResult(0, 1))
			read := m.ExpectQuery("GetProviderCapacityForUpdate").WithArgs("managed")
			switch name {
			case "error":
				read.WillReturnError(errors.New("database unavailable"))
			case "missing":
				read.WillReturnError(sql.ErrNoRows)
			default:
				read.WillReturnRows(sqlmock.NewRows([]string{"max_inflight"}).AddRow(0))
			}
			m.ExpectRollback()
			slots := NewProviderSlots(conn, "managed", 100, time.Minute)
			slots.UseCatalogPolicy = true
			if _, ok, _, err := slots.Acquire(context.Background(), ownershipFixture(1)); err == nil || ok {
				t.Fatalf("policy failure admitted run: ok=%v err=%v", ok, err)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	slots := NewProviderSlots(nil, "managed", 0, time.Minute)
	slots.UseCatalogPolicy = true
	if _, ok, _, err := slots.Acquire(context.Background(), ownershipFixture(1)); ok || err == nil {
		t.Fatal("missing DB disabled managed capacity protection")
	}
}
func TestMaintenanceRunsWhileEveryExecutionPermitIsOccupied(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	m.ExpectQuery("ListExpiredLeaseRunIDs").WithArgs(100).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	m.ExpectQuery("CurrentDBTime").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(time.Now()))
	m.ExpectQuery("ListParkedWaitingExternalRunIDs").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	w := &Worker{Concurrency: 1, Lease: time.Second, Log: slog.Default(), Svc: NewService(conn, nil, slog.Default(), nil)}
	if !w.acquireExecution(context.Background()) {
		t.Fatal("failed to occupy execution pool")
	}
	defer w.releaseExecution()
	w.maintenanceOnce(context.Background())
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestHeartbeatHasIndependentShortTimeout(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	own := ownershipFixture(1)
	w := &Worker{Lease: 100 * time.Millisecond, Log: slog.Default(), Svc: NewService(conn, nil, slog.Default(), nil)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.trackInflight(own, cancel, time.Now())
	defer w.trackInflight(own, nil, time.Time{})
	w.mu.Lock()
	ctl := w.inflight[own.RunID]
	w.mu.Unlock()
	m.ExpectExec("HeartbeatLeaseFenced").WillDelayFor(time.Second).WillReturnResult(sqlmock.NewResult(0, 1))
	start := time.Now()
	w.heartbeatOne(context.Background(), own.RunID, inflightSnapshot{ctl: ctl, leaseRenewedAt: start})
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("heartbeat blocked %s", elapsed)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("failed heartbeat did not trigger independent watchdog")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedProviderReadsNewLimitOnEveryAcquire(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	slots := NewProviderSlots(conn, "managed", 100, time.Minute)
	slots.UseCatalogPolicy = true
	own := ownershipFixture(1)
	for _, limit := range []int{20, 2} {
		m.ExpectExec("EnsureProviderAdmissionLock").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectBegin()
		m.ExpectExec("LockProviderAdmission").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectQuery("GetProviderCapacityForUpdate").WithArgs("managed").WillReturnRows(sqlmock.NewRows([]string{"max_inflight"}).AddRow(limit))
		m.ExpectQuery("GetRunForUpdate").WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "external_run_id", "status", "lease_epoch", "attempt", "max_attempts", "trigger_type", "trigger_id", "conversation_id", "started_at", "finished_at"}).AddRow(own.RunID.Bytes(), 42, "", "running", own.LeaseEpoch, 0, 3, "interactive", nil, nil, nil, nil))
		m.ExpectQuery("GetActiveLeaseForUpdate").WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "worker_id", "lease_token", "lease_epoch", "acquired_at", "heartbeat_at", "expires_at"}).AddRow(1, own.RunID.Bytes(), own.WorkerID, own.LeaseToken.Bytes(), own.LeaseEpoch, time.Now(), nil, time.Now().Add(time.Minute)))
		m.ExpectExec("DeleteExpiredProviderSlots").WillReturnResult(sqlmock.NewResult(0, 0))
		m.ExpectQuery("GetProviderSlotForUpdate").WillReturnError(sql.ErrNoRows)
		m.ExpectQuery("CountProviderEffectiveInflightExcludingRun").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(3))
		if limit > 3 {
			m.ExpectExec("CreateProviderSlot").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
		} else {
			m.ExpectRollback()
		}
		_, ok, _, err := slots.Acquire(context.Background(), own)
		if err != nil || ok != (limit > 3) {
			t.Fatalf("catalog limit=%d admitted=%v err=%v (bootstrap=100)", limit, ok, err)
		}
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
