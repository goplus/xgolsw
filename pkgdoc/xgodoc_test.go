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

func TestNewXGo(t *testing.T) {
	t.Run("UnclassifiedClass", func(t *testing.T) {
		astFile, err := parser.ParseFile(token.NewFileSet(), "Worker.gox", `var (
	// Value belongs to the unresolved class.
	value int
)

// Limit is a package constant.
const Limit = 10

// Record is an explicitly declared type.
type Record struct {
	// Label names the record.
	Label string
}

// Reset belongs to the unresolved class.
func Reset() {}

// Describe belongs to Record.
func (r *Record) Describe() string { return r.Label }

// Shared is a package variable.
var shared int
`, parser.ParseComments|parser.ParseXGoClass)
		require.NoError(t, err)
		require.False(t, astFile.IsClass)
		require.NotNil(t, astFile.ClassFields)

		doc := NewXGo("main", &ast.Package{
			Name:  "main",
			Files: map[string]*ast.File{"Worker.gox": astFile},
		}, nil)

		assert.Empty(t, doc.Funcs)
		assert.Equal(t, map[string]string{"shared": "Shared is a package variable.\n"}, doc.Vars)
		assert.Equal(t, map[string]string{"Limit": "Limit is a package constant.\n"}, doc.Consts)
		require.Len(t, doc.Types, 1)
		record := doc.Types["Record"]
		require.NotNil(t, record)
		assert.Equal(t, "Record is an explicitly declared type.\n", record.Doc)
		assert.Equal(t, map[string]string{"Label": "Label names the record.\n"}, record.Fields)
		assert.Equal(t, map[string]string{"Describe": "Describe belongs to Record.\n"}, record.Methods)
		assert.False(t, astFile.IsClass)
	})

	t.Run("NormalClassWithNilLookup", func(t *testing.T) {
		astFile, err := parser.ParseEntry(token.NewFileSet(), "Record.gox", `var (
	// Value belongs to Record.
	value int
)

// Reset belongs to Record.
func Reset() {}
`, parser.Config{Mode: parser.ParseComments})
		require.NoError(t, err)
		require.True(t, astFile.IsNormalGox)

		doc := NewXGo("main", &ast.Package{
			Name:  "main",
			Files: map[string]*ast.File{"Record.gox": astFile},
		}, nil)

		assert.Empty(t, doc.Vars)
		assert.Empty(t, doc.Funcs)
		require.Len(t, doc.Types, 1)
		record := doc.Types["Record"]
		require.NotNil(t, record)
		assert.Equal(t, map[string]string{"value": "Value belongs to Record.\n"}, record.Fields)
		assert.Equal(t, map[string]string{"Reset": "Reset belongs to Record.\n"}, record.Methods)
	})
}

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
	}, nil)

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

func TestNewXGoTypeDocumentation(t *testing.T) {
	for _, tt := range []struct {
		name string
		typ  string
	}{
		{name: "Alias", typ: "= int"},
		{name: "Basic", typ: "int"},
		{name: "Slice", typ: "[]int"},
		{name: "Map", typ: "map[string]int"},
		{name: "Function", typ: "func(int) string"},
		{name: "Interface", typ: "interface { Read() int }"},
		{name: "Struct", typ: "struct { Value int }"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "main.xgo", "// Value is documented.\ntype Value "+tt.typ+"\n", parser.ParseComments)
			require.NoError(t, err)
			doc := NewXGo("main", &ast.Package{Name: "main", Files: map[string]*ast.File{"main.xgo": file}}, nil)
			require.Contains(t, doc.Types, "Value")
			assert.Equal(t, "Value is documented.\n", doc.Types["Value"].Doc)
		})
	}

	t.Run("Members", func(t *testing.T) {
		file, err := parser.ParseFile(token.NewFileSet(), "main.xgo", `import f "example.com/framework"

type Item struct{}
type Group struct {
	// Item is the active item.
	*Item
	// Copy is an embedded framework value.
	f.Copy
	// Box contains a value.
	f.Box[int]
	// Pair contains two values.
	*f.Pair[int, string]
	// Label names the group.
	label string
	count int // Count tracks items.
}

type Reader interface {
	// Read retrieves one value.
	Read() int
	f.Reader
}
`, parser.ParseComments)
		require.NoError(t, err)
		doc := NewXGo("main", &ast.Package{Name: "main", Files: map[string]*ast.File{"main.xgo": file}}, nil)
		require.Contains(t, doc.Types, "Group")
		assert.Equal(t, map[string]string{
			"Item": "Item is the active item.\n", "Copy": "Copy is an embedded framework value.\n",
			"Box": "Box contains a value.\n", "Pair": "Pair contains two values.\n",
			"label": "Label names the group.\n", "count": "Count tracks items.\n",
		}, doc.Types["Group"].Fields)
		require.Contains(t, doc.Types, "Reader")
		assert.Equal(t, map[string]string{"Read": "Read retrieves one value.\n"}, doc.Types["Reader"].Methods)
	})
}
