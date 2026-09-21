package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/automation/schedule"
	"github.com/shilin414/cas/backend-go/internal/identity"
)

func TestAutomationContractSerialization(t *testing.T) {
	raw := `{"schedule_type":"daily","timezone":"UTC","trigger":{"time":"09:00","starts_at":"2099-01-02T09:00:00Z","ends_at":"2099-01-03T09:00:00Z"},"deliveries":[{"target_type":"chat","target_id":"chat1","condition":{"operator":"contains","text":"Alert"}}]}`
	var body scheduleUpsertBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	ds := toDomainDeliveries(*body.Deliveries)
	if len(ds) != 1 || ds[0].Condition == nil || ds[0].Condition.Operator != "contains" || ds[0].Condition.Text != "Alert" {
		t.Fatalf("condition lost: %+v", ds)
	}
	rec := toScheduleRecord(&schedule.Schedule{TriggerConfig: *body.Trigger, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil)
	rec.Deliveries = []deliveryRecord{{TargetType: ds[0].TargetType, TargetID: ds[0].TargetID, Condition: schedule.NormalizeCondition(ds[0].Condition)}}
	encoded, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"starts_at":"2099-01-02T09:00:00Z"`, `"ends_at":"2099-01-03T09:00:00Z"`, `"condition":{"operator":"contains","text":"Alert"}`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("missing %s in %s", want, encoded)
		}
	}
	rec.Trigger = schedule.TriggerConfig{}
	rec.Deliveries[0].Condition = schedule.NormalizeCondition(nil)
	encoded, _ = json.Marshal(rec)
	if strings.Contains(string(encoded), "starts_at") || !strings.Contains(string(encoded), `"condition":{"operator":"always"}`) {
		t.Fatalf("legacy default: %s", encoded)
	}
}
func TestAutomationPreviewHTTPBounds(t *testing.T) {
	cases := []struct {
		name, raw     string
		status, count int
	}{
		{"bounded", `{"schedule_type":"daily","timezone":"UTC","trigger":{"time":"09:00","starts_at":"2099-01-02T09:00:00Z","ends_at":"2099-01-03T09:00:00Z"}}`, 200, 2},
		{"expired", `{"schedule_type":"daily","timezone":"UTC","trigger":{"time":"09:00","ends_at":"2000-01-01T00:00:00Z"}}`, 200, 0},
		{"invalid-once", `{"schedule_type":"once","run_at":"2099-01-01T00:00:00Z","trigger":{"starts_at":"bad"}}`, 400, 0},
		{"once-outside", `{"schedule_type":"once","run_at":"2099-01-01T00:00:00Z","trigger":{"ends_at":"2098-01-01T00:00:00Z"}}`, 400, 0},
		{"reversed", `{"schedule_type":"daily","trigger":{"time":"09:00","starts_at":"2099-01-02T00:00:00Z","ends_at":"2099-01-01T00:00:00Z"}}`, 400, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v2/schedules/preview", strings.NewReader(tc.raw))
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 1, IsActive: true}}))
			w := httptest.NewRecorder()
			(&Server{}).PreviewScheduleRuns(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if tc.status == 200 {
				var body struct {
					Runs []string `json:"next_runs"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if len(body.Runs) != tc.count {
					t.Fatalf("preview response: %s", w.Body)
				}
			}
		})
	}
}
