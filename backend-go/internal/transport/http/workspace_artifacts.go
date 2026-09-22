package http

import (
	"encoding/base64"
	"encoding/json"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Query only persisted output artifacts; attachments live in a different table.
// Both ownership checks are intentional: staff also see only their own workspace.
const workspaceArtifactSQL = `SELECT a.id, a.name, a.normalized_type, a.created_at, c.id, c.title
 FROM run_artifacts a JOIN runs r ON r.id = a.run_id JOIN conversations c ON c.id = r.conversation_id
 WHERE r.user_id = ? AND c.user_id = ? AND r.application_id = ?
 AND (? = '' OR a.name COLLATE utf8mb4_unicode_ci LIKE ?)
 AND (? IS NULL OR a.created_at < ? OR (a.created_at = ? AND a.id < ?))
 ORDER BY a.created_at DESC, a.id DESC LIMIT ?`

type workspaceArtifactCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}
type workspaceArtifact struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	NormalizedType string    `json:"normalized_type"`
	CreatedAt      time.Time `json:"created_at"`
	ConversationID string    `json:"conversation_id"`
	TaskTitle      string    `json:"task_title"`
}

func (s *Server) ListWorkspaceArtifacts(w http.ResponseWriter, r *http.Request, p genapi.ListWorkspaceArtifactsParams) {
	w.Header().Set("Cache-Control", "no-store")
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, 401, "not authenticated")
		return
	}
	limit := 20
	if p.Limit != nil {
		limit = *p.Limit
	}
	query := ""
	if p.Q != nil {
		query = strings.TrimSpace(*p.Q)
	}
	if p.ApplicationId <= 0 || limit < 1 || limit > 50 || utf8.RuneCountInString(query) > 200 {
		writeDetail(w, 400, "invalid query")
		return
	}
	var cursorDate any
	var cursorID []byte
	if p.Cursor != nil && *p.Cursor != "" {
		var cursor workspaceArtifactCursor
		raw, err := base64.RawURLEncoding.DecodeString(*p.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.CreatedAt.IsZero() {
			writeDetail(w, 400, "invalid cursor")
			return
		}
		id, err := ids.Parse(cursor.ID)
		if err != nil {
			writeDetail(w, 400, "invalid cursor")
			return
		}
		cursorDate = cursor.CreatedAt
		cursorID = id.Bytes()
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := s.DB.QueryContext(r.Context(), workspaceArtifactSQL, caller.ID, caller.ID, p.ApplicationId, query, "%"+escaped+"%", cursorDate, cursorDate, cursorDate, cursorID, limit+1)
	if err != nil {
		writeDetail(w, 500, "resource query failed")
		return
	}
	defer rows.Close()
	items := make([]workspaceArtifact, 0, limit+1)
	for rows.Next() {
		var item workspaceArtifact
		var id []byte
		var conversationID int64
		if err := rows.Scan(&id, &item.Name, &item.NormalizedType, &item.CreatedAt, &conversationID, &item.TaskTitle); err != nil {
			writeDetail(w, 500, "resource query failed")
			return
		}
		item.ID = idsMustString(id)
		item.ConversationID = strconv.FormatInt(conversationID, 10)
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeDetail(w, 500, "resource query failed")
		return
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		raw, _ := json.Marshal(workspaceArtifactCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}
