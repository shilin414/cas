// These tests use ONLY the opt-in isolated CI database (STUDIO_TEST_DB=1).
// Never run them on a shared dev database with active scheduler/worker processes.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/automation/schedule"
	"github.com/shilin414/cas/backend-go/internal/delivery"
	"github.com/shilin414/cas/backend-go/internal/execution"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

func TestAutomationConditionalSnapshotAndTerminalAccounting(t *testing.T) {
	env := newScheduleEnv(t)
	ctx := context.Background()
	q := db.New(env.db)
	now, err := q.DBNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sid := env.seedSchedule(t, now.Add(-time.Minute).Truncate(time.Millisecond), schedule.OverlapQueue)
	// The first destination is skipped, proving it cannot prematurely finish
	// fan-out and prevent later matching expectations from getting executions.
	cases := []struct{ target, op, text, status string }{
		{"prompt-only", "contains", "PromptOnly", schedule.DeliverySkipped},
		{"final-match", "contains", "Alert", schedule.DeliveryPending},
		{"tool-only", "contains", "ToolOnly", schedule.DeliverySkipped},
		{"inverse", "not_contains", "Alert", schedule.DeliverySkipped},
		{"unconditional", "always", "", schedule.DeliveryPending},
	}
	for _, tc := range cases {
		_, err := q.UpsertScheduleDelivery(ctx, db.UpsertScheduleDeliveryParams{ScheduleID: uint64(sid), Channel: "feishu", SenderIdentityMode: "owner_user", TargetType: "chat", TargetID: tc.target, ContentMode: "summary", Enabled: true, ConditionOperator: tc.op, ConditionText: sql.NullString{String: tc.text, Valid: tc.text != ""}})
		if err != nil {
			t.Fatal(err)
		}
	}
	env.schd.ProcessDue(ctx)
	occ, err := q.LatestOccurrenceBySchedule(ctx, uint64(sid))
	if err != nil || !occ.RunID.Valid {
		t.Fatalf("occurrence: %+v %v", occ, err)
	}
	if _, err := env.db.ExecContext(ctx, `UPDATE schedule_deliveries SET condition_operator='always', condition_text=NULL, target_id=CONCAT(target_id,'-edited') WHERE schedule_id=?`, sid); err != nil {
		t.Fatal(err)
	}
	snapshots, err := q.ListOccurrenceDeliveryExpectations(ctx, occ.ID)
	if err != nil || len(snapshots) != len(cases) {
		t.Fatalf("snapshots: %+v %v", snapshots, err)
	}
	var runID ids.ID
	if err := runID.Scan([]byte(occ.RunID.String)); err != nil {
		t.Fatal(err)
	}
	runs := execution.NewService(env.db, nil, testLogger(), nil)
	dispatcher := delivery.NewDispatcher(env.db, testLogger(), nil)
	runs.CreateDeliveryExecutionsTx = dispatcher.CreateInTx
	claimed, won, err := runs.ClaimRun(ctx, runID, "conditional-final", time.Minute)
	if err != nil || !won {
		t.Fatalf("claim %v %v", won, err)
	}
	// Deliberately stale caller output must not determine predicates.
	claimed.Run.Output = map[string]any{"text": "PromptOnly ToolOnly"}
	claimed.Run.Input = map[string]any{"prompt": "PromptOnly"}
	if err := runs.FinalizeOwnedRun(ctx, claimed.Run, claimed.Ownership, &execution.FinishInput{Status: execution.StatusSucceeded, Output: map[string]any{"text": "Final Alert", "tools": []string{"ToolOnly"}}}); err != nil {
		t.Fatal(err)
	}
	rows, err := q.ListDeliveryExecutionsByOccurrence(ctx, occ.ID)
	if err != nil || len(rows) != len(cases) {
		t.Fatalf("executions: %+v %v", rows, err)
	}
	for _, tc := range cases {
		found := false
		for _, row := range rows {
			if row.TargetID != tc.target {
				continue
			}
			found = true
			if row.Status != tc.status {
				t.Fatalf("%s status=%s want=%s", tc.target, row.Status, tc.status)
			}
			if tc.status == schedule.DeliverySkipped && (row.Attempt != 0 || row.SentAt.Valid || row.ErrorCode != "condition_not_met") {
				t.Fatalf("skipped accounting: %+v", row)
			}
			expectedOutbox := int64(0)
			if tc.status == schedule.DeliveryPending {
				expectedOutbox = 1
			}
			if n := env.count(t, `SELECT COUNT(*) FROM outbox_events WHERE aggregate='delivery' AND aggregate_id=?`, row.ID); n != expectedOutbox {
				t.Fatalf("%s outbox=%d", tc.target, n)
			}
		}
		if !found {
			t.Fatalf("immutable target %s missing", tc.target)
		}
	}
	if n := env.count(t, `SELECT COUNT(*) FROM occurrence_delivery_expectations e LEFT JOIN delivery_executions d ON d.occurrence_id=e.occurrence_id AND d.schedule_delivery_id=e.schedule_delivery_id WHERE e.occurrence_id=? AND d.id IS NULL`, occ.ID); n != 0 {
		t.Fatalf("missing expectations=%d", n)
	}
	// At-least-once callback replay must not change either skipped or pending rows.
	tx, err := env.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := dispatcher.CreateInTx(ctx, tx, claimed.Run); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := env.count(t, `SELECT COUNT(*) FROM delivery_executions WHERE occurrence_id=?`, occ.ID); n != int64(len(cases)) {
		t.Fatalf("replayed executions=%d", n)
	}
}
func TestAutomationWindowCRUDAndEnable(t *testing.T) {
	env := newScheduleEnv(t)
	ctx := context.Background()
	now, err := db.New(env.db).DBNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(now.Year(), now.Month(), now.Day()+2, 9, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	startText, endText := start.Format(time.RFC3339), end.Format(time.RFC3339)
	for _, kind := range []string{schedule.TypeOnce, schedule.TypeDaily} {
		t.Run(kind, func(t *testing.T) {
			in := &schedule.CreateInput{Name: "window-crud", ApplicationID: 1, Prompt: "test", ScheduleType: kind, Timezone: "UTC", TriggerConfig: schedule.TriggerConfig{Time: "09:00", StartsAt: &startText, EndsAt: &endText}}
			if kind == schedule.TypeOnce {
				in.RunAt = &start
			}
			sch, err := env.svc.Create(ctx, 42, in)
			if err != nil {
				t.Fatal(err)
			}
			if sch.NextRunAt == nil || !sch.NextRunAt.Equal(start) || sch.TriggerConfig.StartsAt == nil || *sch.TriggerConfig.StartsAt != startText {
				t.Fatalf("roundtrip %+v", sch)
			}
			if _, err := env.schd.TriggerNow(ctx, sch.ID, 42, false); err == nil {
				t.Fatal("manual run before starts_at admitted")
			}
			if _, err := env.svc.SetEnabled(ctx, sch.ID, 42, false, false); err != nil {
				t.Fatal(err)
			}
			sch, err = env.svc.SetEnabled(ctx, sch.ID, 42, false, true)
			if err != nil || sch.NextRunAt == nil || !sch.NextRunAt.Equal(start) {
				t.Fatalf("enable %+v %v", sch, err)
			}
			invalid := []schedule.DeliveryInput{{TargetType: "chat", TargetID: "x", Condition: &schedule.DeliveryCondition{Operator: "contains", Text: " "}}}
			if _, err := env.svc.Update(ctx, sch.ID, 42, false, &schedule.UpdateInput{Deliveries: &invalid}); err == nil {
				t.Fatal("blank update condition accepted")
			}
			expired := now.Add(-time.Hour).Format(time.RFC3339)
			cfg := schedule.TriggerConfig{Time: "09:00", EndsAt: &expired}
			sch2, err := env.svc.Update(ctx, sch.ID, 42, false, &schedule.UpdateInput{TriggerConfig: &cfg})
			if kind == schedule.TypeOnce {
				if err == nil {
					t.Fatal("once outside range accepted")
				}
				return
			}
			if err != nil || sch2.Enabled || sch2.NextRunAt != nil {
				t.Fatalf("expired update %+v %v", sch2, err)
			}
			_, err = env.svc.SetEnabled(ctx, sch.ID, 42, false, true)
			var validation *schedule.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expired enable err=%v", err)
			}
		})
	}
}
func TestAutomationExpiredMisfirePoliciesNeverAdmit(t *testing.T) {
	for _, kind := range []string{schedule.TypeOnce, schedule.TypeDaily} {
		for _, policy := range []string{schedule.MisfireFireOnce, schedule.MisfireSkip, schedule.MisfireCatchUp} {
			t.Run(kind+"/"+policy, func(t *testing.T) {
				env := newScheduleEnv(t)
				ctx := context.Background()
				q := db.New(env.db)
				now, err := q.DBNow(ctx)
				if err != nil {
					t.Fatal(err)
				}
				slot := now.Add(-10 * time.Minute).Truncate(time.Millisecond)
				sid := env.seedSchedule(t, slot, schedule.OverlapQueue)
				start, end := now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Minute).Format(time.RFC3339Nano)
				cfg, _ := json.Marshal(schedule.TriggerConfig{Time: "09:00", StartsAt: &start, EndsAt: &end})
				if _, err := env.db.ExecContext(ctx, `UPDATE schedules SET trigger_config=?,schedule_type=?,run_at=?,misfire_policy=? WHERE id=?`, cfg, kind, slot, policy, sid); err != nil {
					t.Fatal(err)
				}
				env.schd.ProcessDue(ctx)
				sch, err := q.GetScheduleByID(ctx, uint64(sid))
				if err != nil || sch.Enabled || sch.NextRunAt.Valid {
					t.Fatalf("exhausted schedule %+v %v", sch, err)
				}
				occ, err := q.LatestOccurrenceBySchedule(ctx, uint64(sid))
				if err != nil || occ.Status != schedule.OccSkipped || occ.RunID.Valid {
					t.Fatalf("expired occurrence %+v %v", occ, err)
				}
			})
		}
	}
}

func TestAutomationMillisecondWindowRoundTrip(t *testing.T) {
	env := newScheduleEnv(t)
	ctx := context.Background()
	now, err := db.New(env.db).DBNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runAt := now.Add(time.Hour).Truncate(time.Second).Add(123 * time.Millisecond)
	bound := runAt.Format(time.RFC3339Nano)
	in := &schedule.CreateInput{Name: "millisecond window", ApplicationID: 1, Prompt: "test", ScheduleType: schedule.TypeOnce, Timezone: "UTC", RunAt: &runAt, TriggerConfig: schedule.TriggerConfig{StartsAt: &bound, EndsAt: &bound}}
	sch, err := env.svc.Create(ctx, 42, in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = env.db.ExecContext(context.Background(), "DELETE FROM schedules WHERE id=?", sch.ID) })
	if sch.RunAt == nil || !sch.RunAt.Equal(runAt) || sch.NextRunAt == nil || !sch.NextRunAt.Equal(runAt) || !sch.TriggerConfig.WithinWindow(*sch.RunAt) {
		t.Fatalf("stored millisecond instant must remain inside inclusive window: %+v", sch)
	}
	next, err := schedule.NextRunAfter(sch.ScheduleType, sch.TriggerConfig, sch.RunAt, sch.Timezone, now)
	if err != nil || !next.Equal(runAt) {
		t.Fatalf("persisted schedule can no longer advance: %v %v", next, err)
	}
	submillisecond := runAt.Add(400 * time.Microsecond)
	in.RunAt = &submillisecond
	in.TriggerConfig = schedule.TriggerConfig{} // Isolate precision, not outside-window validation.
	_, err = env.svc.Create(ctx, 42, in)
	var validation *schedule.ValidationError
	if !errors.As(err, &validation) || validation.Msg != "run_at supports millisecond precision at most" {
		t.Fatalf("expected precision validation error, got %v", err)
	}
}
