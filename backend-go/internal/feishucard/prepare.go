package feishucard

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Prepared struct {
	Content    string
	Fallback   string
	Downgraded bool
}

// Prepare retains a bounded, independent legacy renderer. A conversion failure
// must never prevent an otherwise sendable plain-text preview from being sent.
func Prepare(o Options) (Prepared, error) { return prepareWithBuilder(o, Build) }
func prepareWithBuilder(o Options, build func(Options) map[string]any) (Prepared, error) {
	legacy, err := json.Marshal(BuildLegacy(o))
	if err != nil {
		return Prepared{}, err
	}
	primary, err := encodeConverted(o, build)
	if err != nil || len(primary) > 30*1024 {
		return Prepared{Content: string(legacy), Downgraded: true}, nil
	}
	p := Prepared{Content: string(primary)}
	if strings.Contains(p.Content, `"tag":"table"`) || strings.Contains(p.Content, `"tag":"img"`) {
		p.Fallback = string(legacy)
	}
	return p, nil
}
func encodeConverted(o Options, build func(Options) map[string]any) (raw []byte, err error) {
	defer func() {
		if recover() != nil {
			raw = nil
			err = fmt.Errorf("card conversion failed")
		}
	}()
	return json.Marshal(build(o))
}

// plainSectionPreview deliberately does not depend on the GFM converter.
func plainSectionPreview(raw string, budget, maxLines int, query bool) ([]any, int, bool) {
	value := previewText(raw)
	if query {
		value = clip(cleanPlainText(raw), queryPreviewBudget)
	}
	if strings.TrimSpace(value) == "" {
		return nil, 0, false
	}
	shortened := false
	lines := strings.Split(value, "\n")
	if len(lines) > maxLines {
		value = strings.Join(lines[:maxLines], "\n") + "\n…"
		shortened = true
	}
	if utf8.RuneCountInString(value) > budget {
		value = clip(value, budget)
		shortened = true
	}
	return []any{div(value)}, utf8.RuneCountInString(value), shortened
}
