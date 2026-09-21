package http

import (
	"context"
	"database/sql"
	"errors"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	"github.com/shilin414/cas/backend-go/internal/execution"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/identity"
)

type attachmentCountingBody struct {
	io.Reader
	bytes int64
}

func (b *attachmentCountingBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.bytes += int64(n)
	return n, err
}
func (b *attachmentCountingBody) Close() error { return nil }

func attachmentUploadFixture(t *testing.T) (*Server, sqlmock.Sqlmock, *avatarHandlerTestStorage) {
	t.Helper()
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	m.ExpectQuery("GetExecutionAuthBundle").WillReturnRows(automationHandlerRow(t, db.GetExecutionAuthBundleRow{
		AppID: 42, AppKind: "chat", AppEnabled: true, AppIsPublic: true, BindingID: 1, BindingEnabled: true,
		BindingProviderKey: "feishu_aily", BindingRuntimeType: "agent", ProviderStatus: sql.NullString{String: "active", Valid: true},
	}))
	store := &avatarHandlerTestStorage{putErr: errors.New("unexpected write")}
	return &Server{DB: conn, Catalog: &catalog.Service{DB: conn}, Runs: execution.NewService(conn, nil, nil, nil), Storage: store}, m, store
}

func attachmentSizedRequest(t *testing.T, size int64, kind string) (*http.Request, *attachmentCountingBody) {
	t.Helper()
	prefix := "--exam\r\nContent-Disposition: form-data; name=\"type\"\r\n\r\n" + kind + "\r\n--exam\r\nContent-Disposition: form-data; name=\"file\"; filename=\"test.pdf\"\r\nContent-Type: application/pdf\r\n\r\n"
	body := &attachmentCountingBody{Reader: io.MultiReader(strings.NewReader(prefix), io.LimitReader(zeroReader{}, size), strings.NewReader("\r\n--exam--\r\n"))}
	req := httptest.NewRequest(http.MethodPost, "/api/v2/applications/42/attachments", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=exam")
	req.ContentLength = -1
	req = req.WithContext(context.WithValue(req.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 42, IsActive: true}}))
	t.Cleanup(func() {
		if req.MultipartForm != nil {
			_ = req.MultipartForm.RemoveAll()
		}
	})
	return req, body
}

func TestApplicationAttachmentRejectsOversizeBeforeStorage(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		size       int64
	}{{"file", "file", (40 << 20) + 1}, {"image", "image", (5 << 20) + 1}} {
		t.Run(tc.name, func(t *testing.T) {
			s, m, store := attachmentUploadFixture(t)
			req, _ := attachmentSizedRequest(t, tc.size, tc.kind)
			rec := httptest.NewRecorder()
			s.UploadApplicationAttachment(rec, req, 42)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("status=%d want 413: %s", rec.Code, rec.Body)
			}
			if store.puts != 0 {
				t.Errorf("oversize attachment reached storage %d times", store.puts)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestApplicationAttachmentBoundsUnknownLengthBody(t *testing.T) {
	s, m, store := attachmentUploadFixture(t)
	req, body := attachmentSizedRequest(t, 60<<20, "file")
	rec := httptest.NewRecorder()
	s.UploadApplicationAttachment(rec, req, 42)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d want 413: %s", rec.Code, rec.Body)
	}
	if body.bytes > (40<<20)+(64<<10)+1 {
		t.Errorf("read %d bytes before rejecting upload; request body unbounded", body.bytes)
	}
	if store.puts != 0 {
		t.Errorf("oversize attachment reached storage")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRunAttachmentRejectsOversizeBeforeProvider(t *testing.T) {
	conn, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	id := ids.New()
	m.ExpectQuery("GetRunByID").WithArgs(id.Bytes()).WillReturnRows(automationHandlerRow(t, db.Run{
		ID: id.Bytes(), UserID: sql.NullInt64{Int64: 42, Valid: true}, Status: "queued", Provider: "feishu_aily", RuntimeType: "agent",
	}))
	s := &Server{Runs: execution.NewService(conn, nil, nil, nil)}
	req, _ := attachmentSizedRequest(t, (40<<20)+1, "file")
	rec := httptest.NewRecorder()
	s.UploadRunAttachment(rec, req, uuid.MustParse(id.String()))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d want 413: %s", rec.Code, rec.Body)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReadAttachmentUploadAcceptsBoundariesAndCleansSpillFiles(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMP", temp)
	t.Setenv("TEMP", temp)
	t.Setenv("TMPDIR", temp)
	for _, tc := range []struct {
		name, kind string
		size       int64
	}{{"empty", "file", 0}, {"small", "file", 128}, {"max_image", "image", 5 << 20}, {"max_file", "file", 40 << 20}} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := attachmentSizedRequest(t, tc.size, tc.kind)
			rec := httptest.NewRecorder()
			in, ok := readAttachmentUpload(rec, req)
			if !ok {
				t.Fatalf("valid upload rejected: %d %s", rec.Code, rec.Body)
			}
			if int64(len(in.Data)) != tc.size || in.Filename != "test.pdf" || in.AttachmentType != tc.kind {
				t.Fatalf("wrong parsed attachment: name=%s type=%s size=%d", in.Filename, in.AttachmentType, len(in.Data))
			}
			entries, err := os.ReadDir(temp)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("multipart spill files leaked: %v", entries)
			}
		})
	}
}

func TestReadAttachmentUploadDocURLAndMalformedBody(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"doc_url", "--exam\r\nContent-Disposition: form-data; name=\"doc_url\"\r\n\r\nhttps://example.test/doc\r\n--exam--\r\n", true},
		{"missing", "--exam--\r\n", false},
		{"malformed", "not a multipart body", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "multipart/form-data; boundary=exam")
			rec := httptest.NewRecorder()
			in, ok := readAttachmentUpload(rec, req)
			if ok != tc.ok {
				t.Fatalf("ok=%v status=%d body=%s", ok, rec.Code, rec.Body)
			}
			if tc.ok && (in.DocURL != "https://example.test/doc" || in.AttachmentType != "file") {
				t.Fatalf("doc URL/default type lost: %+v", in)
			}
			if !tc.ok && rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400", rec.Code)
			}
		})
	}
}

func TestApplicationAttachmentKnownLengthRejectedWithoutReading(t *testing.T) {
	s, m, store := attachmentUploadFixture(t)
	req, body := attachmentSizedRequest(t, 60<<20, "file")
	req.ContentLength = 60 << 20
	rec := httptest.NewRecorder()
	s.UploadApplicationAttachment(rec, req, 42)
	if rec.Code != http.StatusRequestEntityTooLarge || body.bytes != 0 || store.puts != 0 {
		t.Fatalf("status=%d read=%d writes=%d", rec.Code, body.bytes, store.puts)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
