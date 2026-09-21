package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func localConfig(server *httptest.Server, adapter string) Config {
	return Config{Adapter: adapter, BaseURL: server.URL, Credential: "test-secret", AllowedPrivateHosts: []string{"127.0.0.1"}}
}
func TestSafeClientHonorsLongModelTimeoutForResponseHeaders(t *testing.T) {
	client := safeClient(Config{Timeout: 600 * time.Second})
	transport, ok := client.Transport.(*http.Transport)
	if !ok || client.Timeout != 600*time.Second || transport.ResponseHeaderTimeout != 600*time.Second {
		t.Fatalf("client timeout=%v header timeout=%v", client.Timeout, transport.ResponseHeaderTimeout)
	}
}

func TestGLMFlashXUsesDocumentedThinkingAndTokenFields(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":" final"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`)
	}))
	defer server.Close()
	limit := 2048
	result, err := Generate(context.Background(), localConfig(server, "openai_chat"), Request{Model: "glm-4.6v-flashx", MaxOutputTokens: &limit, Messages: []Message{{Role: "user", Text: "inspect"}}}, nil)
	if err != nil || result.Text != " final" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	thinking, ok := received["chat_template_kwargs"].(map[string]any)
	if !ok || thinking["enable_thinking"] != true {
		t.Fatalf("thinking config missing: %#v", received)
	}
	if received["max_tokens"] != float64(limit) {
		t.Fatalf("max_tokens missing: %#v", received)
	}
	if _, exists := received["max_completion_tokens"]; exists {
		t.Fatal("GLM preset used incompatible max_completion_tokens field")
	}
}

func TestOpenAIImageAndHistory(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("unexpected request")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`)
	}))
	defer server.Close()
	result, err := Generate(context.Background(), localConfig(server, "openai_chat"), Request{Model: "vision", Messages: []Message{{Role: "user", Text: "read", Files: []File{{MIME: "image/png", Data: []byte("image")}}}, {Role: "assistant", Text: "prior"}, {Role: "user", Text: "again"}}}, nil)
	if err != nil || result.Text != "answer" || result.InputTokens == nil || *result.InputTokens != 7 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	messages := received["messages"].([]any)
	if len(messages) != 3 {
		t.Fatal("history dropped")
	}
	first := messages[0].(map[string]any)["content"].([]any)
	if len(first) != 2 || !strings.HasPrefix(first[1].(map[string]any)["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,") {
		t.Fatal("image dropped")
	}
}
func TestOpenAIStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	var snapshot string
	got, err := Generate(context.Background(), localConfig(server, "openai_chat"), Request{Model: "m", Stream: true, Messages: []Message{{Role: "user", Text: "hello"}}}, func(s string) error { snapshot = s; return nil })
	if err != nil || got.Text != "你好" || snapshot != "你好" {
		t.Fatalf("%+v %v %q", got, err, snapshot)
	}
}
func TestTruncatedStreamIsNotSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer server.Close()
	_, err := Generate(context.Background(), localConfig(server, "openai_chat"), Request{Model: "m", Stream: true, Messages: []Message{{Role: "user", Text: "x"}}}, nil)
	if ErrorCode(err) != "upstream_incomplete" {
		t.Fatalf("expected incomplete, got %v", err)
	}
}
func TestGeminiNativePDFVideo(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-test:generateContent" || r.Header.Get("x-goog-api-key") != "test-secret" {
			t.Errorf("invalid Gemini endpoint/header")
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"reasoning","thought":true},{"text":"native result"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":3}}`)
	}))
	defer server.Close()
	got, err := Generate(context.Background(), localConfig(server, "gemini"), Request{Model: "gemini-test", SystemPrompt: "help", Messages: []Message{{Role: "user", Text: "compare", Files: []File{{MIME: "application/pdf", Data: []byte("%PDF-1")}, {MIME: "video/mp4", Data: []byte("clip")}}}}}, nil)
	if err != nil || got.Text != "native result" {
		t.Fatalf("%+v %v", got, err)
	}
	content := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if len(content) != 3 {
		t.Fatal("native files missing")
	}
	if _, ok := body["systemInstruction"]; !ok {
		t.Fatal("system instruction missing")
	}
}
func TestSafetyBoundaries(t *testing.T) {
	for _, base := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com?secret=x", "https://example.com#fragment"} {
		t.Run(base, func(t *testing.T) {
			_, err := Generate(context.Background(), Config{Adapter: "openai_chat", BaseURL: base}, Request{Model: "m", Messages: []Message{{Role: "user", Text: "x"}}}, nil)
			if err == nil {
				t.Fatal("unsafe URL accepted")
			}
		})
	}
	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer server.Close()
	cfg := localConfig(server, "openai_chat")
	cfg.AllowedPrivateHosts = nil
	_, err := Generate(context.Background(), cfg, Request{Model: "m", Messages: []Message{{Role: "user", Text: "x"}}}, nil)
	if err == nil || reached {
		t.Fatal("private target allowed")
	}
	cfg = localConfig(server, "openai_chat")
	_, err = Generate(context.Background(), cfg, Request{Model: "m", Messages: []Message{{Role: "user", Files: []File{{MIME: "application/pdf", Data: []byte("pdf")}}}}}, nil)
	if ErrorCode(err) != "unsupported_input" || reached {
		t.Fatalf("unsupported attachment sent: %v", err)
	}
}
func TestRedirectDoesNotLeakCredential(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	_, err := Generate(context.Background(), localConfig(source, "openai_chat"), Request{Model: "m", Messages: []Message{{Role: "user", Text: "x"}}}, nil)
	if err == nil || reached {
		t.Fatal("redirect followed")
	}
}
func TestUpstreamErrorsAreSanitized(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, "credential=test-secret; private prompt")
			}))
			defer server.Close()
			_, err := Generate(context.Background(), localConfig(server, "openai_chat"), Request{Model: "m", Messages: []Message{{Role: "user", Text: "x"}}}, nil)
			if err == nil || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "private prompt") {
				t.Fatalf("leaked error %v", err)
			}
		})
	}
}
func TestPublicIPClassification(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fc00::1", "::ffff:127.0.0.1", "192.0.2.1", "198.18.0.1"} {
		if publicIP(ip) {
			t.Errorf("unsafe IP %s", ip)
		}
	}
	if !publicIP("8.8.8.8") || !publicIP("2606:4700:4700::1111") {
		t.Fatal("public address rejected")
	}
}
func TestInputValidationMatrix(t *testing.T) {
	cases := []struct {
		name    string
		adapter string
		input   Request
		code    string
	}{
		{"adapter", "other", Request{Model: "m"}, "unsupported_adapter"},
		{"model", "gemini", Request{Model: "bad/../../id"}, "invalid_model"},
		{"empty model", "gemini", Request{}, "invalid_model"},
		{"empty history", "openai_chat", Request{Model: "m"}, "input_limit"},
		{"role", "openai_chat", Request{Model: "m", Messages: []Message{{Role: "system", Text: "x"}}}, "invalid_role"},
		{"empty file", "gemini", Request{Model: "m", Messages: []Message{{Role: "user", Files: []File{{MIME: "image/png"}}}}}, "invalid_attachment"},
		{"bad file", "gemini", Request{Model: "m", Messages: []Message{{Role: "user", Files: []File{{MIME: "text/html", Data: []byte("x")}}}}}, "unsupported_input"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateInput(c.adapter, c.input)
			if ErrorCode(err) != c.code {
				t.Fatalf("%v", err)
			}
		})
	}
	temp := 3.0
	tokens := 0
	if ErrorCode(validateInput("openai_chat", Request{Model: "m", Temperature: &temp, Messages: []Message{{Role: "user"}}})) != "invalid_parameter" {
		t.Fatal("invalid temperature")
	}
	if ErrorCode(validateInput("openai_chat", Request{Model: "m", MaxOutputTokens: &tokens, Messages: []Message{{Role: "user"}}})) != "invalid_parameter" {
		t.Fatal("invalid tokens")
	}
	if _, _, err := ValidateFile([]byte("<html>not an image</html>")); ErrorCode(err) != "unsupported_file" {
		t.Fatal("bad signature accepted")
	}
	if _, mime, err := ValidateFile([]byte("%PDF-1.7\n")); err != nil || mime != "application/pdf" {
		t.Fatalf("PDF: %v %s", err, mime)
	}
	if _, _, err := ValidateFile(nil); ErrorCode(err) != "attachment_limit" {
		t.Fatal("empty accepted")
	}
}
func TestGeminiStreamAndMalformedResponses(t *testing.T) {
	for _, tc := range []struct{ adapter, body, contentType, code string }{
		{"gemini", `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`, "application/json", ""},
		{"gemini", `{"error":{"message":"secret"}}`, "application/json", "upstream_error"},
		{"openai_chat", `garbage`, "application/json", "upstream_protocol"},
		{"openai_chat", `{"choices":[]}`, "application/json", "empty_response"},
	} {
		t.Run(tc.adapter+tc.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, err := Generate(context.Background(), localConfig(server, tc.adapter), Request{Model: "m", Messages: []Message{{Role: "user", Text: "x"}}}, nil)
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if ErrorCode(err) != tc.code {
				t.Fatalf("%v", err)
			}
		})
	}
	got, err := readStream(strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"), "gemini", nil)
	if err != nil || got.Text != "ok" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = readStream(strings.NewReader("data: invalid\n\n"), "openai_chat", nil); ErrorCode(err) != "upstream_protocol" {
		t.Fatal("malformed stream")
	}
	if _, err = readStream(strings.NewReader("data: [DONE]\n\n"), "openai_chat", nil); ErrorCode(err) != "empty_response" {
		t.Fatal("empty stream")
	}
}
func TestInvalidUsageCannotStrandPersistence(t *testing.T) {
	for _, adapter := range []string{"openai_chat", "gemini"} {
		var out Result
		var err error
		if adapter == "openai_chat" {
			err = decodeOpenAI([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":-1}}`), &out, false)
		} else {
			err = decodeGemini([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":-1}}`), &out)
		}
		if ErrorCode(err) != "upstream_protocol" || out.InputTokens != nil {
			t.Fatalf("invalid usage persisted: %+v %v", out, err)
		}
	}
}
