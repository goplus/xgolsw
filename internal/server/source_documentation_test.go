package server

import (
	"bytes"
	"cmp"
	gotypes "go/types"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"
)

func TestServerEmbeddedClassFieldDocumentation(t *testing.T) {
	const fixture = `var (
	// Item is the embedded value.
	*Item
	Other // Other is a second embedded value.
)

// Item is the embedded type.
type Item struct { Value int }
type Other struct { Count int }
`
	const filename = "main.xgo"
	for _, tt := range []struct {
		name, field, want string
	}{
		{"LeadingComment", "Item", "Item is the embedded value."},
		{"TrailingComment", "Other", "Other is a second embedded value."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "var record Record\n_ = record."+tt.field[:1]+"|"+tt.field[1:]+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source), "Record.gox": []byte(fixture)})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)
			params := TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)},
				Position:     pos,
			}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, tt.want)
			assert.NotContains(t, hover.Contents.Value, "Item is the embedded type.")

			item := completionItemByLabel(completionItemsAt(t, s, filename, pos), tt.field)
			require.NotNil(t, item)
			require.NotNil(t, item.Documentation)
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
			assert.Contains(t, doc.Value, tt.want)
			assert.NotContains(t, doc.Value, "Item is the embedded type.")
		})
	}
}

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

func TestServerImportedOverloadDocumentation(t *testing.T) {
	const imports = "import dep \"example.com/dependency\"\n"
	const instance = "var record dep.Record\n"
	const groupDoc = "Pick selects a conversion."
	const implementationDoc = "Pick__1 handles text."
	for _, tt := range []struct {
		name, source, wantDoc, completion string
		overview, signature, definition   string
		singleHover                       bool
	}{
		{name: "GroupFallback", source: "dep.pi|ck(1)", wantDoc: groupDoc, completion: "pick"},
		{name: "Implementation", source: "dep.pi|ck(\"text\")", wantDoc: implementationDoc},
		{name: "Chained", source: "dep.ch|ained(1)", wantDoc: "Chained delegates to Target.",
			completion: "chained", signature: "chained(value int)", singleHover: true},
		{name: "FunctionVariable", source: "_ = dep.variableLo|okup()", wantDoc: "VariableLookup resolves a stored value.",
			completion: "variableLookup", signature: "variableLookup() string"},
		{name: "DirectFunctionVariable", source: "_ = dep.readSh|ared()", wantDoc: "ReadTextCallback reads a shared label.",
			completion: "readShared", signature: "readShared() string"},
		{name: "ParenthesizedFunctionVariable", source: "_ = (dep.readSh|ared)()", wantDoc: "ReadTextCallback reads a shared label.",
			completion: "readShared", signature: "readShared() string"},
		{name: "DirectImplementation", source: "dep.Pick|__0(1)",
			signature: "Pick__0(value int)", definition: "pick#0"},
		{name: "DirectImplementationAlias", source: "dep.pick|__0(1)",
			signature: "pick__0(value int)", definition: "pick#0"},
		{name: "DirectMethodImplementation", source: instance + "_ = record.Format|__0(true)",
			signature: "Format__0(flag bool) bool", definition: "Record.format#0"},
		{name: "Method", source: instance + "record.fo|rmat(1)", wantDoc: "Format documents a method overload.", completion: "format"},
		{name: "ChainedMethod", source: instance + "record.re|nder(1)", wantDoc: "Render uses the formatting overloads.", completion: "render"},
		{name: "Template", source: instance + "record.li|st(1)", wantDoc: "List documents a template overload.",
			completion: "list", signature: "list(value int)"},
		{name: "TemplateVariable", source: instance + "_ = record.applyVar|iable(1)",
			wantDoc: "XGot_Record_ApplyVariable applies a stored callback.", completion: "applyVariable", signature: "applyVariable(value int) int"},
		{name: "ParenthesizedTemplateVariable", source: instance + "_ = (record.applyVar|iable)(1)",
			wantDoc: "XGot_Record_ApplyVariable applies a stored callback.", completion: "applyVariable", signature: "applyVariable(value int) int"},
		{name: "ImplicitTemplateVariable", source: instance + "_ = record.readVar|iable",
			wantDoc: "XGot_Record_ReadVariable reads the record's value.", completion: "readVariable", signature: "readVariable() int"},
		{name: "UnboundTemplateVariable", source: instance + "_ = dep.Record.applyVar|iable(record, 1)",
			wantDoc: "XGot_Record_ApplyVariable applies a stored callback.", completion: "applyVariable", signature: "applyVariable(record dep.Record, value int) int"},
		{name: "ChainedTemplate", source: instance + "record.co|llect(1)", wantDoc: "Collect documents a chained template overload.",
			completion: "collect", signature: "collect(value int)"},
		{name: "ChainedPromotedTemplate", source: "var record dep.CollectingRecord\nvar value int = record.Collect\nrecord.co|llect(1)",
			wantDoc: "Collect documents a chained template overload.", completion: "collect", signature: "collect(value int)"},
		{name: "UnboundTemplate", source: instance + "dep.Record.li|st(record, 1)",
			wantDoc: "List documents a template overload.", completion: "list", signature: "list(record dep.Record, value int)"},
		{name: "TypeAsParameters", source: "_ = dep.tran|sform(int, 1)", wantDoc: "Transform converts a value.",
			signature: "transform(T Type, value T) T"},
		{name: "AliasOfTypeAsParameters", source: "_ = dep.convertVa|lue(int, 1)",
			wantDoc: "ConvertValue applies the requested type to a value.", completion: "convertValue",
			signature: "convertValue(T Type, value T) T"},
		{name: "ParenthesizedAliasOfTypeAsParameters", source: "_ = (dep.convertVa|lue)(int, 1)",
			wantDoc: "ConvertValue applies the requested type to a value.", completion: "convertValue",
			signature: "convertValue(T Type, value T) T"},
		{name: "SharedImplementationWithInferredType", source: "_ = dep.convertEi|ther(1)",
			wantDoc: "ConvertEither accepts either an inferred or an explicit type.", completion: "convertEither",
			overview: "convertEither[T any](value T) T", signature: "convertEither(value T) T"},
		{name: "SharedImplementationWithExplicitType", source: "_ = dep.convertEi|ther(int, 1)",
			wantDoc: "ConvertEither accepts either an inferred or an explicit type.", signature: "convertEither(T Type, value T) T",
			singleHover: true},
		{name: "ParenthesizedSharedImplementation", source: "_ = (dep.convertEi|ther)(int, 1)",
			wantDoc: "ConvertEither accepts either an inferred or an explicit type.", signature: "convertEither(T Type, value T) T",
			singleHover: true},
		{name: "ForwardedInferredType", source: "_ = dep.forwardCo|nvert(1)", wantDoc: "ForwardConvert delegates to ConvertEither.",
			overview: "forwardConvert[T any](value T) T", signature: "forwardConvert(value T) T", singleHover: true},
		{name: "ForwardedExplicitType", source: "_ = dep.forwardCo|nvert(int, 1)", wantDoc: "ForwardConvert delegates to ConvertEither.",
			signature: "forwardConvert(T Type, value T) T", singleHover: true},
		{name: "ParenthesizedForwardedExplicitType", source: "_ = (dep.forwardCo|nvert)(int, 1)",
			wantDoc: "ForwardConvert delegates to ConvertEither.", signature: "forwardConvert(T Type, value T) T", singleHover: true},
		{name: "ForwardedTypeAsParameters", source: "_ = dep.zTra|nsform(string, \"text\")",
			signature: "zTransform(T Type, value T) T", singleHover: true},
		{name: "TemplateTypeAsParameters", source: instance + "_ = record.ad|apt(int, 1)",
			wantDoc: "Adapt documents a template overload with type arguments.", completion: "adapt", signature: "adapt(T Type, value T) T"},
		{name: "ForwardedTemplateTypeAsParameters", source: instance + "_ = record.re|layType(int, 1)",
			wantDoc: "RelayType delegates to a conversion with an explicit type argument.", completion: "relayType",
			signature: "relayType(T Type, value T) T", singleHover: true},
		{name: "ParenthesizedForwardedTemplate", source: instance + "_ = (record.re|layType)(int, 1)",
			wantDoc:   "RelayType delegates to a conversion with an explicit type argument.",
			signature: "relayType(T Type, value T) T", singleHover: true},
		{name: "Static", source: "_ = dep.Record.ma|ke(1)", wantDoc: "Make documents a static overload.", completion: "make"},
		{name: "StaticVariableOverload", source: instance + "_ = dep.Record.applySt|atic(record, 1)",
			wantDoc: "XGot_Record_ApplyVariable applies a stored callback.", completion: "applyStatic",
			signature: "applyStatic(record dep.Record, value int) int"},
		{name: "ChainedStatic", source: "_ = dep.Record.bu|ild(1)", wantDoc: "Build documents a chained static overload.",
			completion: "build", signature: "build(value int) dep.Record"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, imports+tt.source+"\n")
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)
			params := TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")},
				Position:     pos,
			}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			if tt.singleHover {
				assert.Equal(t, 1, strings.Count(hover.Contents.Value, `<pre is="definition-item"`))
			}
			if tt.wantDoc != "" {
				assert.Contains(t, hover.Contents.Value, tt.wantDoc)
			}
			overview := cmp.Or(tt.overview, tt.signature)
			if overview != "" {
				assert.Contains(t, hover.Contents.Value, `overview="func `+overview+`"`)
			}
			if tt.definition != "" {
				assert.Contains(t, hover.Contents.Value, `def-id="xgo:example.com/dependency?`+tt.definition+`"`)
			}
			for _, doc := range []string{groupDoc, implementationDoc} {
				if tt.wantDoc != doc {
					assert.NotContains(t, hover.Contents.Value, doc)
				}
			}

			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			if tt.signature != "" {
				assert.Equal(t, tt.signature, help.Signatures[0].Label)
			}
			if tt.completion != "" {
				assert.True(t, strings.HasPrefix(help.Signatures[0].Label, tt.completion+"("), help.Signatures[0].Label)
			}
			doc := help.Signatures[0].Documentation
			if tt.wantDoc == "" {
				assert.Nil(t, doc)
			} else {
				require.NotNil(t, doc)
				assert.Equal(t, tt.wantDoc, doc.Value)
			}
			if tt.completion != "" {
				items := completionItemsAt(t, s, "main.xgo", pos)
				require.Contains(t, completionItemLabels(items), tt.completion)
				assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
					if item.Label != tt.completion {
						return false
					}
					require.NotNil(t, item.Documentation)
					doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
					if overview != "" {
						assert.Contains(t, doc.Value, `overview="func `+overview+`"`)
					}
					return strings.Contains(doc.Value, tt.wantDoc)
				}))
			}
		})
	}
}

func TestServerImportedInterfaceMethodDocumentation(t *testing.T) {
	const filename = "main.xgo"
	const path = "example.com/dependency"
	for _, tt := range []struct {
		typ, owner, doc string
	}{
		{"BetaReader[int]", "BetaReader", "Read reads a beta value."},
		{"AliasReader", "BetaReader", "Read reads a beta value."},
		{"PublicReader", "PublicReader", "Read reads a private value."},
		{"NamedReader", "NamedReader", "Read reads an internal value."},
		{"EmbeddedReader", "EmbeddedReader", "Read reads a promoted value."},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			wantID := "xgo:" + path + "?" + tt.owner + ".read"
			source, pos := typeDisplayTestSource(t, "import dep \""+path+"\"\nvar reader dep."+tt.typ+"\n_ = reader.re|ad()\n")
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			useExportedDependency(t, s, path)
			requireNoDiagnostics(t, s)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
			assert.Contains(t, hover.Contents.Value, tt.doc)

			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, "read() int", help.Signatures[0].Label)
			require.NotNil(t, help.Signatures[0].Documentation)
			assert.Equal(t, tt.doc, help.Signatures[0].Documentation.Value)

			item := completionItemByLabel(completionItemsAt(t, s, filename, pos), "read")
			require.NotNil(t, item)
			assert.Equal(t, wantID, requireValueAs[*CompletionItemData](t, item.Data).Definition.String())
			require.NotNil(t, item.Documentation)
			assert.Contains(t, requireValueAs[MarkupContent](t, item.Documentation.Value).Value, tt.doc)
		})
	}
}

func TestServerImportedAliasMethodDocumentation(t *testing.T) {
	const filename, path = "main.xgo", "example.com/dependency"
	for _, tt := range []struct {
		typ, method, comment string
	}{
		{typ: "PublicCounter", method: "read", comment: "Read returns the current count."},
		{typ: "PublicCounterPointer", method: "reset", comment: "Reset clears the current count."},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import dep \""+path+"\"\nvar counter dep."+tt.typ+
				"\ncounter.re|"+tt.method[2:]+"()\n")
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			useExportedDependency(t, s, path)
			requireNoDiagnostics(t, s)

			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, tt.comment)

			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			require.NotNil(t, help.Signatures[0].Documentation)
			assert.Equal(t, tt.comment, help.Signatures[0].Documentation.Value)

			items := completionItemsAt(t, s, filename, pos)
			assert.NotContains(t, completionItemLabels(items), "readLegacy")
			item := completionItemByLabel(items, tt.method)
			require.NotNil(t, item)
			require.NotNil(t, item.Documentation)
			assert.Contains(t, requireValueAs[MarkupContent](t, item.Documentation.Value).Value, tt.comment)
		})
	}
}

func TestServerImportedStructFieldDocumentation(t *testing.T) {
	const filename = "main.xgo"
	const path = "example.com/dependency"
	for _, name := range []string{"PublicValue", "PublicRecord", "EmbeddedRecord", "PublicPointer", "InlinePointer"} {
		t.Run(name, func(t *testing.T) {
			wantID := "xgo:" + path + "?" + name + ".Value"
			source, pos := typeDisplayTestSource(t, "import dep \""+path+"\"\nvar record dep."+name+"\necho record.Va|lue\n")
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			useExportedDependency(t, s, path)
			requireNoDiagnostics(t, s)

			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
			assert.Contains(t, hover.Contents.Value, "Value stores the payload.")

			items := completionItemsAt(t, s, filename, pos)
			assert.NotContains(t, completionItemLabels(items), "Hidden")
			item := completionItemByLabel(items, "Value")
			require.NotNil(t, item)
			assert.Equal(t, wantID, requireValueAs[*CompletionItemData](t, item.Data).Definition.String())
			require.NotNil(t, item.Documentation)
			assert.Contains(t, requireValueAs[MarkupContent](t, item.Documentation.Value).Value, "Value stores the payload.")
		})
	}
}

func TestServerLocalPointerAliasFieldDocumentation(t *testing.T) {
	const filename = "main.xgo"
	const wantID = "xgo:main?Pointer.Value"
	source, pos := typeDisplayTestSource(t, `type Pointer = *(struct {
    // Value stores the payload.
    Value int
    //xgols:notRecommended
    Hidden int
})
var pointer Pointer
echo pointer.Va|lue
`)
	s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
	t.Cleanup(s.Close)
	requireNoDiagnostics(t, s)

	params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos}
	hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
	assert.Contains(t, hover.Contents.Value, "Value stores the payload.")

	items := completionItemsAt(t, s, filename, pos)
	assert.NotContains(t, completionItemLabels(items), "Hidden")
	item := completionItemByLabel(items, "Value")
	require.NotNil(t, item)
	assert.Equal(t, wantID, requireValueAs[*CompletionItemData](t, item.Data).Definition.String())
}

func useExportedDependency(t *testing.T, s *Server, path string) {
	t.Helper()

	proj := s.getProj()
	fallback := proj.Importer
	pkg, err := fallback.Import(path)
	require.NoError(t, err)
	var export bytes.Buffer
	require.NoError(t, gcexportdata.Write(&export, proj.Fset, pkg))
	pkg, err = gcexportdata.Read(&export, token.NewFileSet(), make(map[string]*gotypes.Package), path)
	require.NoError(t, err)
	proj.Importer = testImporterFunc(func(importPath string) (*gotypes.Package, error) {
		if importPath == path {
			return pkg, nil
		}
		return fallback.Import(importPath)
	})
}

func TestServerImplicitClassOverload(t *testing.T) {
	const filename = "main_fixture.gox"
	const bound = "render(value int) int"
	for _, tt := range []struct {
		name, source, signature string
	}{
		{name: "MethodWithFieldShadow", source: "var Render int\nfunc Run() int { return ren|der(1) }\n", signature: bound},
		{name: "InlineOverload", source: `func Run = (
	func() int { return ren|der(1) }
)
`, signature: bound},
		{name: "ShadowEntry", source: "ren|der 1\n", signature: bound},
		{name: "FieldInitializer", source: "var value int = ren|der(1)\n", signature: bound},
		{name: "StaticMethod", source: "func .Run() int { return ren|der(nil, 1) }\n", signature: "render(app any, value int) int"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, `func RenderValue(app any, value int) int { return value }
const XGoo_RenderBase = "RenderValue"
const XGoo__Render = "RenderBase"
const XGoo__XGot_App_Render = "RenderBase"
`)
			requireNoDiagnostics(t, s)

			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `overview="func `+tt.signature+`"`)
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.signature, help.Signatures[0].Label)
			item := completionItemByLabel(completionItemsAt(t, s, filename, pos), "render")
			require.NotNil(t, item)
			require.NotNil(t, item.Documentation)
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
			assert.Contains(t, doc.Value, `overview="func `+tt.signature+`"`)
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

	t.Run("StructFieldSiblings", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`type Item struct {
// Leading documentation.
First, Second int
Third int // Trailing documentation.
}

type Other struct {
Unrelated int // Other documentation.
}
`)})
		t.Cleanup(s.Close)
		proj := s.requestProject()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		item := info.Pkg.Scope().Lookup("Item")
		require.NotNil(t, item)
		fields := requireValueAs[*gotypes.Struct](t, item.Type().Underlying())
		ctx := &definitionContext{proj: proj}
		doc, ok := ctx.sourceDocumentation(fields.Field(1))
		require.True(t, ok)
		assert.Equal(t, "Leading documentation.\n", doc)
		assert.Empty(t, ctx.structFields)
		doc, ok = ctx.sourceDocumentation(fields.Field(2))
		require.True(t, ok)
		assert.Equal(t, "Trailing documentation.\n", doc)
		assert.Len(t, ctx.structFields, 3)

		other := info.Pkg.Scope().Lookup("Other")
		require.NotNil(t, other)
		otherFields := requireValueAs[*gotypes.Struct](t, other.Type().Underlying())
		doc, ok = ctx.sourceDocumentation(otherFields.Field(0))
		require.True(t, ok)
		assert.Equal(t, "Other documentation.\n", doc)
		assert.Len(t, ctx.structFields, 4)
	})

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
