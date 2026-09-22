package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"strings"
	"testing"
	"time"
)

func TestOverviewCountsAlertsAndReadFailure(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	m.ExpectBegin()
	m.ExpectQuery("CurrentDBTime").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
	m.ExpectQuery("SELECT provider_key, name, status, max_inflight FROM providers").WillReturnRows(sqlmock.NewRows([]string{"provider_key", "name", "status", "max_inflight"}).AddRow("feishu_aily", "Aily", "active", 20))
	m.ExpectQuery("SELECT provider, status, COUNT").WillReturnRows(sqlmock.NewRows([]string{"provider", "status", "n", "oldest"}).AddRow("feishu_aily", "queued", 6, now.Add(-6*time.Minute)).AddRow("feishu_aily", "running", 2, now.Add(-time.Minute)))
	m.ExpectQuery("SELECT COUNT.*schedule_occurrences").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(5))
	m.ExpectQuery("SELECT status, COUNT.*delivery_executions").WillReturnRows(sqlmock.NewRows([]string{"status", "n"}).AddRow("pending", 3).AddRow("sending", 1))
	m.ExpectQuery("CountPendingOutbox").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(4))
	m.ExpectQuery("SELECT COUNT.*effective").WithArgs("feishu_aily", now, "feishu_aily").WillReturnRows(sqlmock.NewRows([]string{"effective", "controlled", "uncontrolled"}).AddRow(2, 1, 1))
	m.ExpectCommit()
	out, err := NewService(conn).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Totals.Queued != 6 || out.Totals.Running != 2 || out.Totals.PendingOccurrences != 5 || out.Totals.PendingOutbox != 4 || len(out.Alerts) < 2 || out.Providers[0].OldestQueuedAgeSeconds != 360 {
		t.Fatalf("out=%+v", out)
	}
	raw, _ := json.Marshal(out)
	for _, secret := range []string{"secret_ref", "runtime_snapshot", "input_payload"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	m.ExpectBegin()
	m.ExpectQuery("CurrentDBTime").WillReturnError(errors.New("database down"))
	m.ExpectRollback()
	if snapshot, err := NewService(conn).Overview(context.Background()); err == nil || snapshot != nil {
		t.Fatalf("failure fabricated snapshot %+v err=%v", snapshot, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestRunPagePreservesLargeIDsAndUsesKeysetCursor(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	now := time.Now().UTC().Truncate(time.Millisecond)
	a, b := ids.New(), ids.New()
	m.ExpectQuery("CurrentDBTime").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
	cols := []string{"id", "provider", "runtime_type", "status", "priority", "trigger_type", "user_id", "application_id", "conversation_id", "queued_at", "available_at", "started_at", "finished_at", "created_at", "attempt", "max_attempts", "error_code", "provider_status"}
	rows := sqlmock.NewRows(cols).AddRow(a.Bytes(), "feishu_aily", "agent", "queued", "scheduled_normal", "scheduled", int64(9007199254740993), 1, 2, now.Add(-time.Minute), now.Add(time.Minute), nil, nil, now, 0, 3, "", "active").AddRow(b.Bytes(), "feishu_aily", "agent", "queued", "interactive_user", "interactive", int64(9007199254740993), 1, 3, now.Add(-time.Minute), nil, nil, nil, now.Add(-time.Second), 0, 3, "", "active")
	m.ExpectQuery("SELECT r.id, r.provider").WithArgs("queued", "feishu_aily", int64(9007199254740993), 2).WillReturnRows(rows)
	f := RunFilter{Status: "queued", Provider: "feishu_aily", OwnerUserID: "9007199254740993", Limit: 1}
	out, err := NewService(conn).ListRuns(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 || out.NextCursor == nil || *out.Results[0].OwnerUserID != "9007199254740993" || out.Results[0].WaitReason != "deferred" {
		t.Fatalf("out=%+v", out)
	}
	cursor, err := decodeCursor(*out.NextCursor, f)
	if err != nil || cursor.ID != a {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	f.Cursor = *out.NextCursor
	m.ExpectQuery("CurrentDBTime").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
	m.ExpectQuery("r.created_at <.*r.id <").WithArgs("queued", "feishu_aily", int64(9007199254740993), now, now, a.Bytes(), 2).WillReturnRows(sqlmock.NewRows(cols))
	out, err = NewService(conn).ListRuns(context.Background(), f)
	if err != nil || out.NextCursor != nil || len(out.Results) != 0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestQueryBudgetAndCancelledContextFailWithoutDatabase(t *testing.T) {
	if _, err := NewService(nil).Overview(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	s := NewService(new(sql.DB))
	for i := 0; i < 4; i++ {
		s.reads <- struct{}{}
	}
	if _, err := s.Overview(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ListRuns(ctx, RunFilter{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
