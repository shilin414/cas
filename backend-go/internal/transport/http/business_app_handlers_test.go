package http

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/businessapps"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	"github.com/shilin414/cas/backend-go/internal/execution"
	dbgen "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/go-chi/chi/v5"
)

func expectBusinessBundle(mock sqlmock.Sqlmock, key string, enabled bool) {
	typ := reflect.TypeOf(dbgen.GetConsumptionBundleRow{})
	columns := make([]string, typ.NumField())
	values := make([]driver.Value, typ.NumField())
	for n := 0; n < typ.NumField(); n++ {
		f := typ.Field(n)
		columns[n] = f.Name
		switch f.Type.Kind() {
		case reflect.String:
			values[n] = ""
		case reflect.Uint64, reflect.Uint32:
			values[n] = int64(0)
		case reflect.Bool:
			values[n] = false
		}
		if f.Type == reflect.TypeOf(time.Time{}) {
			values[n] = time.Now()
		}
	}
	overrides := map[string]driver.Value{"ID": int64(1), "Slug": key, "Kind": "form", "RendererKey": key, "Enabled": enabled, "Tags": "[]", "DefaultConfig": "{}"}
	for n, name := range columns {
		if v, ok := overrides[name]; ok {
			values[n] = v
		}
	}
	mock.ExpectQuery("SELECT a.id, a.slug").WithArgs(uint64(1)).WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
}
func businessRequest(body string, caller *AuthenticatedUser) *http.Request {
	r := httptest.NewRequest("POST", "/api/v2/applications/1/business/execute", strings.NewReader(body))
	route := chi.NewRouteContext()
	route.URLParams.Add("id", "1")
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, route)
	if caller != nil {
		ctx = context.WithValue(ctx, userCtxKey, caller)
	}
	return r.WithContext(ctx)
}
func TestBusinessAppAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name         string
		caller       *AuthenticatedUser
		enabled, acl bool
		body         string
		want         int
	}{
		{"anonymous", nil, true, true, `{}`, 401},
		{"disabled even staff", &AuthenticatedUser{User: &identity.User{ID: 7, IsStaff: true}}, false, true, `{}`, 404},
		{"denied ACL", &AuthenticatedUser{User: &identity.User{ID: 7}}, true, false, `{}`, 404},
		{"target spoof", &AuthenticatedUser{User: &identity.User{ID: 7}}, true, true, `{"confirmed":true,"usercode":"other"}`, 403},
		{"unknown field", &AuthenticatedUser{User: &identity.User{ID: 7}}, true, true, `{"confirmed":true,"endpoint":"evil"}`, 400},
		{"trailing JSON", &AuthenticatedUser{User: &identity.User{ID: 7}}, true, true, `{"confirmed":true} {}`, 400},
		{"confirmation required", &AuthenticatedUser{User: &identity.User{ID: 7}}, true, true, `{}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			s := &Server{DB: db, CatalogRepo: &catalog.Repo{DB: db}, BusinessApps: businessapps.New(businessapps.Config{Accounts: map[string]string{"7": "001"}})}
			if tc.caller != nil {
				expectBusinessBundle(mock, "oa-unlock", tc.enabled)
				if tc.enabled && !tc.caller.IsStaff {
					mock.ExpectQuery("SELECT EXISTS").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(tc.acl))
				}
			}
			w := httptest.NewRecorder()
			s.businessAppExecute(w, businessRequest(tc.body, tc.caller))
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Error("missing no-store")
			}
			if e := mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestBusinessAppAuditBeforeMutation(t *testing.T) {
	for _, auditOK := range []bool{false, true} {
		t.Run(strconv.FormatBool(auditOK), func(t *testing.T) {
			calls := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"code":200,"mess":""}`))
			}))
			defer up.Close()
			db, mock, _ := sqlmock.New()
			defer db.Close()
			s := &Server{DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), CatalogRepo: &catalog.Repo{DB: db}, BusinessApps: businessapps.New(businessapps.Config{OAUnlockURL: up.URL, OAAppID: "app", OASecret: "secret", Accounts: map[string]string{"7": "001"}}), BusinessAppLimiter: execution.NewRateLimiter(nil, "test", 5, time.Minute)}
			expectBusinessBundle(mock, "oa-unlock", true)
			mock.ExpectQuery("SELECT EXISTS").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(true))
			q := mock.ExpectExec("INSERT INTO audit_logs").WithArgs(int64(7), "1", `{"action":"","outcome":"requested","target_account":"001"}`)
			if auditOK {
				q.WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("INSERT INTO audit_logs").WithArgs(int64(7), "1", `{"action":"","outcome":"succeeded","target_account":"001"}`).WillReturnResult(sqlmock.NewResult(2, 1))
			} else {
				q.WillReturnError(errors.New("db down"))
			}
			w := httptest.NewRecorder()
			s.businessAppExecute(w, businessRequest(`{"confirmed":true}`, &AuthenticatedUser{User: &identity.User{ID: 7}}))
			if auditOK && (w.Code != 200 || calls != 1) {
				t.Fatal(w.Code, calls, w.Body)
			}
			if !auditOK && (w.Code != 503 || calls != 0) {
				t.Fatal(w.Code, calls)
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("secret leak")
			}
			if e := mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestBusinessAppAssignedACLForOrdinaryUsers(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(strconv.FormatBool(linked), func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			s := &Server{DB: db, CatalogRepo: &catalog.Repo{DB: db, ACLEnabled: true}, BusinessApps: businessapps.New(businessapps.Config{})}
			expectBusinessBundle(mock, "oa-password", true)
			mock.ExpectQuery("SELECT id,name,enabled,access_mode FROM applications").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "enabled", "access_mode"}).AddRow(1, "OA", "1", "assigned"))
			linkedRows := sqlmock.NewRows([]string{"id"})
			if linked {
				linkedRows.AddRow(70)
			}
			mock.ExpectQuery("SELECT id FROM directory_users WHERE local_user_id").WithArgs(int64(7)).WillReturnRows(linkedRows)
			if linked {
				mock.ExpectQuery("SELECT id,name,is_active,is_resigned,active_status,local_user_id FROM directory_users").WithArgs(int64(70)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "is_active", "is_resigned", "active_status", "local_user_id"}).AddRow(70, "Tester", true, false, 2, 7))
				mock.ExpectQuery("SELECT is_staff FROM users").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"is_staff"}).AddRow(false))
				mock.ExpectQuery("SELECT du.id,du.name FROM application_user_grants").WithArgs(int64(1), int64(70)).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(70, "Tester"))
			}
			w := httptest.NewRecorder()
			s.businessAppStatus(w, businessRequest("", &AuthenticatedUser{User: &identity.User{ID: 7}}))
			want := 404
			if linked {
				want = 200
			}
			if w.Code != want {
				t.Fatal(w.Code, w.Body)
			}
			if e := mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
