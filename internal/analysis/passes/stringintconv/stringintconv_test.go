package stringintconv

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

func TestStringIntconv(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{
			name: "int to string conversion",
			src: `
var x int
_ = string(x)
`,
			wantDiag: true,
		},
		{
			name: "int64 to string conversion",
			src: `
var x int64
_ = string(x)
`,
			wantDiag: true,
		},
		{
			name: "byte to string - no diagnostic",
			src: `
var x byte
_ = string(x)
`,
			wantDiag: false,
		},
		{
			name: "rune to string - no diagnostic",
			src: `
var x rune
_ = string(x)
`,
			wantDiag: false,
		},
		{
			name: "string to string - no diagnostic",
			src: `
var x string
_ = string(x)
`,
			wantDiag: false,
		},
		{
			name: "uint16 to string conversion",
			src: `
var x uint16
_ = string(x)
`,
			wantDiag: true,
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
