package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/database"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

// Temporary tables shadow names on ONE connection; these tests never insert
// fixtures into the actual scheduler/worker tables and never invoke a provider.
func hardeningQueries(t *testing.T) (context.Context, *sql.Conn, *db.Queries) {
	t.Helper()
	if os.Getenv("STUDIO_TEST_DB") != "1" {
		t.Skip("requires isolated MySQL; STUDIO_TEST_DB=1")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	d, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	conn, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for _, table := range []string{"schedules", "schedule_occurrences", "runs", "delivery_executions"} {
		if _, err := conn.ExecContext(ctx, "CREATE TEMPORARY TABLE "+table+" LIKE "+table); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := conn.ExecContext(cleanup, "DROP TEMPORARY TABLE "+table); err != nil {
				t.Errorf("drop temporary %s: %v", table, err)
			}
		})
	}
	return ctx, conn, db.New(conn)
}
func TestHardeningMySQLScansBypassBlockedHead(t *testing.T) {
	ctx, conn, q := hardeningQueries(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	var eligible uint64
	for i := 0; i < 101; i++ {
		result, err := conn.ExecContext(ctx, `INSERT INTO schedules(owner_user_id,name,application_id,schedule_type,next_run_at,overlap_policy) VALUES(42,?,1,'daily',?,'queue')`, fmt.Sprintf("fixture-%d", i), now.Add(time.Duration(i-200)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if i < 100 {
			if _, err := conn.ExecContext(ctx, `INSERT INTO schedule_occurrences(schedule_id,scheduled_at,status) VALUES(?,?,'running')`, id, now.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
		} else {
			eligible = uint64(id)
		}
	}
	due, err := q.ListDueSchedules(ctx, db.ListDueSchedulesParams{NextRunAt: sql.NullTime{Time: now, Valid: true}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != eligible {
		t.Fatalf("eligible schedule=%d due=%v", eligible, due)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO schedule_occurrences(schedule_id,scheduled_at,status) SELECT id,next_run_at,'pending' FROM schedules`); err != nil {
		t.Fatal(err)
	}
	pending, err := q.ListAdmissiblePendingOccurrences(ctx, db.ListAdmissiblePendingOccurrencesParams{MaxOutstanding: sql.NullInt64{Int64: 0, Valid: true}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ScheduleID != eligible {
		t.Fatalf("eligible=%d pending=%v", eligible, pending)
	}
}
func TestHardeningMySQLDeliveryDueTimeAndGeneration(t *testing.T) {
	ctx, conn, q := hardeningQueries(t)
	id := ids.New()
	_, err := conn.ExecContext(ctx, `INSERT INTO delivery_executions(id,occurrence_id,run_id,schedule_delivery_id,sender_user_id,target_type,target_id,next_attempt_at) VALUES(?,1,?,1,42,'chat','fixture',DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 1 HOUR))`, id.Bytes(), ids.New().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	assertRows := func(result sql.Result, err error, want int64) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		n, err := result.RowsAffected()
		if err != nil || n != want {
			t.Fatalf("affected=%d want=%d err=%v", n, want, err)
		}
	}
	r, err := q.CASClaimDelivery(ctx, db.CASClaimDeliveryParams{ID: id.Bytes(), Attempt: 0})
	assertRows(r, err, 0)
	if _, err := conn.ExecContext(ctx, `UPDATE delivery_executions SET next_attempt_at=NULL WHERE id=?`, id.Bytes()); err != nil {
		t.Fatal(err)
	}
	r, err = q.CASClaimDelivery(ctx, db.CASClaimDeliveryParams{ID: id.Bytes(), Attempt: 0})
	assertRows(r, err, 1)
	if _, err := conn.ExecContext(ctx, `UPDATE delivery_executions SET updated_at=DATE_SUB(CURRENT_TIMESTAMP(3),INTERVAL 2 MINUTE) WHERE id=?`, id.Bytes()); err != nil {
		t.Fatal(err)
	}
	r, err = q.ReclaimStuckDeliveries(ctx, (time.Minute).Microseconds())
	assertRows(r, err, 1)
	r, err = q.CASClaimDelivery(ctx, db.CASClaimDeliveryParams{ID: id.Bytes(), Attempt: 1})
	assertRows(r, err, 1)
	r, err = q.CASFinishDelivery(ctx, db.CASFinishDeliveryParams{ID: id.Bytes(), Attempt: 1, Status: "succeeded", Column5: "succeeded"})
	assertRows(r, err, 0)
	r, err = q.RequeueDelivery(ctx, db.RequeueDeliveryParams{ID: id.Bytes(), Attempt: 1, BackoffMicros: 1000000})
	assertRows(r, err, 0)
	r, err = q.CASFinishDelivery(ctx, db.CASFinishDeliveryParams{ID: id.Bytes(), Attempt: 2, Status: "succeeded", Column5: "succeeded"})
	assertRows(r, err, 1)
}
