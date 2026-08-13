package propertyname

import (
	gotypes "go/types"
	"iter"
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
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

type propertynameCallbacks struct {
	isPropertyNameType      func(gotypes.Type) bool
	getPropertyNamesForCall func(*ast.CallExpr) map[string]struct{}
	resolvedCallExprArgs    func(*ast.CallExpr) iter.Seq[xgoutil.ResolvedCallExprArg]
}

func propertyNameSet(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

func isTestPropertyNameType(typ gotypes.Type) bool {
	named, ok := gotypes.Unalias(typ).(*gotypes.Named)
	return ok && named.Obj().Name() == "PropertyName"
}

func testPropertyNames(_ *ast.CallExpr) map[string]struct{} {
	return propertyNameSet("x", "y")
}

func TestPropertyname(t *testing.T) {
	for _, tt := range []struct {
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
				isPropertyNameType:      isTestPropertyNameType,
				getPropertyNamesForCall: testPropertyNames,
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
				isPropertyNameType:      isTestPropertyNameType,
				getPropertyNamesForCall: testPropertyNames,
			},
			wantDiag: false,
		},
		{
			name: "UnknownPropertyFuncDecorator",
			src: `
package test

type PropertyName string

func validate(name PropertyName, fn func()) {}

@validate("unknown")
func run() {}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType:      isTestPropertyNameType,
				getPropertyNamesForCall: testPropertyNames,
			},
			wantDiag: true,
		},
		{
			name: "KnownPropertyFuncDecorator",
			src: `
package test

type PropertyName string

func validate(name PropertyName, fn func()) {}

@validate("x")
func run() {}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType:      isTestPropertyNameType,
				getPropertyNamesForCall: testPropertyNames,
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
				isPropertyNameType:      isTestPropertyNameType,
				getPropertyNamesForCall: testPropertyNames,
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
				isPropertyNameType:      isTestPropertyNameType,
				getPropertyNamesForCall: testPropertyNames,
			},
			wantDiag: false,
		},
		{
			name: "UnknownPropertyKwargLiteral",
			src: `
package test

type PropertyName string

type Options struct {
	Name PropertyName
}

func showVar(opts Options?) {}

func run() {
	showVar name = "unknown"
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType:      isTestPropertyNameType,
				getPropertyNamesForCall: testPropertyNames,
			},
			wantDiag: true,
		},
		{
			name: "KnownPropertyKwargLiteral",
			src: `
package test

type PropertyName string

type Options struct {
	Name PropertyName
}

func showVar(opts Options?) {}

func run() {
	showVar name = "x"
}
`,
			callbacks: propertynameCallbacks{
				isPropertyNameType:      isTestPropertyNameType,
				getPropertyNamesForCall: testPropertyNames,
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
				isPropertyNameType:      nil,
				getPropertyNamesForCall: testPropertyNames,
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
				isPropertyNameType:      isTestPropertyNameType,
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
				isPropertyNameType: isTestPropertyNameType,
				getPropertyNamesForCall: func(_ *ast.CallExpr) map[string]struct{} {
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
				isPropertyNameType: isTestPropertyNameType,
				getPropertyNamesForCall: func(_ *ast.CallExpr) map[string]struct{} {
					return propertyNameSet() // target type known but has no properties
				},
			},
			wantDiag: true,
		},
	} {
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

func TestPropertynameSkipsPropertyLookupWithoutValidatableArguments(t *testing.T) {
	lookups := 0
	resolutions := 0
	diagnostics := runPropertynameAnalyzer(t, `
package test

type PropertyName string

func log(value int) {}
func showVar(name PropertyName) {}

var property PropertyName

func run() {
	log(1)
	showVar(property)
}
`, propertynameCallbacks{
		isPropertyNameType: isTestPropertyNameType,
		getPropertyNamesForCall: func(_ *ast.CallExpr) map[string]struct{} {
			lookups++
			return propertyNameSet("x", "y")
		},
		resolvedCallExprArgs: func(_ *ast.CallExpr) iter.Seq[xgoutil.ResolvedCallExprArg] {
			resolutions++
			return func(_ func(xgoutil.ResolvedCallExprArg) bool) {}
		},
	})

	assert.Empty(t, diagnostics)
	assert.Zero(t, lookups)
	assert.Zero(t, resolutions)
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
		ResolvedCallExprArgs:    callbacks.resolvedCallExprArgs,
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
