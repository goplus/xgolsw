package analysisutil

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractDoc(t *testing.T) {
	const source = `// Package checks provides analyzers.
//
// # Analyzer first
//
// first: checks the first condition
//
// The first analyzer's description.
//
// # Analyzer second
//
// second: checks the second condition
//
// The second analyzer's description.
//
// # Examples
//
// This section is not part of an analyzer's documentation.
package checks
`
	for _, tt := range []struct {
		name     string
		source   string
		analyzer string
		want     string
		wantErr  string
	}{
		{
			name: "FirstSection", source: source, analyzer: "first",
			want: "checks the first condition\n\nThe first analyzer's description.",
		},
		{
			name: "SecondSection", source: source, analyzer: "second",
			want: "checks the second condition\n\nThe second analyzer's description.",
		},
		{
			name: "WindowsLineEndings", source: strings.ReplaceAll(source, "\n", "\r\n"), analyzer: "second",
			want: "checks the second condition\n\nThe second analyzer's description.",
		},
		{
			name: "HeadingAtStart", analyzer: "first",
			source: "// # Analyzer first\n//\n// first: checks calls\npackage checks\n",
			want:   "checks calls",
		},
		{
			name: "BlockComment", analyzer: "first",
			source: "/*\n# Analyzer first\n\nfirst: checks calls\n*/\npackage checks\n",
			want:   "checks calls",
		},
		{
			name: "EmptySource", analyzer: "first",
			wantErr: "empty XGo source file",
		},
		{
			name: "InvalidSource", source: "not a package", analyzer: "first",
			wantErr: "not an XGo source file",
		},
		{
			name: "NoPackageDocumentation", source: "package checks\n", analyzer: "first",
			wantErr: "an XGo source file has no package doc comment",
		},
		{
			name: "EmptyPackageDocumentation", source: "//\npackage checks\n", analyzer: "first",
			wantErr: "package doc comment contains no 'Analyzer first' heading",
		},
		{
			name: "MissingHeading", source: source, analyzer: "missing",
			wantErr: "package doc comment contains no 'Analyzer missing' heading",
		},
		{
			name: "HeadingNamePrefix", source: source, analyzer: "fir",
			wantErr: "package doc comment contains no 'Analyzer fir' heading",
		},
		{
			name: "MissingSummary", analyzer: "first",
			source:  "// # Analyzer first\npackage checks\n",
			wantErr: "'Analyzer first' heading not followed by 'first: summary...' line",
		},
		{
			name: "WrongSummaryName", analyzer: "first",
			source:  "// # Analyzer first\n//\n// second: checks calls\npackage checks\n",
			wantErr: "'Analyzer first' heading not followed by 'first: summary...' line",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractDoc(tt.source, tt.analyzer)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMustExtractDoc(t *testing.T) {
	t.Run("ValidDocumentation", func(t *testing.T) {
		const source = "// # Analyzer first\n//\n// first: checks calls\npackage checks\n"
		assert.Equal(t, "checks calls", MustExtractDoc(source, "first"))
	})

	t.Run("InvalidDocumentation", func(t *testing.T) {
		assert.PanicsWithError(t, "empty XGo source file", func() {
			MustExtractDoc("", "first")
		})
	})
}
