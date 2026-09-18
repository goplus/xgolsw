package server

import (
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
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
	result := newSpxAnalysis(proj)
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
	otherResult := newSpxAnalysis(proj)
	otherResult.spxResourceSet = *set
	assert.Nil(t, spxSpriteResourceForObject(otherResult, runner), "auto-bindings belong to each project analysis")
}

func TestSpxSpriteResourceForFile(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
		want     string
	}{
		{name: "Empty"},
		{name: "MainFile", filename: "main.spx"},
		{name: "MissingSource", filename: "project/Runner.spx"},
		{name: "Sprite", filename: "Runner.spx", want: "Runner"},
		{name: "NormalizedClassName", filename: "Red-Cat.spx", want: "Red_Cat"},
		{name: "MissingSprite", filename: "Missing.spx"},
		{name: "OtherExtension", filename: "Runner.xgo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newSpxTestServer(t, map[string][]byte{
				"main.spx": nil, "Runner.spx": nil, "Red-Cat.spx": nil,
				"assets/sprites/Red_Cat/index.json": []byte(`{}`),
				"assets/sprites/Red-Cat/index.json": []byte(`{}`),
				"assets/index.json":                 []byte(`{}`),
				"assets/sprites/Runner/index.json":  []byte(`{}`),
				"assets/sprites/Stage/index.json":   []byte(`{}`),
			})
			set, err := NewSpxResourceSet(s.getProj())
			require.NoError(t, err)
			result := newSpxAnalysis(s.getProj())
			result.spxResourceSet = *set
			if tt.want == "" {
				assert.Nil(t, spxSpriteResourceForFile(result, tt.filename))
			} else {
				require.NotNil(t, set.Sprite(tt.want))
				assert.Same(t, set.Sprite(tt.want), spxSpriteResourceForFile(result, tt.filename))
			}
		})
	}
}

func TestSpxSpriteResourceForCall(t *testing.T) {
	for _, name := range []string{"NoPos", "UnregisteredPosition"} {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("echo 1\n")})
			proj := s.getProj()
			call := resourceTestCall(t, proj, "main.xgo")
			pos := token.NoPos
			if name == "UnregisteredPosition" {
				pos = token.Pos(proj.Fset.Base())
			}
			require.Nil(t, proj.Fset.File(pos))
			requireValueAs[*ast.Ident](t, call.Fun).NamePos = pos
			result := newSpxAnalysis(proj)
			var got *SpxSpriteResource
			require.NotPanics(t, func() {
				got = spxSpriteResourceForCall(result, call)
			})
			assert.Nil(t, got)
		})
	}
}

func TestInferSpxSpriteResourceEnclosingNode(t *testing.T) {
	t.Run("ExplicitReceiver", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			want   string
		}{
			{"AutoBinding", "Runner.use \"value\"\n", "Runner"},
			{"UnboundObject", "Other.use \"value\"\n", ""},
			{"ShadowedAutoBinding", "func run() {\n\tRunner := Other\n\tRunner.use \"value\"\n}\n", ""},
			{"ReceiverExpression", "(&Runner).Use(\"value\")\n", ""},
			{"FunctionExpression", "(Runner.Use)(\"value\")\n", ""},
			{"LineDirective", "//line virtual.xgo:100:20\nRunner.use \"value\"\n", "Runner"},
			{"NoCall", "const Name = \"value\"\n", ""},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo":                         []byte("type Actor struct {}\nfunc (a *Actor) Use(name string) {}\nvar Runner, Other Actor\n" + tt.source),
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{}`),
					"assets/sprites/Other/index.json":  []byte(`{}`),
				})
				proj := s.getProj()
				info, err := proj.TypeInfo()
				require.NoError(t, err)
				set, err := NewSpxResourceSet(proj)
				require.NoError(t, err)
				result := newSpxAnalysis(proj)
				result.spxResourceSet = *set
				runner := info.Pkg.Scope().Lookup("Runner")
				require.NotNil(t, runner)
				result.spxSpriteResourceAutoBindings[runner] = struct{}{}
				file, err := proj.ASTFile("main.xgo")
				require.NoError(t, err)
				literal := inputSlotLiteral(t, newInputSlotContext(proj, file), `"value"`)
				got := inferSpxSpriteResourceEnclosingNode(result, literal)
				if tt.want == "" {
					assert.Nil(t, got)
				} else {
					require.NotNil(t, set.Sprite(tt.want))
					assert.Same(t, set.Sprite(tt.want), got)
				}
			})
		}
	})

	t.Run("ImplicitReceiver", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			want     string
		}{
			{"Project", "main.spx", "func use(name string) {}\nuse \"value\"\n", ""},
			{"Class", "Runner.spx", "func use(name string) {}\nuse \"value\"\n", "Runner"},
			{"Callback", "Runner.spx", "func use(name string) {}\nonStart => {\n\tuse \"value\"\n}\n", "Runner"},
			{"LineDirective", "Runner.spx", "func use(name string) {}\n//line virtual.spx:100:20\nuse \"value\"\n", "Runner"},
			{"FuncDecorator", "Runner.spx", "func withResource(name string, fn func()) {}\n@withResource(\"value\")\nfunc run() {}\n", "Runner"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx": nil, "Runner.spx": nil,
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{}`),
					"assets/sprites/main/index.json":   []byte(`{}`),
				}
				files[tt.filename] = []byte(tt.source)
				s := newSpxTestServer(t, files)
				proj := s.getProj()
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				set, err := NewSpxResourceSet(proj)
				require.NoError(t, err)
				result := newSpxAnalysis(proj)
				result.mainSpxFile = "main.spx"
				result.spxResourceSet = *set
				file, err := proj.ASTFile(tt.filename)
				require.NoError(t, err)
				literal := inputSlotLiteral(t, newInputSlotContext(proj, file), `"value"`)
				got := inferSpxSpriteResourceEnclosingNode(result, literal)
				if tt.want == "" {
					assert.Nil(t, got)
				} else {
					require.NotNil(t, set.Sprite(tt.want))
					assert.Same(t, set.Sprite(tt.want), got)
				}
			})
		}
	})

	t.Run("SourceChanges", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                         nil,
			"Runner.spx":                       []byte("func use(name string) {}\nuse \"value\"\n"),
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{}`),
		})
		proj := s.getProj()
		call := resourceTestCall(t, proj, "Runner.spx")
		set, err := NewSpxResourceSet(proj)
		require.NoError(t, err)
		result := newSpxAnalysis(proj)
		result.mainSpxFile = "main.spx"
		result.spxResourceSet = *set
		require.NotNil(t, set.Sprite("Runner"))
		assert.Same(t, set.Sprite("Runner"), inferSpxSpriteResourceEnclosingNode(result, call))
		s.ModifyFiles([]FileChange{{Path: "Runner.spx", Content: []byte("func use(name string) {}\nuse \"other\"\n"), Version: 1}})
		assert.Nil(t, inferSpxSpriteResourceEnclosingNode(result, call))
	})
}
