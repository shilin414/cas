package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shilin414/cas/backend-go/internal/identity"
)

func TestAIRuntimeEndpointsEnforceAuthenticationBeforeWork(t *testing.T) {
	s := &Server{}
	handlers := map[string]func(http.ResponseWriter, *http.Request){
		"send":     func(w http.ResponseWriter, r *http.Request) { s.SendAITestMessage(w, r, "x") },
		"upload":   func(w http.ResponseWriter, r *http.Request) { s.UploadAITestAttachment(w, r, "x") },
		"download": func(w http.ResponseWriter, r *http.Request) { s.GetAITestAttachment(w, r, "x", "y") },
		"remove":   func(w http.ResponseWriter, r *http.Request) { s.DeleteAITestAttachment(w, r, "x", "y") },
		"get":      func(w http.ResponseWriter, r *http.Request) { s.GetAIInvocation(w, r, "x") },
		"cancel":   func(w http.ResponseWriter, r *http.Request) { s.CancelAIInvocation(w, r, "x") },
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				user   *identity.User
				status int
			}{
				{"anonymous", nil, 401}, {"not-admin", &identity.User{ID: 1, IsStaff: false}, 403}, {"service-unavailable", &identity.User{ID: 1, IsStaff: true}, 503},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := httptest.NewRequest(http.MethodPost, "/api/v2/admin/ai-models/", strings.NewReader(`{}`))
					if tc.user != nil {
						r = r.WithContext(context.WithValue(r.Context(), userCtxKey, &AuthenticatedUser{User: tc.user}))
					}
					w := httptest.NewRecorder()
					handler(w, r)
					if w.Code != tc.status {
						t.Fatalf("got %d: %s", w.Code, w.Body.String())
					}
				})
			}
		})
	}
}
func TestAIRuntimeUnknownErrorsNeverDiscloseSecrets(t *testing.T) {
	w := httptest.NewRecorder()
	writeAIRuntimeError(w, errors.New("password=top-secret"))
	if w.Code != 500 || strings.Contains(w.Body.String(), "top-secret") {
		t.Fatalf("unsafe error: %s", w.Body.String())
	}
}
func TestAIModelMetricsDoNotIncludeIDs(t *testing.T) {
	cases := map[string]string{
		"/api/v2/admin/ai-models/models/abc":                                                   "/api/v2/admin/ai-models/models/{id}",
		"/xiaoan-platform/api/v2/admin/ai-models/test-sessions/secret/attachments/secret-file": "/api/v2/admin/ai-models/test-sessions/{id}/attachments/{attachmentId}",
		"/api/v2/admin/ai-models/invocations/secret/cancel":                                    "/api/v2/admin/ai-models/invocations/{id}/cancel",
	}
	for actual, want := range cases {
		if got := normalizeRoute(httptest.NewRequest("GET", actual, nil)); got != want {
			t.Fatalf("%s != %s", got, want)
		}
	}
}
func TestAIUploadPreprocessingIsBounded(t *testing.T) {
	s := &Server{}
	releases := []func(){}
	for n := 0; n < 4; n++ {
		release, ok := s.acquireAIUpload()
		if !ok {
			t.Fatal("early capacity rejection")
		}
		releases = append(releases, release)
	}
	if _, ok := s.acquireAIUpload(); ok {
		t.Fatal("unbounded upload preprocessing")
	}
	for _, release := range releases {
		release()
	}
	release, ok := s.acquireAIUpload()
	if !ok {
		t.Fatal("capacity not released")
	}
	release()
}
