package server

import (
	gotypes "go/types"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerOverloadDocumentation(t *testing.T) {
	const groupDoc = "Choose selects a conversion."
	const candidateDoc = "chooseString handles text."
	const fixture = `func chooseInt(value int) int { return value }
// ` + candidateDoc + `
func chooseString(value string) int { return len(value) }

// ` + groupDoc + `
func Choose = (
chooseInt
chooseString
)
`
	for _, tt := range []struct {
		name, source string
		wantDocs     []string
	}{
		{name: "Declaration", source: strings.Replace(fixture, "func Choose", "func Ch|oose", 1),
			wantDocs: []string{groupDoc, candidateDoc}},
		{name: "UndocumentedCandidate", source: fixture + "println Ch|oose(1)\n",
			wantDocs: []string{groupDoc}},
		{name: "DocumentedCandidate", source: fixture + "println Ch|oose(\"text\")\n",
			wantDocs: []string{candidateDoc}},
		{name: "DirectCandidate", source: fixture + "println chooseI|nt(1)\n",
			wantDocs: []string{""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			for _, doc := range []string{groupDoc, candidateDoc} {
				assert.Equal(t, slices.Contains(tt.wantDocs, doc), strings.Contains(hover.Contents.Value, doc))
			}

			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, len(tt.wantDocs))
			for i, want := range tt.wantDocs {
				doc := help.Signatures[i].Documentation
				if want == "" {
					assert.Nil(t, doc)
				} else {
					require.NotNil(t, doc)
					assert.Equal(t, want, doc.Value)
				}
			}
			if tt.name == "UndocumentedCandidate" {
				items := completionItemsAt(t, s, "main.xgo", pos)
				var docs []string
				for _, item := range items {
					if item.Label == "Choose" {
						require.NotNil(t, item.Documentation)
						doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
						docs = append(docs, doc.Value)
					}
				}
				assert.True(t, slices.ContainsFunc(docs, func(doc string) bool { return strings.Contains(doc, groupDoc) }))
			}
		})
	}
}

func TestDefinitionContextSourceDocumentation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   string
	}{
		{name: "GroupedVariable", source: `var (
// Declaration documentation.
value int
other int
)
echo |value
`, want: "Declaration documentation.\n"},
		{name: "GroupedConstant", source: `const (
// Declaration documentation.
value = 1
other = 2
)
echo |value
`, want: "Declaration documentation.\n"},
		{name: "GroupedType", source: `type (
// Declaration documentation.
Item int
Other string
)
var value |Item
`, want: "Declaration documentation.\n"},
		{name: "MultipleNames", source: `// Declaration documentation.
var first, second int
echo |second
`, want: "Declaration documentation.\n"},
		{name: "VariableTrailingComment", source: `var value int // Declaration documentation.
echo |value
`, want: "Declaration documentation.\n"},
		{name: "TypeTrailingComment", source: `type Item int // Declaration documentation.
var value |Item
`, want: "Declaration documentation.\n"},
		{name: "VariableLeadingComment", source: `// Declaration documentation.
var value int // Trailing comment.
echo |value
`, want: "Declaration documentation.\n"},
		{name: "TypeLeadingComment", source: `// Declaration documentation.
type Item int // Trailing comment.
var value |Item
`, want: "Declaration documentation.\n"},
		{name: "GroupDoesNotDocumentIndividualMembers", source: `// Group documentation.
var (
value int
other int
)
echo |value
`},
		{name: "ParameterDoesNotBorrowFunctionDocumentation", source: `// Function documentation.
func run(value int) { echo |value }
`},
		{name: "MethodsWithSameName", source: `type First struct{}
type Second struct{}
// Other documentation.
func (First) Use() {}
// Declaration documentation.
func (Second) Use() {}
var second Second
second.U|se()
`, want: "Declaration documentation.\n"},
		{name: "InterfaceMethod", source: `type Runner interface {
// Declaration documentation.
Run()
}
var runner Runner
runner.R|un()
`, want: "Declaration documentation.\n"},
		{name: "FieldTrailingComment", source: `type Item struct {
Value int // Declaration documentation.
}
var item Item
echo item.|Value
`, want: "Declaration documentation.\n"},
		{name: "SiblingLocalType", source: `func first() {
// Unrelated documentation.
type Item int
var item Item
echo item
}
func second() {
// Declaration documentation.
type Item string
var item |Item
echo item
}
`, want: "Declaration documentation.\n"},
		{name: "NestedLocalType", source: `func run() {
// Outer documentation.
type Item int
if true {
// Declaration documentation.
type Item string
var item |Item
echo item
}
}
`, want: "Declaration documentation.\n"},
		{name: "LocalAlias", source: `func run() {
// Declaration documentation.
type Item = int
var item |Item
echo item
}
`, want: "Declaration documentation.\n"},
		{name: "NestedFunctionLocal", source: `// Enclosing documentation.
func run() {
invoke := func() { value := 1; echo |value }
invoke()
}
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProj()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			_, obj, _ := objectAtPosition(proj, info, file, ToPosition(proj, file, position))
			require.NotNil(t, obj)
			ctx := &definitionContext{proj: proj}
			doc, ok := ctx.sourceDocumentation(obj)
			assert.True(t, ok)
			assert.Equal(t, tt.want, doc)
		})
	}

	t.Run("ForeignPackagePosition", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`// Project documentation.
var value int
`)})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		local := info.Pkg.Scope().Lookup("value")
		require.NotNil(t, local)
		foreign := gotypes.NewVar(local.Pos(), gotypes.NewPackage("main", "main"), "value", gotypes.Typ[gotypes.Int])
		ctx := &definitionContext{proj: proj}
		doc, ok := ctx.sourceDocumentation(foreign)
		assert.False(t, ok)
		assert.Empty(t, doc)
		def := ctx.definitionForVar(foreign, "", false, &pkgdoc.PkgDoc{Vars: map[string]string{"value": "Imported documentation."}})
		assert.Equal(t, "Imported documentation.", def.Detail)
	})

	t.Run("SourceVersion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`// Old documentation.
var value int
`)})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		old := info.Pkg.Scope().Lookup("value")
		require.NotNil(t, old)
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(`// New documentation.
var value int
`), Version: 1}})
		ctx := &definitionContext{proj: s.getProj()}
		doc, ok := ctx.sourceDocumentation(old)
		assert.False(t, ok)
		assert.Empty(t, doc)
	})

	t.Run("GeneratedObject", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": nil})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		obj := gotypes.NewVar(token.NoPos, info.Pkg, "generated", gotypes.Typ[gotypes.Int])
		ctx := &definitionContext{proj: proj}
		doc, ok := ctx.sourceDocumentation(obj)
		assert.False(t, ok)
		assert.Empty(t, doc)
	})
}
