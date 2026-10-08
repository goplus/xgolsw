package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfiguredResourceAliasCompletion(t *testing.T) {
	for _, tt := range []struct{ name, source, want, absent string }{
		{"Intrinsic", "echo Scene(\"|\")\n", "Studio", "Beep"},
		{"Nested", "echo Scene(string(\"|\"))\n", "Studio", "Beep"},
		{"OuterString", "echo string(Scene(\"|\"))\n", "Studio", "Beep"},
		{"Contextual", "clip Scene(\"|\")\n", "Beep", "Studio"},
		{"NestedContextual", "clip string(Scene(\"|\"))\n", "Beep", "Studio"},
		{"PartialName", "clip Scene(\"B|\")\n", "Beep", "Studio"},
		{"VarDeclaration", "func check() {\nvar value Scene = \"s|\"\necho value\n}\n", "Studio", "Beep"},
		{"VarDeclarationWithAlias", "type Alias = Scene\nfunc check() {\nvar value Alias = \"s|\"\necho value\n}\n", "Studio", "Beep"},
		{"Assignment", "var value Scene\nvalue = \"s|\"\n", "Studio", "Beep"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newResourceAliasTestServer(t, source, "Studio", "Beep")
			items := completionItemsAt(t, s, "main_fixture.gox", position)
			item := completionItemByLabel(items, tt.want)
			require.NotNil(t, item)
			require.NotNil(t, item.TextEdit)
			edit := requireValueAs[TextEdit](t, item.TextEdit.Value)
			updated := applyResourceRenameTestEdits(t, source, []TextEdit{edit})
			assert.Contains(t, updated, `"`+tt.want+`"`)
			requireNoDiagnostics(t, newResourceAliasTestServer(t, updated, "Studio", "Beep"))
			assert.NotContains(t, completionItemLabels(items), tt.absent)
		})
	}
}
