package printf

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

func TestPrintf(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{
			name: "Printf missing argument",
			src: `
import "fmt"
fmt.Printf("%s")
`,
			wantDiag: true,
		},
		{
			name: "Printf correct arguments",
			src: `
import "fmt"
fmt.Printf("%s %d", "hello", 42)
`,
			wantDiag: false,
		},
		{
			name: "Printf args without directives",
			src: `
import "fmt"
fmt.Printf("hello", 42)
`,
			wantDiag: true,
		},
		{
			name: "Printf too many arguments",
			src: `
import "fmt"
fmt.Printf("%s", "a", "b")
`,
			wantDiag: true,
		},
		{
			name: "Println with format directive",
			src: `
import "fmt"
fmt.Println("%s", "hello")
`,
			wantDiag: true,
		},
		{
			name: "Println without format directive",
			src: `
import "fmt"
fmt.Println("hello", "world")
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

func runPrintf(t *testing.T, src string) []protocol.Diagnostic {
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

func TestPrintfExtra(t *testing.T) {
	// %% escape: no verb, no args needed
	t.Run("Printf percent-percent no arg", func(t *testing.T) {
		diags := runPrintf(t, `
import "fmt"
fmt.Printf("100%%")
`)
		assert.Empty(t, diags)
	})

	// Printf with non-constant format string and no extra args — triggers
	t.Run("Printf non-constant format no args", func(t *testing.T) {
		diags := runPrintf(t, `
import "fmt"
var msg = "hello %s"
fmt.Printf(msg)
`)
		assert.NotEmpty(t, diags)
	})

	// Printf with non-constant format string and extra args — no diag (can't verify)
	t.Run("Printf non-constant format with args", func(t *testing.T) {
		diags := runPrintf(t, `
import "fmt"
var msg = "hello"
fmt.Printf(msg, "world")
`)
		assert.Empty(t, diags)
	})

	// Sprintf correct (no writer arg offset)
	t.Run("Sprintf correct", func(t *testing.T) {
		diags := runPrintf(t, `
import "fmt"
_ = fmt.Sprintf("%d", 1)
`)
		assert.Empty(t, diags)
	})

	// Println with trailing newline — triggers
	t.Run("Println trailing newline", func(t *testing.T) {
		diags := runPrintf(t, `
import "fmt"
fmt.Println("hello\n")
`)
		assert.NotEmpty(t, diags)
	})

	// Println with URL-like %2F — should NOT trigger (hex escape)
	t.Run("Println URL percent encoding no diag", func(t *testing.T) {
		diags := runPrintf(t, `
import "fmt"
fmt.Println("path%2Fto")
`)
		assert.Empty(t, diags)
	})

	// Printf with ellipsis — no diag (variadic forwarding)
	t.Run("Printf ellipsis forwarding", func(t *testing.T) {
		diags := runPrintf(t, `
import "fmt"
func wrap(args ...interface{}) {
	fmt.Printf("%v %v", args...)
}
`)
		assert.Empty(t, diags)
	})

	// log.Printf correct
	t.Run("log.Printf correct", func(t *testing.T) {
		diags := runPrintf(t, `
import "log"
log.Printf("%s", "msg")
`)
		assert.Empty(t, diags)
	})

	// log.Printf missing arg
	t.Run("log.Printf missing arg", func(t *testing.T) {
		diags := runPrintf(t, `
import "log"
log.Printf("%s")
`)
		assert.NotEmpty(t, diags)
	})
}
