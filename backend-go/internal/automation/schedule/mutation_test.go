package schedule

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
)

func mutationRow(t *testing.T, v any) *sqlmock.Rows {
	t.Helper()
	rv := reflect.ValueOf(v)
	cols := make([]string, rv.NumField())
	values := make([]driver.Value, rv.NumField())
	for i := range cols {
		cols[i] = rv.Type().Field(i).Name
		var err error
		values[i], err = driver.DefaultParameterConverter.ConvertValue(rv.Field(i).Interface())
		if err != nil {
			t.Fatal(err)
		}
	}
	return sqlmock.NewRows(cols).AddRow(values...)
}
func mutationFixture() db.Schedule {
	return db.Schedule{ID: 1, OwnerUserID: 42, Name: "test", ApplicationID: 1, InputPayload: json.RawMessage(`{"prompt":"test"}`), ScheduleType: TypeDaily, TriggerConfig: json.RawMessage(`{"time":"09:00"}`), Timezone: "UTC", Enabled: true, ConversationPolicy: ConversationNewEachRun, OverlapPolicy: OverlapQueue, MisfirePolicy: MisfireFireOnce, DeadlinePolicy: DeadlineExecuteAnyway}
}
func TestUpdateRejectsInvalidConditionBeforeWriting(t *testing.T) {
	for _, c := range []DeliveryCondition{{}, {Operator: "contains"}, {Operator: "not_contains", Text: " \t"}, {Operator: "regex", Text: "x"}} {
		t.Run(c.Operator, func(t *testing.T) {
			conn, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			m.ExpectBegin()
			m.ExpectQuery("GetScheduleRowForUpdate").WithArgs(uint64(1)).WillReturnRows(mutationRow(t, mutationFixture()))
			m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(instant("2030-01-01T00:00:00Z")))
			m.ExpectRollback()
			ds := []DeliveryInput{{TargetType: "chat", TargetID: "c", Condition: &c}}
			_, err = NewService(conn, nil, nil).Update(context.Background(), 1, 42, false, &UpdateInput{Deliveries: &ds})
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("err=%v", err)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestEnableRejectsExpiredWindow(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	row := mutationFixture()
	row.TriggerConfig = json.RawMessage(`{"time":"09:00","ends_at":"2030-01-01T00:00:00Z"}`)
	m.ExpectBegin()
	m.ExpectQuery("GetScheduleRowForUpdate").WillReturnRows(mutationRow(t, row))
	m.ExpectQuery("DBNow").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(instant("2030-01-01T00:00:00Z")))
	m.ExpectRollback()
	_, err = NewService(conn, nil, nil).SetEnabled(context.Background(), 1, 42, false, true)
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("err=%v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestConditionPersistenceAndLegacyRead(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	m.ExpectExec("DeleteScheduleDeliveries").WithArgs(uint64(1)).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec("UpsertScheduleDelivery").WithArgs(uint64(1), "feishu", "owner_user", "chat", "c", "", "summary", true, "contains", " Alert ").WillReturnResult(sqlmock.NewResult(2, 1))
	m.ExpectExec("UpsertScheduleDelivery").WithArgs(uint64(1), "feishu", "owner_user", "user", "u", "", "summary", true, "always", nil).WillReturnResult(sqlmock.NewResult(3, 1))
	input := []DeliveryInput{{TargetType: "chat", TargetID: "c", Condition: &DeliveryCondition{Operator: "contains", Text: " Alert "}}, {TargetType: "user", TargetID: "u"}}
	if err := NewService(conn, nil, nil).replaceDeliveries(context.Background(), db.New(conn), 1, input); err != nil {
		t.Fatal(err)
	}
	got := deliveryFromDBRow(db.ScheduleDelivery{ConditionOperator: "contains", ConditionText: sql.NullString{String: " Alert ", Valid: true}})
	if got.Condition.Operator != "contains" || got.Condition.Text != " Alert " {
		t.Fatalf("read lost condition %+v", got)
	}
	old := deliveryFromDBRow(db.ScheduleDelivery{})
	if old.Condition.Operator != "always" {
		t.Fatalf("legacy %+v", old)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestValidateOnceWindow(t *testing.T) {
	run := instant("2030-01-02T00:00:00Z")
	in := &CreateInput{Name: "test", ApplicationID: 1, Prompt: "x", ScheduleType: TypeOnce, Timezone: "UTC", RunAt: &run, TriggerConfig: TriggerConfig{EndsAt: timestamp("2030-01-01T00:00:00Z")}}
	in.defaults()
	err := validateInput(context.Background(), in, 42, func() time.Time { return instant("2029-01-01T00:00:00Z") }, nil)
	if err == nil {
		t.Fatal("create accepted once outside window")
	}
}

func TestValidateOnceRejectsSubMillisecondRunAt(t *testing.T) {
	run := instant("2030-01-02T00:00:00.000400Z")
	in := &CreateInput{Name: "test", ApplicationID: 1, Prompt: "x", ScheduleType: TypeOnce, Timezone: "UTC", RunAt: &run}
	in.defaults()
	err := validateInput(context.Background(), in, 42, func() time.Time { return instant("2029-01-01T00:00:00Z") }, nil)
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("sub-millisecond run_at must be a validation error: %v", err)
	}
}
