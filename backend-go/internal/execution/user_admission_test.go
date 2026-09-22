package execution

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestUserAdmissionUsesCurrentReadInCallerTransaction(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		t.Run(string(rune('0'+n)), func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			m.ExpectBegin()
			tx, err := BeginUserAdmissionTx(context.Background(), conn)
			if err != nil {
				t.Fatal(err)
			}
			m.ExpectQuery("LockUserRow").WithArgs(uint64(42)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
			m.ExpectQuery("CountOutstandingRunsByUser").WithArgs(sql.NullInt64{Int64: 42, Valid: true}).WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(n))
			err = CheckUserAdmissionInTx(context.Background(), tx, 42, 2)
			if (n >= 2) != errors.Is(err, ErrUserOutstandingExceeded) {
				t.Fatalf("n=%d err=%v", n, err)
			}
			m.ExpectRollback()
			_ = tx.Rollback()
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUserAdmissionFailureAndDisabledBudget(t *testing.T) {
	if err := CheckUserAdmissionInTx(context.Background(), nil, 42, 0); err != nil {
		t.Fatal(err)
	}
	if err := CheckUserAdmissionInTx(context.Background(), nil, 0, 20); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"lock", "count"} {
		t.Run(stage, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			m.ExpectBegin()
			tx, err := BeginUserAdmissionTx(context.Background(), conn)
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("temporary database failure")
			lock := m.ExpectQuery("LockUserRow")
			if stage == "lock" {
				lock.WillReturnError(failure)
			} else {
				lock.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
				m.ExpectQuery("CountOutstandingRunsByUser").WillReturnError(failure)
			}
			if err := CheckUserAdmissionInTx(context.Background(), tx, 42, 20); !errors.Is(err, failure) {
				t.Fatalf("lost infrastructure error: %v", err)
			}
			m.ExpectRollback()
			_ = tx.Rollback()
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
