package server

import (
	gotypes "go/types"
	"io/fs"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	t.Run("ConfiguredProject", func(t *testing.T) {
		files := map[string][]byte{"main_fixture.gox": []byte("var Count = measure(1)\n")}
		proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
		proj.PkgPath = "example.com/project"
		mod := testframework.NewModule(t)
		proj.Mod = mod
		importer := testframework.NewImporter(t, proj.Fset)
		proj.Importer = importer
		typeInfo, err := proj.TypeInfo()
		require.NoError(t, err)

		s := newServer(proj, nil, fileMapGetter(files), &MockScheduler{},
			func() ([]string, error) { return nil, nil },
			func(string) (*pkgdoc.PkgDoc, error) { return nil, fs.ErrNotExist },
		)
		assert.Same(t, proj, s.getProj())
		assert.Equal(t, "example.com/project", proj.PkgPath)
		assert.Same(t, mod, proj.Mod)
		assert.Same(t, importer, proj.Importer)
		gotTypeInfo, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		assert.Same(t, typeInfo, gotTypeInfo)
		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:example.com/project?App.Count"`)
		assert.Contains(t, hover.Contents.Value, `overview="var Count int"`)
	})

	t.Run("InstanceIsolation", func(t *testing.T) {
		var checks []func()
		for _, name := range []string{"first", "second"} {
			filename := "main_" + name + ".gox"
			pkgPath := "example.com/" + name
			files := map[string][]byte{
				filename:      []byte("onStart => {\n\n}\n"),
				"imports.xgo": []byte("import \"example.com/framework\"\nvar _ framework.Item\n"),
			}
			proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
			proj.PkgPath = "main"
			proj.Mod = testframework.NewModule(t)
			class := proj.Mod.Opt.Projects[0]
			class.Ext = "_" + name + ".gox"
			class.FullExt = filename
			class.Works[0].Ext = class.Ext
			require.NoError(t, proj.Mod.ImportClasses())
			proj.Importer = testframework.NewImporter(t, proj.Fset)
			framework, err := proj.Importer.Import(testframework.PkgPath)
			require.NoError(t, err)
			doc := testframework.NewPkgDoc(t)
			wantDoc := "Documentation for the " + name + " server."
			doc.Types["App"].Methods["OnStart"] = wantDoc
			listPkgs := func() ([]string, error) { return []string{pkgPath}, nil }
			lookupPkgDoc := func(path string) (*pkgdoc.PkgDoc, error) {
				switch path {
				case testframework.PkgPath:
					return doc, nil
				case pkgPath:
					return &pkgdoc.PkgDoc{Doc: wantDoc}, nil
				default:
					return nil, fs.ErrNotExist
				}
			}
			s := newServer(proj, nil, fileMapGetter(files), &MockScheduler{}, listPkgs, lookupPkgDoc)
			check := func() {
				t.Helper()

				// Rebuild types so cached results cannot mask shared configuration.
				files[filename] = append(files[filename], '\n')
				typeInfo, err := s.getProjWithFile().TypeInfo()
				require.NoError(t, err)
				assert.Equal(t, "main", typeInfo.Pkg.Path())
				var method gotypes.Object
				for ident, obj := range typeInfo.Uses {
					if ident.Name == "onStart" {
						method = obj
						break
					}
				}
				require.NotNil(t, method)
				assert.Same(t, framework, method.Pkg())
				hover, err := s.textDocumentHover(&HoverParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)},
						Position:     Position{Character: 1},
					},
				})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, wantDoc)

				items := completionItemsAt(t, s, filename, Position{Line: 1})
				item := completionItemByLabel(items, "onStart")
				require.NotNil(t, item)
				require.NotNil(t, item.Documentation)
				assert.Contains(t, requireValueAs[MarkupContent](t, item.Documentation.Value).Value, wantDoc)

				items = completionItemsAt(t, s, "imports.xgo", Position{Character: 8})
				require.Len(t, items, 1)
				assert.Equal(t, pkgPath, items[0].Label)
				require.NotNil(t, items[0].Documentation)
				assert.Contains(t, requireValueAs[MarkupContent](t, items[0].Documentation.Value).Value, wantDoc)
			}
			check()
			checks = append(checks, check)
		}
		for _, check := range checks {
			check()
		}
	})
}

func TestNewTestServer(t *testing.T) {
	t.Run("NoFrameworkDependencies", func(t *testing.T) {
		files := map[string][]byte{
			"main.xgo": []byte("import \"strings\"\nvar Count = len(strings.ToUpper(\"text\"))\nCount = 2\n\n"),
		}
		plain := newTestServer(t, files)
		check := func(s *Server) {
			t.Helper()

			proj := s.getProj()
			assert.False(t, proj.Mod.IsClass("_fixture.gox"))
			assert.False(t, proj.Mod.IsClass(".spx"))
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			_, err = proj.Importer.Import(testframework.PkgPath)
			assert.ErrorIs(t, err, fs.ErrNotExist)
			_, err = s.lookupPkgDoc(testframework.PkgPath)
			assert.ErrorIs(t, err, fs.ErrNotExist)
			assert.Empty(t, completionItemsAt(t, s, "main.xgo", Position{Character: 8}))
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", Position{Line: 3}))
			assert.Contains(t, labels, "Count")
			assert.Contains(t, labels, "len")
			assert.NotContains(t, labels, "Low")
			assert.NotContains(t, labels, "runWhen")
			hover, err := s.textDocumentHover(&HoverParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 2},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `overview="var Count int"`)
		}
		check(plain)

		frameworkServer := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte("onStart => {\n\n}\n"),
			"imports.xgo":      []byte("import \"example.com/framework\"\nvar _ framework.Item\n"),
		})
		_, err := frameworkServer.getProj().TypeInfo()
		require.NoError(t, err)
		item := completionItemByLabel(completionItemsAt(t, frameworkServer, "main_fixture.gox", Position{Line: 1}), "onStart")
		require.NotNil(t, item)
		require.NotNil(t, item.Documentation)
		assert.Contains(t, requireValueAs[MarkupContent](t, item.Documentation.Value).Value, "OnStart accepts a project callback.")
		assert.Equal(t, []string{testframework.PkgPath}, completionItemLabels(completionItemsAt(t, frameworkServer, "imports.xgo", Position{Character: 8})))

		check(plain)
		check(newTestServer(t, files))
	})

	t.Run("InvalidDefaultClasses", func(t *testing.T) {
		defaultOpt := modload.Default.Opt
		t.Cleanup(func() { modload.Default.Opt = defaultOpt })
		modload.Default.Opt = &modfile.File{ClassMods: []string{"example.com/missing-framework"}}
		require.PanicsWithError(t, "failed to import classes: "+xgomod.ErrNotFound.Error(), func() {
			New(xgo.NewProject(nil, nil, xgo.FeatAll), nil, nil, nil)
		})

		for _, tt := range []struct {
			name      string
			filename  string
			source    string
			want      string
			newServer testServerFactory
		}{
			{name: "PlainXGo", filename: "main.xgo", source: "var Count int\n", want: "var Count int", newServer: newTestServer},
			{name: "StandaloneClass", filename: "Record.gox", source: "var Count int\n", want: "var Count int", newServer: newTestServer},
			{name: "Framework", filename: "main_fixture.gox", source: "measure 1\n", want: "Measure__0 is the integer overload of Measure.", newServer: newFrameworkTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := tt.newServer(t, map[string][]byte{tt.filename: []byte(tt.source)})
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				hover, err := s.textDocumentHover(&HoverParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
						Position:     Position{Character: 4},
					},
				})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, tt.want)
			})
		}
	})
}
