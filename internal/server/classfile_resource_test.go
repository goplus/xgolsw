package server

import (
	"go/types"
	"testing"

	xgoast "github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSpxClassfileResourceSchema(t *testing.T) {
	schema, err := getSpxClassfileResourceSchema()
	require.NoError(t, err)

	t.Run("SpriteInterfaceMethod", func(t *testing.T) {
		var fn *types.Func
		iface, ok := types.Unalias(GetSpxSpriteType()).Underlying().(*types.Interface)
		require.True(t, ok)
		for i := range iface.NumExplicitMethods() {
			method := iface.ExplicitMethod(i)
			if method.Name() == "SetCostume__0" {
				fn = method
				break
			}
		}
		require.NotNil(t, fn)

		bindings := schema.apiScopeBindings[fn.FullName()]
		require.Len(t, bindings, 1)
		assert.Equal(t, 0, bindings[0].TargetParam)
		assert.True(t, bindings[0].SourceReceiver)
	})

	t.Run("SpriteImplMethod", func(t *testing.T) {
		var fn *types.Func
		for i := range GetSpxSpriteImplType().NumMethods() {
			method := GetSpxSpriteImplType().Method(i)
			if method.Name() == "TurnTo__b" {
				fn = method
				break
			}
		}
		require.NotNil(t, fn)

		bindings := schema.apiScopeBindings[fn.FullName()]
		require.Len(t, bindings, 1)
		assert.Equal(t, 2, bindings[0].TargetParam)
		assert.True(t, bindings[0].SourceReceiver)
	})
}

func TestResolveSpxSpriteResourceForNode(t *testing.T) {
	m := map[string][]byte{
		"main.spx": []byte(`
onStart => {
	MySprite.setCostume "costume1"
}
`),
		"MySprite.spx": []byte(`
onStart => {
	setCostume "costume1"
}
`),
		"assets/index.json":                  []byte(`{}`),
		"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume1"}]}`),
	}
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

	t.Run("ExplicitReceiver", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)

		pos := PosAt(result.proj, astFile, Position{Line: 2, Character: 23})
		require.True(t, pos.IsValid())

		var lit *xgoast.BasicLit
		xgoutil.WalkPathEnclosingInterval(astFile, pos, pos, false, func(node xgoast.Node) bool {
			if node, ok := node.(*xgoast.BasicLit); ok {
				lit = node
				return false
			}
			return true
		})
		require.NotNil(t, lit)

		sprite := resolveSpxSpriteResourceForNode(result, lit)
		require.NotNil(t, sprite)
		assert.Equal(t, "MySprite", sprite.Name)
	})

	t.Run("ImplicitReceiver", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///MySprite.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)

		pos := PosAt(result.proj, astFile, Position{Line: 2, Character: 13})
		require.True(t, pos.IsValid())

		var lit *xgoast.BasicLit
		xgoutil.WalkPathEnclosingInterval(astFile, pos, pos, false, func(node xgoast.Node) bool {
			if node, ok := node.(*xgoast.BasicLit); ok {
				lit = node
				return false
			}
			return true
		})
		require.NotNil(t, lit)

		sprite := resolveSpxSpriteResourceForNode(result, lit)
		require.NotNil(t, sprite)
		assert.Equal(t, "MySprite", sprite.Name)
	})
}
