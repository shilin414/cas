package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/automation/schedule"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
)

func TestScheduleProgressIsNotATimerHeartbeat(t *testing.T) {
	for _, fail := range []string{"", "clock", "pending", "due"} {
		t.Run(fail, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			now := time.Now()
			clock := m.ExpectQuery("DBNow")
			if fail == "clock" {
				clock.WillReturnError(errors.New("clock unavailable"))
			} else {
				clock.WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
				pending := m.ExpectQuery("ListAdmissiblePendingOccurrences")
				if fail == "pending" {
					pending.WillReturnError(errors.New("pending unavailable"))
				} else {
					pending.WillReturnRows(sqlmock.NewRows([]string{"id"}))
				}
				due := m.ExpectQuery("ListDueSchedules")
				if fail == "due" {
					due.WillReturnError(errors.New("due unavailable"))
				} else {
					due.WillReturnRows(sqlmock.NewRows([]string{"id"}))
				}
			}
			s := New(conn, nil, nil, nil, nil)
			called := false
			var observed error
			s.ObserveLoop = func(name string, e error) {
				if name != "schedule" {
					t.Error(name)
				}
				called = true
				observed = e
			}
			s.ProcessDue(context.Background())
			if !called || (observed != nil) != (fail != "") {
				t.Fatalf("called=%v err=%v", called, observed)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSchedulerPerItemFailureIsPropagatedWithoutSkippingNextScan(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	now := time.Now()
	first, last := errors.New("pending transaction unavailable"), errors.New("due transaction unavailable")
	m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
	m.ExpectQuery("ListAdmissiblePendingOccurrences").WillReturnRows(mockRow(t, db.ScheduleOccurrence{ID: 7, ScheduleID: 1, CreatedAt: now, UpdatedAt: now, ScheduledAt: now}))
	m.ExpectBegin().WillReturnError(first)
	m.ExpectQuery("ListDueSchedules").WillReturnRows(mockRow(t, db.Schedule{ID: 2, NextRunAt: sql.NullTime{Time: now, Valid: true}, CreatedAt: now, UpdatedAt: now}))
	m.ExpectBegin().WillReturnError(last)
	s := New(conn, nil, nil, nil, nil)
	var observed error
	s.ObserveLoop = func(_ string, e error) { observed = e }
	s.ProcessDue(context.Background())
	if !errors.Is(observed, first) || !errors.Is(observed, last) {
		t.Fatalf("lost item errors: %v", observed)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestDeletedPendingOccurrenceDoesNotHideWriteOrCommitFailure(t *testing.T) {
	for _, stage := range []string{"write", "commit"} {
		t.Run(stage, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			failure := errors.New("persistence failed")
			m.ExpectBegin()
			m.ExpectQuery("GetScheduleRowForUpdate").WillReturnError(sql.ErrNoRows)
			write := m.ExpectExec("MarkOccurrenceStatus")
			if stage == "write" {
				write.WillReturnError(failure)
			} else {
				write.WillReturnResult(sqlmock.NewResult(0, 1))
			}
			commit := m.ExpectCommit()
			if stage == "commit" {
				commit.WillReturnError(failure)
			}
			s := New(conn, nil, nil, nil, nil)
			err = s.admitOne(context.Background(), db.ScheduleOccurrence{ID: 7, ScheduleID: 1}, time.Now())
			if !errors.Is(err, failure) {
				t.Fatalf("error swallowed: %v", err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDeadlineSkipDoesNotHideOccurrenceReadOrWriteFailure(t *testing.T) {
	for _, stage := range []string{"read", "write"} {
		t.Run(stage, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			failure := errors.New("occurrence persistence failed")
			now := time.Now().UTC().Truncate(time.Millisecond)
			slot := now.Add(-time.Minute)
			row := db.Schedule{ID: 1, OwnerUserID: 42, Enabled: true, ScheduleType: schedule.TypeOnce, Timezone: "UTC", RunAt: sql.NullTime{Time: slot, Valid: true}, NextRunAt: sql.NullTime{Time: slot, Valid: true}, ExecutionWindowSeconds: 10, DeadlinePolicy: schedule.DeadlineSkip, CreatedAt: now, UpdatedAt: now}
			m.ExpectBegin()
			m.ExpectQuery("GetScheduleRowForUpdate").WillReturnRows(mockRow(t, row))
			m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
			m.ExpectQuery("HasActiveOccurrence").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
			m.ExpectExec("CreateScheduleOccurrence").WillReturnResult(sqlmock.NewResult(7, 1))
			read := m.ExpectQuery("GetScheduleOccurrenceBySlot")
			if stage == "read" {
				read.WillReturnError(failure)
			} else {
				read.WillReturnRows(mockRow(t, db.ScheduleOccurrence{ID: 7, ScheduleID: 1, CreatedAt: now, UpdatedAt: now, ScheduledAt: slot}))
				m.ExpectExec("CaptureOccurrenceDeliveryExpectations").WillReturnResult(sqlmock.NewResult(0, 0))
				m.ExpectExec("MarkOccurrenceDeliverySnapshotCaptured").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("MarkOccurrenceStatus").WillReturnError(failure)
			}
			// Preserve existing advance/commit behavior; only report the previously lost error.
			m.ExpectQuery("GetScheduleByID").WillReturnRows(mockRow(t, row))
			m.ExpectQuery("CountPendingOccurrences").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
			m.ExpectExec("SetScheduleEnabled").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("TouchScheduleRunTimes").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
			s := New(conn, nil, nil, nil, nil)
			err = s.triggerSchedule(context.Background(), schedule.FromDBRow(row), slot, now)
			if !errors.Is(err, failure) {
				t.Fatalf("error swallowed: %v", err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSchedulerMonitoringIntervalMatchesRuntimeDefault(t *testing.T) {
	s := &Scheduler{}
	if s.MonitoringLoopBudgets()["schedule"].Interval != time.Second {
		t.Fatal("default period drifted")
	}
	s.Interval = 30 * time.Second
	if s.MonitoringLoopBudgets()["schedule"].Interval != s.Interval {
		t.Fatal("configured period drifted")
	}
}
