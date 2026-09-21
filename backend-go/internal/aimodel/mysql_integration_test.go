package aimodel

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
)

// Opt-in only. This test NEVER accepts a remote/shared database or another schema.
// Set AI_MODEL_MYSQL_INTEGRATION=1. Optional AI_MODEL_TEST_DSN overrides the local
// .env.local credentials; the same strict localhost/schema guard still applies.
func integrationDatabase(t *testing.T) (*sql.DB, *migrate.Migrate) {
	t.Helper()
	if os.Getenv("AI_MODEL_MYSQL_INTEGRATION") != "1" {
		t.Skip("set AI_MODEL_MYSQL_INTEGRATION=1 for isolated MySQL 5.7 verification")
	}
	dsn := os.Getenv("AI_MODEL_TEST_DSN")
	if dsn == "" {
		f, e := os.Open("../../.env.local")
		if e != nil {
			t.Fatal("local test environment is missing")
		}
		defer f.Close()
		values := map[string]string{}
		scan := bufio.NewScanner(f)
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if ok {
				values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
			}
		}
		if e = scan.Err(); e != nil {
			t.Fatal("cannot read local test environment")
		}
		cfg := mysql.NewConfig()
		cfg.User = values["DB_USER"]
		cfg.Passwd = values["DB_PASSWORD"]
		cfg.Net = "tcp"
		cfg.Addr = values["DB_HOST"] + ":" + values["DB_PORT"]
		cfg.DBName = "studio_model_integration"
		cfg.ParseTime = true
		cfg.MultiStatements = true
		cfg.Loc = time.UTC
		dsn = cfg.FormatDSN()
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil {
		t.Fatal("invalid test DSN")
	}
	if cfg.Net != "tcp" || cfg.Addr != "127.0.0.1:33079" || cfg.DBName != "studio_model_integration" {
		t.Fatal("refusing unsafe integration database: only 127.0.0.1:33079/studio_model_integration is permitted")
	}
	cfg.ParseTime = true
	cfg.MultiStatements = true
	cfg.Loc = time.UTC
	database, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		t.Fatal("cannot open isolated test database")
	}
	database.SetMaxOpenConns(20)
	if e = database.Ping(); e != nil {
		t.Fatal("cannot reach isolated test database")
	}
	var version string
	if e = database.QueryRow("SELECT VERSION()").Scan(&version); e != nil || !strings.HasPrefix(version, "5.7.") {
		t.Fatal("integration suite requires real MySQL 5.7")
	}
	t.Log("database engine verified: MySQL", version)
	driver, e := migratemysql.WithInstance(database, &migratemysql.Config{})
	if e != nil {
		t.Fatal(e)
	}
	migrations, e := migrate.NewWithDatabaseInstance("file://../../db/migrations", "mysql", driver)
	if e != nil {
		t.Fatal(e)
	}
	if e = migrations.Up(); e != nil && !errors.Is(e, migrate.ErrNoChange) {
		t.Fatal(e)
	}
	t.Cleanup(func() { migrations.Close() })
	return database, migrations
}
func TestMySQLPersistenceLifecycle(t *testing.T) {
	database, migrations := integrationDatabase(t)
	ctx := context.Background()
	// Reset 0044 presets and 0043 model tables in the explicitly guarded disposable schema.
	version, dirty, e := migrations.Version()
	if e != nil || version != 44 || dirty {
		t.Fatalf("migration baseline: %v/%v/%v", version, dirty, e)
	}
	var presets int
	if e = database.QueryRow("SELECT COUNT(*) FROM ai_models WHERE id IN ('5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b02','5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b03')").Scan(&presets); e != nil || presets != 2 {
		t.Fatalf("0044 preset count %d: %v", presets, e)
	}
	if e = migrations.Steps(-1); e != nil {
		t.Fatal(e)
	}
	if e = database.QueryRow("SELECT COUNT(*) FROM ai_models WHERE id IN ('5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b02','5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b03')").Scan(&presets); e != nil || presets != 0 {
		t.Fatalf("0044 down left presets %d: %v", presets, e)
	}
	if e = migrations.Steps(-1); e != nil {
		t.Fatal(e)
	}
	var tables int
	if e = database.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='ai_models'").Scan(&tables); e != nil || tables != 0 {
		t.Fatal("0043 down did not remove model tables")
	}
	if e = migrations.Steps(2); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := migrations.Steps(-1); e != nil {
			t.Error(e)
		}
		if e := migrations.Steps(1); e != nil {
			t.Error(e)
		}
	})
	var permissions int
	if e = database.QueryRow("SELECT COUNT(*) FROM admin_permissions WHERE category='ai_model'").Scan(&permissions); e != nil || permissions != 5 {
		t.Fatalf("permission seed count %d: %v", permissions, e)
	}
	var grants int
	if e = database.QueryRow("SELECT COUNT(*) FROM admin_role_permissions rp JOIN admin_roles r ON r.id=rp.role_id JOIN admin_permissions p ON p.id=rp.permission_id WHERE r.code='platform_owner' AND p.category='ai_model'").Scan(&grants); e != nil || grants != 5 {
		t.Fatalf("owner grants %d: %v", grants, e)
	}
	var mu sync.Mutex
	deleted := []string{}
	service, e := NewService(&Repo{DB: database}, Options{EncryptionKey: "test-only-master-key", AllowedPrivateHosts: []string{"127.0.0.1"}, MaxSessionsPerUser: 100, DeleteObject: func(_ context.Context, key string) error {
		mu.Lock()
		defer mu.Unlock()
		deleted = append(deleted, key)
		return nil
	}})
	must(t, e)
	secret := "sensitive-test-token"
	conn, e := service.CreateConnection(ctx, ConnectionInput{Name: "Integration connection", Adapter: "gemini", BaseURL: "http://127.0.0.1:33111/v1", Enabled: true, TimeoutSeconds: 60, MaxConcurrency: 2, Credential: &secret})
	must(t, e)
	if conn.CredentialEnc == secret || !strings.HasPrefix(conn.CredentialEnc, "v1:") {
		t.Fatal("credential not encrypted")
	}
	blob, _ := json.Marshal(conn)
	if strings.Contains(string(blob), secret) || strings.Contains(string(blob), conn.CredentialEnc) {
		t.Fatal("credential serialized")
	}
	connections, e := service.ListConnections(ctx)
	must(t, e)
	if len(connections) != 1 {
		t.Fatal("connection listing")
	}
	must(t, service.SetConnectionCredential(ctx, conn.ID, "rotated-token"))
	conn, e = service.GetConnection(ctx, conn.ID)
	must(t, e)
	if conn.Version != 2 {
		t.Fatal("credential version")
	}
	conn, e = service.UpdateConnection(ctx, conn.ID, ConnectionInput{Name: "Renamed", Adapter: "gemini", BaseURL: conn.BaseURL, Enabled: true, TimeoutSeconds: 30, MaxConcurrency: 2})
	must(t, e)
	mid := conn.ID
	model, e := service.CreateModel(ctx, ModelInput{Name: "Remote", ModelID: "mock-chat", CapabilityKind: "chat", ExecutionLocation: "server_remote", ConnectionID: &mid, Capabilities: Capabilities{Image: true, Video: true, PDF: true, Streaming: true}, Enabled: true})
	must(t, e)
	if model.ValidationStatus != "unverified" {
		t.Fatal("fabricated validation status")
	}
	models, e := service.ListModels(ctx)
	must(t, e)
	if len(models) != 1 {
		t.Fatal("model listing")
	}
	updated := model.ModelInput
	updated.Name = "Remote renamed"
	model, e = service.UpdateModel(ctx, model.ID, updated)
	must(t, e)
	if model.Version != 2 {
		t.Fatal("model version")
	}
	cfg, e := service.ResolveRemote(ctx, model.ID)
	must(t, e)
	if cfg.Credential != "rotated-token" {
		t.Fatal("credential decryption")
	}
	if e = service.DeleteConnection(ctx, conn.ID); !errors.Is(e, ErrInUse) {
		t.Fatalf("connection reference guard: %v", e)
	}
	session, e := service.CreateSession(ctx, 11, model.ID, "owner 11")
	must(t, e)
	if e = service.DeleteModel(ctx, model.ID); !errors.Is(e, ErrInUse) {
		t.Fatalf("model reference guard: %v", e)
	}
	sessions, e := service.ListSessions(ctx, 11, model.ID)
	must(t, e)
	if len(sessions) != 1 {
		t.Fatal("session list")
	}
	sessions, e = service.ListSessions(ctx, 12, "")
	must(t, e)
	if len(sessions) != 0 {
		t.Fatal("owner leak")
	}
	if _, e = service.GetSession(ctx, 12, session.ID); !errors.Is(e, ErrNotFound) {
		t.Fatalf("session owner fence: %v", e)
	}
	if e = service.DeleteSession(ctx, 12, session.ID); !errors.Is(e, ErrNotFound) {
		t.Fatalf("session deletion owner fence: %v", e)
	}
	cfg, e = service.RuntimeConfig(ctx, 11, session.ID)
	must(t, e)
	attachment, e := service.AddAttachment(ctx, 11, session.ID, AttachmentInput{Name: "image.png", MIMEType: "image/png", Kind: "image", SizeBytes: 10, StorageKey: "ai/integration/image-1"})
	must(t, e)
	if _, e = service.GetAttachment(ctx, 12, session.ID, attachment.ID); !errors.Is(e, ErrNotFound) {
		t.Fatalf("attachment owner fence: %v", e)
	}
	aa, e := service.ListAttachments(ctx, 11, session.ID)
	must(t, e)
	if len(aa) != 1 {
		t.Fatal("attachment listing")
	}
	_, e = service.GetAttachment(ctx, 11, session.ID, attachment.ID)
	must(t, e)
	request := MessageInput{Text: "hello", SystemPrompt: "private system instruction", RequestID: uuid.NewString(), AttachmentIDs: []string{attachment.ID}, Config: cfg}
	absent, e := service.LookupInvocation(ctx, 11, session.ID, request)
	must(t, e)
	if absent != nil {
		t.Fatal("unknown request found")
	}
	inv, fresh, e := service.BeginInvocation(ctx, 11, session.ID, request)
	must(t, e)
	if !fresh {
		t.Fatal("first invocation not fresh")
	}
	if strings.Contains(string(inv.ConfigSnapshot), "rotated-token") || strings.Contains(string(inv.ConfigSnapshot), "private system") {
		t.Fatal("sensitive config snapshot")
	}
	if !strings.Contains(string(inv.RequestPayload), "private system instruction") {
		t.Fatal("system prompt not persisted privately")
	}
	if inv.ModelVersion != model.Version || inv.ConnectionVersion != conn.Version {
		t.Fatal("snapshot versions")
	}
	repeated, fresh, e := service.BeginInvocation(ctx, 11, session.ID, request)
	must(t, e)
	if fresh || repeated.ID != inv.ID {
		t.Fatal("idempotency failed")
	}
	different := request
	different.Text = "different"
	if _, _, e = service.BeginInvocation(ctx, 11, session.ID, different); !errors.Is(e, ErrConflict) {
		t.Fatalf("idempotency mismatch: %v", e)
	}
	if _, e = service.LookupInvocation(ctx, 11, session.ID, different); !errors.Is(e, ErrConflict) {
		t.Fatalf("lookup mismatch: %v", e)
	}
	if _, e = service.GetInvocation(ctx, 12, inv.ID); !errors.Is(e, ErrNotFound) {
		t.Fatalf("invocation owner fence: %v", e)
	}
	if _, e = service.RequestCancellation(ctx, 12, inv.ID); !errors.Is(e, ErrNotFound) {
		t.Fatalf("cancel owner fence: %v", e)
	}
	if e = service.DeleteSession(ctx, 11, session.ID); !errors.Is(e, ErrInUse) {
		t.Fatalf("active deletion: %v", e)
	}
	if e = service.DeleteAttachment(ctx, 11, session.ID, attachment.ID); !errors.Is(e, ErrInUse) {
		t.Fatalf("active attachment deletion: %v", e)
	}
	// History includes the committed current user message before runtime begins.
	detail, e := service.GetSession(ctx, 11, session.ID)
	must(t, e)
	if detail.ActiveInvocationID != inv.ID {
		t.Fatal("active invocation not exposed for owner session recovery")
	}
	if len(detail.Messages) != 1 || detail.Messages[0].Text != "hello" || len(detail.Messages[0].Attachments) != 1 {
		t.Fatal("atomic message/attachment admission")
	}
	started, e := service.StartInvocation(ctx, inv.ID)
	must(t, e)
	if !started {
		t.Fatal("failed to start")
	}
	must(t, service.UpdateInvocationOutput(ctx, inv.ID, "partial"))
	status, e := service.GetInvocation(ctx, 11, inv.ID)
	must(t, e)
	if status.OutputText != "partial" || status.Status != "running" {
		t.Fatal("output snapshot")
	}
	n := int64(7)
	must(t, service.FinishInvocation(ctx, inv.ID, Completion{Status: "succeeded", OutputText: "complete", InputTokens: &n, OutputTokens: &n, DurationMs: 15}))
	must(t, service.FinishInvocation(ctx, inv.ID, Completion{Status: "failed", ErrorMessage: "late failure"}))
	detail, e = service.GetSession(ctx, 11, session.ID)
	must(t, e)
	if detail.ActiveInvocationID != "" {
		t.Fatal("terminal invocation left on owned session")
	}
	if len(detail.Messages) != 2 || detail.Messages[1].Role != "assistant" {
		t.Fatal("completion not exactly once")
	}
	if e = service.DeleteAttachment(ctx, 11, session.ID, attachment.ID); !errors.Is(e, ErrInUse) {
		t.Fatalf("history attachment deletion: %v", e)
	}
	if e = service.UpdateInvocationOutput(ctx, inv.ID, "late"); !errors.Is(e, ErrConflict) {
		t.Fatal("terminal output overwritten")
	}
	logs, e := service.ListInvocations(ctx, 11, model.ID)
	must(t, e)
	logjson, _ := json.Marshal(logs)
	if len(logs) != 1 || strings.Contains(string(logjson), "output_text") || strings.Contains(string(logjson), "private") {
		t.Fatalf("unsafe logs %s", logjson)
	}
	// Replay works after model disable; resolving a new remote request does not.
	input := model.ModelInput
	input.Enabled = false
	_, e = service.UpdateModel(ctx, model.ID, input)
	must(t, e)
	replay, e := service.LookupInvocation(ctx, 11, session.ID, request)
	must(t, e)
	if replay.ID != inv.ID {
		t.Fatal("disabled replay failed")
	}
	if _, e = service.ResolveRemote(ctx, model.ID); !errors.Is(e, ErrUnavailable) {
		t.Fatal("disabled model executable")
	}
	input.Enabled = true
	model, e = service.UpdateModel(ctx, model.ID, input)
	must(t, e)
	// Multiple service instances share SQL locks, not a process-local mutex.
	other, e := NewService(&Repo{DB: database}, service.optsWithKey())
	must(t, e)
	t.Run("atomic admin audits and audit rejection rollback", func(t *testing.T) {
		const actorID int64 = 94142
		auditCtx := WithActor(ctx, actorID)
		defer database.Exec("DELETE FROM audit_logs WHERE user_id=? AND resource IN ('ai_connection','ai_model')", actorID)
		connectionInput := ConnectionInput{Name: "PRIVATE_CONNECTION_NAME", Adapter: "gemini", BaseURL: "https://private-audit-host.example/v1", Enabled: true, TimeoutSeconds: 60, MaxConcurrency: 2}
		auditSecret := "PRIVATE_CREDENTIAL_VALUE"
		connectionInput.Credential = &auditSecret
		c, e := service.CreateConnection(auditCtx, connectionInput)
		must(t, e)
		connectionInput.Credential = nil
		connectionInput.Enabled = false
		connectionInput.Name = "PRIVATE_CHANGED_NAME"
		c, e = service.UpdateConnection(auditCtx, c.ID, connectionInput)
		must(t, e)
		connectionInput.Enabled = true
		c, e = service.UpdateConnection(auditCtx, c.ID, connectionInput)
		must(t, e)
		must(t, service.SetConnectionCredential(auditCtx, c.ID, "PRIVATE_ROTATED_CREDENTIAL"))
		must(t, service.SetConnectionCredential(auditCtx, c.ID, ""))
		localInput := validLocalInput()
		localInput.Enabled = true
		localInput.Name = "PRIVATE_LOCAL_MODEL_NAME"
		local, e := service.CreateModel(auditCtx, localInput)
		must(t, e)
		localInput.Enabled = false
		local, e = service.UpdateModel(auditCtx, local.ID, localInput)
		must(t, e)
		localInput.Enabled = true
		local, e = service.UpdateModel(auditCtx, local.ID, localInput)
		must(t, e)
		must(t, service.DeleteModel(auditCtx, local.ID))
		must(t, service.DeleteConnection(auditCtx, c.ID))
		rows, e := database.Query("SELECT user_id,action,resource,resource_id,detail FROM audit_logs WHERE user_id=? ORDER BY id", actorID)
		must(t, e)
		count := 0
		actions := map[string]int{}
		toggles := 0
		for rows.Next() {
			var who int64
			var action, resource, id, detail string
			must(t, rows.Scan(&who, &action, &resource, &id, &detail))
			count++
			actions[action]++
			if who != actorID || (resource != "ai_connection" && resource != "ai_model") || (id != c.ID && id != local.ID) {
				t.Fatal("audit identity mismatch")
			}
			for _, sensitive := range []string{"PRIVATE_", "private-audit-host", "PP-OCRv6", "paddleocr_tiny", "resources", "sha256", "credential_enc", "prompt", "body"} {
				if strings.Contains(detail, sensitive) {
					t.Fatalf("sensitive value in audit: %s", sensitive)
				}
			}
			var payload map[string]json.RawMessage
			must(t, json.Unmarshal([]byte(detail), &payload))
			for key := range payload {
				switch key {
				case "changed_fields", "before_version", "after_version", "enabled_before", "enabled_after":
				default:
					t.Fatalf("unexpected audit field %s", key)
				}
			}
			if string(payload["enabled_before"]) != string(payload["enabled_after"]) && len(payload["enabled_before"]) > 0 && len(payload["enabled_after"]) > 0 {
				toggles++
			}
		}
		must(t, rows.Err())
		rows.Close()
		if count != 10 || toggles != 4 || actions["ai.connection.create"] != 1 || actions["ai.connection.update"] != 2 || actions["ai.connection.credential.replace"] != 1 || actions["ai.connection.credential.clear"] != 1 || actions["ai.model.create"] != 1 || actions["ai.model.update"] != 2 || actions["ai.model.delete"] != 1 || actions["ai.connection.delete"] != 1 {
			t.Fatalf("audit actions/count/toggles: %v/%d/%d", actions, count, toggles)
		}
		// Internal callers without an actor remain explicitly unaudited and runtime APIs are unchanged.
		stableConnection, e := service.CreateConnection(ctx, connectionInput)
		must(t, e)
		stableModel, e := service.CreateModel(ctx, localInput)
		must(t, e)
		defer service.DeleteModel(ctx, stableModel.ID)
		defer service.DeleteConnection(ctx, stableConnection.ID)
		var beforeConnections, beforeModels int
		must(t, database.QueryRow("SELECT COUNT(*) FROM ai_connections").Scan(&beforeConnections))
		must(t, database.QueryRow("SELECT COUNT(*) FROM ai_models").Scan(&beforeModels))
		_, e = database.Exec("CREATE TRIGGER aimodel_test_reject_audit BEFORE INSERT ON audit_logs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='deliberate private audit failure'")
		must(t, e)
		defer database.Exec("DROP TRIGGER IF EXISTS aimodel_test_reject_audit")
		alteredConnection := connectionInput
		alteredConnection.Enabled = false
		alteredConnection.Name = "must rollback"
		alteredModel := localInput
		alteredModel.Enabled = false
		alteredModel.Name = "must rollback"
		mutations := []func() error{
			func() error { _, e := service.CreateConnection(auditCtx, connectionInput); return e },
			func() error {
				_, e := service.UpdateConnection(auditCtx, stableConnection.ID, alteredConnection)
				return e
			},
			func() error { return service.DeleteConnection(auditCtx, stableConnection.ID) },
			func() error { return service.SetConnectionCredential(auditCtx, stableConnection.ID, "must rollback") },
			func() error { _, e := service.CreateModel(auditCtx, localInput); return e },
			func() error { _, e := service.UpdateModel(auditCtx, stableModel.ID, alteredModel); return e },
			func() error { return service.DeleteModel(auditCtx, stableModel.ID) },
		}
		for i, mutate := range mutations {
			if e := mutate(); !errors.Is(e, ErrUnavailable) {
				t.Fatalf("audit rejection mutation %d: %v", i, e)
			}
		}
		afterConnection, e := service.GetConnection(ctx, stableConnection.ID)
		must(t, e)
		afterModel, e := service.GetModel(ctx, stableModel.ID)
		must(t, e)
		if afterConnection.Version != stableConnection.Version || afterConnection.Name != stableConnection.Name || afterConnection.Enabled != stableConnection.Enabled || afterConnection.CredentialEnc != stableConnection.CredentialEnc || afterModel.Version != stableModel.Version || afterModel.Name != stableModel.Name || afterModel.Enabled != stableModel.Enabled {
			t.Fatal("configuration persisted despite audit rejection")
		}
		var afterConnections, afterModels int
		must(t, database.QueryRow("SELECT COUNT(*) FROM ai_connections").Scan(&afterConnections))
		must(t, database.QueryRow("SELECT COUNT(*) FROM ai_models").Scan(&afterModels))
		if beforeConnections != afterConnections || beforeModels != afterModels {
			t.Fatal("create/delete escaped audit rollback")
		}
		if _, e = service.CreateConnection(WithActor(ctx, 0), connectionInput); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid explicit actor accepted")
		}
	})
	t.Run("local model CRUD and optimistic updates", func(t *testing.T) {
		localInput := validLocalInput()
		localInput.Enabled = true
		local, e := service.CreateModel(ctx, localInput)
		must(t, e)
		if _, e = service.CreateSession(ctx, 11, local.ID, "local"); !errors.Is(e, ErrInvalid) {
			t.Fatal("browser OCR server session accepted")
		}
		original := local.ModelInput
		original.ExpectedVersion = local.Version
		_, e = service.UpdateModel(ctx, local.ID, original)
		must(t, e)
		if _, e = service.UpdateModel(ctx, local.ID, original); !errors.Is(e, ErrConflict) {
			t.Fatal("stale PATCH overwritten")
		}
		must(t, service.DeleteModel(ctx, local.ID))
		old, e := service.GetConnection(ctx, conn.ID)
		must(t, e)
		change := ConnectionInput{Name: old.Name, Adapter: old.Adapter, BaseURL: old.BaseURL, Enabled: old.Enabled, TimeoutSeconds: old.TimeoutSeconds, MaxConcurrency: old.MaxConcurrency, ExpectedVersion: old.Version - 1}
		if _, e = service.UpdateConnection(ctx, conn.ID, change); !errors.Is(e, ErrConflict) {
			t.Fatal("stale connection PATCH overwritten")
		}
		messages, e := service.ListMessages(ctx, 11, session.ID)
		must(t, e)
		if len(messages) != 2 {
			t.Fatal("message listing")
		}
	})
	t.Run("concurrent idempotent same request", func(t *testing.T) {
		freshSession, e := service.CreateSession(ctx, 11, model.ID, "dedupe")
		must(t, e)
		defer service.DeleteSession(ctx, 11, freshSession.ID)
		req := MessageInput{Text: "race", RequestID: uuid.NewString()}
		var wg sync.WaitGroup
		ids := make(chan string, 8)
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				svc := service
				if i%2 == 1 {
					svc = other
				}
				v, _, e := svc.BeginInvocation(ctx, 11, freshSession.ID, req)
				if e != nil {
					errs <- e
					return
				}
				ids <- v.ID
			}(i)
		}
		wg.Wait()
		close(ids)
		close(errs)
		for e := range errs {
			t.Errorf("concurrent replay: %v", e)
		}
		expected := ""
		for id := range ids {
			if expected == "" {
				expected = id
			}
			if id != expected {
				t.Fatal("duplicate invocation IDs")
			}
		}
		detail, e := service.GetSession(ctx, 11, freshSession.ID)
		must(t, e)
		if len(detail.Messages) != 1 {
			t.Fatal("duplicate user messages")
		}
		must(t, service.FinishInvocation(ctx, expected, Completion{Status: "failed", ErrorCode: "upstream_auth", ErrorMessage: "secret=do-not-store"}))
		v, e := service.GetInvocation(ctx, 11, expected)
		must(t, e)
		if strings.Contains(v.ErrorMessage, "secret") || v.ErrorCode != "upstream_auth" {
			t.Fatal("error redaction")
		}
	})
	t.Run("connection quota across sessions and instances", func(t *testing.T) {
		var ids []string
		for i := 0; i < 5; i++ {
			v, e := service.CreateSession(ctx, 11, model.ID, "quota")
			must(t, e)
			ids = append(ids, v.ID)
		}
		var wg sync.WaitGroup
		accepted := make(chan *Invocation, 5)
		errs := make(chan error, 5)
		for i, id := range ids {
			wg.Add(1)
			go func(i int, id string) {
				defer wg.Done()
				svc := service
				if i%2 == 1 {
					svc = other
				}
				v, _, e := svc.BeginInvocation(ctx, 11, id, MessageInput{Text: "hello", RequestID: uuid.NewString()})
				if e != nil {
					errs <- e
				} else {
					accepted <- v
				}
			}(i, id)
		}
		wg.Wait()
		close(accepted)
		close(errs)
		admitted := 0
		for v := range accepted {
			admitted++
			must(t, service.FinishInvocation(ctx, v.ID, Completion{Status: "failed"}))
		}
		if admitted != 2 {
			t.Fatalf("connection quota admitted %d, want 2", admitted)
		}
		for e := range errs {
			if !errors.Is(e, ErrLimit) {
				t.Fatalf("unexpected admission error: %v", e)
			}
		}
		for _, id := range ids {
			must(t, service.DeleteSession(ctx, 11, id))
		}
	})
	t.Run("cancellation and stale recovery", func(t *testing.T) {
		v, e := service.CreateSession(ctx, 11, model.ID, "cancel")
		must(t, e)
		call, _, e := service.BeginInvocation(ctx, 11, v.ID, MessageInput{Text: "queued cancel", RequestID: uuid.NewString()})
		must(t, e)
		cancelled, e := service.RequestCancellation(ctx, 11, call.ID)
		must(t, e)
		if cancelled.Status != "cancelled" || !cancelled.CancellationConfirmed || !cancelled.CancelRequested {
			t.Fatal("queued cancellation")
		}
		started, e := service.StartInvocation(ctx, call.ID)
		must(t, e)
		if started {
			t.Fatal("cancelled call started")
		}
		call, _, e = service.BeginInvocation(ctx, 11, v.ID, MessageInput{Text: "running cancel", RequestID: uuid.NewString()})
		must(t, e)
		_, e = service.StartInvocation(ctx, call.ID)
		must(t, e)
		cancelled, e = service.RequestCancellation(ctx, 11, call.ID)
		must(t, e)
		if cancelled.Status != "running" || !cancelled.CancelRequested || cancelled.CancellationConfirmed {
			t.Fatal("false upstream cancellation confirmation")
		}
		_, e = database.Exec("UPDATE ai_invocations SET updated_at=? WHERE id=?", time.Now().UTC().Add(-time.Hour), call.ID)
		must(t, e)
		recovered, e := service.RecoverStaleInvocations(ctx, time.Now().UTC().Add(-time.Minute))
		must(t, e)
		if recovered != 1 {
			t.Fatalf("recovered %d", recovered)
		}
		got, e := service.GetInvocation(ctx, 11, call.ID)
		must(t, e)
		if got.Status != "indeterminate" || got.CancellationConfirmed {
			t.Fatal("false recovery success")
		}
		detail, e := service.GetSession(ctx, 11, v.ID)
		must(t, e)
		for _, m := range detail.Messages {
			if m.Role == "assistant" {
				t.Fatal("partial output in history")
			}
		}
		must(t, service.DeleteSession(ctx, 11, v.ID))
	})
	t.Run("deletion tombstone survives partial storage failure without starving cleanup", func(t *testing.T) {
		bad, e := service.CreateSession(ctx, 11, model.ID, "storage failure")
		must(t, e)
		for _, key := range []string{"gc/first", "gc/fail"} {
			_, e = service.AddAttachment(ctx, 11, bad.ID, AttachmentInput{Name: "x.pdf", MIMEType: "application/pdf", Kind: "pdf", SizeBytes: 1, StorageKey: key})
			must(t, e)
		}
		gcopts := service.optsWithKey()
		gcopts.DeleteObject = func(_ context.Context, key string) error {
			var deleting bool
			if e := database.QueryRow("SELECT deleting FROM ai_test_sessions WHERE id=?", bad.ID).Scan(&deleting); e != nil || !deleting {
				t.Fatal("storage deletion preceded tombstone commit")
			}
			if key == "gc/fail" {
				return errors.New("private object-store failure")
			}
			return nil
		}
		gc, e := NewService(service.Repo, gcopts)
		must(t, e)
		if e = gc.DeleteSession(ctx, 11, bad.ID); !errors.Is(e, ErrUnavailable) {
			t.Fatalf("storage failure: %v", e)
		}
		if _, e = service.GetSession(ctx, 11, bad.ID); !errors.Is(e, ErrNotFound) {
			t.Fatal("partially deleted session remains accessible")
		}
		if _, e = service.RuntimeConfig(ctx, 11, bad.ID); !errors.Is(e, ErrNotFound) {
			t.Fatal("deleting session can execute")
		}
		good, e := service.CreateSession(ctx, 11, model.ID, "must not starve")
		must(t, e)
		_, e = database.Exec("UPDATE ai_test_sessions SET expires_at=? WHERE id=?", time.Now().UTC().Add(-time.Minute), good.ID)
		must(t, e)
		_, e = database.Exec("UPDATE ai_test_sessions SET cleanup_after=? WHERE id=?", time.Now().UTC().Add(-time.Minute), bad.ID)
		must(t, e)
		count, e := gc.CleanupExpired(ctx, 50)
		if count != 1 || !errors.Is(e, ErrUnavailable) {
			t.Fatalf("cleanup did not continue: count=%d err=%v", count, e)
		}
		var attempts int
		var after time.Time
		must(t, database.QueryRow("SELECT cleanup_attempts,cleanup_after FROM ai_test_sessions WHERE id=?", bad.ID).Scan(&attempts, &after))
		if attempts < 2 || !after.After(time.Now().UTC()) {
			t.Fatal("cleanup retry was not backed off")
		}
		later, e := service.CreateSession(ctx, 11, model.ID, "next batch")
		must(t, e)
		_, e = database.Exec("UPDATE ai_test_sessions SET expires_at=? WHERE id=?", time.Now().UTC().Add(-time.Minute), later.ID)
		must(t, e)
		nextCount, e := gc.CleanupExpired(ctx, 1)
		must(t, e)
		if nextCount != 1 {
			t.Fatal("failed tombstone filled the next bounded batch")
		}
		_, e = database.Exec("UPDATE ai_test_sessions SET cleanup_after=? WHERE id=?", time.Now().UTC().Add(-time.Minute), bad.ID)
		must(t, e)
		gcopts.DeleteObject = func(context.Context, string) error { return nil }
		retry, e := NewService(service.Repo, gcopts)
		must(t, e)
		count, e = retry.CleanupExpired(ctx, 50)
		must(t, e)
		if count != 1 {
			t.Fatal("tombstone cleanup retry did not finish")
		}
	})
	t.Run("individual attachment deletion retains durable object cleanup", func(t *testing.T) {
		v, e := service.CreateSession(ctx, 11, model.ID, "attachment delete failure")
		must(t, e)
		a, e := service.AddAttachment(ctx, 11, v.ID, AttachmentInput{Name: "one.pdf", MIMEType: "application/pdf", Kind: "pdf", SizeBytes: 1, StorageKey: "gc/individual"})
		must(t, e)
		opts := service.optsWithKey()
		opts.DeleteObject = func(context.Context, string) error { return errors.New("storage down") }
		broken, e := NewService(service.Repo, opts)
		must(t, e)
		if e = broken.DeleteAttachment(ctx, 11, v.ID, a.ID); !errors.Is(e, ErrUnavailable) {
			t.Fatal("missing attachment storage error")
		}
		if _, e = service.GetAttachment(ctx, 11, v.ID, a.ID); !errors.Is(e, ErrNotFound) {
			t.Fatal("partially deleted attachment remains readable")
		}
		_, e = database.Exec("UPDATE ai_object_deletions SET cleanup_after=? WHERE storage_key=?", time.Now().UTC().Add(-time.Minute), "gc/individual")
		must(t, e)
		opts.DeleteObject = func(context.Context, string) error { return nil }
		fixed, e := NewService(service.Repo, opts)
		must(t, e)
		_, e = fixed.CleanupExpired(ctx, 50)
		must(t, e)
		var pending int
		must(t, database.QueryRow("SELECT COUNT(*) FROM ai_object_deletions WHERE storage_key=?", "gc/individual").Scan(&pending))
		if pending != 0 {
			t.Fatal("object cleanup tombstone was not retried")
		}
		must(t, service.DeleteSession(ctx, 11, v.ID))
	})
	t.Run("attachment deletion and expired cleanup", func(t *testing.T) {
		v, e := service.CreateSession(ctx, 11, model.ID, "expire")
		must(t, e)
		a, e := service.AddAttachment(ctx, 11, v.ID, AttachmentInput{Name: "unused.pdf", MIMEType: "application/pdf", Kind: "pdf", SizeBytes: 12, StorageKey: "ai/integration/unused"})
		must(t, e)
		must(t, service.DeleteAttachment(ctx, 11, v.ID, a.ID))
		_, e = service.AddAttachment(ctx, 11, v.ID, AttachmentInput{Name: "expire.pdf", MIMEType: "application/pdf", Kind: "pdf", SizeBytes: 12, StorageKey: "ai/integration/expire"})
		must(t, e)
		_, e = database.Exec("UPDATE ai_test_sessions SET expires_at=? WHERE id=?", time.Now().UTC().Add(-time.Minute), v.ID)
		must(t, e)
		if _, e = service.GetSession(ctx, 11, v.ID); !errors.Is(e, ErrNotFound) {
			t.Fatal("expired session readable")
		}
		count, e := service.CleanupExpired(ctx, 10)
		must(t, e)
		if count != 1 {
			t.Fatalf("cleanup count %d", count)
		}
	})
	must(t, service.DeleteSession(ctx, 11, session.ID))
	must(t, service.DeleteModel(ctx, model.ID))
	must(t, service.DeleteConnection(ctx, conn.ID))
	if len(deleted) != 3 {
		t.Fatalf("object deletion callbacks %d", len(deleted))
	}
	t.Log("verified migration up/down/up, seeds, encrypted credentials, CRUD/reference guards, owner fences, atomic history, same-request replay, multi-instance connection quota, cancellation, recovery and file cleanup")
}
func (s *Service) optsWithKey() Options {
	o := s.opts
	o.EncryptionKey = "test-only-master-key"
	return o
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
