package propertyname

import (
	gotypes "go/types"
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
	"github.com/goplus/xgolsw/xgo/types"
)

type propertynameCallbacks struct {
	isPropertyNameType      func(gotypes.Type) bool
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
			name: "UnknownPropertyLiteral",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("unknown")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ gotypes.Type) bool {
					named, ok := gotypes.Unalias(typ).(*gotypes.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: true,
		},
		{
			name: "KnownPropertyLiteral",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("x")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ gotypes.Type) bool {
					named, ok := gotypes.Unalias(typ).(*gotypes.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: false,
		},
		{
			name: "ConstIdentifierArgument",
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
				isPropertyNameType: func(typ gotypes.Type) bool {
					named, ok := gotypes.Unalias(typ).(*gotypes.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: true,
		},
		{
			name: "NonConstantIdentifierArgument",
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
				isPropertyNameType: func(typ gotypes.Type) bool {
					named, ok := gotypes.Unalias(typ).(*gotypes.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return []string{"x", "y"}
				},
			},
			wantDiag: false,
		},
		{
			name: "NilIsPropertyNameTypeCallback",
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
			name: "NilGetPropertyNamesForCallCallback",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("unknown")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ gotypes.Type) bool {
					named, ok := gotypes.Unalias(typ).(*gotypes.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: nil,
			},
			wantDiag: false,
		},
		{
			name: "NilReturnFromGetPropertyNamesForCallSkipsValidation",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("unknown")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ gotypes.Type) bool {
					named, ok := gotypes.Unalias(typ).(*gotypes.Named)
					return ok && named.Obj().Name() == "PropertyName"
				},
				getPropertyNamesForCall: func(_ *ast.CallExpr) []string {
					return nil // target type unknown: skip validation
				},
			},
			wantDiag: false,
		},
		{
			name: "EmptyReturnFromGetPropertyNamesForCallReportsAllPropertiesUnknown",
			src: `
package test

type PropertyName string

func showVar(name PropertyName) {}

func run() {
	showVar("x")
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType: func(typ gotypes.Type) bool {
					named, ok := gotypes.Unalias(typ).(*gotypes.Named)
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
			if tt.wantDiag {
				assert.NotEmpty(t, diagnostics)
			} else {
				assert.Empty(t, diagnostics)
			}
		})
	}
}

func runPropertynameAnalyzer(t *testing.T, src string, callbacks propertynameCallbacks) []protocol.Diagnostic {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.xgo", src, parser.ParseComments)
	require.NoError(t, err)

	info := &types.Info{
		Info: typesutil.Info{
			Types: make(map[ast.Expr]gotypes.TypeAndValue),
			Defs:  make(map[*ast.Ident]gotypes.Object),
			Uses:  make(map[*ast.Ident]gotypes.Object),
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
