package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/execution"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/sharing"
	"github.com/google/uuid"
)

func previewServer(t *testing.T) (*Server, sqlmock.Sqlmock) {
	t.Helper()
	d, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		d.Close()
	})
	return &Server{Runs: execution.NewService(d, nil, nil, nil), FeishuAuth: fakeFeishuTokenResolver{token: "uat"}}, m
}
func expectPreviewShare(m sqlmock.Sqlmock, snapshot string, owner int64, revoked any) {
	m.ExpectQuery("FROM conversation_shares WHERE token").WithArgs("token").WillReturnRows(sqlmock.NewRows([]string{"id", "token", "conversation_id", "user_id", "snapshot", "created_at", "revoked_at"}).AddRow(1, "token", 9, owner, snapshot, time.Now(), revoked))
}
func expectPreviewConversation(m sqlmock.Sqlmock) {
	m.ExpectQuery("FROM conversations WHERE id").WithArgs(9).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "application_id", "organization_id", "title", "created_at", "updated_at"}).AddRow(9, 7, nil, nil, "共享历史会话", time.Now(), time.Now()))
}
func expectPreviewMessages(m sqlmock.Sqlmock) {
	m.ExpectQuery("FROM messages WHERE conversation_id").WithArgs(9).WillReturnRows(sqlmock.NewRows([]string{"id", "conversation_id", "role", "content", "metadata", "created_at"}).AddRow(1, 9, "user", "历史隐私绝不能泄漏", nil, time.Now()).AddRow(2, 9, "user", "选中问题", nil, time.Now()).AddRow(3, 9, "assistant", "选中回答", nil, time.Now()).AddRow(4, 9, "assistant", "未来隐私绝不能泄漏", nil, time.Now()))
}
func forwardPreview(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"share_token":"token","targets":[{"target_type":"user","id":"ou_1"},{"target_type":"chat","id":"oc_1"}]}`
	req := httptest.NewRequest("POST", "/api/v2/feishu/forward", strings.NewReader(body))
	req.Header.Set("Origin", "https://studio.example")
	req = req.WithContext(context.WithValue(req.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 7, Username: "分享者"}}))
	w := httptest.NewRecorder()
	s.ForwardShareToFeishu(w, req)
	return w
}
func TestForwardPreviewUsesSelectedMessagesForUserAndChat(t *testing.T) {
	s, m := previewServer(t)
	expectPreviewShare(m, `[{"id":2},{"id":3}]`, 7, nil)
	expectPreviewConversation(m)
	expectPreviewMessages(m)
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			MsgType   string `json:"msg_type"`
			Content   string `json:"content"`
			ReceiveID string `json:"receive_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.MsgType != "interactive" {
			t.Errorf("wrong type: %s", body.MsgType)
		}
		for _, part := range []string{"选中问题", "选中回答", "https://studio.example/share/token", "查看完整对话"} {
			if !strings.Contains(body.Content, part) {
				t.Errorf("missing %s", part)
			}
		}
		if strings.Contains(body.Content, "绝不能泄漏") {
			t.Error("leaked unselected messages")
		}
		if calls == 1 && (r.URL.Query().Get("receive_id_type") != "open_id" || body.ReceiveID != "ou_1") {
			t.Error("wrong user route")
		}
		if calls == 2 && (r.URL.Query().Get("receive_id_type") != "chat_id" || body.ReceiveID != "oc_1") {
			t.Error("wrong chat route")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer provider.Close()
	s.Feishu = identity.NewFeishuClient(provider.URL, "id", "secret", provider.Client())
	w := forwardPreview(t, s)
	if w.Code != 200 || calls != 2 {
		t.Fatalf("status=%d sends=%d body=%s", w.Code, calls, w.Body.String())
	}
}
func TestForwardPreviewRejectsForeignRevokedCorruptAndReadFailure(t *testing.T) {
	for _, scenario := range []string{"foreign", "revoked", "corrupt", "read_failed"} {
		t.Run(scenario, func(t *testing.T) {
			s, m := previewServer(t)
			owner := int64(7)
			var revoked any
			raw := `[{"id":2}]`
			if scenario == "foreign" {
				owner = 8
			}
			if scenario == "revoked" {
				revoked = time.Now()
			}
			if scenario == "corrupt" {
				raw = `invalid`
			}
			expectPreviewShare(m, raw, owner, revoked)
			if scenario == "corrupt" || scenario == "read_failed" {
				expectPreviewConversation(m)
			}
			if scenario == "read_failed" {
				m.ExpectQuery("FROM messages WHERE conversation_id").WillReturnError(sql.ErrConnDone)
			}
			w := forwardPreview(t, s)
			if w.Code != 400 && w.Code != 500 {
				t.Fatalf("must fail before send: %d", w.Code)
			}
		})
	}
}
func TestPublicResultSnapshotDoesNotReadConversationHistory(t *testing.T) {
	s, m := previewServer(t)
	answer := strings.Repeat("完整结果", 1500)
	raw, _ := json.Marshal([]sharing.Entry{{Content: &answer, Role: "assistant", Title: "每日巡检", CreatedAt: time.Now()}})
	expectPreviewShare(m, string(raw), 7, nil)
	expectPreviewConversation(m)
	// No messages query is allowed: even a reused conversation cannot leak.
	w := httptest.NewRecorder()
	s.GetPublicShare(w, httptest.NewRequest("GET", "/share/token", nil), "token")
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Title    string            `json:"title"`
		Messages []sharing.Message `json:"messages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Title != "每日巡检" || len(body.Messages) != 1 || body.Messages[0].Content != answer {
		t.Fatal("full result changed or truncated")
	}
}
func TestPublicLegacyShareStillFiltersSelectedIDs(t *testing.T) {
	s, m := previewServer(t)
	expectPreviewShare(m, `[{"id":2},{"id":3}]`, 7, nil)
	expectPreviewConversation(m)
	expectPreviewMessages(m)
	w := httptest.NewRecorder()
	s.GetPublicShare(w, httptest.NewRequest("GET", "/share/token", nil), "token")
	if w.Code != 200 || strings.Contains(w.Body.String(), "绝不能泄漏") || !strings.Contains(w.Body.String(), "选中回答") {
		t.Fatalf("bad legacy share: %s", w.Body.String())
	}
}
func TestResultSnapshotRejectsUnsharedArtifact(t *testing.T) {
	s, m := previewServer(t)
	expectPreviewShare(m, `[{"content":"result","role":"assistant","artifacts":[]}]`, 7, nil)
	w := httptest.NewRecorder()
	s.OpenPublicShareArtifact(w, httptest.NewRequest("GET", "/artifact", nil), "token", uuid.New())
	if w.Code != 404 {
		t.Fatalf("unshared artifact accessible: %d", w.Code)
	}
}
