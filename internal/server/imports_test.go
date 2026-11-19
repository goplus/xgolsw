package server

import (
	"cmp"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"html/template"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
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
	packages := map[string]*gotypes.Package{"fmt": testframework.NewFmtPackage(t, proj.Fset)}
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
					source, pos := typeDisplayTestSource(t, `echo 1
|
`)
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
						data := requireValueAs[*CompletionItemData](t, item.Data)
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
		{"ExplicitOverride", `import helper "example.com/alternate"
echo helper.Value
|
`, "example.com/alternate", ModuleCompletion},
		{"LocalShadow", `func run() {
helper := 1
echo helper
|
}
`, "main", VariableCompletion},
		{"MemberShadow", `var helper int
echo helper
|
`, "main", VariableCompletion},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			item := completionItemByLabel(completionItemsAt(t, s, "main_fixture.gox", pos), "helper")
			require.NotNil(t, item)
			assert.Equal(t, tt.wantKind, item.Kind)
			data := requireValueAs[*CompletionItemData](t, item.Data)
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
		source, pos := typeDisplayTestSource(t, `import explicit "example.com/support/v2"
echo explicit.Value
|
`)
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
		source, pos := typeDisplayTestSource(t, `echo 1
|
`)
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
			"main_fixture.gox": []byte(`import helper "example.com/alternate"
echo helper.Value
`),
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
		config := multiFrameworkTestModule()
		config.Opt.Projects[0].Import = []*modfile.Import{{Name: "helper", Path: "example.com/second"}}
		config.Opt.Projects[1].Import = []*modfile.Import{{Name: "helper", Path: "example.com/first"}}
		s := newMultiFrameworkTestServerWithModule(t, map[string][]byte{
			"First.first": []byte(`var value helper.Item
echo value
`),
			"Second.second": []byte(`var value helper.Item
echo value
`),
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
		{"Shadowed", `import . "example.com/support/v2"
func run() {
Value := 1
const Default = 1
echo Value, Default, "text"
}
`, false},
		{"Ambiguous", `import . "example.com/support/v2"
import . "example.com/alternate"
echo "text"
`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(tt.source)}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			ctx := inputSlotTestContext(t, s, "main_fixture.gox")
			pkg, err := ctx.proj.Import(importTestPkgPath)
			require.NoError(t, err)
			lit := inputSlotLiteral(t, ctx, `"text"`)
			names := collectPredefinedNames(ctx, XGoInputSlotKindValue, lit, pkg.Scope().Lookup("Name").Type())
			if tt.want {
				assert.Contains(t, names, "Value")
				assert.Contains(t, names, "Default")
				addresses := collectPredefinedNames(ctx, XGoInputSlotKindAddress, lit, nil)
				assert.Contains(t, addresses, "Value")
				assert.NotContains(t, addresses, "Default")
			} else {
				assert.NotContains(t, names, "Value")
				assert.NotContains(t, names, "Default")
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
		{"LocalValue", `func run() {
helper := 1
var value |helper.Name
echo helper, value
}
`, ""},
		{"ClassField", `var (
helper int
value |helper.Name
)
echo value
`, ""},
		{"PackageValue", "var value |helper.Name\necho value\n", "var helper = 1\n"},
		{"TypeArgument", `func run() {
helper := 1
var value Box[|helper.Name]
echo helper, value
}
`, ""},
		{"Initializer", `func run() {
helper := 1
var value |helper.Name = "text"
echo helper, value
}
`, ""},
		{"Parameter", "func run(helper int, value |helper.Name) { echo helper, value }\n", ""},
		{"Result", "func run(helper int) |helper.Name { return \"text\" }\n", ""},
		{"TypeAlias", `func run() {
helper := 1
type Name = |helper.Name
var value Name
echo helper, value
}
`, ""},
		{"CompositeLiteral", `func run() {
helper := 1
value := []|helper.Name{}
echo helper, value
}
`, ""},
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
					source, pos := typeDisplayTestSource(t, prefix+"func run() {\n"+tt.local+`var fn func(Name) Name = |nil
echo fn
}
`)
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
			source, pos := typeDisplayTestSource(t, `import . "example.com/callbacks"
func run() {
var fn func(string) string = |nil
echo fn
}
`)
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
			source, pos := typeDisplayTestSource(t, "import . \"example.com/support/v2\"\n"+tt.globals+"func run() {\n"+tt.local+`var value |bool
echo value
}
`)
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", pos), tt.label)
			insertion := tt.label
			if tt.wantPath != "" {
				require.NotNil(t, item)
				assert.Equal(t, ClassCompletion, item.Kind)
				data := requireValueAs[*CompletionItemData](t, item.Data)
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
		source, pos := typeDisplayTestSource(t, `import . "example.com/support/v2"
var value |error
echo value
`)
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
					source, pos := typeDisplayTestSource(t, `import . "example.com/callbacks"
func run() {
var fn `+tt.callback+" = "+markedValue+"\necho fn\n}\n")
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
			source, pos := typeDisplayTestSource(t, "import "+tt.alias+" \"example.com/support/v2\"\nfunc run() {\n"+tt.local+`var value any = |nil
echo value
}
`)
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
		{"OverloadedProperty", `func (a *App) Label__0() string { return "" }
func (a *App) Label__1(value int) string { return "" }
`, false},
		{"OverloadedArguments", `func (a *App) Label__0(value string) string { return value }
func (a *App) Label__1(value int) string { return "" }
`, true},
		{"TemplateProperty", "func XGot_App_Label(a *App) string { return \"\" }\n", false},
		{"TemplateArgument", "func XGot_App_Label(a *App, value int) string { return \"\" }\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import label "example.com/support/v2"
func run() {
var value any = |nil
echo value
}
`)
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
			source, pos := typeDisplayTestSource(t, prefix+`var value helper.Name
echo value, "text"
|
`)
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
	source, pos := typeDisplayTestSource(t, `import . "example.com/support/v2"
import . "example.com/alternate"
echo 1
|
`)
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	for _, name := range []string{"Name", "Value", "Default", "accept"} {
		assert.NotContains(t, labels, name)
	}
	t.Run("FunctionAndType", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, `import . "example.com/support/v2"
import . "example.com/types"
echo 1
|
`)
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
				source, pos := typeDisplayTestSource(t, `import . "example.com/support/v2"
import . "example.com/types"
func run() {
`+tt.source+"}\n")
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
		{"Unqualified", `import . "example.com/extension"
echo 1
|
`, ""},
		{"Qualified", "import ext \"example.com/extension\"\necho ext.|create(int, \"text\")\n", ""},
		{"Receiver", `import ext "example.com/extension"
var value ext.Other
value.|accept()
`, "example.com/extension"},
		{"FunctionFirst", `import . "example.com/support/v2"
import . "example.com/extension"
echo accept("text")
|
`, importTestPkgPath},
		{"FunctionLast", `import . "example.com/extension"
import . "example.com/support/v2"
echo accept("text")
|
`, importTestPkgPath},
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
				data := requireValueAs[*CompletionItemData](t, item.Data)
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
		source, pos := typeDisplayTestSource(t, `import ext "example.com/extension"
ext.|xGot_Other_Accept()
`)
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		proj := s.getProj()
		file, err := goparser.ParseFile(proj.Fset, "extension.go", `package extension
func XGot_Other_Accept() {}
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
		{"ExplicitAlias", `import h "example.com/support/v2"
var value h.Name
echo |value
`, "helper", "h.Name", "h.Name"},
		{"ExplicitOverride", `import helper "example.com/alternate"
var value helper.Name
echo |value
`, "helper", "helper.Name", "helper.Name"},
		{"LocalShadow", `var value helper.Name
func run() {
helper := 1
echo helper, |value
}
`, "helper", "support.Name", ""},
		{"MemberShadow", `var (
value helper.Name
helper int
)
echo |value
`, "helper", "helper.Name", ""},
		{"DotImport", `import . "example.com/support/v2"
var value Name
echo |value
`, "helper", "Name", "Name"},
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
		s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(`import local "example.com/support/v2"
echo local.Value
`), Version: 1}})
		changed := s.requestProject()
		changedFile, err := changed.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		assert.Contains(t, importsForFile(changed, changedFile).named, "local")
		assert.NotContains(t, updated.named, "local")
	})
}

func TestServerImportedExtendedMembers(t *testing.T) {
	const memberDoc = "Member documentation."
	for _, tt := range []struct {
		name, marker, additionalDecl, decl, body string
		owner, label, overview, signature        string
		templateName, completion, overloadID     string
		checkCompletionOverview                  bool
	}{
		{
			name: "StringPackageMarker", marker: `const XGoPackage = "example.com/support/v2"`,
			decl: "func XGot_Item_Choose(item *Item, value int) int { return value }",
			body: "var item f.Item\n_ = item.ch|oose(1)", owner: "Item", label: "choose",
			overview: "func choose(value int) int", signature: "choose(value int) int", templateName: "Choose",
		},
		{
			name: "HistoricalMarkerAndEscapedTemplate", marker: "const GopPackage = true",
			additionalDecl: "type Item_Name struct{}",
			decl:           "func Gopt__Item_Name__Choose_Value(item *Item_Name, value int) int { return value }",
			body:           "var item f.Item_Name\n_ = item.choose_|Value(1)", owner: "Item_Name", label: "choose_Value",
			overview: "func choose_Value(value int) int", signature: "choose_Value(value int) int", templateName: "Choose_Value",
		},
		{
			name: "HistoricalTypeAsParameter", marker: "const XGoPackage = true",
			decl: "func Gopx_Choose[T any](value T) T { return value }",
			body: "_ = f.ch|oose(int, 1)", label: "choose", overview: "func choose(T Type, value T) T",
			signature: "choose(T Type, value T) T",
		},
		{
			name: "OrdinaryGeneric", marker: "const XGoPackage = true",
			decl: "func Choose[T any](value T) T { return value }",
			body: "_ = f.Ch|oose[int](1)", label: "choose", overview: "func choose[T any](value T) T",
			signature: "choose(value int) int",
		},
		{
			name: "OrdinaryGenericTemplate", marker: "const XGoPackage = true",
			decl: "func XGot_Item_Choose[T any](item Item, value T) T { return value }",
			body: "var item f.Item\n_ = item.ch|oose(1)", owner: "Item", label: "choose",
			overview: "func choose[T any](value T) T", signature: "choose(value T) T",
		},
		{
			name: "EncodedTemplateSelector", marker: "const XGoPackage = true",
			decl: "func XGot_Item_XGox_Choose[T any](item Item, value T) T { return value }",
			body: "var item f.Item\n_ = item.XGox_Ch|oose(1)", owner: "Item", label: "choose",
			overview: "func XGox_Choose[T any](value T) T", signature: "XGox_Choose(value T) T",
		},
		{
			name: "OverloadFromNamedFunctions", marker: "const XGoPackage = true",
			decl: `func IntValue(value int) int { return value }
func StringValue(value string) string { return value }
const XGoo_Choose = "IntValue,StringValue"`,
			body: "_ = f.ch|oose(1)", label: "intValue", completion: "choose",
			overview: "func choose(value int) int", signature: "choose(value int) int",
			checkCompletionOverview: true,
		},
		{
			name: "DirectOverloadCandidate", marker: "const XGoPackage = true",
			decl: `func IntValue(value int) int { return value }
func StringValue(value string) string { return value }
const XGoo_Choose = "IntValue,StringValue"`,
			body: "_ = f.IntV|alue(1)", label: "intValue",
			overview: "func intValue(value int) int", signature: "intValue(value int) int",
		},
		{
			name: "EncodedGenericFunction", marker: "const XGoPackage = true",
			decl: "func Gopx_Choose[T any](value T) T { return value }",
			body: "_ = f.Gopx_Ch|oose[int](1)", label: "Gopx_Choose", overview: "func Gopx_Choose[T any](value T) T",
			signature: "Gopx_Choose(value int) int", completion: "gopx_Choose",
		},
		{
			name: "StaticFunction", marker: "const XGoPackage = true",
			decl: "func XGos_Item_Choose(value int) int { return value }",
			body: "_ = f.Item.ch|oose(1)", owner: "Item", label: "choose",
			overview: "func choose(value int) int", signature: "choose(value int) int",
		},
		{
			name: "EncodedStaticFunction", marker: "const GopPackage = true",
			decl: "func Gops_Item_Choose(value int) int { return value }",
			body: "_ = f.Gops_Item_Ch|oose(1)", label: "Gops_Item_Choose", completion: "gops_Item_Choose",
			overview: "func Gops_Item_Choose(value int) int", signature: "Gops_Item_Choose(value int) int",
		},
		{
			name: "SelectedStaticOverload", marker: "const XGoPackage = true",
			decl: `func XGos_Item_Choose__0(value int) int { return value }
func XGos_Item_Choose__1(value string) string { return value }`,
			body: "_ = f.Item.ch|oose(1)", owner: "Item", label: "choose", overloadID: "#0",
			overview: "func choose(value int) int", signature: "choose(value int) int",
		},
		{
			name: "StaticFunctionValue", marker: "const XGoPackage = true",
			decl: "func XGos_Item_Choose(value int) int { return value }",
			body: "var callback func(int) int = f.Item.Ch|oose", owner: "Item", label: "choose",
			overview: "func choose(value int) int", signature: "Choose(value int) int", completion: "Choose",
		},
		{
			name: "EscapedHistoricalStaticConstant", marker: "const GopPackage = true",
			additionalDecl: "type Item_Name int", decl: "const Gops__Item_Name__Max_Size = 3",
			body: "_ = f.Item_Name.Max_|Size", owner: "Item_Name", label: "Max_Size", overview: "const Max_Size = 3",
		},
		{
			name: "StaticVariable", marker: "const XGoPackage = true", decl: "var XGos_Item_limit = 3",
			body: "f.Item.li|mit = 4", owner: "Item", label: "limit", overview: "var limit int",
		},
		{
			name: "StaticCallbackVariable", marker: "const XGoPackage = true",
			decl: "var XGos_Item_Callback = func(value int) int { return value }",
			body: "_ = f.Item.Call|back(1)", owner: "Item", label: "Callback",
			overview: "var Callback func(value int) int", signature: "Callback(value int) int",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const path = testframework.PkgPath
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"`+"\n"+tt.body+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			proj := s.getProj()
			goFile, err := goparser.ParseFile(proj.Fset, "framework.go",
				"package framework\n"+tt.marker+"\ntype Item struct{}\n"+tt.additionalDecl+"\n"+
					"// "+memberDoc+"\n"+tt.decl+"\n", goparser.ParseComments)
			require.NoError(t, err)
			dep, err := new(gotypes.Config).Check(path, proj.Fset, []*goast.File{goFile}, nil)
			require.NoError(t, err)
			fallback := proj.Importer
			proj.Importer = testImporterFunc(func(importPath string) (*gotypes.Package, error) {
				if importPath == path {
					return dep, nil
				}
				return fallback.Import(importPath)
			})
			doc := pkgdoc.NewGo(path, &goast.Package{Name: "framework", Files: map[string]*goast.File{"framework.go": goFile}})
			lookup := s.lookupPkgDoc
			s.lookupPkgDoc = func(importPath string) (*pkgdoc.PkgDoc, error) {
				if importPath == path {
					return doc, nil
				}
				return lookup(importPath)
			}
			if tt.templateName != "" {
				typeDoc := doc.Types[tt.owner]
				require.NotNil(t, typeDoc)
				assert.Equal(t, memberDoc+"\n", typeDoc.Methods[tt.templateName])
			}

			_, err = proj.TypeInfo()
			require.NoError(t, err)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			idName := tt.label
			if tt.owner != "" {
				idName = tt.owner + "." + tt.label
			}
			assert.Contains(t, hover.Contents.Value, `def-id="xgo:`+path+"?"+idName+tt.overloadID+`"`)
			assert.Contains(t, hover.Contents.Value, `overview="`+tt.overview+`"`)
			assert.Contains(t, hover.Contents.Value, memberDoc)

			items := completionItemsAt(t, s, "main.xgo", pos)
			wantLabel := tt.completion
			if wantLabel == "" {
				wantLabel = tt.label
			}
			assert.Contains(t, completionItemLabels(items), wantLabel)
			if tt.checkCompletionOverview {
				var matched bool
				for _, item := range items {
					if item.Label != wantLabel {
						continue
					}
					data := requireValueAs[*CompletionItemData](t, item.Data)
					require.NotNil(t, data.Definition)
					require.NotNil(t, data.Definition.Name)
					if *data.Definition.Name != idName {
						continue
					}
					require.NotNil(t, item.Documentation)
					doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
					assert.Contains(t, doc.Value, `overview="`+tt.overview+`"`)
					matched = true
				}
				assert.True(t, matched)
			}

			if tt.signature != "" {
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				assert.Equal(t, tt.signature, help.Signatures[0].Label)
			}
		})
	}
}

func TestServerStaticValueCompletionRequiresType(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
_ = item.Li|mit
`)
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	t.Cleanup(s.Close)
	setImportTestFrameworkMethods(t, s, "const XGos_Item_Limit = 3\n")

	_, err := s.getProj().TypeInfo()
	require.ErrorContains(t, err, "Limit")
	assert.NotContains(t, completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), "Limit")
}

func TestServerTemplateMethodAccess(t *testing.T) {
	for _, tt := range []struct {
		name, receiver, call, signature, overview, completion string
		checkPreview                                          bool
	}{
		{name: "Type", receiver: "Item", call: "f.Item.ch|oose(&item, 1)",
			signature: "choose(item *f.Item, value int) int", overview: "func choose(item *f.Item, value int) int",
			completion: "choose", checkPreview: true},
		{name: "Instance", receiver: "Item", call: "item.ch|oose(1)",
			signature: "choose(value int) int", overview: "func choose(value int) int", completion: "choose", checkPreview: true},
		{name: "DirectImplementation", receiver: "Item", call: "f.Gopt_Item_Ch|oose(&item, 1)",
			signature: "Gopt_Item_Choose(item *f.Item, value int) int", overview: "func Gopt_Item_Choose(item *f.Item, value int) int"},
		{name: "IndependentFunction", receiver: "Missing", call: "f.gopt_Missing_Ch|oose(&item, 1)",
			signature: "gopt_Missing_Choose(item *f.Item, value int) int",
			overview:  "func gopt_Missing_Choose(item *f.Item, value int) int", completion: "gopt_Missing_Choose"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
_ = `+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, "func Gopt_"+tt.receiver+"_Choose(item *Item, value int) int { return value }\n")
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `overview="`+tt.overview+`"`)
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.signature, help.Signatures[0].Label)
			if tt.completion != "" {
				item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", pos), tt.completion)
				require.NotNil(t, item)
				if tt.checkPreview {
					require.NotNil(t, item.Documentation)
					doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
					assert.Contains(t, doc.Value, `overview="`+tt.overview+`"`)
				}
			}
		})
	}
}

func TestServerBareGenericSignatures(t *testing.T) {
	const declarations = `func Pair[A, B any](first A, second B) B { return second }
func XGot_Item_Choose[T any](item Item, value T) T { return value }
func XGot_Item_XGox_Select[A, B any](item Item, key A, value B) B { return value }
func XGos_Item_Pick[A, B any](key A, value B) B { return value }
func Restricted[A ~int, B any](key A, value B) B { return value }
`
	for _, tt := range []struct {
		name, expr, annotation, signature, overview, completion, wantError string
	}{
		{name: "PackageFunction", expr: "f.Pa|ir[int, string]", signature: "pair(first int, second string) string"},
		{name: "Template", expr: "(f.Item.Ch|oose[int])", signature: "Choose(item f.Item, value int) int",
			overview: "func Choose(item f.Item, value int) int"},
		{name: "EncodedTemplate", expr: "f.Item.XGox_Se|lect[int, string]", signature: "XGox_Select(item f.Item, key int, value string) string",
			overview: "func XGox_Select(item f.Item, key int, value string) string"},
		{name: "StaticMethod", expr: "f.Item.Pi|ck[int, string]", signature: "Pick(key int, value string) string"},
		{name: "PartiallyInstantiatedTemplate", expr: "f.Item.XGox_Se|lect[int]", annotation: "func(f.Item, int, string) string",
			signature: "XGox_Select(item f.Item, key A, value B) B", overview: "func XGox_Select[A any, B any](item f.Item, key A, value B) B",
			completion: "XGox_Select"},
		{name: "PartiallyInstantiatedStatic", expr: "f.Item.Pi|ck[int]", annotation: "func(int, string) string",
			signature: "Pick(key A, value B) B", completion: "Pick"},
		{name: "InvalidConstraint", expr: "f.Res|tricted[string, int]", signature: "restricted(key A, value B) B",
			wantError: "string does not satisfy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			declaration := "var fn"
			if tt.annotation != "" {
				declaration += " " + tt.annotation
			}
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"`+"\n"+declaration+" = "+tt.expr+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			_, err := s.requestProject().TypeInfo()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.signature, help.Signatures[0].Label)
			if tt.overview != "" {
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, `overview="`+tt.overview+`"`)
			}
			if tt.completion != "" {
				assert.Contains(t, completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), tt.completion)
			}
		})
	}
}

func TestServerBoundTemplateFunctionValueCompletion(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
var callback func(int) int = item.Tem|plate
`)
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	t.Cleanup(s.Close)
	setImportTestFrameworkMethods(t, s, `func XGot_Item_Template(item Item, value int) int { return value }
func (Item) TemplateCallback(value int) int { return value }
`)
	_, err := s.requestProject().TypeInfo()
	require.ErrorContains(t, err, "cannot use item.Template")

	items := completionItemsAt(t, s, "main.xgo", pos)
	labels := completionItemLabels(items)
	assert.NotContains(t, labels, "Template")
	assert.NotContains(t, labels, "template")
	callback := completionItemByLabel(items, "TemplateCallback")
	require.NotNil(t, callback)
	assert.Equal(t, "TemplateCallback", callback.InsertText)
	updated := strings.Replace(source, "item.Template", "item."+callback.InsertText, 1)
	s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
	requireNoDiagnostics(t, s)
}

func TestServerTemplateTypeArgumentAliasCompletion(t *testing.T) {
	const declarations = `func XGot_Item_XGox_Choose[A, B any](item Item, key A, value B) B { return value }
func XGot_Item_XGox_Pick__0[T any](item Item, value T) T { return value }
func XGot_Item_XGox_Pick__1[T any](item Item, value T, extra int) T { return value }
`
	for _, tt := range []struct {
		name, call, label, hidden string
	}{
		{name: "BoundAlias", call: `item.ch|oose(int, 1, "text")`, label: "choose"},
		{name: "UnboundEncoded", call: `f.Item.XGox_Ch|oose[int, string](item, 1, "text")`, label: "xGox_Choose", hidden: "choose"},
		{name: "OverloadedBoundAlias", call: `item.pi|ck(int, 1)`, label: "pick"},
		{name: "OverloadedUnboundEncoded", call: `f.Item.XGox_Pi|ck(item, 1)`, label: "xGox_Pick", hidden: "pick"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
_ = `+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, tt.label)
			if tt.hidden != "" {
				assert.NotContains(t, labels, tt.hidden)
			}
		})
	}
}

func TestServerIndexedMemberCompletion(t *testing.T) {
	const declarations = `func XGot_Item_Choose[A, B any](item Item, key A, value B) B { return value }
func XGot_Item_Restricted[A ~int, B any](item Item, key A, value B) B { return value }
func XGot_Item_Ordinary(item Item, key int, value string) string { return value }
func XGot_Item_PairResults[A, B any](item Item, key A, value B) (A, B) { return key, value }
func XGos_Item_Pick[A, B any](key A, value B) B { return value }
func XGox_Pair[A, B any](key A, value B) B { return value }
func PairOf[A, B any](key A, value B) B { return value }
func PairResults[A, B any](key A, values ...B) (A, B) { return key, values[0] }
func Caption() string { return "text" }
type CaptionFunc func() string
var NamedCaption CaptionFunc
func DeferredCaption(__xgo_autoclosure_text func() string) string { return __xgo_autoclosure_text() }
func Flag() bool { return true }
type CaptionPointer *string
func TextPointer() CaptionPointer { return nil }
func FixedValues() [4]int { return [4]int{} }
func Labels() map[any]string { return nil }
type Entry struct{ Name string }
func CurrentEntry() Entry { return Entry{} }
type Key int
func (Key) XGo_Add(delta int) string { return "label" }
func (Key) XGo_Lsh(count int) string { return "label" }
type ShiftCount int
func shiftFrom(value int, count ShiftCount) string { return "label" }
const XGoo__ShiftCount__XGo_Rsh = "shiftFrom"
func (Item) Caption() string { return "item" }
func (App) Caption() string { return "app" }
func (Item) XGo_Add(suffix string) string { return suffix }
func (Item) XGo_Neg() string { return "item" }
type CaptionText string
func (CaptionText) XGo_Add(count int) CaptionText { return "text" }
type Converted struct{ Value int }
func Converted_Cast(value int) Converted { return Converted{value} }
type ConvertedLabels struct{ Labels []string }
type ConvertedPair struct{ Pair PairTuple }
type ConvertedEntry struct{ Entry Entry }
type ConvertedMap map[string]string
type ConvertedEntries map[string]Entry
type ConvertedNested struct {
	Entries []*Entry
	Indexed [2]Entry
	ByName map[string]Entry
	Extra any
}
type Tagged struct{ Count int }
func Tagged_Cast(count int, options map[string]string) Tagged { return Tagged{count} }
type Blend struct{ Count int }
func Blend_Cast(count int, labels ...string) Blend { return Blend{count} }
type Reverse struct{}
func (Reverse) XGo_Rcast() string { return "text" }
type Limited int
const Limited_Max = 2
type PairTuple struct{ X_0 int; X_1 bool }
type TextList []string
type TextListAlias = TextList
func PairTupleResults[A, B any](pair PairTuple, values ...B) (PairTuple, B) { return pair, values[0] }
func PairOptions[A, B any](key A, options map[string]B) (A, B) { return key, options["label"] }
func PairMixedOptions[A, B any](key A, value B, options map[string]any) (A, B) { return key, value }
func CallbackOf[T any](value T) func() T { return func() T { return value } }
func Ordinary(key int, value string) string { return value }
func Int() int { return 0 }
func String(value string) string { return value }
type Box[T ~int] struct{ Value T }
type BoxAlias[T ~int] = Box[T]
type PairType[A, B any] struct {
	Key A
	Value B
}
type Choice__0[T ~int] struct{ Number T }
type Choice__1[T ~string] struct{ Text T }
type Choice__2[T ~int] struct{ Other T }
func XGox_Choice_Cast__0[T ~int](value T) *Choice__0[T] { return &Choice__0[T]{value} }
func XGox_Choice_Cast__1[T ~string](value T) *Choice__1[T] { return &Choice__1[T]{value} }
type Holder__0[T Choice__0[int]] struct{ Value T }
type Holder__1[T Choice__1[string]] struct{ Value T }
const BlockSize = 4
var Values = []int{1}
var XGos_Item_Values = []int{1}
var XGos_Item_Array [4]int
var XGos_Item_Entries [4]Entry
const XGos_Item_Label = "text"
const XGos_Item_Offset = 2
const XGos_Item_NegativeOffset = -1
`
	for _, tt := range []struct {
		name, source, prefix, want string
		filename, selectedType     string
		hidden                     []string
	}{
		{name: "Template", source: `var fn func(f.Item, int, string) string = f.Item.Ch|[int, string]`,
			prefix: "Ch", want: "Choose", hidden: []string{"ordinary", "Ordinary"}},
		{name: "WrongOrder", source: `var fn func(f.Item, int, string) string = f.Item.Ch|[string, int]`,
			hidden: []string{"choose", "Choose"}},
		{name: "Slice", source: `var fn func(f.Item, []int, string) string = f.Item.Ch|[[]int, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "Pointer", source: `var fn func(f.Item, *f.Item, string) string = f.Item.Ch|[*f.Item, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "NestedType", source: `var fn func(f.Item, f.Box[int], string) string = f.Item.Ch|[f.Box[int], string]`,
			prefix: "Ch", want: "Choose", hidden: []string{"ordinary", "Ordinary"}},
		{name: "NestedTypeWrongCallback", source: `var fn func(f.Item, int, string) string = f.Item.Ch|[f.Box[int], string]`,
			hidden: []string{"choose", "Choose"}},
		{name: "NestedTypeAlias", source: `var fn func(f.Item, f.Box[int], string) string = f.Item.Ch|[f.BoxAlias[int], string]`,
			prefix: "Ch", want: "Choose"},
		{name: "NestedTypeList", source: `var fn func(f.Item, *f.PairType[int, string], string) string = f.Item.Ch|[*f.PairType[int, string], string]`,
			prefix: "Ch", want: "Choose"},
		{name: "NestedMapType", source: `var fn func(f.Item, map[string]f.Box[int], string) string = f.Item.Ch|[map[string]f.Box[int], string]`,
			prefix: "Ch", want: "Choose"},
		{name: "NestedChannelType", source: `var fn func(f.Item, <-chan f.Box[int], string) string = f.Item.Ch|[<-chan f.Box[int], string]`,
			prefix: "Ch", want: "Choose"},
		{name: "NestedSendChannelType", source: `var fn func(f.Item, chan<- f.Box[int], string) string = f.Item.Ch|[chan<- f.Box[int], string]`,
			prefix: "Ch", want: "Choose"},
		{name: "NestedChannelDirectionMismatch", source: `var fn func(f.Item, chan f.Box[int], string) string = f.Item.Ch|[<-chan f.Box[int], string]`,
			hidden: []string{"choose", "Choose"}},
		{name: "FixedArray", source: `var fn func(f.Item, [4]f.Box[int], string) string = f.Item.Ch|[[4]f.Box[int], string]`,
			prefix: "Ch", want: "Choose"},
		{name: "LocalArrayBound", source: `const size = 2
func check() {
	const size = 4
	var fn func(f.Item, [4]int, string) string = f.Item.Ch|[[size]int, string]
	_ = fn
}`, prefix: "Ch", want: "Choose"},
		{name: "ImportedArrayBound", source: `var fn func(f.Item, [4]int, string) string = f.Item.Ch|[[f.BlockSize]int, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "StaticArrayBound", source: `var fn func(f.Item, [2]int, string) string = f.Item.Ch|[[f.Item.Offset]int, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "ComputedStaticArrayBound", source: `var fn func(f.Item, [4]int, string) string = f.Item.Ch|[[1<<f.Item.Offset]int, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "WrongStaticArrayBound", source: `var fn func(f.Item, [3]int, string) string = f.Item.Ch|[[f.Item.Offset]int, string]`,
			hidden: []string{"choose", "Choose"}},
		{name: "WrongArrayBound", source: `var fn func(f.Item, [2]int, string) string = f.Item.Ch|[[f.BlockSize]int, string]`,
			hidden: []string{"choose", "Choose"}},
		{name: "ComputedArrayBound", source: `const size = 2
func check() {
	const size = 1
	var fn func(f.Item, [4]int, string) string = f.Item.Ch|[[f.BlockSize*size]int, string]
	_ = fn
}`, prefix: "Ch", want: "Choose"},
		{name: "WrongComputedArrayBound", source: `var fn func(f.Item, [2]int, string) string = f.Item.Ch|[[1<<2]int, string]`,
			hidden: []string{"choose", "Choose"}},
		{name: "StructType", source: `type Count int
var fn func(f.Item, struct{item f.Item; left, right Count}, string) string = f.Item.Ch|[struct{item f.Item; left, right Count}, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "StructWithShorthandTag", source: `var fn func(f.Item, struct{ Count int "counter" }, string) string = f.Item.Ch|[struct{ Count int "counter" }, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "StructWithStaticArrayBound", source: `var fn func(f.Item, struct{ values [3]int }, string) string =
	f.Item.Ch|[struct{ values [f.Item.Offset-f.Item.NegativeOffset]int }, string]`, prefix: "Ch", want: "Choose"},
		{name: "StructWithComputedArrayBound", source: `import . "example.com/framework"
func check() {
	const Item = 2
	var fn func(f.Item, struct{values [4]int}, string) string = f.Item.Ch|[struct{values [Item<<1]int}, string]
	_ = fn
}`, prefix: "Ch", want: "Choose"},
		{name: "StructWithShadowedElementType", source: `import . "example.com/framework"
func check() {
	const Item = 2
	var fn func(f.Item, struct{first [2]f.Item; second [3]f.Item}, string) string =
		f.Item.Ch|[struct{first [Item]Item; second [Item+1]Item}, string]
	_ = fn
}`, prefix: "Ch", want: "Choose"},
		{name: "WrongStructType", source: `var fn func(f.Item, struct{ Count int }, string) string = f.Item.Ch|[struct{ Count string }, string]`,
			hidden: []string{"choose", "Choose"}},
		{name: "InterfaceType", source: `type Reader interface{ Read() f.Item }
var fn func(f.Item, interface{ Reader; Close() }, string) string = f.Item.Ch|[interface{ Reader; Close() }, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "WrongInterfaceType", source: `var fn func(f.Item, interface{ Read() int }, string) string = f.Item.Ch|[interface{ Read() string }, string]`,
			hidden: []string{"choose", "Choose"}},
		{name: "FunctionType", source: `var fn func(f.Item, func(string, string) (int, bool), string) string = f.Item.Ch|[func(left, right string) (count int, ok bool), string]`,
			prefix: "Ch", want: "Choose"},
		{name: "VariadicFunctionType", source: `var fn func(f.Item, func(...string), string) string = f.Item.Ch|[func(values ...string), string]`,
			prefix: "Ch", want: "Choose"},
		{name: "WrongVariadicFunctionType", source: `var fn func(f.Item, func(...string), string) string = f.Item.Ch|[func(values []string), string]`,
			hidden: []string{"choose", "Choose"}},
		{name: "DotImport", source: "import . \"example.com/support/v2\"\n" +
			`var fn func(f.Item, Name, string) string = f.Item.Ch|[Name, string]`, prefix: "Ch", want: "Choose"},
		{name: "DotImportPropertyIndex", source: "import . \"example.com/framework\"\n" +
			`var value = f.Va|[int]`, prefix: "Va", want: "Values"},
		{name: "ClassLookupPropertyIndex", filename: "main_fixture.gox",
			source: `var value = f.Item.Va|[int]`, prefix: "Va", want: "Values"},
		{name: "DotImportNonProperty", source: "import . \"example.com/framework\"\n" +
			`var fn func(f.Item, string, string) string = f.Item.Ch|[string, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "LocalTypeShadowsDotImport", source: "import . \"example.com/framework\"\ntype int string\n" +
			`var fn func(f.Item, int, string) string = f.Item.Ch|[int, string]`, prefix: "Ch", want: "Choose"},
		{name: "DotImportSliceType", source: "import . \"example.com/framework\"\n" +
			`var fn func(f.Item, []int, string) string = f.Item.Ch|[[]int, string]`, prefix: "Ch", want: "Choose"},
		{name: "ClassAutoImport", filename: "main_fixture.gox",
			source: `var fn func(f.Item, helper.Name, string) string = f.Item.Ch|[helper.Name, string]`, prefix: "Ch", want: "Choose"},
		{name: "PackageEncoded", source: `var fn func(int, string) string = f.XGox_Pa|[int, string]`,
			prefix: "XGox_Pa", want: "XGox_Pair", hidden: []string{"ordinary", "Ordinary", "Pair", "pair"}},
		{name: "PackageEncodedCall", source: `var value = f.XGox_Pa|[int, string](1, "text")`,
			prefix: "XGox_Pa", want: "xGox_Pair", hidden: []string{"pair"}},
		{name: "PackageOrdinaryCall", source: `var value = f.PairO|[int, string](1, "text")`,
			prefix: "PairO", want: "pairOf"},
		{name: "PackagePartialCallResults", source: `func use(number int, text string) {}
use f.PairRes|[int](1, "text")`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallProperty", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.caption)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallAliasedCall", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.caption())`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallAliasedGenericCall", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.pairOf[int](2, "text"))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallWrongAliasedCall", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.flag())`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallNamedFunctionAlias", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.namedCaption())`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallAutoclosureAlias", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.deferredCaption(func() string { return "text" }))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallMissingAlias", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.missingCaption())`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallBinaryProperty", source: `func use(number int, text string) {}
use f.PairRes|[int](1, (f.caption + "!") + f.Item.Label)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallBinaryOperator", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.Item{} + f.caption)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallExplicitCast", source: `func use(number int, value f.Converted) {}
use f.PairRes|[int](1, f.Converted(f.Item.Offset))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStructKeywordCast", source: `func use(number int, value f.Converted) {}
use f.PairRes|[int](1, f.Converted(value = f.Item.Offset))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallLocalStructKeywordCast", source: `type options struct{ value int }
func use(number int, value options) {}
use f.PairRes|[int](1, options(value = f.Item.Offset))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallShortUnicodeKeywordCast",
			source: "type options struct{ \u2c7e int }\n" +
				"func use(number int, value options) {}\nuse f.PairRes|[int](1, options(\u023f = 1))",
			hidden: []string{"pairResults"}},
		{name: "PackagePartialCallUnicodeKeywordCast",
			source: "type options struct{ \u2c7ecore int }\n" +
				"func use(number int, value options) {}\nuse f.PairRes|[int](1, options(\u023fcore = 1))",
			prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallPointerStructKeywordCast", source: `func use(number int, value *f.Converted) {}
use f.PairRes|[int](1, (*f.Converted)(value = f.Item.Offset))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStructKeywordLiteralCast", source: `func use(number int, value f.ConvertedLabels) {}
use f.PairRes|[int](1, f.ConvertedLabels(labels = []))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStructKeywordTupleCast", source: `func use(number int, value f.ConvertedPair) {}
use f.PairRes|[int](1, f.ConvertedPair(pair = (f.Item.Offset, true)))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStructKeywordCompositeCast", source: `func use(number int, value f.ConvertedEntry) {}
use f.PairRes|[int](1, f.ConvertedEntry(entry = {Name: f.Item.Label}))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallNestedKeywordCompositeCast", source: `func use(number int, value f.ConvertedNested) {}
use f.PairRes|[int](1, f.ConvertedNested(
	entries = {{Name: f.Item.Label}},
	indexed = {1: {Name: f.Item.Label}},
	byName = {f.caption: {Name: f.Item.Label}},
	extra = {"name": f.Item.Label},
))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallInvalidKeywordCompositeCast", source: `func use(number int, value f.ConvertedEntry) {}
use f.PairRes|[int](1, f.ConvertedEntry(entry = {Name: f.Item.Offset}))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallUnknownKeywordCompositeField", source: `func use(number int, value f.ConvertedEntry) {}
use f.PairRes|[int](1, f.ConvertedEntry(entry = {Missing: f.Item.Label}))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallInvalidKeywordCompositeIndex", source: `func use(number int, value f.ConvertedNested) {}
use f.PairRes|[int](1, f.ConvertedNested(indexed = {2: f.Entry{Name: f.Item.Label}}))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallInvalidKeywordCompositeKey", source: `func use(number int, value f.ConvertedNested) {}
use f.PairRes|[int](1, f.ConvertedNested(byName = {f.Item.Offset: {Name: f.Item.Label}}))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallUnknownStructKeywordCast", source: `func use(number int, value f.Converted) {}
use f.PairRes|[int](1, f.Converted(missing = f.Item.Offset))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallUnresolvedStructKeywordValue", source: `func use(number int, value f.Converted) {}
use f.PairRes|[int](1, f.Converted(value = missing))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallInvalidStructKeywordValue", source: `func use(number int, value f.Converted) {}
use f.PairRes|[int](1, f.Converted(value = f.Item.Label+1))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallMapKeywordCast", source: `func use(number int, value f.ConvertedMap) {}
use f.PairRes|[int](1, f.ConvertedMap(label = f.caption))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallMapKeywordCompositeCast", source: `func use(number int, value f.ConvertedEntries) {}
use f.PairRes|[int](1, f.ConvertedEntries(label = {Name: f.Item.Label}))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallWrongMapKeywordCast", source: `func use(number int, value f.ConvertedMap) {}
use f.PairRes|[int](1, f.ConvertedMap(label = f.Item.Offset))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallInferredKeywordCast", source: `func use(number int, value f.Tagged) {}
use f.PairRes|[int](1, f.Tagged(f.Item.Offset, label = f.caption))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallZeroValueCast", source: `func use(number int, value f.Converted) {}
use f.PairRes|[int](1, f.Converted())`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallMultipleCastOperands", source: `func use(number int, value f.Blend) {}
use f.PairRes|[int](1, f.Blend(f.Item.Offset, f.caption))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallExpandedTupleCast", source: `func use(number int, value f.Blend) {}
use f.PairRes|[int](1, f.Blend((f.Item.Offset, f.caption)))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallRetainedTupleCast", source: `func use(number int, value f.PairTuple) {}
use f.PairRes|[int](1, f.PairTuple((f.Item.Offset, true)))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallRejectedTupleLiteralCast", source: `func use(number int, value f.PairTuple) {}
use f.PairRes|[int](1, f.PairTuple((f.Item.Label, true)))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallSliceLiteralCast", source: `func use(number int, value f.TextList) {}
use f.PairRes|[int](1, f.TextList([f.Item.Label, f.caption]))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallEmptySliceLiteralCast", source: `func use(number int, value f.TextList) {}
use f.PairRes|[int](1, f.TextList([]))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallRejectedSliceLiteralCast", source: `func use(number int, value f.TextList) {}
use f.PairRes|[int](1, f.TextList([f.Item.Offset]))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallSliceLiteralAliasCast", source: `func use(number int, value f.TextList) {}
use f.PairRes|[int](1, f.TextListAlias([f.Item.Label, f.caption]))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallInferredSliceLiteral", source: `func use(number int, value []string) {}
use f.PairRes|[int](1, [f.Item.Label, f.caption])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallVariadicCast", source: `func use(number int, value f.Blend) {}
var labels []string
use f.PairRes|[int](1, f.Blend(f.Item.Offset, labels...))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallOverloadedNamedCast", source: `func use(number int, value *f.Choice__1[string]) {}
use f.PairRes|[int](1, f.Choice(string, f.caption))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallRejectedOverloadedNamedCast", source: `func use(number int, value *f.Choice__1[string]) {}
use f.PairRes|[int](1, f.Choice(bool, false))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallOverloadedNamedCastWithoutType", source: `func use(number int, value *f.Choice__1[string]) {}
use f.PairRes|[int](1, f.Choice())`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallInvalidCastArity", source: `func use(number int, value int) {}
use f.PairRes|[int](1, int(1, f.Item.Offset))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallReverseCast", source: `func use(number int, text string) {}
use f.PairRes|[int](1, string(f.Reverse{}))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallConstantConversion", source: `func use(number int, text string) {}
use f.PairRes|[int](1, string(f.Item.Offset+63)+f.Item.Label)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallWrongExplicitCast", source: `func use(number int, value f.Converted) {}
use f.PairRes|[int](1, f.Converted(f.Item.Label))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallOverflowingConversion", source: `func use(number int, value uint8) {}
use f.PairRes|[int](1, uint8(f.Item.Offset+256))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallBoundedConversion", source: `func use(number int, value f.Limited) {}
use f.PairRes|[int](1, f.Limited(3))`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallAllowedBoundedConversion", source: `func use(number int, value f.Limited) {}
use f.PairRes|[int](1, f.Limited(f.Item.Offset))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallNamedBinaryOperator", source: `func use(number int, text f.CaptionText) {}
use f.PairRes|[int](1, f.CaptionText("text") + 1)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackageWrongPartialCallBinaryProperty", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.caption + 1)`, hidden: []string{"pairResults"}},
		{name: "PackageWrongPartialCallBinaryConstant", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.Item.Label + 1)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallConstantShift", source: `func use(number int, value int) {}
use f.PairRes|[int](1, 1 << f.Item.Offset)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackageWrongPartialCallConstantShift", source: `func use(number int, value int) {}
use f.PairRes|[int](1, 1 << f.Item.NegativeOffset)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallOverloadedLeftShift", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.Key(1) << f.Item.NegativeOffset)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallOverloadedRightShift", source: `func use(number int, text string) {}
use f.PairRes|[int](1, 1 >> f.ShiftCount(2000))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallOversizedBuiltinShift", source: `func use(number int, value int) {}
use f.PairRes|[int](1, 1 << 2000)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallOversizedShiftWithoutMethod", source: `func use(number int, value int) {}
use f.PairRes|[int](1, 1 << f.ShiftCount(2000))`, hidden: []string{"pairResults"}},
		{name: "PackageWrongPartialCallNamedBinaryOperator", source: `func use(number int, text f.CaptionText) {}
use f.PairRes|[int](1, (f.CaptionText("text") + "!") + "?")`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallUnaryConstant", source: `func use(number int, value int) {}
use f.PairRes|[int](1, -f.Item.Offset)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallUnaryProperty", source: `func use(number int, value bool) {}
use f.PairRes|[int](1, !f.flag)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallUnaryInShift", source: `func use(number int, value int) {}
use f.PairRes|[int](1, 1 << -f.Item.NegativeOffset)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallUnaryOperator", source: `func use(number int, text string) {}
use f.PairRes|[int](1, -f.Item{})`, prefix: "PairRes", want: "pairResults"},
		{name: "PackageWrongPartialCallUnaryOperator", source: `func use(number int, value f.Item) {}
use f.PairRes|[int](1, -f.Item{})`, hidden: []string{"pairResults"}},
		{name: "PackageUnresolvedPartialCallUnaryOperand", source: `func use(number int, value int) {}
use f.PairRes|[int](1, -missing)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallStaticVariableAddress", source: `func use(number int, values *[]int) {}
use f.PairRes|[int](1, &(f.Item.Values))`, prefix: "PairRes", want: "pairResults"},
		{name: "PackageWrongPartialCallStaticConstantAddress", source: `func use(number int, value *int) {}
use f.PairRes|[int](1, &f.Item.Offset)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallOrdinaryVariableAddress", source: `func use(number int, values *[]int) {}
use f.PairRes|[int](1, &f.Values)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallCompositeLiteralAddress", source: `func use(number int, value *f.Item) {}
use f.PairRes|[int](1, &f.Item{})`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallPointerProperty", source: `func use(number int, text string) {}
use f.PairRes|[int](1, *f.textPointer)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackageWrongPartialCallPointerProperty", source: `func use(number int, value int) {}
use f.PairRes|[int](1, *f.textPointer)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallDereferenceNonPointer", source: `func use(number int, text string) {}
use f.PairRes|[int](1, *f.caption)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallAddressOfDereference", source: `func use(number int, text *string) {}
use f.PairRes|[int](1, &*f.textPointer)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStaticIndex", source: `func use(number int, value int) {}
use f.PairRes|[int](1, f.Item.Array[f.Item.Offset])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStaticIndexAddress", source: `func use(number int, value *int) {}
use f.PairRes|[int](1, &f.Item.Array[f.Item.Offset])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStaticIndexedFieldAddress", source: `func use(number int, value *string) {}
use f.PairRes|[int](1, &f.Item.Entries[f.Item.Offset].Name)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallUnaddressablePropertyField", source: `func use(number int, value *string) {}
use f.PairRes|[int](1, &f.currentEntry.Name)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallOutOfBoundsStaticIndex", source: `func use(number int, value int) {}
use f.PairRes|[int](1, f.Item.Array[2*f.Item.Offset])`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallUnknownStaticIndex", source: `func use(number int, value int) {}
use f.PairRes|[int](1, f.Item.Array[missing])`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallOverloadedIndexOperand", source: `func use(number int, value int) {}
var key f.Key
var values map[string]int
use f.PairRes|[int](1, values[key+1])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallWrongOverloadedIndexOperand", source: `func use(number int, value int) {}
var key f.Key
var values map[f.Key]int
use f.PairRes|[int](1, values[key+1])`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallPropertySlice", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.caption[:f.Item.Offset])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStaticFullSlice", source: `func use(number int, values []int) {}
use f.PairRes|[int](1, f.Item.Array[:f.Item.Offset:4])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallUnaddressableArraySlice", source: `func use(number int, values []int) {}
use f.PairRes|[int](1, f.fixedValues[:])`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallPropertyMapIndex", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.labels[nil])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallMapIndexAddress", source: `func use(number int, text *string) {}
use f.PairRes|[int](1, &f.labels[nil])`, hidden: []string{"pairResults"}},
		{name: "PackagePartialCallMethodProperty", source: `func use(number int, text string) {}
var item f.Item
use f.PairRes|[int](1, item.caption)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallMethodExpression", source: `func use(number int, fn func(f.Item) string) {}
use f.PairRes|[int](1, f.Item.caption)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallStaticValue", source: `func use(number int, text string) {}
use f.PairRes|[int](1, f.Item.Label)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallPointerStaticValue", source: `func use(number int, text string) {}
use f.PairRes|[int](1, (*f.Item).Label)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallLocalProperty", source: `func use(number int, text string) {}
func Caption() string { return "local" }
use f.PairRes|[int](1, caption)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallDotImportProperty", source: `import . "example.com/support/v2"
func use(number int, text string) {}
use f.PairRes|[int](1, error)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallClassProperty", filename: "main_fixture.gox", source: `func use(number uint, text string) {}
use f.PairRes|[uint](1, caption)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallClassFieldAddress", filename: "main_fixture.gox", source: `var scores [4]int
func use(number uint, scores *[4]int) {}
use f.PairRes|[uint](1, &scores)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallClassFieldSlice", filename: "main_fixture.gox", source: `var scores [4]int
func use(number uint, scores []int) {}
use f.PairRes|[uint](1, scores[:f.Item.Offset])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallClassFieldIndexAddress", filename: "main_fixture.gox", source: `var scores [4]int
func use(number uint, score *int) {}
use f.PairRes|[uint](1, &scores[f.Item.Offset])`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallShadowedClassField", filename: "main_fixture.gox", source: `var scores [4]int
func use(number uint, score *string) {}
func check() {
	var scores [4]string
	use f.PairRes|[uint](1, &scores[f.Item.Offset])
}`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialCallClassPropertyAddress", filename: "main_fixture.gox", source: `func use(number uint, text *string) {}
use f.PairRes|[uint](1, &caption)`, hidden: []string{"pairResults"}},
		{name: "PackageWrongPartialCallProperty", source: `func use(number int, flag bool) {}
use f.PairRes|[int](1, f.caption)`, hidden: []string{"pairResults"}},
		{name: "PackagePartialKeywordCallResults", source: `func use(number int, text string) {}
use f.PairOp|[int](1, label = "text")`, prefix: "PairOp", want: "pairOptions"},
		{name: "PackagePartialKeywordBinaryProperty", source: `func use(number int, text string) {}
use f.PairOp|[int](1, label = f.Item.Label + f.caption)`, prefix: "PairOp", want: "pairOptions"},
		{name: "PackagePartialMultipleKeywordsWithBinaryOperand", source: `func use(number int, value int) {}
use f.PairOp|[int](1, label = 1, suffix = f.Item.Offset+1)`, prefix: "PairOp", want: "pairOptions"},
		{name: "PackagePartialMultipleKeywordsWithProperty", source: `func use(number int, text string) {}
use f.PairOp|[int](1, label = "text", suffix = f.caption)`, prefix: "PairOp", want: "pairOptions"},
		{name: "PackagePartialMultipleKeywordsWithSliceLiteralCast", source: `func use(number int, text string) {}
use f.PairOp|[int](1, label = "text", suffix = f.TextList([f.Item.Label, f.caption])[0])`,
			prefix: "PairOp", want: "pairOptions"},
		{name: "PackagePartialMultipleKeywordsWithKeywordCast", source: `func use(number int, text string) {}
use f.PairOp|[int](1, label = "text", suffix = f.ConvertedMap(label = f.caption)["label"])`,
			prefix: "PairOp", want: "pairOptions"},
		{name: "PackagePartialKeywordUnaryConstant", source: `func use(number int, value int) {}
use f.PairOp|[int](1, label = -f.Item.Offset)`, prefix: "PairOp", want: "pairOptions"},
		{name: "PackagePartialKeywordExplicitCast", source: `func use(number int, value f.Converted) {}
use f.PairOp|[int](1, label = f.Converted(f.Item.Offset))`, prefix: "PairOp", want: "pairOptions"},
		{name: "PackageWrongPartialKeywordCallResults", source: `func use(number int, text string) {}
use f.PairOp|[int](1, label = false)`, hidden: []string{"pairOptions"}},
		{name: "PackagePartialKeywordCallWithInferredMapLiteral", source: `func use(number int, labels map[string]string) {}
use f.PairOp|[int](1, label = {"name": f.Item.Label})`, prefix: "PairOp", want: "pairOptions"},
		{name: "PackageUntypedNilKeyword", source: `func use(number int, text string) {}
use f.PairOp|[int](1, label = nil)`, hidden: []string{"pairOptions"}},
		{name: "PackagePartialMixedKeywordCallResults", source: `func use(number int, text string) {}
use f.PairMixedOp|[int](1, "text", label = "text", enabled = true)`, prefix: "PairMixedOp", want: "pairMixedOptions"},
		{name: "PackagePartialVariadicCallResults", source: `func use(number int, text string) {}
var values []string
use f.PairRes|[int](1, values...)`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartialTupleCallResults", source: `func use(pair f.PairTuple, text string) {}
var pair f.PairTuple
use f.PairTupleRes|[int]((pair, "text"))`, prefix: "PairTupleRes", want: "pairTupleResults"},
		{name: "PackagePartialCallWithLocalArrayBound", source: `func use(number int, values [3]int) {}
const size = 2
use f.PairRes|[int](1, func() [3]int {
	const size = 3
	return [size]int{}
}())`, prefix: "PairRes", want: "pairResults"},
		{name: "PackageWrongPartialCallResults", source: `func use(number int, flag bool) {}
use f.PairRes|[int](1, "text")`, hidden: []string{"pairResults"}},
		{name: "TemplatePartialCallResults", source: `func use(number int, text string) {}
use f.Item.PairRes|[int](f.Item{}, 1, "text")`, prefix: "PairRes", want: "pairResults"},
		{name: "PackagePartial", source: `var fn func(int, string) string = f.PairO|[int]`,
			prefix: "PairO", want: "PairOf"},
		{name: "PackageWrongPartial", source: `var fn func(int, string) string = f.PairO|[string]`,
			hidden: []string{"PairOf", "pairOf"}},
		{name: "DotImportedGenericFunction", source: "import . \"example.com/framework\"\n" +
			`var fn func(bool, string) string = PairO|[bool, string]`, prefix: "PairO", want: "PairOf", hidden: []string{"Ordinary", "ordinary"}},
		{name: "DotImportedGenericCall", source: "import . \"example.com/framework\"\n" +
			`var value = PairO|[bool, string](true, "text")`, prefix: "PairO", want: "pairOf", hidden: []string{"Ordinary", "ordinary"}},
		{name: "DotImportedPartialCall", source: "import . \"example.com/framework\"\n" +
			`var value string = PairO|[bool](true, "text")`, prefix: "PairO", want: "pairOf"},
		{name: "DotImportedCallReturningCallback", source: "import . \"example.com/framework\"\n" +
			`var callback func() string = CallbackO|[string]("text")`, prefix: "CallbackO", want: "callbackOf"},
		{name: "DotImportedCallReturningWrongCallback", source: "import . \"example.com/framework\"\n" +
			`var callback func() string = CallbackO|[bool](true)`, hidden: []string{"CallbackOf", "callbackOf"}},
		{name: "DotImportedGenericFunctionWrongArgs", source: "import . \"example.com/framework\"\n" +
			`var fn func(int, string) string = PairO|[string, bool]`, hidden: []string{"PairOf", "pairOf", "Ordinary", "ordinary"}},
		{name: "DotImportedRuntimeIndex", source: "import . \"example.com/framework\"\n" +
			`var value = Val|[0]`, prefix: "Val", want: "Values"},
		{name: "PackageGenericType", source: `var box f.Bo|[int]`, prefix: "Bo", want: "Box", hidden: []string{"Item"}},
		{name: "PackageGenericTypeWithDotImport", source: "import . \"example.com/framework\"\n" +
			`var box f.Bo|[int]`, prefix: "Bo", want: "Box"},
		{name: "PackageInvalidGenericType", source: `var box f.Bo|[string]`, hidden: []string{"Box"}},
		{name: "PackageOverloadedType", source: `var choice f.Ch|[string]`, prefix: "Ch", want: "Choice", selectedType: "Choice__1"},
		{name: "PackageOverloadedTypeOrder", source: `var choice f.Ch|[int]`, prefix: "Ch", want: "Choice", selectedType: "Choice__0"},
		{name: "PackageNestedOverloadedType", source: `var choice f.Ho|[f.Choice[string]]`,
			prefix: "Ho", want: "Holder", selectedType: "Holder__1", hidden: []string{"Holder__0"}},
		{name: "DotImportedNestedOverloadedType", source: "import . \"example.com/framework\"\nvar choice Ho|[Choice[string]]",
			prefix: "Ho", want: "Holder", selectedType: "Holder__1", hidden: []string{"Holder__0"}},
		{name: "PackageInvalidOverloadedType", source: `var choice f.Ch|[bool]`, hidden: []string{"Choice"}},
		{name: "DotImportedOverloadedType", source: "import . \"example.com/framework\"\nvar choice Ch|[string]",
			prefix: "Ch", want: "Choice", selectedType: "Choice__1", hidden: []string{"Choice__0", "Choice__2"}},
		{name: "DotImportedInvalidOverloadedType", source: "import . \"example.com/framework\"\nvar choice Ch|[bool]",
			hidden: []string{"Choice", "Choice__0", "Choice__1", "Choice__2"}},
		{name: "DotImportedOverloadedTypeShadowed", source: "import . \"example.com/framework\"\n" +
			"type Choice struct{}\nvar choice Ch|[string]", hidden: []string{"Choice"}},
		{name: "ClassLookupOverloadedType", filename: "main_fixture.gox",
			source: "type choice = Ch|[string]",
			prefix: "Ch", want: "Choice", selectedType: "Choice__1", hidden: []string{"Choice__0", "Choice__2"}},
		{name: "PackageOverloadedTypeInValueExpression", source: `var value = f.Ch|[string]("text")`, hidden: []string{"Choice"}},
		{name: "PackageRuntimeIndex", source: `var value = f.Va|[0]`, prefix: "Va", want: "Values"},
		{name: "NestedRuntimeIndex", source: `var value = f.Va|[f.Values[0]]`, prefix: "Va", want: "Values"},
		{name: "QualifiedType", source: `var fn func(f.Item, f.Item, string) string = f.Item.Ch|[f.Item, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "Parenthesized", source: `var fn func(f.Item, int, string) string = (f.Item.Ch|)[int, string]`,
			prefix: "Ch", want: "Choose"},
		{name: "Call", source: `var result = f.Item.Ch|[int, string](f.Item{}, 1, "text")`, prefix: "Ch", want: "choose"},
		{name: "WrongConstraint", source: `var fn = f.Item.Re|[string, int]`, hidden: []string{"restricted", "Restricted"}},
		{name: "TooManyArguments", source: `var fn = f.Item.Ch|[int, string, bool]`, hidden: []string{"choose", "Choose"}},
		{name: "PartialStatic", source: `var fn func(int, string) string = f.Item.Pi|[int]`, prefix: "Pi", want: "Pick"},
		{name: "WrongPartialStatic", source: `var fn func(int, string) string = f.Item.Pi|[string]`, hidden: []string{"pick", "Pick"}},
		{name: "RuntimeIndex", source: `var fn = f.Item.Va|[0]`, prefix: "Va", want: "Values"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const imports = "import f \"example.com/framework\"\n"
			filename := cmp.Or(tt.filename, "main.xgo")
			source, pos := typeDisplayTestSource(t, imports+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)}, &modfile.Import{Name: "helper", Path: importTestPkgPath})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			items := completionItemsAt(t, s, filename, pos)
			labels := completionItemLabels(items)
			if tt.want != "" {
				i := slices.Index(labels, tt.want)
				require.NotEqual(t, -1, i, "completion items: %v", labels)
				completed := strings.Replace(tt.source, tt.prefix+"|", items[i].InsertText, 1)
				proj := s.getProj().SnapshotWithOverlay(map[string]*xgo.File{
					filename: {Content: []byte(imports + completed + "\n")},
				})
				info, err := proj.TypeInfo()
				require.NoError(t, err)
				if tt.selectedType != "" {
					data := requireValueAs[*CompletionItemData](t, items[i].Data)
					require.NotNil(t, data.Definition)
					assert.Equal(t, "xgo:"+testframework.PkgPath+"?"+tt.selectedType, data.Definition.String())
					require.NotNil(t, items[i].Documentation)
					doc := requireValueAs[MarkupContent](t, items[i].Documentation.Value)
					assert.Contains(t, doc.Value, `overview="type `+tt.want+"[")
					choice := info.Pkg.Scope().Lookup("choice")
					require.NotNil(t, choice)
					named, ok := gotypes.Unalias(choice.Type()).(*gotypes.Named)
					require.True(t, ok)
					assert.Equal(t, tt.selectedType, named.Obj().Name())
				}
			}
			for _, hidden := range tt.hidden {
				assert.NotContains(t, labels, hidden)
			}
		})
	}
}

func TestServerOverloadedTypeCompletionOverview(t *testing.T) {
	const filename = "main.xgo"
	source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var choice f.Ch|
`)
	s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
	t.Cleanup(s.Close)
	setImportTestFrameworkMethods(t, s, `type Choice__0[T ~int] struct{ Number T }
type Choice__1[T ~string] struct{ Text T }
`)

	item := completionItemByLabel(completionItemsAt(t, s, filename, pos), "Choice")
	require.NotNil(t, item)
	require.NotNil(t, item.Documentation)
	doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
	assert.Contains(t, doc.Value, `overview="type Choice"`)
}

func TestServerOverloadedTypeHover(t *testing.T) {
	const filename = "main.xgo"
	for _, tt := range []struct{ name, source, definition, overview string }{
		{"Qualified", "var choice f.Ch|oice[string]", "Choice__1", "type Choice[T ~string]"},
		{"Unqualified", "import . \"example.com/framework\"\nvar choice Ch|oice[string]", "Choice__1", "type Choice[T ~string]"},
		{"DirectImplementation", "var choice f.Ch|oice__1[string]", "Choice__1", "type Choice__1[T ~string]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import f \"example.com/framework\"\n"+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, `type Choice__0[T ~int] struct{ Number T }
type Choice__1[T ~string] struct{ Text T }
`)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)

			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="xgo:`+testframework.PkgPath+"?"+tt.definition+`"`)
			assert.Contains(t, hover.Contents.Value, `overview="`+tt.overview+`"`)
		})
	}
}

func TestServerImportedSparseOverloads(t *testing.T) {
	for _, tt := range []struct {
		name, declaration, source, definition, signature string
	}{
		{name: "Function", declaration: "func Choose__2(value string) string { return value }",
			source: "_ = f.Ch|oose", definition: "choose#2", signature: "Choose(value string) string"},
		{name: "IncompleteFunctionCall", declaration: "func Choose__2(value string) string { return value }",
			source: "_ = f.Ch|oose()", definition: "choose#2", signature: "Choose(value string) string"},
		{name: "Method", declaration: "func (Item) Choose__2(value string) string { return value }",
			source: "_ = f.Item.Ch|oose", definition: "Item.choose#2", signature: "Choose(f.Item, value string) string"},
		{name: "Template", declaration: "func XGot_Item_Choose__2(item Item, value string) string { return value }",
			source: "_ = f.Item.Ch|oose", definition: "Item.choose#2", signature: "Choose(item f.Item, value string) string"},
		{name: "IncompleteStaticCall", declaration: "func XGos_Item_Choose__2(value string) string { return value }",
			source: "_ = f.Item.Ch|oose()", definition: "Item.choose#2", signature: "Choose(value string) string"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, "import f \"example.com/framework\"\n"+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, tt.declaration+"\n")
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)

			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.signature, help.Signatures[0].Label)

			wantID := "xgo:" + testframework.PkgPath + "?" + tt.definition
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Equal(t, 1, strings.Count(hover.Contents.Value, "def-id="))
			assert.Contains(t, hover.Contents.Value, wantID)

			var matches []string
			for _, item := range completionItemsAt(t, s, filename, pos) {
				if item.Label == "choose" {
					data := requireValueAs[*CompletionItemData](t, item.Data)
					require.NotNil(t, data.Definition)
					matches = append(matches, data.Definition.String())
				}
			}
			assert.Equal(t, []string{wantID}, matches)
		})
	}
}

func TestServerExtendedMemberTypeArguments(t *testing.T) {
	const declarations = `func XGot_Item_XGox_Choose[A, B any](item Item, key A, value B) B { return value }
func XGos_Item_Pick[T any](value T) T { return value }
`
	for _, tt := range []struct{ name, call, wantError string }{
		{name: "Template", call: `f.Item.XGox_Choose[Can|didate, string](item, 1, "text")`},
		{name: "Static", call: `f.Item.Pick[Can|didate](1)`},
		{name: "IncompleteStatic", call: `f.Item.Pick[Can|](1)`, wantError: "undefined: Can"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
type Candidate int
var ordinaryValue string
var item f.Item
_ = `+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			_, err := s.requestProject().TypeInfo()
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, "Candidate")
			assert.NotContains(t, labels, "ordinaryValue")
		})
	}
}

func TestServerExtendedMemberCallArguments(t *testing.T) {
	const template = "func XGot_Item_XGox_Choose[A, B any](item *Item, key A, value B) B { return value }"
	const overloaded = `func XGot_Item_XGox_Choose__0[A, B any](item *Item, key A, value B) B { return value }
func XGot_Item_XGox_Choose__1[A, B any](item *Item, key A, value B, extra int) B { return value }`
	const ordinary = "func XGot_Item_Choose[T any](item *Item, value T) T { return value }"
	const static = "func XGos_Item_XGox_Choose[T any](value T) T { return value }"
	const promotedStatic = `func XGos_Item_Choose(value string) string { return value }
type Wrapper struct{ Item }`
	for _, tt := range []struct {
		name, decl, call, wantSignature string
		wantParams                      []string
		wantTypeArgs                    int
		wantInteger                     bool
	}{
		{name: "BoundEncodedSelector", decl: template, call: `item.XGox_Choose(1, "te|xt")`,
			wantSignature: "XGox_Choose(key A, value B) B", wantParams: []string{"key", "value"}, wantInteger: true},
		{name: "OverloadedBoundEncodedSelector", decl: overloaded, call: `item.XGox_Choose(1, "te|xt")`,
			wantSignature: "XGox_Choose(key A, value B) B", wantParams: []string{"key", "value"}, wantInteger: true},
		{name: "BoundAliasWithPartialTypeArguments", decl: overloaded, call: `item.choose(int, 1, "te|xt")`,
			wantSignature: "choose(A Type, key A, value B) B", wantParams: []string{"A", "key", "value"},
			wantTypeArgs: 1, wantInteger: true},
		{name: "IndexedEncodedSelector", decl: template, call: `f.Item.XGox_Choose[int, string](&item, 1, "te|xt")`,
			wantSignature: "XGox_Choose(item *f.Item, key int, value string) string",
			wantParams:    []string{"item", "key", "value"}, wantInteger: true},
		{name: "IndexedOrdinarySelector", decl: ordinary, call: `(f.Item.Choose[string])(&item, "te|xt")`,
			wantSignature: "Choose(item *f.Item, value string) string", wantParams: []string{"item", "value"}},
		{name: "IndexedEncodedFunction", decl: template, call: `f.XGot_Item_XGox_Choose[int, string](&item, 1, "te|xt")`,
			wantSignature: "XGot_Item_XGox_Choose(item *f.Item, key int, value string) string",
			wantParams:    []string{"item", "key", "value"}, wantInteger: true},
		{name: "IndexedStaticSelector", decl: static, call: `f.Item.XGox_Choose[string]("te|xt")`,
			wantSignature: "XGox_Choose(value string) string", wantParams: []string{"value"}},
		{name: "InferredOrdinarySelector", decl: ordinary, call: `f.Item.Choose(&item, "te|xt")`,
			wantSignature: "Choose(item *f.Item, value T) T", wantParams: []string{"item", "value"}},
		{name: "InferredEncodedSelector", decl: template, call: `f.Item.XGox_Choose(&item, 1, "te|xt")`,
			wantSignature: "XGox_Choose(item *f.Item, key A, value B) B",
			wantParams:    []string{"item", "key", "value"}, wantInteger: true},
		{name: "PromotedStaticSelector", decl: promotedStatic, call: `f.Wrapper.choose("te|xt")`,
			wantSignature: "choose(value string) string", wantParams: []string{"value"}},
		{name: "VariadicTemplateReceiver", decl: "func XGot_Item_Collect(values ...any) int { return len(values) }",
			call: `item.collect("te|xt")`, wantSignature: "collect(values ...any) int", wantParams: []string{"values"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
`+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, tt.decl+"\n")
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)

			call := resourceTestCall(t, proj, "main.xgo")
			var paramNames []string
			for arg := range xgoutil.ResolvedCallExprArgs(info, call) {
				assert.Equal(t, len(paramNames) < tt.wantTypeArgs, arg.IsTypeArg())
				paramNames = append(paramNames, arg.Param.Name())
			}
			assert.Equal(t, tt.wantParams, paramNames)

			doc := TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: doc, Position: pos},
			})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.wantSignature, help.Signatures[0].Label)
			assert.Equal(t, uint32(len(tt.wantParams)-1), help.ActiveParameter)

			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: doc}})
			require.NoError(t, err)
			assert.NotNil(t, findInputSlot(slots, "text", "", XGoInputTypeString, XGoInputKindInPlace))
			if tt.wantInteger {
				assert.NotNil(t, findInputSlot(slots, int64(1), "", XGoInputTypeInteger, XGoInputKindInPlace))
			}
		})
	}
}

func TestServerTemplateReceiverCallArguments(t *testing.T) {
	const frameworkImport = "import f \"example.com/framework\"\n"
	const candidates = `var (
	candidateNumber int
	candidateText string
)
`
	for _, tt := range []struct {
		name, filename, source, declarations string
		wantSignature                        string
	}{
		{name: "IncompatibleOverload", filename: "main.xgo",
			source: frameworkImport + candidates + "var client f.Item\nclient.choose(cand|idateMissing)\n",
			declarations: `func XGot_Item_Choose__0(client string, value string) {}
func XGot_Item_Choose__1(client Item, value int) {}
`, wantSignature: "choose(value int)"},
		{name: "ImplicitReceiver", filename: "main_fixture.gox",
			source: candidates + "choose cand|idateMissing\n",
			declarations: `func XGot_App_Choose__0(app string, value string) {}
func XGot_App_Choose__1(app *App, value int) {}
`, wantSignature: "choose(value int)"},
		{name: "GenericReceiver", filename: "main.xgo",
			source: frameworkImport + candidates + "var client f.Client[int]\nclient.choose(cand|idateNumber)\n",
			declarations: `type Client[T any] struct{}
func XGot_Client_Choose[T any](client Client[T], value T) {}
`},
		{name: "GenericPointerReceiver", filename: "main.xgo",
			source: frameworkImport + candidates + "var client f.Client[int]\nclient.choose(cand|idateMissing)\n",
			declarations: `type Client[T any] struct{}
func XGot_Client_Choose[T any](client *Client[T], value T) {}
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{tt.filename: []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, tt.declarations)

			labels := completionItemLabels(completionItemsAt(t, s, tt.filename, pos))
			assert.Contains(t, labels, "candidateNumber")
			assert.NotContains(t, labels, "candidateText")

			if tt.wantSignature != "" {
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
						Position:     pos,
					},
				})
				require.NoError(t, err)
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				assert.Equal(t, tt.wantSignature, help.Signatures[0].Label)
			}

			if updated := strings.Replace(source, "candidateMissing", "candidateNumber", 1); updated != source {
				s.ModifyFiles([]FileChange{{Path: tt.filename, Content: []byte(updated), Version: 1}})
			}
			requireNoDiagnostics(t, s)
		})
	}
}

func TestServerVariadicTemplateReceiverInference(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
var candidateItem f.Item
var candidateText string
_ = item.collect(int, candidate|Item)
`)
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	t.Cleanup(s.Close)
	setImportTestFrameworkMethods(t, s, `func XGot_Item_XGox_Collect__0[A, B any](values ...B) B { return values[0] }
`)
	requireNoDiagnostics(t, s)

	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	assert.Contains(t, labels, "candidateItem")
	assert.NotContains(t, labels, "candidateText")
}

func TestServerVariadicTemplateCallResultCompletion(t *testing.T) {
	const filename = "main.xgo"
	source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
func pair() (f.Item, f.Item) { return item, item }
func single() f.Item { return item }
item.apply(si|ngle())
`)
	s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
	t.Cleanup(s.Close)
	setImportTestFrameworkMethods(t, s, "func XGot_Item_Apply(values ...Item) {}\n")
	requireNoDiagnostics(t, s)

	labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
	assert.Contains(t, labels, "single")
	assert.NotContains(t, labels, "pair")
}

func TestServerMethodExpressionCompletion(t *testing.T) {
	const declarations = `func (Item) ValueOnly(value int) int { return value }
func (*Item) PointerOnly(value int) int { return value }
func XGot_Item_Template(item *Item, value int) int { return value }
func XGos_Item_Static(value int) int { return value }
func XGot_Item_Generic[T any](item Item, value T) T { return value }
func XGot_Item_XGox_Poly[T any](item Item, value T) T { return value }
func XGos_Item_GenericStatic[T any](value T) T { return value }
type Embedded struct{}
func (*Embedded) Promoted(value int) int { return value }
type Wrapper struct{ *Embedded }
`
	for _, tt := range []struct {
		name, source, label, wantError string
	}{
		{name: "ImplicitPointerReceiver", source: "var cb func(*f.Item, int) int = f.Item.Point|erOnly", label: "PointerOnly"},
		{name: "UnsupportedValueCallback", source: "var cb func(f.Item, int) int = f.Item.Point|erOnly",
			label: "PointerOnly", wantError: "cannot use f.Item.PointerOnly"},
		{name: "PromotedPointerReceiver", source: "var cb func(*f.Wrapper, int) int = f.Wrapper.Pro|moted", label: "Promoted"},
		{name: "ValueReceiver", source: "var cb func(f.Item, int) int = f.Item.Val|ueOnly", label: "ValueOnly"},
		{name: "UnsupportedPointerSelector", source: "_ = (*f.Item).val|ueOnly(&item, 1)",
			label: "valueOnly", wantError: "isn't a pointer"},
		{name: "StaticThroughPointerSelector", source: "var cb func(int) int = (*f.Item).St|atic", label: "Static"},
		{name: "TemplateThroughPointerSelector", source: "var cb func(*f.Item, int) int = (*f.Item).Tem|plate", label: "Template"},
		{name: "GenericTemplate", source: "var cb func(f.Item, int) int = f.Item.Gen|eric", label: "Generic"},
		{name: "GenericEncodedTemplate", source: "var cb func(f.Item, int) int = f.Item.XGox_Po|ly", label: "XGox_Poly"},
		{name: "GenericStatic", source: "var cb func(int) int = f.Item.GenericSt|atic", label: "GenericStatic"},
		{name: "GenericCallbackResultMismatch", source: "var cb func(f.Item, int) string = f.Item.Gen|",
			label: "Generic", wantError: "has no method Gen"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
`+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			_, err := s.requestProject().TypeInfo()
			item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", pos), tt.label)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				assert.Nil(t, item)
			} else {
				require.NoError(t, err)
				require.NotNil(t, item)
				assert.Equal(t, tt.label, item.InsertText)
			}
		})
	}

	t.Run("IndexedImplementation", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, `const XGoPackage = true
type Record struct{}
func (Record) read__0(value int) bool { return true }
var callback func(Record, int) bool = Record.re|ad__0
`)
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)
		item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", pos), "read__0")
		require.NotNil(t, item)
		assert.Equal(t, "read__0", item.InsertText)
		require.NotNil(t, item.Documentation)
		doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
		assert.Contains(t, doc.Value, `overview="func read__0(Record, value int) bool"`)
	})
}

func TestServerTypeAliasMemberCompletion(t *testing.T) {
	const declarations = `type Alias = Item
func (Item) Measure(value int) int { return value }
func XGos_Item_Choose(value int) int { return value }
const XGos_Item_Limit = 10
`
	for _, tt := range []struct {
		name, expr, label, typeError string
	}{
		{name: "StaticValue", expr: "f.Alias.Li|mit", label: "Limit"},
		{name: "InstanceMethod", expr: "item.me|asure(1)", label: "measure"},
		{name: "MethodExpression", expr: "f.Alias.me|asure(item, 1)", label: "measure", typeError: "has no method measure"},
		{name: "StaticMethod", expr: "f.Alias.ch|oose(1)", label: "choose", typeError: "has no method choose"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Alias
_ = `+tt.expr+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			_, err := s.requestProject().TypeInfo()
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			if tt.typeError != "" {
				require.ErrorContains(t, err, tt.typeError)
				assert.NotContains(t, labels, tt.label)
			} else {
				require.NoError(t, err)
				assert.Contains(t, labels, tt.label)
			}
		})
	}
}

func TestServerNamedNonStructMemberCompletion(t *testing.T) {
	const declarations = `type Count int
func (Count) Measure(value int) int { return value }
func XGot_Count_Choose(count Count, value int) int { return value }
func XGos_Count_Reset(value int) int { return value }
const XGos_Count_Limit = 10
`
	for _, tt := range []struct {
		name, call, wantLabel string
	}{
		{name: "InstanceMethod", call: "n.me|asure(1)", wantLabel: "measure"},
		{name: "InstanceTemplate", call: "n.ch|oose(1)", wantLabel: "choose"},
		{name: "MethodExpression", call: "f.Count.me|asure(n, 1)", wantLabel: "measure"},
		{name: "UnboundTemplate", call: "f.Count.ch|oose(n, 1)", wantLabel: "choose"},
		{name: "StaticMethod", call: "f.Count.re|set(1)", wantLabel: "reset"},
		{name: "StaticValue", call: "f.Count.Li|mit", wantLabel: "Limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var n f.Count
_ = `+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			assert.Contains(t, completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), tt.wantLabel)
		})
	}
}

func TestServerStaticValueCompletionShadowing(t *testing.T) {
	for _, tt := range []struct {
		name, decl, source, label string
		typeError                 string
		kind                      CompletionItemKind
	}{
		{name: "EmbeddedMethod", decl: `type Embedded struct{}
func (Embedded) Size(value int) int { return value }
type Wrapper struct{ Embedded }
`, source: "var v f.Wrapper\n_ = v.si|ze(1)", label: "size", kind: FunctionCompletion},
		{name: "SameNamedField", decl: "type Wrapper struct{ Size string }\n",
			source: "_ = f.Wrapper.Si|ze", label: "Size", kind: ConstantCompletion},
		{name: "CallbackWithShadowedMethod", decl: `type Embedded struct{}
func (Embedded) Size() int { return 1 }
type Wrapper struct{ Embedded }
`, source: "var fSize func(f.Wrapper) int = f.Wrapper.Si|ze", label: "Size", typeError: "untyped int"},
		{name: "CallbackWithUnshadowedAlias", decl: `type Embedded struct{}
func (Embedded) Size() int { return 1 }
type Wrapper struct{ Embedded }
`, source: "var fSize func(f.Wrapper) int = f.Wrapper.si|ze", label: "size", kind: FunctionCompletion},
		{name: "CallbackWithPointerMethodAlias", decl: `type Embedded struct{}
func (*Embedded) Size() int { return 1 }
type Wrapper struct{ Embedded }
`, source: "var fn func(*f.Wrapper) int = (*f.Wrapper).si|ze", label: "size", kind: FunctionCompletion},
		{name: "CallbackWithInvalidPointerMethod", decl: `type Embedded struct{}
func (Embedded) Size() int { return 1 }
type Wrapper struct{ Embedded }
`, source: "var fn func(*f.Wrapper) int = (*f.Wrapper).si|ze", label: "size", typeError: "isn't a pointer"},
		{name: "CallbackWithShadowedAlias", decl: `type Embedded struct{}
func (Embedded) Size() int { return 1 }
type Wrapper struct{ Embedded }
const XGos_Wrapper_size = 11
`, source: "var fSize func(f.Wrapper) int = f.Wrapper.si|ze", label: "size", typeError: "untyped int"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"`+"\n"+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, tt.decl+"const XGos_Wrapper_Size = 10\n")
			_, err := s.requestProject().TypeInfo()
			if tt.typeError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.typeError)
			}
			var kinds []CompletionItemKind
			for _, item := range completionItemsAt(t, s, "main.xgo", pos) {
				if item.Label == tt.label {
					kinds = append(kinds, item.Kind)
				}
			}
			if tt.kind == 0 {
				assert.Empty(t, kinds)
			} else {
				assert.Equal(t, []CompletionItemKind{tt.kind}, kinds)
			}
		})
	}
}

func TestServerMethodExpressionCompletionWithSameNamedField(t *testing.T) {
	const declarations = `type Row struct{ Choose int }
func XGos_Row_Choose(value int) int { return value }
type Wrapper struct{ Row }
type Shadow struct{ Choose int; Row }
type TemplateOwner struct{ Apply int }
func XGot_TemplateOwner_Apply(owner TemplateOwner, value int) int { return value }
type Sized struct{}
func (Sized) Measure() int { return 1 }
type Outer struct{ Measure int; Sized }
`
	for _, tt := range []struct {
		name, source, label, hiddenLabel, definition string
		overview                                     string
	}{
		{name: "StaticCall", source: "_ = f.Row.ch|oose(1)", label: "choose", hiddenLabel: "Choose", definition: "Row.choose"},
		{name: "StaticCallback", source: "var fn func(int) int = f.Row.Ch|oose", label: "Choose", hiddenLabel: "choose", definition: "Row.choose"},
		{name: "StaticPointerCall", source: "_ = (*f.Row).ch|oose(1)", label: "choose", hiddenLabel: "Choose", definition: "Row.choose"},
		{name: "StaticPointerCallback", source: "var fn func(int) int = (*f.Row).ch|oose", label: "choose", hiddenLabel: "Choose", definition: "Row.choose"},
		{name: "StaticEmbeddedField", source: "_ = f.Wrapper.ch|oose(1)", label: "choose", hiddenLabel: "Choose", definition: "Row.choose"},
		{name: "StaticOuterField", source: "var fn func(int) int = f.Shadow.ch|oose", label: "choose", hiddenLabel: "Choose", definition: "Row.choose"},
		{name: "TemplateCall", source: "var v f.TemplateOwner\n_ = f.TemplateOwner.ap|ply(v, 1)",
			label: "apply", hiddenLabel: "Apply", definition: "TemplateOwner.apply"},
		{name: "TemplateCallback", source: "var fn func(f.TemplateOwner, int) int = f.TemplateOwner.Ap|ply",
			label: "Apply", hiddenLabel: "apply", definition: "TemplateOwner.apply"},
		{name: "PromotedMethodCall", source: "var v f.Outer\n_ = f.Outer.me|asure(v)",
			label: "measure", hiddenLabel: "Measure", definition: "Sized.measure", overview: "func measure(f.Outer) int"},
		{name: "PromotedMethodCallback", source: "var fn func(f.Outer) int = f.Outer.me|asure",
			label: "measure", hiddenLabel: "Measure", definition: "Sized.measure", overview: "func measure(f.Outer) int"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"`+"\n"+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)

			var matches []CompletionItem
			for _, item := range completionItemsAt(t, s, "main.xgo", pos) {
				if item.Label == tt.label || item.Label == tt.hiddenLabel {
					matches = append(matches, item)
				}
			}
			require.Len(t, matches, 1)
			item := matches[0]
			assert.Equal(t, tt.label, item.Label)
			assert.Equal(t, tt.label, item.InsertText)
			data, ok := item.Data.(*CompletionItemData)
			require.True(t, ok)
			require.NotNil(t, data.Definition)
			assert.Equal(t, "xgo:example.com/framework?"+tt.definition, data.Definition.String())
			if tt.overview != "" {
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, `overview="`+tt.overview+`"`)
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, `overview="`+tt.overview+`"`)
			}
		})
	}
}

func TestServerPromotedOverloadMethodExpression(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
_ = f.Wrapper.|choose
`)
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	t.Cleanup(s.Close)
	setImportTestFrameworkMethods(t, s, `func (Item) Choose__0(value int) int { return value }
func (Item) Choose__1(value string) string { return value }
type Wrapper struct{ Item }
`)
	requireNoDiagnostics(t, s)

	var docs []string
	for _, item := range completionItemsAt(t, s, "main.xgo", pos) {
		if item.Label == "choose" {
			require.NotNil(t, item.Documentation)
			docs = append(docs, requireValueAs[MarkupContent](t, item.Documentation.Value).Value)
		}
	}
	require.Len(t, docs, 2)
	hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos,
	}})
	require.NoError(t, err)
	require.NotNil(t, hover)
	for _, overview := range []string{
		`overview="func choose(f.Wrapper, value int) int"`,
		`overview="func choose(f.Wrapper, value string) string"`,
	} {
		assert.Contains(t, strings.Join(docs, "\n"), overview)
		assert.Contains(t, hover.Contents.Value, overview)
	}
}

func TestServerInterfaceExtendedMemberCompletion(t *testing.T) {
	const declarations = `type Runner interface { Run() }
type Wrapper interface { Runner }
type Deep interface { Wrapper }
type Shadow interface { Runner; Choose(value int) string }
func XGot_Runner_Choose(runner Runner, value int) int { return value }
func XGot_Runner_Count(runner Runner) int { return 1 }
func XGos_Runner_Static(value int) int { return value }
`
	for _, tt := range []struct {
		name, source, label, typeError, definition string
		hidden                                     bool
	}{
		{name: "TemplateOnValue", source: "var runner f.Runner\n_ = runner.ch|oose(1)", label: "choose"},
		{name: "TemplateOnType", source: "var runner f.Runner\n_ = f.Runner.ch|oose(runner, 1)", label: "choose"},
		{name: "EmbeddedTemplateOnValue", source: "var runner f.Wrapper\n_ = runner.ch|oose(1)", label: "choose"},
		{name: "EmbeddedTemplateOnType", source: "var runner f.Runner\n_ = f.Wrapper.ch|oose(runner, 1)",
			label: "choose", definition: "Runner.choose"},
		{name: "EmbeddedTemplateProperty", source: "var runner f.Wrapper\nvar count int = runner.co|unt", label: "count"},
		{name: "EmbeddedStaticOnType", source: "var fn func(int) int = f.Wrapper.St|atic", label: "Static", definition: "Runner.static"},
		{name: "ShadowedTemplateOnValue", source: "var runner f.Shadow\nrunner.ch|oose(1)", label: "choose", definition: "Shadow.choose"},
		{name: "StaticOnValue", source: "var runner f.Runner\nrunner.R|un()", label: "static", hidden: true},
		{name: "TemplateOnPointerToInterface", source: "var runner *f.Wrapper\nrunner.ch|oose(1)", label: "choose",
			typeError: "has no field or method choose", hidden: true},
		{name: "IndirectTemplateOnType", source: "var runner f.Runner\n_ = f.Deep.ch|oose(runner, 1)", label: "choose",
			typeError: "has no method choose", hidden: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"`+"\n"+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, declarations)
			_, err := s.requestProject().TypeInfo()
			var matches []CompletionItem
			for _, item := range completionItemsAt(t, s, "main.xgo", pos) {
				if item.Label == tt.label {
					matches = append(matches, item)
				}
			}
			if tt.typeError != "" {
				require.ErrorContains(t, err, tt.typeError)
			} else {
				require.NoError(t, err)
			}
			if tt.hidden {
				assert.Empty(t, matches)
				return
			}
			require.Len(t, matches, 1)
			item := matches[0]
			assert.Equal(t, tt.label, item.InsertText)
			if tt.definition != "" {
				data := requireValueAs[*CompletionItemData](t, item.Data)
				require.NotNil(t, data.Definition)
				assert.Equal(t, "xgo:example.com/framework?"+tt.definition, data.Definition.String())
			}
		})
	}
}

func TestServerImportedCallableCompletionReservedAlias(t *testing.T) {
	for _, tt := range []struct {
		name         string
		declarations string
		source       string
	}{
		{
			name:         "PackageFunction",
			declarations: "func For(value int) int { return value }\n",
			source:       "var value = library.fo|(1)\n",
		},
		{
			name: "PackageOverload",
			declarations: `const XGoPackage = true
func For__0(value int) int { return value }
`,
			source: "var value = library.fo|(1)\n",
		},
		{
			name: "Method",
			declarations: `type Item struct{}
func (Item) For(value int) int { return value }
`,
			source: `var item library.Item
var value = item.fo|(1)
`,
		},
		{
			name: "Template",
			declarations: `const XGoPackage = true
type Item struct{}
func XGot_Item_For(item Item, value int) int { return value }
`,
			source: `var item library.Item
var value = item.fo|(1)
`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			const path = "example.com/library"
			source, pos := typeDisplayTestSource(t, "import \""+path+"\"\n"+tt.source)
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			proj := s.getProj()
			file, err := goparser.ParseFile(proj.Fset, "library.go", "package library\n"+tt.declarations, 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check(path, proj.Fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			fallback := proj.Importer
			proj.Importer = testImporterFunc(func(candidate string) (*gotypes.Package, error) {
				if candidate == path {
					return pkg, nil
				}
				return fallback.Import(candidate)
			})

			items := completionItemsAt(t, s, filename, pos)
			item := completionItemByLabel(items, "For")
			require.NotNil(t, item)
			assert.Equal(t, "For", item.InsertText)
			assert.NotContains(t, completionItemLabels(items), "for")
			completed := strings.Replace(source, ".fo(", "."+item.InsertText+"(", 1)
			s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(completed), Version: 1}})
			requireNoDiagnostics(t, s)
		})
	}
}

func TestServerDirectEncodedOverloads(t *testing.T) {
	for _, tt := range []struct {
		name, decl, source, typeError, signature string
		overviews                                []string
	}{
		{name: "TemplateReceiver", decl: `func Gopt_Item_Choose__0(item *Item, value int) int { return value }
func Gopt_Item_Choose__1(item *Item, value string) string { return value }
`, source: "var item f.Item\n_ = f.Gopt_Item_Ch|oose(&item, 1)",
			signature: "Gopt_Item_Choose(item *f.Item, value int) int",
			overviews: []string{"func Gopt_Item_Choose(item *f.Item, value int) int"}},
		{name: "TypeArgumentInference", decl: `func Gopx_Choose__0[T any](value T) T { return value }
func Gopx_Choose__1[T any](value T, size int) T { return value }
`, source: "_ = f.Gopx_Ch|oose(1)", signature: "Gopx_Choose(value T) T",
			overviews: []string{"func Gopx_Choose[T any](value T) T"}},
		{name: "UnresolvedGroup", decl: `func Gopx_Choose__0[T any](value T) T { return value }
func Gopx_Choose__1[T any](value T, size int) T { return value }
`, source: "_ = f.Gopx_Ch|oose[int]", typeError: "cannot index",
			overviews: []string{"func Gopx_Choose[T any](value T) T", "func Gopx_Choose[T any](value T, size int) T"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"`+"\n"+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, tt.decl)
			_, err := s.requestProject().TypeInfo()
			if tt.typeError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.typeError)
			}
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			for _, overview := range tt.overviews {
				assert.Contains(t, hover.Contents.Value, `overview="`+overview+`"`)
			}
			assert.NotContains(t, hover.Contents.Value, "__xgo_overload_args__")
			if tt.signature != "" {
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				assert.Equal(t, tt.signature, help.Signatures[0].Label)
			}
		})
	}
}

func TestServerTemplateWithoutReceiverParameter(t *testing.T) {
	for _, tt := range []struct {
		name, source, signature string
		wantTypeError           string
		hiddenLabel             string
		visibleLabel            string
	}{
		{name: "InstanceCall", source: "var item f.Item\n_ = item.ch|oose()", wantTypeError: "too many arguments", hiddenLabel: "choose"},
		{name: "InstanceValue", source: "var item f.Item\nvar fn func() int = item.Ch|oose",
			wantTypeError: "cannot use", hiddenLabel: "Choose"},
		{name: "TypeSelector", source: "_ = f.Item.Ch|oose()", signature: "Choose() int", visibleLabel: "choose"},
		{name: "EncodedFunction", source: "_ = f.Gopt_Item_Ch|oose()", signature: "Gopt_Item_Choose() int"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"`+"\n"+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, "func Gopt_Item_Choose() int { return 1 }\n")
			_, err := s.requestProject().TypeInfo()
			if tt.wantTypeError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantTypeError)
			}
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			if tt.signature == "" {
				assert.Nil(t, help)
			} else {
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				assert.Equal(t, tt.signature, help.Signatures[0].Label)
			}
			if tt.hiddenLabel != "" {
				assert.NotContains(t, completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), tt.hiddenLabel)
			}
			if tt.visibleLabel != "" {
				assert.Contains(t, completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), tt.visibleLabel)
			}
		})
	}
}

func TestServerStaticInstanceFunctionCompletion(t *testing.T) {
	for _, tt := range []struct {
		name, source, label string
		want                bool
	}{
		{name: "InstanceImplicitCall", source: "var n int = item.ch|oose", label: "choose"},
		{name: "InstanceFunctionValue", source: "var fn func() int = item.Ch|oose", label: "Choose"},
		{name: "InstanceExplicitCall", source: "var n int = item.ch|oose()", label: "choose"},
		{name: "TypeSelectorCall", source: "var n int = f.Item.ch|oose()", label: "choose", want: true},
		{name: "TypeSelectorValue", source: "var fn func() int = f.Item.Ch|oose", label: "Choose", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import f "example.com/framework"
var item f.Item
`+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, "func XGos_Item_Choose() int { return 1 }\n")
			_, err := s.requestProject().TypeInfo()
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			if tt.want {
				require.NoError(t, err)
				assert.Contains(t, labels, tt.label)
			} else {
				require.Error(t, err)
				assert.NotContains(t, labels, tt.label)
			}
		})
	}
}
