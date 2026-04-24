package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentDocumentLink(t *testing.T) {
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
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		linksForMainSpx, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.Len(t, linksForMainSpx, 6)
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 1, Character: 6},
				End:   Position{Line: 1, Character: 15},
			},
			Target: toURI("xgo:main?Backdrop1"),
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 1, Character: 16},
				End:   Position{Line: 1, Character: 28},
			},
			Target: toURI("xgo:github.com/goplus/spx/v2?BackdropName"),
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 1, Character: 31},
				End:   Position{Line: 1, Character: 42},
			},
			Target: toURI("spx://resources/backdrops/backdrop1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 2, Character: 6},
				End:   Position{Line: 2, Character: 16},
			},
			Target: toURI("xgo:main?Backdrop1a"),
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 2, Character: 19},
				End:   Position{Line: 2, Character: 28},
			},
			Target: toURI("xgo:main?Backdrop1"),
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 2, Character: 19},
				End:   Position{Line: 2, Character: 28},
			},
			Target: toURI("spx://resources/backdrops/backdrop1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindConstantReference,
			},
		})
		linksForMySpriteSpx, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
		})
		require.NoError(t, err)
		require.Len(t, linksForMySpriteSpx, 21)
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 3, Character: 12},
				End:   Position{Line: 3, Character: 23},
			},
			Target: toURI("spx://resources/backdrops/backdrop1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 2, Character: 6},
				End:   Position{Line: 2, Character: 15},
			},
			Target: toURI("spx://resources/sounds/MySound"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 4, Character: 1},
				End:   Position{Line: 4, Character: 9},
			},
			Target: toURI("spx://resources/sprites/MySprite"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindAutoBindingReference,
			},
		})
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 5, Character: 1},
				End:   Position{Line: 5, Character: 9},
			},
			Target: toURI("spx://resources/sprites/MySprite"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindAutoBindingReference,
			},
		})
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 5, Character: 18},
				End:   Position{Line: 5, Character: 25},
			},
			Target: toURI("spx://resources/sprites/MySprite/animations/anim1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 4, Character: 21},
				End:   Position{Line: 4, Character: 31},
			},
			Target: toURI("spx://resources/sprites/MySprite/costumes/costume1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 6, Character: 20},
				End:   Position{Line: 6, Character: 29},
			},
			Target: toURI("spx://resources/widgets/widget1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 7, Character: 29},
				End:   Position{Line: 7, Character: 39},
			},
			Target: toURI("spx://resources/sprites/MySprite"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySpriteSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 8, Character: 14},
				End:   Position{Line: 8, Character: 24},
			},
			Target: toURI("spx://resources/sprites/MySprite"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
	})

	t.Run("KwargDefinitions", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
    Count int
}

type Params interface {
    MaxTokens(n int64) Params
}

type Client struct{}

func (c Client) Params() Params { return nil }

func (c Client) complete(prompt string, params Params?) {}

func configure(opts Options?) {}

var client Client

onStart => {
    configure count = 1
    client.complete "hi", maxTokens = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		assert.Contains(t, links, DocumentLink{
			Range: Range{
				Start: Position{Line: 20, Character: 14},
				End:   Position{Line: 20, Character: 19},
			},
			Target: toURI("xgo:main?Options.Count"),
		})
		assert.Contains(t, links, DocumentLink{
			Range: Range{
				Start: Position{Line: 21, Character: 26},
				End:   Position{Line: 21, Character: 35},
			},
			Target: toURI("xgo:main?interface%7BMaxTokens%28n+int64%29+main.Params%7D.MaxTokens"),
		})
	})

	t.Run("KwargResourceReferences", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
    Sound SoundName
    Sounds []SoundName
    Alias Clip
}

type Clip = SoundName

type Player interface {
    Sound(sound SoundName) Player
}

type Client struct{}

func (c Client) Player() Player { return nil }

func (c Client) play(params Player?) {}

func configure(opts Options?) {}

func configureMap(opts map[string]SoundName?) {}

var client Client

onStart => {
    configure sound = "StructSound"
    configure sounds = ["SliceSound"]
    configure alias = "AliasSound"
    configureMap sound = "MapSound"
    client.play sound = "InterfaceSound"
}
`),
			"assets/index.json":                       []byte(`{}`),
			"assets/sounds/StructSound/index.json":    []byte(`{}`),
			"assets/sounds/SliceSound/index.json":     []byte(`{}`),
			"assets/sounds/AliasSound/index.json":     []byte(`{}`),
			"assets/sounds/MapSound/index.json":       []byte(`{}`),
			"assets/sounds/InterfaceSound/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)

		targets := make([]string, 0, len(links))
		for _, link := range links {
			if link.Target != nil {
				targets = append(targets, string(*link.Target))
			}
		}
		assert.Contains(t, targets, "spx://resources/sounds/StructSound")
		assert.Contains(t, targets, "spx://resources/sounds/SliceSound")
		assert.Contains(t, targets, "spx://resources/sounds/AliasSound")
		assert.Contains(t, targets, "spx://resources/sounds/MapSound")
		assert.Contains(t, targets, "spx://resources/sounds/InterfaceSound")
	})

	t.Run("OverloadKwargResourceReferences", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type Options struct {
	Sound SoundName
}

var worker Worker

func (w *Worker) playSound(opts Options?) {}

func (Worker).play = (
	(Worker).playSound
)

onStart => {
	worker.play sound = "OverloadSound"
}
`),
			"assets/index.json":                          []byte(`{}`),
			"assets/sounds/OverloadSound/index.json":     []byte(`{}`),
			"assets/sounds/UnreferencedSound/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)

		targets := make([]string, 0, len(links))
		for _, link := range links {
			if link.Target != nil {
				targets = append(targets, string(*link.Target))
			}
		}
		assert.Contains(t, targets, "spx://resources/sounds/OverloadSound")
		assert.NotContains(t, targets, "spx://resources/sounds/UnreferencedSound")
	})

	t.Run("NonSpxFile", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`echo "Hello, XGo!"`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		assert.EqualError(t, err, `file "main.xgo" does not have .spx extension`)
		assert.Nil(t, links)
	})

	t.Run("FileNotFound", func(t *testing.T) {
		s := New(newProjectWithoutModTime(map[string][]byte{}), nil, fileMapGetter(map[string][]byte{}), &MockScheduler{})

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///notexist.spx"},
		})
		assert.ErrorIs(t, err, errNoMainSpxFile)
		assert.Nil(t, links)
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
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

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
			Target: toURI("xgo:github.com/goplus/spx/v2?SoundName"),
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

	t.Run("SpxResourceInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getBackdrop() BackdropName {
	return "backdrop1"
}

func getSound() SoundName {
	return "MySound"
}

func getSprite() SpriteName {
	return "MySprite"
}

func getMultipleResourcesMain() (BackdropName, SoundName, SpriteName) {
	return "backdrop1", "MySound", "MySprite"
}

func getMixedTypesMain() (string, BackdropName, error, SoundName) {
	return "hello", "backdrop1", nil, "MySound"
}
`),
			"MySprite.spx": []byte(`
func getCostume() SpriteCostumeName {
	return "costume1"
}

func getAnimation() SpriteAnimationName {
	return "anim1"
}

func getWidget() WidgetName {
	return "widget1"
}

func getMultipleResourcesMySprite() (SpriteCostumeName, SpriteAnimationName, WidgetName) {
	return "costume1", "anim1", "widget1"
}

func getMixedTypesMySprite() (int, SpriteCostumeName, string, SpriteAnimationName) {
	return 42, "costume1", "hello", "anim1"
}
`),
			"assets/index.json":                  []byte(`{"backdrops":[{"name":"backdrop1"}],"zorder":[{"name":"widget1","type":"monitor"}]}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume1"}],"fAnimations":{"anim1":{}}}`),
			"assets/sounds/MySound/index.json":   []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		linksForMainSpx, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 2, Character: 8},
				End:   Position{Line: 2, Character: 19},
			},
			Target: toURI("spx://resources/backdrops/backdrop1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 6, Character: 8},
				End:   Position{Line: 6, Character: 17},
			},
			Target: toURI("spx://resources/sounds/MySound"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 10, Character: 8},
				End:   Position{Line: 10, Character: 18},
			},
			Target: toURI("spx://resources/sprites/MySprite"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 14, Character: 8},
				End:   Position{Line: 14, Character: 19},
			},
			Target: toURI("spx://resources/backdrops/backdrop1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 14, Character: 21},
				End:   Position{Line: 14, Character: 30},
			},
			Target: toURI("spx://resources/sounds/MySound"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 14, Character: 32},
				End:   Position{Line: 14, Character: 42},
			},
			Target: toURI("spx://resources/sprites/MySprite"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 18, Character: 17},
				End:   Position{Line: 18, Character: 28},
			},
			Target: toURI("spx://resources/backdrops/backdrop1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMainSpx, DocumentLink{
			Range: Range{
				Start: Position{Line: 18, Character: 35},
				End:   Position{Line: 18, Character: 44},
			},
			Target: toURI("spx://resources/sounds/MySound"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})

		linksForMySprite, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
		})
		require.NoError(t, err)
		assert.Contains(t, linksForMySprite, DocumentLink{
			Range: Range{
				Start: Position{Line: 2, Character: 8},
				End:   Position{Line: 2, Character: 18},
			},
			Target: toURI("spx://resources/sprites/MySprite/costumes/costume1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySprite, DocumentLink{
			Range: Range{
				Start: Position{Line: 6, Character: 8},
				End:   Position{Line: 6, Character: 15},
			},
			Target: toURI("spx://resources/sprites/MySprite/animations/anim1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySprite, DocumentLink{
			Range: Range{
				Start: Position{Line: 10, Character: 8},
				End:   Position{Line: 10, Character: 17},
			},
			Target: toURI("spx://resources/widgets/widget1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySprite, DocumentLink{
			Range: Range{
				Start: Position{Line: 14, Character: 8},
				End:   Position{Line: 14, Character: 18},
			},
			Target: toURI("spx://resources/sprites/MySprite/costumes/costume1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySprite, DocumentLink{
			Range: Range{
				Start: Position{Line: 14, Character: 20},
				End:   Position{Line: 14, Character: 27},
			},
			Target: toURI("spx://resources/sprites/MySprite/animations/anim1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySprite, DocumentLink{
			Range: Range{
				Start: Position{Line: 14, Character: 29},
				End:   Position{Line: 14, Character: 38},
			},
			Target: toURI("spx://resources/widgets/widget1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySprite, DocumentLink{
			Range: Range{
				Start: Position{Line: 18, Character: 12},
				End:   Position{Line: 18, Character: 22},
			},
			Target: toURI("spx://resources/sprites/MySprite/costumes/costume1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
		assert.Contains(t, linksForMySprite, DocumentLink{
			Range: Range{
				Start: Position{Line: 18, Character: 33},
				End:   Position{Line: 18, Character: 40},
			},
			Target: toURI("spx://resources/sprites/MySprite/animations/anim1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
	})

	t.Run("SpxResourceInKwarg", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Backdrop BackdropName
}

func setup(opts Options) {}

setup backdrop = "backdrop1"
`),
			"assets/index.json": []byte(`{"backdrops":[{"name":"backdrop1"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		assert.Contains(t, links, DocumentLink{
			Range: Range{
				Start: Position{Line: 7, Character: 17},
				End:   Position{Line: 7, Character: 28},
			},
			Target: toURI("spx://resources/backdrops/backdrop1"),
			Data: SpxResourceRefDocumentLinkData{
				Kind: SpxResourceRefKindStringLiteral,
			},
		})
	})

	t.Run("BlankIdentifier", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx":          []byte(`type`),
			"assets/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.Empty(t, links)
	})

	t.Run("BlankIdent", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
const _ = 1
`),
			"assets/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.Empty(t, links)
	})

	t.Run("InvalidSpxResourceReferencesFiltered", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
const ValidBackdrop BackdropName = "backdrop1"
const MissingBackdrop BackdropName = "missingBackdrop"
`),
			"MySprite.spx": []byte(`
onStart => {
	play "MissingSound"
	onBackdrop "backdrop1", func() {}
	MySprite.setCostume "missingCostume"
	MySprite.animate "missingAnim"
	getWidget Monitor, "missingWidget"
	var missingSprite SpriteName = "MissingSprite"
}
`),
			"assets/index.json":                  []byte(`{"backdrops":[{"name":"backdrop1"}],"zorder":[]}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[],"fAnimations":{}}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		collectTargets := func(links []DocumentLink) []string {
			t.Helper()
			targets := make([]string, 0, len(links))
			for _, link := range links {
				if link.Target == nil {
					continue
				}
				targets = append(targets, string(*link.Target))
			}
			return targets
		}

		linksForMainSpx, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		targetsForMainSpx := collectTargets(linksForMainSpx)
		assert.Contains(t, targetsForMainSpx, "spx://resources/backdrops/backdrop1")
		assert.NotContains(t, targetsForMainSpx, "spx://resources/backdrops/missingBackdrop")

		linksForMySpriteSpx, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
		})
		require.NoError(t, err)
		targetsForMySpriteSpx := collectTargets(linksForMySpriteSpx)
		assert.Contains(t, targetsForMySpriteSpx, "spx://resources/backdrops/backdrop1")
		assert.NotContains(t, targetsForMySpriteSpx, "spx://resources/sounds/MissingSound")
		assert.NotContains(t, targetsForMySpriteSpx, "spx://resources/sprites/MissingSprite")
		assert.NotContains(t, targetsForMySpriteSpx, "spx://resources/sprites/MySprite/costumes/missingCostume")
		assert.NotContains(t, targetsForMySpriteSpx, "spx://resources/sprites/MySprite/animations/missingAnim")
		assert.NotContains(t, targetsForMySpriteSpx, "spx://resources/widgets/missingWidget")
	})
}

func TestSortDocumentLinks(t *testing.T) {
	t.Run("NilTargetSorting", func(t *testing.T) {
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 10, Character: 5}}, Target: nil},
			{Range: Range{Start: Position{Line: 5, Character: 10}}, Target: toURI("spx://resources/target1")},
			{Range: Range{Start: Position{Line: 20, Character: 15}}, Target: nil},
			{Range: Range{Start: Position{Line: 15, Character: 20}}, Target: toURI("spx://resources/target2")},
		}

		sortDocumentLinks(links)

		// Links with nil targets should come first.
		require.Len(t, links, 4)
		assert.Nil(t, links[0].Target)
		assert.Nil(t, links[1].Target)
		assert.NotNil(t, links[2].Target)
		assert.NotNil(t, links[3].Target)

		// Nil targets should be sorted by line number.
		assert.Equal(t, uint32(10), links[0].Range.Start.Line)
		assert.Equal(t, uint32(20), links[1].Range.Start.Line)
	})

	t.Run("TargetURISorting", func(t *testing.T) {
		targetA := toURI("spx://resources/A")
		targetB := toURI("spx://resources/B")
		targetC := toURI("spx://resources/C")
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 10, Character: 5}}, Target: targetC},
			{Range: Range{Start: Position{Line: 5, Character: 10}}, Target: targetA},
			{Range: Range{Start: Position{Line: 15, Character: 15}}, Target: targetB},
		}

		sortDocumentLinks(links)

		// Links with targets should be sorted by target URI string.
		require.Len(t, links, 3)
		assert.Equal(t, targetA, links[0].Target)
		assert.Equal(t, targetB, links[1].Target)
		assert.Equal(t, targetC, links[2].Target)
	})

	t.Run("LineNumberSorting", func(t *testing.T) {
		target := toURI("spx://resources/same-target")
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 30, Character: 5}}, Target: target},
			{Range: Range{Start: Position{Line: 10, Character: 10}}, Target: target},
			{Range: Range{Start: Position{Line: 20, Character: 15}}, Target: target},
		}

		sortDocumentLinks(links)

		// Same target URI should be sorted by line number.
		require.Len(t, links, 3)
		assert.Equal(t, uint32(10), links[0].Range.Start.Line)
		assert.Equal(t, uint32(20), links[1].Range.Start.Line)
		assert.Equal(t, uint32(30), links[2].Range.Start.Line)
	})

	t.Run("CharacterPositionSorting", func(t *testing.T) {
		target := toURI("spx://resources/same-target")
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 10, Character: 25}}, Target: target},
			{Range: Range{Start: Position{Line: 10, Character: 5}}, Target: target},
			{Range: Range{Start: Position{Line: 10, Character: 15}}, Target: target},
		}

		sortDocumentLinks(links)

		// Same target URI and line should be sorted by character position.
		require.Len(t, links, 3)
		assert.Equal(t, uint32(5), links[0].Range.Start.Character)
		assert.Equal(t, uint32(15), links[1].Range.Start.Character)
		assert.Equal(t, uint32(25), links[2].Range.Start.Character)
	})

	t.Run("ComplexSorting", func(t *testing.T) {
		targetA := toURI("spx://resources/A")
		targetB := toURI("spx://resources/B")
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 5, Character: 10}}, Target: nil},
			{Range: Range{Start: Position{Line: 5, Character: 20}}, Target: targetB},
			{Range: Range{Start: Position{Line: 10, Character: 5}}, Target: targetA},
			{Range: Range{Start: Position{Line: 5, Character: 5}}, Target: targetA},
			{Range: Range{Start: Position{Line: 5, Character: 15}}, Target: targetA},
			{Range: Range{Start: Position{Line: 1, Character: 10}}, Target: nil},
		}

		sortDocumentLinks(links)

		// Nil targets should come first, sorted by line number.
		require.Len(t, links, 6)
		assert.Nil(t, links[0].Target)
		assert.Nil(t, links[1].Target)
		assert.Equal(t, uint32(1), links[0].Range.Start.Line)
		assert.Equal(t, uint32(5), links[1].Range.Start.Line)

		// Then links sorted by target URI.
		assert.Equal(t, targetA, links[2].Target)
		assert.Equal(t, targetA, links[3].Target)
		assert.Equal(t, targetA, links[4].Target)
		assert.Equal(t, targetB, links[5].Target)

		// Same target URIs should be sorted by line number.
		assert.Equal(t, uint32(5), links[2].Range.Start.Line)
		assert.Equal(t, uint32(5), links[3].Range.Start.Line)
		assert.Equal(t, uint32(10), links[4].Range.Start.Line)

		// Same target URI and line should be sorted by character position.
		assert.Equal(t, uint32(5), links[2].Range.Start.Character)
		assert.Equal(t, uint32(15), links[3].Range.Start.Character)
	})
}
