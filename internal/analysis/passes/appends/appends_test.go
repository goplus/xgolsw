package appends

import (
	gotypes "go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/xgo/types"
)

func TestAppends(t *testing.T) {
	for _, tt := range []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{
			name: "AppendWithoutValues",
			src: `
var s []int
_ = append(s)
`,
			wantDiag: true,
		},
		{
			name: "AppendWithValues",
			src: `
var s []int
_ = append(s, 1)
`,
			wantDiag: false,
		},
		{
			name: "ShadowedAppend",
			src: `
func append(s []int) []int {
	return s
}
var s []int
_ = append(s)
`,
			wantDiag: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Create file set and parse source
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.xgo", tt.src, parser.ParseComments)
			require.NoError(t, err)

			info := &types.Info{
				Info: typesutil.Info{
					Types:     make(map[ast.Expr]gotypes.TypeAndValue),
					Defs:      make(map[*ast.Ident]gotypes.Object),
					Uses:      make(map[*ast.Ident]gotypes.Object),
					Overloads: make(map[*ast.Ident]gotypes.Object),
				},
			}

			checker := typesutil.NewChecker(
				&gotypes.Config{},
				&typesutil.Config{
					Fset:  fset,
					Types: gotypes.NewPackage("test", "test"),
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

			if tt.wantDiag {
				assert.NotEmpty(t, diagnostics)
			} else {
				assert.Empty(t, diagnostics)
			}
		})
	}
}

func TestIsBuiltinAppend(t *testing.T) {
	builtinPkg := gotypes.NewPackage("", "")
	otherPkg := gotypes.NewPackage("example.com/other", "other")

	for _, tt := range []struct {
		name string
		obj  gotypes.Object
		want bool
	}{
		{
			name: "Nil",
		},
		{
			name: "GoBuiltin",
			obj:  gotypes.Universe.Lookup("append"),
			want: true,
		},
		{
			name: "XGoTemplateFunc",
			obj:  newTestTemplateFunc(builtinPkg, "append"),
			want: true,
		},
		{
			name: "XGoOverloadFunc",
			obj: gogen.NewOverloadFunc(
				token.NoPos,
				builtinPkg,
				"append",
				newTestTemplateFunc(builtinPkg, "append"),
			),
			want: true,
		},
		{
			name: "ShadowedTemplateFunc",
			obj:  newTestTemplateFunc(otherPkg, "append"),
		},
		{
			name: "PlainFunc",
			obj: gotypes.NewFunc(
				token.NoPos,
				builtinPkg,
				"append",
				gotypes.NewSignatureType(nil, nil, nil, nil, nil, false),
			),
		},
		{
			name: "InvalidFuncType",
			obj:  gotypes.NewFunc(token.NoPos, builtinPkg, "append", nil),
		},
		{
			name: "WrongTemplateOverload",
			obj: gogen.NewOverloadFunc(
				token.NoPos,
				builtinPkg,
				"append",
				newTestTemplateFunc(builtinPkg, "len"),
			),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isBuiltinAppend(tt.obj))
		})
	}
}

func newTestTemplateFunc(pkg *gotypes.Package, name string) *gogen.TemplateFunc {
	return gogen.NewTemplateFunc(token.NoPos, pkg, name, gogen.NewTemplateSignature(nil, nil, nil, nil, false))
}
