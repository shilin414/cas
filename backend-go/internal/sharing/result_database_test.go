package sharing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/database"
	"github.com/shilin414/cas/backend-go/internal/platform/dbtypes"
)

// This opt-in integration test shadows production names with CONNECTION-LOCAL
// temporary tables. It never migrates the live schema or writes business rows.
func TestResultShareDatabaseMigrationAndIdempotency(t *testing.T) {
	if os.Getenv("STUDIO_TEST_DB") != "1" {
		t.Skip("set STUDIO_TEST_DB=1 for temporary-table MySQL verification")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	conn, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TEMPORARY TABLE conversation_shares (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, token VARCHAR(64) NOT NULL UNIQUE,
 conversation_id BIGINT UNSIGNED NOT NULL, user_id BIGINT UNSIGNED NOT NULL,
 snapshot JSON NOT NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), revoked_at DATETIME(3) NULL
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	defer conn.ExecContext(context.Background(), "DROP TEMPORARY TABLE IF EXISTS conversation_shares")
	exec(`CREATE TEMPORARY TABLE run_artifacts (
 id BINARY(16), run_id BINARY(16), provider VARCHAR(64), external_artifact_id VARCHAR(64),
 provider_artifact_type VARCHAR(64), name VARCHAR(255), normalized_type VARCHAR(64), storage_type VARCHAR(64),
 cached_external_url TEXT, cached_url_fetched_at DATETIME(3), cached_url_expires_at DATETIME(3),
 storage_key VARCHAR(255), resolution_status VARCHAR(64), metadata JSON,
 created_at DATETIME(3), updated_at DATETIME(3)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	defer conn.ExecContext(context.Background(), "DROP TEMPORARY TABLE IF EXISTS run_artifacts")
	up, err := os.ReadFile("../../db/migrations/0041_run_result_shares.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(up))
	q := db.New(conn)
	run := resultRun()
	run.Output = dbtypes.JSONText(`{"text":"` + strings.Repeat("完整🙂", 1800) + `"}`)
	first, err := EnsureResult(ctx, q, run, "集成巡检")
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureResult(ctx, q, run, "另一个接收人")
	if err != nil || first.Token != second.Token {
		t.Fatalf("retry token changed: %v", err)
	}
	// Simulate a racing loser: different random token for the same source run.
	loser, _ := NewToken()
	err = q.CreateRunResultShare(ctx, db.CreateRunResultShareParams{Token: loser, ConversationID: 9, UserID: 7, Snapshot: dbtypes.JSONText(`[]`), SourceRunID: sql.NullString{String: string(run.ID), Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	row, err := q.GetRunResultShare(ctx, sql.NullString{String: string(run.ID), Valid: true})
	if err != nil || row.Token != first.Token {
		t.Fatalf("unique run barrier failed: %v", err)
	}
	var entries []Entry
	if err := json.Unmarshal(row.Snapshot, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len([]rune(*entries[0].Content)) != 5400 {
		t.Fatal("full Unicode result truncated")
	}
	// Multiple legacy shares retain NULL source_run_id and stay valid.
	for _, token := range []string{"manual-one", "manual-two"} {
		if _, err := q.CreateConversationShare(ctx, db.CreateConversationShareParams{Token: token, ConversationID: 9, UserID: 7, Snapshot: dbtypes.JSONText(`[{"id":2}]`)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.RevokeConversationShare(ctx, db.RevokeConversationShareParams{Token: first.Token, UserID: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureResult(ctx, q, run, "task"); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked result revived: %v", err)
	}
	down, err := os.ReadFile("../../db/migrations/0041_run_result_shares.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(down))
	if _, err := q.GetConversationShareByToken(ctx, "manual-one"); err != nil {
		t.Fatalf("migration rollback broke legacy shares: %v", err)
	}
	exec(string(up))
	t.Log("temporary-table migration up/down/up, Unicode full snapshot, unique-run retry/fan-out, revocation and legacy NULL compatibility passed")
}
