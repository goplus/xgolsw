package server

import (
	gotypes "go/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
