// Package schedule owns the Schedule domain: what runs, when, with which
// policies. Schedule only answers "when to create a Run"; it never calls
// providers or sends messages itself.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule types (first stage): structured triggers only — no raw user cron.
const (
	TypeOnce    = "once"
	TypeDaily   = "daily"
	TypeWeekly  = "weekly"
	TypeMonthly = "monthly"
)

// Policies.
const (
	OverlapSkip  = "skip"
	OverlapQueue = "queue"

	MisfireFireOnce = "fire_once"
	MisfireSkip     = "skip"
	MisfireCatchUp  = "catch_up"

	ConversationNewEachRun = "new_each_run"
	ConversationReuse      = "reuse"

	DeadlineSkip          = "skip"
	DeadlineExecuteAnyway = "execute_anyway"
)

// MaxCatchUpSlots bounds how many missed occurrences catch_up may replay
// after downtime (评测 P1: a 30-day outage of a per-minute schedule must
// not enqueue 43200 runs).
const MaxCatchUpSlots = 10

// TriggerConfig is the structured trigger the UI submits; the backend
// derives timing from it. cron_expression stays an internal artifact.
type TriggerConfig struct {
	// StartsAt and EndsAt are inclusive absolute instants, independent of recurrence timezone.
	StartsAt *string `json:"starts_at,omitempty"`
	EndsAt   *string `json:"ends_at,omitempty"`
	// Time is "HH:MM" in the schedule timezone (daily/weekly/monthly).
	Time string `json:"time,omitempty"`
	// DaysOfWeek: 0=Sunday … 6=Saturday (weekly, multiple allowed).
	DaysOfWeek []int `json:"days_of_week,omitempty"`
	// DayOfMonth: 1..31; short months clamp to the last day.
	DayOfMonth int `json:"day_of_month,omitempty"`
}

// Validate checks the config against its schedule type.
func (t TriggerConfig) Validate(scheduleType string) error {
	if _, _, err := t.Bounds(); err != nil {
		return err
	}
	switch scheduleType {
	case TypeOnce:
		return nil
	case TypeDaily:
		return t.validateTime()
	case TypeWeekly:
		if err := t.validateTime(); err != nil {
			return err
		}
		if len(t.DaysOfWeek) == 0 {
			return fmt.Errorf("weekly trigger needs at least one day_of_week")
		}
		for _, d := range t.DaysOfWeek {
			if d < 0 || d > 6 {
				return fmt.Errorf("day_of_week %d out of range 0-6", d)
			}
		}
		return nil
	case TypeMonthly:
		if err := t.validateTime(); err != nil {
			return err
		}
		if t.DayOfMonth < 1 || t.DayOfMonth > 31 {
			return fmt.Errorf("day_of_month out of range 1-31")
		}
		return nil
	default:
		return fmt.Errorf("unsupported schedule_type %q", scheduleType)
	}
}

func (t TriggerConfig) validateTime() error {
	if len(t.Time) != 5 || t.Time[2] != ':' {
		return fmt.Errorf("time must be HH:MM")
	}
	h, err1 := strconv.Atoi(t.Time[:2])
	m, err2 := strconv.Atoi(t.Time[3:])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return fmt.Errorf("time must be HH:MM (00:00-23:59)")
	}
	return nil
}

// CronExpression renders an informational 5-field cron (no seconds) —
// internal/debug use; the UI never asks users to write cron.
func (t TriggerConfig) CronExpression(scheduleType string) string {
	hm := strings.SplitN(t.Time, ":", 2)
	if len(hm) != 2 {
		return ""
	}
	m, errM := strconv.Atoi(hm[1])
	h, errH := strconv.Atoi(hm[0])
	if errM != nil || errH != nil {
		return ""
	}
	switch scheduleType {
	case TypeDaily:
		return fmt.Sprintf("%d %d * * *", m, h)
	case TypeWeekly:
		ds := make([]string, 0, len(t.DaysOfWeek))
		for _, d := range t.DaysOfWeek {
			ds = append(ds, strconv.Itoa(d))
		}
		return fmt.Sprintf("%d %d * * %s", m, h, strings.Join(ds, ","))
	case TypeMonthly:
		return fmt.Sprintf("%d %d %d * *", m, h, t.DayOfMonth)
	}
	return ""
}

// Bounds validates optional RFC3339 instants. Explicit empty strings are invalid.
func (t TriggerConfig) Bounds() (start, end *time.Time, err error) {
	for _, field := range []struct {
		name string
		raw  *string
		dst  **time.Time
	}{
		{"starts_at", t.StartsAt, &start}, {"ends_at", t.EndsAt, &end},
	} {
		if field.raw == nil {
			continue
		}
		v, e := time.Parse(time.RFC3339Nano, *field.raw)
		if e != nil {
			return nil, nil, fmt.Errorf("trigger.%s must be RFC3339", field.name)
		}
		// The SQL schedule clock uses DATETIME(3); finer JSON precision cannot round-trip.
		if v.Nanosecond()%int(time.Millisecond) != 0 {
			return nil, nil, fmt.Errorf("trigger.%s supports millisecond precision at most", field.name)
		}
		*field.dst = &v
	}
	if start != nil && end != nil && start.After(*end) {
		return nil, nil, fmt.Errorf("trigger.ends_at must be on or after starts_at")
	}
	return start, end, nil
}

// WithinWindow fails closed on corrupt stored bounds and includes both endpoints.
func (t TriggerConfig) WithinWindow(at time.Time) bool {
	start, end, err := t.Bounds()
	return err == nil && (start == nil || !at.Before(*start)) && (end == nil || !at.After(*end))
}

// NextRunAfter returns the first trigger strictly after after, within the
// inclusive effective window. Zero means exhausted (once or bounded repeat).
// Monthly 29/30/31 clamp to the last day of short months.
func NextRunAfter(scheduleType string, cfg TriggerConfig, runAt *time.Time, timezone string, after time.Time) (time.Time, error) {
	if err := cfg.Validate(scheduleType); err != nil {
		return time.Time{}, err
	}
	start, end, err := cfg.Bounds()
	if err != nil {
		return time.Time{}, err
	}
	if scheduleType == TypeOnce && runAt == nil {
		return time.Time{}, fmt.Errorf("once schedule requires run_at")
	}
	if scheduleType == TypeOnce && runAt.Nanosecond()%int(time.Millisecond) != 0 {
		return time.Time{}, fmt.Errorf("run_at supports millisecond precision at most")
	}
	if scheduleType == TypeOnce && !cfg.WithinWindow(*runAt) {
		return time.Time{}, fmt.Errorf("run_at must be within trigger starts_at/ends_at")
	}
	if end != nil && !after.Before(*end) {
		return time.Time{}, nil
	}
	// Strictly-after recurrence includes starts_at by moving the cursor back 1ns.
	if start != nil && after.Before(*start) {
		after = start.Add(-time.Nanosecond)
	}
	next, err := nextRunUnbounded(scheduleType, cfg, runAt, timezone, after)
	if err != nil {
		return time.Time{}, err
	}
	if !next.IsZero() && !cfg.WithinWindow(next) {
		return time.Time{}, nil
	}
	return next, nil
}

func nextRunUnbounded(scheduleType string, cfg TriggerConfig, runAt *time.Time, timezone string, after time.Time) (time.Time, error) {
	switch scheduleType {
	case TypeOnce:
		if runAt == nil {
			return time.Time{}, fmt.Errorf("once schedule requires run_at")
		}
		if runAt.After(after) {
			return *runAt, nil
		}
		return time.Time{}, nil
	case TypeDaily, TypeWeekly, TypeMonthly:
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, fmt.Errorf("unknown timezone %q: %w", timezone, err)
		}
		if err := cfg.Validate(scheduleType); err != nil {
			return time.Time{}, err
		}
		hh, _ := strconv.Atoi(cfg.Time[:2])
		mm, _ := strconv.Atoi(cfg.Time[3:])
		local := after.In(loc)
		day := time.Date(local.Year(), local.Month(), local.Day(), hh, mm, 0, 0, loc)
		if scheduleType == TypeDaily {
			if !day.After(after) {
				day = day.AddDate(0, 0, 1)
			}
			return day.UTC(), nil
		}
		if scheduleType == TypeWeekly {
			for i := 0; i < 8; i++ { // 7 days is enough; 8 for paranoia
				cand := day.AddDate(0, 0, i)
				if matchesWeekday(cand, cfg.DaysOfWeek) && cand.After(after) {
					return cand.UTC(), nil
				}
			}
			return time.Time{}, fmt.Errorf("weekly: no matching weekday")
		}
		// Monthly: scan up to 14 months. Anchor on the first of the
		// month — AddDate on day 31 would overflow short months.
		monthStart := time.Date(local.Year(), local.Month(), 1, hh, mm, 0, 0, loc)
		for i := 0; i < 14; i++ {
			cand := clampToMonth(monthStart.AddDate(0, i, 0), cfg.DayOfMonth, hh, mm)
			if cand.After(after) {
				return cand.UTC(), nil
			}
		}
		return time.Time{}, fmt.Errorf("monthly: no future slot")
	default:
		return time.Time{}, fmt.Errorf("unsupported schedule_type %q", scheduleType)
	}
}

func matchesWeekday(t time.Time, days []int) bool {
	wd := int(t.Weekday())
	for _, d := range days {
		if d == wd {
			return true
		}
	}
	return false
}

// clampToMonth builds day-of-month `dom` at hh:mm for cand's month,
// clamping to the last day when the month is shorter (31 → Feb 28/29).
func clampToMonth(monthAnchor time.Time, dom, hh, mm int) time.Time {
	last := time.Date(monthAnchor.Year(), monthAnchor.Month()+1, 0, 0, 0, 0, 0, monthAnchor.Location())
	day := dom
	if day > last.Day() {
		day = last.Day()
	}
	return time.Date(monthAnchor.Year(), monthAnchor.Month(), day, hh, mm, 0, 0, monthAnchor.Location())
}
