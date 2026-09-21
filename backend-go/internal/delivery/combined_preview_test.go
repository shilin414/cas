package delivery

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/execution"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/dbtypes"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
	"github.com/shilin414/cas/backend-go/internal/sharing"
	transport "github.com/shilin414/cas/backend-go/internal/transport/http"
	goredis "github.com/redis/go-redis/v9"
)

func combinedConfig(t *testing.T, base string) *config.Config {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_BASE_PATH", base)
	t.Setenv("PUBLIC_ORIGIN", "https://studio.example")
	t.Setenv("PUBLIC_BASE_URL", "https://obsolete.example/stale")
	// Explicit absent dotenv avoids reading developer credentials from the repo.
	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.env"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://studio.example" + strings.TrimSuffix(base, "/"); cfg.PublicBaseURL != want {
		t.Fatalf("public URL = %q, want %q", cfg.PublicBaseURL, want)
	}
	return cfg
}

type combinedArgument func(driver.Value) bool

func (f combinedArgument) Match(v driver.Value) bool { return f(v) }

// Intercept the only Redis operation exercised by process. Never dial Redis,
// even if a regression introduces another command or bypasses this hook.
type combinedACK struct {
	t     *testing.T
	calls int
}

func (h *combinedACK) DialHook(goredis.DialHook) goredis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		h.t.Error("unexpected Redis dial")
		return nil, fmt.Errorf("network disabled")
	}
}
func (h *combinedACK) ProcessHook(goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		if cmd.Name() != "xack" {
			h.t.Errorf("unexpected Redis command %s", cmd.Name())
			return fmt.Errorf("unexpected command")
		}
		h.calls++
		cmd.(*goredis.IntCmd).SetVal(1)
		return nil
	}
}
func (h *combinedACK) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(context.Context, []goredis.Cmder) error {
		h.t.Error("unexpected Redis pipeline")
		return fmt.Errorf("unexpected pipeline")
	}
}
func combinedRedis(t *testing.T) (*redisx.Client, *combinedACK) {
	t.Helper()
	h := &combinedACK{t: t}
	client := goredis.NewClient(&goredis.Options{Addr: "unused.invalid:1", MaxRetries: -1})
	client.AddHook(h)
	t.Cleanup(func() { _ = client.Close() })
	r := redisx.NewWithPrefix("combined-test")
	r.UniversalClient = client
	return r, h
}

func TestCombinedPreviewConfiguredWorkerToPublicResult(t *testing.T) {
	for _, base := range []string{"/", "/xiaoan-platform/", "/other/nested/"} {
		t.Run(base, func(t *testing.T) {
			cfg := combinedConfig(t, base)
			conn, m, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(want, got string) error {
				err := sqlmock.QueryMatcherRegexp.Match(want, got)
				if err != nil {
					t.Errorf("unexpected database operation: %v", err)
				}
				return err
			})))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			ctx := context.Background()
			now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
			runID, artID := ids.New(), ids.New()
			answer := "CURRENT-RUN-ONLY\n" + strings.Repeat("完整结果正文", 1200) + "\nFULL-RESULT-END"
			output, err := json.Marshal(map[string]string{"text": answer})
			if err != nil {
				t.Fatal(err)
			}
			run := db.Run{ID: runID.Bytes(), UserID: sql.NullInt64{Int64: 7, Valid: true}, ConversationID: sql.NullInt64{Int64: 9, Valid: true}, Status: "succeeded", Output: dbtypes.JSONText(output), CreatedAt: now, UpdatedAt: now, QueuedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true}}
			artifact := db.RunArtifact{ID: artID.Bytes(), RunID: runID.Bytes(), Name: "本次报告.pdf", CreatedAt: now, UpdatedAt: now, CachedExternalUrl: sql.NullString{String: "https://files.invalid/only-this-run.pdf", Valid: true}, CachedUrlExpiresAt: sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true}}
			entries := []sharing.Entry{{Role: "assistant", Content: &answer, Title: "每日巡检", CreatedAt: now, Artifacts: []sharing.ArtifactRef{{ArtifactID: artID.String(), Name: artifact.Name}}}}
			snapshot, err := json.Marshal(entries)
			if err != nil {
				t.Fatal(err)
			}
			var saved []sharing.Entry
			type sentMessage struct {
				MsgType   string `json:"msg_type"`
				Content   string `json:"content"`
				ReceiveID string `json:"receive_id"`
			}
			sends := make(chan sentMessage, 10)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body sentMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				expectedType := map[string]string{"ou_1": "open_id", "oc_1": "chat_id"}[body.ReceiveID]
				if expectedType == "" || r.URL.Query().Get("receive_id_type") != expectedType {
					t.Errorf("wrong recipient route: %s %s", r.URL, body.ReceiveID)
				}
				if r.Header.Get("Authorization") != "Bearer owner-test-uat" {
					t.Error("owner token missing")
				}
				sends <- body
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
			}))
			defer provider.Close()
			rdb, acks := combinedRedis(t)
			worker := NewWorker(conn, rdb, "combined", &FeishuSender{Client: identity.NewFeishuClient(provider.URL, "test-id", "test-secret", provider.Client()), Auth: &staticAuth{token: "owner-test-uat"}}, nil, nil, nil)
			worker.PublicBaseURL = cfg.PublicBaseURL
			for i, target := range []Target{{Type: TargetUser, ID: "ou_1"}, {Type: TargetChat, ID: "oc_1"}} {
				row := db.DeliveryExecution{ID: ids.New().Bytes(), RunID: runID.Bytes(), OccurrenceID: 5, SenderUserID: 7, TargetType: target.Type, TargetID: target.ID, Status: "pending", CreatedAt: now, UpdatedAt: now}
				m.ExpectQuery("GetDeliveryExecutionByID").WithArgs(row.ID).WillReturnRows(previewRow(t, row))
				m.ExpectExec("CASClaimDelivery").WithArgs(row.ID).WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectQuery("GetScheduleOccurrenceByID").WithArgs(5).WillReturnRows(previewRow(t, db.ScheduleOccurrence{ID: 5, ScheduleID: 3, ScheduledAt: now, CreatedAt: now, UpdatedAt: now}))
				m.ExpectQuery("GetScheduleByID").WithArgs(3).WillReturnRows(previewRow(t, db.Schedule{ID: 3, Name: "每日巡检", OwnerUserID: 7, CreatedAt: now, UpdatedAt: now}))
				m.ExpectQuery("GetRunByID").WithArgs(runID.Bytes()).WillReturnRows(previewRow(t, run))
				if i == 0 {
					m.ExpectQuery("GetRunResultShare").WithArgs(string(runID.Bytes())).WillReturnError(sql.ErrNoRows)
					// No conversation-message query is accepted: only this run's artifacts.
					m.ExpectQuery("ListRunArtifacts").WithArgs(runID.Bytes()).WillReturnRows(previewRow(t, artifact))
					m.ExpectExec("CreateRunResultShare").WithArgs(sqlmock.AnyArg(), 9, 7, combinedArgument(func(v driver.Value) bool {
						raw, ok := v.(string)
						if !ok {
							return false
						}
						return json.Unmarshal([]byte(raw), &saved) == nil && reflect.DeepEqual(saved, entries)
					}), string(runID.Bytes())).WillReturnResult(sqlmock.NewResult(1, 1))
				}
				// Model the persisted winner (also valid when another fan-out inserts first).
				m.ExpectQuery("GetRunResultShare").WithArgs(string(runID.Bytes())).WillReturnRows(sqlmock.NewRows([]string{"token", "snapshot", "revoked_at"}).AddRow("token", snapshot, nil))
				m.ExpectExec("CASFinishDelivery").WithArgs("succeeded", "", "", nil, "succeeded", row.ID).WillReturnResult(sqlmock.NewResult(0, 1))
				msg := goredis.XMessage{ID: fmt.Sprintf("%d-0", i+1), Values: map[string]any{"run_id": ids.ID(row.ID).Hex()}}
				worker.process(ctx, msg)
				// A duplicate queue delivery must not send again or mint another share.
				row.Status = "succeeded"
				m.ExpectQuery("GetDeliveryExecutionByID").WithArgs(row.ID).WillReturnRows(previewRow(t, row))
				worker.process(ctx, msg)
			}
			if acks.calls != 4 || len(sends) != 2 {
				t.Fatalf("acks=%d sends=%d, want 4 and 2", acks.calls, len(sends))
			}
			recipients := map[string]int{}
			for len(sends) > 0 {
				sent := <-sends
				recipients[sent.ReceiveID]++
				var card struct {
					Elements []struct {
						Actions []struct {
							URL string `json:"url"`
						} `json:"actions"`
					} `json:"elements"`
				}
				if err := json.Unmarshal([]byte(sent.Content), &card); err != nil {
					t.Fatal(err)
				}
				var urls []string
				for _, element := range card.Elements {
					for _, action := range element.Actions {
						urls = append(urls, action.URL)
					}
				}
				if sent.MsgType != "interactive" || !reflect.DeepEqual(urls, []string{"https://studio.example" + base + "share/token"}) {
					t.Fatalf("wrong card type/URL: %s %v", sent.MsgType, urls)
				}
				if !strings.Contains(sent.Content, "CURRENT-RUN-ONLY") || strings.Contains(sent.Content, "FULL-RESULT-END") || len(sent.Content) > 12000 {
					t.Fatal("preview must contain only a bounded excerpt")
				}
			}
			if recipients["ou_1"] != 1 || recipients["oc_1"] != 1 || !reflect.DeepEqual(saved, entries) {
				t.Fatal("fan-out or persisted full snapshot differs")
			}

			// Read through real public routes, as nginx does after stripping the prefix.
			server := &transport.Server{Config: cfg, Runs: execution.NewService(conn, nil, nil, nil)}
			router := http.StripPrefix(strings.TrimSuffix(base, "/"), genapi.Handler(server))
			expectShare := func() {
				m.ExpectQuery("GetConversationShareByToken").WithArgs("token").WillReturnRows(sqlmock.NewRows([]string{"id", "token", "conversation_id", "user_id", "snapshot", "created_at", "revoked_at"}).AddRow(1, "token", 9, 7, snapshot, now, nil))
			}
			expectShare()
			m.ExpectQuery("GetConversationByID").WithArgs(9).WillReturnRows(previewRow(t, db.Conversation{ID: 9, UserID: 7, Title: "REUSED-CONVERSATION-HISTORY", CreatedAt: now, UpdatedAt: now}))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", base+"api/v2/public/shares/token", nil))
			var full struct {
				Title    string            `json:"title"`
				Messages []sharing.Message `json:"messages"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 200 || full.Title != "每日巡检" || len(full.Messages) != 1 || full.Messages[0].Content != answer || !reflect.DeepEqual(full.Messages[0].Artifacts, entries[0].Artifacts) {
				t.Fatalf("full result changed: status=%d messages=%d", rec.Code, len(full.Messages))
			}
			if strings.Contains(rec.Body.String(), "REUSED-CONVERSATION-HISTORY") {
				t.Fatal("reused conversation leaked")
			}
			expectShare()
			m.ExpectQuery("GetRunArtifactByID").WithArgs(artID.Bytes()).WillReturnRows(previewRow(t, artifact))
			m.ExpectQuery("GetRunByID").WithArgs(runID.Bytes()).WillReturnRows(previewRow(t, run))
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", base+"api/v2/public/shares/token/artifacts/"+artID.String()+"/open", nil))
			if rec.Code != 302 || rec.Header().Get("Location") != artifact.CachedExternalUrl.String {
				t.Fatalf("own artifact route: %d %s", rec.Code, rec.Body.String())
			}
			expectShare()
			rec = httptest.NewRecorder()
			// A forbidden DB read must fail the test, not masquerade as a 404.
			m.ExpectQuery("^SELECT 'combined_read_boundary'$").WillReturnRows(sqlmock.NewRows([]string{"boundary"}).AddRow("ok"))
			router.ServeHTTP(rec, httptest.NewRequest("GET", base+"api/v2/public/shares/token/artifacts/"+ids.New().String()+"/open", nil))
			var boundary string
			if err := conn.QueryRow("SELECT 'combined_read_boundary'").Scan(&boundary); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 404 {
				t.Fatalf("unshared artifact accessible: %d", rec.Code)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCombinedPreviewPredicateMismatchRemainsSkippedAtWorker(t *testing.T) {
	for _, base := range []string{"/", "/xiaoan-platform/", "/other/nested/"} {
		t.Run(base, func(t *testing.T) {
			cfg := combinedConfig(t, base)
			conn, m, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(want, got string) error {
				err := sqlmock.QueryMatcherRegexp.Match(want, got)
				if err != nil {
					t.Errorf("unexpected database operation: %v", err)
				}
				return err
			})))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			now := time.Now()
			runID := ids.New()
			occurrenceID, userID := int64(5), int64(7)
			var deliveryID []byte
			m.ExpectBegin()
			m.ExpectQuery("GetScheduleOccurrenceByID").WithArgs(5).WillReturnRows(previewRow(t, db.ScheduleOccurrence{ID: 5, ScheduleID: 3, ScheduledAt: now, CreatedAt: now, UpdatedAt: now, DeliverySnapshotAt: sql.NullTime{Time: now, Valid: true}}))
			m.ExpectQuery("ListOccurrenceDeliveryExpectations").WithArgs(5).WillReturnRows(previewRow(t, db.OccurrenceDeliveryExpectation{OccurrenceID: 5, ScheduleDeliveryID: 11, Channel: "feishu", TargetType: TargetUser, TargetID: "ou_1", CreatedAt: now, ConditionOperator: "contains", ConditionText: sql.NullString{String: "Alert", Valid: true}}))
			m.ExpectExec("CreateDeliveryExecution").WithArgs(combinedArgument(func(v driver.Value) bool {
				b, ok := v.([]byte)
				deliveryID = append([]byte(nil), b...)
				return ok && len(b) == 16
			}), 5, runID.Bytes(), 11, 7, TargetUser, "ou_1").WillReturnResult(sqlmock.NewResult(1, 1))
			// Pin terminal accounting, not just the generated query's comment/name.
			m.ExpectExec("(?s)UPDATE delivery_executions.*status = 'skipped'.*error_code = 'condition_not_met'").WithArgs(combinedArgument(func(v driver.Value) bool { return reflect.DeepEqual(v, deliveryID) })).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
			tx, err := conn.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			run := &execution.Run{ID: runID, UserID: &userID, TriggerID: &occurrenceID, Input: map[string]any{"prompt": "Alert"}, Output: map[string]any{"text": "normal", "tools": []string{"Alert"}}}
			if err := NewDispatcher(conn, nil, nil).CreateInTx(context.Background(), tx, run); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			// No outbox, CAS claim, result share, or send is permitted. A stale queue
			// event must be ACKed at the skipped guard, not fall into the sending path.
			row := db.DeliveryExecution{ID: deliveryID, RunID: runID.Bytes(), OccurrenceID: 5, SenderUserID: 7, TargetType: TargetUser, TargetID: "ou_1", Status: "skipped", ErrorCode: "condition_not_met", CreatedAt: now, UpdatedAt: now}
			m.ExpectQuery("GetDeliveryExecutionByID").WithArgs(deliveryID).WillReturnRows(previewRow(t, row))
			sender := &fakeSender{}
			rdb, acks := combinedRedis(t)
			worker := NewWorker(conn, rdb, "combined", sender, nil, nil, nil)
			worker.PublicBaseURL = cfg.PublicBaseURL
			worker.process(context.Background(), goredis.XMessage{ID: "1-0", Values: map[string]any{"run_id": ids.ID(deliveryID).Hex()}})
			if sender.calls != 0 || acks.calls != 1 {
				t.Fatalf("skipped sends=%d acks=%d", sender.calls, acks.calls)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
