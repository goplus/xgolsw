package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"html/template"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const importTestPkgPath = "example.com/support/v2"

func newImportTestServer(t testing.TB, files map[string][]byte, imports ...*modfile.Import) *Server {
	t.Helper()
	s := newFrameworkTestServer(t, files)
	proj := s.getProj()
	config := testframework.NewModule(t).Module
	config.Opt.Projects[0].Import = imports
	proj.SetModule(newTestModule(t, config))
	packages := make(map[string]*gotypes.Package)
	// These fixtures need only the compiler's minimal fmt declarations,
	// including when the tests run without a host Go toolchain under WASM.
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	packages["fmt"], err = internal.NewImporter(data.OpenExport).Import("fmt")
	require.NoError(t, err)
	docs := make(map[string]*pkgdoc.PkgDoc)
	for _, path := range []string{importTestPkgPath, "example.com/alternate"} {
		file, err := goparser.ParseFile(proj.Fset, path+"/support.go", `package support
const XGoPackage = true
// Name identifies a value.
type Name string
// Value is the default value.
var Value Name
const Default Name = "default"
func Accept(value Name) Name { return value }
func Error() string { return "error" }
`, goparser.ParseComments)
		require.NoError(t, err)
		pkg, err := new(gotypes.Config).Check(path, proj.Fset, []*goast.File{file}, nil)
		require.NoError(t, err)
		packages[path] = pkg
		docs[path] = pkgdoc.NewGo(path, &goast.Package{Name: "support", Files: map[string]*goast.File{"support.go": file}})
	}
	fallback := proj.Importer
	proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if pkg := packages[path]; pkg != nil {
			return pkg, nil
		}
		return fallback.Import(path)
	})
	lookup := s.lookupPkgDoc
	s.lookupPkgDoc = func(path string) (*pkgdoc.PkgDoc, error) {
		if doc := docs[path]; doc != nil {
			return doc, nil
		}
		return lookup(path)
	}
	return s
}

func setImportTestFrameworkMethods(t testing.TB, s *Server, methods string) {
	t.Helper()
	proj := s.getProj()
	file, err := goparser.ParseFile(proj.Fset, "framework.go", `package framework
const XGoPackage = true
type App struct{}
type Item struct{}
func XGot_App_Main(app any, items ...any) {}
`+methods, 0)
	require.NoError(t, err)
	framework, err := new(gotypes.Config).Check(testframework.PkgPath, proj.Fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	fallback := proj.Importer
	proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if path == framework.Path() {
			return framework, nil
		}
		return fallback.Import(path)
	})
}

func TestServerClassfileImportCompletion(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"TypeAnnotation", "var value |int\necho value\n"},
		{"Assignment", "var value any = |1\necho value\n"},
		{"FunctionArgument", "echo |1\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			assert.Contains(t, completionItemLabels(completionItemsAt(t, s, "main_fixture.gox", pos)), "helper")
		})
	}
	for _, kind := range []struct {
		name, filename string
		class          bool
	}{
		{"Project", "main_fixture.gox", true},
		{"Work", "Worker_fixture.gox", true},
		{"Plain", "main.xgo", false},
		{"NormalClass", "Record.gox", false},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, tt := range []struct{ name, alias, want string }{
				{"Alias", "helper", "helper"},
				{"DeclaredName", "", "support"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					source, pos := typeDisplayTestSource(t, "echo 1\n|\n")
					files := map[string][]byte{kind.filename: []byte(source)}
					if kind.filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := newImportTestServer(t, files, &modfile.Import{Name: tt.alias, Path: importTestPkgPath})
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					items := completionItemsAt(t, s, kind.filename, pos)
					item := completionItemByLabel(items, tt.want)
					if kind.class {
						require.NotNil(t, item)
						assert.Equal(t, ModuleCompletion, item.Kind)
						assert.Equal(t, tt.want, item.InsertText)
						data := requireValueAs[*XGoCompletionItemData](t, item.Data)
						assert.Equal(t, importTestPkgPath, *data.Definition.Package)
					} else {
						assert.Nil(t, item)
					}
					assert.NotContains(t, completionItemLabels(items), "Name")
					assert.NotContains(t, completionItemLabels(items), "v2")
				})
			}
		})
	}
	for _, tt := range []struct {
		name, source, wantPath string
		wantKind               CompletionItemKind
	}{
		{"ExplicitOverride", "import helper \"example.com/alternate\"\necho helper.Value\n|\n", "example.com/alternate", ModuleCompletion},
		{"LocalShadow", "func run() {\nhelper := 1\necho helper\n|\n}\n", "main", VariableCompletion},
		{"MemberShadow", "var helper int\necho helper\n|\n", "main", VariableCompletion},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			item := completionItemByLabel(completionItemsAt(t, s, "main_fixture.gox", pos), "helper")
			require.NotNil(t, item)
			assert.Equal(t, tt.wantKind, item.Kind)
			data := requireValueAs[*XGoCompletionItemData](t, item.Data)
			assert.Equal(t, tt.wantPath, *data.Definition.Package)
		})
	}
	t.Run("PackageMembers", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, "helper.|\n")
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
		items := completionItemsAt(t, s, "main_fixture.gox", pos)
		assert.Contains(t, completionItemLabels(items), "Name")
		assert.Contains(t, completionItemLabels(items), "accept")
	})
	t.Run("MultipleAliases", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, "import explicit \"example.com/support/v2\"\necho explicit.Value\n|\n")
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)},
			&modfile.Import{Name: "helper", Path: importTestPkgPath},
			&modfile.Import{Name: "other", Path: importTestPkgPath})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		labels := completionItemLabels(completionItemsAt(t, s, "main_fixture.gox", pos))
		for _, name := range []string{"explicit", "helper", "other"} {
			assert.Contains(t, labels, name)
		}
	})
	t.Run("DuplicateAlias", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte("echo helper.Value\n")},
			&modfile.Import{Name: "helper", Path: importTestPkgPath},
			&modfile.Import{Name: "helper", Path: "example.com/alternate"})
		proj := s.requestProject()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		file, err := proj.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		assert.Equal(t, "example.com/alternate", importsForFile(proj, file).named["helper"].Imported().Path())
	})
	t.Run("BlankAndDotDirectives", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, "echo 1\n|\n")
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)},
			&modfile.Import{Name: ".", Path: importTestPkgPath},
			&modfile.Import{Name: "_", Path: "example.com/alternate"})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		labels := completionItemLabels(completionItemsAt(t, s, "main_fixture.gox", pos))
		for _, name := range []string{".", "_", "Name", "support"} {
			assert.NotContains(t, labels, name)
		}
	})
}

func TestFileImportsIsolation(t *testing.T) {
	t.Run("Files", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{
			"main_fixture.gox":   []byte("import helper \"example.com/alternate\"\necho helper.Value\n"),
			"Worker_fixture.gox": []byte("echo helper.Value\n"),
			"plain.xgo":          []byte("func run() {}\n"),
		}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
		proj := s.requestProject()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		for _, tt := range []struct{ filename, path string }{
			{"main_fixture.gox", "example.com/alternate"},
			{"Worker_fixture.gox", importTestPkgPath},
			{"plain.xgo", ""},
		} {
			file, err := proj.ASTFile(tt.filename)
			require.NoError(t, err)
			name := importsForFile(proj, file).named["helper"]
			if tt.path == "" {
				assert.Nil(t, name)
			} else {
				require.NotNil(t, name)
				assert.Equal(t, tt.path, name.Imported().Path())
			}
		}
	})
	t.Run("Frameworks", func(t *testing.T) {
		config := classfileTestModule()
		config.Opt.Projects[0].Import = []*modfile.Import{{Name: "helper", Path: "example.com/second"}}
		config.Opt.Projects[1].Import = []*modfile.Import{{Name: "helper", Path: "example.com/first"}}
		s := newClassfileTestServerWithModule(t, map[string][]byte{
			"First.first":   []byte("var value helper.Item\necho value\n"),
			"Second.second": []byte("var value helper.Item\necho value\n"),
		}, config)
		proj := s.requestProject()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		for _, tt := range []struct{ filename, path string }{
			{"First.first", "example.com/second"},
			{"Second.second", "example.com/first"},
		} {
			file, err := proj.ASTFile(tt.filename)
			require.NoError(t, err)
			bindings := importsForFile(proj, file)
			require.NotNil(t, bindings.named["helper"])
			assert.Equal(t, tt.path, bindings.named["helper"].Imported().Path())
		}
	})
}

func TestCollectPredefinedNamesImports(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		want         bool
	}{
		{"Dot", "import . \"example.com/support/v2\"\necho \"text\"\n", true},
		{"Named", "import helper \"example.com/support/v2\"\necho helper.Value, \"text\"\n", false},
		{"Automatic", "echo helper.Value, \"text\"\n", false},
		{"Shadowed", "import . \"example.com/support/v2\"\nfunc run() {\nValue := 1\necho Value, \"text\"\n}\n", false},
		{"Ambiguous", "import . \"example.com/support/v2\"\nimport . \"example.com/alternate\"\necho \"text\"\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(tt.source)}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			ctx := inputSlotTestContext(t, s, "main_fixture.gox")
			pkg, err := ctx.proj.Import(importTestPkgPath)
			require.NoError(t, err)
			names := collectPredefinedNames(ctx, XGoInputSlotKindValue, inputSlotLiteral(t, ctx, `"text"`), pkg.Scope().Lookup("Name").Type())
			if tt.want {
				assert.Contains(t, names, "Value")
			} else {
				assert.NotContains(t, names, "Value")
			}
		})
	}
}

func TestServerCompletionImportNamespaces(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		name := "Explicit"
		if automatic {
			name = "Automatic"
		}
		t.Run(name, func(t *testing.T) {
			for _, tt := range []struct {
				name, alias, source string
				kind                CompletionItemKind
			}{
				{"Function", "accept", "echo accept(\"text\"), accept.Value\n", FunctionCompletion},
				{"Builtin", "echo", "echo 1, echo.Value\n", FunctionCompletion},
				{"Variable", "Value", "echo Value, Value.Value, \"text\"\n", VariableCompletion},
				{"Constant", "Default", "echo Default, Default.Value\n", ConstantCompletion},
				{"Type", "Name", "var value Name\necho Name(\"text\"), Name.Value, value\n", ClassCompletion},
			} {
				t.Run(tt.name, func(t *testing.T) {
					prefix := "import . \"example.com/support/v2\"\n"
					var imports []*modfile.Import
					if automatic {
						imports = []*modfile.Import{{Name: tt.alias, Path: "example.com/alternate"}}
					} else {
						prefix += "import " + tt.alias + " \"example.com/alternate\"\n"
					}
					source, pos := typeDisplayTestSource(t, prefix+tt.source+"|\n")
					s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)}, imports...)
					proj := s.requestProject()
					_, err := proj.TypeInfo()
					require.NoError(t, err)
					var kinds []CompletionItemKind
					for _, item := range completionItemsAt(t, s, "main_fixture.gox", pos) {
						if item.Label == tt.alias {
							kinds = append(kinds, item.Kind)
						}
					}
					assert.ElementsMatch(t, []CompletionItemKind{tt.kind, ModuleCompletion}, kinds)
					pkg, err := proj.Import(importTestPkgPath)
					require.NoError(t, err)
					switch tt.kind {
					case VariableCompletion:
						ctx := inputSlotTestContext(t, s, "main_fixture.gox")
						names := collectPredefinedNames(ctx, XGoInputSlotKindValue, inputSlotLiteral(t, ctx, `"text"`), pkg.Scope().Lookup("Name").Type())
						assert.Contains(t, names, "Value")
					case ClassCompletion:
						file, err := proj.ASTFile("main_fixture.gox")
						require.NoError(t, err)
						display := newTypeDisplay(proj, file, PosAt(proj, file, pos))
						typ := pkg.Scope().Lookup("Name").Type()
						assert.Equal(t, "Name", display.typeString(typ))
						name, ok := display.sourceTypeString(typ)
						require.True(t, ok)
						assert.Equal(t, "Name", name)
						s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(source + "echo " + name + "(\"text\")\n"), Version: 1}})
						_, err = s.requestProject().TypeInfo()
						require.NoError(t, err)
					}
				})
			}
		})
	}
}

func TestServerCompletionImportTypePositions(t *testing.T) {
	for _, tt := range []struct{ name, source, globals string }{
		{"LocalValue", "func run() {\nhelper := 1\nvar value |helper.Name\necho helper, value\n}\n", ""},
		{"ClassField", "var (\nhelper int\nvalue |helper.Name\n)\necho value\n", ""},
		{"PackageValue", "var value |helper.Name\necho value\n", "var helper = 1\n"},
		{"TypeArgument", "func run() {\nhelper := 1\nvar value Box[|helper.Name]\necho helper, value\n}\n", ""},
		{"Initializer", "func run() {\nhelper := 1\nvar value |helper.Name = \"text\"\necho helper, value\n}\n", ""},
		{"Parameter", "func run(helper int, value |helper.Name) { echo helper, value }\n", ""},
		{"Result", "func run(helper int) |helper.Name { return \"text\" }\n", ""},
		{"TypeAlias", "func run() {\nhelper := 1\ntype Name = |helper.Name\nvar value Name\necho helper, value\n}\n", ""},
		{"CompositeLiteral", "func run() {\nhelper := 1\nvalue := []|helper.Name{}\necho helper, value\n}\n", ""},
		{"Assertion", "func run(helper int, value any) { echo value.(|helper.Name), helper }\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte(source),
				"globals.xgo":      []byte(tt.globals),
			}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			item := completionItemByLabel(completionItemsAt(t, s, "main_fixture.gox", pos), "helper")
			require.NotNil(t, item)
			assert.Equal(t, ModuleCompletion, item.Kind)
		})
	}
}

func TestServerCompletionImportedCallbacks(t *testing.T) {
	for _, kind := range []struct{ name, filename string }{
		{"DotImport", "main.xgo"},
		{"ClassfileLookup", "main_fixture.gox"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, tt := range []struct {
				name, local, globals string
				want                 bool
			}{
				{"Unshadowed", "", "", true},
				{"LowercaseValue", "accept := 1\necho accept\n", "", true},
				{"UppercaseValue", "Accept := 1\necho Accept\n", "", false},
				{"LowercaseFunction", "accept := func(value int) int { return value }\necho accept\n", "", true},
				{"UppercaseFunction", "Accept := func(value int) int { return value }\necho Accept\n", "", false},
				{"PackageFunction", "", "func Accept(value int) int { return value }\n", false},
			} {
				t.Run(tt.name, func(t *testing.T) {
					prefix := ""
					if kind.filename == "main.xgo" {
						prefix = "import . \"example.com/support/v2\"\n"
					}
					source, pos := typeDisplayTestSource(t, prefix+"func run() {\n"+tt.local+"var fn func(Name) Name = |nil\necho fn\n}\n")
					s := newImportTestServer(t, map[string][]byte{kind.filename: []byte(source), "globals.xgo": []byte(tt.globals)})
					if kind.filename != "main.xgo" {
						config := testframework.NewModule(t).Module
						config.Opt.Projects[0].PkgPaths = append(config.Opt.Projects[0].PkgPaths, importTestPkgPath)
						s.getProj().SetModule(newTestModule(t, config))
					}
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					items := completionItemsAt(t, s, kind.filename, pos)
					item := completionItemByLabel(items, "Accept")
					insertion := "Accept"
					if tt.want {
						require.NotNil(t, item)
						assert.Equal(t, FunctionCompletion, item.Kind)
						assert.Equal(t, "Accept", item.InsertText)
						insertion = item.InsertText
					} else {
						assert.Nil(t, item)
					}
					assert.NotContains(t, completionItemLabels(items), "accept")
					s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(strings.Replace(source, "= nil", "= "+insertion, 1)), Version: 1}})
					_, err = s.requestProject().TypeInfo()
					if tt.want {
						require.NoError(t, err)
					} else {
						require.ErrorContains(t, err, "cannot use")
					}
				})
			}
		})
	}
	for _, tt := range []struct{ name, filename, function, alias string }{
		{"InheritedMember", "Worker_fixture.gox", "Apply", "apply"},
		{"OverloadedMember", "main_fixture.gox", "Measure", "measure"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import . \"example.com/callbacks\"\nfunc run() {\nvar fn func(string) string = |nil\necho fn\n}\n")
			files := map[string][]byte{"main_fixture.gox": nil}
			files[tt.filename] = []byte(source)
			s := newImportTestServer(t, files)
			proj := s.getProj()
			file, err := goparser.ParseFile(proj.Fset, "callbacks.go", "package callbacks\nfunc "+tt.function+"(value string) string { return value }\n", 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check("example.com/callbacks", proj.Fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			fallback := proj.Importer
			proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == pkg.Path() {
					return pkg, nil
				}
				return fallback.Import(path)
			})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, tt.filename, pos))
			assert.NotContains(t, labels, tt.function)
			assert.NotContains(t, labels, tt.alias)
			s.ModifyFiles([]FileChange{{Path: tt.filename, Content: []byte(strings.Replace(source, "= nil", "= "+tt.function, 1)), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.ErrorContains(t, err, "cannot use")
		})
	}
}

func TestServerCompletionImportedTypeShadowing(t *testing.T) {
	for _, tt := range []struct {
		name, globals, local, label, wantPath string
	}{
		{"LocalValue", "", "Name := 1\necho Name\n", "Name", importTestPkgPath},
		{"LocalFunction", "", "Name := func() {}\nName()\n", "Name", importTestPkgPath},
		{"LocalType", "", "type Name int\n", "Name", "main"},
		{"OuterType", "type Name int\n", "", "Name", "main"},
		{"HiddenOuterType", "type Name int\n", "Name := 1\necho Name\n", "Name", importTestPkgPath},
		{"HiddenBuiltinType", "", "int := 1\necho int\n", "int", ""},
		{"HiddenOuterOnlyType", "type Outer int\n", "Outer := 1\necho Outer\n", "Outer", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import . \"example.com/support/v2\"\n"+tt.globals+"func run() {\n"+tt.local+"var value |bool\necho value\n}\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", pos), tt.label)
			insertion := tt.label
			if tt.wantPath != "" {
				require.NotNil(t, item)
				assert.Equal(t, ClassCompletion, item.Kind)
				data := requireValueAs[*XGoCompletionItemData](t, item.Data)
				assert.Equal(t, tt.wantPath, *data.Definition.Package)
				insertion = item.InsertText
			} else {
				assert.Nil(t, item)
			}
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.Replace(source, "var value bool", "var value "+insertion, 1)), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			if tt.wantPath != "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	t.Run("BuiltinType", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, "import . \"example.com/support/v2\"\nvar value |error\necho value\n")
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", pos), "error")
		require.NotNil(t, item)
		assert.Equal(t, InterfaceCompletion, item.Kind)
	})
}

func TestServerCompletionCallbackFactoryShadowing(t *testing.T) {
	for _, member := range []bool{false, true} {
		name := "PackageFunction"
		if member {
			name = "ClassMember"
		}
		t.Run(name, func(t *testing.T) {
			for _, tt := range []struct {
				name, function, alias, callback, declaration string
				call, wantAlias                              bool
			}{
				{"PropertyValue", "Label", "label", "func() string", "func Label() string { return \"\" }\n", false, false},
				{"PropertyCall", "Label", "label", "func() string", "func Label() string { return \"\" }\n", true, false},
				{"RequiredArgumentValue", "OnStart", "onStart", "func(func())", "func OnStart(callback func()) {}\n", false, true},
				{"RequiredArgumentCall", "OnStart", "onStart", "func(func())", "func OnStart(callback func()) {}\n", true, false},
			} {
				t.Run(tt.name, func(t *testing.T) {
					filename := "main.xgo"
					globals := "func factory() " + tt.callback + " { return nil }\n"
					if member {
						filename = "main_fixture.gox"
						if tt.function == "Label" {
							filename = "Worker_fixture.gox"
						}
					} else {
						globals += tt.declaration
					}
					value := "nil"
					markedValue := "|nil"
					if tt.call {
						value = "factory()"
						markedValue = "fac|tory()"
					}
					source, pos := typeDisplayTestSource(t, "import . \"example.com/callbacks\"\nfunc run() {\nvar fn "+tt.callback+" = "+markedValue+"\necho fn\n}\n")
					files := map[string][]byte{filename: []byte(source), "globals.xgo": []byte(globals)}
					if filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := newImportTestServer(t, files)
					proj := s.getProj()
					file, err := goparser.ParseFile(proj.Fset, "callbacks.go", "package callbacks\nfunc "+tt.function+"() "+tt.callback+" { return nil }\n", 0)
					require.NoError(t, err)
					pkg, err := new(gotypes.Config).Check("example.com/callbacks", proj.Fset, []*goast.File{file}, nil)
					require.NoError(t, err)
					fallback := proj.Importer
					proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
						if path == pkg.Path() {
							return pkg, nil
						}
						return fallback.Import(path)
					})
					_, err = s.requestProject().TypeInfo()
					require.NoError(t, err)
					items := completionItemsAt(t, s, filename, pos)
					if !tt.call {
						item := completionItemByLabel(items, tt.function)
						require.NotNil(t, item)
						s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(strings.Replace(source, "= nil", "= "+item.InsertText, 1)), Version: 1}})
						_, err = s.requestProject().TypeInfo()
						require.NoError(t, err)
					}
					item := completionItemByLabel(items, tt.alias)
					insertion := tt.alias
					if tt.wantAlias {
						require.NotNil(t, item)
						insertion = item.InsertText
					} else {
						assert.Nil(t, item)
					}
					if tt.call {
						insertion += "()"
					}
					s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(strings.Replace(source, "= "+value, "= "+insertion, 1)), Version: 2}})
					_, err = s.requestProject().TypeInfo()
					if tt.wantAlias {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
				})
			}
		})
	}
}

func TestServerCompletionImportQualifierPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name, filename, alias, globals, local string
		want                                  bool
	}{
		{"PackageProperty", "main.xgo", "label", "func Label() string { return \"\" }\n", "", true},
		{"PackageFunction", "main.xgo", "onStart", "func OnStart(callback func()) {}\n", "", true},
		{"ClassProperty", "Worker_fixture.gox", "label", "", "", false},
		{"ClassFunction", "main_fixture.gox", "onStart", "", "", true},
		{"ClassOverloads", "main_fixture.gox", "measure", "", "", true},
		{"LocalValue", "main.xgo", "label", "", "label := 1\necho label\n", false},
		{"LocalUppercaseValue", "main.xgo", "label", "", "Label := 1\necho Label\n", true},
		{"PackageValue", "main_fixture.gox", "onStart", "var onStart = 1\n", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import "+tt.alias+" \"example.com/support/v2\"\nfunc run() {\n"+tt.local+"var value any = |nil\necho value\n}\n")
			files := map[string][]byte{tt.filename: []byte(source), "globals.xgo": []byte(tt.globals)}
			if tt.filename == "Worker_fixture.gox" {
				files["main_fixture.gox"] = nil
			}
			s := newImportTestServer(t, files)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			proj := s.requestProject()
			file, err := proj.ASTFile(tt.filename)
			require.NoError(t, err)
			pkg, err := proj.Import(importTestPkgPath)
			require.NoError(t, err)
			display := newTypeDisplay(proj, file, PosAt(proj, file, pos))
			typeName, ok := display.sourceTypeString(pkg.Scope().Lookup("Name").Type())
			assert.Equal(t, tt.want, ok)
			if tt.want {
				assert.Equal(t, tt.alias+".Name", typeName)
			}
			var qualifier *CompletionItem
			for _, item := range completionItemsAt(t, s, tt.filename, pos) {
				if item.Label == tt.alias && item.Kind == ModuleCompletion {
					qualifier = &item
				}
			}
			insertion := tt.alias
			if tt.want {
				require.NotNil(t, qualifier)
				insertion = qualifier.InsertText
			} else {
				assert.Nil(t, qualifier)
			}
			s.ModifyFiles([]FileChange{{Path: tt.filename, Content: []byte(strings.Replace(source, "= nil", "= "+insertion+".Value", 1)), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			if tt.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	for _, tt := range []struct {
		name, methods string
		want          bool
	}{
		{"OverloadedProperty", "func (a *App) Label__0() string { return \"\" }\nfunc (a *App) Label__1(value int) string { return \"\" }\n", false},
		{"OverloadedArguments", "func (a *App) Label__0(value string) string { return value }\nfunc (a *App) Label__1(value int) string { return \"\" }\n", true},
		{"TemplateProperty", "func XGot_App_Label(a *App) string { return \"\" }\n", false},
		{"TemplateArgument", "func XGot_App_Label(a *App, value int) string { return \"\" }\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import label \"example.com/support/v2\"\nfunc run() {\nvar value any = |nil\necho value\n}\n")
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
			setImportTestFrameworkMethods(t, s, tt.methods)
			proj := s.requestProject()
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			var qualifier bool
			for _, item := range completionItemsAt(t, s, "main_fixture.gox", pos) {
				if item.Label == "label" && item.Kind == ModuleCompletion {
					qualifier = true
				}
			}
			assert.Equal(t, tt.want, qualifier)
			astFile, err := proj.ASTFile("main_fixture.gox")
			require.NoError(t, err)
			pkg, err := proj.Import(importTestPkgPath)
			require.NoError(t, err)
			display := newTypeDisplay(proj, astFile, PosAt(proj, astFile, pos))
			typeName, ok := display.sourceTypeString(pkg.Scope().Lookup("Name").Type())
			assert.Equal(t, tt.want, ok)
			if tt.want {
				assert.Equal(t, "label.Name", typeName)
			}
			s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(strings.Replace(source, "= nil", "= label.Name(\"text\")", 1)), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			if tt.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestFileImportsDuplicateLookups(t *testing.T) {
	for _, tt := range []struct {
		name         string
		lookups, dot int
	}{
		{"LookupAndDot", 1, 1},
		{"RepeatedLookup", 2, 0},
		{"RepeatedDot", 0, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prefix := "import helper \"example.com/support/v2\"\n"
			for range tt.dot {
				prefix += "import . \"example.com/support/v2\"\n"
			}
			source, pos := typeDisplayTestSource(t, prefix+"var value helper.Name\necho value, \"text\"\n|\n")
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
			config := testframework.NewModule(t).Module
			for range tt.lookups {
				config.Opt.Projects[0].PkgPaths = append(config.Opt.Projects[0].PkgPaths, importTestPkgPath)
			}
			s.getProj().SetModule(newTestModule(t, config))
			proj := s.requestProject()
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main_fixture.gox", pos))
			for _, name := range []string{"Name", "Value", "Default", "accept"} {
				assert.NotContains(t, labels, name)
			}
			ctx := inputSlotTestContext(t, s, "main_fixture.gox")
			pkg, err := proj.Import(importTestPkgPath)
			require.NoError(t, err)
			typ := pkg.Scope().Lookup("Name").Type()
			assert.NotContains(t, collectPredefinedNames(ctx, XGoInputSlotKindValue, inputSlotLiteral(t, ctx, `"text"`), typ), "Value")
			file, err := proj.ASTFile("main_fixture.gox")
			require.NoError(t, err)
			display := newTypeDisplay(proj, file, PosAt(proj, file, pos))
			assert.Equal(t, "helper.Name", display.typeString(typ))
			name, ok := display.sourceTypeString(typ)
			require.True(t, ok)
			assert.Equal(t, "helper.Name", name)
			s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(source + "echo " + name + "(\"text\")\n"), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(source + "var ambiguous Name\necho ambiguous\n"), Version: 2}})
			_, err = s.requestProject().TypeInfo()
			require.ErrorContains(t, err, "confliction: Name")
		})
	}
}

func TestServerCompletionAmbiguousImports(t *testing.T) {
	source, pos := typeDisplayTestSource(t, "import . \"example.com/support/v2\"\nimport . \"example.com/alternate\"\necho 1\n|\n")
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	for _, name := range []string{"Name", "Value", "Default", "accept"} {
		assert.NotContains(t, labels, name)
	}
	t.Run("FunctionAndType", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, "import . \"example.com/support/v2\"\nimport . \"example.com/types\"\necho 1\n|\n")
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		proj := s.getProj()
		pkg := gotypes.NewPackage("example.com/types", "types")
		obj := gotypes.NewTypeName(0, pkg, "Accept", nil)
		gotypes.NewNamed(obj, gotypes.Typ[gotypes.Int], nil)
		pkg.Scope().Insert(obj)
		pkg.MarkComplete()
		fallback := proj.Importer
		proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
			if path == pkg.Path() {
				return pkg, nil
			}
			return fallback.Import(path)
		})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
		assert.NotContains(t, labels, "Accept")
		assert.Contains(t, labels, "accept")
		for i, tt := range []struct{ name, source string }{
			{"Callback", "var fn func(Name) Name = |nil\necho fn\n"},
			{"Type", "var value |int\necho value\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, pos := typeDisplayTestSource(t, "import . \"example.com/support/v2\"\nimport . \"example.com/types\"\nfunc run() {\n"+tt.source+"}\n")
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(source), Version: 2*i + 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
				assert.NotContains(t, labels, "Accept")
				assert.NotContains(t, labels, "accept")
				source = strings.Replace(source, "= nil", "= Accept", 1)
				source = strings.Replace(source, "var value int", "var value Accept", 1)
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(source), Version: 2*i + 2}})
				_, err = s.requestProject().TypeInfo()
				require.ErrorContains(t, err, "confliction: Accept")
			})
		}
	})
}

func TestFileImportsExtensionMethods(t *testing.T) {
	for _, tt := range []struct {
		name, source, wantPath string
	}{
		{"Unqualified", "import . \"example.com/extension\"\necho 1\n|\n", ""},
		{"Qualified", "import ext \"example.com/extension\"\necho ext.|create(int, \"text\")\n", ""},
		{"Receiver", "import ext \"example.com/extension\"\nvar value ext.Other\nvalue.|accept()\n", "example.com/extension"},
		{"FunctionFirst", "import . \"example.com/support/v2\"\nimport . \"example.com/extension\"\necho accept(\"text\")\n|\n", importTestPkgPath},
		{"FunctionLast", "import . \"example.com/extension\"\nimport . \"example.com/support/v2\"\necho accept(\"text\")\n|\n", importTestPkgPath},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProj()
			file, err := goparser.ParseFile(proj.Fset, "extension.go", `package extension
const XGoPackage = true
type Other struct {}
func XGot_Other_Accept(obj *Other) {}
func XGot_Other_Choose__0(obj *Other, value int) {}
func XGot_Other_Choose__1(obj *Other, value string) {}
func XGot_Other_XGox_Convert[T any](obj *Other, value string) *T { return nil }
func XGox_Create[T any](value string) *T { return nil }
`, 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check("example.com/extension", proj.Fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			fallback := proj.Importer
			proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == pkg.Path() {
					return pkg, nil
				}
				return fallback.Import(path)
			})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", pos)
			item := completionItemByLabel(items, "accept")
			if tt.wantPath != "" {
				require.NotNil(t, item)
				data := requireValueAs[*XGoCompletionItemData](t, item.Data)
				assert.Equal(t, tt.wantPath, *data.Definition.Package)
			} else {
				assert.Nil(t, item)
			}
			labels := completionItemLabels(items)
			if tt.name == "Receiver" {
				assert.Contains(t, labels, "choose")
				assert.Contains(t, labels, "convert")
			} else {
				assert.NotContains(t, labels, "choose")
				assert.NotContains(t, labels, "convert")
				assert.Contains(t, labels, "create")
			}
		})
	}
	t.Run("GoPackage", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, "import ext \"example.com/extension\"\next.|xGot_Other_Accept()\n")
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		proj := s.getProj()
		file, err := goparser.ParseFile(proj.Fset, "extension.go", "package extension\nfunc XGot_Other_Accept() {}\n", 0)
		require.NoError(t, err)
		pkg, err := new(gotypes.Config).Check("example.com/extension", proj.Fset, []*goast.File{file}, nil)
		require.NoError(t, err)
		fallback := proj.Importer
		proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
			if path == pkg.Path() {
				return pkg, nil
			}
			return fallback.Import(path)
		})
		_, err = s.requestProject().TypeInfo()
		require.NoError(t, err)
		assert.Contains(t, completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), "xGot_Other_Accept")
	})
}

func TestFileImportsUnavailablePackages(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		auto         bool
	}{
		{"Explicit", "import missing \"example.com/missing\"\necho 1\n", false},
		{"Automatic", "echo 1\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var imports []*modfile.Import
			if tt.auto {
				imports = append(imports, &modfile.Import{Name: "missing", Path: "example.com/missing"})
			}
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(tt.source)}, imports...)
			proj := s.getProj()
			proj.Importer = completionTestImporter{Importer: proj.Importer, unavailablePath: "example.com/missing"}
			proj = s.requestProject()
			_, err := proj.TypeInfo()
			require.Error(t, err)
			file, err := proj.ASTFile("main_fixture.gox")
			require.NoError(t, err)
			assert.NotContains(t, importsForFile(proj, file).named, "missing")
		})
	}
}

func TestTypeDisplayClassfileImports(t *testing.T) {
	for _, tt := range []struct {
		name, source, alias, want, sourceName string
	}{
		{"Alias", "var value helper.Name\necho |value\n", "helper", "helper.Name", "helper.Name"},
		{"DeclaredName", "var value support.Name\necho |value\n", "", "support.Name", "support.Name"},
		{"ExplicitAlias", "import h \"example.com/support/v2\"\nvar value h.Name\necho |value\n", "helper", "h.Name", "h.Name"},
		{"ExplicitOverride", "import helper \"example.com/alternate\"\nvar value helper.Name\necho |value\n", "helper", "helper.Name", "helper.Name"},
		{"LocalShadow", "var value helper.Name\nfunc run() {\nhelper := 1\necho helper, |value\n}\n", "helper", "support.Name", ""},
		{"MemberShadow", "var (\nvalue helper.Name\nhelper int\n)\necho |value\n", "helper", "helper.Name", ""},
		{"DotImport", "import . \"example.com/support/v2\"\nvar value Name\necho |value\n", "helper", "Name", "Name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)}, &modfile.Import{Name: tt.alias, Path: importTestPkgPath})
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile("main_fixture.gox")
			require.NoError(t, err)
			_, obj, _ := objectAtPosition(proj, info, file, ToPosition(proj, file, pos))
			require.NotNil(t, obj)
			display := newTypeDisplay(proj, file, PosAt(proj, file, pos))
			assert.Equal(t, tt.want, display.typeString(obj.Type()))
			name, ok := display.sourceTypeString(obj.Type())
			assert.Equal(t, tt.sourceName != "", ok)
			assert.Equal(t, tt.sourceName, name)
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main_fixture.gox")}, Position: pos,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, template.HTMLEscapeString("value "+tt.want))
			if ok {
				// The generated qualifier must also work in a conversion expression.
				s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(source + "\nfunc convert() { echo " + name + "(\"converted\") }\n"), Version: 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
			}
		})
	}
}

func TestServerClassfileImportSignature(t *testing.T) {
	source, pos := typeDisplayTestSource(t, "helper.accept(|\"value\")\n")
	s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main_fixture.gox")}, Position: pos,
	}})
	require.NoError(t, err)
	require.NotNil(t, help)
	require.Len(t, help.Signatures, 1)
	assert.Contains(t, help.Signatures[0].Label, "value helper.Name")
	hints, err := s.textDocumentInlayHint(&InlayHintParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main_fixture.gox")}, Range: Range{End: Position{Line: 1}}})
	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Equal(t, "value", hints[0].Label)
}

func TestFileImportsCache(t *testing.T) {
	t.Run("InstanceIsolation", func(t *testing.T) {
		var results []*fileImports
		for range 2 {
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte("echo 1\n")}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
			proj := s.requestProject()
			file, err := proj.ASTFile("main_fixture.gox")
			require.NoError(t, err)
			results = append(results, importsForFile(proj, file))
		}
		assert.NotSame(t, results[0], results[1])
		assert.NotSame(t, results[0].named["helper"].Imported(), results[1].named["helper"].Imported())
	})
	t.Run("ConcurrentRequests", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte("echo 1\n")}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
		var builds atomic.Int32
		s.getProj().RegisterCacheBuilder(fileImportsCacheKind{}, func(proj *xgo.Project) (any, error) {
			builds.Add(1)
			return buildFileImportsCache(proj)
		})
		proj := s.requestProject()
		file, err := proj.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		results := make([]*fileImports, 12)
		var wg sync.WaitGroup
		for i := range results {
			wg.Go(func() { results[i] = importsForFile(proj, file) })
		}
		wg.Wait()
		assert.EqualValues(t, 1, builds.Load())
		for _, result := range results {
			assert.Same(t, results[0], result)
			require.NotNil(t, result.named["helper"])
			assert.Equal(t, importTestPkgPath, result.named["helper"].Imported().Path())
		}
	})
	t.Run("ConfigurationAndSourceChanges", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte("echo 1\n")}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
		before := s.requestProject()
		file, err := before.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		original := importsForFile(before, file)
		config := testframework.NewModule(t).Module
		config.Opt.Projects[0].Import = []*modfile.Import{{Name: "other", Path: "example.com/alternate"}}
		s.getProj().SetModule(newTestModule(t, config))
		after := s.requestProject()
		updatedFile, err := after.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		updated := importsForFile(after, updatedFile)
		assert.NotSame(t, original, updated)
		assert.Contains(t, updated.named, "other")
		assert.NotContains(t, updated.named, "helper")
		assert.Same(t, original, importsForFile(before, file))
		s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte("import local \"example.com/support/v2\"\necho local.Value\n"), Version: 1}})
		changed := s.requestProject()
		changedFile, err := changed.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		assert.Contains(t, importsForFile(changed, changedFile).named, "local")
		assert.NotContains(t, updated.named, "local")
	})
}
