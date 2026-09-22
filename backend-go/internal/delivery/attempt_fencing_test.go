package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	goredis "github.com/redis/go-redis/v9"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"github.com/shilin414/cas/backend-go/internal/platform/telemetry"
)

func TestStaleDeliveryWakeupIsAcknowledgedWithoutSending(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	redis, acks := combinedRedis(t)
	row := db.DeliveryExecution{ID: ids.New().Bytes(), Status: "pending", Attempt: 2, MaxAttempts: 5, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	m.ExpectQuery("GetDeliveryExecutionByID").WithArgs(row.ID).WillReturnRows(previewRow(t, row))
	m.ExpectExec("CASClaimDelivery").WithArgs(row.ID, row.Attempt).WillReturnResult(sqlmock.NewResult(0, 0))
	sender := &fakeSender{}
	w := NewWorker(conn, redis, "unit", sender, nil, nil, nil)
	w.process(context.Background(), goredis.XMessage{ID: "1-0", Values: map[string]any{"run_id": ids.ID(row.ID).String()}})
	if sender.calls != 0 || acks.calls != 1 {
		t.Fatalf("sends=%d acks=%d", sender.calls, acks.calls)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestDeliveryFailureWritesAreGenerationFenced(t *testing.T) {
	for _, attempt := range []uint32{1, 5} {
		t.Run(string(rune('0'+attempt)), func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			row := db.DeliveryExecution{ID: ids.New().Bytes(), Status: "sending", Attempt: attempt, MaxAttempts: 5}
			if attempt < 5 {
				m.ExpectExec("RequeueDelivery").WithArgs(sqlmock.AnyArg(), "send_failed", "failure", row.ID, attempt).WillReturnResult(sqlmock.NewResult(0, 0))
			} else {
				m.ExpectExec("CASFinishDelivery").WithArgs("failed", "", "send_failed", "failure", "failed", row.ID, attempt).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			metrics := telemetry.NewMetrics("unit")
			w := NewWorker(conn, nil, "unit", nil, nil, nil, metrics)
			w.handleFailure(context.Background(), row, errors.New("failure"))
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
