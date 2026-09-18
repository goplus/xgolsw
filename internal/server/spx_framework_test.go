package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefinitionContextIsInFrameworkEventHandlerSpx(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   bool
	}{
		{"OrdinaryCallback", "func invoke(fn func()) { fn() }\ninvoke => {\n    |println 1\n}\n", false},
		{"UserHandler", "func onCustom(fn func()) { fn() }\nonCustom => {\n    |println 1\n}\n", false},
		{"ShadowedHandler", "onStart := func(fn func()) { fn() }\nonStart => {\n    |println 1\n}\n", false},
		{"Handler", "onStart => {\n    |println 1\n}\n", true},
		{"NestedOrdinaryCallback", "func invoke(fn func()) { fn() }\nonStart => {\n    invoke => {\n        |println 1\n    }\n}\n", true},
		{"ExplicitReceiver", "this.onStart => {\n    |println 1\n}\n", true},
		{"OverloadedHandler", "onKey KeySpace, => {\n    |println 1\n}\n", true},
		{"FunctionLiteral", "onStart func() {\n    |println 1\n}\n", true},
		{"Argument", "onKey |KeySpace, => {}\n", false},
		{"CallbackParameters", "onKey [KeySpace], |key => { println key }\n", false},
		{"Unresolved", "onMissing => {\n    |println 1\n}\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte(source), "assets/index.json": []byte(`{}`)})
			if tt.name != "Unresolved" {
				requireNoDiagnostics(t, s)
			}
			file, err := s.getProj().ASTFile("main.spx")
			require.NoError(t, err)
			ctx := &definitionContext{proj: s.getProj()}
			assert.Equal(t, tt.want, ctx.isInFrameworkEventHandler(PosAt(s.getProj(), file, position)))
		})
	}
}

func TestAnalyzeFramework(t *testing.T) {
	t.Run("UnregisteredClassfile", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.spx": []byte("println 1\n")})
		result, err := analyzeFramework(s.getProj())
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("UnavailableSDK", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte("println 1\n")})
		proj := s.getProj()
		proj.Importer = completionTestImporter{Importer: proj.Importer, unavailablePath: SpxPkgPath}
		result, err := analyzeFramework(proj)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}
