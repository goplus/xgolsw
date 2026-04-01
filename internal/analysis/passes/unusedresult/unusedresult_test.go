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
			if err != nil {
				t.Fatal(err)
			}

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
