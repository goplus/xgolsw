package pkgdoc

import (
	goast "go/ast"
	goparser "go/parser"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGo(t *testing.T) {
	t.Run("AssociatedDeclarations", func(t *testing.T) {
		for _, marker := range []string{"", "XGoPackage", "GopPackage"} {
			name := marker
			if name == "" {
				name = "GoPackage"
			}
			t.Run(name, func(t *testing.T) {
				source := `// Package sample supplies documented values.
package sample

type Item struct{}
type hidden int

// Current is the active item.
var Current Item
// Public is an opaque value.
var Public hidden
// Limit is an opaque limit.
const Limit hidden = 1
// NewItem creates an item.
func NewItem() *Item { return nil }
// Items creates several items.
func Items() ([]Item, error) { return nil, nil }
// NewHidden creates an opaque value.
func NewHidden() hidden { return 0 }
// XGot_Item_Copy copies an item.
func XGot_Item_Copy(item *Item) *Item { return item }
// Print prints a message.
func Print() {}
// private is not exported.
func private() {}
`
				if marker != "" {
					source += "\nconst " + marker + " = true\n"
				}
				doc := newGoTestDoc(t, source)
				assert.Equal(t, "Package sample supplies documented values.\n", doc.Doc)
				assert.Equal(t, "example.com/sample", doc.Path)
				assert.Equal(t, "sample", doc.Name)
				assert.Equal(t, map[string]string{
					"Current": "Current is the active item.\n",
					"Public":  "Public is an opaque value.\n",
				}, doc.Vars)
				assert.Equal(t, "Limit is an opaque limit.\n", doc.Consts["Limit"])
				assert.Equal(t, map[string]string{
					"NewItem":        "NewItem creates an item.\n",
					"Items":          "Items creates several items.\n",
					"NewHidden":      "NewHidden creates an opaque value.\n",
					"XGot_Item_Copy": "XGot_Item_Copy copies an item.\n",
					"Print":          "Print prints a message.\n",
				}, doc.Funcs)
				assert.NotContains(t, doc.Types, "hidden")
				require.Contains(t, doc.Types, "Item")
				if marker == "" {
					assert.Empty(t, doc.Types["Item"].Methods)
				} else {
					assert.Equal(t, "XGot_Item_Copy copies an item.\n", doc.Types["Item"].Methods["Copy"])
				}
			})
		}
	})

	t.Run("OverloadMarkers", func(t *testing.T) {
		doc := newGoTestDoc(t, `package sample

const XGoPackage = true

// Additional overloads.
const (
	// Pick selects a value.
	XGoo_Pick = "PickInt"
	XGoo_Choose = "ChooseInt"
	private = "internal"
)
`)
		assert.Equal(t, "Pick selects a value.\n", doc.Consts["XGoo_Pick"])
		assert.Equal(t, "Additional overloads.\n", doc.Consts["XGoo_Choose"])
		assert.NotContains(t, doc.Consts, "private")
	})

	t.Run("Members", func(t *testing.T) {
		doc := newGoTestDoc(t, `package sample
import "io"

type Item struct{}
type Box[T any] struct{}
type Pair[T, U any] struct{}

// Group combines embedded fields.
type Group (struct {
	// Item is the active item.
	*Item
	// Reader supplies input.
	io.Reader
	// Box contains a value.
	Box[int]
	// Pair contains two values.
	*Pair[int, string]
	// Labels name the group.
	First, Last string
	Count int // Count tracks items.
	// Secret is not exported.
	secret int
})
// Reset empties the group.
func (g *Group) Reset() {}

// Reader retrieves values.
type Reader (interface {
	// Read retrieves one value.
	Read() int
	io.Closer
	// hidden is not exported.
	hidden()
})

// Alias names an item.
type Alias = Item
`)
		require.Contains(t, doc.Types, "Group")
		assert.Equal(t, "Group combines embedded fields.\n", doc.Types["Group"].Doc)
		assert.Equal(t, map[string]string{
			"Item": "Item is the active item.\n", "Reader": "Reader supplies input.\n",
			"Box": "Box contains a value.\n", "Pair": "Pair contains two values.\n",
			"First": "Labels name the group.\n", "Last": "Labels name the group.\n",
			"Count": "Count tracks items.\n",
		}, doc.Types["Group"].Fields)
		assert.Equal(t, "Reset empties the group.\n", doc.Types["Group"].Methods["Reset"])
		require.Contains(t, doc.Types, "Reader")
		assert.Equal(t, map[string]string{"Read": "Read retrieves one value.\n"}, doc.Types["Reader"].Methods)
		require.Contains(t, doc.Types, "Alias")
		assert.Equal(t, "Alias names an item.\n", doc.Types["Alias"].Doc)
	})
}

func TestNewGoPrivateEmbeddedMemberDocumentation(t *testing.T) {
	doc := newGoTestDoc(t, `package sample
type unused struct { Value int }
type embedded[T any] struct {
	// Value stores the payload.
	Value T
	Undocumented int
}
type empty struct { Undocumented int }
type embeddedAlias = embedded[int]
type middle struct { *embeddedAlias }
type Record struct { middle; empty }
type getter interface {
	// Read retrieves a value.
	Read() int
}
type reader interface { getter }
type Reader interface { reader }
`)
	assert.NotContains(t, doc.Types, "unused")
	assert.NotContains(t, doc.Types, "empty")
	require.Contains(t, doc.Types, "embedded")
	assert.Equal(t, map[string]string{"Value": "Value stores the payload.\n"}, doc.Types["embedded"].Fields)
	require.Contains(t, doc.Types, "getter")
	assert.Equal(t, "Read retrieves a value.\n", doc.Types["getter"].Methods["Read"])
}

func TestNewGoPublicAliasMethodDocumentation(t *testing.T) {
	doc := newGoTestDoc(t, `package sample
type counter[T any] struct {
	// Value stores the count.
	Value T
}
// Read returns the count.
func (value counter[T]) Read() T { return value.Value }
func (value counter[T]) Undocumented() {}
type PublicCounter = counter[int]
type resettable[T any] struct { Value T }
// Reset clears the count.
func (value *resettable[T]) Reset() { var zero T; value.Value = zero }
type resettablePointer = *resettable[int]
type PublicPointer = resettablePointer
type unused struct{}
// Hidden is declared on an inaccessible type.
func (unused) Hidden() {}
`)
	require.Contains(t, doc.Types, "counter")
	assert.Equal(t, map[string]string{"Read": "Read returns the count.\n"}, doc.Types["counter"].Methods)
	assert.Empty(t, doc.Types["counter"].Fields)
	require.Contains(t, doc.Types, "resettable")
	assert.Equal(t, map[string]string{"Reset": "Reset clears the count.\n"}, doc.Types["resettable"].Methods)
	assert.NotContains(t, doc.Types, "unused")
}

func TestNewGoPublicStructFieldDocumentation(t *testing.T) {
	doc := newGoTestDoc(t, `package sample
type privateValue[T any] = (struct {
	// Value stores the payload.
	Value T
})
type privateRecord privateValue[int]
type PublicRecord privateRecord
type PublicValue = privateValue[int]
type Pointer = *privateRecord
type DefinedPointer *privateRecord
type DoublePointer = *Pointer
`)
	assert.NotContains(t, doc.Types, "privateRecord")
	for _, name := range []string{"PublicRecord", "PublicValue", "Pointer", "DefinedPointer"} {
		require.Contains(t, doc.Types, name)
		assert.Equal(t, map[string]string{"Value": "Value stores the payload.\n"}, doc.Types[name].Fields)
	}
	require.Contains(t, doc.Types, "DoublePointer")
	assert.Empty(t, doc.Types["DoublePointer"].Fields)
}

func TestNewGoPublicInterfaceMethodDocumentation(t *testing.T) {
	doc := newGoTestDoc(t, `package sample
type privateReader[T any] = (interface {
	// Read retrieves a value.
	Read() T
})
type concreteReader = privateReader[int]
type PublicReader = concreteReader
type ForwardedReader = PublicReader
type internalReader interface {
	// Read reads an internal value.
	Read() int
}
type NamedReader internalReader
type Pointer = *PublicReader
`)
	assert.NotContains(t, doc.Types, "privateReader")
	assert.NotContains(t, doc.Types, "concreteReader")
	for _, name := range []string{"PublicReader", "ForwardedReader"} {
		require.Contains(t, doc.Types, name)
		assert.Equal(t, map[string]string{"Read": "Read retrieves a value.\n"}, doc.Types[name].Methods)
	}
	require.Contains(t, doc.Types, "Pointer")
	assert.Empty(t, doc.Types["Pointer"].Methods)
	require.Contains(t, doc.Types, "NamedReader")
	assert.Equal(t, map[string]string{"Read": "Read reads an internal value.\n"}, doc.Types["NamedReader"].Methods)
}

func newGoTestDoc(t *testing.T, source string) *PkgDoc {
	t.Helper()

	file, err := goparser.ParseFile(token.NewFileSet(), "sample.go", source, goparser.ParseComments)
	require.NoError(t, err)
	return NewGo("example.com/sample", &goast.Package{Name: "sample", Files: map[string]*goast.File{"sample.go": file}})
}

func TestNewGoExtendedMethodOwners(t *testing.T) {
	for _, tt := range []struct {
		name, current string
		marked        bool
	}{
		{name: "Historical", marked: true},
		{name: "Current", current: "const XGoPackage = false", marked: true},
		{name: "CurrentVariable", current: "var XGoPackage bool"},
		{name: "CurrentType", current: "type XGoPackage bool"},
		{name: "CurrentFunction", current: "func XGoPackage() bool { return true }"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			doc := newGoTestDoc(t, `package sample
const GopPackage = true
type Item struct{}
type hidden struct{}
type Alias = Item

func Gopt_Item_Copy(item *Item) {}
func Gopt_hidden_Copy(item *hidden) {}
func Gopt_Missing_Copy(item int) {}
func Gopt_Alias_Copy(item *Alias) {}

// Choose selects an item.
func XGos_Item_Choose() {}
// Find_Value finds an item.
func Gops__Item__Find_Value() {}
func XGos_Missing_Choose() {}
func XGos_Alias_Choose() {}
`+tt.current+"\n")
			require.Contains(t, doc.Types, "Item")
			if tt.marked {
				assert.Contains(t, doc.Types["Item"].Methods, "Copy")
				assert.Equal(t, "Choose selects an item.\n", doc.Types["Item"].Methods["Choose"])
				assert.Equal(t, "Find_Value finds an item.\n", doc.Types["Item"].Methods["Find_Value"])
				require.Contains(t, doc.Types, "hidden")
				assert.Contains(t, doc.Types["hidden"].Methods, "Copy")
			} else {
				assert.Empty(t, doc.Types["Item"].Methods)
				assert.NotContains(t, doc.Types, "hidden")
			}
			assert.NotContains(t, doc.Types, "Missing")
			require.Contains(t, doc.Types, "Alias")
			assert.Empty(t, doc.Types["Alias"].Methods)
			assert.Contains(t, doc.Funcs, "Gopt_Missing_Copy")
			assert.Equal(t, "Choose selects an item.\n", doc.Funcs["XGos_Item_Choose"])
			assert.Equal(t, "Find_Value finds an item.\n", doc.Funcs["Gops__Item__Find_Value"])
			assert.Contains(t, doc.Funcs, "XGos_Missing_Choose")
		})
	}
}

func TestNewGoReconstructedMethodPrecedence(t *testing.T) {
	doc := newGoTestDoc(t, `package sample
const XGoPackage = true
type Base struct{}
// Collect is promoted from Base.
func (Base) Collect() int { return 1 }
// Reset is promoted from Base.
func (Base) Reset() {}
type Item struct { Base }
type Alias = Item
// Read is the declared method.
func (Item) Read() int { return 2 }
// Reset is declared on an alias of Item.
func (Alias) Reset() {}
func (Item) Write() {}
// XGot_Item_Read is the encoded package function.
func XGot_Item_Read(item Item, n string) string { return n }
// XGot_Item_Reset is an encoded package function for Reset.
func XGot_Item_Reset(item Item) {}
// XGot_Item_Write is an encoded package function for Write.
func XGot_Item_Write(item Item) {}
// XGot_Item_Collect supplies the direct template method.
func XGot_Item_Collect(item Item) int { return 3 }
// Gopt_Item_Measure is the first template declaration.
func Gopt_Item_Measure(item Item) int { return 1 }
// XGot_Item_Measure declares another template spelling.
func XGot_Item_Measure(item Item) string { return "" }
`)
	require.Contains(t, doc.Types, "Item")
	assert.Equal(t, map[string]string{
		"Collect": "XGot_Item_Collect supplies the direct template method.\n",
		"Measure": "Gopt_Item_Measure is the first template declaration.\n",
		"Read":    "Read is the declared method.\n",
		"Reset":   "Reset is declared on an alias of Item.\n",
		"Write":   "",
	}, doc.Types["Item"].Methods)
	require.Contains(t, doc.Types, "Alias")
	assert.Equal(t, "Reset is declared on an alias of Item.\n", doc.Types["Alias"].Methods["Reset"])
	assert.Equal(t, "XGot_Item_Read is the encoded package function.\n", doc.Funcs["XGot_Item_Read"])
	assert.Equal(t, "XGot_Item_Measure declares another template spelling.\n", doc.Funcs["XGot_Item_Measure"])
}
