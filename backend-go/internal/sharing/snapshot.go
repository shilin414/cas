// Package sharing owns the persisted read-only snapshot contract shared by
// manual conversation forwarding and automatic result delivery.
package sharing

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
)

type ArtifactRef struct {
	ArtifactID string `json:"artifact_id"`
	Name       string `json:"name"`
}
type Entry struct {
	AgentName      string        `json:"agent_name,omitempty"`
	AgentIcon      string        `json:"agent_icon,omitempty"`
	AgentAvatarKey string        `json:"agent_avatar_key,omitempty"`
	ID             int64         `json:"id,omitempty"`
	Artifacts      []ArtifactRef `json:"artifacts,omitempty"`
	// Inline content is written only by the delivery service. Older manual
	// shares keep resolving selected IDs, preserving their existing behavior.
	Content   *string   `json:"content,omitempty"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	Title     string    `json:"title,omitempty"`
}
type Message struct {
	AgentName      string        `json:"agent_name,omitempty"`
	AgentIcon      string        `json:"agent_icon,omitempty"`
	AgentAvatarKey string        `json:"-"`
	Role           string        `json:"role"`
	Content        string        `json:"content"`
	CreatedAt      time.Time     `json:"created_at"`
	Artifacts      []ArtifactRef `json:"artifacts,omitempty"`
}

func Resolve(entries []Entry, messages []db.Message) []Message {
	byID := make(map[uint64]db.Message, len(messages))
	for _, m := range messages {
		byID[m.ID] = m
	}
	out := make([]Message, 0, len(entries))
	for _, e := range entries {
		if e.Content != nil {
			out = append(out, Message{Role: e.Role, Content: *e.Content, CreatedAt: e.CreatedAt, Artifacts: e.Artifacts, AgentName: e.AgentName, AgentIcon: e.AgentIcon, AgentAvatarKey: e.AgentAvatarKey})
			continue
		}
		if m, ok := byID[uint64(e.ID)]; ok {
			out = append(out, Message{Role: m.Role, Content: m.Content, CreatedAt: m.CreatedAt, Artifacts: e.Artifacts, AgentName: e.AgentName, AgentIcon: e.AgentIcon, AgentAvatarKey: e.AgentAvatarKey})
		}
	}
	return out
}

// SnapshotTitle keeps inline task results independent of the mutable surrounding
// conversation, including when an older result snapshot has no pinned title.
func SnapshotTitle(entries []Entry, conversationTitle string) string {
	for _, entry := range entries {
		if entry.Content != nil {
			if strings.TrimSpace(entry.Title) != "" {
				return entry.Title
			}
			return "自动化执行结果"
		}
	}
	return conversationTitle
}

func NeedsMessages(entries []Entry) bool {
	for _, e := range entries {
		if e.Content == nil && e.ID > 0 {
			return true
		}
	}
	return false
}
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
func URL(base, token string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("PUBLIC_BASE_URL must be an absolute HTTP(S) site URL")
	}
	return strings.TrimRight(u.String(), "/") + "/share/" + url.PathEscape(token), nil
}
