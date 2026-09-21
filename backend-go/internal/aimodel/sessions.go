package aimodel

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/google/uuid"
)

func (s *Service) ownedSession(ctx context.Context, owner int64, id string) (*Session, error) {
	if owner <= 0 || !validID(id) {
		return nil, ErrNotFound
	}
	row, e := s.Repo.q().GetAITestSession(ctx, db.GetAITestSessionParams{ID: id, OwnerID: owner, ExpiresAt: s.now()})
	if e != nil {
		return nil, dbError(e)
	}
	return sessionFrom(row), nil
}
func (s *Service) lockSession(ctx context.Context, q *db.Queries, owner int64, id string) (*Session, error) {
	if owner <= 0 || !validID(id) {
		return nil, ErrNotFound
	}
	row, e := q.LockAITestSession(ctx, db.LockAITestSessionParams{ID: id, OwnerID: owner, ExpiresAt: s.now()})
	if e != nil {
		return nil, dbError(e)
	}
	return sessionFrom(row), nil
}
func (s *Service) ListSessions(ctx context.Context, owner int64, modelID string) ([]Session, error) {
	if owner <= 0 {
		return nil, ErrNotFound
	}
	if modelID != "" && !validID(modelID) {
		return nil, invalid("model_id")
	}
	rows, e := s.Repo.q().ListAITestSessions(ctx, db.ListAITestSessionsParams{OwnerID: owner, ExpiresAt: s.now()})
	if e != nil {
		return nil, dbError(e)
	}
	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		if modelID == "" || r.ModelID == modelID {
			out = append(out, *sessionFrom(r))
		}
	}
	return out, nil
}
func (s *Service) CreateSession(ctx context.Context, owner int64, modelID, title string) (*Session, error) {
	if owner <= 0 || !validID(modelID) || !validText(title, 255) {
		return nil, invalid("session")
	}
	out := &Session{ID: uuid.NewString(), OwnerID: owner, ModelID: modelID, Title: strings.TrimSpace(title), CreatedAt: s.now(), ExpiresAt: s.now().Add(s.opts.SessionTTL)}
	err := s.Repo.transaction(ctx, func(q *db.Queries) error {
		if e := q.EnsureAITestOwner(ctx, owner); e != nil {
			return e
		}
		if _, e := q.LockAITestOwner(ctx, owner); e != nil {
			return e
		}
		n, e := q.CountAITestSessions(ctx, db.CountAITestSessionsParams{OwnerID: owner, ExpiresAt: s.now()})
		if e != nil {
			return e
		}
		if n >= int64(s.opts.MaxSessionsPerUser) {
			return ErrLimit
		}
		row, e := q.LockAIModel(ctx, modelID)
		if e != nil {
			return e
		}
		if !row.Enabled {
			return ErrUnavailable
		}
		if row.ExecutionLocation != "server_remote" {
			return invalid("browser OCR does not create server sessions")
		}
		return q.CreateAITestSession(ctx, db.CreateAITestSessionParams{ID: out.ID, OwnerID: owner, ModelID: modelID, Title: out.Title, CreatedAt: out.CreatedAt, ExpiresAt: out.ExpiresAt})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) GetSession(ctx context.Context, owner int64, id string) (*SessionDetail, error) {
	session, e := s.ownedSession(ctx, owner, id)
	if e != nil {
		return nil, e
	}
	messages, e := s.listMessages(ctx, s.Repo.q(), id)
	if e != nil {
		return nil, dbError(e)
	}
	return &SessionDetail{Session: *session, Messages: messages}, nil
}
func (s *Service) ListMessages(ctx context.Context, owner int64, sessionID string) ([]Message, error) {
	if _, e := s.ownedSession(ctx, owner, sessionID); e != nil {
		return nil, e
	}
	m, e := s.listMessages(ctx, s.Repo.q(), sessionID)
	return m, dbError(e)
}
func (s *Service) listMessages(ctx context.Context, q *db.Queries, sessionID string) ([]Message, error) {
	rows, e := q.ListAITestMessages(ctx, sessionID)
	if e != nil {
		return nil, e
	}
	attachments, e := q.ListAITestAttachments(ctx, sessionID)
	if e != nil {
		return nil, e
	}
	byMessage := map[string][]Attachment{}
	for _, a := range attachments {
		if a.MessageID.Valid {
			byMessage[a.MessageID.String] = append(byMessage[a.MessageID.String], *attachmentFrom(a))
		}
	}
	out := make([]Message, 0, len(rows))
	for _, r := range rows {
		aa := byMessage[r.ID]
		if aa == nil {
			aa = []Attachment{}
		}
		out = append(out, Message{ID: r.ID, Role: r.Role, Text: r.Text, Attachments: aa, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// DeleteSession commits an inaccessible tombstone BEFORE touching object storage.
// A failed object deletion or final DB commit therefore never resurrects a readable
// session with missing files. The maintenance entry point retries the tombstone.
func (s *Service) DeleteSession(ctx context.Context, owner int64, id string) error {
	err := s.Repo.transaction(ctx, func(q *db.Queries) error {
		session, e := s.lockSession(ctx, q, owner, id)
		if e != nil {
			return e
		}
		if session.ActiveInvocationID != "" {
			return ErrInUse
		}
		return affected(q.MarkAITestSessionDeleting(ctx, db.MarkAITestSessionDeletingParams{ID: id, CleanupAfter: sql.NullTime{Time: s.now().Add(5 * time.Minute), Valid: true}}))
	})
	if err != nil {
		return err
	}
	return s.cleanupClaimedSession(ctx, id)
}

// cleanupClaimedSession is called only after a committed deleting marker/lease.
// DeleteObject must be idempotent: a missing object is already successfully deleted.
func (s *Service) cleanupClaimedSession(ctx context.Context, id string) error {
	row, err := s.Repo.q().GetAITestSessionInternal(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return dbError(err)
	}
	if !row.Deleting || row.ActiveInvocationID.Valid {
		return ErrInUse
	}
	fail := func(cause error) error {
		delay := cleanupRetryDelay(row.CleanupAttempts)
		// If the caller context has expired, the committed five-minute lease still
		// bounds retry delay; never erase the durable tombstone on scheduling failure.
		_, scheduleErr := s.Repo.q().RetryAITestSessionCleanup(ctx, db.RetryAITestSessionCleanupParams{ID: id, CleanupAfter: sql.NullTime{Time: s.now().Add(delay), Valid: true}})
		return errors.Join(dbError(cause), dbError(scheduleErr))
	}
	attachments, err := s.Repo.q().ListAITestAttachments(ctx, id)
	if err != nil {
		return fail(err)
	}
	if len(attachments) > 0 && s.opts.DeleteObject == nil {
		return fail(ErrUnavailable)
	}
	for _, attachment := range attachments {
		if err = s.opts.DeleteObject(ctx, attachment.StorageKey); err != nil {
			return fail(ErrUnavailable)
		}
	}
	err = s.Repo.transaction(ctx, func(q *db.Queries) error {
		current, e := q.LockAITestSessionInternal(ctx, id)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if !current.Deleting || current.ActiveInvocationID.Valid {
			return ErrInUse
		}
		if e = q.DeleteAITestSessionAttachments(ctx, id); e != nil {
			return e
		}
		return affected(q.DeleteAITestSession(ctx, id))
	})
	if err != nil {
		return fail(err)
	}
	return nil
}

// CleanupExpired uses a durable lease and exponential retry schedule. Unprocessed
// expired rows sort before failed rows scheduled into the future, so a batch of
// persistent object-store failures cannot permanently starve later sessions.
// All independent rows in the bounded batch are attempted, with sanitized aggregate errors.
func (s *Service) CleanupExpired(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, invalid("cleanup limit")
	}
	now := s.now()
	rows, err := s.Repo.q().ListExpiredAITestSessions(ctx, db.ListExpiredAITestSessionsParams{ExpiryCutoff: now, RetryCutoff: sql.NullTime{Time: now, Valid: true}, Limit: int32(limit)})
	if err != nil {
		return 0, dbError(err)
	}
	count := 0
	var failures []error
	for _, row := range rows {
		claimed := false
		err = s.Repo.transaction(ctx, func(q *db.Queries) error {
			current, e := q.LockAITestSessionInternal(ctx, row.ID)
			if errors.Is(e, sql.ErrNoRows) {
				return nil
			}
			if e != nil {
				return e
			}
			if current.ActiveInvocationID.Valid || (!current.Deleting && current.ExpiresAt.After(s.now())) || (current.CleanupAfter.Valid && current.CleanupAfter.Time.After(s.now())) {
				return nil
			}
			if e = affected(q.MarkAITestSessionDeleting(ctx, db.MarkAITestSessionDeletingParams{ID: row.ID, CleanupAfter: sql.NullTime{Time: s.now().Add(5 * time.Minute), Valid: true}})); e != nil {
				return e
			}
			claimed = true
			return nil
		})
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !claimed {
			continue
		}
		if err = s.cleanupClaimedSession(ctx, row.ID); err != nil {
			failures = append(failures, err)
			continue
		}
		count++
	}
	if err := s.cleanupObjects(ctx, limit); err != nil {
		failures = append(failures, err)
	}
	return count, errors.Join(failures...)
}
