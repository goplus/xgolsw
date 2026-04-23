package analysisutil

import (
	"testing"

	"github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleDoc = `// Package foo is a test package.
//
// # Analyzer mycheck
//
// mycheck: checks something important
//
// Full description of mycheck.
package foo
`

func TestExtractDoc(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		result, err := ExtractDoc(sampleDoc, "mycheck")
		require.NoError(t, err)
		assert.Equal(t, "checks something important\n\nFull description of mycheck.", result)
	})

	t.Run("name not found", func(t *testing.T) {
		_, err := ExtractDoc(sampleDoc, "other")
		assert.Error(t, err)
	})

	t.Run("empty content", func(t *testing.T) {
		_, err := ExtractDoc("", "mycheck")
		assert.Error(t, err)
	})

	t.Run("not go source", func(t *testing.T) {
		_, err := ExtractDoc("not valid go !!!", "mycheck")
		assert.Error(t, err)
	})

	t.Run("no doc comment", func(t *testing.T) {
		_, err := ExtractDoc("package foo\n", "mycheck")
		assert.Error(t, err)
	})

	t.Run("heading without summary line", func(t *testing.T) {
		doc := "// Some text.\n//\n// # Analyzer bad\n//\n// no colon summary here\npackage foo\n"
		_, err := ExtractDoc(doc, "bad")
		assert.Error(t, err)
	})
}

func TestMustExtractDoc(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		result := MustExtractDoc(sampleDoc, "mycheck")
		assert.Equal(t, "checks something important\n\nFull description of mycheck.", result)
	})

	t.Run("panic on error", func(t *testing.T) {
		assert.Panics(t, func() {
			MustExtractDoc("", "mycheck")
		})
	})
}

func TestNoSideEffects(t *testing.T) {
	tests := []struct {
		name string
		expr ast.Expr
		want bool
	}{
		{"BasicLit", &ast.BasicLit{Kind: xgotoken.INT, Value: "1"}, true},
		{"Ident", &ast.Ident{Name: "x"}, true},
		{"ParenExpr", &ast.ParenExpr{X: &ast.Ident{Name: "x"}}, true},
		{"UnaryExpr non-channel", &ast.UnaryExpr{Op: xgotoken.NOT, X: &ast.Ident{Name: "x"}}, true},
		{"UnaryExpr channel receive", &ast.UnaryExpr{Op: xgotoken.ARROW, X: &ast.Ident{Name: "ch"}}, false},
		{"BinaryExpr", &ast.BinaryExpr{X: &ast.Ident{Name: "a"}, Op: xgotoken.ADD, Y: &ast.Ident{Name: "b"}}, true},
		{"StarExpr", &ast.StarExpr{X: &ast.Ident{Name: "p"}}, false},
		{"SelectorExpr", &ast.SelectorExpr{X: &ast.Ident{Name: "s"}, Sel: &ast.Ident{Name: "f"}}, true},
		{"IndexExpr", &ast.IndexExpr{X: &ast.Ident{Name: "s"}, Index: &ast.BasicLit{Kind: xgotoken.INT, Value: "0"}}, false},
		{"SliceExpr", &ast.SliceExpr{X: &ast.Ident{Name: "s"}}, false},
		{"TypeAssertExpr", &ast.TypeAssertExpr{X: &ast.Ident{Name: "x"}, Type: &ast.Ident{Name: "int"}}, false},
		{"CallExpr has side effects", &ast.CallExpr{Fun: &ast.Ident{Name: "f"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NoSideEffects(tt.expr))
		})
	}
}

func TestPrintExpr(t *testing.T) {
	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{"BasicLit", &ast.BasicLit{Kind: xgotoken.INT, Value: "42"}, "42"},
		{"Ident", &ast.Ident{Name: "x"}, "x"},
		{"ParenExpr", &ast.ParenExpr{X: &ast.Ident{Name: "x"}}, "(x)"},
		{"SelectorExpr", &ast.SelectorExpr{X: &ast.Ident{Name: "a"}, Sel: &ast.Ident{Name: "b"}}, "a.b"},
		{"IndexExpr", &ast.IndexExpr{X: &ast.Ident{Name: "s"}, Index: &ast.Ident{Name: "i"}}, "s[i]"},
		{"StarExpr", &ast.StarExpr{X: &ast.Ident{Name: "p"}}, "*p"},
		{"UnaryExpr", &ast.UnaryExpr{Op: xgotoken.SUB, X: &ast.Ident{Name: "x"}}, "-x"},
		{"BinaryExpr", &ast.BinaryExpr{X: &ast.Ident{Name: "a"}, Op: xgotoken.ADD, Y: &ast.Ident{Name: "b"}}, "a + b"},
		{"CallExpr", &ast.CallExpr{Fun: &ast.Ident{Name: "f"}}, "f(...)"},
		{"CompositeLit with type", &ast.CompositeLit{Type: &ast.Ident{Name: "T"}}, "T{...}"},
		{"CompositeLit without type", &ast.CompositeLit{}, "{...}"},
		{"TypeAssertExpr", &ast.TypeAssertExpr{X: &ast.Ident{Name: "x"}, Type: &ast.Ident{Name: "int"}}, "x.(int)"},
		{"SliceExpr", &ast.SliceExpr{X: &ast.Ident{Name: "s"}}, "s[:]"},
		{"unknown expr", &ast.CompositeLit{Elts: []ast.Expr{&ast.Ident{Name: "a"}}}, "{...}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PrintExpr(tt.expr))
		})
	}
}
