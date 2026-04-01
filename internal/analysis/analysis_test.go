package analysis

import (
	"testing"

	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAnalyzers(t *testing.T) {
	wantNames := []string{
		"appends", "assign", "bools",
		"printf", "stringintconv", "unreachable", "unusedresult",
	}
	for _, name := range wantNames {
		t.Run(name, func(t *testing.T) {
			a, ok := DefaultAnalyzers[name]
			require.True(t, ok)
			assert.Equal(t, name, a.String())
			assert.NotNil(t, a.Analyzer())
			assert.True(t, a.EnabledByDefault())
			assert.Equal(t, protocol.SeverityWarning, a.Severity())
			assert.Nil(t, a.ActionKinds())
			assert.Nil(t, a.Tags())
		})
	}

	// loopclosure is registered but disabled by default for Go 1.22+ compatibility.
	t.Run("loopclosure", func(t *testing.T) {
		a, ok := DefaultAnalyzers["loopclosure"]
		require.True(t, ok)
		assert.Equal(t, "loopclosure", a.String())
		assert.NotNil(t, a.Analyzer())
		assert.False(t, a.EnabledByDefault())
	})
}

func TestAnalyzerSeverityDefault(t *testing.T) {
	a := &Analyzer{}
	assert.Equal(t, protocol.SeverityWarning, a.Severity())
}

func TestAnalyzerSeverityCustom(t *testing.T) {
	a := &Analyzer{severity: protocol.SeverityError}
	assert.Equal(t, protocol.SeverityError, a.Severity())
}

func TestAnalyzerNonDefault(t *testing.T) {
	a := &Analyzer{nonDefault: true}
	assert.False(t, a.EnabledByDefault())
}

func TestAnalyzerActionKindsAndTags(t *testing.T) {
	a := &Analyzer{
		actionKinds: []protocol.CodeActionKind{"quickfix"},
		tags:        []protocol.DiagnosticTag{protocol.Unnecessary},
	}
	assert.Equal(t, []protocol.CodeActionKind{"quickfix"}, a.ActionKinds())
	assert.Equal(t, []protocol.DiagnosticTag{protocol.Unnecessary}, a.Tags())
}
