package http

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/operations"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type operationsFake struct {
	calls  int
	actor  int64
	filter operations.RunFilter
	err    error
}

func (f *operationsFake) Overview(context.Context) (*operations.Overview, error) {
	f.calls++
	return &operations.Overview{Providers: []operations.Provider{}, Alerts: []operations.Alert{}}, f.err
}
func (f *operationsFake) ListRuns(_ context.Context, in operations.RunFilter) (*operations.RunPage, error) {
	f.calls++
	f.filter = in
	return &operations.RunPage{Results: []operations.Run{}}, f.err
}
func (f *operationsFake) UpdateCapacity(_ context.Context, actor int64, _ string, _ operations.CapacityInput) (*operations.CapacityResult, error) {
	f.calls++
	f.actor = actor
	return &operations.CapacityResult{EffectiveFor: "new_admissions"}, f.err
}
func operationRequest(method, path, body string, user *identity.User) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, &AuthenticatedUser{User: user}))
	}
	return r
}
func TestOperationsRoutesRequireAdministrativePermissions(t *testing.T) {
	for _, methodPath := range [][2]string{{"GET", "/api/v2/admin/operations/overview"}, {"GET", "/api/v2/admin/operations/runs"}, {"PATCH", "/api/v2/admin/operations/providers/feishu_aily/capacity"}} {
		for _, user := range []*identity.User{nil, {ID: 7}} {
			fake := &operationsFake{}
			s := &Server{Operations: fake}
			router := chi.NewRouter()
			s.registerOperationsRoutes(router)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, operationRequest(methodPath[0], methodPath[1], `{"max_inflight":10,"expected_max_inflight":20,"reason":"test"}`, user))
			want := 403
			if user == nil {
				want = 401
			}
			if w.Code != want || fake.calls != 0 {
				t.Fatalf("%s status=%d calls=%d", methodPath, w.Code, fake.calls)
			}
		}
	}
}
func TestOperationsCapacityInputIsBoundedAndStrict(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{}`, `{"max_inflight":0,"expected_max_inflight":20,"reason":"x"}`, `{"max_inflight":10,"expected_max_inflight":20,"reason":""}`, `{"max_inflight":10,"expected_max_inflight":20,"reason":"x","unknown":1}`, `{"max_inflight":10,"expected_max_inflight":20,"reason":"x"} {}`, `{"reason":"` + strings.Repeat("x", 9000) + `"}`} {
		fake := &operationsFake{}
		s := &Server{Operations: fake}
		r := chi.NewRouter()
		s.registerOperationsRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, operationRequest("PATCH", "/api/v2/admin/operations/providers/feishu_aily/capacity", body, &identity.User{ID: 7, IsStaff: true}))
		if w.Code != 400 && w.Code != 413 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		if fake.calls != 0 {
			t.Fatal("invalid input reached mutation")
		}
	}
}
func TestOperationsMetadataFiltersConflictAndRedactedErrors(t *testing.T) {
	fake := &operationsFake{}
	s := &Server{Operations: fake}
	r := chi.NewRouter()
	s.registerOperationsRoutes(r)
	admin := &identity.User{ID: 7, IsStaff: true}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, operationRequest("GET", "/api/v2/admin/operations/runs?owner_user_id=9007199254740993&limit=20&status=queued", "", admin))
	if w.Code != 200 || fake.filter.OwnerUserID != "9007199254740993" || fake.filter.Limit != 20 {
		t.Fatalf("status=%d filter=%+v", w.Code, fake.filter)
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=-1", "limit=NaN", "extra=x", "status=queued&status=running"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, operationRequest("GET", "/api/v2/admin/operations/runs?"+query, "", admin))
		if w.Code != 400 {
			t.Fatalf("query=%s status=%d", query, w.Code)
		}
	}
	fake.err = operations.ErrConflict
	w = httptest.NewRecorder()
	r.ServeHTTP(w, operationRequest("PATCH", "/api/v2/admin/operations/providers/feishu_aily/capacity", `{"max_inflight":10,"expected_max_inflight":20,"reason":"test"}`, admin))
	if w.Code != 409 || fake.actor != 7 {
		t.Fatalf("status=%d actor=%d", w.Code, fake.actor)
	}
	fake.err = errors.New("SECRET_DB_PASSWORD stacktrace")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, operationRequest("GET", "/api/v2/admin/operations/overview", "", admin))
	if w.Code != 503 || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}
func TestNormalizeOperationsRoutes(t *testing.T) {
	r := httptest.NewRequest("PATCH", "/api/v2/admin/operations/providers/feishu_aily/capacity", nil)
	if got := normalizeRoute(r); got != "/api/v2/admin/operations/providers/{provider}/capacity" {
		t.Fatal(got)
	}
}

func TestOperationsGranularPermissionsAndAuthErrors(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fake := &operationsFake{}
	s := &Server{Operations: fake, AdminRBAC: &adminrbac.Service{DB: conn, Enabled: true}}
	r := chi.NewRouter()
	s.registerOperationsRoutes(r)
	user := &identity.User{ID: 7}
	m.ExpectQuery("SELECT EXISTS").WithArgs(int64(7), "run.monitor.read").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, operationRequest("GET", "/api/v2/admin/operations/overview", "", user))
	if w.Code != 200 {
		t.Fatalf("read status=%d", w.Code)
	}
	m.ExpectQuery("SELECT EXISTS").WithArgs(int64(7), "provider.manage").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(false))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, operationRequest("PATCH", "/api/v2/admin/operations/providers/feishu_aily/capacity", `{"max_inflight":10,"expected_max_inflight":20,"reason":"test"}`, user))
	if w.Code != 403 || fake.calls != 1 {
		t.Fatalf("write status=%d calls=%d", w.Code, fake.calls)
	}
	m.ExpectQuery("SELECT EXISTS").WillReturnError(errors.New("SECRET_AUTH_DATABASE"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, operationRequest("GET", "/api/v2/admin/operations/overview", "", user))
	if w.Code != 503 || strings.Contains(w.Body.String(), "SECRET") {
		t.Fatalf("auth failure exposed: %d %s", w.Code, w.Body)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestOperationsCapacityRemainsBehindCSRF(t *testing.T) {
	for _, valid := range []bool{false, true} {
		fake := &operationsFake{}
		s := &Server{Operations: fake}
		r := chi.NewRouter()
		r.Use(CSRF(&fakeSessionStore{}, false))
		s.registerOperationsRoutes(r)
		req := operationRequest("PATCH", "/api/v2/admin/operations/providers/feishu_aily/capacity", `{"max_inflight":10,"expected_max_inflight":20,"reason":"test"}`, &identity.User{ID: 7, IsStaff: true})
		if valid {
			token := strings.Repeat("a", 64)
			req.AddCookie(&http.Cookie{Name: "studio_csrf", Value: token})
			req.Header.Set("X-CSRF-Token", token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if valid {
			if w.Code != 200 || fake.calls != 1 {
				t.Fatalf("valid=%d calls=%d", w.Code, fake.calls)
			}
		} else if w.Code != 403 || fake.calls != 0 {
			t.Fatalf("unsafe=%d calls=%d", w.Code, fake.calls)
		}
	}
}
