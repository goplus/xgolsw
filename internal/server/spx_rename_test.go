package server

import (
	"encoding/json"
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/backdrops/backdrop1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxBackdropResourceID](t, id), "backdrop2")
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/backdrops/backdrop1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxBackdropResourceID](t, id), "backdrop2")
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/backdrops/backdrop1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxBackdropResourceID](t, id), "backdrop2")
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sounds/Sound1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxSoundResourceID](t, id), "Sound2")
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
	t.Run("PreservedBindings", func(t *testing.T) {
		for _, tt := range []struct {
			name, source, sprite string
		}{
			{"AutoBinding", "onStart => { echo Runner }\n", ""},
			{"QualifiedBinding", "onStart => { Player := 123; echo this.Runner; echo Player }\n", ""},
			{"SpriteMembers", "onStart => { Runner.work() }\n", "var health int\nfunc Work() { echo health; echo Runner }\n"},
			{"TypeAlias", "type Alias = Runner\nvar target *Alias\nonStart => { echo target }\n", ""},
			{"TypeReference", "var target *Runner\nonStart => { echo target }\n", ""},
			{"Labels", `onStart => {
Loop:
	for i := 0; i < 1; i++ {
		echo Runner
		break Loop
	}
}
`, ""},
			{"UTF16", "onStart => { echo \"\U0001f600\"; echo Runner }\n", ""},
			{"ResourceConstant", "const target = \"Runner\"\nonStart => { Runner.turnTo target; echo target }\n", ""},
			{"ConstantConversion", "const target = \"Run\" + string(\"ner\")\nonStart => { Runner.turnTo target }\n", ""},
			{"ParenthesizedConversion", "const target = \"Run\" + (string)(\"ner\")\nonStart => { Runner.turnTo target }\n", ""},
			{"ResourceTypeConversion", "const target = SpriteName(\"Runner\")\nonStart => { Runner.turnTo target }\n", ""},
			{"TypeAliasConversion", "type Alias = Runner\nvar target = (*Alias)(nil)\nonStart => { echo target }\n", ""},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx":                         []byte(tt.source),
					"Runner.spx":                       []byte(tt.sprite),
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{}`),
				}
				s := newSpxTestServer(t, files)
				t.Cleanup(s.Close)
				requireNoDiagnostics(t, s)
				edit, err := s.renameResources([]XGoRenameResourceParams{{
					Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "Player",
				}})
				require.NoError(t, err)
				require.NotNil(t, edit)
				for uri, edits := range edit.Changes {
					filename, err := s.fromDocumentURI(uri)
					require.NoError(t, err)
					files[filename] = []byte(applyResourceRenameTestEdits(t, string(files[filename]), edits))
				}
				files["Player.spx"] = files["Runner.spx"]
				delete(files, "Runner.spx")
				files["assets/sprites/Player/index.json"] = files["assets/sprites/Runner/index.json"]
				delete(files, "assets/sprites/Runner/index.json")
				updated := newSpxTestServer(t, files)
				t.Cleanup(updated.Close)
				requireNoDiagnostics(t, updated)
				class := spxSpriteTypeForFile(updated.requestProject(), "Player.spx")
				require.NotNil(t, class)
				assert.Equal(t, "Player", class.Obj().Name())
				if tt.name == "QualifiedBinding" {
					assert.Contains(t, string(files["main.spx"]), "echo this.Player; echo Player")
				}
				if tt.name == "ResourceConstant" {
					assert.Contains(t, string(files["main.spx"]), `Player.turnTo "Player"; echo target`)
					assert.Contains(t, string(files["main.spx"]), `const target = "Runner"`)
				}
			})
		}
	})

	t.Run("CommandAliases", func(t *testing.T) {
		for _, tt := range []struct{ name, command string }{
			{"XGo", CommandXGoRenameResources},
			{"Spx", CommandSpxRenameResources},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx":                         []byte("onStart => { Player := 123; echo Runner; echo Player }\n"),
					"Runner.spx":                       nil,
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{}`),
				})
				t.Cleanup(s.Close)
				argument, err := json.Marshal(XGoRenameResourceParams{
					Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "Player",
				})
				require.NoError(t, err)
				edit, err := s.workspaceExecuteCommand(&ExecuteCommandParams{Command: tt.command, Arguments: []json.RawMessage{argument}})
				assert.ErrorContains(t, err, "binding would change")
				assert.Nil(t, edit)
			})
		}
	})

	t.Run("ConflictingBatch", func(t *testing.T) {
		source := "play \"Beep\"\nonStart => { Player := 123; echo Runner; echo Player }\n"
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                         []byte(source),
			"Runner.spx":                       nil,
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{}`),
			"assets/sounds/Beep/index.json":    []byte(`{}`),
		})
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)
		edit, err := s.renameResources([]XGoRenameResourceParams{
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Beep"}, NewName: "Chime"},
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "Player"},
		})
		assert.ErrorContains(t, err, "binding would change")
		assert.Nil(t, edit)
		file, ok := s.getProj().File("main.spx")
		require.True(t, ok)
		assert.Equal(t, source, string(file.Content))
	})

	t.Run("MixedResources", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":                         []byte("const sound = \"Be\" + string(\"ep\")\nonStart => { echo Runner; echo Other; play sound }\n"),
			"Runner.spx":                       []byte("var health int\nfunc Work() { echo health; echo Other }\n"),
			"Other.spx":                        []byte("onStart => { Runner.work() }\n"),
			"types.xgo":                        []byte("func castSprite(sprite Runner) Runner { return Runner(sprite) }\n"),
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{}`),
			"assets/sprites/Other/index.json":  []byte(`{}`),
			"assets/sounds/Beep/index.json":    []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)
		edit, err := s.renameResources([]XGoRenameResourceParams{
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "Player"},
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Other"}, NewName: "Target"},
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Beep"}, NewName: "Chime"},
		})
		require.NoError(t, err)
		require.NotNil(t, edit)
		for uri, edits := range edit.Changes {
			filename, err := s.fromDocumentURI(uri)
			require.NoError(t, err)
			files[filename] = []byte(applyResourceRenameTestEdits(t, string(files[filename]), edits))
		}
		for oldName, newName := range map[string]string{"Runner": "Player", "Other": "Target"} {
			files[newName+".spx"] = files[oldName+".spx"]
			delete(files, oldName+".spx")
			files["assets/sprites/"+newName+"/index.json"] = files["assets/sprites/"+oldName+"/index.json"]
			delete(files, "assets/sprites/"+oldName+"/index.json")
		}
		files["assets/sounds/Chime/index.json"] = files["assets/sounds/Beep/index.json"]
		delete(files, "assets/sounds/Beep/index.json")
		updated := newSpxTestServer(t, files)
		t.Cleanup(updated.Close)
		requireNoDiagnostics(t, updated)
		assert.Contains(t, string(files["main.spx"]), `const sound = "Chime"`)
	})

	t.Run("InvalidClassName", func(t *testing.T) {
		for _, tt := range []struct{ name, newName string }{
			{"Empty", ""},
			{"NonIdentifier", "Player-Name"},
			{"Keyword", "type"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx":                         nil,
					"Runner.spx":                       nil,
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{}`),
				})
				t.Cleanup(s.Close)
				edit, err := s.renameResources([]XGoRenameResourceParams{{
					Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: tt.newName,
				}})
				assert.ErrorContains(t, err, "invalid class name")
				assert.Nil(t, edit)
			})
		}
	})

	t.Run("ChangedClassRole", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"Runner.spx":                       nil,
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{}`),
		})
		t.Cleanup(s.Close)
		edit, err := s.renameResources([]XGoRenameResourceParams{{
			Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "main",
		}})
		assert.ErrorContains(t, err, "classfile")
		assert.Nil(t, edit)
	})

	t.Run("MissingClassfile", func(t *testing.T) {
		source := "var Runner Sprite\nonStart => { echo Runner }\n"
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                         []byte(source),
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{}`),
		})
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)
		proj := s.requestProject()
		revision := proj.Revision()
		edit, err := s.renameResources([]XGoRenameResourceParams{{
			Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "Player",
		}})
		assert.ErrorContains(t, err, "unresolved binding")
		assert.Nil(t, edit)
		assert.Equal(t, revision, proj.Revision())
		file, ok := proj.File("main.spx")
		require.True(t, ok)
		assert.Equal(t, source, string(file.Content))
	})

	t.Run("BindingConflicts", func(t *testing.T) {
		for _, tt := range []struct {
			name, source, declarations, newName string
		}{
			{"LocalVariable", "onStart => {\nPlayer := 123\necho Runner\necho Player\n}\n", "", "Player"},
			{"LocalType", "onStart => {\ntype Player int\nvar target *Runner\necho target\n}\n", "", "Player"},
			{"ImportedConstant", "onStart => { echo Runner; echo Mouse }\n", "", "Mouse"},
			{"ProjectField", "var Player int\nonStart => { echo Runner }\n", "", "Player"},
			{"ProjectMethod", "func Player() {}\nonStart => { echo Runner }\n", "", "Player"},
			{"PackageType", "onStart => { echo Runner }\n", "type Player struct{}\n", "Player"},
			{"PackageVariable", "onStart => { echo Runner }\n", "var Player int\n", "Player"},
			{"BuiltinType", "onStart => { var target *int; echo target; echo Runner }\n", "", "int"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx":                         []byte(tt.source),
					"Runner.spx":                       nil,
					"types.xgo":                        []byte(tt.declarations),
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{}`),
				})
				t.Cleanup(s.Close)
				requireNoDiagnostics(t, s)
				proj := s.requestProject()
				revision := proj.Revision()
				edit, err := s.renameResources([]XGoRenameResourceParams{{
					Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: tt.newName,
				}})
				assert.ErrorContains(t, err, "conflict")
				assert.Nil(t, edit)
				assert.Equal(t, revision, proj.Revision())
				file, ok := proj.File("main.spx")
				require.True(t, ok)
				assert.Equal(t, tt.source, string(file.Content))
			})
		}
	})

	t.Run("ClassfileTypeReference", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx": []byte(`var pointer *((Runner))
onStart => { echo pointer }
`),
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sprites/Sprite1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxSpriteResourceID](t, id), "Sprite2")
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		diagnostics, err := s.diagnosticsAt(s.getProj())
		require.NoError(t, err)
		require.True(t, diagnostics.hasErrorSeverityDiagnostic)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sprites/Sprite1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxSpriteResourceID](t, id), "Sprite2")
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sprites/MySprite/costumes/costume1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxSpriteCostumeResourceID](t, id), "costume2")
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/sprites/MySprite/animations/anim1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxSpriteAnimationResourceID](t, id), "anim2")
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
		result, err := analyzeSpx(s.syncProject())
		require.NoError(t, err)
		requireNoDiagnostics(t, s)

		id, err := ParseSpxResourceURI(SpxResourceURI("spx://resources/widgets/widget1"))
		require.NoError(t, err)

		changes, err := s.renameSpxResource(s.syncProject(), result, requireValueAs[SpxWidgetResourceID](t, id), "widget2")
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
