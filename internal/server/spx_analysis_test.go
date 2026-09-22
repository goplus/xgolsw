package server

import (
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectForSpxResourceRefs(t *testing.T) {
	for _, resource := range []struct {
		name string
		id   resourceID
	}{
		{"BackdropName", SpxBackdropResourceID{"Known"}},
		{"SoundName", SpxSoundResourceID{"Known"}},
		{"SpriteName", SpxSpriteResourceID{"Known"}},
		{"SpriteCostumeName", SpxSpriteCostumeResourceID{"Runner", "Known"}},
		{"SpriteAnimationName", SpxSpriteAnimationResourceID{"Runner", "Known"}},
		{"WidgetName", SpxWidgetResourceID{"Known"}},
	} {
		t.Run(resource.name, func(t *testing.T) {
			for _, form := range []struct {
				name   string
				source string
				needle string
				kind   XGoResourceRefKind
			}{
				{"Declaration", `func run() { var value ResourceName = "Known"; println value }`, `"Known"`, XGoResourceRefKindStringLiteral},
				{"Assignment", `func run() { var value ResourceName; value = "Known"; println value }`, `"Known"`, XGoResourceRefKindStringLiteral},
				{"ConstantReturn", `const Value = "Known"
func current() ResourceName { return Value }`, "Value", XGoResourceRefKindConstantReference},
				{"ParenthesizedReturn", `func current() ResourceName { return (("Known")) }`, `"Known"`, XGoResourceRefKindStringLiteral},
				{"NestedReturn", `func current() (string, ResourceName) {
	func() string { return "ordinary" }()
	return "ordinary", func() ResourceName { return "Known" }()
}`, `"Known"`, XGoResourceRefKindStringLiteral},
				{"CallInReturn", `func pick(ordinary string) ResourceName { return "Known" }
func current() ResourceName { return pick("argument") }`, `"Known"`, XGoResourceRefKindStringLiteral},
			} {
				t.Run(form.name, func(t *testing.T) {
					source := "type ResourceName = " + resource.name + "\n" + form.source + "\n"
					s := newSpxTestServer(t, map[string][]byte{
						"main.spx":                         nil,
						"Runner.spx":                       []byte(source),
						"assets/index.json":                []byte(`{"backdrops":[{"name":"Known"}],"zorder":[{"name":"Known"}]}`),
						"assets/sounds/Known/index.json":   []byte(`{}`),
						"assets/sprites/Known/index.json":  []byte(`{}`),
						"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"Known"}],"fAnimations":{"Known":{}}}`),
					})
					_, err := s.getProj().TypeInfo()
					require.NoError(t, err)
					result, err := analyzeSpx(s.syncProject())
					require.NoError(t, err)
					require.Len(t, result.resourceRefs, 1)
					ref := result.resourceRefs[0]
					assert.Equal(t, resource.id, ref.ID)
					assert.Equal(t, form.kind, ref.Kind)
					for _, diagnostics := range resourceDiagnostics(s, result.resourceAnalysis).diagnostics {
						assert.Empty(t, diagnostics)
					}
					offset := strings.LastIndex(source, form.needle)
					require.NotEqual(t, -1, offset)
					line := uint32(strings.Count(source[:offset], "\n"))
					column := uint32(offset - strings.LastIndex(source[:offset], "\n") - 1)
					want := DocumentLink{
						Range:  Range{Start: Position{Line: line, Character: column}, End: Position{Line: line, Character: column + uint32(len(form.needle))}},
						Target: toURI(string(resource.id.URI())),
						Data:   XGoResourceRefDocumentLinkData{Kind: form.kind},
					}
					links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///Runner.spx"}})
					require.NoError(t, err)
					var resourceLinks []DocumentLink
					for _, link := range links {
						if link.Target != nil && strings.HasPrefix(string(*link.Target), "spx://") {
							resourceLinks = append(resourceLinks, link)
						}
					}
					assert.Equal(t, []DocumentLink{want}, resourceLinks)
				})
			}
		})
	}

	t.Run("ContextOverridesConstantType", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
		}{
			{"Declaration", "var backdrop BackdropName = Resource\n"},
			{"Assignment", "func run() { var backdrop BackdropName; backdrop = Resource; println backdrop }\n"},
			{"Return", "func current() BackdropName { return Resource }\n"},
			{"Call", "onBackdrop (Resource), func() {}\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx":                       []byte("const Resource SoundName = \"Known\"\n" + tt.source),
					"assets/index.json":              []byte(`{"backdrops":[{"name":"Known"}]}`),
					"assets/sounds/Known/index.json": []byte(`{}`),
				})
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				result, err := analyzeSpx(s.syncProject())
				require.NoError(t, err)
				require.Len(t, result.resourceRefs, 2)
				got := make(map[resourceID]XGoResourceRefKind)
				for _, ref := range result.resourceRefs {
					got[ref.ID] = ref.Kind
				}
				assert.Equal(t, map[resourceID]XGoResourceRefKind{
					SpxSoundResourceID{"Known"}:    XGoResourceRefKindStringLiteral,
					SpxBackdropResourceID{"Known"}: XGoResourceRefKindConstantReference,
				}, got)
				assert.Empty(t, resourceDiagnostics(s, result.resourceAnalysis).diagnostics["file:///main.spx"])
			})
		}
	})

	t.Run("SpriteContext", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			want   []resourceID
		}{
			{
				name:   "ExplicitThis",
				source: "this.setCostume \"idle\"\n",
				want:   []resourceID{SpxSpriteCostumeResourceID{"Runner", "idle"}},
			},
			{
				name:   "ShadowedThis",
				source: "func run(this *Other) { this.setCostume \"idle\" }\n",
			},
			{
				name:   "TypedConstantWithExplicitReceiver",
				source: "const Costume SpriteCostumeName = \"idle\"\nfunc run() { Other.setCostume Costume }\n",
				want:   []resourceID{SpxSpriteResourceID{"Other"}, SpxSpriteCostumeResourceID{"Runner", "idle"}, SpxSpriteCostumeResourceID{"Other", "idle"}},
			},
			{
				name:   "UntypedConstantWithExplicitReceiver",
				source: "const Costume = \"idle\"\nfunc run() { Other.setCostume Costume }\n",
				want:   []resourceID{SpxSpriteResourceID{"Other"}, SpxSpriteCostumeResourceID{"Other", "idle"}},
			},
			{
				name:   "UnboundReceiver",
				source: "func run(other *Other) { other.setCostume \"idle\" }\n",
			},
			{
				name:   "OrdinaryCallbackReturn",
				source: "func invoke(fn func() string) {}\nfunc current() SpriteCostumeName {\n\tinvoke => { return \"ordinary\" }\n\treturn \"idle\"\n}\n",
				want:   []resourceID{SpxSpriteCostumeResourceID{"Runner", "idle"}},
			},
			{
				name:   "ComputedReturn",
				source: "func current() SpriteCostumeName { return \"id\" + \"le\" }\n",
				want:   []resourceID{SpxSpriteCostumeResourceID{"Runner", "idle"}},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx": nil, "Other.spx": nil,
					"Runner.spx":                       []byte(tt.source),
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle"}]}`),
					"assets/sprites/Other/index.json":  []byte(`{"costumes":[{"name":"idle"}]}`),
				})
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				result, err := analyzeSpx(s.syncProject())
				require.NoError(t, err)
				var got []resourceID
				for _, ref := range result.resourceRefs {
					got = append(got, ref.ID)
				}
				assert.ElementsMatch(t, tt.want, got)
				for _, diagnostics := range resourceDiagnostics(s, result.resourceAnalysis).diagnostics {
					assert.Empty(t, diagnostics)
				}
			})
		}
	})

	t.Run("ValueWithoutSourcePosition", func(t *testing.T) {
		for _, resource := range []struct {
			name string
			id   resourceID
		}{
			{"SpriteCostumeName", SpxSpriteCostumeResourceID{"Runner", "Known"}},
			{"SpriteAnimationName", SpxSpriteAnimationResourceID{"Runner", "Known"}},
		} {
			t.Run(resource.name, func(t *testing.T) {
				for _, name := range []string{"NoPos", "UnregisteredPosition"} {
					t.Run(name, func(t *testing.T) {
						s := newSpxTestServer(t, map[string][]byte{
							"main.spx": nil,
							"Runner.spx": []byte("func invalid() " + resource.name + " { return \"invalid\" }\n" +
								"func valid() " + resource.name + " { return \"Known\" }\n"),
							"assets/index.json":                []byte(`{}`),
							"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"Known"}],"fAnimations":{"Known":{}}}`),
						})
						proj := s.getProj()
						_, err := proj.TypeInfo()
						require.NoError(t, err)
						file, err := proj.ASTFile("Runner.spx")
						require.NoError(t, err)
						literal := inputSlotLiteral(t, newInputSlotContext(proj, file), `"invalid"`)
						pos := token.NoPos
						if name == "UnregisteredPosition" {
							pos = token.Pos(proj.Fset.Base())
						}
						require.Nil(t, proj.Fset.File(pos))
						literal.ValuePos = pos
						set, err := NewSpxResourceSet(proj)
						require.NoError(t, err)
						result := newSpxAnalysis(proj)
						result.mainSpxFile = "main.spx"
						result.spxResourceSet = *set
						require.NotPanics(t, func() {
							collectResourceReferences(s.getProj(), []*resourceProvider{result.resourceProvider()})
						})
						require.Len(t, result.resourceRefs, 1)
						ref := result.resourceRefs[0]
						assert.Equal(t, resource.id, ref.ID)
						assert.Equal(t, XGoResourceRefKindStringLiteral, ref.Kind)
						assert.Equal(t, `"Known"`, requireValueAs[*ast.BasicLit](t, ref.Node).Value)
						assert.Empty(t, resourceDiagnostics(s, result.resourceAnalysis).diagnostics)
					})
				}
			})
		}
	})

	t.Run("ReturnContextWithLineDirectives", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			want     []resourceID
		}{
			{"Project", "main.spx", nil},
			{"Sprite", "Runner.spx", []resourceID{SpxSpriteCostumeResourceID{"Runner", "runner"}, SpxSpriteAnimationResourceID{"Runner", "walk"}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx": nil, "Runner.spx": nil,
					"assets/index.json":                 []byte(`{}`),
					"assets/sprites/Runner/index.json":  []byte(`{"costumes":[{"name":"runner"}],"fAnimations":{"walk":{}}}`),
					"assets/sprites/virtual/index.json": []byte(`{"costumes":[{"name":"runner"}],"fAnimations":{"walk":{}}}`),
				}
				files[tt.filename] = []byte("func currentCostume() SpriteCostumeName {\n//line virtual.spx:100:20\n\treturn \"runner\"\n}\nfunc currentAnimation() SpriteAnimationName {\n//line virtual.spx:200:20\n\treturn \"walk\"\n}\n")
				s := newSpxTestServer(t, files)
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				result, err := analyzeSpx(s.syncProject())
				require.NoError(t, err)
				var ids []resourceID
				for _, ref := range result.resourceRefs {
					ids = append(ids, ref.ID)
				}
				assert.ElementsMatch(t, tt.want, ids)
			})
		}
	})
}
