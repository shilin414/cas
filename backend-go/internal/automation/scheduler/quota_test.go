package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/automation/schedule"
	"github.com/shilin414/cas/backend-go/internal/execution"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
)

func TestScheduledQuotaStopsBeforeCreatingConversation(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	m.ExpectBegin()
	tx, err := execution.BeginUserAdmissionTx(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	m.ExpectQuery("LockUserRow").WithArgs(uint64(42)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	m.ExpectQuery("CountOutstandingRunsByUser").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(1))
	s := New(conn, nil, nil, nil, nil)
	s.MaxOutstanding = 1
	_, err = s.createRunForOccurrenceTx(context.Background(), tx, &schedule.Schedule{OwnerUserID: 42}, 1, &BindingView{}, time.Now())
	if !errors.Is(err, execution.ErrUserOutstandingExceeded) {
		t.Fatalf("err=%v", err)
	}
	m.ExpectRollback()
	_ = tx.Rollback()
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestQuotaDeferredOnceScheduleIsNotDisabled(t *testing.T) {
	for _, misfire := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "misfire"}[misfire], func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			now := time.Now().UTC().Truncate(time.Millisecond)
			row := db.Schedule{ID: 1, OwnerUserID: 42, Enabled: true, ScheduleType: schedule.TypeOnce, Timezone: "UTC", RunAt: sql.NullTime{Time: now, Valid: true}}
			m.ExpectQuery("GetScheduleByID").WillReturnRows(mockRow(t, row))
			// No SetScheduleEnabled(false): otherwise admitOne would skip the pending occurrence.
			m.ExpectExec("TouchScheduleRunTimes").WithArgs(now, nil, uint64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
			s := New(conn, nil, nil, nil, nil)
			if misfire {
				_, err = s.advancePastWithPending(context.Background(), db.New(conn), 1, now, now.Add(time.Hour), true)
			} else {
				err = s.advanceWithPending(context.Background(), db.New(conn), 1, now, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAdvanceRetainsOtherPendingOccurrence(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	now := time.Now().UTC().Truncate(time.Millisecond)
	row := db.Schedule{ID: 1, OwnerUserID: 42, Enabled: true, ScheduleType: schedule.TypeOnce, Timezone: "UTC", RunAt: sql.NullTime{Time: now, Valid: true}}
	m.ExpectQuery("GetScheduleByID").WillReturnRows(mockRow(t, row))
	m.ExpectQuery("CountPendingOccurrences").WithArgs(uint64(1)).WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(1))
	m.ExpectExec("TouchScheduleRunTimes").WithArgs(now, nil, uint64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := New(conn, nil, nil, nil, nil).advance(context.Background(), db.New(conn), 1, now); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type unavailableQuotaBinding struct{}

func (unavailableQuotaBinding) EnabledBinding(context.Context, int64) (*BindingView, error) {
	return nil, nil
}

func TestPendingTerminalPathsRetireExhaustedSchedule(t *testing.T) {
	for _, mode := range []string{"deadline", "window", "binding"} {
		t.Run(mode, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			now := time.Now().UTC().Truncate(time.Millisecond)
			row := windowSchedule(t, now.Add(-time.Minute))
			row.NextRunAt = sql.NullTime{}
			row.ExecutionWindowSeconds = 60
			row.DeadlinePolicy = schedule.DeadlineSkip
			if mode != "window" {
				row.TriggerConfig = []byte(`{"time":"09:00"}`)
			}
			occ := db.ScheduleOccurrence{ID: 9, ScheduleID: row.ID, Status: schedule.OccPending, ScheduledAt: now.Add(-2 * time.Minute), CreatedAt: now, UpdatedAt: now}
			if mode == "binding" {
				occ.ScheduledAt = now
			}
			m.ExpectBegin()
			m.ExpectQuery("GetScheduleRowForUpdate").WillReturnRows(mockRow(t, row))
			m.ExpectQuery("GetScheduleOccurrenceByID").WillReturnRows(mockRow(t, occ))
			m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
			status := schedule.OccSkipped
			if mode == "binding" {
				status = schedule.OccFailed
				m.ExpectQuery("CountActiveOccurrencesExcluding").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
			}
			m.ExpectExec("MarkOccurrenceStatus").WithArgs(status, occ.ID).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectQuery("CountPendingOccurrences").WithArgs(row.ID).WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
			m.ExpectExec("SetScheduleEnabled").WithArgs(false, row.ID).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
			err = New(conn, nil, unavailableQuotaBinding{}, nil, nil).admitOne(context.Background(), occ, now)
			if mode == "binding" {
				if !errors.Is(err, ErrNotSchedulable) {
					t.Fatalf("err=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
