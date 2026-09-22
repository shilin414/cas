package feishucard

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	tableast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	maxCardTables     = 5 // Feishu JSON 1.0 native-table limit.
	maxPreviewColumns = 6
	maxPreviewRows    = 8
)

type previewBlock struct {
	raw   string
	table *tableast.Table
}

// Parse GFM rather than guessing from pipes: fenced code, escaped separators,
// alignment and optional outer pipes follow the same syntax as the web preview.
func previewBlocks(source []byte) []previewBlock {
	document := goldmark.New(goldmark.WithExtensions(extension.Table)).Parser().Parse(text.NewReader(source))
	var blocks []previewBlock
	cursor := 0
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		table, ok := node.(*tableast.Table)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		header := table.FirstChild()
		if header == nil {
			return ast.WalkSkipChildren, nil
		}
		start := header.Pos()
		if start < 0 || start > len(source) || table.LastChild().Pos() < 0 || table.LastChild().Pos() > len(source) {
			return ast.WalkSkipChildren, nil
		}
		// Include container prefixes/indentation, not a partial Markdown source line.
		start = bytes.LastIndexByte(source[:start], '\n') + 1
		end := lineEnd(source, table.LastChild().Pos())
		if table.LastChild() == header {
			end = lineEnd(source, end)
		} // Include delimiter line for an empty table.
		if start < cursor || end < start || end > len(source) {
			return ast.WalkSkipChildren, nil
		}
		if start > cursor {
			blocks = append(blocks, previewBlock{raw: string(source[cursor:start])})
		}
		blocks = append(blocks, previewBlock{table: table})
		cursor = end
		return ast.WalkSkipChildren, nil
	})
	if cursor < len(source) {
		blocks = append(blocks, previewBlock{raw: string(source[cursor:])})
	}
	return blocks
}

func lineEnd(source []byte, start int) int {
	if start >= len(source) {
		return len(source)
	}
	if at := bytes.IndexByte(source[start:], '\n'); at >= 0 {
		return start + at + 1
	}
	return len(source)
}

func cleanPlainText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.ReplaceAll(value, "\r\n", "\n"))
}

func cellPlainText(cell ast.Node, source []byte) string {
	// The output is always data_type=text: decoded HTML/mentions stay inert.
	return strings.TrimSpace(cleanPlainText(html.UnescapeString(string(util.UnescapePunctuations(cell.Text(source))))))
}

// sectionPreview bounds parsing work, text, height, columns, rows and table count.
// Numeric strings are never parsed/reformatted or sliced in the middle of a row.
func sectionPreview(raw string, budget, maxLines int, query bool, tableCount *int) (elements []any, used int, truncated bool) {
	bounded := clip(raw, queryPreviewBudget)
	truncated = bounded != raw
	if truncated {
		// Do not parse a clipped last row into apparently complete native cells.
		if lastBreak := strings.LastIndexByte(bounded, '\n'); lastBreak >= 0 {
			bounded = bounded[:lastBreak]
		}
	}
	source := []byte(bounded)
	remaining, linesLeft := budget, maxLines
	for _, block := range previewBlocks(source) {
		if remaining <= 0 || linesLeft <= 0 {
			truncated = true
			break
		}
		if block.table == nil {
			value := previewText(block.raw)
			if query {
				value = strings.TrimSpace(cleanPlainText(block.raw))
			}
			if value == "" {
				continue
			}
			lines := strings.Split(value, "\n")
			if len(lines) > linesLeft {
				value = strings.Join(lines[:linesLeft], "\n") + "\n…"
				truncated = true
			}
			if utf8.RuneCountInString(value) > remaining {
				value = clip(value, remaining)
				truncated = true
			}
			remaining -= utf8.RuneCountInString(value)
			linesLeft -= len(strings.Split(value, "\n"))
			elements = append(elements, div(value))
			continue
		}
		if block.table.FirstChild().NextSibling() == nil {
			hint := "表格暂无数据。"
			if truncated {
				hint = "表格内容较长，请打开完整页面查看。"
			}
			empty := clip(hint, remaining)
			elements = append(elements, div(empty))
			remaining -= utf8.RuneCountInString(empty)
			linesLeft--
			continue
		}
		if *tableCount >= maxCardTables {
			truncated = true
			break
		}
		table, cost, height, shortened := nativeTable(block.table, source, remaining, linesLeft)
		truncated = truncated || shortened
		if table == nil {
			hint := clip("表格内容较长，请打开完整页面查看。", remaining)
			elements = append(elements, div(hint))
			remaining -= utf8.RuneCountInString(hint)
			truncated = true
			break
		}
		*tableCount++
		elements = append(elements, table)
		remaining -= cost
		linesLeft -= height
	}
	return elements, budget - remaining, truncated
}

func nativeTable(node *tableast.Table, source []byte, budget, maxLines int) (map[string]any, int, int, bool) {
	if maxLines < 2 {
		return nil, 0, 0, true
	}
	header := node.FirstChild()
	var columns []any
	cost := 0
	shortened := false
	for cell := header.FirstChild(); cell != nil; cell = cell.NextSibling() {
		if len(columns) >= maxPreviewColumns {
			shortened = true
			break
		}
		label := cellPlainText(cell, source)
		if label == "" {
			label = fmt.Sprintf("列 %d", len(columns)+1)
		}
		if utf8.RuneCountInString(label) > 80 {
			label = clip(label, 80)
			shortened = true
		}
		cost += utf8.RuneCountInString(label)
		align := "left"
		if c, ok := cell.(*tableast.TableCell); ok && (c.Alignment == tableast.AlignRight || c.Alignment == tableast.AlignCenter) {
			align = c.Alignment.String()
		}
		columns = append(columns, map[string]any{"name": fmt.Sprintf("c%d", len(columns)), "display_name": label, "data_type": "text", "width": "auto", "horizontal_align": align, "vertical_align": "top"})
	}
	if len(columns) == 0 || cost > budget {
		return nil, 0, 0, true
	}
	rows := make([]any, 0)
	for row := header.NextSibling(); row != nil; row = row.NextSibling() {
		if len(rows) >= maxPreviewRows || len(rows)+1 >= maxLines {
			shortened = true
			break
		}
		values := map[string]any{}
		rowCost := 0
		cell := row.FirstChild()
		for i := range columns {
			value := ""
			if cell != nil {
				value = cellPlainText(cell, source)
				cell = cell.NextSibling()
			}
			values[fmt.Sprintf("c%d", i)] = value
			rowCost += max(1, utf8.RuneCountInString(value))
		}
		if cost+rowCost > budget {
			shortened = true
			break
		}
		cost += rowCost
		rows = append(rows, values)
	}
	if len(rows) == 0 {
		return nil, 0, 0, true
	}
	return map[string]any{
		"tag": "table", "page_size": 5, "row_height": "middle", "freeze_first_column": true,
		"header_style": map[string]any{"bold": true, "lines": 1}, "columns": columns, "rows": rows,
	}, cost, len(rows) + 1, shortened
}
