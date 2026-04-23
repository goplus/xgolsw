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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func runUnreachable(t *testing.T, src string) []protocol.Diagnostic {
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

func TestUnreachableExtra(t *testing.T) {
	// if-else both arms return → code after if is unreachable
	t.Run("code after if-else both return", func(t *testing.T) {
		diags := runUnreachable(t, `
func f(x int) int {
	if x > 0 {
		return 1
	} else {
		return -1
	}
	return 0
}
`)
		assert.NotEmpty(t, diags)
	})

	// goto makes labeled stmt reachable
	t.Run("goto makes label reachable", func(t *testing.T) {
		diags := runUnreachable(t, `
func f() {
	goto end
end:
	println("reachable")
}
`)
		assert.Empty(t, diags)
	})

	// range loop body — code after range is always reachable
	t.Run("reachable after range", func(t *testing.T) {
		diags := runUnreachable(t, `
func f() {
	var s []int
	for range s {
		return
	}
	println("reachable")
}
`)
		assert.Empty(t, diags)
	})

	// for loop with condition — code after is reachable (condition can be false)
	t.Run("reachable after conditional for", func(t *testing.T) {
		diags := runUnreachable(t, `
func f() {
	for i := 0; i < 3; i++ {
		println(i)
	}
	println("done")
}
`)
		assert.Empty(t, diags)
	})

	// switch with default — code after is unreachable when all cases return
	t.Run("code after exhaustive switch", func(t *testing.T) {
		diags := runUnreachable(t, `
func f(x int) {
	switch x {
	case 1:
		return
	default:
		return
	}
	println("dead")
}
`)
		assert.NotEmpty(t, diags)
	})

	// labeled break keeps switch reachable
	t.Run("labeled break keeps switch reachable", func(t *testing.T) {
		diags := runUnreachable(t, `
func f(x int) {
outer:
	switch x {
	case 1:
		break outer
	default:
		return
	}
	println("reachable")
}
`)
		assert.Empty(t, diags)
	})

	// func literal inside function (covers FuncLit branch in run)
	t.Run("unreachable in func literal", func(t *testing.T) {
		diags := runUnreachable(t, `
var f = func() {
	return
	println("dead")
}
`)
		assert.NotEmpty(t, diags)
	})

	// type switch with default — code after unreachable
	t.Run("code after exhaustive type switch", func(t *testing.T) {
		diags := runUnreachable(t, `
func f(v interface{}) {
	switch v.(type) {
	case int:
		return
	default:
		return
	}
	println("dead")
}
`)
		assert.NotEmpty(t, diags)
	})
}
