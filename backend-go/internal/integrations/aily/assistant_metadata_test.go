package aily

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/execution"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

func TestAssistantMetadataPreservesProcessWhenArtifactLookupFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run := &execution.Run{ID: ids.New()}
	lookupErr := errors.New("temporary artifact lookup failure")
	mock.ExpectQuery("(?s)SELECT .*FROM run_artifacts WHERE run_id").WithArgs(run.ID.Bytes()).WillReturnError(lookupErr)
	executor := &Executor{Owned: execution.NewService(db, nil, nil, nil).WorkerOwned()}
	raw, err := executor.assistantMetadata(context.Background(), run, "完整执行过程")
	if !errors.Is(err, lookupErr) {
		t.Fatalf("missing lookup error: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("lost durable base metadata: %v", err)
	}
	if meta["process_text"] != "完整执行过程" || meta["run_id"] != run.ID.String() || meta["provider"] != ProviderKey {
		t.Fatalf("lost base metadata: %#v", meta)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAssistantMetadataIncludesProcessWithNoArtifacts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run := &execution.Run{ID: ids.New()}
	mock.ExpectQuery("(?s)SELECT .*FROM run_artifacts WHERE run_id").WithArgs(run.ID.Bytes()).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	executor := &Executor{Owned: execution.NewService(db, nil, nil, nil).WorkerOwned()}
	raw, err := executor.assistantMetadata(context.Background(), run, "过程一\n\n过程二")
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["process_text"] != "过程一\n\n过程二" || meta["run_id"] != run.ID.String() {
		t.Fatalf("bad history metadata: %#v", meta)
	}
	if _, ok := meta["artifacts"]; ok {
		t.Fatal("unexpected artifacts")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
