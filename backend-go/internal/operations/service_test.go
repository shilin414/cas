package operations

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"time"
)

func TestFilterAndCapacityValidation(t *testing.T) {
	for _, f := range []RunFilter{{Status: "injected"}, {Provider: "a' OR 1=1"}, {OwnerUserID: "900719925474099312345"}, {OwnerUserID: "-1"}, {ApplicationID: "1.1"}, {Limit: 101}, {Limit: -1}, {Cursor: "not-a-cursor"}} {
		if _, err := normalizeFilter(f); err == nil {
			t.Fatalf("accepted %+v", f)
		}
	}
	f, err := normalizeFilter(RunFilter{OwnerUserID: "9007199254740993"})
	if err != nil || f.OwnerUserID != "9007199254740993" || f.Status != "active" || f.Limit != 50 {
		t.Fatalf("filter=%+v err=%v", f, err)
	}
	for _, in := range []CapacityInput{{MaxInflight: 0, ExpectedMaxInflight: 10, Reason: "x"}, {MaxInflight: 10001, ExpectedMaxInflight: 10, Reason: "x"}, {MaxInflight: 10, ExpectedMaxInflight: -1, Reason: "x"}, {MaxInflight: 10, ExpectedMaxInflight: 10, Reason: " "}} {
		if err := in.Validate(); err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
}
func TestCursorIsBoundToFiltersAndPreservesPrecision(t *testing.T) {
	f, _ := normalizeFilter(RunFilter{Status: "queued", OwnerUserID: "9007199254740993"})
	at := time.Date(2026, 9, 22, 1, 2, 3, 456000000, time.UTC)
	token := encodeCursor(at, "0199709a-dc00-7000-8000-000000000001", f)
	got, err := decodeCursor(token, f)
	if err != nil || !got.CreatedAt.Equal(at) {
		t.Fatalf("cursor=%+v err=%v", got, err)
	}
	other := f
	other.Status = "running"
	if _, err := decodeCursor(token, other); err == nil {
		t.Fatal("accepted cursor from another filter")
	}
}
func expectCapacityLock(m sqlmock.Sqlmock, old int) {
	m.ExpectExec("EnsureProviderAdmissionLock").WithArgs("feishu_aily").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectBegin()
	m.ExpectExec("LockProviderAdmission").WithArgs("feishu_aily").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("GetProviderCapacityForUpdate").WithArgs("feishu_aily").WillReturnRows(sqlmock.NewRows([]string{"max_inflight"}).AddRow(old))
}
func TestCapacityUpdateIsAtomicWithAuditAndRejectsLostUpdates(t *testing.T) {
	for _, mode := range []string{"success", "conflict", "audit_failure"} {
		t.Run(mode, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			expectCapacityLock(m, 20)
			in := CapacityInput{MaxInflight: 10, ExpectedMaxInflight: 20, Reason: "reduce peak load"}
			if mode == "conflict" {
				in.ExpectedMaxInflight = 30
				m.ExpectRollback()
			} else {
				m.ExpectExec("UPDATE providers SET max_inflight").WithArgs(10, "feishu_aily").WillReturnResult(sqlmock.NewResult(0, 1))
				audit := m.ExpectExec("INSERT INTO audit_logs").WithArgs(int64(7), "provider.capacity.update", "provider", "feishu_aily", sqlmock.AnyArg())
				if mode == "audit_failure" {
					audit.WillReturnError(errors.New("audit unavailable"))
					m.ExpectRollback()
				} else {
					audit.WillReturnResult(sqlmock.NewResult(1, 1))
					m.ExpectQuery("CurrentDBTime").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(time.Now()))
					m.ExpectCommit()
				}
			}
			out, err := NewService(conn).UpdateCapacity(context.Background(), 7, "feishu_aily", in)
			if mode == "success" {
				if err != nil || out.PreviousMaxInflight != 20 || out.MaxInflight != 10 || out.EffectiveFor != "new_admissions" {
					t.Fatalf("out=%+v err=%v", out, err)
				}
			} else if mode == "conflict" {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("err=%v", err)
				}
			} else if err == nil {
				t.Fatal("committed without audit")
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCapacityMissingProviderAndInvalidActorFailClosed(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := NewService(conn)
	in := CapacityInput{MaxInflight: 10, ExpectedMaxInflight: 20, Reason: "test"}
	if _, err := s.UpdateCapacity(context.Background(), 0, "feishu_aily", in); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	m.ExpectExec("EnsureProviderAdmissionLock").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectBegin()
	m.ExpectExec("LockProviderAdmission").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("GetProviderCapacityForUpdate").WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	if _, err := s.UpdateCapacity(context.Background(), 7, "feishu_aily", in); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
