package http

import (
	"context"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/platform/config"
)

func TestPublicShareURLIncludesDeploymentPrefix(t *testing.T) {
	s := &Server{Config: &config.Config{PublicBaseURL: "https://studio.example/xiaoan-platform"}}
	r := httptest.NewRequest("POST", "/api/v2/feishu/forward", nil)
	r.Header.Set("Origin", "https://other.example")
	got, err := s.publicShareURL(r, "token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://studio.example/xiaoan-platform/share/token" {
		t.Fatalf("share URL = %q", got)
	}
}

type deploymentSessionStore struct{ fakeSessionStore }

func (s *deploymentSessionStore) Create(context.Context, identity.Session) (string, string, error) {
	return "test-session", "test-csrf", nil
}

func TestDeploymentCookiesCreatedAndClearedAtSameScope(t *testing.T) {
	for _, base := range []string{"/xiaoan-platform/", "/other/nested/", "/"} {
		t.Run(base, func(t *testing.T) {
			server := &Server{Config: &config.Config{BasePath: base, Session: config.SessionConfig{TTL: time.Hour, Secure: true}}, Store: &deploymentSessionStore{}}
			rec := httptest.NewRecorder()
			server.setSessionCookie(rec, httptest.NewRequest("POST", "/api/identity/admin/login", nil), &identity.User{ID: 1})
			created := 0
			for _, c := range rec.Result().Cookies() {
				if c.MaxAge == -1 {
					if c.Path != "/" {
						t.Fatalf("legacy cookie path %q", c.Path)
					}
					continue
				}
				created++
				if c.Path != base || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Value == "" {
					t.Fatalf("invalid cookie: %#v", c)
				}
				if c.HttpOnly != (c.Name == "studio_session") {
					t.Fatalf("cookie visibility: %#v", c)
				}
			}
			if created != 2 {
				t.Fatalf("created %d cookies", created)
			}
			rec = httptest.NewRecorder()
			server.clearSessionCookies(rec)
			scoped := 0
			for _, c := range rec.Result().Cookies() {
				if c.MaxAge != -1 || c.Value != "" {
					t.Fatalf("cookie not cleared: %#v", c)
				}
				if c.Path == base {
					scoped++
				}
			}
			if scoped != 2 {
				t.Fatalf("cleared %d scoped cookies", scoped)
			}
		})
	}
}
