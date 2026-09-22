package http

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/sharing"
)

type pinnedAgentSnapshot struct{}

func (pinnedAgentSnapshot) Match(value driver.Value) bool {
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return false
	}
	var entries []sharing.Entry
	return json.Unmarshal(raw, &entries) == nil && len(entries) == 1 && entries[0].ID == 3 && entries[0].AgentName == "创作助手" && entries[0].AgentIcon == "✨" && entries[0].AgentAvatarKey == "application-avatars/12/pinned.png"
}
func TestCreateSharePinsSelectedAssistantIdentity(t *testing.T) {
	s, m := previewServer(t)
	m.ExpectQuery("GetConversationByID").WithArgs(uint64(9)).WillReturnRows(automationHandlerRow(t, db.Conversation{ID: 9, UserID: 7, ApplicationID: sql.NullInt64{Int64: 12, Valid: true}}))
	m.ExpectQuery("ListMessagesByConversation").WithArgs(uint64(9)).WillReturnRows(automationHandlerRow(t, db.Message{ID: 3, ConversationID: 9, Role: "assistant", Content: "final"}))
	m.ExpectQuery("GetApplicationByID").WithArgs(uint64(12)).WillReturnRows(automationHandlerRow(t, db.GetApplicationByIDRow{ID: 12, Name: "创作助手", Icon: "✨", AvatarKey: "application-avatars/12/pinned.png"}))
	m.ExpectExec("CreateConversationShare").WithArgs(sqlmock.AnyArg(), uint64(9), uint64(7), pinnedAgentSnapshot{}).WillReturnResult(sqlmock.NewResult(1, 1))
	req := httptest.NewRequest("POST", "/shares", strings.NewReader(`{"message_ids":[3]}`))
	req = req.WithContext(context.WithValue(req.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 7}}))
	response := httptest.NewRecorder()
	s.CreateConversationShare(response, req, 9)
	if response.Code != 200 {
		t.Fatalf("%d: %s", response.Code, response.Body.String())
	}
}

func TestUserOnlyShareDoesNotClaimAnAgentAuthor(t *testing.T) {
	card := feishuShareCard("分享者", "问题", 1, "https://example.test/share/a", []sharing.Message{{Role: "user", Content: "问题正文", AgentName: "不可误用的名称", AgentIcon: "✨"}})
	raw, _ := json.Marshal(card)
	if strings.Contains(string(raw), "不可误用") || strings.Contains(string(raw), "智能体") {
		t.Fatalf("user attributed to agent: %s", raw)
	}
}
