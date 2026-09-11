//go:build !test_no_pkgdata

package server

import (
	gotypes "go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/xgo/types"
)

func TestSpxResourceReturnTypes(t *testing.T) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, "main.spx", `
func resource() (string, string) {
	return wrap("resource", "nested"), "ordinary"
}

func plain() string {
	return "plain"
}

func unresolved() string {
	return "unresolved"
}

func external() string

func excess() string {
	return "resourceWithExtraValue", "extraValue"
}

func nested() string {
	return func() string {
		return "nestedFunction"
	}()
}
`, parser.ParseComments)
	require.NoError(t, err)

	typeInfo := &types.Info{
		Info: typesutil.Info{
			Defs:  make(map[*ast.Ident]gotypes.Object),
			Types: make(map[ast.Expr]gotypes.TypeAndValue),
		},
	}
	resourceType := GetSpxBackdropNameType()
	stringType := gotypes.Typ[gotypes.String]
	for _, decl := range astFile.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		var results *gotypes.Tuple
		switch funcDecl.Name.Name {
		case "unresolved":
			// Incomplete type information can omit a function's definition.
			continue
		case "resource":
			results = gotypes.NewTuple(
				gotypes.NewVar(token.NoPos, nil, "", resourceType),
				gotypes.NewVar(token.NoPos, nil, "", stringType),
			)
		case "nested", "excess":
			results = gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "", resourceType))
		default:
			results = gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "", stringType))
		}
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, results, false)
		typeInfo.Defs[funcDecl.Name] = gotypes.NewFunc(token.NoPos, nil, funcDecl.Name.Name, sig)
	}
	ast.Inspect(astFile, func(node ast.Node) bool {
		funcLit, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		results := gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "", stringType))
		typeInfo.Types[funcLit] = gotypes.TypeAndValue{
			Type: gotypes.NewSignatureType(nil, nil, nil, nil, results, false),
		}
		return true
	})

	got := spxResourceReturnTypes(&ast.Package{
		Name:  "main",
		Files: map[string]*ast.File{"main.spx": astFile},
	}, typeInfo)
	gotByValue := make(map[string]gotypes.Type)
	for literal, typ := range got {
		gotByValue[literal.Value] = typ
	}

	assert.Equal(t, resourceType, gotByValue[`"resource"`])
	assert.Equal(t, resourceType, gotByValue[`"nested"`])
	assert.Equal(t, resourceType, gotByValue[`"resourceWithExtraValue"`])
	assert.NotContains(t, gotByValue, `"extraValue"`)
	assert.NotContains(t, gotByValue, `"ordinary"`)
	assert.NotContains(t, gotByValue, `"plain"`)
	assert.NotContains(t, gotByValue, `"unresolved"`)
	assert.NotContains(t, gotByValue, `"nestedFunction"`)
}

func TestServerInspectSpxResourceRefsForCallExpr(t *testing.T) {
	t.Run("UnknownKwarg", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx": []byte(`type Options struct { Target SpriteName }
func configure(opts Options?) {}
configure target = "OtherSprite", unknown = 9
`),
			"OtherSprite.spx":                       nil,
			"assets/index.json":                     []byte(`{}`),
			"assets/sprites/OtherSprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		result, err := s.compile()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.spxResourceRefs, 1)
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/OtherSprite"), result.spxResourceRefs[0].ID.URI())
	})
}

func TestCompileResultIsInSpxEventHandler(t *testing.T) {
	for _, tt := range []struct {
		name     string
		content  string
		position Position
		want     bool
	}{
		{
			name:     "OrdinaryCallback",
			content:  "func invoke(fn func()) { fn() }\ninvoke => {\n    println 1\n}\n",
			position: Position{Line: 2, Character: 8},
		},
		{
			name:     "NestedOrdinaryCallback",
			content:  "func invoke(fn func()) { fn() }\nonStart => {\n    invoke => {\n        println 1\n    }\n}\n",
			position: Position{Line: 3, Character: 12},
			want:     true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{"main.spx": []byte(tt.content), "assets/index.json": []byte(`{}`)}
			s := newSpxTestServer(t, files)
			result, err := s.compile()
			require.NoError(t, err)
			require.Empty(t, result.diagnostics["file:///main.spx"])
			astFile, err := result.proj.ASTFile("main.spx")
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.isInSpxEventHandler(PosAt(result.proj, astFile, tt.position)))
		})
	}
}
