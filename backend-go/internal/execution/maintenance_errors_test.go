package execution

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

func monitoringRow(t *testing.T, value any) *sqlmock.Rows {
	t.Helper()
	v := reflect.ValueOf(value)
	cols := make([]string, v.NumField())
	values := make([]driver.Value, v.NumField())
	for i := range cols {
		cols[i] = v.Type().Field(i).Name
		var err error
		values[i], err = driver.DefaultParameterConverter.ConvertValue(v.Field(i).Interface())
		if err != nil {
			t.Fatal(err)
		}
	}
	return sqlmock.NewRows(cols).AddRow(values...)
}
func expectMonitoringRecoverySuccess(t *testing.T, m sqlmock.Sqlmock, kind string, id ids.ID) {
	t.Helper()
	m.ExpectBegin()
	if kind == "lease" {
		token := ids.New()
		m.ExpectQuery("GetRunForUpdate").WithArgs(id.Bytes()).WillReturnError(sql.ErrNoRows)
		m.ExpectQuery("GetExpiredLeaseForUpdate").WithArgs(id.Bytes()).WillReturnRows(monitoringRow(t, db.GetExpiredLeaseForUpdateRow{ID: 1, RunID: id.Bytes(), LeaseToken: token.Bytes(), LeaseEpoch: 1, AcquiredAt: time.Now(), ExpiresAt: time.Now()}))
		m.ExpectExec("DeleteLeaseByToken").WithArgs(id.Bytes(), token.Bytes()).WillReturnResult(sqlmock.NewResult(0, 1))
	} else {
		m.ExpectQuery("GetRunForUpdate").WithArgs(id.Bytes()).WillReturnRows(monitoringRow(t, db.GetRunForUpdateRow{ID: id.Bytes(), Status: StatusWaitingExternal}))
		m.ExpectExec("FailParkedExternalRun").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectQuery("AllocRunEventSequence").WithArgs(id.Bytes()).WillReturnRows(sqlmock.NewRows([]string{"sequence"}).AddRow(1))
		m.ExpectExec("BumpRunEventSequence").WithArgs(id.Bytes()).WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectExec("AppendRunEvent").WillReturnResult(sqlmock.NewResult(1, 1))
	}
	m.ExpectCommit()
}
func TestMaintenancePartialFailurePreservesCountAndAllErrors(t *testing.T) {
	for _, kind := range []string{"lease", "parked"} {
		t.Run(kind, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			runIDs := []ids.ID{ids.New(), ids.New(), ids.New(), ids.New()}
			rows := sqlmock.NewRows([]string{"run_id"})
			for _, id := range runIDs {
				rows.AddRow(id.Bytes())
			}
			if kind == "lease" {
				m.ExpectQuery("ListExpiredLeaseRunIDs").WillReturnRows(rows)
			} else {
				m.ExpectQuery("CurrentDBTime").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(time.Now()))
				m.ExpectQuery("ListParkedWaitingExternalRunIDs").WillReturnRows(rows)
			}
			first, last := errors.New("first item failure"), errors.New("last item failure")
			expectMonitoringRecoverySuccess(t, m, kind, runIDs[0])
			m.ExpectBegin()
			m.ExpectQuery("GetRunForUpdate").WithArgs(runIDs[1].Bytes()).WillReturnError(first)
			m.ExpectRollback()
			expectMonitoringRecoverySuccess(t, m, kind, runIDs[2])
			m.ExpectBegin()
			m.ExpectQuery("GetRunForUpdate").WithArgs(runIDs[3].Bytes()).WillReturnError(last)
			m.ExpectRollback()
			s := NewService(conn, nil, nil, nil)
			var count int
			if kind == "lease" {
				count, err = s.RecoverExpiredLeases(context.Background(), 10)
			} else {
				count, err = s.ExpireParkedExternalRuns(context.Background(), 10)
			}
			if count != 2 || !errors.Is(err, first) || !errors.Is(err, last) {
				t.Fatalf("count=%d err=%v; want two commits and both errors", count, err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestPartialMaintenanceFailureNeverReportsHealthy(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	id := ids.New()
	failure := errors.New("per-item transaction failed")
	m.ExpectQuery("ListExpiredLeaseRunIDs").WillReturnRows(sqlmock.NewRows([]string{"run_id"}).AddRow(id.Bytes()))
	m.ExpectBegin()
	m.ExpectQuery("GetRunForUpdate").WillReturnError(failure)
	m.ExpectRollback()
	m.ExpectQuery("CurrentDBTime").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(time.Now()))
	m.ExpectQuery("ListParkedWaitingExternalRunIDs").WillReturnRows(sqlmock.NewRows([]string{"run_id"}))
	called := false
	var observed error
	w := &Worker{Svc: NewService(conn, nil, nil, nil), Log: slog.Default(), ObserveLoop: func(name string, e error) { called = true; observed = e }}
	w.maintenanceOnce(context.Background())
	if !called || !errors.Is(observed, failure) {
		t.Fatalf("partial failure counted healthy: %v", observed)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
