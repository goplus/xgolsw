//go:build !test_no_pkgdata

package server

import (
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerInspectSpxResourceRefsForCallExpr(t *testing.T) {
	for _, tt := range []struct {
		name            string
		source          string
		externalPackage bool
	}{
		{
			name:            "ExternalPackage",
			source:          "import \"example.com/audio\"\naudio.play \"Known\", \"Missing\"\n",
			externalPackage: true,
		},
		{
			name: "ParenthesizedSlice",
			source: `func use(names []SoundName) {}
use ((["Known", ("Missing")]))
`,
		},
		{
			name: "VariadicSlice",
			source: `func use(names ...SoundName) {}
use ["Known", "Missing"]...
`,
		},
		{
			name: "ParenthesizedVariadicSlice",
			source: `func use(names ...SoundName) {}
use ((["Known", "Missing"]))...
`,
		},
		{
			name: "Kwarg",
			source: `type Options struct { Names []SoundName }
func use(opts Options?) {}
use names = (["Known", "Missing"])
`,
		},
		{
			name: "OverloadKwarg",
			source: `type Worker struct{}
type Options struct { Names []SoundName }
var worker Worker
func (w *Worker) useNames(opts Options?) {}
func (Worker).use = (
    (Worker).useNames
)
worker.use names = (["Known", "Missing"])
`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newSpxTestServer(t, map[string][]byte{
				"main.spx":                       []byte(tt.source),
				"assets/index.json":              []byte(`{}`),
				"assets/sounds/Known/index.json": []byte(`{}`),
			})
			if tt.externalPackage {
				proj := s.getProj()
				fallback := proj.Importer
				pkg := gotypes.NewPackage("example.com/audio", "audio")
				params := gotypes.NewTuple(
					gotypes.NewParam(token.NoPos, pkg, "first", GetSpxSoundNameType()),
					gotypes.NewParam(token.NoPos, pkg, "second", GetSpxSoundNameType()),
				)
				pkg.Scope().Insert(gotypes.NewFunc(token.NoPos, pkg, "Play", gotypes.NewSignatureType(nil, nil, nil, params, nil, false)))
				pkg.MarkComplete()
				proj.Importer = inputSlotTestImporter(func(path string) (*gotypes.Package, error) {
					if path == pkg.Path() {
						return pkg, nil
					}
					return fallback.Import(path)
				})
			}
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			result, err := s.compile()
			require.NoError(t, err)
			require.Len(t, result.spxResourceRefs, 2)
			var wantLinks []DocumentLink
			var wantDiagnostics []Diagnostic
			for i, name := range []string{"Known", "Missing"} {
				ref := result.spxResourceRefs[i]
				assert.Equal(t, SpxSoundResourceID{name}, ref.ID)
				assert.Equal(t, SpxResourceRefKindStringLiteral, ref.Kind)
				needle := `"` + name + `"`
				offset := strings.Index(tt.source, needle)
				require.NotEqual(t, -1, offset)
				line := uint32(strings.Count(tt.source[:offset], "\n"))
				column := uint32(offset - strings.LastIndex(tt.source[:offset], "\n") - 1)
				span := Range{Start: Position{Line: line, Character: column}, End: Position{Line: line, Character: column + uint32(len(needle))}}
				if name == "Known" {
					wantLinks = append(wantLinks, DocumentLink{
						Range:  span,
						Target: toURI("spx://resources/sounds/Known"),
						Data:   SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral},
					})
				} else {
					wantDiagnostics = append(wantDiagnostics, Diagnostic{
						Range: span, Severity: SeverityError, Message: `sound resource "Missing" not found`,
					})
				}
			}
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}})
			require.NoError(t, err)
			var resourceLinks []DocumentLink
			for _, link := range links {
				if link.Target != nil && strings.HasPrefix(string(*link.Target), "spx://") {
					resourceLinks = append(resourceLinks, link)
				}
			}
			assert.Equal(t, wantLinks, resourceLinks)
			report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
			require.NoError(t, err)
			require.Len(t, report.Items, 1)
			full := requireWorkspaceFullDocumentDiagnosticReport(t, report.Items[0])
			assert.Equal(t, DocumentURI("file:///main.spx"), full.URI)
			assert.Equal(t, wantDiagnostics, full.Items)
		})
	}

	t.Run("UnknownKwarg", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx": []byte(`type Options struct { Target SpriteName }
func configure(opts Options?) {}
configure target = "OtherSprite", unknown = 9
`),
			"OtherSprite.spx":                       nil,
			"assets/index.json":                     []byte(`{}`),
			"assets/sprites/OtherSprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		result, err := s.compile()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.spxResourceRefs, 1)
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/OtherSprite"), result.spxResourceRefs[0].ID.URI())
	})
}

func TestCompileResultIsInSpxEventHandler(t *testing.T) {
	for _, tt := range []struct {
		name     string
		content  string
		position Position
		want     bool
	}{
		{
			name:     "OrdinaryCallback",
			content:  "func invoke(fn func()) { fn() }\ninvoke => {\n    println 1\n}\n",
			position: Position{Line: 2, Character: 8},
		},
		{
			name:     "NestedOrdinaryCallback",
			content:  "func invoke(fn func()) { fn() }\nonStart => {\n    invoke => {\n        println 1\n    }\n}\n",
			position: Position{Line: 3, Character: 12},
			want:     true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{"main.spx": []byte(tt.content), "assets/index.json": []byte(`{}`)}
			s := newSpxTestServer(t, files)
			result, err := s.compile()
			require.NoError(t, err)
			require.Empty(t, result.diagnostics["file:///main.spx"])
			astFile, err := result.proj.ASTFile("main.spx")
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.isInSpxEventHandler(PosAt(result.proj, astFile, tt.position)))
		})
	}
}

func TestServerInspectForSpxResourceRefs(t *testing.T) {
	for _, resource := range []struct {
		name string
		id   SpxResourceID
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
				kind   SpxResourceRefKind
			}{
				{"Declaration", `func run() { var value ResourceName = "Known"; println value }`, `"Known"`, SpxResourceRefKindStringLiteral},
				{"Assignment", `func run() { var value ResourceName; value = "Known"; println value }`, `"Known"`, SpxResourceRefKindStringLiteral},
				{"ConstantReturn", `const Value = "Known"
func current() ResourceName { return Value }`, "Value", SpxResourceRefKindConstantReference},
				{"ParenthesizedReturn", `func current() ResourceName { return (("Known")) }`, `"Known"`, SpxResourceRefKindStringLiteral},
				{"NestedReturn", `func current() (string, ResourceName) {
	func() string { return "ordinary" }()
	return "ordinary", func() ResourceName { return "Known" }()
}`, `"Known"`, SpxResourceRefKindStringLiteral},
				{"CallInReturn", `func pick(ordinary string) ResourceName { return "Known" }
func current() ResourceName { return pick("argument") }`, `"Known"`, SpxResourceRefKindStringLiteral},
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
					result, err := s.compile()
					require.NoError(t, err)
					require.Len(t, result.spxResourceRefs, 1)
					ref := result.spxResourceRefs[0]
					assert.Equal(t, resource.id, ref.ID)
					assert.Equal(t, form.kind, ref.Kind)
					for _, diagnostics := range result.diagnostics {
						assert.Empty(t, diagnostics)
					}
					offset := strings.LastIndex(source, form.needle)
					require.NotEqual(t, -1, offset)
					line := uint32(strings.Count(source[:offset], "\n"))
					column := uint32(offset - strings.LastIndex(source[:offset], "\n") - 1)
					want := DocumentLink{
						Range:  Range{Start: Position{Line: line, Character: column}, End: Position{Line: line, Character: column + uint32(len(form.needle))}},
						Target: toURI(string(resource.id.URI())),
						Data:   SpxResourceRefDocumentLinkData{Kind: form.kind},
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
				result, err := s.compile()
				require.NoError(t, err)
				require.Len(t, result.spxResourceRefs, 2)
				got := make(map[SpxResourceID]SpxResourceRefKind)
				for _, ref := range result.spxResourceRefs {
					got[ref.ID] = ref.Kind
				}
				assert.Equal(t, map[SpxResourceID]SpxResourceRefKind{
					SpxSoundResourceID{"Known"}:    SpxResourceRefKindStringLiteral,
					SpxBackdropResourceID{"Known"}: SpxResourceRefKindConstantReference,
				}, got)
				assert.Empty(t, result.diagnostics["file:///main.spx"])
			})
		}
	})

	t.Run("SpriteContext", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			want   []SpxResourceID
		}{
			{
				name:   "TypedConstantWithExplicitReceiver",
				source: "const Costume SpriteCostumeName = \"idle\"\nfunc run() { Other.setCostume Costume }\n",
				want:   []SpxResourceID{SpxSpriteResourceID{"Other"}, SpxSpriteCostumeResourceID{"Runner", "idle"}, SpxSpriteCostumeResourceID{"Other", "idle"}},
			},
			{
				name:   "UntypedConstantWithExplicitReceiver",
				source: "const Costume = \"idle\"\nfunc run() { Other.setCostume Costume }\n",
				want:   []SpxResourceID{SpxSpriteResourceID{"Other"}, SpxSpriteCostumeResourceID{"Other", "idle"}},
			},
			{
				name:   "UnboundReceiver",
				source: "func run(other *Other) { other.setCostume \"idle\" }\n",
			},
			{
				name:   "OrdinaryCallbackReturn",
				source: "func invoke(fn func() string) {}\nfunc current() SpriteCostumeName {\n\tinvoke => { return \"ordinary\" }\n\treturn \"idle\"\n}\n",
				want:   []SpxResourceID{SpxSpriteCostumeResourceID{"Runner", "idle"}},
			},
			{
				name:   "ComputedReturn",
				source: "func current() SpriteCostumeName { return \"id\" + \"le\" }\n",
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
				result, err := s.compile()
				require.NoError(t, err)
				var got []SpxResourceID
				for _, ref := range result.spxResourceRefs {
					got = append(got, ref.ID)
				}
				assert.ElementsMatch(t, tt.want, got)
				for _, diagnostics := range result.diagnostics {
					assert.Empty(t, diagnostics)
				}
			})
		}
	})

	t.Run("ValueWithoutSourcePosition", func(t *testing.T) {
		for _, resource := range []struct {
			name string
			id   SpxResourceID
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
						result := newCompileResult(proj, s.lookupPkgDoc)
						result.mainSpxFile = "main.spx"
						result.spxResourceSet = *set
						require.NotPanics(t, func() {
							s.inspectForSpxResourceRefs(result)
						})
						require.Len(t, result.spxResourceRefs, 1)
						ref := result.spxResourceRefs[0]
						assert.Equal(t, resource.id, ref.ID)
						assert.Equal(t, SpxResourceRefKindStringLiteral, ref.Kind)
						assert.Equal(t, `"Known"`, requireValueAs[*ast.BasicLit](t, ref.Node).Value)
						assert.Empty(t, result.diagnostics)
					})
				}
			})
		}
	})

	t.Run("ReturnContextWithLineDirectives", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			want     []SpxResourceID
		}{
			{"Project", "main.spx", nil},
			{"Sprite", "Runner.spx", []SpxResourceID{SpxSpriteCostumeResourceID{"Runner", "runner"}, SpxSpriteAnimationResourceID{"Runner", "walk"}}},
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
				result, err := s.compile()
				require.NoError(t, err)
				var ids []SpxResourceID
				for _, ref := range result.spxResourceRefs {
					ids = append(ids, ref.ID)
				}
				assert.ElementsMatch(t, tt.want, ids)
			})
		}
	})
}
