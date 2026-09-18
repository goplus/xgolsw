package server

import (
	gotypes "go/types"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (s *Server) renameSpxResource(result *spxAnalysis, id resourceID, newName string) (map[DocumentURI][]TextEdit, error) {
	edit, err := s.renameSpxResources(result, []XGoRenameResourceParams{{
		Resource: XGoResourceIdentifier{URI: id.URI()}, NewName: newName,
	}})
	if err != nil {
		return nil, err
	}
	return edit.Changes, nil
}

func TestServerRenameSpxResourcesSpriteTypeReferences(t *testing.T) {
	t.Run("SourceReferences", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
		}{
			{"Parenthesized", "var value *((Runner))\necho value\n"},
			{"Alias", "type Alias = Runner\nvar value Alias\necho value\n"},
			{"Embedded", "type Container struct { Runner }\nvar value Container\necho value\n"},
			{"LineDirective", "//line virtual.xgo:100:20\nvar value *Runner\necho value\n"},
			{"UTF16", "echo \"\U0001f600\", Runner{}\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"types.xgo": []byte("type Runner struct{}\n"),
					"main.xgo":  []byte(tt.source),
				})
				proj := s.getProj()
				info, err := proj.TypeInfo()
				require.NoError(t, err)
				result := newSpxAnalysis(proj)
				result.spxSpriteTypes[info.Pkg.Scope().Lookup("Runner").Type()] = struct{}{}
				changes, err := s.renameSpxResource(result, SpxSpriteResourceID{SpriteName: "Runner"}, "Player")
				require.NoError(t, err)
				require.Len(t, changes, 1)
				edits := changes["file:///main.xgo"]
				require.Len(t, edits, 1)
				edit := edits[0]
				start := PositionOffset([]byte(tt.source), edit.Range.Start)
				end := PositionOffset([]byte(tt.source), edit.Range.End)
				assert.Equal(t, "Runner", tt.source[start:end])
				assert.Equal(t, "Player", edit.NewText)
				updated := tt.source[:start] + edit.NewText + tt.source[end:]
				assert.Equal(t, strings.ReplaceAll(tt.source, "Runner", "Player"), updated)
				updatedProj := newTestServer(t, map[string][]byte{
					"types.xgo": []byte("type Player struct{}\n"),
					"main.xgo":  []byte(updated),
				}).getProj()
				_, err = updatedProj.TypeInfo()
				require.NoError(t, err)
			})
		}
	})

	s := newTestServer(t, map[string][]byte{
		"types.xgo": []byte("type Runner struct{}\ntype Other struct{}\ntype Unclassified struct{}\nvar pointer *Runner\nvar list []Runner\nvar unrelated Other\nvar unclassified Unclassified\n"),
		"main.xgo":  []byte("var runner Runner\necho \"Runner\", Runner{}, runner\n"),
	})
	proj := s.getProj()
	call := resourceTestCall(t, proj, "main.xgo")
	require.Len(t, call.Args, 3)
	_, err := proj.ASTFile("types.xgo")
	require.NoError(t, err)
	typeInfo, err := proj.TypeInfo()
	require.NoError(t, err)
	runner := requireValueAs[*gotypes.TypeName](t, typeInfo.Pkg.Scope().Lookup("Runner"))
	other := requireValueAs[*gotypes.TypeName](t, typeInfo.Pkg.Scope().Lookup("Other"))
	result := newSpxAnalysis(proj)
	id := SpxSpriteResourceID{SpriteName: "Runner"}
	result.addResourceRef(resourceRef{ID: id, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[0]})
	result.spxSpriteTypes[other.Type()] = struct{}{}
	want := map[DocumentURI][]TextEdit{
		"file:///main.xgo": {{Range: Range{Start: Position{Line: 1, Character: 6}, End: Position{Line: 1, Character: 12}}, NewText: "Player"}},
	}
	changes, err := s.renameSpxResource(result, id, "Player")
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
	changes, err = s.renameSpxResource(result, id, "Player")
	require.NoError(t, err)
	require.Len(t, changes, len(want))
	for uri, edits := range want {
		assert.ElementsMatch(t, edits, changes[uri], "edits for %s", uri)
	}
	changes, err = s.renameSpxResource(result, SpxSpriteResourceID{SpriteName: "Unclassified"}, "Player")
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestServerRenameSpxResources(t *testing.T) {
	t.Run("LiteralConversions", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			source     string
			collection string
		}{
			{"Intrinsic", "echo spx.BackdropName(\"Shared\")\n", "backdrops"},
			{"Nested", "echo spx.BackdropName(string((\"Shared\")))\n", "backdrops"},
			{"Initializer", "const name = spx.BackdropName(\"Shared\")\nsetBackdrop name\n", "backdrops"},
			{"Contextual", "play spx.BackdropName(\"Shared\")\n", "sounds"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := "import \"github.com/goplus/spx/v3\"\n" + tt.source
				name := "Shared"
				for _, newName := range []string{"Renamed", "Final"} {
					files := map[string][]byte{"main.spx": []byte(source)}
					if tt.collection == "backdrops" {
						files["assets/index.json"] = []byte(`{"backdrops":[{"name":"` + name + `"}]}`)
					} else {
						files["assets/index.json"] = []byte(`{}`)
						files["assets/sounds/"+name+"/index.json"] = []byte(`{}`)
					}
					s := newSpxTestServer(t, files)
					requireNoDiagnostics(t, s)
					uri := XGoResourceURI("spx://resources/" + tt.collection + "/" + name)
					links, err := s.documentLinksForResources(s.getProj(), "main.spx")
					require.NoError(t, err)
					require.NotEmpty(t, links)
					for _, link := range links {
						require.NotNil(t, link.Target)
						assert.Equal(t, URI(uri), *link.Target)
					}
					edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: uri}, NewName: newName}})
					require.NoError(t, err)
					require.NotNil(t, edit)
					updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.spx"])
					assert.Equal(t, strings.ReplaceAll(source, name, newName), updated)
					source = updated
					name = newName
				}
			})
		}
	})

	t.Run("ConstantExpressionDependency", func(t *testing.T) {
		const source = "const base BackdropName = \"Shared\"\nconst sound = base + \"Suffix\"\nsetBackdrop base\nplay sound\n"
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                              []byte(source),
			"assets/index.json":                     []byte(`{"backdrops":[{"name":"Shared"}]}`),
			"assets/sounds/SharedSuffix/index.json": []byte(`{}`),
		})
		requireNoDiagnostics(t, s)
		edit, err := s.renameResources([]XGoRenameResourceParams{{
			Resource: XGoResourceIdentifier{URI: "spx://resources/backdrops/Shared"}, NewName: "Scene",
		}})
		require.NoError(t, err)
		require.NotNil(t, edit)
		updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.spx"])
		assert.Equal(t, "const base BackdropName = \"Scene\"\nconst sound = \"Shared\" + \"Suffix\"\nsetBackdrop base\nplay sound\n", updated)
		updatedServer := newSpxTestServer(t, map[string][]byte{
			"main.spx":                              []byte(updated),
			"assets/index.json":                     []byte(`{"backdrops":[{"name":"Scene"}]}`),
			"assets/sounds/SharedSuffix/index.json": []byte(`{}`),
		})
		requireNoDiagnostics(t, updatedServer)
	})

	t.Run("SharedTypedConstants", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			declaration string
			backdrop    string
			sound       string
			want        string
		}{
			{"PrimaryResource", "const name BackdropName = \"Shared\"\n", "Scene", "Shared", "const name BackdropName = \"Scene\"\nsetBackdrop name\nplay \"Shared\"\n"},
			{"OtherResource", "const name BackdropName = \"Shared\"\n", "Shared", "Sound", "const name BackdropName = \"Shared\"\nsetBackdrop name\nplay \"Sound\"\n"},
			{"DifferentValues", "const name BackdropName = \"Shared\"\n", "Scene", "Sound", "const name BackdropName = \"Scene\"\nsetBackdrop name\nplay \"Sound\"\n"},
			{"SameValue", "const name BackdropName = \"Shared\"\n", "Scene", "Scene", "const name BackdropName = \"Scene\"\nsetBackdrop name\nplay name\n"},
			{"ConstantDependency", "const base BackdropName = \"Shared\"\nconst name = base\n", "Scene", "Sound", "const base BackdropName = \"Scene\"\nconst name = base\nsetBackdrop name\nplay \"Sound\"\n"},
			{"RepeatedInitializer", "const (\nbase BackdropName = \"Shared\"\nname\n)\n", "Scene", "Sound", "const (\nbase BackdropName = \"Scene\"\nname\n)\nsetBackdrop name\nplay \"Sound\"\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := tt.declaration + "setBackdrop name\nplay name\n"
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx":                        []byte(source),
					"assets/index.json":               []byte(`{"backdrops":[{"name":"Shared"}]}`),
					"assets/sounds/Shared/index.json": []byte(`{}`),
				})
				requireNoDiagnostics(t, s)
				var params []XGoRenameResourceParams
				if tt.backdrop != "Shared" {
					params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: "spx://resources/backdrops/Shared"}, NewName: tt.backdrop})
				}
				if tt.sound != "Shared" {
					params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Shared"}, NewName: tt.sound})
				}
				for range 2 {
					edit, err := s.renameResources(params)
					require.NoError(t, err)
					require.NotNil(t, edit)
					updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.spx"])
					assert.Equal(t, tt.want, updated)
					updatedServer := newSpxTestServer(t, map[string][]byte{
						"main.spx":          []byte(updated),
						"assets/index.json": []byte(`{"backdrops":[{"name":"` + tt.backdrop + `"}]}`),
						"assets/sounds/" + tt.sound + "/index.json": []byte(`{}`),
					})
					requireNoDiagnostics(t, updatedServer)
					slices.Reverse(params)
				}
			})
		}
	})

	t.Run("SharedConstants", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			literal  string
			backdrop string
			sound    string
		}{
			{"SameReplacement", `"Shared"`, "Renamed", "Renamed"},
			{"DifferentReplacements", `"Shared"`, "Scene", "Sound"},
			{"RawString", "`Shared`", "Scene", "Sound`"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := "const name = " + tt.literal + "\nsetBackdrop name\nplay name\n"
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx":                        []byte(source),
					"assets/index.json":               []byte(`{"backdrops":[{"name":"Shared"}]}`),
					"assets/sounds/Shared/index.json": []byte(`{}`),
				})
				requireNoDiagnostics(t, s)
				for _, batch := range []bool{false, true} {
					params := []XGoRenameResourceParams{{
						Resource: XGoResourceIdentifier{URI: "spx://resources/backdrops/Shared"}, NewName: tt.backdrop,
					}}
					want := "const name = " + tt.literal + "\nsetBackdrop \"" + tt.backdrop + "\"\nplay name\n"
					sound := "Shared"
					if batch {
						params = append(params, XGoRenameResourceParams{
							Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Shared"}, NewName: tt.sound,
						})
						want = strings.Replace(want, "play name", "play \""+tt.sound+"\"", 1)
						sound = tt.sound
						if tt.backdrop == tt.sound {
							want = strings.Replace(source, "Shared", tt.backdrop, 1)
						}
					}
					edit, err := s.renameResources(params)
					require.NoError(t, err)
					require.NotNil(t, edit)
					require.Len(t, edit.Changes, 1)
					updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.spx"])
					assert.Equal(t, want, updated)
					updatedServer := newSpxTestServer(t, map[string][]byte{
						"main.spx":                               []byte(updated),
						"assets/index.json":                      []byte(`{"backdrops":[{"name":"` + tt.backdrop + `"}]}`),
						"assets/sounds/" + sound + "/index.json": []byte(`{}`),
					})
					requireNoDiagnostics(t, updatedServer)
				}
			})
		}
	})

	t.Run("ConflictingRequests", func(t *testing.T) {
		for _, source := range []string{"", "play \"Shared\"\n"} {
			s := newSpxTestServer(t, map[string][]byte{
				"main.spx":                        []byte(source),
				"assets/index.json":               []byte(`{}`),
				"assets/sounds/Shared/index.json": []byte(`{}`),
			})
			requireNoDiagnostics(t, s)
			edit, err := s.renameResources([]XGoRenameResourceParams{
				{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Shared"}, NewName: "First"},
				{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/%53hared"}, NewName: "Second"},
			})
			assert.ErrorContains(t, err, "conflicting renames")
			assert.Nil(t, edit, "a conflicting batch must not return partial edits")
		}
	})

	t.Run("BatchTargets", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			first    resourceID
			second   resourceID
			conflict bool
		}{
			{"SameKind", SpxSoundResourceID{"First"}, SpxSoundResourceID{"Second"}, true},
			{"DifferentKinds", SpxBackdropResourceID{"First"}, SpxSoundResourceID{"Second"}, false},
			{"SameSprite", SpxSpriteCostumeResourceID{"Runner", "idle"}, SpxSpriteCostumeResourceID{"Runner", "walk"}, true},
			{"DifferentSprites", SpxSpriteCostumeResourceID{"Runner", "idle"}, SpxSpriteCostumeResourceID{"Other", "idle"}, false},
			{"DifferentSpriteResourceKinds", SpxSpriteCostumeResourceID{"Runner", "idle"}, SpxSpriteAnimationResourceID{"Runner", "idle"}, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"assets/index.json":                []byte(`{"backdrops":[{"name":"First"}]}`),
					"assets/sounds/First/index.json":   []byte(`{}`),
					"assets/sounds/Second/index.json":  []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle"},{"name":"walk"}],"fAnimations":{"idle":{}}}`),
					"assets/sprites/Other/index.json":  []byte(`{"costumes":[{"name":"idle"}]}`),
				})
				proj := s.getProj()
				set, err := NewSpxResourceSet(proj)
				require.NoError(t, err)
				result := newSpxAnalysis(proj)
				result.spxResourceSet = *set
				edit, err := s.renameSpxResources(result, []XGoRenameResourceParams{
					{Resource: XGoResourceIdentifier{URI: tt.first.URI()}, NewName: "Renamed"},
					{Resource: XGoResourceIdentifier{URI: tt.second.URI()}, NewName: "Renamed"},
				})
				if tt.conflict {
					assert.ErrorContains(t, err, "conflicting rename target")
					assert.Nil(t, edit)
				} else {
					require.NoError(t, err)
					require.NotNil(t, edit)
					assert.Empty(t, edit.Changes)
				}
			})
		}
	})

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
				result := newSpxAnalysis(proj)
				result.spxResourceSet = *set
				edit, err := s.renameSpxResources(result, []XGoRenameResourceParams{{
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
		call := resourceTestCall(t, proj, "main.xgo")
		require.Len(t, call.Args, 6)
		set, err := NewSpxResourceSet(proj)
		require.NoError(t, err)
		result := newSpxAnalysis(proj)
		result.spxResourceSet = *set
		var params []XGoRenameResourceParams
		// Names in other resource kinds or other sprites do not conflict.
		for i, rename := range []struct {
			id      resourceID
			newName string
		}{
			{SpxBackdropResourceID{BackdropName: "Studio"}, "Beep"},
			{SpxSoundResourceID{SoundName: "Beep"}, "Runner"},
			{SpxSpriteResourceID{SpriteName: "Runner"}, "Score"},
			{SpxSpriteCostumeResourceID{SpriteName: "Runner", CostumeName: "idle"}, "walk"},
			{SpxSpriteAnimationResourceID{SpriteName: "Runner", AnimationName: "walk"}, "idle"},
			{SpxWidgetResourceID{WidgetName: "Score"}, "Studio"},
		} {
			result.addResourceRef(resourceRef{ID: rename.id, Kind: XGoResourceRefKindStringLiteral, Node: call.Args[i]})
			params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: rename.id.URI()}, NewName: rename.newName})
		}
		params = append(params, params[0])
		edit, err := s.renameSpxResources(result, params)
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
		edit, err = s.renameSpxResources(result, params)
		assert.ErrorContains(t, err, `sprite resource "Missing" not found`)
		assert.Nil(t, edit, "a failed batch must not return partial edits")
	})

	t.Run("Empty", func(t *testing.T) {
		s := newTestServer(t, nil)
		edit, err := s.renameSpxResources(newSpxAnalysis(s.getProj()), nil)
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{})
	})

	t.Run("InvalidURI", func(t *testing.T) {
		s := newTestServer(t, nil)
		edit, err := s.renameSpxResources(newSpxAnalysis(s.getProj()), []XGoRenameResourceParams{{
			Resource: XGoResourceIdentifier{URI: "file:///assets/Studio"}, NewName: "Park",
		}})
		assert.EqualError(t, err, "failed to parse spx resource URI: invalid spx resource URI: file:///assets/Studio")
		assert.Nil(t, edit)
	})
}
