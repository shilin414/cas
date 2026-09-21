package integration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func databaseWideRedisCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if selector.Sel.Name == "FlushDB" || selector.Sel.Name == "FlushAll" {
		return true
	}
	if selector.Sel.Name != "Do" {
		return false
	}
	for _, arg := range call.Args {
		if literal, ok := arg.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			value, err := strconv.Unquote(literal.Value)
			if err == nil && (strings.EqualFold(strings.TrimSpace(value), "FLUSHDB") || strings.EqualFold(strings.TrimSpace(value), "FLUSHALL")) {
				return true
			}
		}
	}
	return false
}
func TestIntegrationRedisResetIsNeverDatabaseWide(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		set := token.NewFileSet()
		node, err := parser.ParseFile(set, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(node, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && databaseWideRedisCall(call) {
				t.Errorf("database-wide Redis clearing is prohibited; delete only owned fixture keys: %s", set.Position(call.Pos()))
			}
			return true
		})
	}
}
func TestDatabaseWideRedisCallGuard(t *testing.T) {
	for expression, want := range map[string]bool{`client.FlushDB(ctx)`: true, `client.FlushAll(ctx)`: true, `client.Do(ctx,"FLUSHDB")`: true, `client.Do(ctx,"flushall")`: true, `client.Del(ctx,"owned:queue:fixture")`: false, `client.Get(ctx,"owned:key")`: false} {
		node, err := parser.ParseExpr(expression)
		if err != nil {
			t.Fatal(err)
		}
		if got := databaseWideRedisCall(node.(*ast.CallExpr)); got != want {
			t.Errorf("%s: %v", expression, got)
		}
	}
}
