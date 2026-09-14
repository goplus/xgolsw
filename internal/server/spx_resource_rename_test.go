package server

import (
	"go/constant"
	gotypes "go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerSpxRenameResourceAtRefs(t *testing.T) {
	t.Run("LiteralsAndConstants", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":      []byte("echo \"\U0001f600\", \"Studio\", Scene, TypedScene, Scene, \"Other\", PairScene\n"),
			"resources.xgo": []byte("const Scene = \"Studio\"\nconst TypedScene string = \"Studio\"\nconst OtherScene, PairScene = \"Other\", \"Studio\"\n"),
		})
		proj := s.getProj()
		call := spxResourceTestCall(t, proj, "main.xgo")
		require.Len(t, call.Args, 7)
		id := SpxBackdropResourceID{BackdropName: "Studio"}
		result := newCompileResult(proj, s.lookupPkgDoc)
		result.spxResourceRefs = []SpxResourceRef{
			{ID: id, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[1]},
			{ID: id, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[1]},
			{ID: id, Kind: SpxResourceRefKindConstantReference, Node: call.Args[2]},
			{ID: id, Kind: SpxResourceRefKindConstantReference, Node: call.Args[3]},
			{ID: id, Kind: SpxResourceRefKindConstantReference, Node: call.Args[4]},
			{ID: SpxBackdropResourceID{BackdropName: "Other"}, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[5]},
			{ID: id, Kind: SpxResourceRefKindConstantReference, Node: call.Args[6]},
		}
		want := map[DocumentURI][]TextEdit{
			"file:///main.xgo": {{Range: Range{Start: Position{Character: 12}, End: Position{Character: 18}}, NewText: "Park"}},
			"file:///resources.xgo": {
				{Range: Range{Start: Position{Character: 15}, End: Position{Character: 21}}, NewText: "Park"},
				{Range: Range{Start: Position{Line: 1, Character: 27}, End: Position{Line: 1, Character: 33}}, NewText: "Park"},
				{Range: Range{Start: Position{Line: 2, Character: 40}, End: Position{Line: 2, Character: 46}}, NewText: "Park"},
			},
		}
		assert.Equal(t, want, s.spxRenameResourceAtRefs(result, id, "Park"))
		assert.Empty(t, s.spxRenameResourceAtRefs(result, SpxSoundResourceID{SoundName: "Studio"}, "Park"))
		assert.Equal(t, want, s.spxRenameResourceAtRefs(result, id, "Park"))
	})

	t.Run("ConstantInitializers", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			want   TextEdit
		}{
			{
				name: "DefinedStringType", source: "type Name string\nconst Scene Name = \"Studio\"\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Line: 1, Character: 20}, End: Position{Line: 1, Character: 26}}, NewText: "Park"},
			},
			{
				name: "Alias", source: "const Base = \"Studio\"\nconst Scene = Base\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Line: 1, Character: 14}, End: Position{Line: 1, Character: 18}}, NewText: `"Park"`},
			},
			{
				name: "Concatenation", source: "const Scene = \"Stu\" + \"dio\"\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Character: 14}, End: Position{Character: 27}}, NewText: `"Park"`},
			},
			{
				name: "RawConcatenation", source: "const Scene = \"Stu\" + `d\rio`\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Character: 14}, End: Position{Character: 28}}, NewText: `"Park"`},
			},
			{
				name: "Parentheses", source: "const Scene = (\"Studio\")\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Character: 14}, End: Position{Character: 24}}, NewText: `"Park"`},
			},
			{
				name: "RawString", source: "const Scene = `Studio`\necho Scene\n",
				want: TextEdit{Range: Range{Start: Position{Character: 15}, End: Position{Character: 21}}, NewText: "Park"},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				proj := s.getProj()
				call := spxResourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 1)
				id := SpxBackdropResourceID{BackdropName: "Studio"}
				result := newCompileResult(proj, s.lookupPkgDoc)
				result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: SpxResourceRefKindConstantReference, Node: call.Args[0]})
				changes := s.spxRenameResourceAtRefs(result, id, "Park")
				require.Equal(t, map[DocumentURI][]TextEdit{"file:///main.xgo": {tt.want}}, changes)
				edit := changes["file:///main.xgo"][0]
				content := []byte(tt.source)
				updated := tt.source[:PositionOffset(content, edit.Range.Start)] + edit.NewText + tt.source[PositionOffset(content, edit.Range.End):]
				updatedProj := newTestServer(t, map[string][]byte{"main.xgo": []byte(updated)}).getProj()
				spxResourceTestCall(t, updatedProj, "main.xgo")
				typeInfo, err := updatedProj.TypeInfo()
				require.NoError(t, err)
				scene := requireValueAs[*gotypes.Const](t, typeInfo.Pkg.Scope().Lookup("Scene"))
				assert.Equal(t, `"Park"`, scene.Val().ExactString())
			})
		}
	})

	t.Run("StringEscaping", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			literal     string
			newName     string
			wantLiteral string
		}{
			{name: "Quotes", literal: `"Studio"`, newName: `A"B`, wantLiteral: `"A\"B"`},
			{name: "Backslash", literal: `"Studio"`, newName: `A\B`, wantLiteral: `"A\\B"`},
			{name: "ControlCharacters", literal: `"Studio"`, newName: "A\n\r\t\x00B", wantLiteral: `"A\n\r\t\x00B"`},
			{name: "Unicode", literal: `"Studio"`, newName: "\U0001f600", wantLiteral: "\"\U0001f600\""},
			{name: "Empty", literal: `"Studio"`, wantLiteral: `""`},
			{name: "EscapedOriginal", literal: `"Stu\x64io"`, newName: "Park", wantLiteral: `"Park"`},
			{name: "Interpolation", literal: `"Studio"`, newName: "${name}", wantLiteral: `"\x24{name}"`},
			{name: "DollarEscape", literal: `"Studio"`, newName: "$$", wantLiteral: `"\x24\x24"`},
			{name: "RawQuotesAndBackslash", literal: "`Studio`", newName: `A"\B`, wantLiteral: "`A\"\\B`"},
			{name: "RawBacktick", literal: "`Studio`", newName: "A`B", wantLiteral: "\"A`B\""},
			{name: "RawControlCharacters", literal: "`Studio`", newName: "A\n\r\t\x00B", wantLiteral: `"A\n\r\t\x00B"`},
			{name: "RawInterpolation", literal: "`Studio`", newName: "${name} $$", wantLiteral: `"\x24{name} \x24\x24"`},
			{name: "RawCarriageReturn", literal: "`Stu\rdio`", newName: "Park", wantLiteral: "`Park`"},
			{name: "RawCarriageReturnWithQuoteChange", literal: "`Stu\rdio`", newName: "A`B", wantLiteral: "\"A`B\""},
			{name: "RawMultipleCarriageReturns", literal: "`S\rtu\r\rdio\r`", newName: "Park", wantLiteral: "`Park`"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, form := range []struct {
					name   string
					prefix string
					suffix string
					kind   SpxResourceRefKind
				}{
					{name: "Literal", prefix: "echo ", suffix: "\n", kind: SpxResourceRefKindStringLiteral},
					{name: "Constant", prefix: "const Scene = ", suffix: "\necho Scene\n", kind: SpxResourceRefKindConstantReference},
				} {
					t.Run(form.name, func(t *testing.T) {
						source := form.prefix + tt.literal + form.suffix
						s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
						proj := s.getProj()
						call := spxResourceTestCall(t, proj, "main.xgo")
						require.Len(t, call.Args, 1)
						id := SpxBackdropResourceID{BackdropName: "Studio"}
						result := newCompileResult(proj, s.lookupPkgDoc)
						result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: form.kind, Node: call.Args[0]})
						changes := s.spxRenameResourceAtRefs(result, id, tt.newName)
						require.Len(t, changes, 1)
						require.Len(t, changes["file:///main.xgo"], 1)
						edit := changes["file:///main.xgo"][0]
						content := []byte(source)
						updated := source[:PositionOffset(content, edit.Range.Start)] + edit.NewText + source[PositionOffset(content, edit.Range.End):]
						assert.Equal(t, form.prefix+tt.wantLiteral+form.suffix, updated)
						updatedProj := newTestServer(t, map[string][]byte{"main.xgo": []byte(updated)}).getProj()
						updatedCall := spxResourceTestCall(t, updatedProj, "main.xgo")
						require.Len(t, updatedCall.Args, 1)
						typeInfo, err := updatedProj.TypeInfo()
						require.NoError(t, err)
						value := typeInfo.Types[updatedCall.Args[0]].Value
						require.NotNil(t, value)
						require.Equal(t, constant.String, value.Kind())
						assert.Equal(t, tt.newName, constant.StringVal(value))
					})
				}
			})
		}
	})

	t.Run("UneditableConstants", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			source  string
			oldName string
		}{
			{name: "ImplicitInitializer", source: "const (\n First = \"Studio\"\n Scene\n)\necho Scene, \"Studio\"\n", oldName: "Studio"},
			{name: "ExternalDeclaration", source: "import . \"time\"\necho RFC3339, \"2006-01-02T15:04:05Z07:00\"\n", oldName: "2006-01-02T15:04:05Z07:00"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				proj := s.getProj()
				call := spxResourceTestCall(t, proj, "main.xgo")
				require.Len(t, call.Args, 2)
				id := SpxBackdropResourceID{BackdropName: tt.oldName}
				result := newCompileResult(proj, s.lookupPkgDoc)
				result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: SpxResourceRefKindConstantReference, Node: call.Args[0]})
				assert.Empty(t, s.spxRenameResourceAtRefs(result, id, "Park"))
				result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[1]})
				wantRange := RangeForNode(proj, call.Args[1])
				wantRange.Start.Character++
				wantRange.End.Character--
				assert.Equal(t, map[DocumentURI][]TextEdit{
					"file:///main.xgo": {{Range: wantRange, NewText: "Park"}},
				}, s.spxRenameResourceAtRefs(result, id, "Park"))
			})
		}
	})

	t.Run("IdentifierAndSpriteContext", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("var Runner int\necho Runner, \"idle\", \"idle\"\n"),
		})
		proj := s.getProj()
		call := spxResourceTestCall(t, proj, "main.xgo")
		require.Len(t, call.Args, 3)
		result := newCompileResult(proj, s.lookupPkgDoc)
		spriteID := SpxSpriteResourceID{SpriteName: "Runner"}
		costumeID := SpxSpriteCostumeResourceID{SpriteName: "Runner", CostumeName: "idle"}
		result.spxResourceRefs = []SpxResourceRef{
			{ID: spriteID, Kind: SpxResourceRefKindAutoBindingReference, Node: call.Args[0]},
			{ID: costumeID, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[1]},
			{ID: SpxSpriteCostumeResourceID{SpriteName: "Other", CostumeName: "idle"}, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[2]},
		}
		assert.Equal(t, map[DocumentURI][]TextEdit{
			"file:///main.xgo": {{Range: Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 11}}, NewText: "Player"}},
		}, s.spxRenameResourceAtRefs(result, spriteID, "Player"))
		assert.Equal(t, map[DocumentURI][]TextEdit{
			"file:///main.xgo": {{Range: Range{Start: Position{Line: 1, Character: 14}, End: Position{Line: 1, Character: 18}}, NewText: "rest"}},
		}, s.spxRenameResourceAtRefs(result, costumeID, "rest"))
	})
}

func TestServerSpxRenameSpriteResourceTypeReferences(t *testing.T) {
	s := newTestServer(t, map[string][]byte{
		"types.xgo": []byte("type Runner struct{}\ntype Other struct{}\ntype Unclassified struct{}\nvar pointer *Runner\nvar list []Runner\nvar unrelated Other\nvar unclassified Unclassified\n"),
		"main.xgo":  []byte("var runner Runner\necho \"Runner\", Runner{}, runner\n"),
	})
	proj := s.getProj()
	call := spxResourceTestCall(t, proj, "main.xgo")
	require.Len(t, call.Args, 3)
	_, err := proj.ASTFile("types.xgo")
	require.NoError(t, err)
	typeInfo, err := proj.TypeInfo()
	require.NoError(t, err)
	runner := requireValueAs[*gotypes.TypeName](t, typeInfo.Pkg.Scope().Lookup("Runner"))
	other := requireValueAs[*gotypes.TypeName](t, typeInfo.Pkg.Scope().Lookup("Other"))
	result := newCompileResult(proj, s.lookupPkgDoc)
	id := SpxSpriteResourceID{SpriteName: "Runner"}
	result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[0]})
	result.spxSpriteTypes[other.Type()] = struct{}{}
	want := map[DocumentURI][]TextEdit{
		"file:///main.xgo": {{Range: Range{Start: Position{Line: 1, Character: 6}, End: Position{Line: 1, Character: 12}}, NewText: "Player"}},
	}
	changes, err := s.spxRenameSpriteResource(result, id, "Player")
	require.NoError(t, err)
	assert.Equal(t, want, changes, "only classified sprite types should be renamed")

	result.spxSpriteTypes[runner.Type()] = struct{}{}
	want["file:///main.xgo"] = append(want["file:///main.xgo"],
		TextEdit{Range: Range{Start: Position{Character: 11}, End: Position{Character: 17}}, NewText: "Player"},
		TextEdit{Range: Range{Start: Position{Line: 1, Character: 15}, End: Position{Line: 1, Character: 21}}, NewText: "Player"},
	)
	want["file:///types.xgo"] = []TextEdit{
		{Range: Range{Start: Position{Line: 3, Character: 13}, End: Position{Line: 3, Character: 19}}, NewText: "Player"},
		{Range: Range{Start: Position{Line: 4, Character: 11}, End: Position{Line: 4, Character: 17}}, NewText: "Player"},
	}
	changes, err = s.spxRenameSpriteResource(result, id, "Player")
	require.NoError(t, err)
	require.Len(t, changes, len(want))
	for uri, edits := range want {
		assert.ElementsMatch(t, edits, changes[uri], "edits for %s", uri)
	}
	changes, err = s.spxRenameSpriteResource(result, SpxSpriteResourceID{SpriteName: "Unclassified"}, "Player")
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestServerSpxRenameResourcesWithCompileResult(t *testing.T) {
	t.Run("Validation", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			uri     SpxResourceURI
			wantErr string
		}{
			{name: "BackdropAlreadyExists", uri: "spx://resources/backdrops/Studio", wantErr: `backdrop resource "Taken" already exists`},
			{name: "SoundAlreadyExists", uri: "spx://resources/sounds/Beep", wantErr: `sound resource "Taken" already exists`},
			{name: "SpriteAlreadyExists", uri: "spx://resources/sprites/Runner", wantErr: `sprite resource "Taken" already exists`},
			{name: "CostumeAlreadyExists", uri: "spx://resources/sprites/Runner/costumes/idle", wantErr: `sprite costume resource "Taken" already exists`},
			{name: "AnimationAlreadyExists", uri: "spx://resources/sprites/Runner/animations/walk", wantErr: `sprite animation resource "Taken" already exists`},
			{name: "WidgetAlreadyExists", uri: "spx://resources/widgets/Score", wantErr: `widget resource "Taken" already exists`},
			{name: "CostumeWithoutSprite", uri: "spx://resources/sprites/Missing/costumes/idle", wantErr: `sprite resource "Missing" not found`},
			{name: "AnimationWithoutSprite", uri: "spx://resources/sprites/Missing/animations/walk", wantErr: `sprite resource "Missing" not found`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"assets/index.json":                []byte(`{"backdrops":[{"name":"Studio"},{"name":"Taken"}],"zorder":[{"name":"Score"},{"name":"Taken"}]}`),
					"assets/sounds/Beep/index.json":    []byte(`{}`),
					"assets/sounds/Taken/index.json":   []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle"},{"name":"Taken"}],"fAnimations":{"walk":{},"Taken":{}}}`),
					"assets/sprites/Taken/index.json":  []byte(`{}`),
				})
				proj := s.getProj()
				set, err := NewSpxResourceSet(proj)
				require.NoError(t, err)
				result := newCompileResult(proj, s.lookupPkgDoc)
				result.spxResourceSet = *set
				edit, err := s.spxRenameResourcesWithCompileResult(result, []XGoRenameResourceParams{{
					Resource: XGoResourceIdentifier{URI: tt.uri}, NewName: "Taken",
				}})
				assert.EqualError(t, err, "failed to rename spx resource \""+string(tt.uri)+"\": "+tt.wantErr)
				assert.Nil(t, edit)
			})
		}
	})

	t.Run("Batch", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":                         []byte("echo \"Studio\", \"Beep\", \"Runner\", \"idle\", \"walk\", \"Score\"\n"),
			"assets/index.json":                []byte(`{"backdrops":[{"name":"Studio"}],"zorder":[{"name":"Score"}]}`),
			"assets/sounds/Beep/index.json":    []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle"}],"fAnimations":{"walk":{}}}`),
			"assets/sprites/Other/index.json":  []byte(`{"costumes":[{"name":"walk"}],"fAnimations":{"idle":{}}}`),
		})
		proj := s.getProj()
		call := spxResourceTestCall(t, proj, "main.xgo")
		require.Len(t, call.Args, 6)
		set, err := NewSpxResourceSet(proj)
		require.NoError(t, err)
		result := newCompileResult(proj, s.lookupPkgDoc)
		result.spxResourceSet = *set
		var params []XGoRenameResourceParams
		// Names in other resource kinds or other sprites do not conflict.
		for i, rename := range []struct {
			id      SpxResourceID
			newName string
		}{
			{SpxBackdropResourceID{BackdropName: "Studio"}, "Beep"},
			{SpxSoundResourceID{SoundName: "Beep"}, "Runner"},
			{SpxSpriteResourceID{SpriteName: "Runner"}, "Score"},
			{SpxSpriteCostumeResourceID{SpriteName: "Runner", CostumeName: "idle"}, "walk"},
			{SpxSpriteAnimationResourceID{SpriteName: "Runner", AnimationName: "walk"}, "idle"},
			{SpxWidgetResourceID{WidgetName: "Score"}, "Studio"},
		} {
			result.addSpxResourceRef(SpxResourceRef{ID: rename.id, Kind: SpxResourceRefKindStringLiteral, Node: call.Args[i]})
			params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: rename.id.URI()}, NewName: rename.newName})
		}
		params = append(params, params[0])
		edit, err := s.spxRenameResourcesWithCompileResult(result, params)
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
			"file:///main.xgo": {
				{Range: Range{Start: Position{Character: 6}, End: Position{Character: 12}}, NewText: "Beep"},
				{Range: Range{Start: Position{Character: 16}, End: Position{Character: 20}}, NewText: "Runner"},
				{Range: Range{Start: Position{Character: 24}, End: Position{Character: 30}}, NewText: "Score"},
				{Range: Range{Start: Position{Character: 34}, End: Position{Character: 38}}, NewText: "walk"},
				{Range: Range{Start: Position{Character: 42}, End: Position{Character: 46}}, NewText: "idle"},
				{Range: Range{Start: Position{Character: 50}, End: Position{Character: 55}}, NewText: "Studio"},
			},
		})
		assert.NotNil(t, set.Backdrop("Studio"))
		assert.Nil(t, set.Backdrop("Beep"))

		params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Missing/costumes/idle"}, NewName: "rest"})
		edit, err = s.spxRenameResourcesWithCompileResult(result, params)
		assert.ErrorContains(t, err, `sprite resource "Missing" not found`)
		assert.Nil(t, edit, "a failed batch must not return partial edits")
	})

	t.Run("Empty", func(t *testing.T) {
		s := newTestServer(t, nil)
		edit, err := s.spxRenameResourcesWithCompileResult(newCompileResult(s.getProj(), s.lookupPkgDoc), nil)
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{})
	})

	t.Run("InvalidURI", func(t *testing.T) {
		s := newTestServer(t, nil)
		edit, err := s.spxRenameResourcesWithCompileResult(newCompileResult(s.getProj(), s.lookupPkgDoc), []XGoRenameResourceParams{{
			Resource: XGoResourceIdentifier{URI: "file:///assets/Studio"}, NewName: "Park",
		}})
		assert.EqualError(t, err, "failed to parse spx resource URI: invalid spx resource URI: file:///assets/Studio")
		assert.Nil(t, edit)
	})
}
