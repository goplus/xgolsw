package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentFormattingSpx(t *testing.T) {
	t.Run("WithUnusedLambdaParamsForSprite", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": {},
			"MySprite.spx": []byte(`// An spx game.
onKey [KeyLeft, KeyRight], (key) => {
	println "key"
}
onTouchStart "MySprite", (s) => {
	println "touched", s
}
onTouchStart "MySprite", (s) => {}
onTouchStart (s, t) => { // type mismatch
}
onTouchStart 123, (s) => { // type mismatch
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 12, Character: 0},
			},
			NewText: `// An spx game.
onKey [KeyLeft, KeyRight], () => {
	println "key"
}
onTouchStart "MySprite", (s) => {
	println "touched", s
}
onTouchStart "MySprite", () => {
}
onTouchStart (s, t) => { // type mismatch
}
onTouchStart 123, (s) => { // type mismatch
}
`,
		}, edits[0])
	})
}
