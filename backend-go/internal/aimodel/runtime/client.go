// Package runtime contains bounded, SSRF-safe remote model adapters. It has no
// knowledge of HTTP callers, credentials at rest, or business task execution.
package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const MaxInputBytes = 24 << 20
const MaxOutputBytes = 1 << 20
const maxResponseBytes = 4 << 20

type Config struct {
	Adapter             string
	BaseURL             string
	Credential          string
	AllowedPrivateHosts []string
	Timeout             time.Duration
}
type File struct {
	MIME string
	Data []byte
}
type Message struct {
	Role  string
	Text  string
	Files []File
}
type Request struct {
	Model           string
	SystemPrompt    string
	Messages        []Message
	Temperature     *float64
	MaxOutputTokens *int
	Stream          bool
}
type Result struct {
	Text         string
	InputTokens  *int64
	OutputTokens *int64
	FinishReason string
}

// Error deliberately excludes upstream bodies, credentials, and URL query data.
type Error struct {
	Code      string
	Message   string
	Uncertain bool
}

func (e *Error) Error() string { return e.Message }
func ErrorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "internal_error"
}
func IsUncertain(err error) bool      { var e *Error; return errors.As(err, &e) && e.Uncertain }
func fail(code, message string) error { return &Error{Code: code, Message: message} }

var prohibitedNetworks = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16",
}

func publicIP(raw string) bool {
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, rawPrefix := range prohibitedNetworks {
		if netip.MustParsePrefix(rawPrefix).Contains(ip) {
			return false
		}
	}
	return true
}
func allowedHost(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, item := range allowed {
		if host == strings.ToLower(strings.TrimSuffix(strings.TrimSpace(item), ".")) && host != "" {
			return true
		}
	}
	return false
}
func validatedBase(cfg Config) (*url.URL, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return nil, fail("invalid_endpoint", "模型连接地址无效")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && allowedHost(u.Hostname(), cfg.AllowedPrivateHosts)) {
		return nil, fail("invalid_endpoint", "模型连接必须使用 HTTPS；本地部署例外需要服务端明确配置")
	}
	if strings.ContainsAny(cfg.Credential, "\r\n") {
		return nil, fail("invalid_credential", "模型凭据格式无效")
	}
	return u, nil
}

// safeClient pins the resolved IP in DialContext, eliminating a validation/dial
// DNS rebinding gap. Proxies and redirects are never inherited implicitly.
func safeClient(cfg Config) *http.Client {
	timeout := cfg.Timeout
	if timeout <= 0 || timeout > 10*time.Minute {
		timeout = 90 * time.Second
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 20 * time.Second}
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: timeout, MaxResponseHeaderBytes: 64 << 10}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fail("invalid_endpoint", "连接地址无效")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fail("network_error", "无法解析模型服务地址")
		}
		explicit := allowedHost(host, cfg.AllowedPrivateHosts)
		if len(ips) == 0 {
			return nil, fail("network_error", "模型服务没有可用地址")
		}
		// Mixed public/private DNS answers are rejected rather than partially used.
		for _, ip := range ips {
			if !explicit && !publicIP(ip.String()) {
				return nil, fail("blocked_endpoint", "模型服务地址不符合出网策略")
			}
		}
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, fail("network_error", "无法连接模型服务")
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
}

// Generate never retries requests. A timeout after submission may have incurred
// upstream usage; the caller must not convert it to a safe-to-retry result.
func Generate(ctx context.Context, cfg Config, input Request, onSnapshot func(string) error) (Result, error) {
	var result Result
	base, err := validatedBase(cfg)
	if err != nil {
		return result, err
	}
	if err = validateInput(cfg.Adapter, input); err != nil {
		return result, err
	}
	payload, endpoint, err := makePayload(base, cfg.Adapter, input)
	if err != nil {
		return result, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return result, fail("invalid_input", "无法编码模型请求")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return result, fail("invalid_endpoint", "模型连接地址无效")
	}
	request.Header.Set("Content-Type", "application/json")
	if cfg.Adapter == "gemini" {
		request.Header.Set("x-goog-api-key", cfg.Credential)
	} else {
		request.Header.Set("Authorization", "Bearer "+cfg.Credential)
	}
	client := safeClient(cfg)
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		var policyError *Error
		if errors.As(err, &policyError) {
			return result, policyError
		}
		return result, &Error{Code: "upstream_unconfirmed", Message: "模型请求中断或超时，上游是否完成尚未确认", Uncertain: true}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code, message := "upstream_error", "模型服务返回错误，请检查连接和模型配置"
		switch response.StatusCode {
		case 401, 403:
			code, message = "upstream_auth", "模型服务认证失败"
		case 429:
			code, message = "upstream_rate_limit", "模型服务限流，请稍后重试"
		default:
			if response.StatusCode >= 300 && response.StatusCode < 400 {
				code, message = "redirect_blocked", "模型服务重定向已阻止，请配置最终服务地址"
			}
		}
		return result, fail(code, message)
	}
	if input.Stream {
		if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
			return result, fail("upstream_protocol", "模型服务未返回约定的流式格式")
		}
		return readStream(response.Body, cfg.Adapter, onSnapshot)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return result, &Error{Code: "upstream_incomplete", Message: "模型响应读取中断", Uncertain: true}
	}
	if len(raw) > maxResponseBytes {
		return result, fail("output_limit", "模型响应超过安全大小限制")
	}
	if cfg.Adapter == "gemini" {
		err = decodeGemini(raw, &result)
	} else {
		err = decodeOpenAI(raw, &result, false)
	}
	if err != nil {
		return result, err
	}
	if len(result.Text) > MaxOutputBytes {
		return Result{}, fail("output_limit", "模型输出超过安全大小限制")
	}
	if result.Text == "" {
		return result, fail("empty_response", "模型未返回可显示的文字，可能受内容策略或输出类型限制")
	}
	return result, nil
}
func validateInput(adapter string, in Request) error {
	if adapter != "openai_chat" && adapter != "gemini" {
		return fail("unsupported_adapter", "不支持的模型协议")
	}
	if strings.TrimSpace(in.Model) == "" || len(in.Model) > 256 || strings.ContainsAny(in.Model, "\r\n") {
		return fail("invalid_model", "模型标识无效")
	}
	if adapter == "gemini" {
		for _, c := range strings.TrimPrefix(in.Model, "models/") {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return fail("invalid_model", "Gemini 模型标识包含不支持的字符")
			}
		}
	}
	if len(in.Messages) == 0 || len(in.Messages) > 32 {
		return fail("input_limit", "对话历史必须在 1–32 条消息内，请新建测试对话")
	}
	if in.Temperature != nil && (*in.Temperature < 0 || *in.Temperature > 2) {
		return fail("invalid_parameter", "temperature 必须在 0–2 之间")
	}
	if in.MaxOutputTokens != nil && (*in.MaxOutputTokens < 1 || *in.MaxOutputTokens > 32768) {
		return fail("invalid_parameter", "输出限制必须在 1–32768 之间")
	}
	total := len(in.SystemPrompt)
	for _, message := range in.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return fail("invalid_role", "无效的对话角色")
		}
		if len(message.Files) > 4 {
			return fail("input_limit", "每条消息最多包含 4 个附件")
		}
		total += len(message.Text)
		for _, file := range message.Files {
			if len(file.Data) == 0 {
				return fail("invalid_attachment", "附件为空")
			}
			total += len(file.Data)
			image := file.MIME == "image/jpeg" || file.MIME == "image/png" || file.MIME == "image/webp"
			native := file.MIME == "application/pdf" || file.MIME == "video/mp4" || file.MIME == "video/webm" || file.MIME == "video/quicktime"
			if !image && !(adapter == "gemini" && native) {
				return fail("unsupported_input", "当前模型协议不支持此附件类型；未发送任何内容")
			}
		}
	}
	if total > MaxInputBytes {
		return fail("input_limit", "对话历史与附件总大小超过 24 MiB，请新建测试对话")
	}
	return nil
}
func makePayload(base *url.URL, adapter string, in Request) (map[string]any, string, error) {
	u := *base
	u.Path = strings.TrimRight(u.Path, "/")
	if adapter == "openai_chat" {
		u.Path += "/chat/completions"
		messages := make([]any, 0, len(in.Messages)+1)
		if in.SystemPrompt != "" {
			messages = append(messages, map[string]any{"role": "system", "content": in.SystemPrompt})
		}
		for _, m := range in.Messages {
			var content any = m.Text
			if len(m.Files) > 0 {
				parts := []any{map[string]any{"type": "text", "text": m.Text}}
				for _, f := range m.Files {
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + f.MIME + ";base64," + base64.StdEncoding.EncodeToString(f.Data)}})
				}
				content = parts
			}
			messages = append(messages, map[string]any{"role": m.Role, "content": content})
		}
		body := map[string]any{"model": in.Model, "messages": messages, "stream": in.Stream}
		if in.Stream {
			body["stream_options"] = map[string]any{"include_usage": true}
		}
		if in.Temperature != nil {
			body["temperature"] = *in.Temperature
		}
		if in.Model == "glm-4.6v-flashx" {
			body["chat_template_kwargs"] = map[string]any{"enable_thinking": true}
			if in.MaxOutputTokens != nil {
				body["max_tokens"] = *in.MaxOutputTokens
			}
		} else if in.MaxOutputTokens != nil {
			body["max_completion_tokens"] = *in.MaxOutputTokens
		}
		return body, u.String(), nil
	}
	method := "generateContent"
	if in.Stream {
		method = "streamGenerateContent"
		u.RawQuery = "alt=sse"
	}
	u.Path += "/models/" + strings.TrimPrefix(in.Model, "models/") + ":" + method
	contents := make([]any, 0, len(in.Messages))
	for _, m := range in.Messages {
		parts := []any{}
		if m.Text != "" {
			parts = append(parts, map[string]any{"text": m.Text})
		}
		for _, f := range m.Files {
			parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": f.MIME, "data": base64.StdEncoding.EncodeToString(f.Data)}})
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	body := map[string]any{"contents": contents}
	if in.SystemPrompt != "" {
		body["systemInstruction"] = map[string]any{"parts": []any{map[string]any{"text": in.SystemPrompt}}}
	}
	generation := map[string]any{}
	if in.Temperature != nil {
		generation["temperature"] = *in.Temperature
	}
	if in.MaxOutputTokens != nil {
		generation["maxOutputTokens"] = *in.MaxOutputTokens
	}
	if len(generation) > 0 {
		body["generationConfig"] = generation
	}
	return body, u.String(), nil
}
func decodeOpenAI(raw []byte, out *Result, delta bool) error {
	var frame struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			Prompt     *int64 `json:"prompt_tokens"`
			Completion *int64 `json:"completion_tokens"`
		} `json:"usage"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return fail("upstream_protocol", "模型服务返回无法识别的响应")
	}
	if len(frame.Error) > 0 && string(frame.Error) != "null" {
		return fail("upstream_error", "模型服务报告流式调用错误")
	}
	if len(frame.Choices) > 0 {
		c := frame.Choices[0]
		if delta {
			out.Text += c.Delta.Content
		} else {
			out.Text = c.Message.Content
		}
		if c.FinishReason != nil {
			out.FinishReason = *c.FinishReason
		}
	}
	if frame.Usage != nil {
		if (frame.Usage.Prompt != nil && *frame.Usage.Prompt < 0) || (frame.Usage.Completion != nil && *frame.Usage.Completion < 0) {
			return fail("upstream_protocol", "模型服务返回无效的用量数据")
		}
		out.InputTokens = frame.Usage.Prompt
		out.OutputTokens = frame.Usage.Completion
	}
	return nil
}
func decodeGemini(raw []byte, out *Result) error {
	var frame struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
			Finish string `json:"finishReason"`
		} `json:"candidates"`
		Usage *struct {
			Prompt     *int64 `json:"promptTokenCount"`
			Completion *int64 `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return fail("upstream_protocol", "模型服务返回无法识别的响应")
	}
	if len(frame.Error) > 0 && string(frame.Error) != "null" {
		return fail("upstream_error", "模型服务报告调用错误")
	}
	if len(frame.Candidates) > 0 {
		c := frame.Candidates[0]
		for _, p := range c.Content.Parts {
			if !p.Thought {
				out.Text += p.Text
			}
		}
		if c.Finish != "" {
			out.FinishReason = c.Finish
		}
	}
	if frame.Usage != nil {
		if (frame.Usage.Prompt != nil && *frame.Usage.Prompt < 0) || (frame.Usage.Completion != nil && *frame.Usage.Completion < 0) {
			return fail("upstream_protocol", "模型服务返回无效的用量数据")
		}
		out.InputTokens = frame.Usage.Prompt
		out.OutputTokens = frame.Usage.Completion
	}
	return nil
}
func readStream(body io.Reader, adapter string, onSnapshot func(string) error) (Result, error) {
	result := Result{}
	scanner := bufio.NewScanner(io.LimitReader(body, 32<<20))
	scanner.Buffer(make([]byte, 8192), maxResponseBytes)
	var data []string
	done := false
	consume := func() error {
		if len(data) == 0 {
			return nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		if raw == "[DONE]" {
			done = true
			return nil
		}
		var err error
		if adapter == "gemini" {
			err = decodeGemini([]byte(raw), &result)
		} else {
			err = decodeOpenAI([]byte(raw), &result, true)
		}
		if err != nil {
			return err
		}
		if len(result.Text) > MaxOutputBytes {
			return fail("output_limit", "模型输出超过安全大小限制")
		}
		if onSnapshot != nil {
			return onSnapshot(result.Text)
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := consume(); err != nil {
				return result, err
			}
			if done {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return result, &Error{Code: "upstream_incomplete", Message: "模型流式响应读取中断", Uncertain: true}
	}
	if err := consume(); err != nil {
		return result, err
	}
	if result.FinishReason == "" && !done {
		return result, &Error{Code: "upstream_incomplete", Message: "模型流式响应缺少完成标记", Uncertain: true}
	}
	if result.Text == "" {
		return result, fail("empty_response", "模型没有返回可显示的文字")
	}
	return result, nil
}

// ValidateFile uses signatures, not merely browser supplied MIME/extension.
func ValidateFile(data []byte) (kind, mime string, err error) {
	if len(data) == 0 || len(data) > 20<<20 {
		return "", "", fail("attachment_limit", "文件为空或超过 20 MiB")
	}
	detected := http.DetectContentType(data)
	switch detected {
	case "image/jpeg", "image/png", "image/webp":
		return "image", detected, nil
	case "application/pdf":
		return "pdf", detected, nil
	case "video/mp4", "video/webm":
		return "video", detected, nil
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		if string(data[8:12]) == "qt  " {
			return "video", "video/quicktime", nil
		}
		return "video", "video/mp4", nil
	}
	return "", "", fail("unsupported_file", "仅支持 JPEG、PNG、WebP、MP4、WebM、MOV 和 PDF 文件")
}

func (r Result) String() string { return fmt.Sprintf("model result (%d bytes)", len(r.Text)) }
