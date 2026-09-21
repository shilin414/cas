package sharing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/dbtypes"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

type resultRepo struct {
	row       db.GetRunResultShareRow
	created   []db.CreateRunResultShareParams
	artifacts []db.RunArtifact
	err       error
}

func (f *resultRepo) GetRunResultShare(context.Context, sql.NullString) (db.GetRunResultShareRow, error) {
	if f.err != nil {
		return f.row, f.err
	}
	if f.row.Token == "" {
		return f.row, sql.ErrNoRows
	}
	return f.row, nil
}
func (f *resultRepo) CreateRunResultShare(_ context.Context, p db.CreateRunResultShareParams) error {
	f.created = append(f.created, p)
	f.row = db.GetRunResultShareRow{Token: p.Token, Snapshot: p.Snapshot}
	return nil
}
func (f *resultRepo) ListRunArtifacts(context.Context, []byte) ([]db.RunArtifact, error) {
	return f.artifacts, nil
}
func resultRun() db.Run {
	return db.Run{ID: ids.New().Bytes(), Status: "succeeded", UserID: sql.NullInt64{Int64: 7, Valid: true}, ConversationID: sql.NullInt64{Int64: 9, Valid: true}, Output: dbtypes.JSONText(`{"text":"本次完整结果，不截断"}`), FinishedAt: sql.NullTime{Valid: true, Time: time.Now()}}
}
func TestResultShareIsStableScopedAndRevocable(t *testing.T) {
	f := &resultRepo{artifacts: []db.RunArtifact{{ID: ids.New().Bytes(), Name: "报告.pdf"}}}
	run := resultRun()
	first, err := EnsureResult(context.Background(), f, run, "每日巡检")
	if err != nil {
		t.Fatal(err)
	}
	run.Output = dbtypes.JSONText(`{"text":"重试不应改变原结果"}`)
	second, err := EnsureResult(context.Background(), f, run, "改名也不影响快照")
	if err != nil || first.Token != second.Token || len(f.created) != 1 {
		t.Fatalf("unstable share: %v", err)
	}
	var entries []Entry
	if err := json.Unmarshal(second.Snapshot, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != 0 || entries[0].Content == nil || *entries[0].Content != "本次完整结果，不截断" || entries[0].Title != "每日巡检" || len(entries[0].Artifacts) != 1 {
		t.Fatalf("bad scoped snapshot: %+v", entries)
	}
	if len(first.Token) != 64 || strings.Contains(first.Token, ids.ID(run.ID).Hex()) {
		t.Fatal("token must be random, not run id")
	}
	if f.created[0].SourceRunID.String != string(run.ID) {
		t.Fatal("binary run id corrupted")
	}
	f.row.RevokedAt = sql.NullTime{Valid: true, Time: time.Now()}
	if _, err = EnsureResult(context.Background(), f, run, "task"); !errors.Is(err, ErrRevoked) {
		t.Fatalf("must not revive revoked share: %v", err)
	}
	if len(f.created) != 1 {
		t.Fatal("revocation bypassed")
	}
}
func TestResultShareFailsClosed(t *testing.T) {
	f := &resultRepo{err: errors.New("database offline")}
	if _, err := EnsureResult(context.Background(), f, resultRun(), "task"); err == nil || len(f.created) != 0 {
		t.Fatal("db failure must not create a share")
	}
	f = &resultRepo{}
	run := resultRun()
	run.Output = dbtypes.JSONText(`not json`)
	if _, err := EnsureResult(context.Background(), f, run, "task"); err == nil {
		t.Fatal("corrupt output accepted")
	}
	run = resultRun()
	run.UserID = sql.NullInt64{}
	if _, err := EnsureResult(context.Background(), f, run, "task"); err == nil {
		t.Fatal("ownerless run accepted")
	}
}
