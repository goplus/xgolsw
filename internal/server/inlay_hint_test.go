package server

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentInlayHint(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var (
	MySprite Sprite
)

onStart => {
	// Function calls with named parameters.
	println "Hello, World!"
	MySprite.turn Left

	// Call with multiple parameters.
	setEffect ColorEffect, 50

	// Function with RGB color value.
	color := RGB(255, 0, 0)
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newMapFSWithoutModTime(m), nil, fileMapGetter(m))

		params := &InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 100, Character: 0},
			},
		}

		inlayHints, err := s.textDocumentInlayHint(params)
		require.NoError(t, err)
		require.NotNil(t, inlayHints)
		assert.NotEmpty(t, inlayHints)

		assert.True(t, slices.ContainsFunc(inlayHints, func(hint InlayHint) bool {
			return hint.Position.Line == 7 && hint.Label != "" && hint.Kind == Parameter
		}))

		assert.True(t, slices.ContainsFunc(inlayHints, func(hint InlayHint) bool {
			return hint.Position.Line == 8 && hint.Label != "" && hint.Kind == Parameter
		}))

		setEffectHintCount := 0
		for _, hint := range inlayHints {
			if hint.Position.Line == 11 && hint.Kind == Parameter {
				setEffectHintCount++
			}
		}
		assert.Equal(t, 2, setEffectHintCount)

		rgbHintCount := 0
		for _, hint := range inlayHints {
			if hint.Position.Line == 14 && hint.Kind == Parameter {
				rgbHintCount++
			}
		}
		assert.Equal(t, 3, rgbHintCount)
	})

	t.Run("EmptyFile", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
		}
		s := New(newMapFSWithoutModTime(m), nil, fileMapGetter(m))

		params := &InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 1, Character: 0},
			},
		}

		inlayHints, err := s.textDocumentInlayHint(params)
		require.NoError(t, err)
		assert.Empty(t, inlayHints)
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		s := New(newMapFSWithoutModTime(map[string][]byte{}), nil, fileMapGetter(map[string][]byte{}))

		params := &InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///nonexistent.spx"},
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 10, Character: 0},
			},
		}

		inlayHints, err := s.textDocumentInlayHint(params)
		require.Error(t, err)
		assert.Nil(t, inlayHints)
	})

	t.Run("SpecificRange", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var (
	MySprite Sprite
)

onStart => {
	// Line 6
	println "Hello"
	// Line 8
	MySprite.turn Left
	// Line 10
	setEffect ColorEffect, 50
	// Line 12
	color := RGB(255, 0, 0)
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newMapFSWithoutModTime(m), nil, fileMapGetter(m))

		params := &InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			Range: Range{
				Start: Position{Line: 7, Character: 0},
				End:   Position{Line: 11, Character: 0},
			},
		}

		inlayHints, err := s.textDocumentInlayHint(params)
		require.NoError(t, err)
		require.NotNil(t, inlayHints)
		assert.Equal(t, 2, len(inlayHints))

		for _, hint := range inlayHints {
			assert.True(t, hint.Position.Line >= 7 && hint.Position.Line <= 11)
		}
		assert.True(t, slices.ContainsFunc(inlayHints, func(hint InlayHint) bool {
			return hint.Position.Line == 7 && hint.Kind == Parameter
		}))
		assert.True(t, slices.ContainsFunc(inlayHints, func(hint InlayHint) bool {
			return hint.Position.Line == 9 && hint.Kind == Parameter
		}))
		assert.False(t, slices.ContainsFunc(inlayHints, func(hint InlayHint) bool {
			return hint.Position.Line < 7 || hint.Position.Line > 11
		}))
	})
}

func TestCollectInlayHints(t *testing.T) {
	t.Run("FunctionCallsWithNamedParams", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var (
	MySprite Sprite
)

onStart => {
	// Regular function calls with parameters.
	println "Hello, World!"
	MySprite.turn Left

	// Function call with multiple parameters.
	setEffect ColorEffect, 50

	// Function call with lambda expression.
	onKey [KeySpace], () => {
		println "Space pressed"
	}

	// Variables with function calls.
	color := RGB(255, 0, 0)
}
`),
			"MySprite.spx": []byte(`
onStart => {
	// Branch statement that converts to call expression.
	goto "OtherSprite"
}
`),
			"OtherSprite.spx":                       []byte(``),
			"assets/index.json":                     []byte(`{}`),
			"assets/sprites/MySprite/index.json":    []byte(`{}`),
			"assets/sprites/OtherSprite/index.json": []byte(`{}`),
		}
		s := New(newMapFSWithoutModTime(m), nil, fileMapGetter(m))

		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		inlayHints := collectInlayHints(result, astFile, 0, 0)
		require.NotNil(t, inlayHints)
		assert.NotEmpty(t, inlayHints)

		assert.True(t, slices.ContainsFunc(inlayHints, func(hint InlayHint) bool {
			return hint.Position.Line == 7 && hint.Label != ""
		}))

		assert.True(t, slices.ContainsFunc(inlayHints, func(hint InlayHint) bool {
			return hint.Position.Line == 8 && hint.Label != ""
		}))

		setEffectHintCount := 0
		for _, hint := range inlayHints {
			if hint.Position.Line == 11 {
				setEffectHintCount++
			}
		}
		assert.Equal(t, 2, setEffectHintCount)

		rgbHintCount := 0
		for _, hint := range inlayHints {
			if hint.Position.Line == 19 {
				rgbHintCount++
			}
		}
		assert.Equal(t, 3, rgbHintCount)

		spriteResult, _, spriteAstFile, err := s.compileAndGetASTFileForDocumentURI("file:///MySprite.spx")
		require.NoError(t, err)
		require.NotNil(t, spriteAstFile)

		spriteInlayHints := collectInlayHints(spriteResult, spriteAstFile, 0, 0)
		require.NotNil(t, spriteInlayHints)
		assert.NotEmpty(t, spriteInlayHints)

		hasGotoHint := false
		for _, hint := range spriteInlayHints {
			if hint.Position.Line == 3 {
				hasGotoHint = true
				break
			}
		}
		assert.True(t, hasGotoHint)
	})

	t.Run("NoInlayHints", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var (
	MySprite Sprite
)

onStart => {
	// No function calls with parameters.
	a := 5
	b := 10
	c := a + b
}
`),
		}
		s := New(newMapFSWithoutModTime(m), nil, fileMapGetter(m))

		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		inlayHints := collectInlayHints(result, astFile, 0, 0)
		assert.Empty(t, inlayHints)
	})

	t.Run("LambdaExpressionSkipped", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	// Function with lambda argument (should be skipped).
	onKey [KeySpace], () => {
		println "Space pressed"
	}
}
`),
		}
		s := New(newMapFSWithoutModTime(m), nil, fileMapGetter(m))

		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		inlayHints := collectInlayHints(result, astFile, 0, 0)
		require.NotNil(t, inlayHints)
		assert.NotEmpty(t, inlayHints)

		onKeyHintCount := 0
		for _, hint := range inlayHints {
			if hint.Position.Line == 4 {
				onKeyHintCount++
			}
		}
		assert.Equal(t, 1, onKeyHintCount)
	})

	t.Run("EmptyFile", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
		}
		s := New(newMapFSWithoutModTime(m), nil, fileMapGetter(m))

		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		inlayHints := collectInlayHints(result, astFile, 0, 0)
		assert.Empty(t, inlayHints)
	})

	t.Run("RangeFiltering", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var (
	MySprite Sprite
)

onStart => {
	// Line 6
	println "Hello"
	// Line 8
	MySprite.turn Left
	// Line 10
	setEffect ColorEffect, 50
	// Line 12
	color := RGB(255, 0, 0)
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newMapFSWithoutModTime(m), nil, fileMapGetter(m))

		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		rangeStart := result.posAt(astFile, Position{Line: 7, Character: 0})
		rangeEnd := result.posAt(astFile, Position{Line: 10, Character: 0})
		filteredHints := collectInlayHints(result, astFile, rangeStart, rangeEnd)
		require.NotNil(t, filteredHints)
		assert.NotEmpty(t, filteredHints)

		allHints := collectInlayHints(result, astFile, 0, 0)
		require.NotNil(t, allHints)
		assert.NotEmpty(t, allHints)

		assert.Less(t, len(filteredHints), len(allHints))

		for _, hint := range filteredHints {
			assert.True(t, hint.Position.Line >= 7 && hint.Position.Line <= 10)
		}
		assert.True(t, slices.ContainsFunc(filteredHints, func(hint InlayHint) bool {
			return hint.Position.Line == 7 && hint.Kind == Parameter
		}))
		assert.True(t, slices.ContainsFunc(filteredHints, func(hint InlayHint) bool {
			return hint.Position.Line == 9 && hint.Kind == Parameter
		}))

		rgbHintCount := 0
		for _, hint := range filteredHints {
			if hint.Position.Line > 10 {
				rgbHintCount++
			}
		}
		assert.Zero(t, rgbHintCount)
	})
}
