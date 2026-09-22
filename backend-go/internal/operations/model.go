// Package operations exposes bounded, metadata-only administrative views.
package operations

import (
	"errors"
	"time"
)

var (
	ErrInvalid     = errors.New("operations: invalid input")
	ErrConflict    = errors.New("operations: capacity changed; refresh before retrying")
	ErrNotFound    = errors.New("operations: provider not found")
	ErrBusy        = errors.New("operations: query budget exhausted")
	ErrUnavailable = errors.New("operations: database unavailable")
)

type Counts struct {
	Queued          int64 `json:"queued"`
	Running         int64 `json:"running"`
	WaitingInput    int64 `json:"waiting_input"`
	WaitingExternal int64 `json:"waiting_external"`
	Cancelling      int64 `json:"cancelling"`
}
type Totals struct {
	Counts
	PendingOccurrences int64 `json:"pending_occurrences"`
	PendingDeliveries  int64 `json:"pending_deliveries"`
	SendingDeliveries  int64 `json:"sending_deliveries"`
	PendingOutbox      int64 `json:"pending_outbox"`
}
type Provider struct {
	Key                  string `json:"key"`
	Name                 string `json:"name"`
	Status               string `json:"status"`
	MaxInflight          int64  `json:"max_inflight"`
	EffectiveInflight    int64  `json:"effective_inflight"`
	ControlledInflight   int64  `json:"controlled_inflight"`
	UncontrolledInflight int64  `json:"uncontrolled_inflight"`
	Counts
	OldestQueuedAt         *time.Time `json:"oldest_queued_at"`
	OldestQueuedAgeSeconds int64      `json:"oldest_queued_age_seconds"`
}
type Alert struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Provider string `json:"provider,omitempty"`
	Message  string `json:"message"`
}
type Overview struct {
	SampledAt   time.Time  `json:"sampled_at"`
	Providers   []Provider `json:"providers"`
	Totals      Totals     `json:"totals"`
	Alerts      []Alert    `json:"alerts"`
	Limitations []string   `json:"limitations"`
}
type RunFilter struct {
	Status, Provider, OwnerUserID, ApplicationID, Cursor string
	Limit                                                int
}
type Run struct {
	ID              string     `json:"id"`
	Provider        string     `json:"provider"`
	RuntimeType     string     `json:"runtime_type"`
	Status          string     `json:"status"`
	Priority        string     `json:"priority"`
	TriggerType     string     `json:"trigger_type"`
	OwnerUserID     *string    `json:"owner_user_id"`
	ApplicationID   *string    `json:"application_id"`
	ConversationID  *string    `json:"conversation_id"`
	QueuedAt        time.Time  `json:"queued_at"`
	AvailableAt     *time.Time `json:"available_at"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	CreatedAt       time.Time  `json:"created_at"`
	Attempt         uint32     `json:"attempt"`
	MaxAttempts     uint32     `json:"max_attempts"`
	ErrorCode       string     `json:"error_code"`
	QueueAgeSeconds *int64     `json:"queue_age_seconds"`
	WaitReason      string     `json:"wait_reason"`
}
type RunPage struct {
	Results    []Run   `json:"results"`
	NextCursor *string `json:"next_cursor"`
}
type CapacityInput struct {
	MaxInflight         int64  `json:"max_inflight"`
	ExpectedMaxInflight int64  `json:"expected_max_inflight"`
	Reason              string `json:"reason"`
}
type CapacityResult struct {
	Provider            string    `json:"provider"`
	PreviousMaxInflight int64     `json:"previous_max_inflight"`
	MaxInflight         int64     `json:"max_inflight"`
	EffectiveFor        string    `json:"effective_for"`
	UpdatedAt           time.Time `json:"updated_at"`
}
