package server

import (
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfiguredResourceCallReferences(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
	}{
		{
			name:   "ExternalPackage",
			source: "import \"example.com/audio\"\naudio.play \"Known\", \"Missing\"\n",
		},
		{
			name: "ParenthesizedSlice",
			source: `func use(names []Asset) {}
use ((["Known", ("Missing")]))
`,
		},
		{
			name: "VariadicSlice",
			source: `func use(names ...Asset) {}
use ["Known", "Missing"]...
`,
		},
		{
			name: "ParenthesizedVariadicSlice",
			source: `func use(names ...Asset) {}
use ((["Known", "Missing"]))...
`,
		},
		{
			name: "Kwarg",
			source: `type Options struct { Names []Asset }
func use(opts Options?) {}
use names = (["Known", "Missing"])
`,
		},
		{
			name: "OverloadKwarg",
			source: `type Worker struct{}
type Options struct { Names []Asset }
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
			s := newConfiguredResourceTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte(tt.source),
				"types.xgo":        []byte("import audio \"example.com/audio\"\ntype Asset = audio.Name\n"),
				"resources.json":   []byte(`{"demo://resources/clips":["Known"]}`),
			}, `{"dataFile":"resources.json","types":[{"pkgPath":"example.com/audio","typeName":"Name","contextURI":"demo://resources/clips"}]}`)
			proj := s.getProj()
			fallback := proj.Importer
			pkg := gotypes.NewPackage("example.com/audio", "audio")
			nameType := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "Name", nil), gotypes.Typ[gotypes.String])
			pkg.Scope().Insert(nameType.Obj())
			params := gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "first", nameType),
				gotypes.NewParam(token.NoPos, pkg, "second", nameType),
			)
			pkg.Scope().Insert(gotypes.NewFunc(token.NoPos, pkg, "Play", gotypes.NewSignatureType(nil, nil, nil, params, nil, false)))
			pkg.MarkComplete()
			proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == pkg.Path() {
					return pkg, nil
				}
				return fallback.Import(path)
			})
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			analysis, err := analyzeFramework(s.requestProject())
			require.NoError(t, err)
			require.NotNil(t, analysis)
			result := analysis.resources
			require.Len(t, result.resourceRefs, 2)
			var wantLinks []DocumentLink
			var wantDiagnostics []Diagnostic
			for i, name := range []string{"Known", "Missing"} {
				ref := result.resourceRefs[i]
				assert.Equal(t, configuredResourceID{"demo://resources/clips", name}, ref.ID)
				assert.Equal(t, XGoResourceRefKindStringLiteral, ref.Kind)
				needle := `"` + name + `"`
				offset := strings.Index(tt.source, needle)
				require.NotEqual(t, -1, offset)
				line := uint32(strings.Count(tt.source[:offset], "\n"))
				column := uint32(offset - strings.LastIndex(tt.source[:offset], "\n") - 1)
				span := Range{Start: Position{Line: line, Character: column}, End: Position{Line: line, Character: column + uint32(len(needle))}}
				if name == "Known" {
					wantLinks = append(wantLinks, DocumentLink{
						Range:  span,
						Target: toURI("demo://resources/clips/Known"),
						Data:   XGoResourceRefDocumentLinkData{Kind: XGoResourceRefKindStringLiteral},
					})
				} else {
					wantDiagnostics = append(wantDiagnostics, Diagnostic{
						Range: span, Severity: SeverityError, Message: `resource "Missing" not found in "demo://resources/clips"`,
					})
				}
			}
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}})
			require.NoError(t, err)
			var resourceLinks []DocumentLink
			for _, link := range links {
				if link.Target != nil && strings.HasPrefix(string(*link.Target), "demo://") {
					resourceLinks = append(resourceLinks, link)
				}
			}
			assert.Equal(t, wantLinks, resourceLinks)
			report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
			require.NoError(t, err)
			require.Len(t, report.Items, 2)
			for _, item := range report.Items {
				full := requireWorkspaceFullDocumentDiagnosticReport(t, item)
				if full.URI == "file:///main_fixture.gox" {
					assert.Equal(t, wantDiagnostics, full.Items)
				} else {
					assert.Equal(t, DocumentURI("file:///types.xgo"), full.URI)
					assert.Empty(t, full.Items)
				}
			}
		})
	}

	t.Run("UnknownKwarg", func(t *testing.T) {
		s := newConfiguredResourceTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`import clips "example.com/support/v2"
type Options struct { Target clips.Name }
func configure(opts Options?) {}
configure target = "Known", unknown = 9
`),
			"resources.json": []byte(`{"demo://resources/clips":["Known"]}`),
		}, resourceTestConfig)
		analysis, err := analyzeFramework(s.requestProject())
		require.NoError(t, err)
		require.NotNil(t, analysis)
		require.Len(t, analysis.resources.resourceRefs, 1)
		assert.Equal(t, XGoResourceURI("demo://resources/clips/Known"), analysis.resources.resourceRefs[0].ID.URI())
	})
}
