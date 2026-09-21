package server

import (
	"archive/zip"
	"bytes"
	gotypes "go/types"
	"io/fs"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/config"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionImportBindings(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   []string
		absent []string
	}{
		{name: "DeclaredName", source: "import \"example.com/version/v2\"\nprintln versioned.Value\n|\n", want: []string{"versioned"}, absent: []string{"v2"}},
		{name: "Alias", source: "import custom \"example.com/version/v2\"\nprintln custom.Value\n|\n", want: []string{"custom"}, absent: []string{"versioned", "v2"}},
		{name: "Dot", source: "import . \"example.com/framework\"\nprintln Limit\n|\n", want: []string{"Limit", "runWhen"}, absent: []string{".", "framework"}},
		{name: "Blank", source: "import _ \"fmt\"\n|\n", absent: []string{"_", "fmt"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newFrameworkTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProj()
			fallback := proj.Importer
			pkg := gotypes.NewPackage("example.com/version/v2", "versioned")
			pkg.Scope().Insert(gotypes.NewVar(token.NoPos, pkg, "Value", gotypes.Typ[gotypes.Int]))
			pkg.MarkComplete()
			proj.Importer = testImporterFunc(func(pkgPath string) (*gotypes.Package, error) {
				if pkgPath == pkg.Path() {
					return pkg, nil
				}
				return fallback.Import(pkgPath)
			})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", position)
			labels := completionItemLabels(items)
			for _, label := range tt.want {
				assert.Contains(t, labels, label)
			}
			for _, label := range tt.absent {
				assert.NotContains(t, labels, label)
			}
		})
	}
}

func TestServerTextDocumentCompletionImports(t *testing.T) {
	for _, tt := range []struct {
		name     string
		source   string
		position Position
	}{
		{name: "InImportStringLit", source: "\nimport \"f\n", position: Position{Line: 1, Character: 9}},
		{name: "InImportGroupStringLit", source: "\nimport (\n\t\"f\n", position: Position{Line: 2, Character: 3}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newFrameworkTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
			s.listPkgs = func() ([]string, error) {
				return []string{"fmt", testframework.PkgPath, "example.com/undocumented"}, nil
			}
			lookup := s.lookupPkgDoc
			s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
				if pkgPath == "fmt" {
					return &pkgdoc.PkgDoc{Doc: "Package fmt formats values."}, nil
				}
				return lookup(pkgPath)
			}

			items := completionItemsAt(t, s, "main.xgo", tt.position)
			assert.ElementsMatch(t, []string{"fmt", testframework.PkgPath, "example.com/undocumented"}, completionItemLabels(items))
			item := completionItemByLabel(items, "fmt")
			require.NotNil(t, item)
			assert.Equal(t, ModuleCompletion, item.Kind)
			assert.Equal(t, "fmt", item.InsertText)
			assert.Equal(t, ToPtr(PlainTextTextFormat), item.InsertTextFormat)
			data := requireValueAs[*CompletionItemData](t, item.Data)
			assert.Equal(t, ToPtr("fmt"), data.Definition.Package)
			require.NotNil(t, item.Documentation)
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
			assert.Contains(t, doc.Value, "Package fmt formats values.")
		})
	}

	t.Run("EmptyPackageList", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("import \"f\n")})
		s.listPkgs = func() ([]string, error) { return nil, nil }
		assert.Empty(t, completionItemsAt(t, s, "main.xgo", Position{Character: 9}))
	})

	t.Run("PackageListError", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("import \"f\n")})
		s.listPkgs = func() ([]string, error) { return nil, fs.ErrPermission }
		_, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Character: 9},
			},
		})
		assert.ErrorIs(t, err, fs.ErrPermission)
		assert.ErrorContains(t, err, "failed to list packages")
	})
}

func TestServerTextDocumentCompletionImportWithoutDocumentation(t *testing.T) {
	archive := testframework.NewPkgDataZip(t)
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	for _, tt := range []struct{ name, documentation string }{
		{"Missing", ""},
		{"Invalid", "{"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for _, file := range zr.File {
				if strings.HasSuffix(file.Name, ".pkgexport") {
					require.NoError(t, zw.Copy(file))
				}
			}
			if tt.documentation != "" {
				w, err := zw.Create(testframework.PkgPath + ".pkgdoc")
				require.NoError(t, err)
				_, err = w.Write([]byte(tt.documentation))
				require.NoError(t, err)
			}
			require.NoError(t, zw.Close())
			data, err := pkgdata.New(buf.Bytes())
			require.NoError(t, err)
			_, err = data.GetPkgDoc(testframework.PkgPath)
			require.Error(t, err)
			source, pos := typeDisplayTestSource(t, "import \"|example.com/framework\"\nvar value framework.Item\n")
			files := map[string][]byte{"main.xgo": []byte(source)}
			proj, _, err := config.NewProject(newFileMap(files), config.Options{PkgData: data})
			require.NoError(t, err)
			s := New(proj, nil, fileMapGetter(files), &MockScheduler{}, data.ListPkgs, data.GetPkgDoc)
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", pos), testframework.PkgPath)
			require.NotNil(t, item)
			assert.Equal(t, ModuleCompletion, item.Kind)
			assert.Equal(t, testframework.PkgPath, item.InsertText)
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("import \"" + item.InsertText + "\"\nvar value framework.Item\n"), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
		})
	}
}
