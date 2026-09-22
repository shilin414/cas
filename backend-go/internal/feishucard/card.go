// Package feishucard renders bounded, mobile-friendly Feishu preview cards.
package feishucard

import (
	"regexp"
	"strings"
	"unicode"
)

type Kind string

const (
	Conversation       Kind = "conversation"
	Result             Kind = "result"
	Query              Kind = "query"
	previewBudget           = 800
	queryPreviewBudget      = 4000
)

type Section struct{ Label, Text string }
type Options struct {
	AgentName, AgentIcon, AgentImageKey string
	Kind                                Kind
	Title, Subtitle, URL                string
	Sections                            []Section
}

// Build uses Feishu's native card components. Untrusted titles, names and
// content are plain_text (including native table cells), never executable
// mentions or card markup. The full
// Markdown remains unchanged on the read-only web page.
func Build(o Options) map[string]any { return build(o, true) }

// BuildLegacy keeps the previous plain-text card as the compatibility fallback.
func BuildLegacy(o Options) map[string]any { return build(o, false) }

func build(o Options, nativeTables bool) map[string]any {
	color, eyebrow, action := "blue", "对话分享", "查看完整对话"
	if o.Kind == Result {
		color, eyebrow, action = "green", "自动化 · 执行完成", "查看完整结果"
	}
	if o.Kind == Query {
		color, eyebrow, action = "blue", "业务查询 · 查询结果", "打开查询应用"
	}
	title := clip(strings.Join(strings.Fields(o.Title), " "), 72)
	if title == "" {
		title = eyebrow
	}
	elements := []any{}
	if o.AgentName != "" || o.Kind == Result {
		name := clip(strings.Join(strings.Fields(o.AgentName), " "), 64)
		if name == "" {
			name = "智能体"
		}
		icon := clip(strings.Join(strings.Fields(o.AgentIcon), " "), 12)
		if icon == "" {
			icon = "🤖"
		}
		author := []any{}
		if nativeTables && o.AgentImageKey != "" {
			author = append(author, map[string]any{"tag": "img", "img_key": o.AgentImageKey, "alt": map[string]any{"tag": "plain_text", "content": name}})
		} else {
			name = icon + " " + name
		}
		author = append(author, map[string]any{"tag": "plain_text", "content": name})
		elements = append(elements, map[string]any{"tag": "note", "elements": author})
	}

	if o.Subtitle != "" {
		elements = append(elements, note(clip(o.Subtitle, 120)))
	}
	elements = append(elements, map[string]any{"tag": "hr"})
	budget, maxLines := previewBudget, 14
	if o.Kind == Query {
		budget, maxLines = queryPreviewBudget, 160
	}
	remaining, shown, truncated := budget, 0, false
	tableCount := 0
	for _, section := range o.Sections {
		if shown >= 4 || remaining <= 0 {
			truncated = true
			break
		}
		limit := remaining
		// Reserve most of a conversation's budget for the answer.
		if section.Label == "提问" && limit > 180 {
			limit = 180
		}
		var body []any
		var used int
		var shortened bool
		if nativeTables {
			body, used, shortened = sectionPreview(section.Text, limit, maxLines, o.Kind == Query, &tableCount)
		} else {
			body, used, shortened = plainSectionPreview(section.Text, limit, maxLines, o.Kind == Query)
		}
		truncated = truncated || shortened
		if len(body) == 0 {
			continue
		}
		remaining -= used
		label := "✦ 内容预览"
		switch section.Label {
		case "查询条件":
			label = "查询条件"
		case "提问":
			label = "💬 提问"
		case "回答预览":
			label = "✦ 回答预览"
		case "结果预览":
			label = "✓ 查询结果"
		}
		elements = append(elements, map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": "**" + label + "**"}})
		elements = append(elements, body...)
		shown++
	}
	if shown == 0 {
		elements = append(elements, div("暂无文字内容，可打开完整页面查看附件或详情。"))
	}
	if truncated {
		hint := "正文为节选 · 完整内容请点击下方按钮"
		if o.Kind == Query {
			hint = "结果较长，卡片已展示主要信息 · 可打开应用查看同一份快照"
		}
		elements = append(elements, note(hint))
	}
	elements = append(elements, map[string]any{"tag": "hr"})
	if o.URL != "" {
		elements = append(elements, map[string]any{"tag": "action", "actions": []any{map[string]any{
			"tag": "button", "type": "primary", "text": map[string]any{"tag": "plain_text", "content": action}, "url": o.URL,
		}}})
	}
	if o.Kind == Query {
		elements = append(elements, note("查询结果已在卡片中，可直接阅读 · 打开应用需登录并具备权限 · 快照24小时有效"))
	} else {
		elements = append(elements, note("小安工作助手  ·  只读分享"))
	}
	return map[string]any{
		"config":   map[string]any{"wide_screen_mode": true, "enable_forward": true},
		"header":   map[string]any{"template": color, "title": map[string]any{"tag": "plain_text", "content": eyebrow + "｜" + title}},
		"elements": elements,
	}
}
func div(s string) map[string]any {
	return map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": s}}
}
func note(s string) map[string]any {
	return map[string]any{"tag": "note", "elements": []any{map[string]any{"tag": "plain_text", "content": s}}}
}
func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return strings.TrimSpace(string(runes[:n-1])) + "…"
}

var (
	markdownLink = regexp.MustCompile(`!?\[([^\]\n]*)\]\([^\)\n]*\)`)
	fence        = regexp.MustCompile("(?m)^\\s*(```|~~~)[^\\n]*$")
	heading      = regexp.MustCompile(`^#{1,6}\s+`)
	tableRule    = regexp.MustCompile(`^\s*\|?\s*:?-{3,}.*$`)
)

// previewText is intentionally a small readability transform, not an HTML
// renderer. Raw angle brackets remain inert because the component is plain_text.
func previewText(s string) string {
	// Bound normalization work independently of the original result size.
	s = clip(s, 4000)
	s = fence.ReplaceAllString(s, "")
	s = markdownLink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = heading.ReplaceAllString(line, "")
		line = strings.TrimPrefix(line, "> ")
		if tableRule.MatchString(line) {
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			line = "• " + line[2:]
		}
		line = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) && r != '\t' {
				return -1
			}
			return r
		}, line)
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
