//go:build !test_no_pkgdata

package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentDocumentLinkSpx(t *testing.T) {
	t.Run("LargeList", func(t *testing.T) {
		files := spxDocumentLinkLargeListFiles(20_001)
		s := newSpxTestServer(t, files)
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		_, err = s.getProj().TypeInfo()
		require.NoError(t, err)
		targets := documentLinkTargets(t, links)
		assert.Contains(t, targets, "xgo:main?Game.large")
		assert.Contains(t, targets, "spx://resources/sounds/KnownSound")
	})

	t.Run("UnresolvedCallKeepsResourceReferences", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx": []byte(`func resource() SoundName { return "KnownSound" }
func broken() { missing "UnusedSound" }
`),
			"assets/index.json":                    []byte(`{}`),
			"assets/sounds/KnownSound/index.json":  []byte(`{}`),
			"assets/sounds/UnusedSound/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		_, err := s.workspaceRootFS.TypeInfo()
		require.ErrorContains(t, err, "undefined: missing")
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		targets := documentLinkTargets(t, links)
		assert.Contains(t, targets, "spx://resources/sounds/KnownSound")
		assert.NotContains(t, targets, "spx://resources/sounds/UnusedSound")
	})

	t.Run("MissingMainFile", func(t *testing.T) {
		files := map[string][]byte{"Worker.spx": []byte("var Count = 1\n")}
		s := newSpxTestServer(t, files)
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///Worker.spx"},
		})
		require.NoError(t, err)
		assert.Equal(t, []DocumentLink{{
			Range: Range{Start: Position{Character: 4}, End: Position{Character: 9}}, Target: toURI("xgo:main?Worker.Count"),
		}}, links)
	})

	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
const Backdrop1 BackdropName = "backdrop1"
const Backdrop1a = Backdrop1
`),
			"MySprite.spx": []byte(`
onStart => {
	play "MySound"
	onBackdrop "backdrop1", func() {}
	MySprite.setCostume "costume1"
	MySprite.animate "anim1"
	getWidget Monitor, "widget1"
	var spriteName SpriteName = "MySprite"
	spriteName = "MySprite"
}
`),
			"assets/index.json":                  []byte(`{"backdrops":[{"name":"backdrop1"}],"zorder":[{"name":"widget1","type":"monitor"}]}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume1"}],"fAnimations":{"anim1":{}}}`),
			"assets/sounds/MySound/index.json":   []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		for _, tt := range []struct {
			filename  string
			wantCount int
			want      []DocumentLink
		}{
			{
				filename: "main.spx", wantCount: 6,
				want: []DocumentLink{
					{Range: Range{Start: Position{Line: 1, Character: 6}, End: Position{Line: 1, Character: 15}},
						Target: toURI("xgo:main?Backdrop1")},
					{Range: Range{Start: Position{Line: 1, Character: 16}, End: Position{Line: 1, Character: 28}},
						Target: toURI("xgo:github.com/goplus/spx/v3?BackdropName")},
					{Range: Range{Start: Position{Line: 1, Character: 31}, End: Position{Line: 1, Character: 42}},
						Target: toURI("spx://resources/backdrops/backdrop1"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral}},
					{Range: Range{Start: Position{Line: 2, Character: 6}, End: Position{Line: 2, Character: 16}},
						Target: toURI("xgo:main?Backdrop1a")},
					{Range: Range{Start: Position{Line: 2, Character: 19}, End: Position{Line: 2, Character: 28}},
						Target: toURI("xgo:main?Backdrop1")},
					{Range: Range{Start: Position{Line: 2, Character: 19}, End: Position{Line: 2, Character: 28}},
						Target: toURI("spx://resources/backdrops/backdrop1"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindConstantReference}},
				},
			},
			{
				filename: "MySprite.spx", wantCount: 21,
				want: []DocumentLink{
					{Range: Range{Start: Position{Line: 3, Character: 12}, End: Position{Line: 3, Character: 23}},
						Target: toURI("spx://resources/backdrops/backdrop1"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral}},
					{Range: Range{Start: Position{Line: 2, Character: 6}, End: Position{Line: 2, Character: 15}},
						Target: toURI("spx://resources/sounds/MySound"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral}},
					{Range: Range{Start: Position{Line: 4, Character: 1}, End: Position{Line: 4, Character: 9}},
						Target: toURI("spx://resources/sprites/MySprite"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindAutoBindingReference}},
					{Range: Range{Start: Position{Line: 5, Character: 1}, End: Position{Line: 5, Character: 9}},
						Target: toURI("spx://resources/sprites/MySprite"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindAutoBindingReference}},
					{Range: Range{Start: Position{Line: 5, Character: 18}, End: Position{Line: 5, Character: 25}},
						Target: toURI("spx://resources/sprites/MySprite/animations/anim1"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral}},
					{Range: Range{Start: Position{Line: 4, Character: 21}, End: Position{Line: 4, Character: 31}},
						Target: toURI("spx://resources/sprites/MySprite/costumes/costume1"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral}},
					{Range: Range{Start: Position{Line: 6, Character: 20}, End: Position{Line: 6, Character: 29}},
						Target: toURI("spx://resources/widgets/widget1"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral}},
					{Range: Range{Start: Position{Line: 7, Character: 29}, End: Position{Line: 7, Character: 39}},
						Target: toURI("spx://resources/sprites/MySprite"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral}},
					{Range: Range{Start: Position{Line: 8, Character: 14}, End: Position{Line: 8, Character: 24}},
						Target: toURI("spx://resources/sprites/MySprite"), Data: SpxResourceRefDocumentLinkData{Kind: SpxResourceRefKindStringLiteral}},
				},
			},
		} {
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
				TextDocument: TextDocumentIdentifier{URI: DocumentURI("file:///" + tt.filename)},
			})
			require.NoError(t, err)
			require.Len(t, links, tt.wantCount)
			for _, link := range tt.want {
				assert.Contains(t, links, link, tt.filename)
			}
		}
	})

	t.Run("ParseError", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
// Invalid syntax
const (
	MySound SoundName = "MySound"
`),
			"assets/index.json":                []byte(`{}`),
			"assets/sounds/MySound/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.Len(t, links, 3)
		assert.Contains(t, links, DocumentLink{
			Range: Range{
				Start: Position{Line: 3, Character: 1},
				End:   Position{Line: 3, Character: 8},
			},
			Target: toURI("xgo:main?MySound"),
		})
		assert.Contains(t, links, DocumentLink{
			Range: Range{
				Start: Position{Line: 3, Character: 9},
				End:   Position{Line: 3, Character: 18},
			},
			Target: toURI("xgo:github.com/goplus/spx/v3?SoundName"),
		})
		assert.Contains(t, links, DocumentLink{
			Range: Range{
				Start: Position{Line: 3, Character: 21},
				End:   Position{Line: 3, Character: 30},
			},
			Target: toURI("spx://resources/sounds/MySound"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
	})

	t.Run("MissingResource", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":          []byte("play \"MissingSound\"\n"),
			"assets/index.json": []byte(`{}`),
		})
		result, err := s.compile()
		require.NoError(t, err)
		require.Len(t, result.spxResourceRefs, 1)
		assert.Equal(t, SpxSoundResourceID{"MissingSound"}, result.spxResourceRefs[0].ID)
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotEmpty(t, links)
		assert.NotContains(t, documentLinkTargets(t, links), "spx://resources/sounds/MissingSound")
	})
}

func documentLinkTargets(t testing.TB, links []DocumentLink) []string {
	t.Helper()

	targets := make([]string, 0, len(links))
	for _, link := range links {
		if link.Target != nil {
			targets = append(targets, string(*link.Target))
		}
	}
	return targets
}

func BenchmarkServerDocumentLinkWithLargeListSpx(b *testing.B) {
	files := spxDocumentLinkLargeListFiles(20_001)
	server := newSpxTestServer(b, files)
	params := &DocumentLinkParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
	}
	links, err := server.textDocumentDocumentLink(params)
	require.NoError(b, err)
	_, err = server.getProj().TypeInfo()
	require.NoError(b, err)
	require.Contains(b, documentLinkTargets(b, links), "spx://resources/sounds/KnownSound")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := server.textDocumentDocumentLink(params)
		require.NoError(b, err)
	}
}

func spxDocumentLinkLargeListFiles(elementCount int) map[string][]byte {
	mainSpx := `var large List = NewList("value"` + strings.Repeat(`, "value"`, elementCount-1) + ")\n" +
		"play \"KnownSound\"\n"
	return map[string][]byte{
		"main.spx":                            []byte(mainSpx),
		"assets/index.json":                   []byte(`{}`),
		"assets/sounds/KnownSound/index.json": []byte(`{}`),
	}
}
