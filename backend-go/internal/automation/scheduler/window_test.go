package scheduler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/automation/schedule"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
)

func mockRow(t *testing.T, v any) *sqlmock.Rows {
	t.Helper()
	rv := reflect.ValueOf(v)
	cols := make([]string, rv.NumField())
	vals := make([]driver.Value, rv.NumField())
	for i := range cols {
		cols[i] = rv.Type().Field(i).Name
		var err error
		vals[i], err = driver.DefaultParameterConverter.ConvertValue(rv.Field(i).Interface())
		if err != nil {
			t.Fatal(err)
		}
	}
	return sqlmock.NewRows(cols).AddRow(vals...)
}
func windowSchedule(t *testing.T, now time.Time) db.Schedule {
	t.Helper()
	start, end := now.Add(-time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)
	cfg, _ := json.Marshal(schedule.TriggerConfig{Time: "09:00", StartsAt: &start, EndsAt: &end})
	return db.Schedule{ID: 1, OwnerUserID: 42, Enabled: true, ScheduleType: schedule.TypeDaily, Timezone: "UTC", TriggerConfig: cfg, OverlapPolicy: schedule.OverlapQueue, NextRunAt: sql.NullTime{Time: now.Add(-time.Minute), Valid: true}}
}
func TestRunNowEffectiveWindowAndInclusiveBoundaries(t *testing.T) {
	for _, offset := range []time.Duration{-time.Hour - time.Millisecond, -time.Hour, 0, time.Millisecond} {
		t.Run(offset.String(), func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			end := time.Date(2030, 1, 2, 9, 0, 0, 0, time.UTC)
			now := end.Add(offset)
			row := windowSchedule(t, end)
			m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
			m.ExpectBegin()
			m.ExpectQuery("GetScheduleRowForUpdate").WithArgs(uint64(1)).WillReturnRows(mockRow(t, row))
			m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
			outside := offset < -time.Hour || offset > 0
			sentinel := errors.New("passed window gate")
			if !outside {
				m.ExpectQuery("HasActiveOccurrence").WillReturnError(sentinel)
			}
			m.ExpectRollback()
			_, err = New(conn, nil, nil, nil, nil).TriggerNow(context.Background(), 1, 42, false)
			var validation *schedule.ValidationError
			if outside && !errors.As(err, &validation) {
				t.Fatalf("outside window err=%v", err)
			}
			if !outside && !errors.Is(err, sentinel) {
				t.Fatalf("boundary rejected: %v", err)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestExpiredMisfireSkipsAndClearsNextRun(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	end := time.Date(2030, 1, 2, 9, 0, 0, 0, time.UTC)
	now := end.Add(time.Hour)
	row := windowSchedule(t, end)
	slot := row.NextRunAt.Time
	occ := db.ScheduleOccurrence{ID: 9, ScheduleID: 1, ScheduledAt: slot, Status: schedule.OccPending, CreatedAt: slot, UpdatedAt: slot}
	m.ExpectBegin()
	m.ExpectQuery("GetScheduleRowForUpdate").WillReturnRows(mockRow(t, row))
	m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
	m.ExpectExec("CreateScheduleOccurrence").WillReturnResult(sqlmock.NewResult(9, 1))
	m.ExpectQuery("GetScheduleOccurrenceBySlot").WillReturnRows(mockRow(t, occ))
	m.ExpectExec("CaptureOccurrenceDeliveryExpectations").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec("MarkOccurrenceDeliverySnapshotCaptured").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("MarkOccurrenceStatus").WithArgs(schedule.OccSkipped, uint64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("GetScheduleByID").WillReturnRows(mockRow(t, row))
	// The slot at end itself is also missed; it must not be replayed after expiry.
	m.ExpectExec("SetScheduleEnabled").WithArgs(false, uint64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("TouchScheduleRunTimes").WithArgs(slot, nil, uint64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := New(conn, nil, nil, nil, nil).triggerSchedule(context.Background(), schedule.FromDBRow(row), slot, now); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestPendingAdmissionRejectsExpiredWindowBeforeOverlap(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	end := time.Date(2030, 1, 2, 9, 0, 0, 0, time.UTC)
	now := end.Add(time.Hour)
	row := windowSchedule(t, end)
	occ := db.ScheduleOccurrence{ID: 9, ScheduleID: 1, ScheduledAt: end.Add(-time.Minute), Status: schedule.OccPending, CreatedAt: end, UpdatedAt: end}
	m.ExpectBegin()
	m.ExpectQuery("GetScheduleRowForUpdate").WillReturnRows(mockRow(t, row))
	m.ExpectQuery("GetScheduleOccurrenceByID").WillReturnRows(mockRow(t, occ))
	m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
	m.ExpectExec("MarkOccurrenceStatus").WithArgs(schedule.OccSkipped, uint64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := New(conn, nil, nil, nil, nil).admitOne(context.Background(), occ, now); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
