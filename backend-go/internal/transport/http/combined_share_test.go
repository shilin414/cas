package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/execution"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"github.com/shilin414/cas/backend-go/internal/sharing"
)

// Surface unexpected reads even if a handler turns the resulting SQL error
// into an expected 404. Negative cases install a final read-boundary query.
func combinedShareServer(t *testing.T) (*Server, sqlmock.Sqlmock) {
	t.Helper()
	conn, m, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(want, got string) error {
		err := sqlmock.QueryMatcherRegexp.Match(want, got)
		if err != nil {
			t.Errorf("unexpected database read/write: %v", err)
		}
		return err
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		_ = conn.Close()
	})
	return &Server{DB: conn, Runs: execution.NewService(conn, nil, nil, nil), FeishuAuth: fakeFeishuTokenResolver{token: "uat"}}, m
}
func combinedShareConfig(t *testing.T, base string) *config.Config {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_BASE_PATH", base)
	t.Setenv("PUBLIC_ORIGIN", "https://studio.example")
	t.Setenv("PUBLIC_BASE_URL", "https://obsolete.example/stale")
	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.env"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://studio.example" + strings.TrimSuffix(base, "/"); cfg.PublicBaseURL != want {
		t.Fatalf("public URL=%q want=%q", cfg.PublicBaseURL, want)
	}
	return cfg
}

// Real Feishu client and generated API routes; the test proxy models nginx's
// documented prefix stripping, not nginx itself or the frontend SPA router.
func combinedForwardCards(t *testing.T, s *Server, base string) []string {
	t.Helper()
	type sent struct {
		MsgType   string `json:"msg_type"`
		Content   string `json:"content"`
		ReceiveID string `json:"receive_id"`
	}
	sends := make(chan sent, 10)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body sent
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		idType := map[string]string{"ou_1": "open_id", "oc_1": "chat_id"}[body.ReceiveID]
		if idType == "" || r.URL.Query().Get("receive_id_type") != idType {
			t.Errorf("wrong recipient: %s %s", r.URL, body.ReceiveID)
		}
		if body.MsgType != "interactive" {
			t.Errorf("expected interactive, got %s", body.MsgType)
		}
		if r.Header.Get("Authorization") != "Bearer uat" {
			t.Error("missing user access token")
		}
		sends <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer provider.Close()
	s.Feishu = identity.NewFeishuClient(provider.URL, "test-id", "test-secret", provider.Client())
	body := `{"share_token":"token","targets":[{"target_type":"user","id":"ou_1"},{"target_type":"chat","id":"oc_1"}]}`
	req := httptest.NewRequest("POST", base+"api/v2/feishu/forward", strings.NewReader(body))
	req.Header.Set("Origin", "https://untrusted-origin.invalid")
	req = req.WithContext(context.WithValue(req.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 7, Username: "分享者"}}))
	rec := httptest.NewRecorder()
	http.StripPrefix(strings.TrimSuffix(base, "/"), genapi.Handler(s)).ServeHTTP(rec, req)
	var response struct {
		Success int `json:"success_count"`
		Fail    int `json:"fail_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || len(sends) != 2 || response.Success != 2 || response.Fail != 0 {
		t.Fatalf("forward status=%d sends=%d body=%s", rec.Code, len(sends), rec.Body.String())
	}
	cards := []string{}
	recipients := map[string]int{}
	for len(sends) > 0 {
		sent := <-sends
		recipients[sent.ReceiveID]++
		var card struct {
			Elements []struct {
				Actions []struct {
					URL string `json:"url"`
				} `json:"actions"`
			} `json:"elements"`
		}
		if err := json.Unmarshal([]byte(sent.Content), &card); err != nil {
			t.Fatal(err)
		}
		var urls []string
		for _, e := range card.Elements {
			for _, a := range e.Actions {
				urls = append(urls, a.URL)
			}
		}
		if !reflect.DeepEqual(urls, []string{"https://studio.example" + base + "share/token"}) {
			t.Errorf("card URL=%v", urls)
		}
		cards = append(cards, sent.Content)
	}
	if recipients["ou_1"] != 1 || recipients["oc_1"] != 1 {
		t.Fatalf("recipient send counts: %v", recipients)
	}
	return cards
}

func TestCombinedShareInlineResultTitleNeverUsesConversation(t *testing.T) {
	for _, base := range []string{"/", "/xiaoan-platform/", "/other/nested/"} {
		t.Run(base, func(t *testing.T) {
			cfg := combinedShareConfig(t, base)
			for _, title := range []string{"Daily health check", ""} {
				name := title
				if name == "" {
					name = "neutral-fallback"
				}
				t.Run(name, func(t *testing.T) {
					s, m := combinedShareServer(t)
					s.Config = cfg
					content := "CURRENT-RUN-ONLY"
					raw, err := json.Marshal([]sharing.Entry{{Content: &content, Role: "assistant", Title: title, CreatedAt: time.Now()}})
					if err != nil {
						t.Fatal(err)
					}
					expectPreviewShare(m, string(raw), 7, nil)
					m.ExpectQuery("GetConversationByID").WithArgs(9).WillReturnRows(automationHandlerRow(t, db.Conversation{ID: 9, UserID: 7, Title: "CONFIDENTIAL prior customer incident", CreatedAt: time.Now(), UpdatedAt: time.Now()}))
					// Inline results never require messages from the reused conversation.
					for i, rawCard := range combinedForwardCards(t, s, base) {
						var card struct {
							Header struct {
								Title struct {
									Content string `json:"content"`
								} `json:"title"`
							} `json:"header"`
						}
						if err := json.Unmarshal([]byte(rawCard), &card); err != nil {
							t.Fatal(err)
						}
						if strings.Contains(rawCard, "CONFIDENTIAL prior customer incident") {
							t.Errorf("target %d: inline result leaked reused conversation title: %q", i, card.Header.Title.Content)
						}
						if title != "" && !strings.Contains(card.Header.Title.Content, title) {
							t.Errorf("target %d: header=%q, want pinned title %q", i, card.Header.Title.Content, title)
						}
						if title == "" && strings.TrimSpace(card.Header.Title.Content) == "" {
							t.Errorf("target %d: missing neutral fallback title", i)
						}
						if !strings.Contains(rawCard, content) {
							t.Errorf("target %d: inline result omitted", i)
						}
					}
				})
			}
		})
	}
}

func TestCombinedShareSelectedMessagesAndArtifactCapability(t *testing.T) {
	for _, base := range []string{"/", "/xiaoan-platform/", "/other/nested/"} {
		t.Run(base, func(t *testing.T) {
			cfg := combinedShareConfig(t, base)
			s, m := combinedShareServer(t)
			s.Config = cfg
			now := time.Now()
			artID, foreignArtID, runID := ids.New(), ids.New(), ids.New()
			answer := "选中回答\n" + strings.Repeat("完整人工分享内容", 1000) + "\nSELECTED-ANSWER-END"
			entries := []sharing.Entry{{ID: 2}, {ID: 3, Artifacts: []sharing.ArtifactRef{{ArtifactID: artID.String(), Name: "selected.pdf"}}}}
			raw, err := json.Marshal(entries)
			if err != nil {
				t.Fatal(err)
			}
			messages := func() {
				m.ExpectQuery("ListMessagesByConversation").WithArgs(9).WillReturnRows(sqlmock.NewRows([]string{"id", "conversation_id", "role", "content", "metadata", "created_at"}).
					AddRow(1, 9, "user", "PRIVATE-HISTORY-BEFORE", nil, now).
					AddRow(2, 9, "user", "选中问题", nil, now).
					AddRow(3, 9, "assistant", answer, nil, now).
					AddRow(4, 9, "assistant", "PRIVATE-HISTORY-AFTER", nil, now))
			}
			expectPreviewShare(m, string(raw), 7, nil)
			expectPreviewConversation(m)
			messages()
			for _, card := range combinedForwardCards(t, s, base) {
				if !strings.Contains(card, "选中问题") || !strings.Contains(card, "选中回答") || strings.Contains(card, "PRIVATE-HISTORY") || strings.Contains(card, "SELECTED-ANSWER-END") || len(card) > 12000 {
					t.Fatal("manual card selection/preview bounds violated")
				}
			}
			router := http.StripPrefix(strings.TrimSuffix(base, "/"), genapi.Handler(s))
			get := func(path string) *httptest.ResponseRecorder {
				r := httptest.NewRecorder()
				router.ServeHTTP(r, httptest.NewRequest("GET", base+"api/v2/public/shares/token"+path, nil))
				return r
			}
			expectPreviewShare(m, string(raw), 7, nil)
			expectPreviewConversation(m)
			messages()
			rec := get("")
			var full struct {
				Messages []sharing.Message `json:"messages"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 200 || len(full.Messages) != 2 || full.Messages[0].Content != "选中问题" || full.Messages[1].Content != answer || !reflect.DeepEqual(full.Messages[1].Artifacts, entries[1].Artifacts) || strings.Contains(rec.Body.String(), "PRIVATE-HISTORY") {
				t.Fatalf("manual public selection/full content changed: status=%d", rec.Code)
			}
			// Allowed metadata resolves to a redirect without fetching external bytes.
			artifact := db.RunArtifact{ID: artID.Bytes(), RunID: runID.Bytes(), Name: "selected.pdf", CachedExternalUrl: sql.NullString{String: "https://files.invalid/selected.pdf", Valid: true}, CachedUrlExpiresAt: sql.NullTime{Time: now.Add(time.Hour), Valid: true}, CreatedAt: now, UpdatedAt: now}
			run := db.Run{ID: runID.Bytes(), Status: "succeeded", UserID: sql.NullInt64{Int64: 7, Valid: true}, ConversationID: sql.NullInt64{Int64: 9, Valid: true}, CreatedAt: now, UpdatedAt: now, QueuedAt: now}
			expectPreviewShare(m, string(raw), 7, nil)
			m.ExpectQuery("GetRunArtifactByID").WithArgs(artID.Bytes()).WillReturnRows(automationHandlerRow(t, artifact))
			m.ExpectQuery("GetRunByID").WithArgs(runID.Bytes()).WillReturnRows(automationHandlerRow(t, run))
			rec = get("/artifacts/" + artID.String() + "/open")
			if rec.Code != 302 || rec.Header().Get("Location") != artifact.CachedExternalUrl.String {
				t.Fatalf("allowed artifact: %d %s", rec.Code, rec.Body.String())
			}
			// A query sentinel makes forbidden extra reads fail loudly. Without
			// it, sqlmock's unexpected-query error could itself produce a 404,
			// concealing a removed capability/revocation guard.
			denied := func(path string) {
				t.Helper()
				m.ExpectQuery("^SELECT 'combined_read_boundary'$").WillReturnRows(sqlmock.NewRows([]string{"boundary"}).AddRow("ok"))
				rec := get(path)
				if rec.Code != 404 || rec.Header().Get("Location") != "" {
					t.Errorf("denied share %s=%d", path, rec.Code)
				}
				var boundary string
				if err := s.DB.QueryRow("SELECT 'combined_read_boundary'").Scan(&boundary); err != nil {
					t.Fatal(err)
				}
			}
			// An artifact from an unselected turn is forbidden before any artifact read.
			expectPreviewShare(m, string(raw), 7, nil)
			denied("/artifacts/" + foreignArtID.String() + "/open")
			// Revocation must block even the previously allowed artifact and full page.
			for _, path := range []string{"", "/artifacts/" + artID.String() + "/open"} {
				expectPreviewShare(m, string(raw), 7, now)
				denied(path)
			}
		})
	}
}
