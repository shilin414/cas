package aimodel

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

const testSession = "11111111-1111-4111-8111-111111111111"
const testModel = "22222222-2222-4222-8222-222222222222"
const testRequest = "33333333-3333-4333-8333-333333333333"

func mockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s, e := NewService(&Repo{DB: db}, Options{EncryptionKey: "test-encryption-key"})
	if e != nil {
		t.Fatal(e)
	}
	s.now = func() time.Time { return time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC) }
	return s, m
}
func TestSessionOwnerFence(t *testing.T) {
	s, m := mockService(t)
	m.ExpectQuery("SELECT .* FROM ai_test_sessions WHERE id=\\? AND owner_id=\\? AND expires_at>\\?").WithArgs(testSession, int64(99), s.now()).WillReturnError(sql.ErrNoRows)
	if _, e := s.GetSession(context.Background(), 99, testSession); !errors.Is(e, ErrNotFound) {
		t.Fatalf("owner fence: %v", e)
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestBeginInvocationLocksOwnerSessionBeforeAnyWrites(t *testing.T) {
	s, m := mockService(t)
	m.ExpectBegin()
	m.ExpectQuery("SELECT .* FROM ai_test_sessions.*FOR UPDATE").WithArgs(testSession, int64(99), s.now()).WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	if _, _, e := s.BeginInvocation(context.Background(), 99, testSession, MessageInput{Text: "hello", RequestID: testRequest}); !errors.Is(e, ErrNotFound) {
		t.Fatalf("owner fence: %v", e)
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestDatabaseErrorsNeverEchoDriverValues(t *testing.T) {
	if got := dbError(errors.New("password=do-not-echo")); got != ErrUnavailable {
		t.Fatalf("unsafe error: %v", got)
	}
}

func TestDeletionFinalCommitFailureKeepsDurableTombstone(t *testing.T) {
	s, m := mockService(t)
	ctx := context.Background()
	now := s.now()
	expires := now.Add(time.Hour)
	sessionRows := func(deleting bool) *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "owner_id", "model_id", "title", "deleting", "cleanup_after", "cleanup_attempts", "active_invocation_id", "created_at", "expires_at"}).AddRow(testSession, 11, testModel, "session", deleting, nil, 0, nil, now, expires)
	}
	m.ExpectBegin()
	m.ExpectQuery("SELECT .* FROM ai_test_sessions.*FOR UPDATE").WithArgs(testSession, int64(11), now).WillReturnRows(sessionRows(false))
	m.ExpectExec("UPDATE ai_test_sessions SET deleting=TRUE").WithArgs(sqlmock.AnyArg(), testSession).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT .* FROM ai_test_sessions WHERE id=\\?").WithArgs(testSession).WillReturnRows(sessionRows(true))
	m.ExpectQuery("SELECT .* FROM ai_test_attachments WHERE session_id=\\?").WithArgs(testSession).WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "message_id", "name", "mime_type", "size_bytes", "kind", "storage_key", "created_at"}).AddRow(testRequest, testSession, nil, "x.png", "image/png", 1, "image", "immutable-key", now))
	deleted := false
	s.opts.DeleteObject = func(context.Context, string) error { deleted = true; return nil }
	m.ExpectBegin()
	m.ExpectQuery("SELECT .* FROM ai_test_sessions WHERE id=\\? FOR UPDATE").WithArgs(testSession).WillReturnRows(sessionRows(true))
	m.ExpectExec("DELETE FROM ai_test_attachments").WithArgs(testSession).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM ai_test_sessions").WithArgs(testSession).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit().WillReturnError(errors.New("commit transport failure"))
	m.ExpectExec("UPDATE ai_test_sessions SET cleanup_after=\\?,cleanup_attempts=cleanup_attempts\\+1").WithArgs(sqlmock.AnyArg(), testSession).WillReturnResult(sqlmock.NewResult(0, 1))
	if e := s.DeleteSession(ctx, 11, testSession); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("commit failure: %v", e)
	}
	if !deleted {
		t.Fatal("storage callback did not run")
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
