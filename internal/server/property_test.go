package server

import (
	"encoding/json"
	gotypes "go/types"
	"io/fs"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerXGoGetProperties(t *testing.T) {
	t.Run("UnicodeMethodNames", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Record struct{}\nfunc (Record) \u0393amma() int { return 1 }\nfunc (Record) \u03b4elta() int { return 2 }\n")})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
		require.NoError(t, err)
		require.Len(t, properties, 1)
		assert.Equal(t, "\u0393amma", properties[0].Name)
	})

	t.Run("OverloadWrapper", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`type Record struct{}
func (r *Record) intValue() int { return 1 }
func (r *Record) stringValue() string { return "one" }
func (Record).Value = (
    (Record).intValue
    (Record).stringValue
)
func (r *Record) Label() string { return "record" }
`)})
		info, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		named := requirePropertyTestType(t, info.Pkg, "Record")
		method := requireValueAs[*gotypes.Func](t, requirePropertyTestMember(t, named, "Value"))
		require.Len(t, xgoutil.ExpandXGoOverloadableFunc(method), 2)
		assert.False(t, (&definitionContext{proj: s.getProj()}).isPropertyMethod(method))
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
		require.NoError(t, err)
		require.Len(t, properties, 1)
		assert.Equal(t, "label", properties[0].Name)
		ctx := &completionContext{
			definitionContext: definitionContext{proj: s.getProj(), lookupPkgDoc: s.lookupPkgDoc},
			typeInfo:          info, itemSet: newCompletionItemSet(Markdown),
		}
		ctx.collectPropertyNames("Record")
		assert.Equal(t, []string{`"label"`}, completionItemLabels(ctx.itemSet.items))
	})

	t.Run("DeclarationDocumentation", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"types.xgo":  nil,
			"broken.xgo": []byte("func broken("),
		})
		for version, doc := range []string{"First documentation.", "Second documentation.", ""} {
			s.ModifyFiles([]FileChange{{Path: "types.xgo", Content: []byte(strings.ReplaceAll(`type Item struct {
    // MESSAGE
    Value int
}
type Copy Item
`, "MESSAGE", doc)), Version: version + 1}})
			for _, target := range []string{"Item", "Copy"} {
				properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: target})
				require.NoError(t, err)
				require.Len(t, properties, 1)
				assert.Equal(t, doc, strings.TrimSpace(properties[0].Doc))
				assert.Equal(t, "xgo:main?"+target+".Value", properties[0].Definition.String())
			}
		}
	})

	t.Run("ClassNameConflict", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox":   []byte("var Worker Item\n"),
			"Worker_fixture.gox": []byte("var count int\n"),
		})
		info, err := s.getProj().TypeInfo()
		require.ErrorContains(t, err, "Worker conflicts with class name")
		require.NotNil(t, info)
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Worker"})
		require.NoError(t, err)
		assert.Contains(t, properties, XGoProperty{
			Name: "count", Type: "int", Kind: XGoPropertyKindField,
			Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Worker.count")},
		})
	})

	t.Run("SourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			filename  string
			target    string
			class     bool
			newServer testServerFactory
		}{
			{name: "XGo", filename: "main.xgo", target: "Record", newServer: newTestServer},
			{name: "LegacyXGo", filename: "main.gop", target: "Record", newServer: newTestServer},
			{name: "StandaloneClass", filename: "Record.gox", target: "Record", class: true, newServer: newTestServer},
			{name: "ProjectClass", filename: "main_fixture.gox", target: "App", class: true, newServer: newFrameworkTestServer},
			{name: "WorkClass", filename: "Worker_fixture.gox", target: "Worker", class: true, newServer: newFrameworkTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := `type Record struct {
    score int
    level int
}

func (r *Record) GetScore() int { return r.score }
`
				if tt.class {
					source = `var (
    score int
    level int
)

func GetScore() int { return score }
`
				}
				files := map[string][]byte{tt.filename: []byte(source)}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := tt.newServer(t, files)
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: tt.target})
				require.NoError(t, err)
				want := []XGoProperty{
					{Name: "level", Type: "int", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr(tt.target + ".level")}},
					{Name: "score", Type: "int", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr(tt.target + ".score")}},
					{Name: "getScore", Type: "int", Kind: XGoPropertyKindMethod, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr(tt.target + ".GetScore")}},
				}
				if tt.name == "WorkClass" {
					want = append(want, XGoProperty{
						Name: "label", Type: "string", Kind: XGoPropertyKindMethod,
						Doc:        "Label is exposed as a property in XGo source.\n",
						Definition: XGoDefinitionIdentifier{Package: ToPtr("example.com/framework"), Name: ToPtr("Item.label")},
					})
				}
				assert.Equal(t, want, properties)
			})
		}
	})

	t.Run("Filtering", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`type Count int
type Value struct{}
type List struct{}
type Record struct {
    name string
    enabled bool
    score float64
    count Count
    values []int
    other struct{}
    value Value
    list List
}

func (r *Record) GetScore() float64 { return r.score }
func (r *Record) SetScore(v float64) {}
func (r *Record) WithParam(v float64) float64 { return v }
func (r *Record) Reset() {}
func (r *Record) Pair() (int, int) { return 1, 2 }
func (r *Record) Counts() []int { return nil }
func (r *Record) NamedCount() Count { return 0 }
func (r *Record) CurrentValue() Value { return Value{} }
func (r *Record) CurrentList() List { return List{} }
func (r *Record) hidden() int { return 0 }
func (r *Record) XGo_Internal() int { return 0 }
`)})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
		require.NoError(t, err)
		assert.Equal(t, []XGoProperty{
			{Name: "enabled", Type: "bool", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.enabled")}},
			{Name: "name", Type: "string", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.name")}},
			{Name: "score", Type: "float64", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.score")}},
			{Name: "getScore", Type: "float64", Kind: XGoPropertyKindMethod, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.GetScore")}},
		}, properties)
	})

	t.Run("EmbeddedMembers", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`type Base struct {
    // Score stores the base score.
    Score int
    Shared int
}

// Label describes the base.
func (b *Base) Label() string { return "base" }
func (b *Base) Size() int { return 1 }

type Record struct {
    *Base
    Shared string
}

func (r *Record) Size() string { return "record" }
type RecordAlias = Record
type RecordPointer = *Record
`)})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		for _, target := range []string{"Record", "RecordAlias", "RecordPointer"} {
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: target})
			require.NoError(t, err)
			assert.Equal(t, []XGoProperty{
				{Name: "Score", Type: "int", Kind: XGoPropertyKindField, Doc: "Score stores the base score.\n", Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Base.Score")}},
				{Name: "Shared", Type: "string", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.Shared")}},
				{Name: "label", Type: "string", Kind: XGoPropertyKindMethod, Doc: "Label describes the base.\n", Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Base.Label")}},
				{Name: "size", Type: "string", Kind: XGoPropertyKindMethod, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.Size")}},
			}, properties, target)
		}
	})

	t.Run("ShadowedMembers", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			source    string
			want      *XGoProperty
			newServer testServerFactory
		}{
			{
				name:   "UnsupportedField",
				source: "type Base struct { Keep, Count int }\ntype Record struct { *Base; Count []int }\n",
			},
			{
				name:   "MethodWithParameter",
				source: "type Base struct { Keep int }\nfunc (b *Base) Size() int { return 1 }\ntype Record struct { *Base }\nfunc (r *Record) Size(n int) int { return n }\n",
			},
			{
				name:   "MethodWithoutResult",
				source: "type Base struct { Keep int }\nfunc (b *Base) Size() int { return 1 }\ntype Record struct { *Base }\nfunc (r *Record) Size() {}\n",
			},
			{
				name:   "MethodWithMultipleResults",
				source: "type Base struct { Keep int }\nfunc (b *Base) Size() int { return 1 }\ntype Record struct { *Base }\nfunc (r *Record) Size() (int, int) { return 1, 2 }\n",
			},
			{
				name:   "MethodWithUnsupportedResult",
				source: "type Base struct { Keep int }\nfunc (b *Base) Size() int { return 1 }\ntype Record struct { *Base }\nfunc (r *Record) Size() []int { return nil }\n",
			},
			{
				name:   "FieldShadowsMethod",
				source: "type Base struct { Keep int }\nfunc (b *Base) Size() int { return 1 }\ntype Record struct { *Base; size []int }\n",
			},
			{
				name:   "MethodShadowsField",
				source: "type Base struct { Keep, size int }\ntype Record struct { *Base }\nfunc (r *Record) Size(n int) int { return n }\n",
			},
			{
				name:   "LowercaseMethod",
				source: "type Base struct { Keep int }\nfunc (b *Base) Size() int { return 1 }\ntype Record struct { *Base }\nfunc (r *Record) size() int { return 2 }\n",
			},
			{
				name:   "ExactMethodBeforeAlias",
				source: "type Base struct { Keep int }\ntype Record struct { *Base }\nfunc (r *Record) Size() int { return 1 }\nfunc (r *Record) size(n int) int { return n }\n",
			},
			{
				name:   "EmbeddedField",
				source: "type Count struct{}\ntype Base struct { Keep, Count int }\ntype Record struct { *Base; Count }\n",
			},
			{
				name:   "IntermediateMember",
				source: "type Base struct { Keep, Count int }\ntype Middle struct { *Base; Count []int }\ntype Record struct { *Middle }\n",
			},
			{
				name:   "DifferentCase",
				source: "type Base struct { Keep int }\nfunc (b *Base) Size() int { return 1 }\ntype Record struct { *Base; Size []int }\n",
				want: &XGoProperty{Name: "size", Type: "int", Kind: XGoPropertyKindMethod,
					Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Base.Size")}},
			},
			{
				name: "ImportedPrivateField", newServer: newFrameworkTestServer,
				source: "import f \"example.com/framework\"\ntype Base struct { Keep int }\ntype Record struct { *Base; f.PrivateField }\n",
				want: &XGoProperty{Name: "label", Type: "string", Kind: XGoPropertyKindMethod,
					Doc:        "Label is exposed as a property in XGo source.\n",
					Definition: XGoDefinitionIdentifier{Package: ToPtr("example.com/framework"), Name: ToPtr("Item.label")}},
			},
			{
				name: "ImportedPrivateMethod", newServer: newFrameworkTestServer,
				source: "import f \"example.com/framework\"\ntype Base struct { Keep int }\ntype Record struct { *Base; f.PrivateMethod }\n",
				want: &XGoProperty{Name: "label", Type: "string", Kind: XGoPropertyKindMethod,
					Doc:        "Label is exposed as a property in XGo source.\n",
					Definition: XGoDefinitionIdentifier{Package: ToPtr("example.com/framework"), Name: ToPtr("Item.label")}},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				newServer := tt.newServer
				if newServer == nil {
					newServer = newTestServer
				}
				s := newServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				proj := s.getProj()
				info, err := proj.TypeInfo()
				require.NoError(t, err)
				want := []XGoProperty{{Name: "Keep", Type: "int", Kind: XGoPropertyKindField,
					Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Base.Keep")}}}
				if tt.want != nil {
					want = append(want, *tt.want)
				}
				properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
				require.NoError(t, err)
				assert.Equal(t, want, properties)

				ctx := &completionContext{
					definitionContext: definitionContext{proj: proj, lookupPkgDoc: s.lookupPkgDoc},
					typeInfo:          info, itemSet: newCompletionItemSet(PlainText),
				}
				ctx.collectPropertyNames("Record")
				assert.Len(t, ctx.itemSet.items, len(want))
				for _, property := range want {
					item := completionItemByLabel(ctx.itemSet.items, "\""+property.Name+"\"")
					require.NotNil(t, item)
					data := requireValueAs[*CompletionItemData](t, item.Data)
					assert.Equal(t, property.Definition, *data.Definition)
				}
			})
		}
	})

	t.Run("ImportedDocumentationUpdates", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{"main.xgo": []byte(`import "example.com/framework"
type Record struct { framework.Item }
`)})
		lookupPkgDoc := s.lookupPkgDoc
		for _, missing := range []bool{false, true, false} {
			s.lookupPkgDoc = lookupPkgDoc
			wantDoc := "Label is exposed as a property in XGo source.\n"
			if missing {
				s.lookupPkgDoc = func(string) (*pkgdoc.PkgDoc, error) { return nil, fs.ErrNotExist }
				wantDoc = ""
			}
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
			require.NoError(t, err)
			assert.Equal(t, []XGoProperty{{
				Name: "label", Type: "string", Kind: XGoPropertyKindMethod, Doc: wantDoc,
				Definition: XGoDefinitionIdentifier{Package: ToPtr("example.com/framework"), Name: ToPtr("Item.label")},
			}}, properties)
		}
	})

	t.Run("InvalidTargets", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`type Count int
type Alias = int
var count int
`)})
		for _, tt := range []struct {
			target string
			want   string
		}{
			{target: "Missing", want: `target "Missing" not found`},
			{target: "Count", want: `target "Count" is not a struct type`},
			{target: "Alias", want: `target "Alias" is not a named type`},
			{target: "count", want: `target "count" is not a type`},
		} {
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: tt.target})
			assert.EqualError(t, err, tt.want)
			assert.Nil(t, properties)
		}
	})

	t.Run("DocumentUpdates", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"Record.gox": []byte("var Before int\n")})
		params := XGoGetPropertiesParams{Target: "Record"}
		before, err := s.xgoGetProperties(params)
		require.NoError(t, err)
		assert.Equal(t, []XGoProperty{{
			Name: "Before", Type: "int", Kind: XGoPropertyKindField,
			Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.Before")},
		}}, before)
		s.ModifyFiles([]FileChange{{Path: "Record.gox", Content: []byte("var After string\nAfter = missing\n"), Version: 1}})
		after, err := s.xgoGetProperties(params)
		require.NoError(t, err)
		typeInfo, err := s.workspaceRootFS.TypeInfo()
		require.Error(t, err)
		require.NotNil(t, typeInfo)
		assert.Equal(t, []XGoProperty{{
			Name: "After", Type: "string", Kind: XGoPropertyKindField,
			Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.After")},
		}}, after)
	})

	t.Run("ExecuteCommand", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"Record.gox": []byte("var Count int\n")})
		result, err := s.workspaceExecuteCommand(&ExecuteCommandParams{
			Command: CommandXGoGetProperties, Arguments: []json.RawMessage{json.RawMessage(`{"target":"Record"}`)},
		})
		require.NoError(t, err)
		properties := requireValueAs[[]XGoProperty](t, result)
		assert.Equal(t, []XGoProperty{{
			Name: "Count", Type: "int", Kind: XGoPropertyKindField,
			Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Record.Count")},
		}}, properties)
	})
}

func TestIsPropertyOfEnclosingType(t *testing.T) {
	s := newFrameworkTestServer(t, map[string][]byte{
		"main_fixture.gox": nil,
		"Worker_fixture.gox": []byte(`var (
    x int
    y float64
    values []int
)

func Speed() float64 { return 0 }
func Move(dx, dy int) {}
func hidden() int { return 0 }
func XGo_Internal() int { return 0 }
`),
		"main.xgo": []byte("var value int\nconst Limit = 1\n"),
	})
	typeInfo, err := s.workspaceRootFS.TypeInfo()
	require.NoError(t, err)
	named := requirePropertyTestType(t, typeInfo.Pkg, "Worker")
	for _, tt := range []struct {
		name string
		want bool
	}{
		{name: "x", want: true},
		{name: "y", want: true},
		{name: "values"},
		{name: "Speed", want: true},
		{name: "Move"},
		{name: "hidden"},
		{name: "XGo_Internal"},
		{name: "Value"},
		{name: "Label", want: true},
	} {
		obj := requirePropertyTestMember(t, named, tt.name)
		assert.Equal(t, tt.want, (&definitionContext{proj: s.getProj()}).isPropertyOfEnclosingType(obj), tt.name)
	}
	for _, name := range []string{"value", "Limit", "Worker"} {
		obj := typeInfo.Pkg.Scope().Lookup(name)
		require.NotNil(t, obj)
		assert.False(t, (&definitionContext{proj: s.getProj()}).isPropertyOfEnclosingType(obj), name)
	}
	assert.False(t, (&definitionContext{proj: s.getProj()}).isPropertyOfEnclosingType(nil))
}

func TestMemberTypeName(t *testing.T) {
	t.Run("ClassMembers", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": nil,
			"Worker_fixture.gox": []byte(`var (
    x int
    y float64
)

func Speed() float64 { return 0 }
func Move(dx, dy int) {}
`),
			"Other_fixture.gox": []byte(`var (
    x string
    z bool
)

func Speed() int { return 1 }
func Jump() {}
`),
		})
		typeInfo, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		for _, tt := range []struct {
			name    string
			members []string
		}{
			{name: "Worker", members: []string{"x", "y", "Speed", "Move"}},
			{name: "Other", members: []string{"x", "z", "Speed", "Jump"}},
		} {
			named := requirePropertyTestType(t, typeInfo.Pkg, tt.name)
			for _, name := range tt.members {
				obj := requirePropertyTestMember(t, named, name)
				assert.Equal(t, named.Obj().Name(), memberTypeName(s.getProj(), obj), "%s.%s", tt.name, name)
			}
		}
	})

	t.Run("MixedScopeObjects", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`const Limit = 1
var value int
func helper() {}
type Alias = int
type Count int
type Interface interface { Run() }
type Record struct { x int }
func (r Record) Value() int { return r.x }
func (r *Record) Reset() {}
`)})
		typeInfo, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		named := requirePropertyTestType(t, typeInfo.Pkg, "Record")
		for _, name := range []string{"x", "Value", "Reset"} {
			assert.Equal(t, named.Obj().Name(), memberTypeName(s.getProj(), requirePropertyTestMember(t, named, name)), name)
		}
		for _, name := range []string{"Limit", "value", "helper", "Alias", "Count", "Interface"} {
			obj := typeInfo.Pkg.Scope().Lookup(name)
			require.NotNil(t, obj)
			assert.Empty(t, memberTypeName(s.getProj(), obj), name)
		}
	})

	t.Run("AnonymousFields", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
		}{
			{name: "Variable", source: "var item struct { Count int }\nitem.Count = 1\n"},
			{name: "Nested", source: "type Record struct { Nested struct { Count int } }\nvar item Record\nitem.Nested.Count = 1\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				info, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				var field *gotypes.Var
				for ident, obj := range info.Defs {
					if ident.Name == "Count" {
						field = requireValueAs[*gotypes.Var](t, obj)
					}
				}
				require.NotNil(t, field)
				assert.Empty(t, memberTypeName(s.getProj(), field))
			})
		}
	})

	t.Run("ImportedPositionCollision", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Record struct { Count int }\nvar item Record\nitem.Count = 1\n")})
		info, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		record := requirePropertyTestType(t, info.Pkg, "Record")
		localField := requirePropertyTestMember(t, record, "Count")
		pkg := gotypes.NewPackage("example.com/external", "external")
		field := gotypes.NewField(localField.Pos(), pkg, "Count", gotypes.Typ[gotypes.Int], false)
		named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "External", nil), gotypes.NewStruct([]*gotypes.Var{field}, nil), nil)
		pkg.Scope().Insert(named.Obj())
		assert.Equal(t, "External", memberTypeName(s.getProj(), field))
	})

	t.Run("UnavailableObjects", func(t *testing.T) {
		s := newTestServer(t, nil)
		assert.Empty(t, memberTypeName(s.getProj(), nil))
		pkg := gotypes.NewPackage("example.com/record", "record")
		field := gotypes.NewField(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int], false)
		named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Record", nil), gotypes.NewStruct([]*gotypes.Var{field}, nil), nil)
		pkg.Scope().Insert(named.Obj())
		assert.Equal(t, "Record", memberTypeName(s.getProj(), field))
		assert.Empty(t, memberTypeName(s.getProj(), gotypes.NewField(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int], false)))
		assert.Empty(t, memberTypeName(s.getProj(), gotypes.NewField(token.NoPos, nil, "x", gotypes.Typ[gotypes.Int], false)))
		assert.Empty(t, memberTypeName(s.getProj(), gotypes.NewVar(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int])))
		copyType := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Copy", nil), named.Underlying(), nil)
		pkg.Scope().Insert(copyType.Obj())
		assert.Empty(t, memberTypeName(s.getProj(), field))
	})

	t.Run("UnnamedReceivers", func(t *testing.T) {
		s := newTestServer(t, nil)
		for _, tt := range []struct {
			name string
			typ  gotypes.Type
		}{
			{name: "None"},
			{name: "PointerToBasic", typ: gotypes.NewPointer(gotypes.Typ[gotypes.String])},
			{name: "Interface", typ: gotypes.NewInterfaceType(nil, nil).Complete()},
			{name: "Struct", typ: gotypes.NewStruct(nil, nil)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var recv *gotypes.Var
				if tt.typ != nil {
					recv = gotypes.NewVar(token.NoPos, nil, "recv", tt.typ)
				}
				sig := gotypes.NewSignatureType(recv, nil, nil, nil, nil, false)
				method := gotypes.NewFunc(token.NoPos, nil, "Method", sig)
				assert.Empty(t, memberTypeName(s.getProj(), method))
			})
		}
	})
}

func requirePropertyTestType(t *testing.T, pkg *gotypes.Package, name string) *gotypes.Named {
	t.Helper()

	obj := pkg.Scope().Lookup(name)
	require.NotNil(t, obj, name)
	return requireValueAs[*gotypes.Named](t, obj.Type())
}

func requirePropertyTestMember(t *testing.T, named *gotypes.Named, name string) gotypes.Object {
	t.Helper()

	obj, _, _ := gotypes.LookupFieldOrMethod(named, true, named.Obj().Pkg(), name)
	require.NotNil(t, obj, "%s.%s", named, name)
	return obj
}
