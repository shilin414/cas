package http

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeBatchInputRejectsMalformedAndUnknownJSON(t *testing.T) {
	cases := []string{`{"targets":["directory"],}`, `{"targets":["directory"],"extra":true}`, `{"targets":["directory"]} {}`, `null`, `[]`}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v2/admin/sync-batches", strings.NewReader(body))
			if _, err := decodeBatchInput(httptest.NewRecorder(), req); err == nil {
				t.Fatal("expected strict JSON rejection")
			}
		})
	}
}
func TestDecodeBatchInputRejectsOversizedBody(t *testing.T) {
	body := `{"targets":["` + strings.Repeat("x", 70<<10) + `"]}`
	req := httptest.NewRequest("POST", "/api/v2/admin/sync-batches", strings.NewReader(body))
	if _, err := decodeBatchInput(httptest.NewRecorder(), req); err == nil {
		t.Fatal("expected oversized body rejection")
	}
}
func TestNormalizeEnterpriseSyncRoutes(t *testing.T) {
	cases := map[string]string{
		"/api/v2/admin/sync-targets/directory/config": "/api/v2/admin/sync-targets/{target}/config",
		"/api/v2/admin/sync-targets/user_groups/jobs": "/api/v2/admin/sync-targets/{target}/jobs",
		"/api/v2/admin/sync-batches/123":              "/api/v2/admin/sync-batches/{id}",
		"/api/v2/admin/sync-jobs":                     "/api/v2/admin/sync-jobs",
	}
	for path, want := range cases {
		req := httptest.NewRequest("GET", path, nil)
		if got := normalizeRoute(req); got != want {
			t.Fatalf("normalizeRoute(%q)=%q want %q", path, got, want)
		}
	}
}

func TestDecodeTargetConfigInputIsStrictAndBounded(t *testing.T) {
	bad := []string{`{"enabled":true,"schedule_type":"interval","interval_minutes":60,"daily_time":"02:00","timezone":"UTC","extra":1}`, `{"enabled":true} {}`, `null`, `[]`}
	for _, body := range bad {
		t.Run(body, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/api/v2/admin/sync-targets/directory/config", strings.NewReader(body))
			if _, err := decodeTargetConfigInput(httptest.NewRecorder(), req); err == nil {
				t.Fatal("expected strict config JSON rejection")
			}
		})
	}
	oversized := `{"timezone":"` + strings.Repeat("x", 70<<10) + `"}`
	req := httptest.NewRequest("PUT", "/api/v2/admin/sync-targets/directory/config", strings.NewReader(oversized))
	if _, err := decodeTargetConfigInput(httptest.NewRecorder(), req); err == nil {
		t.Fatal("expected oversized config rejection")
	}
	valid := `{"enabled":true,"schedule_type":"interval","interval_minutes":60,"daily_time":"02:00","timezone":"UTC"}`
	req = httptest.NewRequest("PUT", "/api/v2/admin/sync-targets/directory/config", strings.NewReader(valid))
	in, err := decodeTargetConfigInput(httptest.NewRecorder(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Enabled || in.IntervalMinutes != 60 {
		t.Fatalf("decoded=%+v", in)
	}
}
