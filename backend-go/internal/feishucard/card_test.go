package feishucard

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCardPreviewBudgetAndSafeContent(t *testing.T) {
	card := Build(Options{Title: strings.Repeat("日报", 200), Subtitle: "吴同学 · 6 条消息", Kind: Conversation, URL: "https://studio.example/share/token", Sections: []Section{{Label: "提问", Text: strings.Repeat("问", 500)}, {Label: "回答预览", Text: "<at id=all>所有人</at> " + strings.Repeat("中🙂", 1500)}}})
	raw, err := json.Marshal(card)
	if err != nil || !utf8.Valid(raw) {
		t.Fatalf("invalid card: %s %v", raw, err)
	}
	if len(raw) > 12000 {
		t.Fatalf("unbounded card: %d", len(raw))
	}
	elements := card["elements"].([]any)
	var bodies string
	for _, e := range elements {
		el := e.(map[string]any)
		if text, ok := el["text"].(map[string]any); ok && text["tag"] == "plain_text" {
			bodies += text["content"].(string)
		}
	}
	if !strings.Contains(bodies, "<at id=all>") {
		t.Fatal("answer not included as inert plain text")
	}
	if !strings.Contains(string(raw), "查看完整对话") || !strings.Contains(string(raw), "节选") {
		t.Fatal("missing action/truncation hint")
	}
	if card["header"].(map[string]any)["template"] != "blue" {
		t.Fatal("conversation theme")
	}
}

func TestResultCardEmptyAndMarkdown(t *testing.T) {
	card := Build(Options{Title: "每日巡检", Kind: Result, URL: "https://studio.example/share/result", Sections: []Section{{Label: "结果预览", Text: ""}}})
	raw, _ := json.Marshal(card)
	for _, s := range []string{"查看完整结果", "暂无文字内容", "green", "每日巡检"} {
		if !strings.Contains(string(raw), s) {
			t.Fatalf("missing %s", s)
		}
	}
	if got := previewText("## 检查结果\n**正常**\n- [报告](https://example.com)\n```go\nhello()\n```"); strings.Contains(got, "##") || strings.Contains(got, "**") || strings.Contains(got, "```") || strings.Contains(got, "https://") {
		t.Fatalf("unreadable markdown: %s", got)
	}
}

func TestCardBoundedManySections(t *testing.T) {
	sections := make([]Section, 1000)
	for i := range sections {
		sections[i] = Section{Label: "回答预览", Text: strings.Repeat("字", 100)}
	}
	raw, _ := json.Marshal(Build(Options{Kind: Conversation, Sections: sections}))
	if len(raw) > 12000 {
		t.Fatalf("too many sections: %d", len(raw))
	}
}

func TestCardShortAndMultilineBounds(t *testing.T) {
	short := Build(Options{Kind: Result, Title: "日报", URL: "https://studio.example/share/a", Sections: []Section{{Label: "结果预览", Text: "全部正常🙂"}}})
	raw, _ := json.Marshal(short)
	if strings.Contains(string(raw), "正文为节选") || !strings.Contains(string(raw), "全部正常🙂") {
		t.Fatal("short result should remain complete")
	}
	long := Build(Options{Kind: Result, Sections: []Section{{Label: "结果预览", Text: strings.Repeat("检查正常\n", 100)}}})
	raw, _ = json.Marshal(long)
	if !strings.Contains(string(raw), "正文为节选") || strings.Count(string(raw), "检查正常") > 14 {
		t.Fatal("tall card not bounded")
	}
}
