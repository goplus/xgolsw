package propertyname

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

type propertynameCallbacks struct {
	isPropertyNameType      func(types.Type) bool
	getPropertyNamesForCall func(*ast.CallExpr) []string
}

func TestPropertyname(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		callbacks propertynameCallbacks
		wantDiag  bool
	}{
		{
			name: "unknown property literal",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("unknown")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ types.Type) bool {
					named, ok := types.Unalias(typ).(*types.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: true,
		},
		{
			name: "known property literal",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("x")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ types.Type) bool {
					named, ok := types.Unalias(typ).(*types.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: false,
		},
		{
			name: "const identifier argument",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

const prop = "unknown"

func run() {
	showVar(prop)
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ types.Type) bool {
					named, ok := types.Unalias(typ).(*types.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: true,
		},
		{
			name: "non constant identifier argument",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

var prop PropertyName = "unknown"

func run() {
	showVar(prop)
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ types.Type) bool {
					named, ok := types.Unalias(typ).(*types.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: false,
		},
		{
			name: "nil IsPropertyNameType callback",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("unknown")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: nil,
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: false,
		},
		{
			name: "nil GetPropertyNamesForCall callback",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("unknown")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ types.Type) bool {
					named, ok := types.Unalias(typ).(*types.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: nil,
			},
			wantDiag: false,
		},
		{
			name: "nil return from GetPropertyNamesForCall skips validation",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("unknown")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ types.Type) bool {
					named, ok := types.Unalias(typ).(*types.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return nil // target type unknown: skip validation
				},
			},
			wantDiag: false,
		},
		{
			name: "empty return from GetPropertyNamesForCall reports all properties unknown",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("x")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ types.Type) bool {
					named, ok := types.Unalias(typ).(*types.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{} // target type known but has no properties
				},
			},
			wantDiag: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := runPropertynameAnalyzer(t, tt.src, tt.callbacks)
			assert.Equal(t, tt.wantDiag, len(diagnostics) > 0)
		})
	}
}

func runPropertynameAnalyzer(t *testing.T, src string, callbacks propertynameCallbacks) []protocol.Diagnostic {
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
		&types.Config{},
		&typesutil.Config{
			Fset:  fset,
			Types: types.NewPackage("test", "test"),
		},
		nil,
		&info.Info,
	)
	require.NoError(t, checker.Files(nil, []*ast.File{f}))

	var diagnostics []protocol.Diagnostic
	pass := &protocol.Pass{
		Fset:                    fset,
		Files:                   []*ast.File{f},
		TypesInfo:               info,
		IsPropertyNameType:      callbacks.isPropertyNameType,
		GetPropertyNamesForCall: callbacks.getPropertyNamesForCall,
		Report: func(d protocol.Diagnostic) {
			diagnostics = append(diagnostics, d)
		},
		ResultOf: map[*protocol.Analyzer]any{
			inspect.Analyzer: inspector.New([]*ast.File{f}),
		},
	}

	_, err = Analyzer.Run(pass)
	require.NoError(t, err)

	return diagnostics
}
