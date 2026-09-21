package aimodel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	platformcrypto "github.com/shilin414/cas/backend-go/internal/platform/crypto"
	"github.com/go-sql-driver/mysql"
)

func TestInvalidConstructorOptions(t *testing.T) {
	if _, e := NewService(nil, Options{}); e == nil {
		t.Fatal("nil repo accepted")
	}
	base, _ := mockService(t)
	for _, opts := range []Options{{}, {EncryptionKey: "key", SessionTTL: time.Second}, {EncryptionKey: "key", MaxSessionsPerUser: -1}, {EncryptionKey: "key", MaxAttachmentsPerSession: -1}, {EncryptionKey: "key", MaxSessionBytes: MaxHistoryBytes + 1}} {
		if _, e := NewService(base.Repo, opts); e == nil {
			t.Fatalf("invalid options accepted: %+v", opts)
		}
	}
}
func TestCredentialCannotCrossOAuthPurpose(t *testing.T) {
	c, _ := newCredentialCipher("same-master")
	oauth, _ := platformcrypto.NewAESGCM("same-master")
	oauthEnc, _ := oauth.Encrypt("token")
	if _, e := c.decrypt(testSession, oauthEnc); e == nil {
		t.Fatal("OAuth token accepted as model credential")
	}
	enc, _ := c.encrypt(testSession, "token")
	if _, e := oauth.Decrypt(strings.TrimPrefix(enc, "v1:")); e == nil {
		t.Fatal("model token accepted as OAuth token")
	}
	if v, e := c.encrypt(testSession, ""); e != nil || v != "" {
		t.Fatal("empty encryption")
	}
	if v, e := c.decrypt(testSession, ""); e != nil || v != "" {
		t.Fatal("empty decryption")
	}
	for _, v := range []string{"v1:invalid*", "v1:YQ"} {
		if _, e := c.decrypt(testSession, v); e == nil {
			t.Fatal("corrupt credential accepted")
		}
	}
}
func TestConnectionInputBoundaries(t *testing.T) {
	s := &Service{opts: Options{AllowedPrivateHosts: []string{"localhost"}}}
	base := ConnectionInput{Name: "test", Adapter: "gemini", BaseURL: "https://example.com", TimeoutSeconds: 60, MaxConcurrency: 2}
	badSecret := "bad\r\n"
	for _, mutate := range []func(*ConnectionInput){func(v *ConnectionInput) { v.Name = " " }, func(v *ConnectionInput) { v.Adapter = "code" }, func(v *ConnectionInput) { v.TimeoutSeconds = 601 }, func(v *ConnectionInput) { v.MaxConcurrency = 0 }, func(v *ConnectionInput) { v.Credential = &badSecret }} {
		v := base
		mutate(&v)
		if e := s.validateConnection(v); !errors.Is(e, ErrInvalid) {
			t.Fatal("bad connection accepted")
		}
	}
	for _, u := range []string{"https://example.com:0", "https://example.com:70000", "http://example.com", "https://example.com.", "https://%zz", "https://x.local", "https://[::1]"} {
		if e := s.validateURL(u); !errors.Is(e, ErrInvalid) {
			t.Errorf("accepted URL %s", u)
		}
	}
	if e := s.validateURL("http://localhost:1234/v1"); e != nil {
		t.Fatal("explicit local HTTP exception rejected")
	}
	if e := s.validateURL("http://localhost.evil:1234"); e == nil {
		t.Fatal("allowlist suffix attack accepted")
	}
	nan := math.NaN()
	if e := validateParameters(Parameters{Temperature: &nan}); e == nil {
		t.Fatal("NaN accepted")
	}
}
func validLocalInput() ModelInput {
	return ModelInput{Name: "OCR", CapabilityKind: "ocr", ExecutionLocation: "browser_local", BrowserManifest: json.RawMessage(`{"adapter":"paddleocr_tiny","version":"version-1","resources":[{"name":"PP-OCRv6_tiny_det","url":"/base/ocr-assets/version-1/PP-OCRv6_tiny_det.tar","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size_bytes":1},{"name":"PP-OCRv6_tiny_rec","url":"/base/ocr-assets/version-1/PP-OCRv6_tiny_rec.tar","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size_bytes":1}]}`)}
}
func TestManifestAndModelBoundaryMatrix(t *testing.T) {
	s := &Service{}
	local := validLocalInput()
	if e := s.validateModel(&local); e != nil {
		t.Fatal(e)
	}
	if local.ModelID != "PP-OCRv6_tiny" {
		t.Fatal("local default model ID")
	}
	tests := []struct{ old, new string }{
		{"version-1", "../escape"}, {"PP-OCRv6_tiny_rec", "PP-OCRv6_tiny_det"}, {"PP-OCRv6_tiny_det.tar", "arbitrary.js"}, {"/base/ocr-assets/", "https://evil.example/ocr-assets/"}, {"/base/ocr-assets/", "/base/%2e%2e/ocr-assets/"}, {"/base/ocr-assets/", "//host/ocr-assets/"}, {`.tar"`, `.tar?key=x"`}, {`.tar"`, `.tar#x"`}, {`"size_bytes":1`, `"size_bytes":0`}, {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "not-a-hash"}, {"/base/ocr-assets/", "/base@/ocr-assets/"},
	}
	for _, tc := range tests {
		in := validLocalInput()
		in.BrowserManifest = json.RawMessage(strings.ReplaceAll(string(in.BrowserManifest), tc.old, tc.new))
		if e := s.validateModel(&in); !errors.Is(e, ErrInvalid) {
			t.Errorf("manifest attack accepted %q -> %q", tc.old, tc.new)
		}
	}
	for _, mutate := range []func(*ModelInput){func(v *ModelInput) { v.Name = "" }, func(v *ModelInput) { v.ModelID = strings.Repeat("x", 256) }, func(v *ModelInput) { v.ExecutionLocation = "unknown" }, func(v *ModelInput) { v.Capabilities.Video = true }, func(v *ModelInput) { v.ConnectionID = &testConnectionPointer }, func(v *ModelInput) { v.BrowserManifest = json.RawMessage("{}{}") }, func(v *ModelInput) { v.BrowserManifest = json.RawMessage("[]") }, func(v *ModelInput) { v.CapabilityKind = "chat" }} {
		in := validLocalInput()
		mutate(&in)
		if e := s.validateModel(&in); !errors.Is(e, ErrInvalid) {
			t.Fatal("bad model accepted")
		}
	}
	remote := ModelInput{Name: "chat", ModelID: "model", CapabilityKind: "chat", ExecutionLocation: "server_remote"}
	if e := s.validateModel(&remote); e == nil {
		t.Fatal("remote without connection")
	}
	remote.ConnectionID = &testConnectionPointer
	remote.BrowserManifest = json.RawMessage("{}")
	if e := s.validateModel(&remote); e == nil {
		t.Fatal("remote with manifest")
	}
	if safePathSegment(".") || safePathSegment("..") || safePathSegment("") || safePathSegment("a/b") {
		t.Fatal("unsafe segment")
	}
}

var testConnectionPointer = "44444444-4444-4444-8444-444444444444"

func TestMessageAttachmentAndCompletionValidation(t *testing.T) {
	for _, in := range []MessageInput{{}, {RequestID: testRequest}, {RequestID: testRequest, Text: "x", AttachmentIDs: []string{testSession, testSession}}, {RequestID: testRequest, Text: strings.Repeat("x", MaxOutputBytes+1)}} {
		if e := validateMessage(in); e == nil {
			t.Fatal("invalid message accepted")
		}
	}
	good := AttachmentInput{Name: "x.png", MIMEType: "image/png", SizeBytes: 1, Kind: "image", StorageKey: "key"}
	if e := validateAttachment(good); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*AttachmentInput){func(v *AttachmentInput) { v.Name = "" }, func(v *AttachmentInput) { v.SizeBytes = MaxAttachmentBytes + 1 }, func(v *AttachmentInput) { v.MIMEType = "application/x-shellscript" }, func(v *AttachmentInput) { v.Kind = "video" }, func(v *AttachmentInput) { v.Kind = "pdf" }, func(v *AttachmentInput) { v.Kind = "script" }} {
		v := good
		mutate(&v)
		if e := validateAttachment(v); e == nil {
			t.Fatal("invalid attachment accepted")
		}
	}
	if e := validateAttachment(AttachmentInput{Name: "x", MIMEType: "video/mp4", SizeBytes: 1, Kind: "video", StorageKey: "key"}); e != nil {
		t.Fatal(e)
	}
	negative := int64(-1)
	for _, c := range []Completion{{Status: "running"}, {Status: "succeeded", DurationMs: -1}, {Status: "succeeded", InputTokens: &negative}, {Status: "succeeded", OutputTokens: &negative}, {Status: "succeeded", CancellationConfirmed: true}, {Status: "succeeded", OutputText: strings.Repeat("x", MaxOutputBytes+1)}} {
		if e := validateCompletion(c); e == nil {
			t.Fatal("bad completion accepted")
		}
	}
	if supportsAttachment(nil, "image") || supportsAttachment(&RuntimeConfig{}, "video") {
		t.Fatal("nil configuration supports files")
	}
	cfg := &RuntimeConfig{Model: &Model{ModelInput: ModelInput{Capabilities: Capabilities{Video: true, PDF: true, Image: true}}}, Connection: &Connection{Adapter: "gemini"}}
	if !supportsAttachment(cfg, "video") || supportsAttachment(cfg, "other") {
		t.Fatal("attachment capability mismatch")
	}
	cfg.Connection.Adapter = "openai_chat"
	if supportsAttachment(cfg, "pdf") || supportsAttachment(cfg, "video") {
		t.Fatal("chat protocol received native PDF/video")
	}
}
func TestServiceInvalidIdentifiersDoNotTouchDatabase(t *testing.T) {
	s, _ := mockService(t)
	ctx := context.Background()
	bad := "not-uuid"
	_, e := s.GetConnection(ctx, bad)
	mustError(t, e)
	_, e = s.GetModel(ctx, bad)
	mustError(t, e)
	mustError(t, s.DeleteConnection(ctx, bad))
	mustError(t, s.DeleteModel(ctx, bad))
	mustError(t, s.SetConnectionCredential(ctx, bad, "x"))
	_, e = s.GetSession(ctx, 0, testSession)
	mustError(t, e)
	_, e = s.ListMessages(ctx, 0, testSession)
	mustError(t, e)
	_, e = s.ListSessions(ctx, 0, "")
	mustError(t, e)
	_, e = s.ListSessions(ctx, 1, bad)
	mustError(t, e)
	_, e = s.CreateSession(ctx, 0, testModel, "")
	mustError(t, e)
	_, e = s.GetAttachment(ctx, 1, testSession, bad)
	mustError(t, e)
	mustError(t, s.DeleteAttachment(ctx, 1, testSession, bad))
	_, e = s.ListAttachments(ctx, 0, testSession)
	mustError(t, e)
	_, e = s.GetInvocation(ctx, 1, bad)
	mustError(t, e)
	_, e = s.ListInvocations(ctx, 0, "")
	mustError(t, e)
	_, e = s.ListInvocations(ctx, 1, bad)
	mustError(t, e)
	_, e = s.StartInvocation(ctx, bad)
	mustError(t, e)
	mustError(t, s.UpdateInvocationOutput(ctx, bad, ""))
	mustError(t, s.UpdateInvocationOutput(ctx, testSession, strings.Repeat("x", MaxOutputBytes+1)))
	mustError(t, s.FinishInvocation(ctx, bad, Completion{}))
	mustError(t, s.FinishInvocation(ctx, testSession, Completion{}))
	_, e = s.CleanupExpired(ctx, 0)
	mustError(t, e)
	_, e = s.RecoverStaleInvocations(ctx, time.Time{})
	mustError(t, e)
	_, e = s.ResolveRemote(ctx, bad)
	mustError(t, e)
	_, e = s.RuntimeConfig(ctx, 0, bad)
	mustError(t, e)
	_, _, e = s.BeginInvocation(ctx, 1, testSession, MessageInput{})
	mustError(t, e)
	_, e = s.LookupInvocation(ctx, 1, testSession, MessageInput{})
	mustError(t, e)
	_, e = s.CreateConnection(ctx, ConnectionInput{})
	mustError(t, e)
	_, e = s.UpdateConnection(ctx, testSession, ConnectionInput{})
	mustError(t, e)
	secret := "x"
	_, e = s.UpdateConnection(ctx, testSession, ConnectionInput{Credential: &secret})
	mustError(t, e)
	_, e = s.CreateModel(ctx, ModelInput{})
	mustError(t, e)
	_, e = s.UpdateModel(ctx, testModel, ModelInput{})
	mustError(t, e)
	mustError(t, s.SetConnectionCredential(ctx, testSession, "bad\ncredential"))
	_, e = s.AddAttachment(ctx, 1, testSession, AttachmentInput{})
	mustError(t, e)
}
func mustError(t *testing.T, e error) {
	t.Helper()
	if e == nil {
		t.Fatal("expected error")
	}
}
func TestRepositoryMappingAndSanitization(t *testing.T) {
	for _, n := range []uint16{1062, 1205, 1213, 1452} {
		if e := dbError(&mysql.MySQLError{Number: n}); e == nil || e == ErrUnavailable {
			t.Fatalf("driver mapping %d", n)
		}
	}
	if e := affected(0, nil); e != ErrNotFound {
		t.Fatal(e)
	}
	if e := affected(0, errors.New("private detail")); e != ErrUnavailable {
		t.Fatal(e)
	}
	if nullString(nil).Valid || stringPtr(sql.NullString{}) != nil {
		t.Fatal("null conversion")
	}
	if _, e := modelFrom(db.AiModel{Capabilities: json.RawMessage("bad")}); e != ErrUnavailable {
		t.Fatal("invalid persisted capabilities")
	}
	if _, e := modelFrom(db.AiModel{Capabilities: json.RawMessage("{}"), DefaultParameters: json.RawMessage("bad")}); e != ErrUnavailable {
		t.Fatal("invalid persisted parameters")
	}
}
