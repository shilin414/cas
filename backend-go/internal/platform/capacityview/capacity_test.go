package capacityview

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"strings"
	"testing"
	"time"
)

func TestCapacityUsesOneDBSamplingInstantAndOneStatement(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	at := time.Date(2026, 9, 22, 12, 0, 0, 100000000, time.UTC)
	// Later query execution must not re-evaluate expiry at the later wall clock.
	m.ExpectQuery("SELECT COUNT.*effective").WithArgs("p", at, "p").WillReturnRows(sqlmock.NewRows([]string{"effective", "controlled", "uncontrolled"}).AddRow(1, 1, 0))
	got, err := Read(context.Background(), conn, "p", at)
	if err != nil || got.Effective != 1 || got.Controlled != 1 || got.Uncontrolled != 0 {
		t.Fatalf("depth=%+v err=%v", got, err)
	}
	if strings.Contains(query, "CURRENT_TIMESTAMP") || !strings.Contains(query, "GROUP BY run_id") {
		t.Fatal("capacity observation has drifting time or duplicates")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestCapacityReadFailsClosedWithoutClockOrOnDatabaseError(t *testing.T) {
	if _, err := Read(context.Background(), nil, "p", time.Time{}); err == nil {
		t.Fatal("missing authoritative sample clock accepted")
	}
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	failure := errors.New("database unavailable")
	m.ExpectQuery("SELECT COUNT.*effective").WillReturnError(failure)
	if _, err := Read(context.Background(), conn, "p", time.Now()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
