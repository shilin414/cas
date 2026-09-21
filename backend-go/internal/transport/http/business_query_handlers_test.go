package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/businessapps"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	"github.com/shilin414/cas/backend-go/internal/execution"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/go-chi/chi/v5"
)

type fakeQuerySnapshots struct {
	mu       sync.Mutex
	snapshot *businessapps.QuerySnapshot
	states   map[string]string
	fail     bool
}

func (f *fakeQuerySnapshots) Put(_ context.Context, s *businessapps.QuerySnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("cache down")
	}
	f.snapshot = s
	return nil
}
func (f *fakeQuerySnapshots) Get(_ context.Context, token string) (*businessapps.QuerySnapshot, error) {
	if f.fail || f.snapshot == nil || f.snapshot.Token != token {
		return nil, businessapps.ErrSnapshotUnavailable
	}
	return f.snapshot, nil
}
func (f *fakeQuerySnapshots) Claim(_ context.Context, _ string, key string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.states == nil {
		f.states = map[string]string{}
	}
	if value := f.states[key]; value != "" {
		return value, "", nil
	}
	f.states[key] = "busy"
	return "claimed", "lease", nil
}
func (f *fakeQuerySnapshots) Finish(_ context.Context, _ string, key, _ string, outcome string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if outcome != "failed" {
		f.states[key] = outcome
	} else {
		delete(f.states, key)
	}
	return nil
}
func querySnapshot(t *testing.T) *businessapps.QuerySnapshot {
	t.Helper()
	snapshot, e := businessapps.NewQuerySnapshot(7, 1, "barcode-query", "barcode-query", "条码信息查询", businessapps.Input{Action: "flow", Barcode: "01234567890123456789"}, json.RawMessage(`"工厂: H010"`), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	return snapshot
}
func queryRequest(body, token string, owner int64) *http.Request {
	r := businessRequest(body, &AuthenticatedUser{User: &identity.User{ID: owner, IsStaff: true, DisplayName: "Tester"}})
	chi.RouteContext(r.Context()).URLParams.Add("token", token)
	return r
}
func TestQueryForwardPartialRetryAndTrustedContent(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	snapshot := querySnapshot(t)
	store := &fakeQuerySnapshots{snapshot: snapshot}
	var mu sync.Mutex
	calls := map[string]int{}
	uuids := map[string]string{}
	failGroup := true
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		defer mu.Unlock()
		id := payload["receive_id"]
		calls[id]++
		if old := uuids[id]; old != "" && old != payload["uuid"] {
			t.Error("uuid changed on retry")
		}
		uuids[id] = payload["uuid"]
		if r.Header.Get("Authorization") != "Bearer uat" {
			t.Error("not user identity")
		}
		if !strings.Contains(payload["content"], "/base/app/barcode-query?result=") || !strings.Contains(payload["content"], "01234567890123456789") {
			t.Error("wrong card")
		}
		if id == "oc-group" && failGroup {
			_, _ = w.Write([]byte(`{"code":999,"msg":"failure"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer up.Close()
	s := &Server{DB: db, CatalogRepo: &catalog.Repo{DB: db}, BusinessSnapshots: store, BusinessForwardLimiter: execution.NewRateLimiter(nil, "test", 100, time.Minute), Config: &config.Config{PublicBaseURL: "https://example.test/base/"}, FeishuAuth: fakeFeishuTokenResolver{token: "uat"}, Feishu: identity.NewFeishuClient(up.URL, "id", "secret", up.Client())}
	body := `{"snapshot_token":"` + snapshot.Token + `","targets":[{"target_type":"user","id":"ou-user"},{"target_type":"chat","id":"oc-group"}]}`
	for i := 0; i < 2; i++ {
		expectBusinessBundle(mock, "barcode-query", true)
		w := httptest.NewRecorder()
		s.forwardBusinessQuery(w, queryRequest(body, "", 7))
		var response struct {
			Success int `json:"success_count"`
			Fail    int `json:"fail_count"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &response)
		if w.Code != 200 || response.Success != i+1 {
			t.Fatal(w.Code, w.Body)
		}
		mu.Lock()
		failGroup = false
		mu.Unlock()
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["ou-user"] != 1 || calls["oc-group"] != 2 {
		t.Fatal(calls)
	}
	if len(uuids["ou-user"]) != 32 {
		t.Fatal("missing provider dedupe")
	}
	if e := mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestQueryForwardRejectsWrongOwnerExpiredForgedAndDuplicate(t *testing.T) {
	for _, kind := range []string{"owner", "expired", "tampered", "duplicate", "password-app"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			snapshot := querySnapshot(t)
			owner := int64(7)
			key := "barcode-query"
			want := 400
			if kind == "owner" {
				owner = 8
				want = 404
			}
			if kind == "expired" {
				snapshot.ExpiresAt = time.Now().Add(-time.Second)
				want = 404
			}
			if kind == "password-app" {
				key = "oa-password"
			}
			s := &Server{CatalogRepo: &catalog.Repo{DB: db}, BusinessSnapshots: &fakeQuerySnapshots{snapshot: snapshot}, BusinessForwardLimiter: execution.NewRateLimiter(nil, "test", 100, time.Minute)}
			targets := `[{"target_type":"user","id":"ou-user"}]`
			if kind == "duplicate" {
				targets = `[{"target_type":"user","id":"ou-user"},{"target_type":"user","id":"ou-user"}]`
			}
			body := `{"snapshot_token":"` + snapshot.Token + `","targets":` + targets
			if kind == "tampered" {
				body += `,"data":"forged"`
			}
			body += `}`
			expectBusinessBundle(mock, key, true)
			w := httptest.NewRecorder()
			s.forwardBusinessQuery(w, queryRequest(body, "", owner))
			if w.Code != want {
				t.Fatal(w.Code, w.Body)
			}
			if e := mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestQuerySnapshotViewerDoesNotAcquireForwardOwnership(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	snapshot := querySnapshot(t)
	s := &Server{CatalogRepo: &catalog.Repo{DB: db}, BusinessSnapshots: &fakeQuerySnapshots{snapshot: snapshot}}
	expectBusinessBundle(mock, "barcode-query", true)
	w := httptest.NewRecorder()
	s.getBusinessQuerySnapshot(w, queryRequest("", snapshot.Token, 8))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"can_forward":false`) || strings.Contains(w.Body.String(), "owner_id") {
		t.Fatal(w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cached data")
	}
}
func TestSuccessfulQuerySnapshotCacheFailureIsNonFatal(t *testing.T) {
	s := &Server{BusinessSnapshots: &fakeQuerySnapshots{fail: true}}
	result := businessapps.Result{Data: "successful result", Message: "查询完成"}
	r := httptest.NewRequest("POST", "/", nil)
	s.attachBusinessQuerySnapshot(r, &catalog.Application{ID: 1, Slug: "barcode-query", RendererKey: "barcode-query"}, &AuthenticatedUser{User: &identity.User{ID: 7}}, businessapps.Input{Action: "production", Barcode: "01234567890123456789"}, &result)
	if result.Data != "successful result" || result.Snapshot != nil || result.ForwardUnavailable == "" {
		t.Fatal(result)
	}
}

func TestQuerySnapshotReadAuthorizationMatrix(t *testing.T) {
	for _, kind := range []string{"anonymous", "no-acl", "disabled", "cross-app", "allowed-viewer"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			snapshot := querySnapshot(t)
			if kind == "cross-app" {
				snapshot.ApplicationID = 99
			}
			s := &Server{CatalogRepo: &catalog.Repo{DB: db}, BusinessSnapshots: &fakeQuerySnapshots{snapshot: snapshot}}
			r := queryRequest("", snapshot.Token, 8)
			userFrom(r.Context()).IsStaff = false
			want := 404
			if kind == "anonymous" {
				r = businessRequest("", nil)
				want = 401
			} else {
				expectBusinessBundle(mock, "barcode-query", kind != "disabled")
				if kind != "disabled" {
					mock.ExpectQuery("SELECT EXISTS").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(kind != "no-acl"))
				}
				if kind == "allowed-viewer" {
					want = 200
				}
			}
			w := httptest.NewRecorder()
			s.getBusinessQuerySnapshot(w, r)
			if w.Code != want {
				t.Fatal(w.Code, w.Body)
			}
			if e := mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestUncertainSendIsNotRetried(t *testing.T) {
	snapshot := querySnapshot(t)
	store := &fakeQuerySnapshots{snapshot: snapshot}
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	}))
	defer up.Close()
	s := &Server{BusinessSnapshots: store, Feishu: identity.NewFeishuClient(up.URL, "id", "secret", up.Client())}
	target := queryForwardTarget{ID: "ou-1", Type: "user"}
	for range 2 {
		result := s.sendBusinessQueryTarget(context.Background(), snapshot, target, "uat", "{}")
		if result.OK || !strings.Contains(result.Error, "不再") {
			t.Fatal(result)
		}
	}
	if calls != 1 {
		t.Fatalf("uncertain message replayed %d times", calls)
	}
}
func TestNon2xxAuthErrorCanBeReauthorized(t *testing.T) {
	snapshot := querySnapshot(t)
	store := &fakeQuerySnapshots{snapshot: snapshot}
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"code":99991668,"msg":"expired"}`))
	}))
	defer up.Close()
	s := &Server{BusinessSnapshots: store, Feishu: identity.NewFeishuClient(up.URL, "id", "secret", up.Client())}
	target := queryForwardTarget{ID: "ou-1", Type: "user"}
	result := s.sendBusinessQueryTarget(context.Background(), snapshot, target, "uat", "{}")
	if result.OK || !strings.Contains(result.Error, "重新绑定") {
		t.Fatal(result)
	}
	state, _, _ := store.Claim(context.Background(), snapshot.Token, "user:ou-1")
	if state != "claimed" || calls != 1 {
		t.Fatal(state, calls)
	}
}

func expectQueryAssignedAccess(mock sqlmock.Sqlmock, granted bool) {
	mock.ExpectQuery("SELECT id,name,enabled,access_mode FROM applications").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "enabled", "access_mode"}).AddRow(1, "查询", true, "assigned"))
	mock.ExpectQuery("SELECT id FROM directory_users WHERE local_user_id").WithArgs(int64(8)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(70))
	mock.ExpectQuery("SELECT id,name,is_active,is_resigned,active_status,local_user_id FROM directory_users").WithArgs(int64(70)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "is_active", "is_resigned", "active_status", "local_user_id"}).AddRow(70, "Recipient", true, false, 2, 8))
	mock.ExpectQuery("SELECT is_staff FROM users").WithArgs(int64(8)).WillReturnRows(sqlmock.NewRows([]string{"is_staff"}).AddRow(false))
	rows := sqlmock.NewRows([]string{"id", "name"})
	if granted {
		rows.AddRow(70, "Recipient")
	}
	mock.ExpectQuery("SELECT du.id,du.name FROM application_user_grants").WithArgs(int64(1), int64(70)).WillReturnRows(rows)
	if !granted {
		mock.ExpectQuery("SELECT dep.id,dep.name,dg.include_children FROM directory_user_departments").WithArgs(int64(70), int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "include_children"}))
		mock.ExpectQuery("SELECT g.id,g.name,g.source_type,g.external_group_type FROM application_group_grants").WithArgs(int64(1), int64(70)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "source_type", "external_group_type"}))
		mock.ExpectQuery("SELECT g.id,g.name,g.source_type,g.external_group_type,gd.include_children FROM application_group_grants").WithArgs(int64(1), int64(70)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "source_type", "external_group_type", "include_children"}))
		mock.ExpectQuery("SELECT g.id,g.name,g.source_type,g.external_group_type FROM application_group_grants").WithArgs(int64(1), int64(70)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "source_type", "external_group_type"}))
	}
}
func TestQuerySnapshotAssignedACLGrantAndRevocation(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	snapshot := querySnapshot(t)
	s := &Server{CatalogRepo: &catalog.Repo{DB: db, ACLEnabled: true}, BusinessSnapshots: &fakeQuerySnapshots{snapshot: snapshot}}
	// An unchanged snapshot must be checked against current enterprise grants on
	// every read: denied initially, visible after grant, hidden after revocation.
	for _, granted := range []bool{false, true, false} {
		expectBusinessBundle(mock, "barcode-query", true)
		expectQueryAssignedAccess(mock, granted)
		r := queryRequest("", snapshot.Token, 8)
		userFrom(r.Context()).IsStaff = false
		w := httptest.NewRecorder()
		s.getBusinessQuerySnapshot(w, r)
		if granted {
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"can_forward":false`) {
				t.Fatal(w.Code, w.Body)
			}
		} else if w.Code != 404 || strings.Contains(w.Body.String(), "工厂") {
			t.Fatal("denied snapshot disclosed", w.Code, w.Body)
		}
	}
	if e := mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
