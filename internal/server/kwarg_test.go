package server

import (
	gotypes "go/types"
	"slices"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvedCallExprArgs(t *testing.T) {
	t.Run("UnresolvedOverloadKwargs", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Worker struct{}
type Options struct {
	Count int
	Name string
}
func (w *Worker) handleInt(prefix int, opts Options?, values ...int) {}
func (w *Worker) handleString(prefix string, opts Options?, values ...int) {}
func (Worker).handle = (
	(Worker).handleInt
	(Worker).handleString
)
var worker Worker
worker.handle missing, unknown, count = unknown, name = "task"
`),
		})
		proj := s.workspaceRootFS
		typeInfo, err := proj.TypeInfo()
		require.NotNil(t, typeInfo)
		require.ErrorContains(t, err, "undefined: missing")
		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)
		var call *ast.CallExpr
		ast.Inspect(astFile, func(node ast.Node) bool {
			if expr, ok := node.(*ast.CallExpr); ok {
				call = expr
				return false
			}
			return true
		})
		require.NotNil(t, call)
		assert.Empty(t, slices.Collect(xgoutil.ResolvedCallExprArgs(typeInfo, call)))

		args := slices.Collect(resolvedCallExprArgs(proj, typeInfo, call))
		require.Len(t, args, 8)
		for overloadIndex, prefixType := range []gotypes.Type{gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]} {
			for i, tt := range []struct {
				paramIndex int
				paramName  string
				kind       xgoutil.ResolvedCallExprArgKind
				valueType  gotypes.Type
				fieldName  string
			}{
				{0, "prefix", xgoutil.ResolvedCallExprArgPositional, prefixType, ""},
				{2, "values", xgoutil.ResolvedCallExprArgPositional, gotypes.Typ[gotypes.Int], ""},
				{1, "opts", xgoutil.ResolvedCallExprArgKeyword, gotypes.Typ[gotypes.Int], "Count"},
				{1, "opts", xgoutil.ResolvedCallExprArgKeyword, gotypes.Typ[gotypes.String], "Name"},
			} {
				arg := args[overloadIndex*4+i]
				assert.Equal(t, i, arg.ArgIndex)
				assert.Equal(t, tt.paramIndex, arg.ParamIndex)
				require.NotNil(t, arg.Param)
				assert.Equal(t, tt.paramName, arg.Param.Name())
				assert.Equal(t, tt.kind, arg.Kind)
				assert.Equal(t, tt.valueType, arg.ExpectedType)
				if i < len(call.Args) {
					assert.Same(t, call.Args[i], arg.Arg)
					assert.Nil(t, arg.Kwarg)
					assert.Nil(t, arg.KwargTarget)
					continue
				}
				kwarg := call.Kwargs[i-len(call.Args)]
				assert.Same(t, kwarg, arg.Kwarg)
				assert.Same(t, kwarg.Value, arg.Arg)
				require.NotNil(t, arg.KwargTarget)
				assert.Equal(t, kwarg.Name.Name, arg.KwargTarget.Name)
				require.NotNil(t, arg.KwargTarget.Field)
				assert.Equal(t, tt.fieldName, arg.KwargTarget.Field.Name())
			}
		}
	})
}

func TestKwargRenameText(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")

	t.Run("StructField", func(t *testing.T) {
		field := gotypes.NewField(0, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		assert.Equal(t, "count", kwargRenameText(field, "Count"))
	})

	t.Run("StructUnicodeField", func(t *testing.T) {
		field := gotypes.NewField(0, pkg, "\u00c4ge", gotypes.Typ[gotypes.Int], false)
		assert.Equal(t, "\u00e4ge", kwargRenameText(field, "\u00c4ge"))
	})

	t.Run("InterfaceMethod", func(t *testing.T) {
		method := gotypes.NewFunc(0, pkg, "MaxTokens", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		assert.Equal(t, "maxTokens", kwargRenameText(method, "MaxTokens"))
	})

	t.Run("InterfaceUnicodeMethod", func(t *testing.T) {
		method := gotypes.NewFunc(0, pkg, "\u00c4ge", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		assert.Equal(t, "\u00c4ge", kwargRenameText(method, "\u00c4ge"))
	})
}

func TestKwargDefinitionRenameText(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")

	t.Run("ExportedStructField", func(t *testing.T) {
		field := gotypes.NewField(0, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		assert.Equal(t, "Total", kwargDefinitionRenameText(field, "total"))
	})

	t.Run("LocalStructField", func(t *testing.T) {
		field := gotypes.NewField(0, pkg, "count", gotypes.Typ[gotypes.Int], false)
		assert.Equal(t, "total", kwargDefinitionRenameText(field, "total"))
	})

	t.Run("ExportedUnicodeStructField", func(t *testing.T) {
		field := gotypes.NewField(0, pkg, "\u00c4ge", gotypes.Typ[gotypes.Int], false)
		assert.Equal(t, "\u00c4ge", kwargDefinitionRenameText(field, "\u00e4ge"))
	})

	t.Run("InterfaceMethod", func(t *testing.T) {
		method := gotypes.NewFunc(0, pkg, "MaxTokens", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		assert.Equal(t, "Limit", kwargDefinitionRenameText(method, "limit"))
	})

	t.Run("InterfaceUnicodeMethod", func(t *testing.T) {
		method := gotypes.NewFunc(0, pkg, "\u00c4ge", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		assert.Equal(t, "\u00c4ge", kwargDefinitionRenameText(method, "\u00c4ge"))
	})
}
