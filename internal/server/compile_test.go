package server

import (
	gotypes "go/types"
	"strings"
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
		case "resource":
			results = gotypes.NewTuple(
				gotypes.NewVar(token.NoPos, nil, "", resourceType),
				gotypes.NewVar(token.NoPos, nil, "", stringType),
			)
		case "nested":
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
	assert.NotContains(t, gotByValue, `"ordinary"`)
	assert.NotContains(t, gotByValue, `"plain"`)
	assert.NotContains(t, gotByValue, `"nestedFunction"`)
}

func BenchmarkServerDocumentLinkWithLargeList(b *testing.B) {
	files := largeListProjectFiles(20_001)
	server := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
	params := &DocumentLinkParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := server.textDocumentDocumentLink(params)
		require.NoError(b, err)
	}
}

func BenchmarkServerGetInputSlotsWithLargeList(b *testing.B) {
	const slotCount = 20_001
	files := largeListProjectFiles(slotCount)
	server := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
	params := []XGoGetInputSlotsParams{{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
	}}
	slots, err := server.spxGetInputSlots(params)
	require.NoError(b, err)
	require.Len(b, slots, slotCount)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := server.spxGetInputSlots(params)
		require.NoError(b, err)
	}
}

func BenchmarkServerGetInputSlotsWithMixedLargeList(b *testing.B) {
	const expressionCount = 8_000
	files := mixedListProjectFiles(expressionCount)
	server := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
	params := []XGoGetInputSlotsParams{{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
	}}
	slots, err := server.spxGetInputSlots(params)
	require.NoError(b, err)
	require.Len(b, slots, expressionCount*3)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := server.spxGetInputSlots(params)
		require.NoError(b, err)
	}
}

func largeListProjectFiles(elementCount int) map[string][]byte {
	mainSpx := `var large List = NewList("value"` + strings.Repeat(`, "value"`, elementCount-1) + ")\n"
	return map[string][]byte{
		"main.spx":          []byte(mainSpx),
		"assets/index.json": []byte(`{}`),
	}
}

func mixedListProjectFiles(expressionCount int) map[string][]byte {
	mainSpx := `var large List = NewList(` + strings.Repeat(`1 + 2, `, expressionCount) +
		`"value"` + strings.Repeat(`, "value"`, expressionCount-1) + ")\n"
	return map[string][]byte{
		"main.spx":          []byte(mainSpx),
		"assets/index.json": []byte(`{}`),
	}
}
