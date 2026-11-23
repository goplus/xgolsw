package server

import (
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpxDiagnosticPass(t *testing.T) {
	t.Run("RegisteredClassfileReceiver", func(t *testing.T) {
		s := newMultiFrameworkTestServer(t, map[string][]byte{
			"First.first": nil,
			"types.xgo":   []byte("type PropertyName string\n"),
			"Worker.firstwork": []byte(`var Count int
func show(name PropertyName) {}
show "Count"
show "Missing"
`),
		})
		proj := s.getProj()
		config := multiFrameworkTestModule()
		config.Opt.Projects[0].Works[0].Prefix = "Actor"
		proj.SetModule(newTestModule(t, config))
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		result := newSpxAnalysis(proj)
		configurePass := spxDiagnosticPass(proj, result)
		diagnostics := newDiagnosticResult()
		propertyNameType := info.Pkg.Scope().Lookup("PropertyName").Type()
		s.inspectDiagnosticsAnalyzers(proj, &diagnostics, func(filename string, pass *protocol.Pass) {
			configurePass(filename, pass)
			pass.IsPropertyNameType = func(typ gotypes.Type) bool { return typ == propertyNameType }
		})
		assert.Equal(t, map[DocumentURI][]Diagnostic{
			"file:///Worker.firstwork": {{
				Severity: SeverityError, Message: `unknown property "Missing"`,
				Range: Range{Start: Position{Line: 3, Character: 5}, End: Position{Line: 3, Character: 14}},
			}},
		}, diagnostics.diagnostics)
	})

	t.Run("PropertyShadowingUpdates", func(t *testing.T) {
		s := newImportTestServer(t, nil)
		t.Cleanup(s.Close)
		for version, tt := range []struct {
			name     string
			members  string
			shadowed bool
		}{
			{name: "Inherited", members: "type Record struct { *Base }\n"},
			{name: "Shadowed", members: `type Record struct { *Base; Count []int }
func (r *Record) Size(n int) int { return n }
`, shadowed: true},
			{name: "Restored", members: "type Record struct { *Base }\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := `type PropertyName string
type Base struct { Keep, Count int }
func (b *Base) Size() int { return 1 }
` + tt.members +
					`func (r *Record) show(name PropertyName) {}
var item Record
item.show "Keep"
item.show "Count"
item.show "size"
`
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(source), Version: version + 1}})
				proj := s.getProj()
				info, err := proj.TypeInfo()
				require.NoError(t, err)
				result := newSpxAnalysis(proj)
				configurePass := spxDiagnosticPass(proj, result)
				diagnostics := newDiagnosticResult()
				propertyNameType := info.Pkg.Scope().Lookup("PropertyName").Type()
				s.inspectDiagnosticsAnalyzers(proj, &diagnostics, func(filename string, pass *protocol.Pass) {
					configurePass(filename, pass)
					// Keep the adapter's target lookup and use the fixture's property-name type.
					pass.IsPropertyNameType = func(typ gotypes.Type) bool { return typ == propertyNameType }
				})
				if !tt.shadowed {
					assert.Empty(t, diagnostics.diagnostics)
					return
				}
				line := uint32(strings.Count(source, "\n") - 1)
				assert.Equal(t, map[DocumentURI][]Diagnostic{"file:///main.xgo": {{
					Severity: SeverityError, Message: `unknown property "size"`,
					Range: Range{Start: Position{Line: line, Character: 10}, End: Position{Line: line, Character: 16}},
				}}}, diagnostics.diagnostics)
			})
		}
	})
}

func TestSpxDiagnosticPassAutoPropertyReceiver(t *testing.T) {
	for _, receiver := range []struct{ name, expression string }{
		{"Bare", "current"},
		{"Selector", "this.current"},
		{"Parenthesized", "(this.current)"},
		{"Chained", "this.current.next"},
	} {
		t.Run(receiver.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte("func Current() *Record { return nil }\n" + receiver.expression + ".Show(\"Count\")\n" + receiver.expression + ".Show(\"Missing\")\n"),
				"types.xgo": []byte(`type PropertyName string
type Record struct { Count int }
func (*Record) Show(name PropertyName) {}
func (*Record) Next() *Record { return nil }
`),
			})
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			configurePass := spxDiagnosticPass(proj, newSpxAnalysis(proj))
			diagnostics := newDiagnosticResult()
			propertyNameType := info.Pkg.Scope().Lookup("PropertyName").Type()
			s.inspectDiagnosticsAnalyzers(proj, &diagnostics, func(filename string, pass *protocol.Pass) {
				configurePass(filename, pass)
				pass.IsPropertyNameType = func(typ gotypes.Type) bool { return typ == propertyNameType }
			})
			assert.Equal(t, map[DocumentURI][]Diagnostic{"file:///main_fixture.gox": {{
				Severity: SeverityError, Message: `unknown property "Missing"`,
				Range: Range{Start: Position{Line: 2, Character: uint32(len(receiver.expression) + 6)}, End: Position{Line: 2, Character: uint32(len(receiver.expression) + 15)}},
			}}}, diagnostics.diagnostics)
		})
	}
}

func TestSpxPropertyNamesUseCallReceiver(t *testing.T) {
	const filename = "main.spx"
	const declarations = `var stageScore int

type Record struct {
    Count int
    Total int
    //xgols:notRecommended
    Hidden int
}

func (*Record) Show(name PropertyName) error { return nil }
`
	for _, tt := range []struct{ name, body string }{
		{"ExplicitMethod", `func (r *Record) Check() {
    show "|Count"
    show "stageScore"
    show "Hidden"
}
`},
		{"ErrorWrappedCommand", `func check() {
    var record Record
    record.Show! "|Count"
    record.Show! "stageScore"
    record.Show! "Hidden"
}
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, declarations+tt.body)
			s := newSpxTestServer(t, map[string][]byte{filename: []byte(source), "assets/index.json": []byte(`{}`)})
			t.Cleanup(s.Close)
			addErrorWrapTestImport(t, s)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			assert.Contains(t, labels, "Count")
			assert.Contains(t, labels, "Total")
			assert.NotContains(t, labels, "Hidden")
			assert.NotContains(t, labels, "stageScore")

			report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
			require.NoError(t, err)
			require.NotNil(t, report)
			require.Len(t, report.Items, 1)
			items := requireWorkspaceFullDocumentDiagnosticReport(t, report.Items[0]).Items
			require.Len(t, items, 1)
			assert.Equal(t, Diagnostic{
				Range: Range{
					Start: Position{Line: pos.Line + 1, Character: pos.Character - 1},
					End:   Position{Line: pos.Line + 1, Character: pos.Character - 1 + uint32(len(`"stageScore"`))},
				},
				Severity: SeverityError, Message: `unknown property "stageScore"`,
			}, items[0])
		})
	}
}

func TestSpxPropertyNamesUseLocalReceiverType(t *testing.T) {
	const filename = "main.spx"
	source, pos := typeDisplayTestSource(t, `type Record struct { PackageField int }

func check() {
    type Record struct {
        Show func(name PropertyName)
        LocalField int
    }
    var item Record
    item.Show("|LocalField")
}
`)
	s := newSpxTestServer(t, map[string][]byte{filename: []byte(source), "assets/index.json": []byte(`{}`)})
	t.Cleanup(s.Close)
	requireNoDiagnostics(t, s)
	labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
	assert.Contains(t, labels, "LocalField")
	assert.NotContains(t, labels, "PackageField")
}
