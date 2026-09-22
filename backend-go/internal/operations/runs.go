package operations

import (
	"context"
	"database/sql"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"strconv"
	"strings"
	"time"
)

// This projection deliberately excludes prompt/output/error_message and secrets.
const runProjection = `SELECT r.id, r.provider, r.runtime_type, r.status, r.priority, r.trigger_type,
 r.user_id, r.application_id, r.conversation_id, r.queued_at, r.available_at,
 r.started_at, r.finished_at, r.created_at, r.attempt, r.max_attempts, r.error_code,
 COALESCE(p.status,'missing')
 FROM runs r LEFT JOIN providers p ON p.provider_key=r.provider`

func (s *Service) ListRuns(ctx context.Context, f RunFilter) (*RunPage, error) {
	f, err := normalizeFilter(f)
	if err != nil {
		return nil, err
	}
	leave, err := s.enter(ctx, false)
	if err != nil {
		return nil, err
	}
	defer leave()
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	now, err := db.New(s.DB).CurrentDBTime(ctx)
	if err != nil {
		return nil, err
	}
	query, args, err := runQuery(f)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Run, 0, f.Limit+1)
	for rows.Next() {
		var r Run
		var id ids.ID
		var owner, app, conversation sql.NullInt64
		var available, started, finished sql.NullTime
		var providerStatus string
		if err := rows.Scan(&id, &r.Provider, &r.RuntimeType, &r.Status, &r.Priority, &r.TriggerType, &owner, &app, &conversation, &r.QueuedAt, &available, &started, &finished, &r.CreatedAt, &r.Attempt, &r.MaxAttempts, &r.ErrorCode, &providerStatus); err != nil {
			return nil, err
		}
		r.ID = id.String()
		r.OwnerUserID = stringID(owner)
		r.ApplicationID = stringID(app)
		r.ConversationID = stringID(conversation)
		r.QueuedAt = r.QueuedAt.UTC()
		r.CreatedAt = r.CreatedAt.UTC()
		r.AvailableAt = nullTime(available)
		r.StartedAt = nullTime(started)
		r.FinishedAt = nullTime(finished)
		if r.Status == "queued" {
			age := ageSeconds(now, r.QueuedAt)
			r.QueueAgeSeconds = &age
			switch {
			case providerStatus == "missing":
				r.WaitReason = "provider_unavailable"
			case providerStatus != "active":
				r.WaitReason = "provider_paused"
			case available.Valid && available.Time.After(now):
				r.WaitReason = "deferred"
			default:
				r.WaitReason = "queued"
			}
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := &RunPage{Results: items}
	if len(items) > f.Limit {
		out.Results = items[:f.Limit]
		last := out.Results[len(out.Results)-1]
		cursor := encodeCursor(last.CreatedAt, last.ID, f)
		out.NextCursor = &cursor
	}
	return out, nil
}
func runQuery(f RunFilter) (string, []any, error) {
	clauses := []string{}
	args := []any{}
	switch f.Status {
	case "active":
		clauses = append(clauses, "r.status IN "+activeStatesSQL)
	case "all":
	default:
		clauses = append(clauses, "r.status=?")
		args = append(args, f.Status)
	}
	if f.Provider != "" {
		clauses = append(clauses, "r.provider=?")
		args = append(args, f.Provider)
	}
	for _, item := range []struct{ column, value string }{{"r.user_id", f.OwnerUserID}, {"r.application_id", f.ApplicationID}} {
		if item.value != "" {
			n, err := strconv.ParseInt(item.value, 10, 64)
			if err != nil {
				return "", nil, ErrInvalid
			}
			clauses = append(clauses, item.column+"=?")
			args = append(args, n)
		}
	}
	if f.Cursor != "" {
		cursor, err := decodeCursor(f.Cursor, f)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, "(r.created_at < ? OR (r.created_at = ? AND r.id < ?))")
		args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID.Bytes())
	}
	query := runProjection
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY r.created_at DESC, r.id DESC LIMIT ?"
	args = append(args, f.Limit+1)
	return query, args, nil
}
func stringID(v sql.NullInt64) *string {
	if !v.Valid {
		return nil
	}
	s := strconv.FormatInt(v.Int64, 10)
	return &s
}
func nullTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	at := v.Time.UTC()
	return &at
}
