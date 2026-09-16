package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkServerTextDocumentCompletionPackages(b *testing.B) {
	for _, tt := range []struct {
		name      string
		filename  string
		source    string
		position  Position
		label     string
		newServer testServerFactory
	}{
		{
			name: "Framework", filename: "main.xgo", newServer: newFrameworkTestServer,
			source:   "import api \"example.com/framework\"\necho api.Low\n",
			position: Position{Line: 1, Character: 9}, label: "Low",
		},
		{
			name: "Math", filename: "main.xgo", newServer: newTestServer,
			source:   "import api \"math\"\necho api.Pi\n",
			position: Position{Line: 1, Character: 9}, label: "Pi",
		},
		{
			name: "Reflect", filename: "main.xgo", newServer: newTestServer,
			source:   "import api \"reflect\"\necho api.Invalid\n",
			position: Position{Line: 1, Character: 9}, label: "Invalid",
		},
		{
			name: "Syscall", filename: "main.xgo", newServer: newTestServer,
			source:   "import api \"syscall\"\necho api.EINVAL\n",
			position: Position{Line: 1, Character: 9}, label: "EINVAL",
		},
		{
			name: "Classfile", filename: "main_fixture.gox", newServer: newFrameworkTestServer,
			source:   "onStart => {\n\n}\n",
			position: Position{Line: 1}, label: "Low",
		},
	} {
		b.Run(tt.name, func(b *testing.B) {
			s := tt.newServer(b, map[string][]byte{tt.filename: []byte(tt.source)})
			params := &CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: tt.position,
			}}
			// Warm imports and project analysis before measuring repeated requests.
			result, err := s.textDocumentCompletion(params)
			require.NoError(b, err)
			items, ok := result.([]CompletionItem)
			require.True(b, ok)
			require.NotNil(b, completionItemByLabel(items, tt.label))

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, err := s.textDocumentCompletion(params)
				require.NoError(b, err)
			}
		})
	}
}
