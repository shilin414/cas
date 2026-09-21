package http

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/automation/schedule"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/identity"
)

func automationHandlerRow(t *testing.T, value any) *sqlmock.Rows {
	t.Helper()
	rv := reflect.ValueOf(value)
	columns := make([]string, rv.NumField())
	values := make([]driver.Value, rv.NumField())
	for i := range columns {
		columns[i] = rv.Type().Field(i).Name
		var err error
		values[i], err = driver.DefaultParameterConverter.ConvertValue(rv.Field(i).Interface())
		if err != nil {
			t.Fatal(err)
		}
	}
	return sqlmock.NewRows(columns).AddRow(values...)
}

func TestAutomationDetailExplicitDeliveries(t *testing.T) {
	for _, kind := range []string{"empty", "configured", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			row := db.Schedule{ID: 71, OwnerUserID: 42, Name: "test", ApplicationID: 1, InputPayload: json.RawMessage(`{"prompt":"test"}`), ScheduleType: "daily", TriggerConfig: json.RawMessage(`{"time":"09:00"}`), Timezone: "UTC", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			for i := 0; i < 2; i++ {
				m.ExpectQuery("GetScheduleByID").WithArgs(uint64(71)).WillReturnRows(automationHandlerRow(t, row))
			}
			query := m.ExpectQuery("ListDeliveriesBySchedule").WithArgs(uint64(71))
			if kind == "unavailable" {
				query.WillReturnError(errors.New("database unavailable"))
			} else if kind == "configured" {
				query.WillReturnRows(automationHandlerRow(t, db.ScheduleDelivery{ID: 1, ScheduleID: 71, TargetType: "chat", TargetID: "test-target", TargetName: "测试群", Enabled: true, ConditionOperator: "contains", ConditionText: sql.NullString{String: "需要关注", Valid: true}}))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			s := &Server{Schedules: schedule.NewService(conn, nil, nil), Log: slog.Default()}
			r := httptest.NewRequest(http.MethodGet, "/api/v2/schedules/71", nil)
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 42, IsActive: true}}))
			w := httptest.NewRecorder()
			s.GetSchedule(w, r, 71)
			if kind == "unavailable" {
				if w.Code != 500 {
					t.Fatalf("incomplete detail must fail closed, status=%d body=%s", w.Code, w.Body)
				}
			} else {
				if w.Code != 200 {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if _, ok := body["deliveries"]; !ok {
					t.Fatalf("successful detail omitted deliveries: %s", w.Body)
				}
				var ds []deliveryRecord
				if err := json.Unmarshal(body["deliveries"], &ds); err != nil {
					t.Fatal(err)
				}
				if kind == "empty" && string(body["deliveries"]) != "[]" {
					t.Fatalf("empty config must be explicit []: %s", w.Body)
				}
				if kind == "configured" && (len(ds) != 1 || ds[0].Condition.Operator != "contains" || ds[0].Condition.Text != "需要关注") {
					t.Fatalf("condition missing: %+v", ds)
				}
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
