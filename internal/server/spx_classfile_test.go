package server

import (
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
			s := newSpxTestServer(t, map[string][]byte{
				tt.project:          []byte(tt.className + ".setCostume \"known\"\nvar target *" + tt.className + "\n"),
				tt.sprite:           []byte("setCostume \"known\"\n"),
				"assets/index.json": []byte(`{}`),
				"assets/sprites/" + tt.className + "/index.json": []byte(`{"costumes":[{"name":"known"}]}`),
			})
			fullExt := "*" + tt.projectExt
			if tt.project == "main.spx" {
				fullExt = "main.spx"
			}
			s.getProj().SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
				Ext: tt.projectExt, FullExt: fullExt, Class: "Game", PkgPaths: []string{SpxPkgPath},
				Works: []*modfile.Class{{Ext: tt.workExt, Class: "SpriteImpl", Prefix: tt.prefix, Embedded: true}},
			}}}}))
			result, err := s.compileAt(s.getProj())
			require.NoError(t, err)
			requireNoDiagnostics(t, s)
			assert.Equal(t, tt.project, result.mainSpxFile)
			named := spxSpriteTypeForFile(result.proj, tt.sprite)
			require.NotNil(t, named)
			assert.Equal(t, tt.className, named.Obj().Name())
			assert.True(t, result.hasSpxSpriteType(named))
			assert.Same(t, result.spxResourceSet.Sprite(tt.className), spxSpriteResourceForFile(result, tt.sprite))
			assert.Nil(t, spxSpriteResourceForFile(result, tt.project))
			for _, filename := range []string{tt.project, tt.sprite} {
				uri := s.toDocumentURI(filename)
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: uri}})
				require.NoError(t, err)
				assert.Contains(t, documentLinkTargets(t, links), "spx://resources/sprites/"+tt.className+"/costumes/known")
				slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: uri}}})
				require.NoError(t, err)
				require.NotEmpty(t, slots)
				assert.Equal(t, ToPtr(FormatSpxSpriteCostumeResourceContextURI(tt.className)), slots[0].Accept.ResourceContext)
			}
			changes, err := s.spxRenameSpriteResource(result, SpxSpriteResourceID{SpriteName: tt.className}, "Renamed")
			require.NoError(t, err)
			assert.Contains(t, changes[s.toDocumentURI(tt.project)], TextEdit{
				Range:   Range{Start: Position{Line: 1, Character: 12}, End: Position{Line: 1, Character: uint32(12 + len(tt.className))}},
				NewText: "Renamed",
			})
		})
	}

	t.Run("CompletionProjectClass", func(t *testing.T) {
		source, position := typeDisplayTestSource(t, "var Runner Sprite\nfunc choose() {\n    var target Sprite = R|\n}\n")
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

func TestSpxClassForFile(t *testing.T) {
	t.Run("OtherFrameworkUsingSpxExtension", func(t *testing.T) {
		s := newFrameworkTestServerWithSpxExtension(t, map[string][]byte{"main.spx": []byte("echo 1\n")})
		assert.Nil(t, spxClassForFile(s.getProj(), "main.spx"))
		result, err := s.compileAt(s.getProj())
		require.NoError(t, err)
		assert.Nil(t, result)
	})
	t.Run("SDKAsAdditionalPackage", func(t *testing.T) {
		s := newSpxTestServer(t, nil)
		proj := s.getProj()
		proj.SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
			Ext: ".spx", Class: "App", PkgPaths: []string{"example.com/framework", SpxPkgPath},
		}}}}))
		assert.Nil(t, spxClassForFile(proj, "main.spx"))
		ctx := &definitionContext{proj: proj}
		assert.Empty(t, ctx.spxTypeName(spxTestType(t, s, "SpriteImpl")))
	})
}
