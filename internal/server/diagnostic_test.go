package server

import (
	"io/fs"
	"testing"

	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireRelatedFullDocumentDiagnosticReport(t *testing.T, report *DocumentDiagnosticReport) RelatedFullDocumentDiagnosticReport {
	t.Helper()

	fullReport, ok := report.Value.(RelatedFullDocumentDiagnosticReport)
	require.True(t, ok, "want RelatedFullDocumentDiagnosticReport, got %T", report.Value)
	return fullReport
}

func requireWorkspaceFullDocumentDiagnosticReport(t *testing.T, item WorkspaceDocumentDiagnosticReport) WorkspaceFullDocumentDiagnosticReport {
	t.Helper()

	fullReport, ok := item.Value.(WorkspaceFullDocumentDiagnosticReport)
	require.True(t, ok, "want WorkspaceFullDocumentDiagnosticReport, got %T", item.Value)
	return fullReport
}

func TestServerTextDocumentDiagnostic(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":   []byte(`println "hello"`),
			"first.xgo":  []byte(`var first = 1`),
			"second.xgo": []byte(`var second = 2`),
		})
		params := &DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		}

		report, err := s.textDocumentDiagnostic(params)
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
		assert.Empty(t, fullReport.Items)
	})

	t.Run("EmbeddedWorkClassFieldInitializer", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(``),
			"Bar_fixture.gox":  []byte(``),
			"Foo_fixture.gox": []byte(`var (
	others = [Bar]
)
`),
		}
		s := newFrameworkTestServer(t, m)

		report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///Foo_fixture.gox"},
		})
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		assert.Empty(t, fullReport.Items)
	})

	t.Run("LocalEnum", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`func check() {
	type Permission const (
		Read = 1 << iota
		Write
	)
	var permission Permission = Write
	println(permission)
}
`),
		}
		s := newTestServer(t, m)

		report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		assert.Empty(t, fullReport.Items)
	})

	t.Run("ParseError", func(t *testing.T) {
		fileMap := map[string][]byte{}
		fileMap["main.xgo"] = []byte(`
// Invalid syntax, missing closing parenthesis
var (
	Foobar string
`)
		s := newTestServer(t, fileMap)
		params := &DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		}

		report, err := s.textDocumentDiagnostic(params)
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
		require.Len(t, fullReport.Items, 2)
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  "expected ')', found 'EOF'",
			Range: Range{
				Start: Position{Line: 3, Character: 14},
				End:   Position{Line: 3, Character: 14},
			},
		})
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  "expected ';', found 'EOF'",
			Range: Range{
				Start: Position{Line: 3, Character: 14},
				End:   Position{Line: 3, Character: 14},
			},
		})
	})

	t.Run("TypeError", func(t *testing.T) {
		fileMap := map[string][]byte{}
		// case for https://github.com/goplus/xgolsw/issues/163
		fileMap["main_fixture.gox"] = []byte(`
func calcPos() (posX float64, posY float64) {
	return 1.0, 1.0
}

onStart => {
	var x, y int
	x, y = calcPos()
}
`)
		s := newFrameworkTestServer(t, fileMap)
		params := &DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		report, err := s.textDocumentDiagnostic(params)
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		require.Len(t, fullReport.Items, 1)
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  "cannot use float64 value as type int in assignment",
			Range: Range{
				Start: Position{Line: 7, Character: 1},
				End:   Position{Line: 7, Character: 17},
			},
		})
	})

	t.Run("MissingReturn", func(t *testing.T) {
		fileMap := map[string][]byte{}
		fileMap["main.xgo"] = []byte(`
func getValue() int {
	var x = 1
	x++
}
`)
		s := newTestServer(t, fileMap)
		params := &DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		}

		report, err := s.textDocumentDiagnostic(params)
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		require.Len(t, fullReport.Items, 1)
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  "missing return",
			Range: Range{
				Start: Position{Line: 4, Character: 0},
				End:   Position{Line: 4, Character: 1},
			},
		})
	})

	t.Run("PlainXGo", func(t *testing.T) {
		fileMap := map[string][]byte{}
		fileMap["main.xgo"] = []byte(`println "Hello, XGo!"`)
		s := newTestServer(t, fileMap)
		params := &DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		}

		report, err := s.textDocumentDiagnostic(params)
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
		assert.Empty(t, fullReport.Items)
	})

	t.Run("FileNotFound", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":   []byte(`println "hello"`),
			"first.xgo":  []byte(`var first = 1`),
			"second.xgo": []byte(`var second = 2`),
		})
		params := &DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///notexist.xgo"},
		}

		report, err := s.textDocumentDiagnostic(params)
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
		assert.Empty(t, fullReport.Items)
	})
}

func TestServerWorkspaceDiagnostic(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":   []byte(`println "hello"`),
			"first.xgo":  []byte(`var first = 1`),
			"second.xgo": []byte(`var second = 2`),
		})

		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.NotNil(t, report)
		assert.Len(t, report.Items, 3)
		foundFiles := make(map[string]struct{})
		for _, item := range report.Items {
			fullReport := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			relPath, err := s.fromDocumentURI(fullReport.URI)
			require.NoError(t, err)
			foundFiles[relPath] = struct{}{}
			assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
			assert.Empty(t, fullReport.Items)
		}
		assert.Contains(t, foundFiles, "main.xgo")
		assert.Contains(t, foundFiles, "first.xgo")
		assert.Contains(t, foundFiles, "second.xgo")
	})

	t.Run("ParseError", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
// Invalid syntax, missing closing parenthesis
var (
	Foobar string
`),
			"first.xgo": []byte(`var x int`),
		}
		s := newTestServer(t, m)

		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.NotNil(t, report)
		assert.Len(t, report.Items, 2)
		for _, item := range report.Items {
			fullReport := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			if fullReport.URI == "file:///main.xgo" {
				require.Len(t, fullReport.Items, 2)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  "expected ')', found 'EOF'",
					Range: Range{
						Start: Position{Line: 3, Character: 14},
						End:   Position{Line: 3, Character: 14},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  "expected ';', found 'EOF'",
					Range: Range{
						Start: Position{Line: 3, Character: 14},
						End:   Position{Line: 3, Character: 14},
					},
				})
			} else {
				assert.Empty(t, fullReport.Items)
			}
		}
	})

	t.Run("EmptyWorkspace", func(t *testing.T) {
		s := newTestServer(t, nil)

		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.NotNil(t, report)
		assert.Equal(t, []WorkspaceDocumentDiagnosticReport{}, report.Items)
	})
}

func TestServerGetDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		want    []Diagnostic
	}{

		{name: "IncompletePackage", content: "package", want: []Diagnostic{{
			Severity: SeverityError,
			Message:  "failed to parse source file: main.xgo:1:8: expected ';', found 'EOF' (and 1 more errors)",
		}}},
		{name: "NoError", content: "println 1\n", want: []Diagnostic{}},
		{name: "UndefinedImport", content: "fmt.println \"hello\"\n", want: []Diagnostic{{
			Severity: SeverityError, Message: "undefined: fmt",
			Range: Range{Start: Position{}, End: Position{Character: 3}},
		}}},
		{name: "SyntaxError", content: "var (\n    x int\n", want: []Diagnostic{
			{Severity: SeverityError, Message: "expected ')', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
			{Severity: SeverityError, Message: "expected ';', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
		}},
		{name: "UTF16SyntaxError", content: "var (\n    x int // \U0001f600\n", want: []Diagnostic{
			{Severity: SeverityError, Message: "expected ')', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 15}, End: Position{Line: 1, Character: 15}}},
			{Severity: SeverityError, Message: "expected ';', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 15}, End: Position{Line: 1, Character: 15}}},
		}},
		{name: "MultipleTypeErrors", content: "var x int = \"string\"\nvar y bool = 42\n", want: []Diagnostic{
			{Severity: SeverityError, Message: "cannot use \"string\" (type untyped string) as type int in assignment", Range: Range{Start: Position{Character: 12}, End: Position{Character: 20}}},
			{Severity: SeverityError, Message: "cannot use 42 (type untyped int) as type bool in assignment", Range: Range{Start: Position{Line: 1, Character: 13}, End: Position{Line: 1, Character: 15}}},
		}},
		{name: "UTF16", content: "println \"\U0001f600\", missing\n", want: []Diagnostic{{
			Severity: SeverityError, Message: "undefined: missing",
			Range: Range{Start: Position{Character: 14}, End: Position{Character: 21}},
		}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.content)})
			diagnostics := s.getDiagnostics("main.xgo")
			assert.ElementsMatch(t, tt.want, diagnostics)
			report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, requireRelatedFullDocumentDiagnosticReport(t, report).Items)
		})
	}
}

func TestServerDiagnosticsAt(t *testing.T) {
	t.Run("UnavailableFramework", func(t *testing.T) {
		files := map[string][]byte{
			"main_fixture.gox": []byte("println 1\n"),
			"broken.xgo":       []byte("var (\n    x int\n"),
		}
		s := newFrameworkTestServer(t, files)
		proj := s.getProj()
		importer := proj.Importer
		proj.Importer = completionTestImporter{Importer: importer, unavailablePath: testframework.PkgPath}
		_, err := proj.TypeInfo()
		var typeErr typesutil.Error
		require.ErrorAs(t, err, &typeErr)
		require.False(t, typeErr.Pos.IsValid())
		require.Equal(t, fs.ErrNotExist.Error(), typeErr.Msg)

		want := map[DocumentURI][]Diagnostic{
			"file:///main_fixture.gox": {},
			"file:///broken.xgo": {
				{Severity: SeverityError, Message: "expected ')', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
				{Severity: SeverityError, Message: "expected ';', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
			},
		}
		for filename := range files {
			assert.Equal(t, want[s.toDocumentURI(filename)], s.getDiagnostics(filename))
		}
		replier := newMockReplier()
		s.replier = replier
		require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI: "file:///main_fixture.gox", Version: 1, Text: string(files["main_fixture.gox"]),
			},
		}))
		assert.Equal(t, []Diagnostic{}, requirePublishedDiagnostics(t, replier, "file:///main_fixture.gox"))
		for uri, diagnostics := range want {
			report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			assert.Equal(t, diagnostics, requireRelatedFullDocumentDiagnosticReport(t, report).Items)
		}
		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.Len(t, report.Items, len(want))
		got := make(map[DocumentURI][]Diagnostic)
		for _, item := range report.Items {
			full := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			got[full.URI] = full.Items
		}
		assert.Equal(t, want, got)

		// A later edit must refresh diagnostics after the framework becomes available.
		proj.Importer = importer
		require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
			TextDocument: protocol.VersionedTextDocumentIdentifier{
				TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///broken.xgo"}, Version: 1,
			},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: "var value int = \"bad\"\n"}},
		}))
		wantTypeError := []Diagnostic{{
			Severity: SeverityError, Message: "cannot use \"bad\" (type untyped string) as type int in assignment",
			Range: Range{Start: Position{Character: 16}, End: Position{Character: 21}},
		}}
		assert.Equal(t, wantTypeError, requirePublishedDiagnostics(t, replier, "file:///broken.xgo"))
		documentReport, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///broken.xgo"}})
		require.NoError(t, err)
		assert.Equal(t, wantTypeError, requireRelatedFullDocumentDiagnosticReport(t, documentReport).Items)
	})

	t.Run("NoAST", func(t *testing.T) {
		for _, tt := range []struct {
			name            string
			withParsedFiles bool
		}{
			{name: "OnlyFailedFiles"},
			{name: "MixedFiles", withParsedFiles: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx":           []byte("println 1\n"),
					"nested/Unknown.spx": []byte("println 2\n"),
					"notes.txt":          []byte("not source code"),
				}
				parseFailure := []Diagnostic{{
					Severity: SeverityError,
					Message:  "failed to parse source file: unknown file kind",
				}}
				want := map[DocumentURI][]Diagnostic{
					"file:///main.spx":           parseFailure,
					"file:///nested/Unknown.spx": parseFailure,
				}
				if tt.withParsedFiles {
					files["helper.xgo"] = []byte("var value = 1\n")
					files["broken.xgo"] = []byte("var (\n    x int\n")
					want["file:///helper.xgo"] = []Diagnostic{}
					want["file:///broken.xgo"] = []Diagnostic{
						{Severity: SeverityError, Message: "expected ')', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
						{Severity: SeverityError, Message: "expected ';', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
					}
				}
				s := newTestServer(t, files)
				replier := newMockReplier()
				s.replier = replier
				for _, filename := range []string{"main.spx", "nested/Unknown.spx"} {
					astFile, err := s.getProj().ASTFile(filename)
					require.ErrorIs(t, err, parser.ErrUnknownFileKind)
					require.Nil(t, astFile)
					uri := s.toDocumentURI(filename)
					require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
						TextDocument: protocol.TextDocumentItem{URI: uri, Version: 1, Text: string(files[filename])},
					}))
					assert.Equal(t, parseFailure, requirePublishedDiagnostics(t, replier, uri))
				}
				for uri, diagnostics := range want {
					report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: uri}})
					require.NoError(t, err)
					full := requireRelatedFullDocumentDiagnosticReport(t, report)
					assert.Equal(t, string(DiagnosticFull), full.Kind)
					assert.Equal(t, diagnostics, full.Items)
				}
				report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
				require.NoError(t, err)
				require.Len(t, report.Items, len(want))
				got := make(map[DocumentURI][]Diagnostic)
				for _, item := range report.Items {
					full := requireWorkspaceFullDocumentDiagnosticReport(t, item)
					assert.Equal(t, string(DiagnosticFull), full.Kind)
					got[full.URI] = full.Items
				}
				assert.Equal(t, want, got)
			})
		}
	})

	t.Run("UnavailableAST", func(t *testing.T) {
		s := newTestServer(t, nil)
		s.workspaceRootFS = xgo.NewProject(nil, nil, 0)
		documentReport, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
		require.ErrorIs(t, err, xgo.ErrUnknownCacheKind)
		assert.Nil(t, documentReport)
		workspaceReport, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.ErrorIs(t, err, xgo.ErrUnknownCacheKind)
		assert.Nil(t, workspaceReport)
	})

	t.Run("Classfiles", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			filename    string
			projectFile string
			newServer   testServerFactory
		}{
			{name: "PlainXGo", filename: "main.xgo", newServer: newTestServer},
			{name: "LegacyXGo", filename: "main.gop", newServer: newTestServer},
			{name: "StandaloneClass", filename: "Record.gox", newServer: newTestServer},
			{name: "ProjectClass", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
			{name: "WorkClass", filename: "Worker_fixture.gox", projectFile: "main_fixture.gox", newServer: newFrameworkTestServer},
			{name: "OtherFrameworkWithSpxExtension", filename: "main.spx", newServer: newFrameworkTestServerWithSpxExtension},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte("println missing\n")}
				if tt.projectFile != "" {
					files[tt.projectFile] = nil
				}
				s := tt.newServer(t, files)
				report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
				require.NoError(t, err)
				require.Len(t, report.Items, len(files))
				for _, item := range report.Items {
					full := requireWorkspaceFullDocumentDiagnosticReport(t, item)
					assert.Equal(t, string(DiagnosticFull), full.Kind)
					if full.URI == s.toDocumentURI(tt.filename) {
						assert.Equal(t, []Diagnostic{{
							Severity: SeverityError, Message: "undefined: missing",
							Range: Range{Start: Position{Character: 8}, End: Position{Character: 15}},
						}}, full.Items)
					} else {
						assert.Equal(t, []Diagnostic{}, full.Items)
					}
				}
			})
		}
	})

	t.Run("CrossFileTypeError", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":   []byte("println value()\n"),
			"values.xgo": []byte("func value() int {\n    var number int = \"bad\"\n    return 1\n}\n"),
			"notes.txt":  []byte("not source code"),
		})
		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.Len(t, report.Items, 2)
		want := []Diagnostic{{
			Severity: SeverityError, Message: "cannot use \"bad\" (type untyped string) as type int in assignment",
			Range: Range{Start: Position{Line: 1, Character: 21}, End: Position{Line: 1, Character: 26}},
		}}
		for _, item := range report.Items {
			full := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			switch full.URI {
			case "file:///values.xgo":
				assert.Equal(t, want, full.Items)
			case "file:///main.xgo":
				assert.Equal(t, []Diagnostic{}, full.Items)
			default:
				assert.Fail(t, "unexpected diagnostic document", "%s", full.URI)
			}
		}
		diagnostics := s.getDiagnostics("main.xgo")
		assert.Empty(t, diagnostics)
		diagnostics = s.getDiagnostics("values.xgo")
		assert.Equal(t, want, diagnostics)
	})

	t.Run("Analyzer", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("values := [1, 2]\nvalues = append(values)\n")})
		report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
		require.NoError(t, err)
		assert.Equal(t, []Diagnostic{{
			Severity: SeverityError, Message: "append with no values",
			Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 23}},
		}}, requireRelatedFullDocumentDiagnosticReport(t, report).Items)
	})
}
