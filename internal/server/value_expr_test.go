package server

import (
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValueExprTypes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   map[string]string
	}{
		{
			name: "DeclarationsAndAssignments",
			source: `type Name string
const First, Second Name = "first", "second"
var initial Name = "initial"
var first, second = First, 5
func run() {
	var local Name
	var number int
	local = "assigned"
	short := First
	local, short = "left", "right"
	local, number = "mixed", 42
	local += "suffix"
	var object struct { Value Name }
	object.Value = "field"
	var items [1]Name
	items[0] = "element"
	println local, short, number, object, items
}
`,
			want: map[string]string{
				`"first"`: "main.Name", `"second"`: "main.Name", `"initial"`: "main.Name",
				`"assigned"`: "main.Name", "First": "main.Name", `"left"`: "main.Name", `"right"`: "main.Name",
				`"field"`: "main.Name", `"element"`: "main.Name",
				"0": "int", "1": "int", "5": "int", `"mixed"`: "main.Name", "42": "int",
			},
		},
		{
			name: "Returns",
			source: `type Name = string
func wrap(s string) Name { return s }
func values() (Name, int) { return ("parenthesized"), 7 }
func nested() Name {
	func() int { return 9 }()
	return wrap("argument")
}
func closure() string {
	return func() Name { return "inner" }()
}
func named() (name Name) { return }
`,
			want: map[string]string{
				"s": "main.Name", `("parenthesized")`: "main.Name", "7": "int", "9": "int",
				`wrap("argument")`: "main.Name", `func() Name { return "inner" }()`: "string", `"inner"`: "main.Name",
			},
		},
		{
			name: "LambdaBoundary",
			source: `type Name string
func invoke(fn func() string) {}
func current() Name {
	invoke => {
		var value Name = "local"
		println value
		return "ordinary"
	}
	return "outer"
}
`,
			want: map[string]string{`"local"`: "main.Name", `"ordinary"`: "string", `"outer"`: "main.Name"},
		},
		{
			name: "MultipleResults",
			source: `func pair() (string, int) { return "first", 2 }
var text, number = pair()
func run() {
	left, right := pair()
	left, right = pair()
	println left, right
}
func forward() (string, int) { return pair() }
`,
			want: map[string]string{`"first"`: "string", "2": "int"},
		},
		{
			name: "PhysicalSource",
			source: `type Name string
func run() {
	var value Name
//line virtual.xgo:100:20
	value = "physical"
	println value
}
`,
			want: map[string]string{`"physical"`: "main.Name"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
			proj := s.getProj()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			pkg, err := proj.ASTPackage()
			require.NoError(t, err)
			got := make(map[string]string)
			for expr, typ := range valueExprTypes(pkg, info) {
				file := proj.Fset.File(expr.Pos())
				got[tt.source[file.Offset(expr.Pos()):file.Offset(expr.End())]] = typ.String()
			}
			assert.Equal(t, tt.want, got)
			count := 0
			for range valueExprTypes(pkg, info) {
				count++
				break
			}
			assert.Equal(t, 1, count)
		})
	}

	t.Run("IncompleteTypeInfo", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`func known() string { return "known" }
func unresolved() string {
	var value string = "local"
	println value
	return "unresolved"
}
func run() { println func() string { return "untyped" }() }
func external() string
`)})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		pkg, err := proj.ASTPackage()
		require.NoError(t, err)
		for ident := range info.Defs {
			if ident.Name == "unresolved" {
				delete(info.Defs, ident)
			}
		}
		for expr := range info.Types {
			if _, ok := expr.(*ast.FuncLit); ok {
				delete(info.Types, expr)
			}
		}
		var got []string
		for expr := range valueExprTypes(pkg, info) {
			got = append(got, requireValueAs[*ast.BasicLit](t, expr).Value)
		}
		assert.Equal(t, []string{`"known"`, `"local"`}, got)
	})

	t.Run("IncompleteResults", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`func current() string { return "first", "extra" }
`)})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.Error(t, err)
		pkg, err := proj.ASTPackage()
		require.NoError(t, err)
		var got []string
		for expr := range valueExprTypes(pkg, info) {
			got = append(got, requireValueAs[*ast.BasicLit](t, expr).Value)
		}
		assert.Equal(t, []string{`"first"`}, got)
	})

	t.Run("MissingInput", func(t *testing.T) {
		for _, tt := range []struct {
			pkg  *ast.Package
			info *types.Info
		}{
			{}, {pkg: &ast.Package{}}, {info: &types.Info{}},
		} {
			count := 0
			for range valueExprTypes(tt.pkg, tt.info) {
				count++
			}
			assert.Zero(t, count)
		}
	})
}
