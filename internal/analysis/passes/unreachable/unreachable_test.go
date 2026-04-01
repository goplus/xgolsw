package unreachable

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

func TestUnreachable(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{
			name: "code after return",
			src: `
func f() {
	return
	println("dead")
}
`,
			wantDiag: true,
		},
		{
			name: "code after break in loop",
			src: `
func f() {
	for {
		break
		println("dead")
	}
}
`,
			wantDiag: true,
		},
		{
			name: "reachable code",
			src: `
func f() {
	println("alive")
	return
}
`,
			wantDiag: false,
		},
		{
			name: "code after return in case",
			src: `
func f(x int) {
	switch x {
	case 1:
		return
		println("dead")
	}
}
`,
			wantDiag: true,
		},
		{
			name: "code after continue in loop",
			src: `
func f() {
	for i := 0; i < 10; i++ {
		continue
		println("dead")
	}
}
`,
			wantDiag: true,
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
