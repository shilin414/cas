package aimodel

import (
	"context"
	"strings"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/google/uuid"
)

type Service struct {
	Repo   *Repo
	opts   Options
	cipher *credentialCipher
	now    func() time.Time
}

func NewService(repo *Repo, opts Options) (*Service, error) {
	if repo == nil || repo.DB == nil {
		return nil, ErrUnavailable
	}
	cipher, err := newCredentialCipher(opts.EncryptionKey)
	if err != nil {
		return nil, err
	}
	opts.EncryptionKey = ""
	if opts.SessionTTL == 0 {
		opts.SessionTTL = 24 * time.Hour
	}
	if opts.SessionTTL < time.Minute || opts.SessionTTL > 30*24*time.Hour {
		return nil, invalid("session TTL")
	}
	if opts.MaxSessionsPerUser == 0 {
		opts.MaxSessionsPerUser = 20
	}
	if opts.MaxSessionsPerUser < 1 || opts.MaxSessionsPerUser > 1000 {
		return nil, invalid("session quota")
	}
	if opts.MaxAttachmentsPerSession == 0 {
		opts.MaxAttachmentsPerSession = 32
	}
	if opts.MaxAttachmentsPerSession < 1 || opts.MaxAttachmentsPerSession > 128 {
		return nil, invalid("attachment quota")
	}
	if opts.MaxSessionBytes == 0 {
		opts.MaxSessionBytes = MaxHistoryBytes
	}
	if opts.MaxSessionBytes < 1 || opts.MaxSessionBytes > MaxHistoryBytes {
		return nil, invalid("session bytes quota")
	}
	return &Service{Repo: repo, opts: opts, cipher: cipher, now: func() time.Time { return time.Now().UTC() }}, nil
}
func (s *Service) ListConnections(ctx context.Context) ([]Connection, error) {
	rows, err := s.Repo.q().ListAIConnections(ctx)
	if err != nil {
		return nil, dbError(err)
	}
	out := make([]Connection, 0, len(rows))
	for _, v := range rows {
		out = append(out, *connectionFrom(v))
	}
	return out, nil
}
func (s *Service) GetConnection(ctx context.Context, id string) (*Connection, error) {
	if !validID(id) {
		return nil, ErrNotFound
	}
	v, e := s.Repo.q().GetAIConnection(ctx, id)
	if e != nil {
		return nil, dbError(e)
	}
	return connectionFrom(v), nil
}
func (s *Service) CreateConnection(ctx context.Context, in ConnectionInput) (*Connection, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := s.validateConnection(in); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	var secret string
	var err error
	if in.Credential != nil {
		secret, err = s.cipher.encrypt(id, *in.Credential)
		if err != nil {
			return nil, err
		}
	}
	now := s.now()
	var out *Connection
	err = s.Repo.transaction(ctx, func(q *db.Queries) error {
		if e := q.CreateAIConnection(ctx, db.CreateAIConnectionParams{ID: id, Name: in.Name, Adapter: in.Adapter, BaseUrl: in.BaseURL, CredentialEnc: secret, Enabled: in.Enabled, TimeoutSeconds: int32(in.TimeoutSeconds), MaxConcurrency: int32(in.MaxConcurrency), CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		fields := []string{"name", "adapter", "base_url", "enabled", "timeout_seconds", "max_concurrency"}
		if in.Credential != nil {
			fields = append(fields, "credential")
		}
		if e := writeAdminAudit(ctx, q, "ai.connection.create", "ai_connection", id, auditDetail{ChangedFields: fields, AfterVersion: 1, EnabledAfter: &in.Enabled}); e != nil {
			return e
		}
		row, e := q.GetAIConnection(ctx, id)
		if e != nil {
			return e
		}
		out = connectionFrom(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) UpdateConnection(ctx context.Context, id string, in ConnectionInput) (*Connection, error) {
	if in.Credential != nil {
		return nil, invalid("use separate credential endpoint")
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := s.validateConnection(in); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, ErrNotFound
	}
	var out *Connection
	err := s.Repo.transaction(ctx, func(q *db.Queries) error {
		row, e := q.LockAIConnection(ctx, id)
		if e != nil {
			return e
		}
		old := connectionFrom(row)
		if in.ExpectedVersion != 0 && in.ExpectedVersion != old.Version {
			return ErrConflict
		}
		n, e := q.UpdateAIConnection(ctx, db.UpdateAIConnectionParams{ID: id, Name: in.Name, Adapter: in.Adapter, BaseUrl: in.BaseURL, Enabled: in.Enabled, TimeoutSeconds: int32(in.TimeoutSeconds), MaxConcurrency: int32(in.MaxConcurrency), Version: old.Version, UpdatedAt: s.now()})
		if e != nil {
			return e
		}
		if n == 0 {
			return ErrConflict
		}
		if e = writeAdminAudit(ctx, q, "ai.connection.update", "ai_connection", id, auditDetail{ChangedFields: connectionChangedFields(old, in), BeforeVersion: old.Version, AfterVersion: old.Version + 1, EnabledBefore: &old.Enabled, EnabledAfter: &in.Enabled}); e != nil {
			return e
		}
		row, e = q.GetAIConnection(ctx, id)
		if e != nil {
			return e
		}
		out = connectionFrom(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) SetConnectionCredential(ctx context.Context, id, value string) error {
	if !validID(id) {
		return ErrNotFound
	}
	if err := validateCredential(value); err != nil {
		return err
	}
	enc, err := s.cipher.encrypt(id, value)
	if err != nil {
		return err
	}
	return s.Repo.transaction(ctx, func(q *db.Queries) error {
		old, e := q.LockAIConnection(ctx, id)
		if e != nil {
			return e
		}
		if e = affected(q.SetAIConnectionCredential(ctx, db.SetAIConnectionCredentialParams{ID: id, CredentialEnc: enc, UpdatedAt: s.now()})); e != nil {
			return e
		}
		action := "ai.connection.credential.replace"
		if value == "" {
			action = "ai.connection.credential.clear"
		}
		return writeAdminAudit(ctx, q, action, "ai_connection", id, auditDetail{ChangedFields: []string{"credential"}, BeforeVersion: old.Version, AfterVersion: old.Version + 1})
	})
}
func (s *Service) DeleteConnection(ctx context.Context, id string) error {
	if !validID(id) {
		return ErrNotFound
	}
	return s.Repo.transaction(ctx, func(q *db.Queries) error {
		old, e := q.LockAIConnection(ctx, id)
		if e != nil {
			return e
		}
		if e = affected(q.DeleteAIConnection(ctx, id)); e != nil {
			return e
		}
		return writeAdminAudit(ctx, q, "ai.connection.delete", "ai_connection", id, auditDetail{BeforeVersion: old.Version, EnabledBefore: &old.Enabled})
	})
}
func (s *Service) ListModels(ctx context.Context) ([]Model, error) {
	rows, err := s.Repo.q().ListAIModels(ctx)
	if err != nil {
		return nil, dbError(err)
	}
	out := make([]Model, 0, len(rows))
	for _, v := range rows {
		m, e := modelFrom(v)
		if e != nil {
			return nil, e
		}
		out = append(out, *m)
	}
	return out, nil
}
func (s *Service) GetModel(ctx context.Context, id string) (*Model, error) {
	if !validID(id) {
		return nil, ErrNotFound
	}
	row, err := s.Repo.q().GetAIModel(ctx, id)
	if err != nil {
		return nil, dbError(err)
	}
	return modelFrom(row)
}
func (s *Service) checkModelConnection(ctx context.Context, in ModelInput) error {
	if in.ConnectionID == nil {
		return nil
	}
	c, e := s.GetConnection(ctx, *in.ConnectionID)
	if e != nil {
		return e
	}
	if c.Adapter == "openai_chat" && (in.Capabilities.Video || in.Capabilities.PDF) {
		return invalid("openai_chat supports image attachments only")
	}
	return nil
}
func (s *Service) CreateModel(ctx context.Context, in ModelInput) (*Model, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := s.validateModel(&in); err != nil {
		return nil, err
	}
	if err := s.checkModelConnection(ctx, in); err != nil {
		return nil, err
	}
	now := s.now()
	id := uuid.NewString()
	var out *Model
	err := s.Repo.transaction(ctx, func(q *db.Queries) error {
		if e := q.CreateAIModel(ctx, db.CreateAIModelParams{ID: id, Name: in.Name, ModelID: in.ModelID, CapabilityKind: in.CapabilityKind, ExecutionLocation: in.ExecutionLocation, ConnectionID: nullString(in.ConnectionID), BrowserManifest: manifestJSON(in.BrowserManifest), Capabilities: encoded(in.Capabilities), DefaultParameters: encoded(in.DefaultParameters), Enabled: in.Enabled, CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		if e := writeAdminAudit(ctx, q, "ai.model.create", "ai_model", id, auditDetail{ChangedFields: []string{"name", "model_id", "capability_kind", "execution_location", "connection_id", "browser_manifest", "capabilities", "default_parameters", "enabled"}, AfterVersion: 1, EnabledAfter: &in.Enabled}); e != nil {
			return e
		}
		row, e := q.GetAIModel(ctx, id)
		if e != nil {
			return e
		}
		out, e = modelFrom(row)
		return e
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) UpdateModel(ctx context.Context, id string, in ModelInput) (*Model, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := s.validateModel(&in); err != nil {
		return nil, err
	}
	if err := s.checkModelConnection(ctx, in); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, ErrNotFound
	}
	var out *Model
	err := s.Repo.transaction(ctx, func(q *db.Queries) error {
		row, e := q.LockAIModel(ctx, id)
		if e != nil {
			return e
		}
		old, e := modelFrom(row)
		if e != nil {
			return e
		}
		if in.ExpectedVersion != 0 && in.ExpectedVersion != old.Version {
			return ErrConflict
		}
		n, e := q.UpdateAIModel(ctx, db.UpdateAIModelParams{ID: id, Name: in.Name, ModelID: in.ModelID, CapabilityKind: in.CapabilityKind, ExecutionLocation: in.ExecutionLocation, ConnectionID: nullString(in.ConnectionID), BrowserManifest: manifestJSON(in.BrowserManifest), Capabilities: encoded(in.Capabilities), DefaultParameters: encoded(in.DefaultParameters), Enabled: in.Enabled, Version: old.Version, UpdatedAt: s.now()})
		if e != nil {
			return e
		}
		if n == 0 {
			return ErrConflict
		}
		if e = writeAdminAudit(ctx, q, "ai.model.update", "ai_model", id, auditDetail{ChangedFields: modelChangedFields(old, in), BeforeVersion: old.Version, AfterVersion: old.Version + 1, EnabledBefore: &old.Enabled, EnabledAfter: &in.Enabled}); e != nil {
			return e
		}
		row, e = q.GetAIModel(ctx, id)
		if e != nil {
			return e
		}
		out, e = modelFrom(row)
		return e
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) DeleteModel(ctx context.Context, id string) error {
	if !validID(id) {
		return ErrNotFound
	}
	return s.Repo.transaction(ctx, func(q *db.Queries) error {
		old, e := q.LockAIModel(ctx, id)
		if e != nil {
			return e
		}
		if e = affected(q.DeleteAIModel(ctx, id)); e != nil {
			return e
		}
		return writeAdminAudit(ctx, q, "ai.model.delete", "ai_model", id, auditDetail{BeforeVersion: old.Version, EnabledBefore: &old.Enabled})
	})
}

// ResolveRemote is internal-only; callers must never serialize or log Credential.

func (s *Service) ResolveRemote(ctx context.Context, modelID string) (*RuntimeConfig, error) {
	return s.resolveRemote(ctx, s.Repo.q(), modelID)
}
func (s *Service) resolveRemote(ctx context.Context, q *db.Queries, modelID string) (*RuntimeConfig, error) {
	if !validID(modelID) {
		return nil, ErrNotFound
	}
	row, err := q.GetAIModel(ctx, modelID)
	if err != nil {
		return nil, dbError(err)
	}
	m, err := modelFrom(row)
	if err != nil {
		return nil, err
	}
	if !m.Enabled || m.ExecutionLocation != "server_remote" || m.ConnectionID == nil {
		return nil, ErrUnavailable
	}
	cr, err := q.GetAIConnection(ctx, *m.ConnectionID)
	if err != nil {
		return nil, dbError(err)
	}
	c := connectionFrom(cr)
	if !c.Enabled || !c.HasCredential {
		return nil, ErrUnavailable
	}
	if err = s.validateURL(c.BaseURL); err != nil {
		return nil, err
	}
	secret, err := s.cipher.decrypt(c.ID, c.CredentialEnc)
	if err != nil {
		return nil, err
	}
	return &RuntimeConfig{Model: m, Connection: c, Credential: secret}, nil
}
func (s *Service) RuntimeConfig(ctx context.Context, owner int64, sessionID string) (*RuntimeConfig, error) {
	session, err := s.ownedSession(ctx, owner, sessionID)
	if err != nil {
		return nil, err
	}
	cfg, err := s.ResolveRemote(ctx, session.ModelID)
	if err != nil {
		return nil, err
	}
	cfg.Session = session
	return cfg, nil
}
