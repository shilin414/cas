package aily

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractTextNestedObject(t *testing.T) {
	m := Mapper{}
	// Real-environment shape: delta text arrives as a nested content object.
	data := map[string]any{"text": map[string]any{"text": "收到", "type": "content"}}
	if got := m.ExtractText(data); got != "收到" {
		t.Fatalf("nested text = %q, want 收到", got)
	}
	// Plain string still works.
	if got := m.ExtractText(map[string]any{"text": "hello"}); got != "hello" {
		t.Fatalf("plain text = %q, want hello", got)
	}
	// Missing → empty, never a crash.
	if got := m.ExtractText(map[string]any{}); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestExtractArtifactsCrossItemPairing(t *testing.T) {
	m := Mapper{}
	// Real-environment shape: markdown text item and artifact entry are two
	// separate content items; the filename pairs them.
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "这是图 ![a](artifacts/报告/画图.png) 与 ![b](artifacts/dog2.png)"},
			map[string]any{"type": "text", "text": "额外文字"},
			map[string]any{"type": "image", "agent_artifact_id": "art-1", "artifact_type": "sandbox_file"},
			map[string]any{"type": "image", "agent_artifact_id": "art-2", "artifact_type": "sandbox_file"},
		},
	}
	arts := m.ExtractArtifacts(result)
	if len(arts) != 2 {
		t.Fatalf("got %d artifacts, want 2", len(arts))
	}
	if arts[0].Name != "画图.png" {
		t.Fatalf("artifact[0].name = %q, want 画图.png (multi-level dir)", arts[0].Name)
	}
	if arts[1].Name != "dog2.png" {
		t.Fatalf("artifact[1].name = %q, want dog2.png", arts[1].Name)
	}
}

func TestExtractArtifactsNamedEntryWins(t *testing.T) {
	m := Mapper{}
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "![x](artifacts/from-md.png)"},
			map[string]any{"type": "file", "agent_artifact_id": "art-9", "artifact_type": "sandbox_file", "name": "official.png"},
		},
	}
	arts := m.ExtractArtifacts(result)
	if len(arts) != 1 || arts[0].Name != "official.png" {
		t.Fatalf("named entry should keep its own name, got %+v", arts)
	}
}

func TestMapProviderStatus(t *testing.T) {
	m := Mapper{}
	cases := map[string]string{
		"Completed": "succeeded",
		"Running":   "running",
		"Failed":    "failed",
		"Cancelled": "cancelled",
		"Weird":     "",
		"":          "",
	}
	for in, want := range cases {
		if got := m.MapProviderStatus(in); got != want {
			t.Fatalf("MapProviderStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToUnifiedUnknownEventKeepsRunAlive(t *testing.T) {
	m := Mapper{}
	// Undocumented event shape must not crash or fail the run.
	events := m.ToUnified("aily_custom_future_event", map[string]any{"foo": "bar"})
	if len(events) != 0 {
		t.Fatalf("unknown textless event should map to nothing, got %+v", events)
	}
	events = m.ToUnified("aily_custom_future_event", map[string]any{"text": "hi"})
	if len(events) != 1 || events[0].Type != "content.delta" {
		t.Fatalf("unknown text event should pass through as delta, got %+v", events)
	}
}

func TestToUnifiedArtifactDiscovery(t *testing.T) {
	m := Mapper{}
	events := m.ToUnified("message", map[string]any{
		"agent_artifact_id": "art-42",
		"artifact_type":     "sandbox_file",
	})
	found := false
	for _, ev := range events {
		if ev.Type == "artifact.discovered" && ev.Payload["external_artifact_id"] == "art-42" {
			found = true
		}
	}
	if !found {
		t.Fatalf("artifact discovery missing from %+v", events)
	}
}

func TestParseSSEDataTolerant(t *testing.T) {
	m := Mapper{}
	// Malformed JSON keeps the raw payload — never kills the run.
	data := m.ParseSSEData([]byte(`{"text": "unclosed`))
	if _, ok := data["raw"]; !ok {
		t.Fatalf("malformed payload should be preserved raw, got %v", data)
	}
	good := m.ParseSSEData([]byte(`{"text": "ok"}`))
	if good["text"] != "ok" {
		t.Fatalf("good payload parsed wrong: %v", good)
	}
}

func TestNormalizeArtifactType(t *testing.T) {
	m := Mapper{}
	if got := m.NormalizeArtifactType("sandbox_file"); got != "file" {
		t.Fatalf("sandbox_file → %q", got)
	}
	if got := m.NormalizeArtifactType("image"); got != "image" {
		t.Fatalf("image → %q", got)
	}
	if got := m.NormalizeArtifactType(""); got != "file" {
		t.Fatalf("empty → %q", got)
	}
}

// SSE line-level behavior: event names set state; data lines map.
func TestStreamEventNameSwitching(t *testing.T) {
	m := Mapper{}
	payloads := []string{
		`{"agent_chat_id": "777", "session_id": "conversation_x", "event": "start"}`,
		`{"text": {"text": "收到", "type": "content"}}`,
	}
	var (
		chatID  string
		session string
		texts   []string
	)
	for _, raw := range payloads {
		parsed := m.ParseSSEData([]byte(raw))
		if v, ok := parsed["agent_chat_id"].(string); ok && v != "" {
			chatID = v
		}
		if v, ok := parsed["session_id"].(string); ok && v != "" {
			session = v
		}
		if txt := m.ExtractText(parsed); txt != "" {
			texts = append(texts, txt)
		}
	}
	if chatID != "777" || session != "conversation_x" {
		t.Fatalf("identity not surfaced: chat=%q session=%q", chatID, session)
	}
	if strings.Join(texts, "") != "收到" {
		t.Fatalf("text not extracted: %v", texts)
	}
	var _ = json.Marshal
}

func TestGalleryArtifactKeepsDirectoryIdentity(t *testing.T) {
	result := map[string]any{"content": []any{
		map[string]any{"text": "![snow](artifacts/landscape_gallery/snow.jpg) ![forest](artifacts/landscape_gallery/forest.jpg) ![chart](artifacts/chart.png)"},
		map[string]any{"agent_artifact_id": "gallery", "artifact_type": "sandbox_gallery"},
		map[string]any{"agent_artifact_id": "chart", "artifact_type": "sandbox_file"},
	}}
	arts := (Mapper{}).ExtractArtifacts(result)
	if len(arts) != 2 || arts[0].Name != "landscape_gallery" || arts[1].Name != "chart.png" {
		t.Fatalf("wrong gallery pairing: %+v", arts)
	}
}

// Shape reproduced from the affected completed Aily chat: earlier content
// entries are progress messages; the last text entry is the user-facing reply.
func TestExtractFinalTextDoesNotConcatenateExecutionProcess(t *testing.T) {
	result := map[string]any{"status": "Completed", "content": []any{
		map[string]any{"type": "text", "text": "我先获取权限清单、SQL规范和指标定义。"},
		map[string]any{"type": "text", "text": "权限校验和SQL规范已获取。现在执行权限查询。"},
		map[string]any{"type": "text", "text": "已获取权限，正在加载字段定义。"},
		map[string]any{"type": "text", "text": "正在检查维度归属。"},
		map[string]any{"type": "text", "text": "已确定查询维度。"},
		map[string]any{"type": "text", "text": "正在执行查询。"},
		map[string]any{"type": "text", "text": "查询完成。\n\n#### 销售情况\n\n| 数量 | 金额 |\n|---|---|\n| 10 | 100 |"},
	}}
	want := "查询完成。\n\n#### 销售情况\n\n| 数量 | 金额 |\n|---|---|\n| 10 | 100 |"
	if got := (Mapper{}).ExtractFinalText(result); got != want {
		t.Fatalf("execution process leaked into final reply: got %q, want %q", got, want)
	}
}

func TestSplitResponseTextPreservesBoundaries(t *testing.T) {
	cases := []struct {
		name           string
		status         string
		items          []any
		final, process string
	}{
		{"one answer keeps all paragraphs", "Completed", []any{map[string]any{"type": "text", "text": "先获取权限也可以是用户要求的答案。\n\n第二段\n![图](artifacts/a.png)"}}, "先获取权限也可以是用户要求的答案。\n\n第二段\n![图](artifacts/a.png)", ""},
		{"progress separated from last text with trailing artifacts", "Completed", []any{map[string]any{"type": "text", "text": "过程一"}, map[string]any{"type": "text", "text": "过程二"}, map[string]any{"type": "text", "text": "答案 ![a](artifacts/a.png) ![b](artifacts/b.png)"}, map[string]any{"type": "image", "agent_artifact_id": "gallery", "artifact_type": "sandbox_gallery"}}, "答案 ![a](artifacts/a.png) ![b](artifacts/b.png)", "过程一\n\n过程二"},
		{"malformed and blank items ignored", "Completed", []any{nil, 42, map[string]any{"type": "text", "text": 42}, map[string]any{"type": "tool", "text": "tool log"}, map[string]any{"type": "text", "text": "答案"}, map[string]any{"type": "text", "text": " "}}, "答案", ""},
		{"no text", "Completed", []any{}, "", ""},
		{"failed result is only process", "Failed", []any{map[string]any{"type": "text", "text": "未完成内容"}}, "", "未完成内容"},
		{"cancelled result is only process", "Cancelled", []any{map[string]any{"type": "text", "text": "未完成内容"}}, "", "未完成内容"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := map[string]any{"status": tc.status, "content": tc.items}
			final, process := (Mapper{}).SplitResponseText(result)
			if final != tc.final || process != tc.process {
				t.Fatalf("got (%q,%q), want (%q,%q)", final, process, tc.final, tc.process)
			}
		})
	}
}
