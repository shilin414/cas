package businessapps

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestQuerySnapshotMetadataAndCard(t *testing.T) {
	snapshot, e := NewQuerySnapshot(7, 3, "barcode-query", "barcode-query", "条码信息查询", Input{Action: "flow", Barcode: "01234567890123456789", Password: "never-save"}, json.RawMessage(`"工厂: A\\r\\n<at id=all>"`), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "never-save") {
		t.Fatal("unrelated secret persisted")
	}
	card := QueryResultCard(snapshot, "测试用户", "https://example.test/base/app/barcode-query?result="+snapshot.Token)
	b, _ := json.Marshal(card)
	if !strings.Contains(string(b), "流向记录") || !strings.Contains(string(b), "01234567890123456789") || !strings.Contains(string(b), "打开查询应用") || !strings.Contains(string(b), "查询结果已在卡片中") {
		t.Fatal(string(b))
	}
	if len(b) > 28000 {
		t.Fatal("oversized card")
	}
}
func TestQueryCardShowsReadableMultiRecordDetailsWithoutRequiringClickThrough(t *testing.T) {
	rows := make([]map[string]any, 20)
	for i := range rows {
		rows[i] = map[string]any{
			"物料编码": fmt.Sprintf("MAT-%02d", i+1),
			"物料名称": fmt.Sprintf("测试物料-%02d-用于验证转发卡片能够承载充足查询信息", i+1),
			"工厂":   "晋江工厂",
			"状态":   "可用",
		}
	}
	rawData, _ := json.Marshal(map[string]any{"output": rows, "code": 0, "message": "success"})
	snapshot, _ := NewQuerySnapshot(1, 2, "material-query", "material-query", "物料信息查询", Input{Action: "article", Query: "A8305"}, rawData, time.Now())
	cardRaw, _ := json.Marshal(QueryResultCard(snapshot, "User", "https://example.test/app/material-query?result="+snapshot.Token))
	cardText := string(cardRaw)
	for _, want := range []string{"共 20 条记录", "记录 1", "物料编码：MAT-01", "物料名称：测试物料-01", "MAT-20", "打开查询应用"} {
		if !strings.Contains(cardText, want) {
			t.Fatalf("card missing %q: %s", want, cardText)
		}
	}
	if strings.Contains(cardText, "查看完整结果（需登录）") {
		t.Fatal("query card should not tell recipients they must click through to read the result")
	}
	if len(cardRaw) > 28000 {
		t.Fatalf("oversized card: %d", len(cardRaw))
	}
}

func TestOnlyQueriesCanCreateSnapshot(t *testing.T) {
	if _, e := NewQuerySnapshot(7, 3, "oa-password", "oa-password", "密码", Input{}, json.RawMessage(`{}`), time.Now()); e == nil {
		t.Fatal("password snapshot accepted")
	}
}
func TestQuerySnapshotURLAndLongResult(t *testing.T) {
	snapshot, _ := NewQuerySnapshot(1, 2, "material-query", "material-query", "物料", Input{Action: "article", Query: "A8305"}, json.RawMessage(`[{"name":"`+strings.Repeat("物料", 12000)+`"}]`), time.Now())
	link, e := QuerySnapshotURL("https://example.test/xiaoan-platform/", snapshot)
	if e != nil || !strings.HasPrefix(link, "https://example.test/xiaoan-platform/app/material-query?result=") {
		t.Fatal(link, e)
	}
	card := QueryResultCard(snapshot, "User", link)
	raw, _ := json.Marshal(card)
	if len(raw) > 28000 || !strings.Contains(string(raw), "节选") {
		t.Fatal(len(raw))
	}
	if _, e := QuerySnapshotURL("", snapshot); e == nil {
		t.Fatal("empty origin accepted")
	}
}

func TestQueryCardRemainsBoundedWhenJSONEscapesPlainText(t *testing.T) {
	snapshot, _ := NewQuerySnapshot(7, 1, "barcode-query", "barcode-query", "条码", Input{Action: "production", Barcode: "01234567890123456789"}, json.RawMessage(`"`+strings.Repeat("<", 12000)+`"`), time.Now())
	raw, _ := json.Marshal(QueryResultCard(snapshot, "User", "https://example.test/app/barcode-query?result="+snapshot.Token))
	if len(raw) > 28000 {
		t.Fatalf("escaped card exceeded transport budget: %d", len(raw))
	}
	if !strings.Contains(string(raw), "结果较长") {
		t.Fatal("large result should disclose that the card is truncated")
	}
}

func TestQueryCardPreservesLiteralIdentifiersAndEmptyResult(t *testing.T) {
	snapshot, _ := NewQuerySnapshot(7, 1, "material-query", "material-query", "物料", Input{Action: "article", Query: "A__B_12"}, json.RawMessage(`null`), time.Now())
	raw, _ := json.Marshal(QueryResultCard(snapshot, "User", "https://example.test/"))
	if !strings.Contains(string(raw), "A__B_12") || !strings.Contains(string(raw), "未查询到相关记录") {
		t.Fatal(string(raw))
	}
}
