package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerClassMemberCacheAcrossEdits(t *testing.T) {
	const filename = "main_fixture.gox"
	const original = "func run() { use |\"text\" }\n"
	source, _ := typeDisplayTestSource(t, original)
	s := newImportTestServer(t, map[string][]byte{
		filename:      []byte(source),
		"globals.xgo": []byte("func Use(value string) {}\n"),
	})
	t.Cleanup(s.Close)
	setImportTestFrameworkMethods(t, s, "func (*App) Label() string { return \"\" }\n")

	lookup := func(source string, wantSlot bool) (*gotypes.Named, autoProperty) {
		t.Helper()

		_, pos := typeDisplayTestSource(t, source)
		require.NotEmpty(t, completionItemsAt(t, s, filename, pos))
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{
			TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)},
		}})
		require.NoError(t, err)
		require.NotEmpty(t, slots)
		if wantSlot {
			assert.Contains(t, slots[len(slots)-1].PredefinedNames, "label")
		} else {
			assert.NotContains(t, slots[len(slots)-1].PredefinedNames, "label")
		}

		proj := s.requestProject()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		parsed, err := proj.ASTPackage()
		require.NoError(t, err)
		class := info.ClassTypes[parsed.Files[filename]]
		require.NotNil(t, class)
		resolver := newClassAutoPropertyResolver(proj, gotypes.NewPointer(class))
		return class, resolver.resolve("label")
	}
	edit := func(source string, version int) {
		t.Helper()

		content, _ := typeDisplayTestSource(t, source)
		s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(content), Version: version}})
	}

	beforeClass, before := lookup(original, true)
	require.NotNil(t, before.selectedMember)
	require.NotNil(t, before.selectedType)
	assert.Equal(t, testframework.PkgPath, before.selectedMember.Pkg().Path())
	assert.Equal(t, gotypes.Typ[gotypes.String], before.typ)

	edited := strings.Replace(original, "\"text\"", "\"edited\"", 1)
	edit(edited, 1)
	afterClass, after := lookup(edited, true)
	assert.NotSame(t, beforeClass, afterClass)
	assert.Same(t, before.selectedMember, after.selectedMember)
	assert.Same(t, before.selectedType, after.selectedType)

	shadowed := "var label int\n\n" + edited
	edit(shadowed, 2)
	_, local := lookup(shadowed, false)
	require.NotNil(t, local.selectedMember)
	assert.Equal(t, "label", local.selectedMember.Name())
	assert.Equal(t, gotypes.Typ[gotypes.Int], local.typ)

	edit(original, 3)
	_, restored := lookup(original, true)
	assert.Same(t, before.selectedType, restored.selectedType)

	setImportTestFrameworkMethods(t, s, "func (*App) Label() int { return 0 }\n")
	edit(edited, 4)
	_, reloaded := lookup(edited, false)
	require.NotNil(t, reloaded.selectedMember)
	assert.NotSame(t, before.selectedMember, reloaded.selectedMember)
	assert.Equal(t, gotypes.Typ[gotypes.Int], reloaded.typ)
}

func TestClassMemberIndex(t *testing.T) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "support.go", `package support
type Base struct { *Base }
func (*Base) Label() string { return "" }
type Other struct {}
func (Other) Label() int { return 0 }
type Class struct { *Base }
type Shadow struct { *Base; label int }
type Conflict struct { *Base; Other }
type Alias = Base
type Aliased struct { Alias }
type Methods interface { Label() int }
type WithInterface struct { *Base; Methods }
type Box[T any] struct { T T }
type WithGeneric struct { *Base; Box[int] }
`, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/support", fset, []*goast.File{file}, nil)
	require.NoError(t, err)

	index := func(name string) classMemberIndex {
		return buildClassMemberIndex(gotypes.NewPointer(pkg.Scope().Lookup(name).Type()))
	}
	member := index("Class").ordinaryImportedMethod("label", "main")
	require.NotNil(t, member.method)
	assert.Equal(t, "Label", member.method.Name())
	assert.Equal(t, "Base", member.owner.Obj().Name())
	assert.Nil(t, index("Class").ordinaryImportedMethod("label", pkg.Path()).method)
	assert.Nil(t, index("Shadow").ordinaryImportedMethod("label", "main").method)
	assert.Nil(t, index("Conflict").ordinaryImportedMethod("label", "main").method)
	assert.Empty(t, index("Aliased"))
	assert.Empty(t, index("WithInterface"))
	assert.Empty(t, index("WithGeneric"))
}
