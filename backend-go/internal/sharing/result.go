package sharing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/dbtypes"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

var ErrRevoked = errors.New("result share has been revoked")

type resultQueries interface {
	GetRunResultShare(context.Context, sql.NullString) (db.GetRunResultShareRow, error)
	CreateRunResultShare(context.Context, db.CreateRunResultShareParams) error
	ListRunArtifacts(context.Context, []byte) ([]db.RunArtifact, error)
}

// EnsureResult pins ONLY this run's output and artifacts, not the surrounding
// conversation. The unique source_run_id arbitrates concurrent fan-out. A
// losing insert reads the winner's random token; retries never revive revocation.
func EnsureResult(ctx context.Context, q resultQueries, run db.Run, title string) (db.GetRunResultShareRow, error) {
	var zero db.GetRunResultShareRow
	if len(run.ID) != 16 || !run.UserID.Valid || run.UserID.Int64 <= 0 || !run.ConversationID.Valid || run.ConversationID.Int64 <= 0 || run.Status != "succeeded" {
		return zero, fmt.Errorf("result share requires a succeeded run with an owner and conversation")
	}
	runID := sql.NullString{String: string(run.ID), Valid: true}
	existing, err := q.GetRunResultShare(ctx, runID)
	if err == nil {
		return activeResult(existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, fmt.Errorf("read result share: %w", err)
	}
	var output struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(run.Output, &output); err != nil {
		return zero, fmt.Errorf("decode result output: %w", err)
	}
	artifacts, err := q.ListRunArtifacts(ctx, run.ID)
	if err != nil {
		return zero, fmt.Errorf("read result artifacts: %w", err)
	}
	entry := Entry{Role: "assistant", Content: &output.Text, Title: title, CreatedAt: run.CreatedAt}
	if run.FinishedAt.Valid {
		entry.CreatedAt = run.FinishedAt.Time
	}
	for _, a := range artifacts {
		entry.Artifacts = append(entry.Artifacts, ArtifactRef{ArtifactID: ids.ID(a.ID).String(), Name: a.Name})
	}
	raw, err := json.Marshal([]Entry{entry})
	if err != nil {
		return zero, err
	}
	token, err := NewToken()
	if err != nil {
		return zero, err
	}
	if err = q.CreateRunResultShare(ctx, db.CreateRunResultShareParams{Token: token, ConversationID: uint64(run.ConversationID.Int64), UserID: uint64(run.UserID.Int64), Snapshot: dbtypes.JSONText(raw), SourceRunID: runID}); err != nil {
		return zero, fmt.Errorf("save result share: %w", err)
	}
	existing, err = q.GetRunResultShare(ctx, runID)
	if err != nil {
		return zero, fmt.Errorf("read saved result share: %w", err)
	}
	return activeResult(existing)
}
func activeResult(row db.GetRunResultShareRow) (db.GetRunResultShareRow, error) {
	if row.RevokedAt.Valid {
		return db.GetRunResultShareRow{}, ErrRevoked
	}
	return row, nil
}
