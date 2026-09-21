package aimodel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/go-sql-driver/mysql"
)

// Repo owns SQL persistence. Runtime consumers use Service, not raw ownerless queries.
type Repo struct{ DB *sql.DB }

func (r *Repo) q() *db.Queries { return db.New(r.DB) }
func (r *Repo) transaction(ctx context.Context, fn func(*db.Queries) error) error {
	// READ COMMITTED makes quota counts see the most recent committed admission after obtaining a lock.
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	if err = fn(db.New(tx)); err != nil {
		return dbError(err)
	}
	return dbError(tx.Commit())
}
func dbError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1451:
			return ErrInUse
		case 1452:
			return ErrNotFound
		case 1062, 1213, 1205:
			return ErrConflict
		}
	}
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrInUse) || errors.Is(err, ErrLimit) || errors.Is(err, ErrUnavailable) {
		return err
	}
	// Database driver messages may contain SQL, paths, connection strings, or values.
	return ErrUnavailable
}
func affected(n int64, err error) error {
	if err != nil {
		return dbError(err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func nullString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}
func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	p := v.String
	return &p
}
func numberPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	p := v.Int64
	return &p
}
func nullNumber(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}
func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	p := v.Time
	return &p
}
func encoded(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func manifestJSON(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("null")
	}
	return v
}
func connectionFrom(v db.AiConnection) *Connection {
	return &Connection{ID: v.ID, Name: v.Name, Adapter: v.Adapter, BaseURL: v.BaseUrl, CredentialEnc: v.CredentialEnc, HasCredential: v.CredentialEnc != "", Enabled: v.Enabled, TimeoutSeconds: int(v.TimeoutSeconds), MaxConcurrency: int(v.MaxConcurrency), Version: v.Version, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func modelFrom(v db.AiModel) (*Model, error) {
	m := &Model{ID: v.ID, ModelInput: ModelInput{Name: v.Name, ModelID: v.ModelID, CapabilityKind: v.CapabilityKind, ExecutionLocation: v.ExecutionLocation, ConnectionID: stringPtr(v.ConnectionID), BrowserManifest: v.BrowserManifest, Enabled: v.Enabled}, Version: v.Version, ValidationStatus: v.ValidationStatus, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	if err := json.Unmarshal(v.Capabilities, &m.Capabilities); err != nil {
		return nil, ErrUnavailable
	}
	if err := json.Unmarshal(v.DefaultParameters, &m.DefaultParameters); err != nil {
		return nil, ErrUnavailable
	}
	return m, nil
}
func sessionFrom(v db.AiTestSession) *Session {
	return &Session{ID: v.ID, OwnerID: v.OwnerID, ModelID: v.ModelID, Title: v.Title, CreatedAt: v.CreatedAt, ExpiresAt: v.ExpiresAt, ActiveInvocationID: v.ActiveInvocationID.String}
}
func attachmentFrom(v db.AiTestAttachment) *Attachment {
	return &Attachment{ID: v.ID, SessionID: v.SessionID, MessageID: v.MessageID.String, AttachmentInput: AttachmentInput{Name: v.Name, MIMEType: v.MimeType, SizeBytes: v.SizeBytes, Kind: v.Kind, StorageKey: v.StorageKey}, CreatedAt: v.CreatedAt}
}
func invocationFrom(v db.AiInvocation) *Invocation {
	return &Invocation{ID: v.ID, SessionID: v.SessionID, OwnerID: v.OwnerID, ModelID: v.ModelID, RequestID: v.RequestID, ConnectionID: v.ConnectionID, RequestPayload: v.RequestPayload, RequestHash: v.RequestHash, Status: v.Status, OutputText: v.OutputText, ErrorCode: v.ErrorCode, ErrorMessage: v.ErrorMessage, DurationMs: v.DurationMs, InputTokens: numberPtr(v.InputTokens), OutputTokens: numberPtr(v.OutputTokens), CancelRequested: v.CancelRequested, CancellationConfirmed: v.CancellationConfirmed, ModelVersion: v.ModelVersion, ConnectionVersion: v.ConnectionVersion, ConfigSnapshot: v.ConfigSnapshot, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, CompletedAt: timePtr(v.CompletedAt)}
}
