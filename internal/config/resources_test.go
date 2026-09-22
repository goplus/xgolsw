package config

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseResourceConfig(t *testing.T) {
	const valid = `{"dataFile":"assets/resources.json","types":[{"pkgPath":"example.com/framework","typeName":"ClipName","contextURI":"demo://resources/clips"}]}`
	data := []byte(valid)
	config, err := ParseResourceConfig(data)
	require.NoError(t, err)
	clear(data)
	assert.Equal(t, "assets/resources.json", config.DataFile())
	bindings := slices.Collect(config.Types())
	assert.Equal(t, []ResourceType{{"example.com/framework", "ClipName", "demo://resources/clips"}}, bindings)
	require.Len(t, bindings, 1)
	bindings[0].ContextURI = "other://resources/clips"
	assert.Equal(t, "demo://resources/clips", slices.Collect(config.Types())[0].ContextURI)
	for range config.Types() {
		break
	}
	for _, tt := range []struct{ name, data string }{
		{"Syntax", `{`},
		{"Null", `null`},
		{"Array", `[]`},
		{"TrailingObject", valid + `{}`},
		{"TrailingGarbage", valid + `garbage`},
		{"UnknownField", `{"dataFile":"resources.json","rules":[]}`},
		{"EmptyTypes", `{"dataFile":"resources.json","types":[]}`},
		{"AbsolutePath", `{"dataFile":"/resources.json"}`},
		{"ParentPath", `{"dataFile":"../resources.json"}`},
		{"BackslashPath", `{"dataFile":"assets\\resources.json"}`},
		{"SourceFile", `{"dataFile":"resources.xgo"}`},
		{"InvalidPackage", `{"dataFile":"resources.json","types":[{"pkgPath":"bad path","typeName":"Clip","contextURI":"demo://clips"}]}`},
		{"InvalidType", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"*Clip","contextURI":"demo://clips"}]}`},
		{"BlankType", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"_","contextURI":"demo://clips"}]}`},
		{"RelativeURI", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"clips"}]}`},
		{"OpaqueURI", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"demo:clips"}]}`},
		{"TrailingSlash", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"demo://clips/"}]}`},
		{"Query", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"demo://clips?q=1"}]}`},
		{"Fragment", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"demo://clips#part"}]}`},
		{"InvalidEscape", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"demo://resources/%"}]}`},
		{"DuplicateType", `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"demo://clips"},{"pkgPath":"main","typeName":"Clip","contextURI":"demo://other"}]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResourceConfig([]byte(tt.data))
			assert.Error(t, err)
		})
	}
}
