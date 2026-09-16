package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefinitionContextMemberForObject(t *testing.T) {
	t.Run("InstantiatedFields", func(t *testing.T) {
		s := newClassfileTestServer(t, nil)
		pkg, err := s.getProj().Importer.Import("example.com/first/internal/base")
		require.NoError(t, err)
		box := pkg.Scope().Lookup("Box").Type()
		intBox, err := gotypes.Instantiate(nil, box, []gotypes.Type{gotypes.Typ[gotypes.Int]}, true)
		require.NoError(t, err)
		stringBox, err := gotypes.Instantiate(nil, box, []gotypes.Type{gotypes.Typ[gotypes.String]}, true)
		require.NoError(t, err)
		alias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "IntAlias", nil), gotypes.NewPointer(intBox))
		ctx := &definitionContext{proj: s.getProj(), lookupPkgDoc: s.lookupPkgDoc}
		var origin *gotypes.Var
		for range 2 {
			for _, tt := range []struct {
				receiver gotypes.Type
				typ      gotypes.Type
			}{
				{intBox, gotypes.Typ[gotypes.Int]},
				{stringBox, gotypes.Typ[gotypes.String]},
				{alias, gotypes.Typ[gotypes.Int]},
			} {
				receiver := tt.receiver
				named := resolvedNamedType(receiver)
				require.NotNil(t, named)
				field := requireValueAs[*gotypes.Struct](t, named.Underlying()).Field(0)
				if origin == nil {
					origin = field.Origin()
				}
				require.Same(t, origin, field.Origin())
				member := ctx.memberForObject(receiver, field)
				require.NotNil(t, member)
				assert.Same(t, field, member.Member)
				assert.Same(t, named, member.Selector)
				defs := ctx.definitionsForSelection(field, receiver)
				require.Len(t, defs, 1)
				assert.Equal(t, "xgo:example.com/first/internal/base?Box.Value", defs[0].ID.String())
				assert.True(t, gotypes.Identical(tt.typ, defs[0].TypeHint))
				assert.Contains(t, defs[0].Detail, "Value belongs to the box in first.")
			}
		}
	})

	t.Run("SelectedOverloads", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`type Worker struct{}
func (w *Worker) handleInt(value int) {}
func (w *Worker) handleString(value string) {}
func (Worker).handle = (
    (Worker).handleInt
    (Worker).handleString
)
`)})
		info, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		named := requirePropertyTestType(t, info.Pkg, "Worker")
		method := requireValueAs[*gotypes.Func](t, requirePropertyTestMember(t, named, "handle"))
		overloads := xgoutil.ExpandXGoOverloadableFunc(method)
		require.Len(t, overloads, 2)
		ctx := &definitionContext{proj: s.getProj()}
		for range 2 {
			for _, overload := range append(overloads, method) {
				member := ctx.memberForObject(named, overload)
				require.NotNil(t, member)
				assert.Same(t, overload, member.Member)
				assert.Same(t, named, member.Selector)
			}
		}
	})

	t.Run("ShadowedFields", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`type Base struct { Value int }
type Record struct { Base; Value string }
`)})
		info, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		base := requirePropertyTestType(t, info.Pkg, "Base")
		record := requirePropertyTestType(t, info.Pkg, "Record")
		baseField := requirePropertyTestMember(t, base, "Value")
		field := requirePropertyTestMember(t, record, "Value")
		ctx := &definitionContext{proj: s.getProj()}
		for range 2 {
			assert.Nil(t, ctx.memberForObject(record, baseField))
			member := ctx.memberForObject(record, field)
			require.NotNil(t, member)
			assert.Same(t, field, member.Member)
			assert.Same(t, record, member.Selector)
		}
	})

	t.Run("InterfaceSelectors", func(t *testing.T) {
		s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte(`import f "example.com/first"
type CombinedFirst interface { f.CombinedReader; f.PublicReader }
type PublicFirst interface { f.PublicReader; f.CombinedReader }
`)})
		info, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		ctx := &definitionContext{proj: s.getProj(), lookupPkgDoc: s.lookupPkgDoc}
		for range 2 {
			for _, tt := range []struct{ name, selector string }{
				{"CombinedFirst", "CombinedReader"},
				{"PublicFirst", "PublicReader"},
			} {
				receiver := info.Pkg.Scope().Lookup(tt.name).Type()
				method := requireValueAs[*gotypes.Interface](t, receiver.Underlying()).Method(0)
				defs := ctx.definitionsForSelection(method, receiver)
				require.Len(t, defs, 1)
				assert.Equal(t, "xgo:example.com/first?"+tt.selector+".read", defs[0].ID.String())
				assert.Contains(t, defs[0].Detail, "Read belongs to the reader in first.")
			}
		}
	})
}

func TestImportedInterfaceMembers(t *testing.T) {
	for _, tt := range []struct {
		name  string
		limit int
	}{
		{"SharedEmbeddings", 2},
		{"StopAfterFirst", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newClassfileTestServer(t, nil)
			first, err := s.getProj().Importer.Import("example.com/first")
			require.NoError(t, err)
			second, err := s.getProj().Importer.Import("example.com/second")
			require.NoError(t, err)
			// Type constraints can include non-interface terms and repeated
			// embedded interfaces reached through aliases.
			receiver := gotypes.NewInterfaceType(nil, []gotypes.Type{
				gotypes.Typ[gotypes.Int],
				first.Scope().Lookup("PublicReader").Type(),
				first.Scope().Lookup("ReaderAlias").Type(),
				second.Scope().Lookup("PublicReader").Type(),
			}).Complete()
			var packages []string
			for member := range importedInterfaceMembers(receiver) {
				assert.Equal(t, "Read", member.Member.Name())
				packages = append(packages, member.Selector.Obj().Pkg().Path())
				if tt.limit == 1 {
					break
				}
			}
			assert.Equal(t, []string{"example.com/first", "example.com/second"}[:tt.limit], packages)
		})
	}
}
