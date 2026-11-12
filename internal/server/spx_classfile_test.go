package server

import (
	"maps"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerSpxClassfileResources(t *testing.T) {
	for _, tt := range []struct {
		name, project, sprite, projectExt, workExt, prefix, className string
	}{
		{"Normalized", "main.spx", "Red-Cat.spx", ".spx", ".spx", "", "Red_Cat"},
		{"ProjectName", "Stage.stage", "Runner.actor", ".stage", ".actor", "", "Runner"},
		{"WorkPrefix", "Stage.stage", "Runner.actor", ".stage", ".actor", "Actor", "ActorRunner"},
		{"GoxExtension", "Stage_stage.gox", "Runner_actor.gox", "_stage.gox", "_actor.gox", "", "Runner"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{
				tt.project:          []byte(tt.className + ".setCostume \"known\"\nvar target *" + tt.className + "\n"),
				tt.sprite:           []byte("setCostume \"known\"\n"),
				"assets/index.json": []byte(`{}`),
				"assets/sprites/" + tt.className + "/index.json": []byte(`{"costumes":[{"name":"known"}]}`),
			}
			s := newSpxTestServer(t, files)
			t.Cleanup(s.Close)
			fullExt := "*" + tt.projectExt
			if tt.project == "main.spx" {
				fullExt = "main.spx"
			}
			s.getProj().SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
				Ext: tt.projectExt, FullExt: fullExt, Class: "Game", PkgPaths: []string{SpxPkgPath},
				Works: []*modfile.Class{{Ext: tt.workExt, Class: "SpriteImpl", Prefix: tt.prefix, Embedded: true}},
			}}}}))
			result, err := analyzeSpx(s.getProj())
			require.NoError(t, err)
			requireNoDiagnostics(t, s)
			assert.Equal(t, tt.project, result.mainSpxFile)
			named := spxSpriteTypeForFile(s.getProj(), tt.sprite)
			require.NotNil(t, named)
			assert.Equal(t, tt.className, named.Obj().Name())
			assert.True(t, result.hasSpxSpriteType(named))
			assert.Same(t, result.spxResourceSet.Sprite(tt.className), spxSpriteResourceForFile(s.getProj(), result, tt.sprite))
			assert.Nil(t, spxSpriteResourceForFile(s.getProj(), result, tt.project))
			for _, filename := range []string{tt.project, tt.sprite} {
				uri := s.toDocumentURI(filename)
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: uri}})
				require.NoError(t, err)
				assert.Contains(t, documentLinkTargets(t, links), "spx://resources/sprites/"+tt.className+"/costumes/known")
				slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: uri}}})
				require.NoError(t, err)
				require.NotEmpty(t, slots)
				assert.Equal(t, new(FormatSpxSpriteCostumeResourceContextURI(tt.className)), slots[0].Accept.ResourceContext)
			}
			newName := tt.prefix + "Renamed"
			originalFiles := maps.Clone(files)
			changes, err := s.renameSpxResource(s.getProj(), result, SpxSpriteResourceID{SpriteName: tt.className}, newName)
			require.NoError(t, err)
			assert.Contains(t, changes[s.toDocumentURI(tt.project)], TextEdit{
				Range:   Range{Start: Position{Line: 1, Character: 12}, End: Position{Line: 1, Character: uint32(12 + len(tt.className))}},
				NewText: newName,
			})
			for uri, edits := range changes {
				filename, err := s.fromDocumentURI(uri)
				require.NoError(t, err)
				files[filename] = []byte(applyResourceRenameTestEdits(t, string(files[filename]), edits))
			}
			files["Renamed"+tt.workExt] = files[tt.sprite]
			delete(files, tt.sprite)
			files["assets/sprites/"+newName+"/index.json"] = files["assets/sprites/"+tt.className+"/index.json"]
			delete(files, "assets/sprites/"+tt.className+"/index.json")
			updated := newSpxTestServer(t, files)
			updated.getProj().SetModule(s.getProj().Module())
			t.Cleanup(updated.Close)
			requireNoDiagnostics(t, updated)
			class := spxSpriteTypeForFile(updated.requestProject(), "Renamed"+tt.workExt)
			require.NotNil(t, class)
			assert.Equal(t, newName, class.Obj().Name())
			if tt.prefix != "" {
				for _, invalid := range []struct{ name, newName string }{
					{"MissingPrefix", "Renamed"},
					{"EmptyClassName", tt.prefix},
				} {
					t.Run(invalid.name, func(t *testing.T) {
						original := newSpxTestServer(t, originalFiles)
						original.getProj().SetModule(s.getProj().Module())
						t.Cleanup(original.Close)
						edit, err := original.renameResources([]XGoRenameResourceParams{{
							Resource: XGoResourceIdentifier{URI: XGoResourceURI("spx://resources/sprites/" + tt.className)}, NewName: invalid.newName,
						}})
						assert.ErrorContains(t, err, "class prefix")
						assert.Nil(t, edit)
					})
				}
			}
		})
	}

	t.Run("CompletionProjectClass", func(t *testing.T) {
		source, position := typeDisplayTestSource(t, `var Runner Sprite
func choose() {
    var target Sprite = R|
}
`)
		s := newSpxTestServer(t, map[string][]byte{
			"Stage.stage":                      []byte(source),
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{}`),
		})
		s.getProj().SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
			Ext: ".stage", FullExt: "*.stage", Class: "Game", PkgPaths: []string{SpxPkgPath},
		}}}}))
		items := completionItemsAt(t, s, "Stage.stage", position)
		item := completionItemByLabel(items, "Runner")
		require.NotNil(t, item)
		data := requireValueAs[*CompletionItemData](t, item.Data)
		assert.Equal(t, "xgo:main?Stage.Runner", data.Definition.String())
	})
}

func TestSpxFrameworkForFile(t *testing.T) {
	t.Run("OtherFrameworkUsingSpxExtension", func(t *testing.T) {
		s := newFrameworkTestServerWithSpxExtension(t, map[string][]byte{"main.spx": []byte("echo 1\n")})
		assert.Nil(t, spxFrameworkForFile(s.getProj(), "main.spx"))
		result, err := analyzeSpx(s.getProj())
		require.NoError(t, err)
		assert.Nil(t, result)
	})
	t.Run("SDKAsAdditionalPackage", func(t *testing.T) {
		s := newSpxTestServer(t, nil)
		proj := s.getProj()
		proj.SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
			Ext: ".spx", Class: "App", PkgPaths: []string{"example.com/framework", SpxPkgPath},
		}}}}))
		assert.Nil(t, spxFrameworkForFile(proj, "main.spx"))
		ctx := newSpxSymbols(proj)
		assert.Empty(t, ctx.spxTypeName(spxTestType(t, s, "SpriteImpl")))
	})
}
