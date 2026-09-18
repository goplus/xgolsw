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
		s := newClassfileTestServer(t, map[string][]byte{
			"First.first": nil,
			"types.xgo":   []byte("type PropertyName string\n"),
			"Worker.firstwork": []byte(`var Count int
func show(name PropertyName) {}
show "Count"
show "Missing"
`),
		})
		proj := s.getProj()
		config := classfileTestModule()
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
		s := newTestServer(t, nil)
		for version, tt := range []struct {
			name     string
			members  string
			shadowed bool
		}{
			{name: "Inherited", members: "type Record struct { *Base }\n"},
			{name: "Shadowed", members: "type Record struct { *Base; Count []int }\nfunc (r *Record) Size(n int) int { return n }\n", shadowed: true},
			{name: "Restored", members: "type Record struct { *Base }\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := "type PropertyName string\ntype Base struct { Keep, Count int }\nfunc (b *Base) Size() int { return 1 }\n" + tt.members +
					"func (r *Record) show(name PropertyName) {}\nvar item Record\nitem.show \"Keep\"\nitem.show \"Count\"\nitem.show \"size\"\n"
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
				var want []Diagnostic
				for i, name := range []string{"Count", "size"} {
					line := uint32(strings.Count(source, "\n") - 2 + i)
					want = append(want, Diagnostic{
						Severity: SeverityError, Message: "unknown property \"" + name + "\"",
						Range: Range{Start: Position{Line: line, Character: 10}, End: Position{Line: line, Character: uint32(12 + len(name))}},
					})
				}
				assert.Equal(t, map[DocumentURI][]Diagnostic{"file:///main.xgo": want}, diagnostics.diagnostics)
			})
		}
	})
}
