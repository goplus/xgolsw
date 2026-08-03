package pkgdoc

import (
	"encoding/json"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewXGoEnumMemberDocumentation(t *testing.T) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, "main.xgo", `type First const (
	_ = iota
	// First member documentation.
	Unknown
)

type Second const (
	// Second member documentation.
	Unknown = iota
)
`, parser.ParseComments)
	require.NoError(t, err)

	pkgDoc := NewXGo("main", &ast.Package{
		Name:  "main",
		Files: map[string]*ast.File{"main.xgo": astFile},
	})

	require.Contains(t, pkgDoc.Types, "First")
	require.Contains(t, pkgDoc.Types, "Second")
	assert.Equal(t, "First member documentation.\n", pkgDoc.Types["First"].EnumMembers["Unknown"])
	assert.Equal(t, "Second member documentation.\n", pkgDoc.Types["Second"].EnumMembers["Unknown"])
	assert.NotContains(t, pkgDoc.Types["First"].EnumMembers, "_")
	assert.NotContains(t, pkgDoc.Consts, "Unknown")
}

func TestTypeDocJSON(t *testing.T) {
	data, err := json.Marshal(&TypeDoc{})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "EnumMembers")

	data, err = json.Marshal(&TypeDoc{EnumMembers: map[string]string{"Red": "Red documentation."}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"Doc":"","Fields":null,"Methods":null,"EnumMembers":{"Red":"Red documentation."}}`, string(data))
}
