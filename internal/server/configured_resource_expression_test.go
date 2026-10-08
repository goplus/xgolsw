package server

import (
	"strings"
	"testing"

	"github.com/goplus/xgolsw/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfiguredResourceExpressions(t *testing.T) {
	const configuration = `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Asset","contextURI":"demo://assets"}]}`
	const declarations = "type Asset string\nfunc use(value Asset) {}\n"
	for _, tt := range []struct {
		name, source, want string
		kind               XGoResourceRefKind
	}{
		{"ConstantConversion", "const name = \"Intro\"\nuse Asset(name)\necho name\n", "const name = \"Intro\"\nuse Asset(\"Next\")\necho name\n", XGoResourceRefKindConstantReference},
		{"NestedConstantConversion", "const name = \"Intro\"\nuse Asset(string(name))\necho name\n", "const name = \"Intro\"\nuse Asset(string(\"Next\"))\necho name\n", XGoResourceRefKindConstantReference},
		{"IntrinsicConstantConversion", "const name = \"Intro\"\necho Asset(name)\n", "const name = \"Next\"\necho Asset(name)\n", XGoResourceRefKindConstantReference},
		{"Concatenation", "use \"In\" + \"tro\"\n", "use \"Next\"\n", XGoResourceRefKindStringExpression},
		{"ConstantConcatenation", "const prefix = \"In\"\nuse prefix + \"tro\"\necho prefix\n", "const prefix = \"In\"\nuse \"Next\"\necho prefix\n", XGoResourceRefKindStringExpression},
		{"ConvertedConcatenation", "use Asset(\"In\" + \"tro\")\n", "use Asset(\"Next\")\n", XGoResourceRefKindStringExpression},
		{"RawConcatenation", "use `In\r` + `tro\r`\n", "use \"Next\"\n", XGoResourceRefKindStringExpression},
		{"ConvertedRawConcatenation", "use Asset(`In\r` + `tro\r`)\n", "use Asset(\"Next\")\n", XGoResourceRefKindStringExpression},
		{"EscapedConcatenation", "use \"In$$\" + \"tro\"\n", "use \"Next\"\n", XGoResourceRefKindStringExpression},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, filename := range []string{"main.xgo", "main_fixture.gox"} {
				form := "Plain"
				if filename == "main_fixture.gox" {
					form = "Classfile"
				}
				t.Run(form, func(t *testing.T) {
					name := "Intro"
					if tt.name == "EscapedConcatenation" {
						name = "In$tro"
					}
					files := map[string][]byte{
						filename: []byte(tt.source), "types.xgo": []byte(declarations),
						"resources.json": []byte(`{"demo://assets":["Intro","In$tro"]}`),
					}
					s := newConfiguredResourceTestServer(t, files, configuration)
					proj := s.requestProject()
					_, err := proj.TypeInfo()
					require.NoError(t, err)
					links, err := documentLinksForResources(proj, filename)
					require.NoError(t, err)
					require.Len(t, links, 1)
					assert.Equal(t, ToPtr(URI(configuredResourceID{"demo://assets", name}.URI())), links[0].Target)
					assert.Equal(t, XGoResourceRefDocumentLinkData{Kind: tt.kind}, links[0].Data)
					source := tt.source
					for _, next := range []string{"Next", "Final"} {
						edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: configuredResourceID{"demo://assets", name}.URI()}, NewName: next}})
						require.NoError(t, err)
						require.Len(t, edit.Changes, 1)
						source = applyResourceRenameTestEdits(t, source, edit.Changes[s.toDocumentURI(filename)])
						assert.Equal(t, strings.ReplaceAll(tt.want, "Next", next), source)
						files[filename] = []byte(source)
						files["resources.json"] = []byte(`{"demo://assets":["` + next + `"]}`)
						s = newConfiguredResourceTestServer(t, files, configuration)
						_, err = s.requestProject().TypeInfo()
						require.NoError(t, err)
						name = next
					}
				})
			}
		})
	}
}

func TestServerConfiguredResourceImportedConstant(t *testing.T) {
	const source = "import clips \"example.com/support/v2\"\nimport scenes \"example.com/alternate\"\nclips.Accept clips.Default\nscenes.Accept scenes.Name(clips.Default)\n"
	files := map[string][]byte{
		"main.xgo":       []byte(source),
		"resources.json": []byte(`{"demo://resources/clips":["default"],"demo://resources/scenes":["default"]}`),
	}
	s := newConfiguredResourceTestServer(t, files, resourceTestConfig)
	proj := s.requestProject()
	_, err := proj.TypeInfo()
	require.NoError(t, err)
	links, err := documentLinksForResources(proj, "main.xgo")
	require.NoError(t, err)
	require.Len(t, links, 2)
	assert.ElementsMatch(t, []string{"demo://resources/clips/default", "demo://resources/scenes/default"}, documentLinkTargets(t, links))
	for _, link := range links {
		assert.Equal(t, XGoResourceRefDocumentLinkData{Kind: XGoResourceRefKindConstantReference}, link.Data)
		assert.Equal(t, "clips.Default", source[PositionOffset([]byte(source), link.Range.Start):PositionOffset([]byte(source), link.Range.End)])
	}
	edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://resources/clips/default"}, NewName: "Next"}})
	require.NoError(t, err)
	require.Len(t, edit.Changes, 1)
	updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
	assert.Equal(t, strings.Replace(source, "clips.Accept clips.Default", `clips.Accept clips.Name("Next")`, 1), updated)
	files["main.xgo"] = []byte(updated)
	files["resources.json"] = []byte(`{"demo://resources/clips":["Next"],"demo://resources/scenes":["default"]}`)
	s = newConfiguredResourceTestServer(t, files, resourceTestConfig)
	proj = s.requestProject()
	_, err = proj.TypeInfo()
	require.NoError(t, err)
	links, err = documentLinksForResources(proj, "main.xgo")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"demo://resources/clips/Next", "demo://resources/scenes/default"}, documentLinkTargets(t, links))
}

func TestServerConfiguredResourcePredefinedContexts(t *testing.T) {
	for _, tt := range []struct {
		name, sound, clip string
	}{
		{"Direct", "name", "name"},
		{"Conversion", "string(name)", "name"},
		{"ImportedAliasConversion", "sdk.SoundName(name)", "sdk.SoundName(name)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := newSpxTestServer(t, map[string][]byte{
				"main.spx":                       []byte("import sdk \"github.com/goplus/spx/v3\"\ntype Clip = string\nvar name Clip = \"Intro\"\nfunc Use(value Clip) {}\nplay " + tt.sound + "\nuse " + tt.clip + "\n"),
				"assets/index.json":              []byte(`{}`),
				"assets/sounds/Intro/index.json": []byte(`{}`),
				"resources.json":                 []byte(`{"demo://clips":["Intro"]}`),
			})
			configuration, err := config.ParseResourceConfig([]byte(`{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Clip","contextURI":"demo://clips"}]}`))
			require.NoError(t, err)
			s := New(base.getProj(), newMockReplier(), base.fileMapGetter, &MockScheduler{}, base.listPkgs, base.lookupPkgDoc, configuration)
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
			require.NoError(t, err)
			contexts := make(map[uint32]XGoResourceContextURI)
			for _, slot := range slots {
				if slot.Input.Name == "name" && slot.Accept.ResourceContext != nil {
					contexts[slot.Range.Start.Line] = *slot.Accept.ResourceContext
				}
			}
			assert.Equal(t, map[uint32]XGoResourceContextURI{4: SpxSoundResourceContextURI, 5: "demo://clips"}, contexts)
		})
	}
}

func TestServerConfiguredResourceConstantContexts(t *testing.T) {
	const source = "const name Clip = \"Intro\"\nplay string(name)\n"
	s := newMixedResourceTestServer(t, source)
	proj := s.requestProject()
	_, err := proj.TypeInfo()
	require.NoError(t, err)
	links, err := documentLinksForResources(proj, "main.spx")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"demo://clips/Intro", "spx://resources/sounds/Intro"}, documentLinkTargets(t, links))
	edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://clips/Intro"}, NewName: "Next"}})
	require.NoError(t, err)
	file, ok := proj.File("main.spx")
	require.True(t, ok)
	updated := applyResourceRenameTestEdits(t, string(file.Content), edit.Changes["file:///main.spx"])
	assert.True(t, strings.HasSuffix(updated, "const name Clip = \"Next\"\nplay string(Clip(\"Intro\"))\n"), updated)
	body := strings.SplitN(updated, "func Use(name Clip) {}\n", 2)
	require.Len(t, body, 2)
	updatedServer := newMixedResourceTestServer(t, body[1])
	_, err = updatedServer.requestProject().TypeInfo()
	require.NoError(t, err)
	links, err = documentLinksForResources(updatedServer.requestProject(), "main.spx")
	require.NoError(t, err)
	assert.Contains(t, documentLinkTargets(t, links), "spx://resources/sounds/Intro")
	assert.NotContains(t, documentLinkTargets(t, links), "demo://clips/Intro")
}

func TestServerConfiguredResourceExpressionDependencies(t *testing.T) {
	const source = "type Asset string\nconst name Asset = \"Intro\"\nfunc use(value Asset) {}\nuse name + \"Tail\"\n"
	const configuration = `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Asset","contextURI":"demo://assets"}]}`
	for _, tt := range []struct {
		name             string
		renameExpression bool
		want             string
	}{
		{"PreserveExpression", false, "use Asset(\"Intro\") + \"Tail\"\n"},
		{"RenameExpression", true, "use \"NextTail\"\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{
				"main.xgo":       []byte(source),
				"resources.json": []byte(`{"demo://assets":["Intro","IntroTail"]}`),
			}
			s := newConfiguredResourceTestServer(t, files, configuration)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			params := []XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "demo://assets/Intro"}, NewName: "Next"}}
			if tt.renameExpression {
				params = append(params, XGoRenameResourceParams{Resource: XGoResourceIdentifier{URI: "demo://assets/IntroTail"}, NewName: "NextTail"})
			}
			edit, err := s.renameResources(params)
			require.NoError(t, err)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
			assert.Equal(t, "type Asset string\nconst name Asset = \"Next\"\nfunc use(value Asset) {}\n"+tt.want, updated)
			files["main.xgo"] = []byte(updated)
			files["resources.json"] = []byte(`{"demo://assets":["Next","IntroTail","NextTail"]}`)
			s = newConfiguredResourceTestServer(t, files, configuration)
			proj := s.requestProject()
			_, err = proj.TypeInfo()
			require.NoError(t, err)
			links, err := documentLinksForResources(proj, "main.xgo")
			require.NoError(t, err)
			want := "demo://assets/IntroTail"
			if tt.renameExpression {
				want = "demo://assets/NextTail"
			}
			assert.ElementsMatch(t, []string{"demo://assets/Next", want}, documentLinkTargets(t, links))
		})
	}
}

func TestServerConfiguredResourceExpressionFragments(t *testing.T) {
	for _, tt := range []struct {
		name, expression string
		want             []string
	}{
		{"NumericConversion", `Asset(65)`, nil},
		{"NestedNumericConversion", `Asset(string(65))`, nil},
		{"RuntimeCall", `Asset(value())`, nil},
		{"RuntimeConcatenation", `Asset("In" + value())`, nil},
		{"DirectInterpolation", `Asset("${value()}")`, nil},
		{"Interpolation", `Asset("${value()}" + "tro")`, nil},
		{"LiteralFragments", `"In" + "tro"`, []string{"demo://assets/Intro"}},
		{"ConvertedFragments", `Asset("In") + "tro"`, []string{"demo://assets/Intro"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newConfiguredResourceTestServer(t, map[string][]byte{
				"main.xgo":       []byte("type Asset string\nfunc use(value Asset) {}\nfunc value() string { return \"In\" }\nuse " + tt.expression + "\n"),
				"resources.json": []byte(`{"demo://assets":["Intro","In","tro","A"]}`),
			}, `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Asset","contextURI":"demo://assets"}]}`)
			proj := s.requestProject()
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			links, err := documentLinksForResources(proj, "main.xgo")
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, documentLinkTargets(t, links))
			if tt.name == "DirectInterpolation" {
				slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
				require.NoError(t, err)
				for _, slot := range slots {
					if slot.Range.Start.Line == 3 {
						assert.NotEqual(t, XGoInputKindInPlace, slot.Input.Kind)
					}
				}
			}
			if strings.HasSuffix(tt.name, "Fragments") {
				position := Position{Line: 3, Character: 6}
				if tt.name == "ConvertedFragments" {
					position.Character += 6
				}
				labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", position))
				assert.NotContains(t, labels, "Intro")
				slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
				require.NoError(t, err)
				for _, slot := range slots {
					assert.NotEqual(t, XGoInputTypeResourceName, slot.Accept.Type)
				}
			}
		})
	}
}
