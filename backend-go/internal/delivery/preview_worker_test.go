package delivery

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/dbtypes"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"github.com/shilin414/cas/backend-go/internal/sharing"
)

// Generated models follow SELECT order for these queries. Reflect only their
// values so schema additions produce a scan failure instead of a fake success.
func previewRow(t *testing.T, v any) *sqlmock.Rows {
	t.Helper()
	rv := reflect.ValueOf(v)
	cols := make([]string, rv.NumField())
	values := make([]driver.Value, rv.NumField())
	for i := range cols {
		cols[i] = rv.Type().Field(i).Name
		value, err := driver.DefaultParameterConverter.ConvertValue(rv.Field(i).Interface())
		if err != nil {
			t.Fatal(err)
		}
		values[i] = value
	}
	return sqlmock.NewRows(cols).AddRow(values...)
}
func TestWorkerDeliversPersistedFullResultAsPreviewToBothTargets(t *testing.T) {
	for _, target := range []string{TargetUser, TargetChat} {
		t.Run(target, func(t *testing.T) {
			d, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			runID := ids.New()
			now := time.Now()
			answer := strings.Repeat("完整结果", 1000)
			snapshot, _ := json.Marshal([]sharing.Entry{{Role: "assistant", Content: &answer, Title: "结果标题快照", CreatedAt: now}})
			row := db.DeliveryExecution{ID: ids.New().Bytes(), RunID: runID.Bytes(), OccurrenceID: 5, SenderUserID: 7, TargetType: target, TargetID: "target"}
			m.ExpectQuery("GetScheduleOccurrenceByID").WithArgs(5).WillReturnRows(previewRow(t, db.ScheduleOccurrence{ID: 5, ScheduleID: 3, ScheduledAt: now, CreatedAt: now, UpdatedAt: now}))
			m.ExpectQuery("GetScheduleByID").WithArgs(3).WillReturnRows(previewRow(t, db.Schedule{ID: 3, Name: "任务后来改名", OwnerUserID: 7, CreatedAt: now, UpdatedAt: now}))
			m.ExpectQuery("GetRunByID").WithArgs(runID.Bytes()).WillReturnRows(previewRow(t, db.Run{ID: runID.Bytes(), UserID: sql.NullInt64{Valid: true, Int64: 7}, ConversationID: sql.NullInt64{Valid: true, Int64: 9}, Status: "succeeded", Output: dbtypes.JSONText(`{"text":"must use saved snapshot"}`), CreatedAt: now, UpdatedAt: now, QueuedAt: now}))
			m.ExpectQuery("GetRunResultShare").WithArgs(string(runID.Bytes())).WillReturnRows(sqlmock.NewRows([]string{"token", "snapshot", "revoked_at"}).AddRow("stable-token", snapshot, nil))
			provider := &recordingFeishu{}
			w := &Worker{DB: d, PublicBaseURL: "https://studio.example", Sender: &FeishuSender{Client: provider, Auth: &staticAuth{token: "owner-uat"}}}
			if err := w.send(context.Background(), row); err != nil {
				t.Fatal(err)
			}
			if provider.lastMsgType != "interactive" || !strings.Contains(provider.lastContent, "结果标题快照") || !strings.Contains(provider.lastContent, "https://studio.example/share/stable-token") || !strings.Contains(provider.lastContent, "完整结果") || strings.Contains(provider.lastContent, "must use saved snapshot") {
				t.Fatalf("wrong card: %s", provider.lastContent)
			}
			if len(provider.lastContent) > 12000 {
				t.Fatal("card did not truncate")
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
