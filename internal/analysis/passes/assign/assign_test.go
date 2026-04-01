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
			if err != nil {
				t.Fatal(err)
			}

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
			if err != nil {
				t.Fatal(err)
			}

			for _, d := range diagnostics {
				t.Logf("got diagnostic: %v", d)
			}
			if hasDiag := len(diagnostics) > 0; hasDiag != tt.wantDiag {
				t.Errorf("got diagnostic = %v, want %v", hasDiag, tt.wantDiag)
			}
		})
	}
}
