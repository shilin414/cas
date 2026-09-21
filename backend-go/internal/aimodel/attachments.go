package aimodel

import (
	"context"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/google/uuid"
)

func (s *Service) AddAttachment(ctx context.Context, owner int64, sessionID string, in AttachmentInput) (*Attachment, error) {
	if e := validateAttachment(in); e != nil {
		return nil, e
	}
	out := &Attachment{ID: uuid.NewString(), SessionID: sessionID, AttachmentInput: in, CreatedAt: s.now()}
	e := s.Repo.transaction(ctx, func(q *db.Queries) error {
		session, e := s.lockSession(ctx, q, owner, sessionID)
		if e != nil {
			return e
		}
		if session.ActiveInvocationID != "" {
			return ErrInUse
		}
		cfg, e := s.resolveRemote(ctx, q, session.ModelID)
		if e != nil {
			return e
		}
		if !supportsAttachment(cfg, in.Kind) {
			return invalid("unsupported attachment capability")
		}
		rows, e := q.ListAITestAttachments(ctx, sessionID)
		if e != nil {
			return e
		}
		if len(rows) >= s.opts.MaxAttachmentsPerSession {
			return ErrLimit
		}
		total := in.SizeBytes
		for _, a := range rows {
			total += a.SizeBytes
		}
		if total > s.opts.MaxSessionBytes {
			return ErrLimit
		}
		return q.CreateAITestAttachment(ctx, db.CreateAITestAttachmentParams{ID: out.ID, SessionID: sessionID, Name: in.Name, MimeType: in.MIMEType, SizeBytes: in.SizeBytes, Kind: in.Kind, StorageKey: in.StorageKey, CreatedAt: out.CreatedAt})
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}
func supportsAttachment(c *RuntimeConfig, kind string) bool {
	if c == nil || c.Model == nil || c.Connection == nil {
		return false
	}
	switch kind {
	case "image":
		return c.Model.Capabilities.Image
	case "video":
		return c.Model.Capabilities.Video && c.Connection.Adapter == "gemini"
	case "pdf":
		return c.Model.Capabilities.PDF && c.Connection.Adapter == "gemini"
	}
	return false
}
func (s *Service) GetAttachment(ctx context.Context, owner int64, sessionID, id string) (*Attachment, error) {
	if !validID(id) {
		return nil, ErrNotFound
	}
	if _, e := s.ownedSession(ctx, owner, sessionID); e != nil {
		return nil, e
	}
	a, e := s.Repo.q().GetAITestAttachment(ctx, db.GetAITestAttachmentParams{ID: id, SessionID: sessionID})
	if e != nil {
		return nil, dbError(e)
	}
	return attachmentFrom(a), nil
}
func (s *Service) ListAttachments(ctx context.Context, owner int64, sessionID string) ([]Attachment, error) {
	if _, e := s.ownedSession(ctx, owner, sessionID); e != nil {
		return nil, e
	}
	rows, e := s.Repo.q().ListAITestAttachments(ctx, sessionID)
	if e != nil {
		return nil, dbError(e)
	}
	out := make([]Attachment, 0, len(rows))
	for _, r := range rows {
		out = append(out, *attachmentFrom(r))
	}
	return out, nil
}

// DeleteAttachment commits metadata removal and an object-deletion tombstone
// together. Object storage is touched only after commit; failure is retryable.
func (s *Service) DeleteAttachment(ctx context.Context, owner int64, sessionID, id string) error {
	if !validID(id) {
		return ErrNotFound
	}
	var key string
	err := s.Repo.transaction(ctx, func(q *db.Queries) error {
		session, e := s.lockSession(ctx, q, owner, sessionID)
		if e != nil {
			return e
		}
		if session.ActiveInvocationID != "" {
			return ErrInUse
		}
		a, e := q.GetAITestAttachment(ctx, db.GetAITestAttachmentParams{ID: id, SessionID: sessionID})
		if e != nil {
			return e
		}
		if a.MessageID.Valid {
			return ErrInUse
		}
		key = a.StorageKey
		if e = q.CreateAIObjectDeletion(ctx, db.CreateAIObjectDeletionParams{StorageKey: key, CreatedAt: s.now(), CleanupAfter: s.now().Add(5 * time.Minute)}); e != nil {
			return e
		}
		return affected(q.DeleteAITestAttachment(ctx, db.DeleteAITestAttachmentParams{ID: id, SessionID: sessionID}))
	})
	if err != nil {
		return err
	}
	return s.cleanupClaimedObject(ctx, key)
}
