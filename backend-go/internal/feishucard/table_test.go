package feishucard

import (
	"encoding/json"
	"strings"
	"testing"
)

func cardTables(card map[string]any) []map[string]any {
	var tables []map[string]any
	for _, value := range card["elements"].([]any) {
		el := value.(map[string]any)
		if el["tag"] == "table" {
			tables = append(tables, el)
		}
	}
	return tables
}

func TestPreviewMarkdownTableUsesNativeCells(t *testing.T) {
	markdown := "## 销售情况\n\n查询完成。\n\n| 销售部 | 金额（元） | 数量 |\n|---|---:|---:|\n| 示例销售部 | 428,656.8 | 5,050.0 |\n\n指标说明。"
	for _, kind := range []Kind{Conversation, Result, Query} {
		t.Run(string(kind), func(t *testing.T) {
			card := Build(Options{Kind: kind, Sections: []Section{{Label: "回答预览", Text: markdown}}})
			tables := cardTables(card)
			if len(tables) != 1 {
				t.Fatalf("got %d native tables, want 1", len(tables))
			}
			columns := tables[0]["columns"].([]any)
			rows := tables[0]["rows"].([]any)
			if len(columns) != 3 || len(rows) != 1 {
				t.Fatalf("invalid shape: %#v", tables[0])
			}
			amount := columns[1].(map[string]any)
			if amount["display_name"] != "金额（元）" || amount["horizontal_align"] != "right" || amount["data_type"] != "text" {
				t.Fatalf("column metadata lost: %#v", amount)
			}
			if rows[0].(map[string]any)[amount["name"].(string)] != "428,656.8" {
				t.Fatal("amount formatting changed")
			}
			raw, _ := json.Marshal(card)
			if strings.Contains(string(raw), "|---") || !strings.Contains(string(raw), "指标说明") || !strings.Contains(string(raw), "查询完成") {
				t.Fatalf("table boundaries lost: %s", raw)
			}
		})
	}
}

func TestCardDisplayBrand(t *testing.T) {
	for _, kind := range []Kind{Conversation, Result} {
		raw, _ := json.Marshal(Build(Options{Kind: kind, Title: "测试"}))
		if strings.Contains(string(raw), "xiaoan-platform") || !strings.Contains(string(raw), "小安工作助手") {
			t.Fatalf("old brand in card: %s", raw)
		}
	}
}

func TestNativeTableParsingBoundariesAndSafety(t *testing.T) {
	cases := []struct {
		name, input string
		tables      int
		want        string
	}{
		{"fence is not a table", "```text\n| a | b |\n|---|---|\n| 1 | 2 |\n```", 0, "a | b"},
		{"optional pipes and CRLF", "a | b\r\n--- | ---:\r\none | 2\r\n", 1, "one"},
		{"escaped pipes and inline styles", "| **列一** | 列二 |\n|---|---|\n| a\\|b | `x\\|y` |", 1, "a|b"},
		{"quoted table", "> | a | b |\n> |---|---|\n> | one | two |", 1, "two"},
		{"prefix without blank", "前言\n| a | b |\n|---|---|\n| one | two |\n\n后记", 1, "前言"},
		{"empty table", "| a | b |\n|---|---|", 0, "暂无数据"},
		{"duplicate headers", "| a | a |\n|---|---|\n| one | two |", 1, "two"},
		{"inert mentions and links", "| name | content |\n|---|---|\n| **用户** | <at id=all>hi</at> [说明](https://example.test) |", 1, "说明"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := Build(Options{Kind: Conversation, Sections: []Section{{Label: "回答预览", Text: tc.input}}})
			tables := cardTables(card)
			if len(tables) != tc.tables {
				t.Fatalf("got %d tables: %#v", len(tables), card)
			}
			raw, _ := json.Marshal(card)
			if !strings.Contains(string(raw), tc.want) {
				t.Fatalf("missing %q: %s", tc.want, raw)
			}
			for _, table := range tables {
				for _, c := range table["columns"].([]any) {
					if c.(map[string]any)["data_type"] != "text" {
						t.Fatal("untrusted cell became executable markup")
					}
				}
			}
		})
	}
}

func TestNativeTableBudgetsAndFormatting(t *testing.T) {
	simple := "| 名称 | 数值 |\n|---|---:|\n| 测试 | 12,345.00 |\n\n"
	manyTables := strings.Repeat(simple, 10)
	manyRows := "| a | b |\n|---|---|\n" + strings.Repeat("| 1 | 2 |\n", 100)
	manyColumns := "| a | b | c | d | e | f | g | h |\n|---|---|---|---|---|---|---|---|\n| 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |"
	for _, input := range []string{manyTables, manyRows, manyColumns} {
		card := Build(Options{Kind: Conversation, Sections: []Section{{Label: "回答预览", Text: input}}})
		raw, _ := json.Marshal(card)
		if len(raw) > 12000 {
			t.Fatalf("unbounded card: %d bytes", len(raw))
		}
		if !strings.Contains(string(raw), "节选") {
			t.Fatal("silent truncation")
		}
		if len(cardTables(card)) > 5 {
			t.Fatal("provider table limit exceeded")
		}
		for _, table := range cardTables(card) {
			if len(table["rows"].([]any)) > maxPreviewRows || len(table["columns"].([]any)) > maxPreviewColumns {
				t.Fatal("unbounded dimensions")
			}
		}
	}
	// Never publish a partial numeric value merely to fit the remaining budget.
	card := Build(Options{Kind: Conversation, Sections: []Section{{Label: "回答预览", Text: strings.Repeat("序", 780) + "\n\n| 名称 | 数值 |\n|---|---|\n| 测试 | 12345678901234567890 |"}}})
	if len(cardTables(card)) != 0 {
		t.Fatal("table row should not be partly clipped")
	}
}

func TestCardShowsAgentIdentityAndKeepsLegacyAvatarFallback(t *testing.T) {
	options := Options{Title: "测试", AgentName: "创作助手", AgentIcon: "✨", AgentImageKey: "img_test", Sections: []Section{{Text: "回复"}}}
	native, _ := json.Marshal(Build(options))
	legacy, _ := json.Marshal(BuildLegacy(options))
	if !strings.Contains(string(native), `"img_key":"img_test"`) || !strings.Contains(string(native), "创作助手") {
		t.Fatalf("missing identity: %s", native)
	}
	if strings.Contains(string(legacy), "img_test") || !strings.Contains(string(legacy), "✨ 创作助手") {
		t.Fatalf("unsafe fallback: %s", legacy)
	}
}

func TestNativeTableDoesNotPublishAClippedSourceRow(t *testing.T) {
	card := Build(Options{Kind: Query, Sections: []Section{{Text: "| 数值 |\n|---|\n| " + strings.Repeat("9", 4500) + " |"}}})
	if len(cardTables(card)) != 0 {
		t.Fatal("partial source row published as a number")
	}
	raw, _ := json.Marshal(card)
	if !strings.Contains(string(raw), "较长") || strings.Contains(string(raw), "暂无数据") {
		t.Fatalf("misleading empty-result fallback: %s", raw)
	}
}
