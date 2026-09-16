package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkServerTextDocumentCompletionMembers(b *testing.B) {
	for _, size := range []int{16, 128, 512} {
		for _, kind := range []string{"StructLiteral", "Kwargs"} {
			b.Run(fmt.Sprintf("%s/Fields%d", kind, size), func(b *testing.B) {
				var source strings.Builder
				source.WriteString("type Options struct {\n")
				for i := range size {
					fmt.Fprintf(&source, "Field%d int\n", i)
				}
				source.WriteString("}\n")
				var position Position
				if kind == "StructLiteral" {
					source.WriteString("value := Options{}\necho value\n")
					position = Position{Line: uint32(size + 2), Character: uint32(len("value := Options{"))}
				} else {
					source.WriteString("func configure(opts Options?) {}\nconfigure field = 0\n")
					position = Position{Line: uint32(size + 3), Character: uint32(len("configure field"))}
				}
				s := newTestServer(b, map[string][]byte{"main.xgo": []byte(source.String())})
				params := &CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
				}}
				result, err := s.textDocumentCompletion(params)
				require.NoError(b, err)
				items, ok := result.([]CompletionItem)
				require.True(b, ok)
				require.Len(b, items, size)

				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					_, err := s.textDocumentCompletion(params)
					require.NoError(b, err)
				}
			})
		}
	}
}

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
