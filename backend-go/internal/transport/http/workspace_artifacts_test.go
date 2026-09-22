package http

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"
)

func TestWorkspaceArtifactsOwnerAndApplicationScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{DB: db}
	mock.ExpectQuery(regexp.QuoteMeta(workspaceArtifactSQL)).WithArgs(int64(12), int64(12), int64(7), "", "%%", nil, nil, nil, []byte(nil), 21).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "normalized_type", "created_at", "conversation_id", "title"}).AddRow(make([]byte, 16), "日报.pdf", "file", time.Now(), int64(88), "日报任务"))
	r := httptest.NewRequest("GET", "/api/v2/workspace/artifacts?application_id=7", nil)
	r = r.WithContext(context.WithValue(r.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 12, IsStaff: true}}))
	w := httptest.NewRecorder()
	s.ListWorkspaceArtifacts(w, r, genapi.ListWorkspaceArtifactsParams{ApplicationId: 7})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestWorkspaceArtifactsRejectsInvalidQuery(t *testing.T) {
	s := &Server{}
	bad := "not-a-cursor"
	for _, p := range []genapi.ListWorkspaceArtifactsParams{{ApplicationId: 0}, {ApplicationId: 1, Cursor: &bad}} {
		r := httptest.NewRequest("GET", "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 1}}))
		w := httptest.NewRecorder()
		s.ListWorkspaceArtifacts(w, r, p)
		if w.Code != 400 {
			t.Fatalf("got %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.ListWorkspaceArtifacts(w, httptest.NewRequest("GET", "/", nil), genapi.ListWorkspaceArtifactsParams{ApplicationId: 1})
	if w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestWorkspaceArtifactsPaginationAndLiteralSearch(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := &Server{DB: conn}
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	limit := 1
	query := "%_"
	columns := []string{"id", "name", "normalized_type", "created_at", "conversation_id", "title"}
	id1 := make([]byte, 16)
	id1[15] = 2
	id2 := make([]byte, 16)
	id2[15] = 1
	m.ExpectQuery(regexp.QuoteMeta(workspaceArtifactSQL)).WithArgs(int64(12), int64(12), int64(7), query, `%\%\_%`, nil, nil, nil, []byte(nil), 2).WillReturnRows(sqlmock.NewRows(columns).AddRow(id1, "a.pdf", "file", now, int64(88), "任务").AddRow(id2, "b.pdf", "file", now, int64(88), "任务"))
	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 12}}))
	w := httptest.NewRecorder()
	s.ListWorkspaceArtifacts(w, r, genapi.ListWorkspaceArtifactsParams{ApplicationId: 7, Limit: &limit, Q: &query})
	var page struct {
		Items []workspaceArtifact `json:"items"`
		Next  string              `json:"next_cursor"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Next == "" || page.Items[0].Name != "a.pdf" {
		t.Fatalf("bad page %s", w.Body.String())
	}
	m.ExpectQuery(regexp.QuoteMeta(workspaceArtifactSQL)).WithArgs(int64(12), int64(12), int64(7), query, `%\%\_%`, now, now, now, id1, 2).WillReturnRows(sqlmock.NewRows(columns).AddRow(id2, "b.pdf", "file", now, int64(88), "任务"))
	w = httptest.NewRecorder()
	s.ListWorkspaceArtifacts(w, r, genapi.ListWorkspaceArtifactsParams{ApplicationId: 7, Limit: &limit, Q: &query, Cursor: &page.Next})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
