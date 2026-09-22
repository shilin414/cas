package feishucard

import (
	"strings"
	"testing"
)

func TestPreparedCardHasLegacyAlternateOnlyForNativeTables(t *testing.T) {
	o := Options{Kind: Conversation, Title: "测试", URL: "https://example.test/share/1", Sections: []Section{{Label: "回答预览", Text: "| a | b |\n|---|---|\n| one | two |"}}}
	p, err := Prepare(o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Content, `"tag":"table"`) || strings.Contains(p.Fallback, `"tag":"table"`) || !strings.Contains(p.Fallback, "小安工作助手") || !strings.Contains(p.Fallback, "one") {
		t.Fatalf("invalid prepared cards: %+v", p)
	}
	o.Sections[0].Text = "普通文字"
	p, err = Prepare(o)
	if err != nil || p.Fallback != "" {
		t.Fatalf("unnecessary alternate: %+v %v", p, err)
	}
}

func TestLocalConversionFailureUsesPlainCardWithoutSendingBrokenPayload(t *testing.T) {
	o := Options{Title: "测试", Sections: []Section{{Text: "保留的原文"}}}
	p, err := prepareWithBuilder(o, func(Options) map[string]any { panic("parser failure") })
	if err != nil || !p.Downgraded || p.Fallback != "" || !strings.Contains(p.Content, "保留的原文") {
		t.Fatalf("failed to downgrade: %+v %v", p, err)
	}
	p, err = prepareWithBuilder(o, func(Options) map[string]any { return map[string]any{"unsupported": make(chan int)} })
	if err != nil || !p.Downgraded {
		t.Fatalf("encode failure not downgraded: %+v %v", p, err)
	}
}
