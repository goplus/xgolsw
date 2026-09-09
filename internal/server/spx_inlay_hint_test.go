package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectInlayHintsSpx(t *testing.T) {
	for _, tt := range []struct {
		name      string
		arguments string
		want      []InlayHint
	}{
		{name: "SpxStepToWithAmbiguousArgument", arguments: "1,"},
		{
			name:      "SpxStepToWithPositionArguments",
			arguments: "1, 2",
			want: []InlayHint{
				{Position: Position{Line: 2, Character: 12}, Label: "x", Kind: Parameter},
				{Position: Position{Line: 2, Character: 15}, Label: "y", Kind: Parameter},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{
				"main.spx":                           {},
				"MySprite.spx":                       []byte("\nonStart => {\n\tstepToWith " + tt.arguments + "\n}\n"),
				"assets/index.json":                  []byte(`{}`),
				"assets/sprites/MySprite/index.json": []byte(`{}`),
			}
			s := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})

			result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///MySprite.spx")
			require.NoError(t, err)
			require.NotNil(t, astFile)

			inlayHints := collectInlayHints(result.proj, astFile, 0, 0)
			require.Len(t, inlayHints, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want, inlayHints[i])
			}
		})
	}
}
