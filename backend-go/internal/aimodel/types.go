// Package aimodel owns persistent AI model administration and owner-isolated test data.
// It deliberately has no dependency on the remote execution implementation.
package aimodel

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid AI model input")
	ErrNotFound    = errors.New("AI model resource not found")
	ErrConflict    = errors.New("AI model resource conflict")
	ErrInUse       = errors.New("AI model resource is in use")
	ErrLimit       = errors.New("AI model resource limit exceeded")
	ErrUnavailable = errors.New("AI model service unavailable")
)

const (
	PermissionRead        = "ai.model.read"
	PermissionWrite       = "ai.model.write"
	PermissionSecretWrite = "ai.connection.secret.write"
	PermissionTest        = "ai.model.test"
	PermissionLogRead     = "ai.model.log.read"
	MaxManifestBytes      = 1 << 20
	MaxOutputBytes        = 1 << 20
	MaxAttachmentBytes    = 20 << 20
	MaxHistoryBytes       = 24 << 20
	MaxHistoryMessages    = 32
)

type Options struct {
	EncryptionKey            string
	AllowedPrivateHosts      []string
	SessionTTL               time.Duration
	MaxSessionsPerUser       int
	MaxAttachmentsPerSession int
	MaxSessionBytes          int64
	DeleteObject             func(context.Context, string) error
}
type Connection struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Adapter        string    `json:"adapter"`
	BaseURL        string    `json:"base_url"`
	HasCredential  bool      `json:"has_credential"`
	Enabled        bool      `json:"enabled"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	MaxConcurrency int       `json:"max_concurrency"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	CredentialEnc  string    `json:"-"`
}
type ConnectionInput struct {
	// ExpectedVersion is supplied internally by PATCH handlers to prevent lost updates.
	ExpectedVersion int64   `json:"-"`
	Name            string  `json:"name"`
	Adapter         string  `json:"adapter"`
	BaseURL         string  `json:"base_url"`
	Enabled         bool    `json:"enabled"`
	TimeoutSeconds  int     `json:"timeout_seconds"`
	MaxConcurrency  int     `json:"max_concurrency"`
	Credential      *string `json:"credential,omitempty"`
}
type Capabilities struct {
	Image     bool `json:"image"`
	Video     bool `json:"video"`
	PDF       bool `json:"pdf"`
	Streaming bool `json:"streaming"`
}
type Parameters struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
}
type ModelInput struct {
	ExpectedVersion   int64           `json:"-"`
	Name              string          `json:"name"`
	ModelID           string          `json:"model_id"`
	CapabilityKind    string          `json:"capability_kind"`
	ExecutionLocation string          `json:"execution_location"`
	ConnectionID      *string         `json:"connection_id,omitempty"`
	BrowserManifest   json.RawMessage `json:"browser_manifest,omitempty"`
	Capabilities      Capabilities    `json:"capabilities"`
	DefaultParameters Parameters      `json:"default_parameters"`
	Enabled           bool            `json:"enabled"`
}
type Model struct {
	ID string `json:"id"`
	ModelInput
	Version          int64     `json:"version"`
	ValidationStatus string    `json:"validation_status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type Session struct {
	ID                 string    `json:"id"`
	OwnerID            int64     `json:"-"`
	ModelID            string    `json:"model_id"`
	Title              string    `json:"title"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
	ActiveInvocationID string    `json:"active_invocation_id,omitempty"`
}
type SessionDetail struct {
	Session
	Messages []Message `json:"messages"`
}
type Message struct {
	ID          string       `json:"id"`
	Role        string       `json:"role"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
	CreatedAt   time.Time    `json:"created_at"`
}
type AttachmentInput struct {
	Name       string `json:"name"`
	MIMEType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
	Kind       string `json:"kind"`
	StorageKey string `json:"-"`
}
type Attachment struct {
	ID string `json:"id"`
	AttachmentInput
	SessionID string    `json:"-"`
	MessageID string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}
type MessageInput struct {
	Text          string     `json:"text"`
	AttachmentIDs []string   `json:"attachment_ids"`
	SystemPrompt  string     `json:"system_prompt,omitempty"`
	Parameters    Parameters `json:"parameters"`
	RequestID     string     `json:"request_id"`
	// Optional immutable config from RuntimeConfig/ResolveRemote. Checked against the session model.
	// Omit to resolve config inside the admission transaction.
	Config *RuntimeConfig `json:"-"`
}
type Invocation struct {
	ID                    string          `json:"id"`
	SessionID             string          `json:"session_id"`
	OwnerID               int64           `json:"-"`
	ModelID               string          `json:"model_id"`
	RequestID             string          `json:"-"`
	ConnectionID          string          `json:"-"`
	RequestPayload        json.RawMessage `json:"-"`
	RequestHash           string          `json:"-"`
	Status                string          `json:"status"`
	OutputText            string          `json:"output_text"`
	ErrorCode             string          `json:"error_code,omitempty"`
	ErrorMessage          string          `json:"error_message,omitempty"`
	DurationMs            int64           `json:"duration_ms"`
	InputTokens           *int64          `json:"input_tokens,omitempty"`
	OutputTokens          *int64          `json:"output_tokens,omitempty"`
	CancelRequested       bool            `json:"cancel_requested"`
	CancellationConfirmed bool            `json:"cancellation_confirmed"`
	ModelVersion          int64           `json:"model_version"`
	ConnectionVersion     int64           `json:"connection_version"`
	ConfigSnapshot        json.RawMessage `json:"-"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"-"`
	CompletedAt           *time.Time      `json:"completed_at,omitempty"`
}

// InvocationLog is deliberately a separate type: no prompt, output, secret, request hash or config.
type InvocationLog struct {
	ID           string     `json:"id"`
	SessionID    string     `json:"session_id"`
	ModelID      string     `json:"model_id"`
	Status       string     `json:"status"`
	ErrorCode    string     `json:"error_code,omitempty"`
	DurationMs   int64      `json:"duration_ms"`
	InputTokens  *int64     `json:"input_tokens,omitempty"`
	OutputTokens *int64     `json:"output_tokens,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}
type Completion struct {
	Status                string
	OutputText            string
	ErrorCode             string
	ErrorMessage          string
	InputTokens           *int64
	OutputTokens          *int64
	DurationMs            int64
	CancellationConfirmed bool
}
type RuntimeConfig struct {
	Session    *Session    `json:"-"`
	Model      *Model      `json:"model"`
	Connection *Connection `json:"connection"`
	Credential string      `json:"-"`
}
