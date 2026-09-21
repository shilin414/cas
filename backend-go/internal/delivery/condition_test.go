package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/execution"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

// Only immutable expectation rows are provided. Any read of mutable delivery
// configuration, premature commit, or outbox for a mismatch fails this test.
func TestConditionalFanoutUsesFinalReplyAndImmutableSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, op, text, reply string
		skip                  bool
	}{
		{"mismatch", "contains", "Alert", "normal", true},
		{"literal-case", "contains", "Alert", "alert", true},
		{"match", "contains", "Alert", "Alert: warning", false},
		{"inverse", "not_contains", "Alert", "Alert: warning", true},
		{"legacy-always", "always", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			mock.ExpectBegin()
			tx, err := conn.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			now := time.Now().UTC()
			mock.ExpectQuery("GetScheduleOccurrenceByID").WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "schedule_id", "scheduled_at", "enqueued_at", "admitted_at", "run_id", "status", "triggered_at", "finished_at", "created_at", "updated_at", "delivery_snapshot_at"}).AddRow(7, 3, now, nil, nil, nil, "succeeded", nil, now, now, now, now))
			mock.ExpectQuery("ListOccurrenceDeliveryExpectations").WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"occurrence_id", "schedule_delivery_id", "channel", "sender_identity_mode", "target_type", "target_id", "target_name", "content_mode", "created_at", "condition_operator", "condition_text"}).AddRow(7, 11, "feishu", "owner_user", "user", "snapshot-target", "old name", "summary", now, tc.op, tc.text))
			rid := ids.New()
			occID, userID := int64(7), int64(42)
			mock.ExpectExec("CreateDeliveryExecution").WithArgs(sqlmock.AnyArg(), uint64(7), rid.Bytes(), uint64(11), uint64(42), "user", "snapshot-target").WillReturnResult(sqlmock.NewResult(1, 1))
			if tc.skip {
				mock.ExpectExec("SkipConditionalDelivery").WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
			} else {
				mock.ExpectExec("CreateOutboxEvent").WillReturnResult(sqlmock.NewResult(1, 1))
			}
			mock.ExpectCommit()
			run := &execution.Run{ID: rid, TriggerID: &occID, UserID: &userID, Input: map[string]any{"prompt": "Alert"}, Output: map[string]any{"text": tc.reply, "tools": []string{"Alert"}}}
			if err := NewDispatcher(conn, nil, nil).CreateInTx(context.Background(), tx, run); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
