package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentRenameSpxResource(t *testing.T) {
	t.Run("SpxResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.turnTo "OtherSprite"
`),
			"MySprite.spx": []byte(`
onStart => {
	MySprite.turnTo "OtherSprite"
}
`),
			"OtherSprite.spx":                       []byte(``),
			"assets/index.json":                     []byte(`{}`),
			"assets/sprites/MySprite/index.json":    []byte(`{}`),
			"assets/sprites/OtherSprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		workspaceEdit, err := s.textDocumentRename(&RenameParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			Position:     Position{Line: 1, Character: 0},
			NewName:      "NewSprite",
		})
		require.NoError(t, err)
		require.Nil(t, workspaceEdit)

		workspaceEdit, err = s.textDocumentRename(&RenameParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			Position:     Position{Line: 1, Character: 16},
			NewName:      "NewSprite",
		})
		require.NoError(t, err)
		require.Nil(t, workspaceEdit)
	})
}

func TestServerRenameSpxResourcesBackdrop(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onBackdrop "backdrop1", func() {}
`),
			"MySprite.spx": []byte(`
onStart => {
	onBackdrop "backdrop1", func() {}
}
`),
			"assets/index.json": []byte(`{"backdrops":[{"name":"backdrop1","path":"backdrop1.png"}]}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/backdrops/backdrop1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxBackdropResourceID](t, id), "backdrop2")
		require.NoError(t, err)
		require.Len(t, changes, 2)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 1, Character: 12},
				End:   Position{Line: 1, Character: 21},
			},
			NewText: "backdrop2",
		}}, changes[s.toDocumentURI("main.spx")])

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 2, Character: 13},
				End:   Position{Line: 2, Character: 22},
			},
			NewText: "backdrop2",
		}}, changes[s.toDocumentURI("MySprite.spx")])
	})

	t.Run("ConstantName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
const Backdrop1 = "backdrop1"
onBackdrop Backdrop1, func() {}
`),
			"MySprite.spx": []byte(`
onStart => {
	onBackdrop Backdrop1, func() {}
}
`),
			"assets/index.json": []byte(`{"backdrops":[{"name":"backdrop1","path":"backdrop1.png"}]}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/backdrops/backdrop1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxBackdropResourceID](t, id), "backdrop2")
		require.NoError(t, err)
		require.Len(t, changes, 1)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 1, Character: 19},
				End:   Position{Line: 1, Character: 28},
			},
			NewText: "backdrop2",
		}}, changes[s.toDocumentURI("main.spx")])

		assert.Empty(t, changes[s.toDocumentURI("MySprite.spx")])
	})

	t.Run("TypedConstantName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
const Backdrop1 BackdropName = "backdrop1"
onBackdrop "backdrop1", func() {}
`),
			"MySprite.spx": []byte(`
onStart => {
	onBackdrop "backdrop1", func() {}
}
`),
			"assets/index.json": []byte(`{"backdrops":[{"name":"backdrop1","path":"backdrop1.png"}]}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/backdrops/backdrop1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxBackdropResourceID](t, id), "backdrop2")
		require.NoError(t, err)
		require.Len(t, changes, 2)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 1, Character: 32},
				End:   Position{Line: 1, Character: 41},
			},
			NewText: "backdrop2",
		}, {
			Range: Range{
				Start: Position{Line: 2, Character: 12},
				End:   Position{Line: 2, Character: 21},
			},
			NewText: "backdrop2",
		}}, changes[s.toDocumentURI("main.spx")])

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 2, Character: 13},
				End:   Position{Line: 2, Character: 22},
			},
			NewText: "backdrop2",
		}}, changes[s.toDocumentURI("MySprite.spx")])
	})
}

func TestServerRenameSpxResourcesSound(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play "Sound1"
`),
			"MySprite.spx": []byte(`
onStart => {
	play "Sound1"
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
			"assets/sounds/Sound1/index.json":    []byte(`{"path":"sound1.wav"}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sounds/Sound1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxSoundResourceID](t, id), "Sound2")
		require.NoError(t, err)
		require.Len(t, changes, 2)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 1, Character: 6},
				End:   Position{Line: 1, Character: 12},
			},
			NewText: "Sound2",
		}}, changes[s.toDocumentURI("main.spx")])

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 2, Character: 7},
				End:   Position{Line: 2, Character: 13},
			},
			NewText: "Sound2",
		}}, changes[s.toDocumentURI("MySprite.spx")])
	})
}

func TestServerRenameSpxResourcesSprite(t *testing.T) {
	t.Run("ClassfileTypeReference", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                         []byte("var pointer *((Runner))\nonStart => { echo pointer }\n"),
			"Runner.spx":                       nil,
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{}`),
		})
		requireNoDiagnostics(t, s)
		edit, err := s.renameResources([]XGoRenameResourceParams{{
			Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "Player",
		}})
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
			"file:///main.spx": {{Range: Range{Start: Position{Character: 15}, End: Position{Character: 21}}, NewText: "Player"}},
		})
	})

	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
Sprite1.turn Left
`),
			"Sprite1.spx": []byte(`
onStart => {
	Sprite1.turn Right
}
`),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sprites/Sprite1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxSpriteResourceID](t, id), "Sprite2")
		require.NoError(t, err)
		require.Len(t, changes, 2)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 1, Character: 0},
				End:   Position{Line: 1, Character: 7},
			},
			NewText: "Sprite2",
		}}, changes[s.toDocumentURI("main.spx")])

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 8},
			},
			NewText: "Sprite2",
		}}, changes[s.toDocumentURI("Sprite1.spx")])
	})

	// See https://github.com/goplus/builder/issues/1470.
	t.Run("WrongCodeWithInvalidType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	Sprite1.turn Right
	invalidFunc()
}

func invalidFunc() {
	invalidVar = [rand(-200,200), rand(-200,200)]
}
`),
			"Sprite1.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		diagnostics, err := s.diagnosticsAt(s.getProj())
		require.NoError(t, err)
		require.True(t, diagnostics.hasErrorSeverityDiagnostic)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sprites/Sprite1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxSpriteResourceID](t, id), "Sprite2")
		require.NoError(t, err)
		require.Len(t, changes, 1)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 8},
			},
			NewText: "Sprite2",
		}}, changes[s.toDocumentURI("main.spx")])
	})
}

func TestServerRenameSpxResourcesSpriteCostume(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.setCostume "costume1"
`),
			"MySprite.spx": []byte(`
onStart => {
	setCostume "costume1"
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume1"}]}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sprites/MySprite/costumes/costume1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxSpriteCostumeResourceID](t, id), "costume2")
		require.NoError(t, err)
		require.Len(t, changes, 2)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 1, Character: 21},
				End:   Position{Line: 1, Character: 29},
			},
			NewText: "costume2",
		}}, changes[s.toDocumentURI("main.spx")])

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 2, Character: 13},
				End:   Position{Line: 2, Character: 21},
			},
			NewText: "costume2",
		}}, changes[s.toDocumentURI("MySprite.spx")])
	})
}

func TestServerRenameSpxResourcesSpriteAnimation(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.animate "anim1"
`),
			"MySprite.spx": []byte(`
onStart => {
	animate "anim1"
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"fAnimations":{"anim1":{}}}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sprites/MySprite/animations/anim1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxSpriteAnimationResourceID](t, id), "anim2")
		require.NoError(t, err)
		require.Len(t, changes, 2)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 1, Character: 18},
				End:   Position{Line: 1, Character: 23},
			},
			NewText: "anim2",
		}}, changes[s.toDocumentURI("main.spx")])

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 2, Character: 10},
				End:   Position{Line: 2, Character: 15},
			},
			NewText: "anim2",
		}}, changes[s.toDocumentURI("MySprite.spx")])
	})
}

func TestServerRenameSpxResourcesWidget(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
onStart => {
	getWidget Monitor, "widget1"
}
`),
			"assets/index.json": []byte(`{"zorder":[{"name":"widget1"}]}`),
		}
		s := newSpxTestServer(t, m)
		result, err := analyzeSpx(s.getProjWithFile())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/widgets/widget1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(result, requireValueAs[SpxWidgetResourceID](t, id), "widget2")
		require.NoError(t, err)
		require.Len(t, changes, 1)

		assert.ElementsMatch(t, []TextEdit{{
			Range: Range{
				Start: Position{Line: 2, Character: 21},
				End:   Position{Line: 2, Character: 28},
			},
			NewText: "widget2",
		}}, changes[s.toDocumentURI("MySprite.spx")])
	})
}
