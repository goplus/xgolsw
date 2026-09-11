package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckNonSpxDocumentation(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "Spx", path: "github.com/goplus/spx"},
		{name: "VersionedSpx", path: "github.com/goplus/spx/v3"},
		{name: "SpxSubpackage", path: "github.com/goplus/spx/v3/internal/engine"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &testFailureRecorder{TB: t}
			err := checkNonSpxDocumentation(recorder, tt.path)
			assert.EqualError(t, err, "unexpected spx documentation lookup: "+tt.path)
			messages := recorder.recordedFailures()
			require.Len(t, messages, 1)
			assert.Contains(t, messages[0], "unexpected spx documentation lookup: "+tt.path)
		})
	}
	t.Run("OtherPackages", func(t *testing.T) {
		for _, pkgPath := range []string{"fmt", testframework.PkgPath, "github.com/goplus/spxutils"} {
			assert.NoError(t, checkNonSpxDocumentation(t, pkgPath))
		}
	})
}

func TestServerHandleMessageRejectedTestDependencies(t *testing.T) {
	for _, environment := range []struct {
		name      string
		newServer testServerFactory
	}{
		{name: "PlainXGo", newServer: newTestServer},
		{name: "Framework", newServer: newFrameworkTestServer},
	} {
		t.Run(environment.name, func(t *testing.T) {
			for _, tt := range []struct {
				name    string
				source  string
				method  string
				params  any
				message string
			}{
				{
					name: "Import", source: "import \"github.com/goplus/spx/v3\"\nvar Count int\n",
					method:  "textDocument/diagnostic",
					params:  DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}},
					message: "unexpected spx import: github.com/goplus/spx/v3",
				},
				{
					name: "Documentation", source: "import \"strings\"\nvar _ strings.Builder\n",
					method: "textDocument/completion",
					params: CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Character: 8},
					}},
					message: "unexpected spx documentation lookup: github.com/goplus/spx/v3",
				},
			} {
				t.Run(tt.name, func(t *testing.T) {
					recorder := &testFailureRecorder{TB: t}
					s := environment.newServer(recorder, map[string][]byte{"main.xgo": []byte(tt.source)})
					s.listPkgs = func() ([]string, error) { return []string{"github.com/goplus/spx/v3"}, nil }
					replier := newMockReplier()
					s.replier = replier
					initializeServerForTest(t, s, replier)
					require.Empty(t, recorder.recordedFailures())
					call, err := jsonrpc2.NewCall(jsonrpc2.NewIntID(1), tt.method, tt.params)
					require.NoError(t, err)
					require.NoError(t, s.HandleMessage(call))
					response := replier.waitForResponse(call.ID(), 5*time.Second)
					require.NotNil(t, response, "dependency rejection must not abort the request goroutine")
					require.NoError(t, response.Err())
					assert.True(t, json.Valid(response.Result()))
					messages := recorder.recordedFailures()
					require.NotEmpty(t, messages)
					assert.Contains(t, strings.Join(messages, "\n"), tt.message)

					if tt.name == "Documentation" {
						doc, err := s.lookupPkgDoc("github.com/goplus/spx/v3")
						assert.Nil(t, doc)
						assert.EqualError(t, err, tt.message)
						_, err = s.lookupPkgDoc("example.com/missing")
						assert.ErrorIs(t, err, fs.ErrNotExist)
					}

					// A later request must still complete after the rejected dependency.
					shutdown, err := jsonrpc2.NewCall(jsonrpc2.NewIntID(2), "shutdown", nil)
					require.NoError(t, err)
					require.NoError(t, s.HandleMessage(shutdown))
					response = replier.waitForResponse(shutdown.ID(), 5*time.Second)
					require.NotNil(t, response)
					assert.NoError(t, response.Err())
				})
			}
		})
	}
}

type testFailureRecorder struct {
	testing.TB
	mu       sync.Mutex
	messages []string
}

func (r *testFailureRecorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
	r.mu.Unlock()
}

func (r *testFailureRecorder) recordedFailures() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.messages)
}
