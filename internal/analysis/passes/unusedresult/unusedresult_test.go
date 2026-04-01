package unusedresult

import (
	"go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	internalpkg "github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnusedresult(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{
			name: "unused fmt.Sprintf result",
			src: `
import "fmt"
fmt.Sprintf("hello")
`,
			wantDiag: true,
		},
		{
			name: "used fmt.Sprintf result",
			src: `
import "fmt"
s := fmt.Sprintf("hello")
_ = s
`,
			wantDiag: false,
		},
		{
			name: "unused errors.New result",
			src: `
import "errors"
errors.New("something")
`,
			wantDiag: true,
		},
		{
			name: "used errors.New result",
			src: `
import "errors"
err := errors.New("something")
_ = err
`,
			wantDiag: false,
		},
		{
			name: "unused strings.Replace result",
			src: `
import "strings"
strings.Replace("a", "b", "c", 1)
`,
			wantDiag: true,
		},
		{
			name: "fmt.Println result not flagged",
			src: `
import "fmt"
fmt.Println("hello")
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
					Types:      make(map[ast.Expr]types.TypeAndValue),
					Defs:       make(map[*ast.Ident]types.Object),
					Uses:       make(map[*ast.Ident]types.Object),
					Selections: make(map[*ast.SelectorExpr]*types.Selection),
				},
			}

			checker := typesutil.NewChecker(
				&types.Config{
					Error:    func(err error) {},
					Importer: internalpkg.Importer,
				},
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

func runUnusedresult(t *testing.T, src string) []protocol.Diagnostic {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.xgo", src, parser.ParseComments)
	require.NoError(t, err)

	info := &xgotypes.Info{
		Info: typesutil.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		},
	}
	checker := typesutil.NewChecker(
		&types.Config{Error: func(error) {}, Importer: internalpkg.Importer},
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

func TestUnusedresultExtra(t *testing.T) {
	// Error() method result not used (mustUseStringMethods)
	t.Run("unused Error() result", func(t *testing.T) {
		diags := runUnusedresult(t, `
import "errors"
var err = errors.New("oops")
err.Error()
`)
		assert.NotEmpty(t, diags)
	})

	// String() method result not used
	t.Run("unused String() result", func(t *testing.T) {
		diags := runUnusedresult(t, `
import "fmt"
var s fmt.Stringer
s.String()
`)
		assert.NotEmpty(t, diags)
	})

	// Used Error() result — no diagnostic
	t.Run("used Error() result", func(t *testing.T) {
		diags := runUnusedresult(t, `
import "errors"
var err = errors.New("oops")
msg := err.Error()
_ = msg
`)
		assert.Empty(t, diags)
	})

	// fmt.Sprint unused
	t.Run("unused fmt.Sprint", func(t *testing.T) {
		diags := runUnusedresult(t, `
import "fmt"
fmt.Sprint("x")
`)
		assert.NotEmpty(t, diags)
	})

	// fmt.Sprintln unused
	t.Run("unused fmt.Sprintln", func(t *testing.T) {
		diags := runUnusedresult(t, `
import "fmt"
fmt.Sprintln("x")
`)
		assert.NotEmpty(t, diags)
	})
}
