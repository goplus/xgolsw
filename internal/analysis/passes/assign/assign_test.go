package assign

import (
	"go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssign(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{
			name: "self-assignment variable",
			src: `
var x int
x = x
`,
			wantDiag: true,
		},
		{
			name: "different variables",
			src: `
var x, y int
x = y
_ = x
`,
			wantDiag: false,
		},
		{
			name: "define assignment skipped",
			src: `
x := 1
_ = x
`,
			wantDiag: false,
		},
		{
			name: "selector self-assignment",
			src: `
type T struct{ f int }
var s T
s.f = s.f
`,
			wantDiag: true,
		},
		{
			name: "multiple lhs different rhs",
			src: `
var a, b int
a, b = b, a
_ = a
_ = b
`,
			wantDiag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.xgo", tt.src, parser.ParseComments)
			require.NoError(t, err)

			info := &xgotypes.Info{
				Info: typesutil.Info{
					Types: make(map[ast.Expr]types.TypeAndValue),
					Defs:  make(map[*ast.Ident]types.Object),
					Uses:  make(map[*ast.Ident]types.Object),
				},
			}

			checker := typesutil.NewChecker(
				&types.Config{Error: func(err error) {}},
				&typesutil.Config{
					Fset:  fset,
					Types: types.NewPackage("test", "test"),
				},
				nil,
				&info.Info,
			)

			if err := checker.Files(nil, []*ast.File{f}); err != nil {
				t.Log("type checking error:", err)
			}

			var diagnostics []protocol.Diagnostic
			pass := &protocol.Pass{
				Fset:      fset,
				Files:     []*ast.File{f},
				TypesInfo: info,
				Report: func(d protocol.Diagnostic) {
					diagnostics = append(diagnostics, d)
				},
				ResultOf: map[*protocol.Analyzer]any{
					inspect.Analyzer: inspector.New([]*ast.File{f}),
				},
			}

			_, err = Analyzer.Run(pass)
			require.NoError(t, err)

			for _, d := range diagnostics {
				t.Logf("got diagnostic: %v", d)
			}
			assert.Equal(t, tt.wantDiag, len(diagnostics) > 0)
		})
	}
}

// runAssign is a helper that parses src, type-checks it with an importer, runs
// the assign analyzer and returns the collected diagnostics.
func runAssign(t *testing.T, src string) []protocol.Diagnostic {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.xgo", src, parser.ParseComments)
	require.NoError(t, err)

	info := &xgotypes.Info{
		Info: typesutil.Info{
			Types: make(map[ast.Expr]types.TypeAndValue),
			Defs:  make(map[*ast.Ident]types.Object),
			Uses:  make(map[*ast.Ident]types.Object),
		},
	}
	checker := typesutil.NewChecker(
		&types.Config{Error: func(error) {}},
		&typesutil.Config{Fset: fset, Types: types.NewPackage("test", "test")},
		nil, &info.Info,
	)
	if err := checker.Files(nil, []*ast.File{f}); err != nil {
		t.Log("type checking error:", err)
	}

	var diagnostics []protocol.Diagnostic
	pass := &protocol.Pass{
		Fset:      fset,
		Files:     []*ast.File{f},
		TypesInfo: info,
		Report:    func(d protocol.Diagnostic) { diagnostics = append(diagnostics, d) },
		ResultOf:  map[*protocol.Analyzer]any{inspect.Analyzer: inspector.New([]*ast.File{f})},
	}
	_, err = Analyzer.Run(pass)
	require.NoError(t, err)
	return diagnostics
}

func TestAssignSameExprVariants(t *testing.T) {
	// IndexExpr self-assignment: s[0] = s[0]
	t.Run("index self-assignment", func(t *testing.T) {
		diags := runAssign(t, `
var s [3]int
s[0] = s[0]
`)
		assert.NotEmpty(t, diags)
	})

	// StarExpr self-assignment: *p = *p
	t.Run("star self-assignment", func(t *testing.T) {
		diags := runAssign(t, `
var x int
p := &x
*p = *p
`)
		assert.NotEmpty(t, diags)
	})

	// Map index should NOT be flagged (map index excluded)
	t.Run("map index not flagged", func(t *testing.T) {
		diags := runAssign(t, `
var m = map[string]int{"a": 1}
m["a"] = m["a"]
`)
		assert.Empty(t, diags)
	})

	// Unary self-assignment: (-x) = (-x) — side effects check short-circuits, no diag expected
	t.Run("different unary", func(t *testing.T) {
		diags := runAssign(t, `
var a, b int
a = b
_ = a
`)
		assert.Empty(t, diags)
	})

	// Nested selector self-assignment: a.b.c = a.b.c
	t.Run("nested selector self-assignment", func(t *testing.T) {
		diags := runAssign(t, `
type Inner struct{ c int }
type Outer struct{ b Inner }
var a Outer
a.b.c = a.b.c
`)
		assert.NotEmpty(t, diags)
	})
}
