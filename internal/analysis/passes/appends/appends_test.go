package appends

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
)

func TestAppends(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{
			name: "append without values",
			src: `
var s []int
_ = append(s)
`,
			wantDiag: true,
		},
		{
			name: "append with values",
			src: `
var s []int
_ = append(s, 1)
`,
			wantDiag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create file set and parse source
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
				&types.Config{},
				&typesutil.Config{
					Fset:  fset,
					Types: types.NewPackage("test", "test"),
				},
				nil,
				&info.Info,
			)

			_ = checker.Files(nil, []*ast.File{f}) // xgo snippets without package declaration may fail type checking

			var diagnostics []protocol.Diagnostic
			// Create pass
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

			// Run analyzer
			_, err = Analyzer.Run(pass)
			require.NoError(t, err)

			assert.Equal(t, tt.wantDiag, len(diagnostics) > 0)
		})
	}
}
