package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/stretchr/testify/require"
)

func symbolBenchmarkSource(size int, sparse bool) []byte {
	var source strings.Builder
	for i := range size {
		resultType, resultValue := "int", "0"
		if sparse {
			resultType = fmt.Sprintf("[%d]int", i+1)
			resultValue = resultType + "{}"
		}
		fmt.Fprintf(&source, "type Reader%d interface { Read() %s }\ntype Item%d struct{}\nfunc (Item%d) Read() %s { return %s }\nfunc use%d(reader Reader%d) { println reader.Read(), Item%d{}.Read() }\n", i, resultType, i, i, resultType, resultValue, i, i, i)
	}
	return []byte(source.String())
}

func BenchmarkServerSymbolRequests(b *testing.B) {
	for _, size := range []int{16, 128} {
		for _, workload := range []struct {
			name   string
			sparse bool
		}{{"Dense", false}, {"Sparse", true}} {
			for _, request := range []string{"References", "Rename", "Implementation", "Highlight"} {
				b.Run(fmt.Sprintf("%s/Types%d/%s", request, size, workload.name), func(b *testing.B) {
					s := newTestServer(b, map[string][]byte{"main.xgo": symbolBenchmarkSource(size, workload.sparse)})
					s.replier = discardReplier{}
					_, err := s.requestProject().TypeInfo()
					require.NoError(b, err)
					position := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Character: 25}}
					implementations := size
					if workload.sparse {
						implementations = 1
					}
					run := func() {
						switch request {
						case "References":
							result, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: position})
							require.NoError(b, err)
							require.Len(b, result, implementations*2)
						case "Rename":
							result, err := s.textDocumentRename(&RenameParams{TextDocument: position.TextDocument, Position: position.Position, NewName: "Fetch"})
							require.NoError(b, err)
							require.NotNil(b, result)
							require.Len(b, result.Changes[position.TextDocument.URI], implementations*4)
						case "Implementation":
							result, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: position})
							require.NoError(b, err)
							locations, ok := result.([]Location)
							require.True(b, ok)
							require.Len(b, locations, implementations)
						case "Highlight":
							result, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: position})
							require.NoError(b, err)
							require.NotNil(b, result)
							require.Len(b, *result, 2)
						}
					}
					run()
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						run()
					}
				})
			}
		}
	}
}

func BenchmarkProjectSymbolAnalysis(b *testing.B) {
	for _, size := range []int{16, 128} {
		for _, workload := range []struct {
			name   string
			sparse bool
		}{{"Dense", false}, {"Sparse", true}} {
			b.Run(fmt.Sprintf("Types%d/%s", size, workload.name), func(b *testing.B) {
				s := newTestServer(b, map[string][]byte{"main.xgo": symbolBenchmarkSource(size, workload.sparse)})
				base := s.requestProject()
				info, err := base.TypeInfo()
				require.NoError(b, err)
				obj := info.Pkg.Scope().Lookup("Reader0")
				require.NotNil(b, obj)
				// Each snapshot shares completed types, but starts with cold symbol
				// caches. This measures indexing separately from type checking.
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					proj := base.Snapshot()
					source, err := sourceInfoForProject(proj)
					require.NoError(b, err)
					require.Len(b, source.references[obj], 1)
					methods, err := methodInfoForProject(proj)
					require.NoError(b, err)
					for _, relations := range methods.relations {
						require.NotEmpty(b, relations())
					}
				}
			})
		}
	}
}

type discardReplier struct{}

func (discardReplier) ReplyMessage(jsonrpc2.Message) error { return nil }
