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
		{"OrdinaryCallback", `func invoke(fn func()) { fn() }
invoke => {
    |println 1
}
`, false},
		{"UserHandler", `func onCustom(fn func()) { fn() }
onCustom => {
    |println 1
}
`, false},
		{"ShadowedHandler", `onStart := func(fn func()) { fn() }
onStart => {
    |println 1
}
`, false},
		{"Handler", `onStart => {
    |println 1
}
`, true},
		{"NestedOrdinaryCallback", `func invoke(fn func()) { fn() }
onStart => {
    invoke => {
        |println 1
    }
}
`, true},
		{"ExplicitReceiver", `this.onStart => {
    |println 1
}
`, true},
		{"OverloadedHandler", `onKey KeySpace, => {
    |println 1
}
`, true},
		{"FunctionLiteral", `onStart func() {
    |println 1
}
`, true},
		{"Argument", "onKey |KeySpace, => {}\n", false},
		{"CallbackParameters", "onKey [KeySpace], |key => { println key }\n", false},
		{"Unresolved", `onMissing => {
    |println 1
}
`, false},
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
