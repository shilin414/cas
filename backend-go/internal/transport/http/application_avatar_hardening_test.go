package http

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/platform/storage"
)

func TestValidApplicationAvatarBytesDecodesCompleteStaticImages(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	encoders := map[string]func(io.Writer) error{
		"png":  func(w io.Writer) error { return png.Encode(w, img) },
		"jpg":  func(w io.Writer) error { return jpeg.Encode(w, img, nil) },
		"jpeg": func(w io.Writer) error { return jpeg.Encode(w, img, nil) },
		"gif":  func(w io.Writer) error { return gif.Encode(w, img, nil) },
	}
	webpBytes, err := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ/Y/+ByKi/wEA")
	if err != nil {
		t.Fatalf("decode webp fixture: %v", err)
	}
	if _, ok := validApplicationAvatarBytes("webp", webpBytes); !ok {
		t.Fatal("valid webp was rejected")
	}

	for ext, encode := range encoders {
		t.Run(ext, func(t *testing.T) {
			var data bytes.Buffer
			if err := encode(&data); err != nil {
				t.Fatalf("encode %s: %v", ext, err)
			}
			if _, ok := validApplicationAvatarBytes(ext, data.Bytes()); !ok {
				t.Fatalf("valid %s was rejected", ext)
			}
			truncated := data.Bytes()[:data.Len()/2]
			if _, ok := validApplicationAvatarBytes(ext, truncated); ok {
				t.Fatalf("truncated %s was accepted", ext)
			}
		})
	}
}

func TestValidApplicationAvatarBytesRejectsOversizedOrAnimatedImages(t *testing.T) {
	var oversized bytes.Buffer
	if err := png.Encode(&oversized, image.NewRGBA(image.Rect(0, 0, avatarMaxDimension+1, 1))); err != nil {
		t.Fatalf("encode oversized png: %v", err)
	}
	if _, ok := validApplicationAvatarBytes("png", oversized.Bytes()); ok {
		t.Fatal("oversized dimensions were accepted")
	}

	palette := color.Palette{color.Black, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	var animated bytes.Buffer
	if err := gif.EncodeAll(&animated, &gif.GIF{
		Image: []*image.Paletted{frame, frame},
		Delay: []int{1, 1},
	}); err != nil {
		t.Fatalf("encode animated gif: %v", err)
	}
	if _, ok := validApplicationAvatarBytes("gif", animated.Bytes()); ok {
		t.Fatal("animated gif was accepted")
	}

	standaloneVP8X := make([]byte, 30)
	copy(standaloneVP8X, "RIFF")
	binary.LittleEndian.PutUint32(standaloneVP8X[4:8], uint32(len(standaloneVP8X)-8))
	copy(standaloneVP8X[8:12], "WEBP")
	copy(standaloneVP8X[12:16], "VP8X")
	binary.LittleEndian.PutUint32(standaloneVP8X[16:20], 10)
	if _, ok := validApplicationAvatarBytes("webp", standaloneVP8X); ok {
		t.Fatal("header-only webp was accepted")
	}
}

func TestUploadApplicationAvatarRejectsInvalidImageBytesWithoutWriting(t *testing.T) {
	server, dbState, blobStore := newAvatarHandlerTestServer(t, "")
	blobStore.putErr = errors.New("unexpected storage write")
	req := newAvatarUploadRequest(t, "avatar.png", []byte("not actually a png"), 0)
	req = withAvatarTestUser(req)
	recorder := httptest.NewRecorder()

	server.UploadApplicationAvatar(recorder, req, genapi.ApplicationId(42))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if blobStore.puts != 0 {
		t.Fatalf("storage writes=%d, want 0", blobStore.puts)
	}
	if dbState.execs != 0 {
		t.Fatalf("database writes=%d, want 0", dbState.execs)
	}
}

func TestUploadApplicationAvatarRejectsWhenDecoderCapacityIsFull(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	for i := 0; i < cap(avatarDecodeSlots); i++ {
		avatarDecodeSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < cap(avatarDecodeSlots); i++ {
			<-avatarDecodeSlots
		}
	})

	server, dbState, blobStore := newAvatarHandlerTestServer(t, "")
	req := withAvatarTestUser(newAvatarUploadRequest(t, "avatar.png", data.Bytes(), 0))
	recorder := httptest.NewRecorder()
	server.UploadApplicationAvatar(recorder, req, genapi.ApplicationId(42))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("retry-after=%q", got)
	}
	if blobStore.puts != 0 || dbState.execs != 0 {
		t.Fatalf("writes: storage=%d database=%d", blobStore.puts, dbState.execs)
	}
}

func TestUploadApplicationAvatarRejectsOversizedMultipartEnvelope(t *testing.T) {
	server, dbState, blobStore := newAvatarHandlerTestServer(t, "")
	blobStore.putErr = errors.New("unexpected storage write")
	req := newAvatarUploadRequest(t, "avatar.png", []byte("small file"), avatarMultipartMaxBytes)
	// Exercise MaxBytesReader rather than relying on the Content-Length fast path.
	req.ContentLength = -1
	req = withAvatarTestUser(req)
	recorder := httptest.NewRecorder()

	server.UploadApplicationAvatar(recorder, req, genapi.ApplicationId(42))

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if blobStore.puts != 0 {
		t.Fatalf("storage writes=%d, want 0", blobStore.puts)
	}
	if dbState.execs != 0 {
		t.Fatalf("database writes=%d, want 0", dbState.execs)
	}
}

func TestGetApplicationAvatarStreamsCompleteValidatedObject(t *testing.T) {
	avatarKey := "application-avatars/42/avatar.png"
	server, _, blobStore := newAvatarHandlerTestServer(t, avatarKey)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	blobStore.openData = imageData.Bytes()
	blobStore.openObject = storage.Object{Key: avatarKey, Size: int64(imageData.Len())}

	req := withAvatarTestUser(httptest.NewRequest(http.MethodGet, "/api/v2/applications/42/avatar", nil))
	recorder := httptest.NewRecorder()
	server.GetApplicationAvatar(recorder, req, genapi.ApplicationId(42))

	if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), imageData.Bytes()) {
		t.Fatalf("status=%d body-bytes=%d", recorder.Code, recorder.Body.Len())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-cache" {
		t.Fatalf("cache-control=%q", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff=%q", got)
	}
	if !blobStore.readerClosed {
		t.Fatal("storage reader was not closed")
	}
}

func TestGetApplicationAvatarMatchingETagDoesNotReadObject(t *testing.T) {
	avatarKey := "application-avatars/42/avatar.png"
	server, _, blobStore := newAvatarHandlerTestServer(t, avatarKey)
	blobStore.openData = []byte("stored bytes")
	blobStore.openObject = storage.Object{Key: avatarKey, Size: int64(len(blobStore.openData))}

	req := withAvatarTestUser(httptest.NewRequest(http.MethodGet, "/api/v2/applications/42/avatar", nil))
	req.Header.Set("If-None-Match", `"`+avatarVersion(avatarKey)+`"`)
	recorder := httptest.NewRecorder()
	server.GetApplicationAvatar(recorder, req, genapi.ApplicationId(42))

	if recorder.Code != http.StatusNotModified {
		t.Fatalf("status=%d", recorder.Code)
	}
	if blobStore.readCalls != 0 {
		t.Fatalf("storage reads=%d, want 0", blobStore.readCalls)
	}
}

func TestGetApplicationAvatarZeroLengthObjectUsesFallback(t *testing.T) {
	avatarKey := "application-avatars/42/avatar.png"
	server, _, blobStore := newAvatarHandlerTestServer(t, avatarKey)
	blobStore.openObject = storage.Object{Key: avatarKey, Size: 0}

	req := withAvatarTestUser(httptest.NewRequest(http.MethodGet, "/api/v2/applications/42/avatar", nil))
	recorder := httptest.NewRecorder()
	server.GetApplicationAvatar(recorder, req, genapi.ApplicationId(42))

	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q", recorder.Code, recorder.Header().Get("Cache-Control"))
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Fatalf("content-type=%q", got)
	}
}

func TestGetApplicationAvatarShortReadUsesFallback(t *testing.T) {
	avatarKey := "application-avatars/42/avatar.png"
	server, _, blobStore := newAvatarHandlerTestServer(t, avatarKey)
	blobStore.openData = []byte("short")
	blobStore.openObject = storage.Object{Key: avatarKey, Size: 100}

	req := withAvatarTestUser(httptest.NewRequest(http.MethodGet, "/api/v2/applications/42/avatar", nil))
	recorder := httptest.NewRecorder()
	server.GetApplicationAvatar(recorder, req, genapi.ApplicationId(42))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control=%q", got)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Fatalf("content-type=%q", got)
	}
}

func TestGetApplicationAvatarMissingObjectUsesRetryableFallback(t *testing.T) {
	avatarKey := "application-avatars/42/missing.png"
	server, dbState, blobStore := newAvatarHandlerTestServer(t, avatarKey)
	blobStore.openErr = storage.ErrNotFound
	req := httptest.NewRequest(http.MethodGet, "/api/v2/applications/42/avatar", nil)
	req.Header.Set("If-None-Match", `"`+avatarVersion(avatarKey)+`"`)
	req = withAvatarTestUser(req)
	recorder := httptest.NewRecorder()

	server.GetApplicationAvatar(recorder, req, genapi.ApplicationId(42))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Fatalf("content-type=%q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control=%q, want no-store", got)
	}
	if got := recorder.Header().Get("ETag"); got != "" {
		t.Fatalf("etag=%q, want empty", got)
	}
	if got := recorder.Header().Get("Vary"); got != "Cookie" {
		t.Fatalf("vary=%q, want Cookie", got)
	}
	if dbState.execs != 0 {
		t.Fatalf("database writes=%d, want 0", dbState.execs)
	}
}

type avatarHandlerTestDBState struct {
	avatarKey string
	manager   bool
	execs     int
}

type avatarHandlerTestConnector struct {
	state *avatarHandlerTestDBState
}

func (c *avatarHandlerTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &avatarHandlerTestConn{state: c.state}, nil
}

func (c *avatarHandlerTestConnector) Driver() driver.Driver {
	return &avatarHandlerTestDriver{state: c.state}
}

type avatarHandlerTestDriver struct {
	state *avatarHandlerTestDBState
}

func (d *avatarHandlerTestDriver) Open(string) (driver.Conn, error) {
	return &avatarHandlerTestConn{state: d.state}, nil
}

type avatarHandlerTestConn struct {
	state *avatarHandlerTestDBState
}

func (c *avatarHandlerTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}

func (c *avatarHandlerTestConn) Close() error { return nil }

func (c *avatarHandlerTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported by avatar handler tests")
}

func (c *avatarHandlerTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "SELECT EXISTS") {
		allowed := false
		if strings.Contains(query, "admin_role_assignments") {
			allowed = c.state.manager
		}
		return &avatarHandlerTestRows{columns: []string{"allowed"}, values: []driver.Value{allowed}}, nil
	}
	if !strings.Contains(query, "FROM applications a") {
		return nil, errors.New("unexpected avatar handler test query: " + query)
	}
	now := time.Now()
	return &avatarHandlerTestRows{
		columns: []string{
			"id", "slug", "name", "description", "icon", "avatar_key", "color",
			"kind", "renderer_key", "executor_key", "category_id", "is_public",
			"is_default_agent", "enabled", "usage_count", "tags", "default_config",
			"created_by", "organization_id", "created_at", "updated_at", "category_slug", "category_name",
		},
		values: []driver.Value{
			int64(42), "avatar-test", "Avatar Test", "", "AT", c.state.avatarKey, "#fff",
			"chat", "chat", "chat", nil, true, false, true, int64(0), []byte("[]"), []byte("{}"),
			int64(1), nil, now, now, nil, nil,
		},
	}, nil
}

func (c *avatarHandlerTestConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.execs++
	return driver.RowsAffected(1), nil
}

type avatarHandlerTestRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (r *avatarHandlerTestRows) Columns() []string { return r.columns }
func (r *avatarHandlerTestRows) Close() error      { return nil }
func (r *avatarHandlerTestRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(dest, r.values)
	r.done = true
	return nil
}

type avatarHandlerTestStorage struct {
	puts         int
	putErr       error
	openErr      error
	openData     []byte
	openObject   storage.Object
	readerClosed bool
	readCalls    int
}

func (s *avatarHandlerTestStorage) Put(_ context.Context, key string, r io.Reader, contentType string) (storage.Object, error) {
	s.puts++
	if s.putErr != nil {
		return storage.Object{}, s.putErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return storage.Object{}, err
	}
	return storage.Object{Key: key, Size: int64(len(data)), ContentType: contentType}, nil
}

func (s *avatarHandlerTestStorage) Open(context.Context, string) (io.ReadSeekCloser, storage.Object, error) {
	if s.openErr != nil {
		return nil, storage.Object{}, s.openErr
	}
	return &avatarTestReadSeekCloser{Reader: bytes.NewReader(s.openData), closed: &s.readerClosed, reads: &s.readCalls}, s.openObject, nil
}

func (s *avatarHandlerTestStorage) Delete(context.Context, string) error { return nil }
func (s *avatarHandlerTestStorage) Stat(context.Context, string) (storage.Object, error) {
	return storage.Object{}, s.openErr
}
func (s *avatarHandlerTestStorage) PresignedURL(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func newAvatarHandlerTestServer(t *testing.T, avatarKey string) (*Server, *avatarHandlerTestDBState, *avatarHandlerTestStorage) {
	t.Helper()
	state := &avatarHandlerTestDBState{avatarKey: avatarKey}
	db := sql.OpenDB(&avatarHandlerTestConnector{state: state})
	t.Cleanup(func() { _ = db.Close() })
	blobStore := &avatarHandlerTestStorage{}
	return &Server{
		DB:          db,
		CatalogRepo: &catalog.Repo{DB: db},
		Storage:     blobStore,
	}, state, blobStore
}

func newAvatarUploadRequest(t *testing.T, filename string, fileBytes []byte, paddingBytes int) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if paddingBytes > 0 {
		field, err := writer.CreateFormField("padding")
		if err != nil {
			t.Fatalf("create padding field: %v", err)
		}
		if _, err := io.CopyN(field, zeroReader{}, int64(paddingBytes)); err != nil {
			t.Fatalf("write padding field: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v2/applications/42/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

type avatarTestReadSeekCloser struct {
	*bytes.Reader
	closed *bool
	reads  *int
}

func (r *avatarTestReadSeekCloser) Read(p []byte) (int, error) {
	*r.reads++
	return r.Reader.Read(p)
}

func (r *avatarTestReadSeekCloser) Close() error {
	*r.closed = true
	return nil
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func withAvatarTestUser(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{
		ID:      1,
		IsStaff: true,
	}})
	return r.WithContext(ctx)
}

func TestApplicationManagerCanReadPrivateAvatarWithoutConsumeAccess(t *testing.T) {
	for _, manager := range []bool{false, true} {
		t.Run(fmt.Sprint(manager), func(t *testing.T) {
			server, state, store := newAvatarHandlerTestServer(t, "application-avatars/42/private.png")
			state.manager = manager
			server.AdminRBAC = &adminrbac.Service{DB: server.DB, Enabled: true}
			var imageData bytes.Buffer
			if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			store.openData = imageData.Bytes()
			store.openObject = storage.Object{Size: int64(len(store.openData)), ContentType: "image/png"}
			req := httptest.NewRequest(http.MethodGet, "/api/v2/applications/42/avatar", nil)
			req = req.WithContext(context.WithValue(req.Context(), userCtxKey, &AuthenticatedUser{User: &identity.User{ID: 1}}))
			rec := httptest.NewRecorder()
			server.GetApplicationAvatar(rec, req, 42)
			want := http.StatusNotFound
			if manager {
				want = http.StatusOK
			}
			if rec.Code != want {
				t.Fatalf("manager=%v status=%d want=%d body=%s", manager, rec.Code, want, rec.Body.String())
			}
			if !manager && store.readCalls != 0 {
				t.Fatal("unauthorized caller read private avatar")
			}
		})
	}
}
