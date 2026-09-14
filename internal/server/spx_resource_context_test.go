package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpxSpriteResourceForObject(t *testing.T) {
	s := newTestServer(t, map[string][]byte{
		"main.xgo":                         []byte("var Runner, Other, Missing int\necho Runner, Other, Missing\n"),
		"assets/index.json":                []byte(`{}`),
		"assets/sprites/Runner/index.json": []byte(`{}`),
		"assets/sprites/Other/index.json":  []byte(`{}`),
	})
	proj := s.getProj()
	typeInfo, err := proj.TypeInfo()
	require.NoError(t, err)
	runner := typeInfo.Pkg.Scope().Lookup("Runner")
	other := typeInfo.Pkg.Scope().Lookup("Other")
	missing := typeInfo.Pkg.Scope().Lookup("Missing")
	require.NotNil(t, runner)
	require.NotNil(t, other)
	require.NotNil(t, missing)
	set, err := NewSpxResourceSet(proj)
	require.NoError(t, err)
	result := newCompileResult(proj, s.lookupPkgDoc)
	result.spxResourceSet = *set
	result.spxSpriteResourceAutoBindings[runner] = struct{}{}
	result.spxSpriteResourceAutoBindings[missing] = struct{}{}
	require.NotNil(t, set.Sprite("Runner"))
	assert.Same(t, set.Sprite("Runner"), spxSpriteResourceForObject(result, runner))
	assert.Nil(t, spxSpriteResourceForObject(result, nil))
	assert.Nil(t, spxSpriteResourceForObject(result, other))
	assert.Nil(t, spxSpriteResourceForObject(result, missing))

	otherProj := newTestServer(t, map[string][]byte{"main.xgo": []byte("var Runner int\necho Runner\n")}).getProj()
	otherInfo, err := otherProj.TypeInfo()
	require.NoError(t, err)
	sameName := otherInfo.Pkg.Scope().Lookup("Runner")
	require.NotNil(t, sameName)
	assert.Nil(t, spxSpriteResourceForObject(result, sameName), "auto-bindings match object identity, not just names")
	otherResult := newCompileResult(proj, s.lookupPkgDoc)
	otherResult.spxResourceSet = *set
	assert.Nil(t, spxSpriteResourceForObject(otherResult, runner), "auto-bindings belong to each compile result")
}

func TestCompletionContextGetCurrentFileSpxSpriteResource(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
		want     string
	}{
		{name: "Empty"},
		{name: "MainFile", filename: "project/Stage.spx"},
		{name: "MainFileBasename", filename: "Stage.spx"},
		{name: "Sprite", filename: "Runner.spx", want: "Runner"},
		{name: "SpriteBasename", filename: "project/Runner.spx", want: "Runner"},
		{name: "MissingSprite", filename: "Missing.spx"},
		{name: "OtherExtension", filename: "Runner.xgo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"assets/index.json":                []byte(`{}`),
				"assets/sprites/Runner/index.json": []byte(`{}`),
				"assets/sprites/Stage/index.json":  []byte(`{}`),
			})
			set, err := NewSpxResourceSet(s.getProj())
			require.NoError(t, err)
			result := newCompileResult(s.getProj(), s.lookupPkgDoc)
			result.mainSpxFile = "project/Stage.spx"
			result.spxResourceSet = *set
			ctx := completionContext{filename: tt.filename, spxResult: result}
			if tt.want == "" {
				assert.Nil(t, ctx.getCurrentFileSpxSpriteResource())
			} else {
				require.NotNil(t, set.Sprite(tt.want))
				assert.Same(t, set.Sprite(tt.want), ctx.getCurrentFileSpxSpriteResource())
			}
		})
	}
}
