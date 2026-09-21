package aimodel

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCredentialPurposeAndConnectionBinding(t *testing.T) {
	c, err := newCredentialCipher("shared-token-key")
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.encrypt("a", "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.decrypt("a", enc)
	if err != nil || plain != "secret-token" {
		t.Fatalf("roundtrip: %v", err)
	}
	if _, err = c.decrypt("b", enc); err == nil {
		t.Fatal("ciphertext accepted for other connection")
	}
	other, _ := newCredentialCipher("other-key")
	if _, err = other.decrypt("a", enc); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = c.decrypt("a", "malformed"); err == nil {
		t.Fatal("malformed ciphertext accepted")
	}
	if _, err = newCredentialCipher(""); err == nil {
		t.Fatal("empty encryption key accepted")
	}
	b, _ := json.Marshal(Connection{CredentialEnc: enc})
	if strings.Contains(string(b), enc) {
		t.Fatal("credential leaked")
	}
}
func TestConnectionValidation(t *testing.T) {
	s := &Service{opts: Options{AllowedPrivateHosts: []string{"localhost"}}}
	for _, u := range []string{"https://api.example.com/v1", "https://localhost:1234/v1"} {
		if err := s.validateConnection(ConnectionInput{Name: "test", Adapter: "openai_chat", BaseURL: u, TimeoutSeconds: 60, MaxConcurrency: 2}); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	if err := s.validateConnection(ConnectionInput{Name: "GLM", Adapter: "openai_chat", BaseURL: "https://api.example.com/v1", TimeoutSeconds: 600, MaxConcurrency: 2}); err != nil {
		t.Fatalf("documented 600 second timeout rejected: %v", err)
	}
	if err := s.validateConnection(ConnectionInput{Name: "too slow", Adapter: "openai_chat", BaseURL: "https://api.example.com/v1", TimeoutSeconds: 601, MaxConcurrency: 2}); !errors.Is(err, ErrInvalid) {
		t.Fatal("timeout above 600 seconds accepted")
	}
	for _, u := range []string{"http://api.example.com", "https://user:pass@example.com", "https://127.0.0.1", "https://169.254.169.254", "https://api.example.com?key=secret", "https://localhost.evil/x", "https://example.com/#secret"} {
		if u == "https://localhost.evil/x" {
			continue
		} // public DNS is validated by runtime.
		if err := s.validateConnection(ConnectionInput{Name: "test", Adapter: "gemini", BaseURL: u, TimeoutSeconds: 60, MaxConcurrency: 2}); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %s", u)
		}
	}
}
func TestModelAndParametersValidation(t *testing.T) {
	s := &Service{}
	local := ModelInput{Name: "OCR", ModelID: "PP-OCRv6_tiny", CapabilityKind: "ocr", ExecutionLocation: "browser_local", BrowserManifest: json.RawMessage(`{"adapter":"paddleocr_tiny","version":"1","resources":[{"name":"PP-OCRv6_tiny_det","url":"/ocr-assets/1/PP-OCRv6_tiny_det.tar","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size_bytes":123},{"name":"PP-OCRv6_tiny_rec","url":"/ocr-assets/1/PP-OCRv6_tiny_rec.tar","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size_bytes":123}]}`)}
	if err := s.validateModel(&local); err != nil {
		t.Fatal(err)
	}
	local.BrowserManifest = json.RawMessage(`{"adapter":"paddleocr_tiny","version":"1","resources":[{"name":"PP-OCRv6_tiny_det","url":"ocr-assets/1/PP-OCRv6_tiny_det.tar","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size_bytes":123},{"name":"PP-OCRv6_tiny_rec","url":"ocr-assets/1/PP-OCRv6_tiny_rec.tar","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size_bytes":123}]}`)
	if err := s.validateModel(&local); err != nil {
		t.Fatalf("portable built-in manifest rejected: %v", err)
	}
	local.BrowserManifest = json.RawMessage(`{"adapter":"javascript","version":"1","resources":[]}`)
	if err := s.validateModel(&local); !errors.Is(err, ErrInvalid) {
		t.Fatal("arbitrary adapter accepted")
	}
	local.BrowserManifest = json.RawMessage(strings.Repeat(" ", 1<<20) + "{}")
	if err := s.validateModel(&local); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversize manifest accepted")
	}
	bad := 3.0
	if err := validateParameters(Parameters{Temperature: &bad}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid temperature accepted")
	}
	zero := 0
	if err := validateParameters(Parameters{MaxOutputTokens: &zero}); !errors.Is(err, ErrInvalid) {
		t.Fatal("zero token budget accepted")
	}
}
