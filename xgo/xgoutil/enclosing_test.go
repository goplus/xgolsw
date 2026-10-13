package xgoutil

import (
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathEnclosingInterval(t *testing.T) {
	t.Run("SelectionBoundaries", func(t *testing.T) {
		const source = `// Header.
package main

func run() {
	value := left + right // trailing
	println(value)
}
`
		for _, tt := range []struct {
			name      string
			selection string
			want      ast.Node
			exact     bool
			reverse   bool
			cursor    bool
		}{
			{name: "Identifier", selection: "left", want: (*ast.Ident)(nil), exact: true},
			{name: "CursorOnIdentifier", selection: "left", want: (*ast.Ident)(nil), exact: true, cursor: true},
			{name: "Operator", selection: "+", want: (*ast.BinaryExpr)(nil), exact: true},
			{name: "BinaryExpression", selection: "left + right", want: (*ast.BinaryExpr)(nil), exact: true},
			{name: "ReversedSelection", selection: "left + right", want: (*ast.BinaryExpr)(nil), exact: true, reverse: true},
			{name: "AdjacentWhitespace", selection: " left + right ", want: (*ast.BinaryExpr)(nil), exact: true},
			{name: "InteriorWhitespace", selection: " ", want: (*ast.BinaryExpr)(nil)},
			{name: "OverlappingChildren", selection: "value := left", want: (*ast.AssignStmt)(nil)},
			{name: "TrailingComment", selection: "// trailing", want: (*ast.BlockStmt)(nil)},
			{name: "BeforePackage", selection: "// Header.", want: (*ast.File)(nil)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				fset, file, err := newTestFile("main.xgo", source)
				require.NoError(t, err)
				tokenFile := fset.File(file.Pos())
				require.NotNil(t, tokenFile)
				offset := strings.Index(source, tt.selection)
				if tt.name == "InteriorWhitespace" {
					offset = strings.Index(source, "left +") + len("left")
				}
				require.NotEqual(t, -1, offset)
				start := tokenFile.Pos(offset)
				end := start + token.Pos(len(tt.selection))
				if tt.cursor {
					end = start
				}
				if tt.reverse {
					start, end = end, start
				}
				path, exact := PathEnclosingInterval(file, start, end)
				require.NotEmpty(t, path)
				assert.IsType(t, tt.want, path[0])
				assert.Same(t, file, path[len(path)-1])
				assert.Equal(t, tt.exact, exact)
			})
		}
	})

	t.Run("AfterFile", func(t *testing.T) {
		_, file, err := newTestFile("main.xgo", "println 1\n")
		require.NoError(t, err)
		path, exact := PathEnclosingInterval(file, file.End()+1, file.End()+2)
		assert.Equal(t, []ast.Node{file}, path)
		assert.False(t, exact)
	})

	t.Run("WholeFile", func(t *testing.T) {
		_, file, err := newTestFile("main.xgo", "package main\nfunc run() {}\n")
		require.NoError(t, err)
		path, exact := PathEnclosingInterval(file, file.Pos(), file.End())
		assert.Equal(t, []ast.Node{file}, path)
		assert.True(t, exact)
	})

	for _, tt := range []struct {
		name      string
		source    string
		selection string
		want      ast.Node
	}{
		{"ReceiveChannel", "var ch <-chan int", "<-chan", (*ast.ChanType)(nil)},
		{"SendChannel", "var ch chan<- int", "chan<-", (*ast.ChanType)(nil)},
		{"BidirectionalChannel", "var ch chan int", "chan", (*ast.ChanType)(nil)},
		{"StructKeyword", "type Record struct { Count int }", "struct", (*ast.StructType)(nil)},
		{"InterfaceKeyword", "type Reader interface { Read() int }", "interface", (*ast.InterfaceType)(nil)},
		{"MapKeyword", "var values map[string]int", "map", (*ast.MapType)(nil)},
		{"FunctionType", "var handler func(int)", "func", (*ast.FuncType)(nil)},
		{"GoKeyword", "go work()", "go", (*ast.GoStmt)(nil)},
		{"DeferKeyword", "defer work()", "defer", (*ast.DeferStmt)(nil)},
		{"ReturnKeyword", "return", "return", (*ast.ReturnStmt)(nil)},
		{"IfKeyword", "if ready { work() }", "if", (*ast.IfStmt)(nil)},
		{"ForKeyword", "for ready { work() }", "for", (*ast.ForStmt)(nil)},
		{"RangeKeyword", "for key := range values { work(key) }", "for", (*ast.RangeStmt)(nil)},
		{"SwitchKeyword", "switch value { case 1: work() }", "switch", (*ast.SwitchStmt)(nil)},
		{"SwitchCase", "switch value { case 1: work() }", "case", (*ast.CaseClause)(nil)},
		{"SwitchDefault", "switch value { default: work() }", "default", (*ast.CaseClause)(nil)},
		{"TypeSwitch", "switch value.(type) { case int: work() }", "switch", (*ast.TypeSwitchStmt)(nil)},
		{"SelectKeyword", "select { case ch <- value: }", "select", (*ast.SelectStmt)(nil)},
		{"SelectCase", "select { case ch <- value: }", "case", (*ast.CommClause)(nil)},
		{"SelectDefault", "select { default: work() }", "default", (*ast.CommClause)(nil)},
		{"SendOperator", "ch <- value", "<-", (*ast.SendStmt)(nil)},
		{"IncrementOperator", "count++", "++", (*ast.IncDecStmt)(nil)},
		{"DecrementOperator", "count--", "--", (*ast.IncDecStmt)(nil)},
		{"VariadicCall", "work(values...)", "...", (*ast.CallExpr)(nil)},
		{"ArrayEllipsis", "var values = [...]int{1, 2}", "...", (*ast.Ellipsis)(nil)},
		{"IndexBracket", "echo values[0]", "[", (*ast.IndexExpr)(nil)},
		{"MultipleTypeArguments", "work[int, string](1, name)", "[", (*ast.IndexListExpr)(nil)},
		{"SliceBracket", "echo values[1:3]", "[", (*ast.SliceExpr)(nil)},
		{"TypeAssertion", "echo value.(int)", ".", (*ast.TypeAssertExpr)(nil)},
		{"LabelColon", "done: return", ":", (*ast.LabeledStmt)(nil)},
		{"KeyValueColon", "echo map[string]int{key: value}", ":", (*ast.KeyValueExpr)(nil)},
		{"Parenthesis", "echo (left + right)", "(", (*ast.ParenExpr)(nil)},
		{"PointerType", "var pointer *Record", "*", (*ast.StarExpr)(nil)},
		{"UnaryOperator", "echo !ready", "!", (*ast.UnaryExpr)(nil)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const prefix = "func run() {\n"
			fset, file, err := newTestFile("main.xgo", prefix+tt.source+"\n}\n")
			require.NoError(t, err)
			tokenFile := fset.File(file.Pos())
			require.NotNil(t, tokenFile)
			offset := strings.Index(tt.source, tt.selection)
			require.NotEqual(t, -1, offset)
			start := tokenFile.Pos(len(prefix) + offset)
			path, exact := PathEnclosingInterval(file, start, start+token.Pos(len(tt.selection)))
			require.NotEmpty(t, path)
			assert.IsType(t, tt.want, path[0])
			assert.True(t, exact)
			assert.Same(t, file, path[len(path)-1])
		})
	}
}
