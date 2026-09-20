package server

import (
	"go/constant"
	gotypes "go/types"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (s *Server) renameResourceAtRefs(t testing.TB, result *resourceAnalysis, id resourceID, newName string) map[DocumentURI][]TextEdit {
	t.Helper()

	changes, err := s.renameResourcesAtRefs(s.getProj(), result, map[resourceID]string{id: newName})
	require.NoError(t, err)
	return changes
}

func TestServerRenameResourceAtRefs(t *testing.T) {
	t.Run("DefinedConstantDependency", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
		}{
			{"Suffix", "const derived = base + \"Suffix\"\necho derived\n"},
			{"Prefix", "const derived = \"Prefix\" + base\necho derived\n"},
			{"MultipleUses", "const derived = base + base\necho derived\n"},
			{"Conversion", "const derived = Asset(base + \"Suffix\")\necho derived\n"},
			{"Comparison", "const derived = base == \"Studio\"\necho derived\n"},
			{"Repeated", "const (\nfirst = base + string('0' + iota)\nsecond\n)\necho first, second\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const prefix = "type Asset string\nfunc use(value Asset) {}\nconst base Asset = \"Studio\"\nuse base\nuse \"Other\"\n"
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(prefix + tt.source)})
				proj := s.getProj()
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				result := newTestResourceAnalysis()
				for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
					result.addResourceRef(ref)
				}
				changes, err := s.renameResourcesAtRefs(s.getProj(), result, map[resourceID]string{
					testResourceID{"files", "Studio"}: "Park",
					testResourceID{"files", "Other"}:  "Changed",
				})
				require.ErrorContains(t, err, "cannot preserve a derived constant")
				assert.Nil(t, changes)
			})
		}
	})

	t.Run("ClassMemberShadowsConversionType", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			importLine   string
			shadowImport bool
			wantType     string
		}{
			{"Implicit", "", false, ""},
			{"Qualified", "import f \"example.com/framework\"\n", false, "f.AssetName"},
			{"ShadowedImport", "import f \"example.com/framework\"\n", true, ""},
			{"AlternateImport", "import f \"example.com/framework\"\nimport g \"example.com/framework\"\n", true, "g.AssetName"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				newServer := func(source string) *Server {
					t.Helper()

					s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
					pkg, err := s.getProj().Importer.Import(testframework.PkgPath)
					require.NoError(t, err)
					typ := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "AssetName", nil), gotypes.Typ[gotypes.String], nil)
					pkg.Scope().Insert(typ.Obj())
					app := requireValueAs[*gotypes.Named](t, pkg.Scope().Lookup("App").Type())
					sig := gotypes.NewSignatureType(gotypes.NewVar(token.NoPos, pkg, "app", gotypes.NewPointer(app)), nil, nil, nil, gotypes.NewTuple(gotypes.NewVar(token.NoPos, pkg, "", gotypes.Typ[gotypes.String])), false)
					app.AddMethod(gotypes.NewFunc(token.NoPos, pkg, "AssetName", sig))
					if tt.shadowImport {
						app.AddMethod(gotypes.NewFunc(token.NoPos, pkg, "F", sig))
					}
					return s
				}
				source := tt.importLine + "const asset AssetName = \"Studio\"\nfunc other(value any) {}\nother asset\n"
				s := newServer(source)
				proj := s.getProj()
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				result := newTestResourceAnalysis()
				for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
					if value.Call != nil {
						return testResourceID{"other", value.Name}, true
					}
					return testResourceID{"files", value.Name}, true
				}) {
					result.addResourceRef(ref)
				}
				require.Len(t, result.resourceRefs, 2)
				changes, err := s.renameResourcesAtRefs(s.getProj(), result, map[resourceID]string{testResourceID{"files", "Studio"}: "Park"})
				if tt.wantType == "" {
					require.ErrorContains(t, err, "cannot preserve resource constant type")
					assert.Nil(t, changes)
					return
				}
				require.NoError(t, err)
				updated := applyResourceRenameTestEdits(t, source, changes["file:///main_fixture.gox"])
				assert.Contains(t, updated, `other `+tt.wantType+`("Studio")`)
				_, err = newServer(updated).getProj().TypeInfo()
				require.NoError(t, err)
			})
		}
	})

	t.Run("DerivedConstant", func(t *testing.T) {
		const source = "type Asset string\nconst asset = Asset(\"Studio\") + \"Suffix\"\necho asset\n"
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		proj := s.getProj()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		result := newTestResourceAnalysis()
		for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
			result.addResourceRef(ref)
		}
		require.Len(t, result.resourceRefs, 1)
		changes, err := s.renameResourcesAtRefs(s.getProj(), result, map[resourceID]string{testResourceID{"files", "Studio"}: "Park"})
		require.ErrorContains(t, err, "cannot rename a resource within a derived constant")
		assert.Nil(t, changes)
	})

	t.Run("RepeatedRenameWithDefinedTypes", func(t *testing.T) {
		const declarations = "type Asset string\ntype OtherAsset = Asset\nfunc use(value Asset) {}\nfunc other(value OtherAsset) {}\n"
		source := "const asset Asset = \"Studio\"\nuse asset\nother asset\n"
		for _, step := range []struct {
			collection string
			oldName    string
			newName    string
			want       string
		}{
			{"files", "Studio", "Park", "const asset Asset = \"Park\"\nuse asset\nother Asset(\"Studio\")\n"},
			{"scenes", "Studio", "Evening", "const asset Asset = \"Park\"\nuse asset\nother Asset(\"Evening\")\n"},
		} {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte(declarations)})
			proj := s.getProj()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			assetType := info.Pkg.Scope().Lookup("Asset").Type()
			otherType := info.Pkg.Scope().Lookup("OtherAsset").Type()
			result := newTestResourceAnalysis(testResourceID{step.collection, step.oldName})
			for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
				switch value.Type {
				case assetType:
					return testResourceID{"files", value.Name}, true
				case otherType:
					return testResourceID{"scenes", value.Name}, true
				}
				return nil, false
			}) {
				result.addResourceRef(ref)
			}
			require.Len(t, result.resourceRefs, 3)
			links := result.resourceDocumentLinks(s.getProj(), "main.xgo")
			require.NotEmpty(t, links)
			changes := s.renameResourceAtRefs(t, result, testResourceID{step.collection, step.oldName}, step.newName)
			require.Len(t, changes, 1)
			source = applyResourceRenameTestEdits(t, source, changes["file:///main.xgo"])
			assert.Equal(t, step.want, source)
		}
		_, err := newTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte(declarations)}).getProj().TypeInfo()
		require.NoError(t, err)
	})

	t.Run("ImportedConversionType", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			declarations string
			source       string
			wantType     string
		}{
			{"MissingImport", "const asset names.Name = \"Studio\"\n", "other asset\n", ""},
			{"ImportAlias", "const asset names.Name = \"Studio\"\n", "import local \"example.com/names\"\nother asset\n", "local.Name"},
			{"DotImport", "const asset names.Name = \"Studio\"\n", "import . \"example.com/names\"\nother asset\n", "Name"},
			{"LocalAlias", "type Asset = names.Name\nconst asset Asset = \"Studio\"\n", "other asset\n", "Asset"},
			{"PrivateTypeAlias", "const asset names.Public = \"Studio\"\n", "import local \"example.com/names\"\nother asset\n", "local.Public"},
			{"ShadowedImport", "const asset names.Name = \"Studio\"\n", "import names \"example.com/names\"\nfunc run() {\nnames := 1\nother asset\necho names\n}\nrun()\n", ""},
		} {
			t.Run(tt.name, func(t *testing.T) {
				newServer := func(files map[string][]byte) *Server {
					t.Helper()

					s := newTestServer(t, files)
					pkg := gotypes.NewPackage("example.com/names", "names")
					for _, name := range []string{"Name", "private"} {
						named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, name, nil), gotypes.Typ[gotypes.String], nil)
						pkg.Scope().Insert(named.Obj())
					}
					alias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "Public", nil), pkg.Scope().Lookup("private").Type())
					pkg.Scope().Insert(alias.Obj())
					pkg.MarkComplete()
					base := s.getProj().Importer
					s.getProj().Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
						if path == pkg.Path() {
							return pkg, nil
						}
						return base.Import(path)
					})
					return s
				}
				declarations := "import names \"example.com/names\"\n" + tt.declarations + "func other(value any) {}\n"
				files := map[string][]byte{"main.xgo": []byte(tt.source), "types.xgo": []byte(declarations)}
				s := newServer(files)
				proj := s.getProj()
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				result := newTestResourceAnalysis()
				for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
					if value.Call != nil {
						return testResourceID{"other", value.Name}, true
					}
					return testResourceID{"files", value.Name}, true
				}) {
					result.addResourceRef(ref)
				}
				require.Len(t, result.resourceRefs, 2)
				changes, err := s.renameResourcesAtRefs(s.getProj(), result, map[resourceID]string{testResourceID{"files", "Studio"}: "Park"})
				if tt.wantType == "" {
					require.ErrorContains(t, err, "cannot preserve resource constant type")
					assert.Nil(t, changes)
					return
				}
				require.NoError(t, err)
				require.Len(t, changes, 2)
				for filename, source := range files {
					files[filename] = []byte(applyResourceRenameTestEdits(t, string(source), changes[s.toDocumentURI(filename)]))
				}
				assert.Contains(t, string(files["main.xgo"]), tt.wantType+"(\"Studio\")")
				_, err = newServer(files).getProj().TypeInfo()
				require.NoError(t, err)
			})
		}
	})

	t.Run("ShadowedConversionType", func(t *testing.T) {
		const source = "type Asset string\nconst asset Asset = \"Studio\"\nfunc other(value any) {}\nfunc run() {\ntype Asset int\nother asset\n}\nrun()\n"
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		proj := s.getProj()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		result := newTestResourceAnalysis()
		for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
			if _, ok := value.Expr.(*ast.Ident); ok {
				return testResourceID{"other", value.Name}, true
			}
			return testResourceID{"files", value.Name}, true
		}) {
			result.addResourceRef(ref)
		}
		require.Len(t, result.resourceRefs, 2)
		changes, err := s.renameResourcesAtRefs(s.getProj(), result, map[resourceID]string{testResourceID{"files", "Studio"}: "Park"})
		require.ErrorContains(t, err, "cannot preserve resource constant type")
		assert.Nil(t, changes)
	})

	t.Run("SharedConstants", func(t *testing.T) {
		const declarations = "type Asset = string\ntype OtherAsset = string\nfunc use(name Asset) {}\nfunc other(name OtherAsset) {}\nfunc marked(note string, name Asset) {}\ntype Defined string\ntype OtherDefined = Defined\nfunc useDefined(name Defined) {}\nfunc otherDefined(name OtherDefined) {}\n"
		for _, tt := range []struct {
			name   string
			source string
			want   string
		}{
			{
				name:   "OtherResource",
				source: "const name = \"Shared\"\nuse name\nother name\n",
				want:   "const name = \"Shared\"\nuse \"Renamed\"\nother name\n",
			},
			{
				name:   "TypedInitializer",
				source: "const name Asset = \"Shared\"\nuse name\nother name\n",
				want:   "const name Asset = \"Renamed\"\nuse name\nother \"Shared\"\n",
			},
			{
				name:   "DefinedType",
				source: "const name Defined = \"Shared\"\nuseDefined name\notherDefined name\n",
				want:   "const name Defined = \"Renamed\"\nuseDefined name\notherDefined Defined(\"Shared\")\n",
			},
			{
				name:   "OrdinaryUse",
				source: "const name = \"Shared\"\nuse name\nprintln name\n",
				want:   "const name = \"Shared\"\nuse \"Renamed\"\nprintln name\n",
			},
			{
				name:   "SharedExpression",
				source: "const name = \"Sha\" + \"red\"\nuse name\nother name\n",
				want:   "const name = \"Sha\" + \"red\"\nuse \"Renamed\"\nother name\n",
			},
			{
				name:   "ConstantDependency",
				source: "const base Asset = \"Shared\"\nconst name = base\nuse base\nother name\n",
				want:   "const base Asset = \"Renamed\"\nconst name = base\nuse base\nother \"Shared\"\n",
			},
			{
				name:   "ExpressionDependency",
				source: "const base = \"Shared\"\nconst name = base + \"Suffix\"\nuse base\nother name\n",
				want:   "const base = \"Shared\"\nconst name = base + \"Suffix\"\nuse \"Renamed\"\nother name\n",
			},
			{
				name:   "IndependentConstant",
				source: "const base Asset = \"Shared\"\nconst name = base\nuse name\nother base\n",
				want:   "const base Asset = \"Renamed\"\nconst name = base\nuse name\nother \"Shared\"\n",
			},
			{
				name:   "RepeatedInitializer",
				source: "const (\nbase Asset = \"Shared\"\nname\n)\nuse base\nother name\n",
				want:   "const (\nbase Asset = \"Renamed\"\nname\n)\nuse base\nother \"Shared\"\n",
			},
			{
				name:   "MultipleNames",
				source: "const base, name = \"Other\", \"Shared\"\nuse name\nother name\n",
				want:   "const base, name = \"Other\", \"Shared\"\nuse \"Renamed\"\nother name\n",
			},
			{
				name:   "PrivateConstant",
				source: "const name = \"Shared\"\nuse name\nuse name\n",
				want:   "const name = \"Renamed\"\nuse name\nuse name\n",
			},
			{
				name:   "ShadowedName",
				source: "const name = \"Shared\"\nfunc run() {\nconst name = \"Shared\"\nother(name)\n}\nuse name\nrun()\n",
				want:   "const name = \"Renamed\"\nfunc run() {\nconst name = \"Shared\"\nother(name)\n}\nuse name\nrun()\n",
			},
			{
				name:   "PhysicalUTF16Range",
				source: "const name = \"Shared\"\n//line virtual.xgo:100:20\nmarked \"\U0001f600\", ((name))\nother name\n",
				want:   "const name = \"Shared\"\n//line virtual.xgo:100:20\nmarked \"\U0001f600\", ((\"Renamed\"))\nother name\n",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(tt.source), "types.xgo": []byte(declarations),
				})
				proj := s.getProj()
				info, err := proj.TypeInfo()
				require.NoError(t, err)
				assetType := info.Pkg.Scope().Lookup("Asset").Type()
				otherType := info.Pkg.Scope().Lookup("OtherAsset").Type()
				definedType := info.Pkg.Scope().Lookup("Defined").Type()
				otherDefinedType := info.Pkg.Scope().Lookup("OtherDefined").Type()
				result := newTestResourceAnalysis()
				for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
					switch value.Type {
					case assetType, definedType:
						return testResourceID{"files", value.Name}, true
					case otherType, otherDefinedType:
						return testResourceID{"scenes", value.Name}, true
					}
					return nil, false
				}) {
					result.addResourceRef(ref)
				}
				changes := s.renameResourceAtRefs(t, result, testResourceID{"files", "Shared"}, "Renamed")
				require.Len(t, changes, 1)
				updated := applyResourceRenameTestEdits(t, tt.source, changes["file:///main.xgo"])
				assert.Equal(t, tt.want, updated)
				updatedProj := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(updated), "types.xgo": []byte(declarations),
				}).getProj()
				_, err = updatedProj.TypeInfo()
				require.NoError(t, err)
			})
		}
	})

	t.Run("ParenthesizedConstantReferences", func(t *testing.T) {
		const source = "type Asset string\nconst asset Asset = ((\"Studio\"))\nfunc use(value Asset) {}\nuse asset\n"
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		proj := s.getProj()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		result := newTestResourceAnalysis()
		for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
			result.addResourceRef(ref)
		}
		require.Len(t, result.resourceRefs, 2)
		changes := s.renameResourceAtRefs(t, result, testResourceID{"files", "Studio"}, "Park")
		require.Len(t, changes, 1)
		edits := changes["file:///main.xgo"]
		require.Len(t, edits, 1)
		edit := edits[0]
		start := PositionOffset([]byte(source), edit.Range.Start)
		end := PositionOffset([]byte(source), edit.Range.End)
		updated := source[:start] + edit.NewText + source[end:]
		assert.Equal(t, strings.ReplaceAll(source, "Studio", "Park"), updated)
		updatedProj := newTestServer(t, map[string][]byte{"main.xgo": []byte(updated)}).getProj()
		_, err = updatedProj.TypeInfo()
		require.NoError(t, err)
	})

	t.Run("LiteralsAndConstants", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":      []byte("echo \"\U0001f600\", \"Studio\", Scene, TypedScene, Scene, \"Other\", PairScene\n"),
			"resources.xgo": []byte("const Scene = \"Studio\"\nconst TypedScene string = \"Studio\"\nconst OtherScene, PairScene = \"Other\", \"Studio\"\n"),
		})
		proj := s.getProj()
		call := resourceTestCall(t, proj, "main.xgo")
		require.Len(t, call.Args, 7)
		id := testResourceID{"scenes", "Studio"}
		result := newTestResourceAnalysis()
		result.resourceRefs = []resourceRef{
			{ID: id, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[1]},
			{ID: id, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[1]},
			{ID: id, Kind: XGoResourceRefKindConstantReference, Node: call.Args[2]},
			{ID: id, Kind: XGoResourceRefKindConstantReference, Node: call.Args[3]},
			{ID: id, Kind: XGoResourceRefKindConstantReference, Node: call.Args[4]},
			{ID: testResourceID{"scenes", "Other"}, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[5]},
			{ID: id, Kind: XGoResourceRefKindConstantReference, Node: call.Args[6]},
		}
		want := map[DocumentURI][]TextEdit{
			"file:///main.xgo": {{Range: Range{Start: Position{Character: 12}, End: Position{Character: 18}}, NewText: "Park"}},
			"file:///resources.xgo": {
				{Range: Range{Start: Position{Character: 15}, End: Position{Character: 21}}, NewText: "Park"},
				{Range: Range{Start: Position{Line: 1, Character: 27}, End: Position{Line: 1, Character: 33}}, NewText: "Park"},
				{Range: Range{Start: Position{Line: 2, Character: 40}, End: Position{Line: 2, Character: 46}}, NewText: "Park"},
			},
		}
		assert.Equal(t, want, s.renameResourceAtRefs(t, result, id, "Park"))
		assert.Empty(t, s.renameResourceAtRefs(t, result, testResourceID{"clips", "Studio"}, "Park"))
		assert.Equal(t, want, s.renameResourceAtRefs(t, result, id, "Park"))
	})

	t.Run("ConstantInitializers", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			want   TextEdit
		}{
			{
				name: "DefinedStringType", source: "type Name string\nconst Scene Name = \"Studio\"\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Line: 1, Character: 20}, End: Position{Line: 1, Character: 26}}, NewText: "Park"},
			},
			{
				name: "Alias", source: "const Base = \"Studio\"\nconst Scene = Base\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Line: 1, Character: 14}, End: Position{Line: 1, Character: 18}}, NewText: `"Park"`},
			},
			{
				name: "Concatenation", source: "const Scene = \"Stu\" + \"dio\"\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Character: 14}, End: Position{Character: 27}}, NewText: `"Park"`},
			},
			{
				name: "RawConcatenation", source: "const Scene = \"Stu\" + `d\rio`\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Character: 14}, End: Position{Character: 28}}, NewText: `"Park"`},
			},
			{
				name: "Parentheses", source: "const Scene = (\"Studio\")\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Character: 16}, End: Position{Character: 22}}, NewText: "Park"},
			},
			{
				name: "RawString", source: "const Scene = `Studio`\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Character: 15}, End: Position{Character: 21}}, NewText: "Park"},
			},
			{
				name: "LineDirective", source: "//line virtual.xgo:100:20\nconst Scene = `Stu\rdio`\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Line: 1, Character: 15}, End: Position{Line: 1, Character: 22}}, NewText: "Park"},
			},
			{
				name: "ImplicitInitializer", source: "const (\n First = \"Studio\"\n Scene\n)\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Line: 1, Character: 10}, End: Position{Line: 1, Character: 16}}, NewText: "Park"},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				proj := s.getProj()
				call := resourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 1)
				id := testResourceID{"scenes", "Studio"}
				result := newTestResourceAnalysis()
				result.addResourceRef(resourceRef{ID: id, Kind: XGoResourceRefKindConstantReference, Node: call.Args[0]})
				changes := s.renameResourceAtRefs(t, result, id, "Park")
				require.Equal(t, map[DocumentURI][]TextEdit{"file:///main.xgo": {tt.want}}, changes)
				edit := changes["file:///main.xgo"][0]
				content := []byte(tt.source)
				updated := tt.source[:PositionOffset(content, edit.Range.Start)] + edit.NewText + tt.source[PositionOffset(content, edit.Range.End):]
				updatedProj := newTestServer(t, map[string][]byte{"main.xgo": []byte(updated)}).getProj()
				resourceTestCall(t, updatedProj, "main.xgo")
				typeInfo, err := updatedProj.TypeInfo()
				require.NoError(t, err)
				scene := requireValueAs[*gotypes.Const](t, typeInfo.Pkg.Scope().Lookup("Scene"))
				assert.Equal(t, `"Park"`, scene.Val().ExactString())
			})
		}
	})

	t.Run("StringEscaping", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			literal     string
			newName     string
			wantLiteral string
		}{
			{name: "Quotes", literal: `"Studio"`, newName: `A"B`, wantLiteral: `"A\"B"`},
			{name: "Backslash", literal: `"Studio"`, newName: `A\B`, wantLiteral: `"A\\B"`},
			{name: "ControlCharacters", literal: `"Studio"`, newName: "A\n\r\t\x00B", wantLiteral: `"A\n\r\t\x00B"`},
			{name: "Unicode", literal: `"Studio"`, newName: "\U0001f600", wantLiteral: "\"\U0001f600\""},
			{name: "Empty", literal: `"Studio"`, wantLiteral: `""`},
			{name: "EscapedOriginal", literal: `"Stu\x64io"`, newName: "Park", wantLiteral: `"Park"`},
			{name: "Interpolation", literal: `"Studio"`, newName: "${name}", wantLiteral: `"\x24{name}"`},
			{name: "DollarEscape", literal: `"Studio"`, newName: "$$", wantLiteral: `"\x24\x24"`},
			{name: "RawQuotesAndBackslash", literal: "`Studio`", newName: `A"\B`, wantLiteral: "`A\"\\B`"},
			{name: "RawBacktick", literal: "`Studio`", newName: "A`B", wantLiteral: "\"A`B\""},
			{name: "RawControlCharacters", literal: "`Studio`", newName: "A\n\r\t\x00B", wantLiteral: `"A\n\r\t\x00B"`},
			{name: "RawInterpolation", literal: "`Studio`", newName: "${name} $$", wantLiteral: `"\x24{name} \x24\x24"`},
			{name: "RawCarriageReturn", literal: "`Stu\rdio`", newName: "Park", wantLiteral: "`Park`"},
			{name: "RawCarriageReturnWithQuoteChange", literal: "`Stu\rdio`", newName: "A`B", wantLiteral: "\"A`B\""},
			{name: "RawMultipleCarriageReturns", literal: "`S\rtu\r\rdio\r`", newName: "Park", wantLiteral: "`Park`"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, form := range []struct {
					name   string
					prefix string
					suffix string
					kind   XGoResourceRefKind
				}{
					{name: "Literal", prefix: "echo ", suffix: "\n", kind: XGoResourceRefKindStringLiteral},
					{name: "Constant", prefix: "const Scene = ", suffix: "\necho Scene\n", kind: XGoResourceRefKindConstantReference},
				} {
					t.Run(form.name, func(t *testing.T) {
						source := form.prefix + tt.literal + form.suffix
						s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
						proj := s.getProj()
						call := resourceTestCall(t, proj, "main.xgo")
						require.Len(t, call.Args, 1)
						id := testResourceID{"scenes", "Studio"}
						result := newTestResourceAnalysis()
						result.addResourceRef(resourceRef{ID: id, Kind: form.kind, Node: call.Args[0]})
						changes := s.renameResourceAtRefs(t, result, id, tt.newName)
						require.Len(t, changes, 1)
						require.Len(t, changes["file:///main.xgo"], 1)
						edit := changes["file:///main.xgo"][0]
						content := []byte(source)
						updated := source[:PositionOffset(content, edit.Range.Start)] + edit.NewText + source[PositionOffset(content, edit.Range.End):]
						assert.Equal(t, form.prefix+tt.wantLiteral+form.suffix, updated)
						updatedProj := newTestServer(t, map[string][]byte{"main.xgo": []byte(updated)}).getProj()
						updatedCall := resourceTestCall(t, updatedProj, "main.xgo")
						require.Len(t, updatedCall.Args, 1)
						typeInfo, err := updatedProj.TypeInfo()
						require.NoError(t, err)
						value := typeInfo.Types[updatedCall.Args[0]].Value
						require.NotNil(t, value)
						require.Equal(t, constant.String, value.Kind())
						assert.Equal(t, tt.newName, constant.StringVal(value))
					})
				}
			})
		}
	})

	t.Run("UneditableConstants", func(t *testing.T) {
		t.Run("ImportedPositionCollision", func(t *testing.T) {
			const source = "import . \"example.com/assets\"\nfunc local() { const Name = \"Local\" }\necho Name\n"
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProj()
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			var declaration token.Pos
			ast.Inspect(file, func(node ast.Node) bool {
				if spec, ok := node.(*ast.ValueSpec); ok && spec.Names[0].Name == "Name" {
					declaration = spec.Pos()
				}
				return true
			})
			require.True(t, declaration.IsValid())
			pkg := gotypes.NewPackage("example.com/assets", "assets")
			pkg.Scope().Insert(gotypes.NewConst(declaration, pkg, "Name", gotypes.Typ[gotypes.UntypedString], constant.MakeString("Remote")))
			pkg.MarkComplete()
			fallback := proj.Importer
			proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == pkg.Path() {
					return pkg, nil
				}
				return fallback.Import(path)
			})
			_, err = proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, "main.xgo")
			require.Len(t, call.Args, 1)
			id := testResourceID{"files", "Remote"}
			result := newTestResourceAnalysis()
			result.addResourceRef(resourceRef{ID: id, Kind: XGoResourceRefKindConstantReference, Node: call.Args[0]})
			assert.Empty(t, s.renameResourceAtRefs(t, result, id, "Changed"))
		})

		for _, tt := range []struct {
			name    string
			source  string
			oldName string
		}{
			{name: "ExternalDeclaration", source: "import . \"time\"\necho RFC3339, \"2006-01-02T15:04:05Z07:00\"\n", oldName: "2006-01-02T15:04:05Z07:00"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				proj := s.getProj()
				call := resourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 2)
				id := testResourceID{"scenes", tt.oldName}
				result := newTestResourceAnalysis()
				result.addResourceRef(resourceRef{ID: id, Kind: XGoResourceRefKindConstantReference, Node: call.Args[0]})
				assert.Empty(t, s.renameResourceAtRefs(t, result, id, "Park"))
				result.addResourceRef(resourceRef{ID: id, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[1]})
				wantRange := RangeForNode(proj, call.Args[1])
				wantRange.Start.Character++
				wantRange.End.Character--
				assert.Equal(t, map[DocumentURI][]TextEdit{
					"file:///main.xgo": {{Range: wantRange, NewText: "Park"}},
				}, s.renameResourceAtRefs(t, result, id, "Park"))
			})
		}
	})

	t.Run("IdentifierAndSpriteContext", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("var Runner int\necho Runner, \"idle\", \"idle\"\n"),
		})
		proj := s.getProj()
		call := resourceTestCall(t, proj, "main.xgo")
		require.Len(t, call.Args, 3)
		result := newTestResourceAnalysis()
		spriteID := testResourceID{"actors", "Runner"}
		skinID := testResourceID{"actors/Runner/skins", "idle"}
		result.resourceRefs = []resourceRef{
			{ID: spriteID, Kind: XGoResourceRefKindAutoBindingReference, Node: call.Args[0]},
			{ID: skinID, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[1]},
			{ID: testResourceID{"actors/Other/skins", "idle"}, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[2]},
		}
		assert.Equal(t, map[DocumentURI][]TextEdit{
			"file:///main.xgo": {{Range: Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 11}}, NewText: "Player"}},
		}, s.renameResourceAtRefs(t, result, spriteID, "Player"))
		assert.Equal(t, map[DocumentURI][]TextEdit{
			"file:///main.xgo": {{Range: Range{Start: Position{Line: 1, Character: 14}, End: Position{Line: 1, Character: 18}}, NewText: "rest"}},
		}, s.renameResourceAtRefs(t, result, skinID, "rest"))
	})
}

func applyResourceRenameTestEdits(t *testing.T, source string, edits []TextEdit) string {
	t.Helper()

	require.NotEmpty(t, edits)
	edits = slices.Clone(edits)
	slices.SortFunc(edits, func(a, b TextEdit) int { return comparePositions(b.Range.Start, a.Range.Start) })
	content := []byte(source)
	updated := source
	for i, edit := range edits {
		if i > 0 {
			require.LessOrEqual(t, comparePositions(edit.Range.End, edits[i-1].Range.Start), 0, "rename edits must not overlap")
		}
		start := PositionOffset(content, edit.Range.Start)
		end := PositionOffset(content, edit.Range.End)
		updated = updated[:start] + edit.NewText + updated[end:]
	}
	return updated
}
