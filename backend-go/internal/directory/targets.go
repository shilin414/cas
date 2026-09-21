package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const (
	TargetDirectory  = "directory"
	TargetUserGroups = "user_groups"
)

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int64  `json:"count,omitempty"`
}

type Result struct {
	Metrics  map[string]int64 `json:"metrics"`
	Warnings []Warning        `json:"warnings"`
	Version  int64            `json:"version"`
}

type TargetRunner interface {
	Code() string
	Dependencies() []string
	Run(context.Context, *SyncRun, string) (Result, error)
}

type TargetDefinition struct {
	Code          string   `json:"code"`
	DisplayName   string   `json:"display_name"`
	Description   string   `json:"description"`
	Dependencies  []string `json:"dependencies"`
	MetricsSchema []string `json:"metrics_schema"`
}

var targetDefinitions = []TargetDefinition{
	{Code: TargetDirectory, DisplayName: "Directory", Description: "Departments, users, and department memberships", MetricsSchema: []string{"departments", "users", "active_users", "memberships", "active_memberships"}},
	{Code: TargetUserGroups, DisplayName: "User groups", Description: "Feishu normal/dynamic groups and mapped members", Dependencies: []string{TargetDirectory}, MetricsSchema: []string{"groups", "group_members", "unknown_users"}},
}

func TargetDefinitions() []TargetDefinition {
	out := make([]TargetDefinition, len(targetDefinitions))
	copy(out, targetDefinitions)
	for i := range out {
		out[i].Dependencies = append([]string(nil), out[i].Dependencies...)
		out[i].MetricsSchema = append([]string(nil), out[i].MetricsSchema...)
	}
	return out
}

func ValidateTarget(code string) error {
	for _, target := range targetDefinitions {
		if target.Code == code {
			return nil
		}
	}
	return fmt.Errorf("unknown sync target %q", code)
}

func OrderedTargets(requested []string) ([]string, error) {
	if len(requested) == 0 {
		requested = []string{TargetDirectory, TargetUserGroups}
	}
	selected := map[string]bool{}
	for _, code := range requested {
		if err := ValidateTarget(code); err != nil {
			return nil, err
		}
		selected[code] = true
	}
	defs := map[string]TargetDefinition{}
	order := []string{}
	for _, d := range targetDefinitions {
		defs[d.Code] = d
		order = append(order, d.Code)
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	out := []string{}
	var visit func(string) error
	visit = func(code string) error {
		if visited[code] {
			return nil
		}
		if visiting[code] {
			return fmt.Errorf("sync target dependency cycle at %s", code)
		}
		visiting[code] = true
		for _, dep := range defs[code].Dependencies {
			if selected[dep] {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		visiting[code] = false
		visited[code] = true
		out = append(out, code)
		return nil
	}
	for _, code := range order {
		if selected[code] {
			if err := visit(code); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

type TargetConfig struct {
	TargetCode       string     `json:"target_code"`
	Enabled          bool       `json:"enabled"`
	ScheduleType     string     `json:"schedule_type"`
	IntervalMinutes  int        `json:"interval_minutes"`
	DailyTime        string     `json:"daily_time"`
	Timezone         string     `json:"timezone"`
	NextRunAt        *time.Time `json:"next_run_at"`
	LastRunAt        *time.Time `json:"last_run_at"`
	LastSuccessAt    *time.Time `json:"last_success_at"`
	TargetVersion    int64      `json:"target_version"`
	LastErrorCode    string     `json:"last_error_code"`
	LastErrorMessage string     `json:"last_error_message"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type SyncTargetView struct {
	TargetDefinition
	Config *TargetConfig `json:"config,omitempty"`
}

type SyncBatch struct {
	ID               int64      `json:"id"`
	TriggerType      string     `json:"trigger_type"`
	Status           string     `json:"status"`
	RequestedTargets []string   `json:"requested_targets"`
	CreatedBy        *int64     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	FinishedAt       *time.Time `json:"finished_at"`
	Jobs             []SyncRun  `json:"jobs"`
}

func encodeResult(result Result) ([]byte, []byte, error) {
	metrics, err := json.Marshal(result.Metrics)
	if err != nil {
		return nil, nil, err
	}
	warnings, err := json.Marshal(result.Warnings)
	if err != nil {
		return nil, nil, err
	}
	return metrics, warnings, nil
}

func sortedMetricKeys(metrics map[string]int64) []string {
	out := make([]string, 0, len(metrics))
	for k := range metrics {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
