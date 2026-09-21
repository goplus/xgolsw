package server

import (
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerXGoGetInputSlotsClassfileProperties(t *testing.T) {
	const plain = "func (a *App) Label() string { return \"\" }\n"
	const required = "func (a *App) Label(value int) string { return \"\" }\n"
	for _, tt := range []struct {
		name, methods, local, globals string
		want                          bool
	}{
		{name: "Plain", methods: plain, want: true},
		{name: "Overload", methods: "func (a *App) Label__0() string { return \"\" }\nfunc (a *App) Label__1(value int) string { return \"\" }\n", want: true},
		{name: "Template", methods: "func XGot_App_Label(a *App) string { return \"\" }\n", want: true},
		{name: "TemplateOverload", methods: "func XGot_App_Label__0(a *App) string { return \"\" }\nfunc XGot_App_Label__1(a *App, value int) string { return \"\" }\n", want: true},
		{name: "RequiredArguments", methods: required},
		{name: "VariadicAlone", methods: "func (a *App) Label(values ...int) string { return \"\" }\n"},
		{name: "OverloadArgumentsFirst", methods: "func (a *App) Label__0(value int) string { return \"\" }\nfunc (a *App) Label__1() string { return \"\" }\n", want: true},
		{name: "OverloadVoidFirst", methods: "func (a *App) Label__0() {}\nfunc (a *App) Label__1() string { return \"\" }\n"},
		{name: "OverloadDifferentResultFirst", methods: "func (a *App) Label__0() int { return 0 }\nfunc (a *App) Label__1() string { return \"\" }\n"},
		{name: "OverloadVariadicFirst", methods: "func (a *App) Label__0(values ...int) int { return 0 }\nfunc (a *App) Label__1() string { return \"\" }\n"},
		{name: "OverloadOptionalFirst", methods: "func (a *App) Label__0(__xgo_optional_value int) int { return 0 }\nfunc (a *App) Label__1() string { return \"\" }\n"},
		{name: "OverloadMatchingResultFirst", methods: "func (a *App) Label__0() string { return \"\" }\nfunc (a *App) Label__1() int { return 0 }\n", want: true},
		{name: "TemplateOverloadArgumentsFirst", methods: "func XGot_App_Label__0(a *App, value int) int { return 0 }\nfunc XGot_App_Label__1(a *App) string { return \"\" }\n", want: true},
		{name: "TemplateOverloadVariadicFirst", methods: "func XGot_App_Label__0(a *App, values ...int) int { return 0 }\nfunc XGot_App_Label__1(a *App) string { return \"\" }\n"},
		{name: "LocalAlias", methods: plain, local: "label := 1\n_ = label\n"},
		{name: "LocalDeclaredName", methods: plain, local: "Label := 1\n_ = Label\n", want: true},
		{name: "GlobalAlias", methods: plain, globals: "var label int\n", want: true},
		{name: "NonPropertyFallback", methods: required, globals: "var label = \"global\"\n", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "func run() {\n" + tt.local + "use \"text\"\n}\n"
			s := newImportTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte(source),
				"globals.xgo":      []byte("func Use(value string) {}\n" + tt.globals),
			})
			setImportTestFrameworkMethods(t, s, tt.methods)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
			require.NoError(t, err)
			require.NotEmpty(t, slots)
			slot := slots[len(slots)-1]
			start, end := PositionOffset([]byte(source), slot.Range.Start), PositionOffset([]byte(source), slot.Range.End)
			require.Equal(t, `"text"`, source[start:end])
			if tt.want {
				assert.Contains(t, slot.PredefinedNames, "label")
			} else {
				assert.NotContains(t, slot.PredefinedNames, "label")
			}
			for _, name := range slot.PredefinedNames {
				assert.NotContains(t, name, "__")
				assert.NotContains(t, name, "XGot_")
			}
			s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(source[:start] + "label" + source[end:]), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			if tt.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	t.Run("ExactMethodName", func(t *testing.T) {
		source := "func label() string { return \"value\" }\nfunc use(value string) {}\nfunc run() { use \"text\" }\n"
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
		setImportTestFrameworkMethods(t, s, plain)
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
		require.NoError(t, err)
		require.NotEmpty(t, slots)
		assert.NotContains(t, slots[len(slots)-1].PredefinedNames, "label")
		s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(strings.Replace(source, `"text"`, "label", 1)), Version: 1}})
		_, err = s.requestProject().TypeInfo()
		require.Error(t, err)
	})
}

func TestServerXGoGetInputSlotsImportedMemberShadowing(t *testing.T) {
	for _, name := range []string{"Label", "Apply", "Value", "Item"} {
		t.Run(name, func(t *testing.T) {
			for _, lookup := range []string{"DotImport", "ClassfilePackage"} {
				t.Run(lookup, func(t *testing.T) {
					source := "accept \"text\"\n"
					if lookup == "DotImport" {
						source = "import . \"example.com/support/v2\"\n" + source
					}
					s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": nil, "Worker_fixture.gox": []byte(source)})
					if lookup == "ClassfilePackage" {
						config := testframework.NewModule(t).Module
						config.Opt.Projects[0].PkgPaths = append(config.Opt.Projects[0].PkgPaths, importTestPkgPath)
						s.getProj().SetModule(newTestModule(t, config))
					}
					pkg, err := s.getProj().Import(importTestPkgPath)
					require.NoError(t, err)
					typ := pkg.Scope().Lookup("Name").Type()
					pkg.Scope().Insert(gotypes.NewVar(token.NoPos, pkg, name, typ))
					_, err = s.requestProject().TypeInfo()
					require.NoError(t, err)
					slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"}}})
					require.NoError(t, err)
					require.Len(t, slots, 1)
					assert.NotContains(t, slots[0].PredefinedNames, name)
					s.ModifyFiles([]FileChange{{Path: "Worker_fixture.gox", Content: []byte(strings.Replace(source, `"text"`, name, 1)), Version: 1}})
					_, err = s.requestProject().TypeInfo()
					require.Error(t, err)
				})
			}
		})
	}
	t.Run("NamedImport", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte("use \"text\"\n"),
			"globals.xgo":      []byte("func Use(value string) {}\n"),
		}, &modfile.Import{Name: "label", Path: importTestPkgPath})
		setImportTestFrameworkMethods(t, s, "func (a *App) Label() string { return \"\" }\n")
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
		require.NoError(t, err)
		require.Len(t, slots, 1)
		assert.Contains(t, slots[0].PredefinedNames, "label")
		s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte("use label\n"), Version: 1}})
		_, err = s.requestProject().TypeInfo()
		require.NoError(t, err)
	})
}
