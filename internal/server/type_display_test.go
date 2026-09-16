package server

import (
	gotypes "go/types"
	"html/template"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTypeDisplay(t *testing.T) {
	for _, tt := range []struct {
		name      string
		filename  string
		newServer testServerFactory
		source    string
		want      string
	}{
		{
			name:   "LocalType",
			source: "type Record struct{}\nvar value Record\necho |value\n",
			want:   "Record",
		},
		{
			name:   "ImportAlias",
			source: "import f \"example.com/framework\"\nvar value f.Item\necho |value\n",
			want:   "f.Item",
		},
		{
			name:   "UnsafeAlias",
			source: "import raw \"unsafe\"\nvar value raw.Pointer\necho |value\n",
			want:   "raw.Pointer",
		},
		{
			name:   "UnsafeDotImport",
			source: "import . \"unsafe\"\nvar value Pointer\necho |value\n",
			want:   "Pointer",
		},
		{
			name:   "DotImport",
			source: "import . \"example.com/framework\"\nvar value Item\necho |value\n",
			want:   "Item",
		},
		{
			name:     "Classfile",
			filename: "main_fixture.gox",
			source:   "var value Item\necho |value\n",
			want:     "Item",
		},
		{
			name:      "FrameworkWithSpxExtension",
			filename:  "main.spx",
			newServer: newFrameworkTestServerWithSpxExtension,
			source:    "var value Item\necho |value\n",
			want:      "Item",
		},
		{
			name:     "ClassfileWithAlias",
			filename: "Worker_fixture.gox",
			source:   "import f \"example.com/framework\"\nvar value f.Copy\necho |value\n",
			want:     "Copy",
		},
		{
			name:     "ShadowedImplicitType",
			filename: "main_fixture.gox",
			source:   "import f \"example.com/framework\"\ntype Copy int\nvar value f.Copy\necho |value\n",
			want:     "f.Copy",
		},
		{
			name:     "UnshadowedImplicitType",
			filename: "main_fixture.gox",
			source:   "type Copy int\nvar value Item\necho |value\n",
			want:     "Item",
		},
		{
			name:   "ShadowedDotImport",
			source: "import . \"example.com/framework\"\nfunc run() {\nvar value Item\ntype Item int\necho |value\n}\n",
			want:   "framework.Item",
		},
		{
			name:   "ShadowedImportAlias",
			source: "import f \"example.com/framework\"\nfunc run() {\nvar value f.Item\nf := 1\necho f, |value\n}\n",
			want:   "framework.Item",
		},
		{
			name:   "ShadowedPackageName",
			source: "import \"example.com/framework\"\nfunc run() {\nvar value framework.Item\nframework := 1\necho framework, |value\n}\n",
			want:   "example.com/framework.Item",
		},
		{
			name:   "ShadowedLocalType",
			source: "type Record struct{}\nvar value Record\nfunc run() {\ntype Record int\necho |value\n}\n",
			want:   "main.Record",
		},
		{
			name:   "TypeAlias",
			source: "import f \"example.com/framework\"\ntype Local = f.Item\nvar value Local\necho |value\n",
			want:   "Local",
		},
		{
			name:   "GenericTypeArguments",
			source: "import f \"example.com/framework\"\nvar value f.Box[f.Item]\necho |value\n",
			want:   "f.Box[f.Item]",
		},
		{
			name:   "MapKeyAndValue",
			source: "import f \"example.com/framework\"\nvar value map[f.Item][]*f.Box[f.Copy]\necho |value\n",
			want:   "map[f.Item][]*f.Box[f.Copy]",
		},
		{
			name:   "ChannelsAndArrays",
			source: "import f \"example.com/framework\"\nvar value chan<- [2]f.Item\necho |value\n",
			want:   "chan<- [2]f.Item",
		},
		{
			name:   "Function",
			source: "import f \"example.com/framework\"\nvar value func(f.Item, ...f.Copy) (f.Group, error)\necho |value\n",
			want:   "func(f.Item, ...f.Copy) (f.Group, error)",
		},
		{
			name:   "Struct",
			source: "import f \"example.com/framework\"\nvar value struct { Item f.Item }\necho |value\n",
			want:   "struct{Item f.Item}",
		},
		{
			name:   "Interface",
			source: "import f \"example.com/framework\"\nvar value interface { Accept(f.Item) f.Copy }\necho |value\n",
			want:   "interface{Accept(f.Item) f.Copy}",
		},
		{
			name:     "CompositeShadowing",
			filename: "main_fixture.gox",
			source:   "import f \"example.com/framework\"\ntype Copy int\nvar value map[f.Item]f.Copy\necho |value\n",
			want:     "map[f.Item]f.Copy",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			filename := tt.filename
			if filename == "" {
				filename = "main.xgo"
			}
			source, position := typeDisplayTestSource(t, tt.source)
			files := map[string][]byte{filename: []byte(source)}
			if filename == "Worker_fixture.gox" {
				files["main_fixture.gox"] = nil
			}
			factory := tt.newServer
			if factory == nil {
				factory = newFrameworkTestServer
			}
			s := factory(t, files)
			proj := s.getProj()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile(filename)
			require.NoError(t, err)
			_, obj, _ := objectAtPosition(proj, info, file, ToPosition(proj, file, position))
			require.NotNil(t, obj)
			display := newTypeDisplay(proj, file, PosAt(proj, file, position))
			assert.Equal(t, tt.want, display.typeString(obj.Type()))

			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `overview="`+template.HTMLEscapeString("var value "+tt.want)+`"`)
		})
	}
}

func typeDisplayTestSource(t *testing.T, source string) (string, Position) {
	t.Helper()

	require.Equal(t, 1, strings.Count(source, "|"))
	before, after, _ := strings.Cut(source, "|")
	return before + after, Position{
		Line:      uint32(strings.Count(before, "\n")),
		Character: uint32(UTF16Len(before[strings.LastIndex(before, "\n")+1:])),
	}
}

func TestTypeDisplaySourceContexts(t *testing.T) {
	s := newFrameworkTestServer(t, map[string][]byte{
		"first.xgo":        []byte("import f \"example.com/framework\"\nfunc first() {\n f.use(nil)\n}\n"),
		"second.xgo":       []byte("import g \"example.com/framework\"\nfunc second() {\n g.use(nil)\n}\n"),
		"main_fixture.gox": []byte("onStart => {\n\n use(nil)\n}\n"),
	})
	for _, step := range []struct {
		filename string
		alias    string
		change   bool
	}{
		{filename: "first.xgo", alias: "f"},
		{filename: "second.xgo", alias: "g"},
		{filename: "main_fixture.gox"},
		{filename: "first.xgo", alias: "f"},
		{filename: "first.xgo", alias: "renamed", change: true},
		{filename: "second.xgo", alias: "g"},
	} {
		if step.change {
			s.ModifyFiles([]FileChange{{Path: step.filename, Content: []byte("import renamed \"example.com/framework\"\nfunc first() {\n renamed.use(nil)\n}\n"), Version: 1}})
		}
		prefix := step.alias
		line, start := uint32(2), uint32(1)
		if prefix != "" {
			prefix += "."
			start += uint32(len(prefix))
		}
		uri := s.toDocumentURI(step.filename)
		position := Position{Line: line, Character: start}
		hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri}, Position: position,
		}})
		require.NoError(t, err)
		require.NotNil(t, hover)
		wantSignature := "use(item *" + prefix + "Item) (*" + prefix + "Item, bool)"
		assert.Contains(t, hover.Contents.Value, `overview="func `+wantSignature+`"`)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:example.com/framework?use"`)

		position.Character += 4
		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri}, Position: position,
		}})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, wantSignature, help.Signatures[0].Label)
		require.NotNil(t, help.Signatures[0].Documentation)
		assert.Equal(t, "Use accepts and returns a work item.", help.Signatures[0].Documentation.Value)
		assert.Contains(t, hover.Contents.Value, "Use accepts and returns a work item.")

		completionPosition := Position{Line: line, Character: start}
		if step.alias == "" {
			completionPosition = Position{Line: 1}
		}
		items := completionItemsAt(t, s, step.filename, completionPosition)
		for _, want := range []struct{ label, overview, id, detail string }{
			{label: "use", overview: "func " + wantSignature, id: "use", detail: "Use accepts and returns a work item."},
			{label: "Current", overview: "var Current *" + prefix + "Item", id: "Current", detail: "Current is the current work item."},
			{label: "ItemRef", overview: "type ItemRef = *" + prefix + "Item", id: "ItemRef", detail: "ItemRef aliases a work item pointer."},
			{label: "Limit", overview: "const Limit " + prefix + "Level = 3", id: "Limit", detail: "Limit is the default work level."},
		} {
			item := completionItemByLabel(items, want.label)
			require.NotNil(t, item, step.filename, want.label)
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
			assert.Contains(t, doc.Value, `overview="`+want.overview+`"`)
			assert.Contains(t, doc.Value, want.detail)
			data := requireValueAs[*CompletionItemData](t, item.Data)
			assert.Equal(t, "xgo:example.com/framework?"+want.id, data.Definition.String())
		}
	}
}

func TestTypeDisplayProperties(t *testing.T) {
	s := newFrameworkTestServer(t, map[string][]byte{
		"first.xgo":        []byte("import f \"example.com/framework\"\ntype Record struct { Value f.Count }\nfunc (r *Record) Current() f.Count { return r.Value }\n"),
		"second.xgo":       []byte("import g \"example.com/framework\"\ntype Other struct { Value g.Count }\n"),
		"main_fixture.gox": []byte("var value Count\nfunc CurrentItem() Count { return value }\n"),
	})
	for _, tt := range []struct{ target, prefix string }{
		{target: "Record", prefix: "f."}, {target: "Other", prefix: "g."}, {target: "App"},
	} {
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: tt.target})
		require.NoError(t, err)
		require.NotEmpty(t, properties)
		for _, property := range properties {
			assert.Equal(t, tt.prefix+"Count", property.Type, tt.target+"."+property.Name)
		}
	}
}

func TestTypeDisplayUnits(t *testing.T) {
	source, position := typeDisplayTestSource(t, "import clock \"time\"\nfunc wait(d clock.Duration) {}\nwait 1m|s\n")
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
	}})
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Contains(t, hover.Contents.Value, "for `clock.Duration`")
	result, err := s.textDocumentCompletion(&CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
	}})
	require.NoError(t, err)
	items := requireValueAs[CompletionList](t, result).Items
	item := completionItemByLabel(items, "ms")
	require.NotNil(t, item)
	assert.Equal(t, "clock.Duration", item.Detail)
}

func TestTypeDisplayPackageIdentity(t *testing.T) {
	for _, tt := range []struct {
		name      string
		imports   string
		implicit  bool
		wantFirst string
		wantOther string
	}{
		{name: "ExplicitAliases", imports: "import first \"example.com/framework\"\nimport other \"example.com/other\"\n", wantFirst: "first.Item", wantOther: "other.Item"},
		{name: "ImplicitCollision", implicit: true, wantFirst: "example.com/framework.Item", wantOther: "example.com/other.Item"},
		{name: "ImplicitCollisionWithAlias", imports: "import other \"example.com/other\"\n", implicit: true, wantFirst: "example.com/framework.Item", wantOther: "other.Item"},
		{name: "PackageAbsentFromFile", imports: "import first \"example.com/framework\"\n", wantFirst: "first.Item", wantOther: "example.com/other.Item"},
		{name: "BlankImport", imports: "import _ \"example.com/other\"\n", wantFirst: "framework.Item", wantOther: "framework.Item"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			filename := "main.xgo"
			mod := testframework.NewModule(t)
			if tt.implicit {
				filename = "main_fixture.gox"
				mod.Opt.Projects[0].PkgPaths = append(mod.Opt.Projects[0].PkgPaths, "example.com/other")
				require.NoError(t, mod.ImportClasses())
			}
			s := newFrameworkTestServerWithModule(t, map[string][]byte{filename: []byte(tt.imports + "echo 1\n")}, mod)
			proj := s.getProj()
			first, err := proj.Importer.Import(testframework.PkgPath)
			require.NoError(t, err)
			other := gotypes.NewPackage("example.com/other", "framework")
			item := gotypes.NewTypeName(token.NoPos, other, "Item", nil)
			gotypes.NewNamed(item, gotypes.NewStruct(nil, nil), nil)
			other.Scope().Insert(item)
			other.MarkComplete()
			baseImporter := proj.Importer
			proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == other.Path() {
					return other, nil
				}
				return baseImporter.Import(path)
			})
			_, err = proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile(filename)
			require.NoError(t, err)
			display := newTypeDisplay(proj, file, file.End()-1)
			assert.Equal(t, tt.wantFirst, display.typeString(first.Scope().Lookup("Item").Type()))
			assert.Equal(t, tt.wantOther, display.typeString(item.Type()))
			if tt.name == "BlankImport" {
				both := gotypes.NewMap(first.Scope().Lookup("Item").Type(), item.Type())
				assert.Equal(t, "map[example.com/framework.Item]example.com/other.Item", display.typeString(both))
			}
		})
	}
}

func TestTypeDisplayDistinctPackage(t *testing.T) {
	s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte("echo 1\n")})
	proj := s.getProj()
	_, err := proj.TypeInfo()
	require.NoError(t, err)
	file, err := proj.ASTFile("main_fixture.gox")
	require.NoError(t, err)
	pkg, err := proj.Importer.Import(testframework.PkgPath)
	require.NoError(t, err)
	display := newTypeDisplay(proj, file, file.End()-1)
	assert.Equal(t, "Item", display.typeString(pkg.Scope().Lookup("Item").Type()))

	other := gotypes.NewPackage(pkg.Path(), pkg.Name())
	item := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, other, "Item", nil), gotypes.NewStruct(nil, nil), nil)
	assert.Equal(t, "example.com/framework.Item", display.typeString(item))
}

func TestDisplayedTypeNames(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/types", "types")
	obj := gotypes.NewTypeName(token.NoPos, pkg, "Number", nil)
	named := gotypes.NewNamed(obj, gotypes.Typ[gotypes.Int], nil)
	constraint := gotypes.NewInterfaceType(nil, []gotypes.Type{gotypes.NewUnion([]*gotypes.Term{
		gotypes.NewTerm(false, named), gotypes.NewTerm(true, gotypes.Typ[gotypes.String]),
	})}).Complete()
	param := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "T", nil), constraint)
	sig := gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{param}, gotypes.NewTuple(gotypes.NewVar(token.NoPos, pkg, "value", param)), nil, false)
	assert.Equal(t, []*gotypes.TypeName{obj}, slices.Collect(displayedTypeNames(sig)))

	genericObj := gotypes.NewTypeName(token.NoPos, pkg, "Box", nil)
	generic := gotypes.NewNamed(genericObj, gotypes.NewStruct(nil, nil), nil)
	generic.SetTypeParams([]*gotypes.TypeParam{gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "T", nil), constraint)})
	assert.Equal(t, []*gotypes.TypeName{genericObj, obj}, slices.Collect(displayedTypeNames(generic)))

	// Names behind recursive type declarations are not printed or expanded.
	recursive := gotypes.NewTypeName(token.NoPos, pkg, "Node", nil)
	node := gotypes.NewNamed(recursive, nil, nil)
	node.SetUnderlying(gotypes.NewStruct([]*gotypes.Var{gotypes.NewField(token.NoPos, pkg, "Next", gotypes.NewPointer(node), false)}, nil))
	assert.Equal(t, []*gotypes.TypeName{recursive}, slices.Collect(displayedTypeNames(node)))
	var visits int
	for range displayedTypeNames(gotypes.NewMap(named, node)) {
		visits++
		break
	}
	assert.Equal(t, 1, visits)
}

func TestTypeDisplayTypeString(t *testing.T) {
	for _, tt := range []struct {
		name string
		pkg  *gotypes.Package
		want string
	}{
		{name: "NoPackage", want: "Value"},
		{name: "OtherPackage", pkg: gotypes.NewPackage("example.com/sample", "sample"), want: "sample.Value"},
		{name: "OtherPackageNamedSpx", pkg: gotypes.NewPackage("example.com/spx", "spx"), want: "spx.Value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, tt.pkg, "Value", nil), gotypes.NewStruct(nil, nil), nil)
			assert.Equal(t, tt.want, (typeDisplay{}).typeString(named))
		})
	}
}

func TestTypeDisplayOverloadSignatures(t *testing.T) {
	source, position := typeDisplayTestSource(t, "import client \"example.com/framework\"\nhandle |missing\n")
	s := newFrameworkTestServer(t, map[string][]byte{
		"main.xgo": []byte(source),
		"functions.xgo": []byte(`import original "example.com/framework"
func item(value original.Item) {}
func copy(value original.Copy) {}
func handle = (
    item
    copy
)
`),
	})
	_, err := s.getProj().TypeInfo()
	require.ErrorContains(t, err, "undefined: missing")
	help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
	}})
	require.NoError(t, err)
	require.NotNil(t, help)
	assert.Equal(t, []SignatureInformation{
		{Label: "handle(value client.Item)", Parameters: []ParameterInformation{{Label: "value client.Item"}}},
		{Label: "handle(value client.Copy)", Parameters: []ParameterInformation{{Label: "value client.Copy"}}},
	}, help.Signatures)
}

func TestTypeDisplayDefinitions(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
		source   string
		label    string
		overview string
		doc      string
	}{
		{
			name: "LocalAlias", source: "import f \"example.com/framework\"\n// Handle names an item.\ntype Handle = *f.Item\nvar value |Handle\n",
			label: "Handle", overview: "type Handle = *f.Item", doc: "Handle names an item.",
		},
		{
			name: "ImportedAlias", source: "import f \"example.com/framework\"\nvar value f.|ItemRef\n",
			label: "ItemRef", overview: "type ItemRef = *f.Item", doc: "ItemRef aliases a work item pointer.",
		},
		{
			name: "ImplicitAlias", filename: "main_fixture.gox", source: "var value |ItemRef\n",
			label: "ItemRef", overview: "type ItemRef = *Item", doc: "ItemRef aliases a work item pointer.",
		},
		{
			name: "ShadowedAliasTarget", filename: "main_fixture.gox", source: "type Item int\nvar value |ItemRef\n",
			label: "ItemRef", overview: "type ItemRef = *framework.Item", doc: "ItemRef aliases a work item pointer.",
		},
		{
			name: "GenericType", source: "import f \"example.com/framework\"\nvar value f.|Box[int]\n",
			label: "Box", overview: "type Box[T any]", doc: "Box provides an instantiated field for member lookup tests.",
		},
		{
			name: "GenericAlias", source: "import f \"example.com/framework\"\nvar value f.|Table[string, int]\n",
			label: "Table", overview: "type Table[K comparable, V any] = map[K]V", doc: "Table names a generic map.",
		},
		{
			name: "LocalTypedConst", source: "type Level int\n// Limit is documented.\nconst Limit Level = 3\necho |Limit\n",
			label: "Limit", overview: "const Limit Level = 3", doc: "Limit is documented.",
		},
		{
			name: "ImportedTypedConst", source: "import f \"example.com/framework\"\necho f.|Limit\n",
			label: "Limit", overview: "const Limit f.Level = 3", doc: "Limit is the default work level.",
		},
		{
			name: "ImplicitTypedConst", filename: "main_fixture.gox", source: "echo |Limit\n",
			label: "Limit", overview: "const Limit Level = 3", doc: "Limit is the default work level.",
		},
		{
			name: "UntypedConst", source: "// Limit is documented.\nconst Limit = 3\necho |Limit\n",
			label: "Limit", overview: "const Limit = 3", doc: "Limit is documented.",
		},
		{
			name: "BasicTypedConst", source: "// Limit is documented.\nconst Limit int = 3\necho |Limit\n",
			label: "Limit", overview: "const Limit int = 3", doc: "Limit is documented.",
		},
		{
			name: "AnyTypedConst", source: "// Limit is documented.\nconst Limit any = 3\necho |Limit\n",
			label: "Limit", overview: "const Limit interface{} = 3", doc: "Limit is documented.",
		},
		{
			name: "InterfaceTypedConst", source: "// Limit is documented.\nconst Limit interface{} = 3\necho |Limit\n",
			label: "Limit", overview: "const Limit interface{} = 3", doc: "Limit is documented.",
		},
		{
			name: "NamedInterfaceTypedConst", source: "type Value interface{}\n// Limit is documented.\nconst Limit Value = 3\necho |Limit\n",
			label: "Limit", overview: "const Limit Value = 3", doc: "Limit is documented.",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			filename := tt.filename
			if filename == "" {
				filename = "main.xgo"
			}
			source, position := typeDisplayTestSource(t, tt.source)
			s := newFrameworkTestServer(t, map[string][]byte{filename: []byte(source)})
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `overview="`+template.HTMLEscapeString(tt.overview)+`"`)
			assert.Contains(t, hover.Contents.Value, tt.doc)

			completionPosition := position
			completionPosition.Character++
			item := completionItemByLabel(completionItemsAt(t, s, filename, completionPosition), tt.label)
			require.NotNil(t, item)
			require.NotNil(t, item.Documentation)
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
			assert.Contains(t, doc.Value, `overview="`+template.HTMLEscapeString(tt.overview)+`"`)
			assert.Contains(t, doc.Value, tt.doc)
		})
	}
}
