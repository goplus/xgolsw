package server

import (
	gotypes "go/types"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type completionTestImporter struct {
	gotypes.Importer
	unavailablePath string
}

func (i completionTestImporter) Import(pkgPath string) (*gotypes.Package, error) {
	if pkgPath == i.unavailablePath {
		return nil, fs.ErrNotExist
	}
	return i.Importer.Import(pkgPath)
}

func TestServerTextDocumentCompletionSymbols(t *testing.T) {
	t.Run("UnavailablePackage", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			pkgPath string
		}{
			{name: "Classfile", pkgPath: testframework.PkgPath},
			{name: "BuiltinAlias", pkgPath: "github.com/qiniu/x/osx"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main_fixture.gox": []byte("import \"fmt\"\nvar Count int\nCount = 1\n\n"),
				})
				proj := s.workspaceRootFS
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				// Package data can become unavailable after type information was cached.
				proj.Importer = completionTestImporter{Importer: proj.Importer, unavailablePath: tt.pkgPath}
				items := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 3})
				for _, label := range []string{"Count", "fmt", "len", "echo"} {
					assert.Contains(t, completionItemLabels(items), label)
				}
			})
		}
	})

	t.Run("SourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			owner    string
		}{
			{name: "XGo", filename: "main.xgo"},
			{name: "LegacyXGo", filename: "main.gop"},
			{name: "StandaloneClass", filename: "Record.gox", owner: "Record."},
			{name: "ProjectClass", filename: "main_fixture.gox", owner: "App."},
			{name: "WorkClass", filename: "Worker_fixture.gox", owner: "Worker."},
			{name: "OtherFrameworkWithSpxExtension", filename: "main.spx", owner: "App."},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte("// Count documentation.\nvar Count int\nCount = 1\n\n")}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newTestServer(t, files)
				if tt.name == "OtherFrameworkWithSpxExtension" {
					s.workspaceRootFS.Mod = xgomod.New(modload.Module{
						Opt: &modfile.File{Projects: []*modfile.Project{{
							Ext: ".spx", FullExt: "main.spx", Class: "App", PkgPaths: []string{testframework.PkgPath},
							Works: []*modfile.Class{{Ext: ".spx", Class: "Item", Embedded: true}},
						}}},
					})
					require.NoError(t, s.workspaceRootFS.Mod.ImportClasses())
				}
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				items := completionItemsAt(t, s, tt.filename, Position{Line: 3})
				item := completionItemByLabel(items, "Count")
				require.NotNil(t, item)
				assert.Equal(t, VariableCompletion, item.Kind)
				assert.Equal(t, "Count", item.InsertText)
				data := requireValueAs[*CompletionItemData](t, item.Data)
				assert.Equal(t, "xgo:main?"+tt.owner+"Count", data.Definition.String())
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, "Count documentation.")
				assert.Contains(t, completionItemLabels(items), "echo")
				if tt.owner == "App." || tt.owner == "Worker." {
					assert.Contains(t, completionItemLabels(items), "Low")
					assert.Contains(t, completionItemLabels(items), "runWhen")
				}
				if tt.owner == "App." {
					assert.Contains(t, completionItemLabels(items), "onStart")
				}
				if tt.owner == "Worker." {
					assert.Contains(t, completionItemLabels(items), "Value")
					assert.Contains(t, completionItemLabels(items), "apply")
				}
			})
		}
	})

	t.Run("CallbackScope", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox":   nil,
			"Worker_fixture.gox": []byte("var Count int\nonValue value => {\n\t\n}\n"),
		})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		items := completionItemsAt(t, s, "Worker_fixture.gox", Position{Line: 2, Character: 1})
		for _, label := range []string{"Count", "Value", "apply", "value"} {
			assert.Contains(t, completionItemLabels(items), label)
		}
	})

	t.Run("FrameworkMethodOverloads", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			position Position
		}{
			{name: "ImplicitReceiver", filename: "main_fixture.gox", source: "onStart => {\n\n}\n", position: Position{Line: 1}},
			{name: "ExplicitReceiver", filename: "main.xgo", source: "import \"example.com/framework\"\nvar app framework.App\napp.measure(1)\n", position: Position{Line: 2, Character: 5}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{tt.filename: []byte(tt.source)})
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				items := completionItemsAt(t, s, tt.filename, tt.position)
				assert.Equal(t, 2, countCompletionItemLabel(items, "measure"))
				assert.NotContains(t, completionItemLabels(items), "Measure__0")
				assert.NotContains(t, completionItemLabels(items), "Measure__1")
				overloads := make(map[string]CompletionItem)
				for _, item := range items {
					if item.Label != "measure" {
						continue
					}
					data := requireValueAs[*CompletionItemData](t, item.Data)
					require.NotNil(t, data.Definition)
					assert.Equal(t, ToPtr(testframework.PkgPath), data.Definition.Package)
					assert.Equal(t, ToPtr("App.measure"), data.Definition.Name)
					require.NotNil(t, data.Definition.OverloadID)
					overloads[*data.Definition.OverloadID] = item
				}
				require.Len(t, overloads, 2)
				for _, overload := range []struct {
					id       string
					typeName string
					doc      string
				}{
					{id: "0", typeName: "int", doc: "Measure__0 is the integer overload of Measure."},
					{id: "1", typeName: "string", doc: "Measure__1 is the string overload of Measure."},
				} {
					item, ok := overloads[overload.id]
					require.True(t, ok, overload.id)
					assert.Equal(t, FunctionCompletion, item.Kind)
					assert.Equal(t, "measure", item.InsertText)
					assert.Equal(t, ToPtr(PlainTextTextFormat), item.InsertTextFormat)
					require.NotNil(t, item.Documentation)
					doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
					assert.Contains(t, doc.Value, `overview="func measure(value `+overload.typeName+`) int"`)
					assert.Contains(t, doc.Value, overload.doc)
				}
			})
		}
	})

	t.Run("ClassfileImplicitPackages", func(t *testing.T) {
		for _, sourceKind := range []struct {
			name     string
			filename string
			isClass  bool
		}{
			{name: "Project", filename: "main_fixture.gox", isClass: true},
			{name: "Work", filename: "Worker_fixture.gox", isClass: true},
			{name: "XGo", filename: "main.xgo"},
		} {
			t.Run(sourceKind.name, func(t *testing.T) {
				for _, tt := range []struct {
					name     string
					withMath bool
				}{
					{name: "FrameworkOnly"},
					{name: "WithAdditionalPackage", withMath: true},
				} {
					t.Run(tt.name, func(t *testing.T) {
						files := map[string][]byte{"main_fixture.gox": nil}
						files[sourceKind.filename] = []byte("func run() {\n\n}\n")
						s := newTestServer(t, files)
						if tt.withMath {
							class, ok := s.workspaceRootFS.Mod.LookupClass("_fixture.gox")
							require.True(t, ok)
							class.PkgPaths = append(class.PkgPaths, "math")
						}
						lookup := s.lookupPkgDoc
						s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
							if pkgPath == "math" {
								return &pkgdoc.PkgDoc{Funcs: map[string]string{"Abs": "Additional package documentation."}}, nil
							}
							return lookup(pkgPath)
						}
						_, err := s.workspaceRootFS.TypeInfo()
						require.NoError(t, err)
						items := completionItemsAt(t, s, sourceKind.filename, Position{Line: 1})
						assert.Equal(t, sourceKind.isClass, slices.Contains(completionItemLabels(items), "runWhen"))
						item := completionItemByLabel(items, "abs")
						if !sourceKind.isClass || !tt.withMath {
							assert.Nil(t, item)
							return
						}
						require.NotNil(t, item)
						assert.Equal(t, FunctionCompletion, item.Kind)
						data := requireValueAs[*CompletionItemData](t, item.Data)
						require.NotNil(t, data.Definition)
						assert.Equal(t, "xgo:math?abs", data.Definition.String())
						require.NotNil(t, item.Documentation)
						doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
						assert.Contains(t, doc.Value, "Additional package documentation.")
					})
				}
			})
		}
	})

	t.Run("EmbeddedInterfaceMethods", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			declarations string
			embedded     string
		}{
			{name: "Direct", embedded: "fmt.Stringer"},
			{name: "Alias", declarations: "type External = fmt.Stringer\n", embedded: "External"},
			{name: "Nested", declarations: "type External interface { fmt.Stringer }\n", embedded: "External"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := "import \"fmt\"\n" + tt.declarations + `type Combined interface {
	` + tt.embedded + `
	methodOne()
}

func run() {
	var value Combined
	value.methodOne()
}
`
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				lookup := s.lookupPkgDoc
				s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
					if pkgPath == "fmt" {
						return &pkgdoc.PkgDoc{Types: map[string]*pkgdoc.TypeDoc{
							"Stringer": {Methods: map[string]string{"String": "Imported method documentation."}},
						}}, nil
					}
					return lookup(pkgPath)
				}
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				items := completionItemsAt(t, s, "main.xgo", Position{
					Line:      uint32(strings.Count(source[:strings.Index(source, "\tvalue.methodOne()")], "\n")),
					Character: uint32(UTF16Len("\tvalue.m")),
				})
				assert.ElementsMatch(t, []string{"string", "methodOne"}, completionItemLabels(items))
				for _, method := range []struct {
					label      string
					definition string
					doc        string
				}{
					{label: "string", definition: "xgo:fmt?Stringer.string", doc: "Imported method documentation."},
					{label: "methodOne", definition: "xgo:main?Combined.methodOne", doc: `overview="func methodOne()"`},
				} {
					item := completionItemByLabel(items, method.label)
					require.NotNil(t, item)
					assert.Equal(t, FunctionCompletion, item.Kind)
					data := requireValueAs[*CompletionItemData](t, item.Data)
					require.NotNil(t, data.Definition)
					assert.Equal(t, method.definition, data.Definition.String())
					require.NotNil(t, item.Documentation)
					doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
					assert.Contains(t, doc.Value, method.doc)
				}
			})
		}
	})

	t.Run("Symbols", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`import f "fmt"
// Limit documentation.
const Limit = 3
// State documentation.
type State struct{}
func helper() {}
func main() {
	var local int

}
`)})
		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8})
		for _, tt := range []struct {
			label string
			kind  CompletionItemKind
		}{
			{label: "f", kind: ModuleCompletion},
			{label: "Limit", kind: ConstantCompletion},
			{label: "State", kind: StructCompletion},
			{label: "helper", kind: FunctionCompletion},
			{label: "local", kind: VariableCompletion},
		} {
			item := completionItemByLabel(items, tt.label)
			require.NotNil(t, item, tt.label)
			assert.Equal(t, tt.kind, item.Kind)
		}
		assert.NotContains(t, completionItemLabels(items), "fmt")
	})

	t.Run("DocumentationUpdates", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			source   string
			position Position
			label    string
			pkgPath  string
			setDoc   func(*pkgdoc.PkgDoc, string)
		}{
			{name: "Package", source: "import f \"example.com/framework\"\n\n", position: Position{Line: 1}, label: "f", pkgPath: testframework.PkgPath,
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Doc = text }},
			{name: "PackageType", source: "import \"example.com/framework\"\nframework.\n", position: Position{Line: 1, Character: 10}, label: "Item", pkgPath: testframework.PkgPath,
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Types["Item"].Doc = text }},
			{name: "PackageConstant", source: "import \"example.com/framework\"\nframework.\n", position: Position{Line: 1, Character: 10}, label: "Low", pkgPath: testframework.PkgPath,
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Consts["Low"] = text }},
			{name: "PackageFunction", source: "import \"example.com/framework\"\nframework.\n", position: Position{Line: 1, Character: 10}, label: "runWhen", pkgPath: testframework.PkgPath,
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Funcs["RunWhen"] = text }},
			{name: "StructField", source: "import \"example.com/framework\"\nvar item framework.Item\nitem.\n", position: Position{Line: 2, Character: 5}, label: "Value", pkgPath: testframework.PkgPath,
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Types["Item"].Fields["Value"] = text }},
			{name: "StructMethod", source: "import \"example.com/framework\"\nvar item framework.Item\nitem.\n", position: Position{Line: 2, Character: 5}, label: "apply", pkgPath: testframework.PkgPath,
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Types["Item"].Methods["Apply"] = text }},
			{name: "StructLiteralField", source: "import \"example.com/framework\"\nitem := framework.Item{}\n", position: Position{Line: 1, Character: 23}, label: "Value", pkgPath: testframework.PkgPath,
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Types["Item"].Fields["Value"] = text }},
			{name: "InterfaceMethod", source: "import \"fmt\"\nvar s fmt.Stringer\ns.\n", position: Position{Line: 2, Character: 2}, label: "string", pkgPath: "fmt",
				setDoc: func(doc *pkgdoc.PkgDoc, text string) {
					doc.Types["Stringer"] = &pkgdoc.TypeDoc{Methods: map[string]string{"String": text}}
				}},
			{name: "Builtin", source: "\n\n", position: Position{Line: 1}, label: "len", pkgPath: "builtin",
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Funcs["len"] = text }},
			{name: "BuiltinAlias", source: "\n\n", position: Position{Line: 1}, label: "echo", pkgPath: "fmt",
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Funcs["Println"] = text }},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				lookup := s.lookupPkgDoc
				var doc *pkgdoc.PkgDoc
				s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
					if pkgPath == tt.pkgPath {
						if doc == nil {
							return nil, fs.ErrNotExist
						}
						return doc, nil
					}
					return lookup(pkgPath)
				}
				for _, text := range []string{"First documentation.", "Second documentation.", "", "Restored documentation."} {
					if text == "" {
						doc = nil
					} else {
						doc = testframework.NewPkgDoc(t)
						tt.setDoc(doc, text)
					}
					items := completionItemsAt(t, s, "main.xgo", tt.position)
					item := completionItemByLabel(items, tt.label)
					require.NotNil(t, item, tt.label)
					documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
					if text == "" {
						assert.NotContains(t, documentation.Value, "documentation.")
					} else {
						assert.Contains(t, documentation.Value, text)
					}
				}
			})
		}
	})

	t.Run("DocumentUpdates", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("// Before update.\nvar count int\ncount = 1\n\n")})
		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3})
		before := completionItemByLabel(items, "count")
		require.NotNil(t, before)
		beforeDoc := requireValueAs[MarkupContent](t, before.Documentation.Value)
		assert.Contains(t, beforeDoc.Value, "Before update.")
		assert.Contains(t, beforeDoc.Value, "var count int")

		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("// After update.\nvar count string\ncount = \"\"\n\n"), Version: 1}})
		items = completionItemsAt(t, s, "main.xgo", Position{Line: 3})
		after := completionItemByLabel(items, "count")
		require.NotNil(t, after)
		afterDoc := requireValueAs[MarkupContent](t, after.Documentation.Value)
		assert.Contains(t, afterDoc.Value, "After update.")
		assert.Contains(t, afterDoc.Value, "var count string")
		assert.NotContains(t, afterDoc.Value, "Before update.")
	})

	t.Run("UTF16Position", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("import \"example.com/framework\"\r\nvar item framework.Item\r\necho \"\U0001f600\", item.\r\n"),
		})
		items := completionItemsAt(t, s, "main.xgo", Position{Line: 2, Character: 16})
		assert.Contains(t, completionItemLabels(items), "Value")
		assert.Contains(t, completionItemLabels(items), "apply")
	})

	t.Run("UnavailablePosition", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			position Position
		}{
			{name: "MissingFile", filename: "missing.xgo"},
			{name: "OutOfBounds", filename: "main.xgo", position: Position{Line: 100, Character: 100}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte("echo 1\n")})
				result, err := s.textDocumentCompletion(&CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: tt.position,
				}})
				require.NoError(t, err)
				assert.Empty(t, result)
			})
		}
	})

	t.Run("DisabledContexts", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			source   string
			position Position
		}{
			{name: "InStringLit", source: `
echo "a
`, position: Position{Line: 1, Character: 7}},
			{name: "InComment", source: `
// Run My G
`, position: Position{Line: 1, Character: 11}},
			{name: "NoCompletionInFuncDeclName", source: `
func d
`, position: Position{Line: 1, Character: 6}},
			{name: "NoCompletionInImportAlias", source: `
import f "fmt"
`, position: Position{Line: 1, Character: 8}},
			{name: "NoCompletionInTypeDeclName", source: `
type Fo struct{}
`, position: Position{Line: 1, Character: 7}},
			{name: "NoCompletionInVarDeclName", source: `
func main() {
	var foo int
}
`, position: Position{Line: 2, Character: 8}},
			{name: "NoCompletionInConstDeclName", source: `
const foo = 1
`, position: Position{Line: 1, Character: 8}},
			{name: "NoCompletionInPackageName", source: `package main

func test() {}
`, position: Position{Line: 0, Character: 10}},
			{name: "NoCompletionInFuncReceiverName", source: `
type T struct{}

func (t T) test() {}
`, position: Position{Line: 3, Character: 7}},
			{name: "NoCompletionInFuncParamName", source: `
func test(foo int) {}
`, position: Position{Line: 1, Character: 11}},
			{name: "NoCompletionInFuncResultName", source: `
func test() (result int) { return 0 }
`, position: Position{Line: 1, Character: 19}},
			{name: "NoCompletionInStructFieldName", source: `
type T struct {
	field int
}
`, position: Position{Line: 2, Character: 4}},
			{name: "NoCompletionInInterfaceMethodName", source: `
type T interface {
	Run()
}
`, position: Position{Line: 2, Character: 3}},
			{name: "NoCompletionInLabelName", source: `
func test() {
loop:
	for {
		break
	}
}
`, position: Position{Line: 2, Character: 3}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				assert.Empty(t, completionItemsAt(t, s, "main.xgo", tt.position))
			})
		}
	})

	t.Run("PackageMember", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
import "fmt"
fmt.
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 2, Character: 4})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "println"))
	})

	t.Run("MainPackageInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Runner interface {
	Run()
}

type MyRunner struct {}
func (r *MyRunner) Run() {}

func main() {
	var r Runner = new(MyRunner)
	r.
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 10, Character: 3})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Runner.Run"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyRunner.Run"),
		}))
	})

	t.Run("MainPackageInterfaceMethodWithAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Runner interface {
	Run()
}

type RunnerAlias = Runner

type MyRunner struct {}
func (r *MyRunner) Run() {}

func main() {
	var r RunnerAlias = new(MyRunner)
	r.
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 12, Character: 3})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Runner.Run"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyRunner.Run"),
		}))
	})

	t.Run("NonMainPackageInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
import "fmt"

type MyStringer struct {}
func (s *MyStringer) String() string {}

func main() {
	var s fmt.Stringer = new(MyStringer)
	s.
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 3})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("fmt"),
			Name:    ToPtr("Stringer.string"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyStringer.String"),
		}))
	})

	t.Run("MainPackageStructLiteralField", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Point struct {
	X int
	Y int
}

func main() {
	p := Point{}
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 12})
		assert.NotEmpty(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:main?Point.X" {
				assert.Equal(t, "X: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:main?Point.Y" {
				assert.Equal(t, "Y: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
	})

	t.Run("MainPackageStructLiteralFieldWithAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Point struct {
	X int
	Y int
}

type PointAlias = Point

func main() {
	p := PointAlias{}
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 17})
		assert.NotEmpty(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:main?Point.X" {
				assert.Equal(t, "X: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
	})

	t.Run("MainPackageStructDotWithPointerAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Point struct {
	X int
	Y int
}

type PointPtrAlias = *Point

func main() {
	var p PointPtrAlias = new(Point)
	p.
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 10, Character: 3})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Point.X"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Point.Y"),
		}))
	})

	t.Run("NonMainPackageStructLiteralField", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
import "image/color"

func main() {
	c := color.RGBA{}
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 4, Character: 17})
		assert.NotEmpty(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:image/color?RGBA.R" {
				assert.Equal(t, "R: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
	})

	t.Run("WithinIdentifier", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
func main() {
	var abc bool
	if ab {
	}
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 6})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "abc"))
	})

	t.Run("ErrorInterfaceMethodCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type myError struct{}

func (myError) Error() string {
	return "myError"
}

func main() {
	var err error = myError{}
	echo err.
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 10})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "error"))
	})

	t.Run("StartWithInvalidChar", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte("\n\u201c\u201dvar (\n\tmaps []int\n)\n"),
		}
		s := newTestServer(t, m)

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 9, Character: 10},
			},
		})
		require.NoError(t, err)
		var items []CompletionItem
		if itemsResult != nil {
			items = requireValueAs[[]CompletionItem](t, itemsResult)
		}
		require.Nil(t, items)
		assert.Empty(t, items)
	})

	t.Run("StructLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type MyStruct struct {
	Foobar int
}

func main() {
	ms := My
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 6, Character: 9})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MyStruct"))
	})

	t.Run("StructLitFieldName", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type MyStruct struct {
	Foobar int
}

func main() {
	ms := MyStruct{
		Fo
	}
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 4})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "Foobar"))
	})

	t.Run("TypeAssertion", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type MyStruct struct {
	Foobar int
}

func main() {
	var i any = MyStruct{}
	_, ok := i.(My)
}
`),
		}
		s := newTestServer(t, m)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 15})
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MyStruct"))
	})

	t.Run("ChainedCompletionAfterPackagePropertyLikeFunc", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
import "time"

func main() {
	echo time.now.y
}
`),
		}
		s := newTestServer(t, m)

		timeNowItems := completionItemsAt(t, s, "main.xgo", Position{Line: 4, Character: 16})
		assert.True(t, containsCompletionItemLabel(timeNowItems, "year"))
		assert.False(t, containsCompletionItemLabel(timeNowItems, "Now"))
	})

	t.Run("ChainedCompletionAfterLocalPropertyLikeFunc", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
import "time"

func Now() time.Time {
	return time.now
}

func main() {
	echo now.y
}
`),
		}
		s := newTestServer(t, m)

		nowItems := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 11})
		assert.True(t, containsCompletionItemLabel(nowItems, "year"))
		assert.False(t, containsCompletionItemLabel(nowItems, "Now"))
	})
}

func completionItemsAt(t *testing.T, s *Server, filename string, position Position) []CompletionItem {
	t.Helper()

	result, err := s.textDocumentCompletion(&CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: position,
	}})
	require.NoError(t, err)
	items := requireValueAs[[]CompletionItem](t, result)
	require.NotNil(t, items)
	return items
}
