package server

import (
	"io/fs"
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
				return []string{"fmt", testframework.PkgPath, "example.com/missing"}, nil
			}
			lookup := s.lookupPkgDoc
			s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
				if pkgPath == "fmt" {
					return &pkgdoc.PkgDoc{Doc: "Package fmt formats values."}, nil
				}
				return lookup(pkgPath)
			}

			items := completionItemsAt(t, s, "main.xgo", tt.position)
			assert.ElementsMatch(t, []string{"fmt", testframework.PkgPath}, completionItemLabels(items))
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
