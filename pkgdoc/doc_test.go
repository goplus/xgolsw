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

	t.Run("Members", func(t *testing.T) {
		doc := newGoTestDoc(t, `package sample
import "io"

type Item struct{}
type Box[T any] struct{}
type Pair[T, U any] struct{}

// Group combines embedded fields.
type Group struct {
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
}
// Reset empties the group.
func (g *Group) Reset() {}

// Reader retrieves values.
type Reader interface {
	// Read retrieves one value.
	Read() int
	io.Closer
	// hidden is not exported.
	hidden()
}

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

func newGoTestDoc(t *testing.T, source string) *PkgDoc {
	t.Helper()

	file, err := goparser.ParseFile(token.NewFileSet(), "sample.go", source, goparser.ParseComments)
	require.NoError(t, err)
	return NewGo("example.com/sample", &goast.Package{Name: "sample", Files: map[string]*goast.File{"sample.go": file}})
}
