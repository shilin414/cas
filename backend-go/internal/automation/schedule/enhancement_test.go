package schedule

import (
	"encoding/json"
	"testing"
	"time"
)

func timestamp(s string) *string { return &s }
func instant(s string) time.Time {
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return v
}
func TestEffectiveWindowNextRun(t *testing.T) {
	start, end := "2030-01-02T09:00:00Z", "2030-01-03T09:00:00Z"
	for _, kind := range []string{TypeOnce, TypeDaily, TypeWeekly, TypeMonthly} {
		t.Run(kind, func(t *testing.T) {
			cfg := TriggerConfig{Time: "09:00", DaysOfWeek: []int{2, 3, 4}, DayOfMonth: 2, StartsAt: &start, EndsAt: &end}
			runAt := instant(start)
			next, err := NextRunAfter(kind, cfg, &runAt, "UTC", instant("2030-01-01T00:00:00Z"))
			if err != nil || !next.Equal(runAt) {
				t.Fatalf("start boundary: %v %v", next, err)
			}
			next, err = NextRunAfter(kind, cfg, &runAt, "UTC", instant(end))
			if err != nil || !next.IsZero() {
				t.Fatalf("expiry: %v %v", next, err)
			}
		})
	}
	cfg := TriggerConfig{Time: "09:00", StartsAt: &start, EndsAt: &end}
	next, err := NextRunAfter(TypeDaily, cfg, nil, "UTC", instant(start))
	if err != nil || !next.Equal(instant(end)) {
		t.Fatalf("inclusive end: %v %v", next, err)
	}
	for _, at := range []string{start, end} {
		if !cfg.WithinWindow(instant(at)) {
			t.Fatalf("excluded boundary %s", at)
		}
	}
	if cfg.WithinWindow(instant(start).Add(-time.Nanosecond)) || cfg.WithinWindow(instant(end).Add(time.Nanosecond)) {
		t.Fatal("included outside window")
	}
}
func TestInvalidEffectiveWindows(t *testing.T) {
	for _, cfg := range []TriggerConfig{
		{StartsAt: timestamp("")}, {StartsAt: timestamp("2030-01-01")}, {EndsAt: timestamp("wrong")},
		{StartsAt: timestamp("2030-01-02T00:00:00Z"), EndsAt: timestamp("2030-01-01T00:00:00Z")},
	} {
		for _, kind := range []string{TypeOnce, TypeDaily} {
			cfg.Time = "09:00"
			if cfg.Validate(kind) == nil {
				t.Fatalf("accepted %s %#v", kind, cfg)
			}
		}
	}
	cfg := TriggerConfig{StartsAt: timestamp("2030-01-01T00:00:00Z"), EndsAt: timestamp("2029-12-31T19:00:00-05:00")}
	if err := cfg.Validate(TypeOnce); err != nil {
		t.Fatal(err)
	}
}
func TestPreviewWindowAndOnce(t *testing.T) {
	run := instant("2099-01-02T09:00:00Z")
	in := &CreateInput{ScheduleType: TypeOnce, Timezone: "UTC", RunAt: &run, TriggerConfig: TriggerConfig{StartsAt: timestamp("2099-01-03T00:00:00Z")}}
	if _, err := PreviewRuns(in, 5); err == nil {
		t.Fatal("once outside window accepted")
	}
	in.ScheduleType = TypeDaily
	in.TriggerConfig = TriggerConfig{Time: "09:00", StartsAt: timestamp("2099-01-02T09:00:00Z"), EndsAt: timestamp("2099-01-03T09:00:00Z")}
	runs, err := PreviewRuns(in, 5)
	if err != nil || len(runs) != 2 {
		t.Fatalf("preview: %v %v", runs, err)
	}
	in.TriggerConfig.EndsAt = timestamp("2000-01-01T00:00:00Z")
	in.TriggerConfig.StartsAt = nil
	runs, err = PreviewRuns(in, 5)
	if err != nil || len(runs) != 0 {
		t.Fatalf("expired: %v %v", runs, err)
	}
}
func TestDeliveryCondition(t *testing.T) {
	for _, tt := range []struct {
		c     DeliveryCondition
		reply string
		want  bool
	}{
		{DeliveryCondition{}, "", true}, {DeliveryCondition{Operator: "always"}, "", true},
		{DeliveryCondition{Operator: "contains", Text: "Alert"}, "Alert: hello", true},
		{DeliveryCondition{Operator: "contains", Text: "Alert"}, "alert", false},
		{DeliveryCondition{Operator: "not_contains", Text: "Alert"}, "alert", true},
		{DeliveryCondition{Operator: "contains", Text: "a.*b"}, "axxb", false},
		{DeliveryCondition{Operator: "contains", Text: " 告警 "}, "告警", false},
		{DeliveryCondition{Operator: "not_contains", Text: "x"}, "", true},
	} {
		if got := tt.c.Matches(tt.reply); got != tt.want {
			t.Fatalf("%+v on %q = %v", tt.c, tt.reply, got)
		}
	}
	for _, c := range []DeliveryCondition{{Operator: "contains"}, {Operator: "not_contains", Text: " \t"}, {Operator: "regex", Text: "a"}} {
		if c.Validate() == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	var d DeliveryInput
	if err := json.Unmarshal([]byte(`{"target_type":"user","target_id":"u","condition":{"operator":"contains","text":"Alert"}}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.Condition == nil || !d.Condition.Matches("Alert") {
		t.Fatalf("condition lost: %+v", d)
	}
}

func TestEffectiveWindowUsesInstantsNotRecurrenceTimezone(t *testing.T) {
	cfg := TriggerConfig{Time: "17:00", StartsAt: timestamp("2030-01-02T01:00:00-08:00"), EndsAt: timestamp("2030-01-02T09:00:00Z")}
	next, err := NextRunAfter(TypeDaily, cfg, nil, "Asia/Shanghai", instant("2030-01-01T00:00:00Z"))
	if err != nil || !next.Equal(instant("2030-01-02T09:00:00Z")) {
		t.Fatalf("offset boundary %v %v", next, err)
	}
	if _, err := NextRunAfter(TypeOnce, cfg, nil, "UTC", instant("2099-01-01T00:00:00Z")); err == nil {
		t.Fatal("missing run_at accepted on expired once")
	}
}

func TestEffectiveWindowRejectsSubMillisecondPrecision(t *testing.T) {
	for _, field := range []string{"starts_at", "ends_at"} {
		t.Run(field, func(t *testing.T) {
			cfg := TriggerConfig{Time: "09:00"}
			v := timestamp("2030-01-02T09:00:00.000400Z")
			if field == "starts_at" {
				cfg.StartsAt = v
			} else {
				cfg.EndsAt = v
			}
			if err := cfg.Validate(TypeDaily); err == nil {
				t.Fatal("sub-millisecond bound must be rejected before persistence")
			}
		})
	}
	runAt := instant("2030-01-02T09:00:00.000400Z")
	if _, err := NextRunAfter(TypeOnce, TriggerConfig{}, &runAt, "UTC", instant("2030-01-01T00:00:00Z")); err == nil {
		t.Fatal("sub-millisecond run_at must be rejected by preview")
	}
	runAt = instant("2030-01-02T09:00:00.123Z")
	cfg := TriggerConfig{StartsAt: timestamp("2030-01-02T09:00:00.123Z"), EndsAt: timestamp("2030-01-02T09:00:00.123Z")}
	got, err := NextRunAfter(TypeOnce, cfg, &runAt, "UTC", instant("2030-01-01T00:00:00Z"))
	if err != nil || !got.Equal(runAt) {
		t.Fatalf("millisecond boundary must stay inclusive: %v %v", got, err)
	}
}
