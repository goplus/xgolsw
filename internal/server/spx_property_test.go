package server

import (
	"encoding/json"
	gotypes "go/types"
	"slices"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentRenameSpxPropertyVisibility(t *testing.T) {
	for _, tt := range []struct {
		name, source, newName string
		want                  bool
	}{
		{name: "PromotedField", source: "type Base struct { title string }\nvar Base\nfunc La|bel() string { return \"\" }\n", newName: "Title"},
		{name: "PromotedSliceField", source: "type Base struct { title []string }\nvar Base\nfunc La|bel() string { return \"\" }\n", newName: "Title"},
		{name: "InternalField", source: "var Va|lue string\n", newName: "XGo_value", want: true},
		{name: "OrdinaryGetter", source: "func La|bel() string { return \"\" }\n", newName: "Title", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte(source)})
			replier := newMockReplier()
			s.replier = replier
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}, Position: pos, NewName: tt.newName})
			require.NoError(t, err)
			require.NotNil(t, edit)
			if !tt.want {
				assert.Empty(t, replier.getMessages())
				return
			}
			require.Len(t, replier.getMessages(), 1)
			var params PropertyRenamedParams
			require.NoError(t, json.Unmarshal(requireValueAs[*jsonrpc2.Notification](t, replier.getMessages()[0]).Params(), &params))
			assert.Equal(t, "Game", params.Target)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.spx"])
			s.ModifyFiles([]FileChange{{Path: "main.spx", Content: []byte(updated), Version: 1}})
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Game"})
			require.NoError(t, err)
			assert.True(t, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == params.NewName }))
		})
	}
}

func TestServerXGoGetPropertiesSpx(t *testing.T) {
	t.Run("UnicodeMethodNames", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte("func \u0393amma() int { return 1 }\n")})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Game"})
		require.NoError(t, err)
		assert.False(t, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == "\u0393amma" }),
			"spx only creates automatic aliases for ASCII method names")
	})

	t.Run("ClassMembers", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx": []byte("var score int\n"),
			"MySprite.spx": []byte(`var health int

func GetDamage() int { return 10 }
`),
		}
		s := newSpxTestServer(t, files)
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		for _, tt := range []struct {
			target string
			field  string
			owner  string
		}{
			{target: "Game", field: "score", owner: "Game"},
			{target: "MySprite", field: "health", owner: "Sprite"},
		} {
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: tt.target})
			require.NoError(t, err)
			assert.Contains(t, properties, XGoProperty{
				Name: tt.field, Type: "int", Kind: XGoPropertyKindField,
				Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr(tt.target + "." + tt.field)},
			})
			idx := slices.IndexFunc(properties, func(property XGoProperty) bool { return property.Name == "volume" })
			require.NotEqual(t, -1, idx, "volume")
			volume := properties[idx]
			assert.Equal(t, XGoPropertyKindMethod, volume.Kind)
			assert.NotEmpty(t, volume.Doc, "%s.volume", tt.target)
			assert.Equal(t, XGoDefinitionIdentifier{Package: ToPtr(SpxPkgPath), Name: ToPtr(tt.owner + ".volume")}, volume.Definition)
			if tt.target == "MySprite" {
				assert.Contains(t, properties, XGoProperty{
					Name: "xpos", Type: "float64", Kind: XGoPropertyKindMethod,
					Definition: XGoDefinitionIdentifier{Package: ToPtr(SpxPkgPath), Name: ToPtr("Sprite.xpos")},
				})
				assert.Contains(t, properties, XGoProperty{
					Name: "getDamage", Type: "int", Kind: XGoPropertyKindMethod,
					Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("MySprite.GetDamage")},
				})
			}
		}
	})

	t.Run("ValueAndList", func(t *testing.T) {
		files := map[string][]byte{"main.spx": []byte(`var (
    value Value
    list List
)

func CurrentValue() Value { return value }
func CurrentList() List { return list }
`)}
		s := newSpxTestServer(t, files)
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Game"})
		require.NoError(t, err)
		for _, want := range []XGoProperty{
			{Name: "value", Type: "Value", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Game.value")}},
			{Name: "list", Type: "List", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Game.list")}},
			{Name: "currentValue", Type: "Value", Kind: XGoPropertyKindMethod, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Game.CurrentValue")}},
			{Name: "currentList", Type: "List", Kind: XGoPropertyKindMethod, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Game.CurrentList")}},
		} {
			assert.Contains(t, properties, want)
		}
	})
}

func TestSpxSymbolsMonitorValueTypes(t *testing.T) {
	s := newSpxTestServer(t, nil)
	other := newSpxTestServer(t, nil)
	ctx := newSpxSymbols(s.getProj())
	for _, name := range []string{"Value", "List"} {
		t.Run(name, func(t *testing.T) {
			for _, source := range []*Server{s, other} {
				typ := spxTestType(t, source, name)
				field := gotypes.NewField(token.NoPos, nil, "Data", typ, false)
				getter := gotypes.NewFunc(token.NoPos, nil, "Data", gotypes.NewSignatureType(nil, nil, nil, nil,
					gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "", typ)), false))
				assert.Equal(t, source == s, ctx.isMonitorField(field))
				assert.Equal(t, source == s, ctx.isMonitorValueType(getter.Signature().Results().At(0).Type()))
			}
		})
	}
}

func TestSpxSymbolsMonitorProperties(t *testing.T) {
	for _, tt := range []struct {
		name, declarations, fields string
		wantType                   string
		wantLabel                  bool
	}{
		{name: "BasicField", fields: "Count int", wantType: "int"},
		{name: "PointerField", fields: "Count *int", wantType: "*int"},
		{name: "PointerAliasField", declarations: "type Counter = *int", fields: "Count Counter", wantType: "Counter"},
		{name: "SliceField", fields: "Count []int"},
		{name: "NamedScalar", declarations: "type Count int", fields: "Count Count"},
		{name: "ValueField", fields: "Count spx.Value", wantType: "spx.Value"},
		{name: "ListField", fields: "Count spx.List", wantType: "spx.List"},
		{name: "Getter", declarations: "func (*Record) Label() string { return \"\" }", wantLabel: true},
		{name: "PointerGetter", declarations: "func (*Record) Label() *int { return nil }", wantLabel: true},
		{name: "PointerAliasGetter", declarations: "type Counter = *int\nfunc (*Record) Label() Counter { return nil }", wantLabel: true},
		{name: "GetterWithParameter", declarations: "func (*Record) Label(value int) string { return \"\" }"},
		{name: "GetterReturningSlice", declarations: "func (*Record) Label() []int { return nil }"},
		{name: "Overload", declarations: "func (Record) text() string { return \"\" }\nfunc (Record) number() int { return 0 }\nfunc (Record).Label = ((Record).text; (Record).number)"},
		{name: "FieldBeforeMethod", declarations: "type Base struct { label []int }\nfunc (*Record) Label() string { return \"\" }", fields: "Base"},
		{name: "PrivateEmbedding", declarations: "type base struct { Count int }", fields: "base"},
		{name: "ShallowerField", declarations: "type Deep struct { Count int }\ntype Left struct { Deep }\ntype Right struct { Count string }", fields: "Left; Right", wantType: "string"},
		{name: "AmbiguousMethod", declarations: "type Left struct{}\nfunc (Left) Label() string { return \"\" }\ntype Right struct{}\nfunc (Right) Label() string { return \"\" }", fields: "Left; Right"},
		{name: "RecursiveEmbedding", declarations: "type Base struct { *Record; Count int }", fields: "*Base", wantType: "int"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "import spx \"" + SpxPkgPath + "\"\n" + tt.declarations + "\ntype Record struct { spx.Game; " + tt.fields + " }\n"
			s := newSpxTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
			require.NoError(t, err)
			index := slices.IndexFunc(properties, func(p XGoProperty) bool { return p.Name == "Count" })
			if tt.wantType == "" {
				assert.Equal(t, -1, index)
			} else {
				require.NotEqual(t, -1, index)
				assert.Equal(t, tt.wantType, properties[index].Type)
			}
			assert.Equal(t, tt.wantLabel, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == "label" }))
		})
	}
}

func TestSpxSymbolsPropertyTargetIsolation(t *testing.T) {
	s := newSpxTestServer(t, map[string][]byte{
		"main.spx":  []byte("var Values []int\nfunc Items() []int { return nil }\n"),
		"types.xgo": []byte("type Record struct { Values []int }\nfunc (Record) Items() []int { return nil }\n"),
	})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	for _, tt := range []struct {
		name string
		want bool
	}{{"Game", false}, {"Record", true}} {
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: tt.name})
		require.NoError(t, err)
		for _, name := range []string{"Values", "items"} {
			assert.Equal(t, tt.want, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == name }), "%s.%s", tt.name, name)
		}
	}
	other := newSpxTestServer(t, nil)
	symbols := newSpxSymbols(s.requestProject())
	otherBase := requireValueAs[*gotypes.Named](t, spxTestType(t, other, "Game"))
	assert.Nil(t, symbols.properties(otherBase))
}

func TestServerXGoGetInputSlotsSpxSourceProperties(t *testing.T) {
	s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte("var values []int\nfunc Items() []int { return nil }\nfunc use(value []int) {}\nfunc run() { use values }\n")})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Game"})
	require.NoError(t, err)
	assert.False(t, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == "items" }))
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	assert.Contains(t, slots[len(slots)-1].PredefinedNames, "items")
}
