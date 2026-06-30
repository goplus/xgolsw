package server

import (
	gotypes "go/types"
	"slices"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// semanticTokenPositionImporter returns an imported variable at a chosen position.
type semanticTokenPositionImporter struct {
	fallback gotypes.Importer
	pos      token.Pos
}

// Import returns a package variable at a position from a separate file set.
func (i semanticTokenPositionImporter) Import(path string) (*gotypes.Package, error) {
	if path != "example.com/dep" {
		return i.fallback.Import(path)
	}
	pkg := gotypes.NewPackage(path, "dep")
	pkg.Scope().Insert(gotypes.NewVar(i.pos, pkg, "Value", gotypes.Typ[gotypes.Int]))
	pkg.MarkComplete()
	return pkg, nil
}

func TestServerTextDocumentSemanticTokensFull(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.turn Left
`),
			"MySprite.spx": []byte(`
onStart => {
	MySprite.turn Right
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		mainSpxTokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, mainSpxTokens)
		assert.Equal(t, []uint32{
			1, 0, 1, 13, 0, // {
			0, 0, 8, 6, 0, // MySprite
			0, 8, 1, 13, 0, // .
			0, 1, 4, 8, 0, // turn
			0, 5, 4, 5, 6, // Left
			0, 4, 1, 13, 0, // }
		}, mainSpxTokens.Data)

		mySpriteTokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteTokens)
		assert.Equal(t, []uint32{
			1, 0, 1, 13, 0, //{
			0, 0, 7, 8, 0, // onStart
			0, 8, 2, 13, 0, // =>
			0, 3, 1, 13, 0, // {
			1, 1, 8, 6, 0, // MySprite
			0, 8, 1, 13, 0, // .
			0, 1, 4, 8, 0, // turn
			0, 5, 5, 5, 6, // Right
			1, 0, 1, 13, 0, // }
			0, 1, 1, 13, 0, // }
		}, mySpriteTokens.Data)
	})

	t.Run("KwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

onStart => {
	configure count = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
			line:      8,
			character: 11,
			length:    5,
			tokenType: PropertyType,
		})
	})

	t.Run("XGoUnit", func(t *testing.T) {
		s := newXGoUnitTestServer(`import "time"

func wait(d time.Duration) {}

onStart => {
	wait 1m
}
`)

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
			line:      5,
			character: 6,
			length:    1,
			tokenType: NumberType,
		})
	})

	t.Run("UTF16Positions", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var 中文 []int
	中文 = append(中文, 1)
	println "非英文", 中文
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		decodedTokens := decodeSemanticTokens(tokens.Data)
		assert.Contains(t, decodedTokens, decodedSemanticToken{
			line:      4,
			character: 9,
			length:    5,
			tokenType: StringType,
		})
		assert.Contains(t, decodedTokens, decodedSemanticToken{
			line:      4,
			character: 16,
			length:    2,
			tokenType: VariableType,
		})
	})

	t.Run("MultilineInterpolatedString", func(t *testing.T) {
		s := newXGoUnitTestServer(`
onStart => {
	name := "world"
	println ` + "`" + `hello
${name}
done` + "`" + `
}
`)

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		decodedTokens := decodeSemanticTokens(tokens.Data)
		for _, want := range []decodedSemanticToken{
			{line: 4, character: 0, length: 2, tokenType: StringType},
			{line: 4, character: 2, length: 4, tokenType: VariableType},
			{line: 4, character: 6, length: 1, tokenType: StringType},
			{line: 5, character: 0, length: 5, tokenType: StringType},
		} {
			assert.Contains(t, decodedTokens, want)
		}
		assertNoOverlappingSemanticToken(t, decodedTokens, decodedSemanticToken{
			line:      4,
			character: 2,
			length:    4,
			tokenType: StringType,
		})
	})

	t.Run("FuncDecorator", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`func retry(fn func()) {
	fn()
}

@retry
func run() {}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		decoded := decodeSemanticTokens(tokens.Data)
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      4,
			character: 0,
			length:    1,
			tokenType: OperatorType,
		})
	})

	t.Run("ImportedPositionCollision", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`import "example.com/dep"

var (
	x = dep.Value
)
`),
		}
		proj := newProjectWithoutModTime(m)
		astPkg, err := proj.ASTPackage()
		require.NoError(t, err)

		var referencePos token.Pos
		ast.Inspect(astPkg.Files["main.spx"], func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "Value" {
				referencePos = selector.Sel.Pos()
			}
			return true
		})
		require.True(t, referencePos.IsValid())

		foreignFset := token.NewFileSet()
		foreignFile := foreignFset.AddFile("dep.go", -1, int(referencePos))
		foreignPos := foreignFile.Pos(int(referencePos) - 1)
		require.Equal(t, referencePos, foreignPos)

		s := New(proj, nil, fileMapGetter(m), &MockScheduler{})
		s.workspaceRootFS.Importer = semanticTokenPositionImporter{
			fallback: s.workspaceRootFS.Importer,
			pos:      foreignPos,
		}
		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		want := decodedSemanticToken{line: 3, character: 9, length: 5, tokenType: VariableType}
		assert.Contains(t, decodeSemanticTokens(tokens.Data), want)
		assertSemanticTokenModifierMask(t, tokens.Data, want, 0)
	})
}

func TestSemanticTokenSegments(t *testing.T) {
	for _, tt := range []struct {
		name           string
		start          Position
		end            Position
		lineLengths    []uint32
		fallbackLength uint32
		want           []semanticTokenSegment
	}{
		{
			name:        "SingleLine",
			start:       Position{Line: 2, Character: 4},
			end:         Position{Line: 2, Character: 9},
			lineLengths: []uint32{0, 0, 12},
			want: []semanticTokenSegment{
				{line: 2, char: 4, length: 5},
			},
		},
		{
			name:        "MultiLine",
			start:       Position{Line: 1, Character: 3},
			end:         Position{Line: 3, Character: 4},
			lineLengths: []uint32{0, 10, 5, 8},
			want: []semanticTokenSegment{
				{line: 1, char: 3, length: 7},
				{line: 2, length: 5},
				{line: 3, length: 4},
			},
		},
		{
			name:        "StartCharacterPastLineEnd",
			start:       Position{Line: 1, Character: 10},
			end:         Position{Line: 2, Character: 3},
			lineLengths: []uint32{0, 8, 5},
			want: []semanticTokenSegment{
				{line: 2, length: 3},
			},
		},
		{
			name:        "EndCharacterAtLineStart",
			start:       Position{Line: 1, Character: 3},
			end:         Position{Line: 3, Character: 0},
			lineLengths: []uint32{0, 10, 5, 8},
			want: []semanticTokenSegment{
				{line: 1, char: 3, length: 7},
				{line: 2, length: 5},
			},
		},
		{
			name:           "SyntheticSpan",
			start:          Position{Line: 0, Character: 5},
			end:            Position{Line: 0, Character: 5},
			lineLengths:    []uint32{10},
			fallbackLength: 1,
			want: []semanticTokenSegment{
				{line: 0, char: 5, length: 1},
			},
		},
		{
			name:           "AllSegmentsEmptyFallback",
			start:          Position{Line: 1, Character: 10},
			end:            Position{Line: 2, Character: 0},
			lineLengths:    []uint32{0, 10, 0},
			fallbackLength: 2,
			want: []semanticTokenSegment{
				{line: 1, char: 10, length: 2},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, semanticTokenSegments(tt.start, tt.end, tt.lineLengths, tt.fallbackLength))
		})
	}
}

func TestSemanticTokenLineLengths(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content []byte
		want    []uint32
	}{
		{
			name:    "LF",
			content: []byte("abc\ndef\n中文"),
			want:    []uint32{3, 3, 2},
		},
		{
			name:    "CRLF",
			content: []byte("abc\r\ndef\r\n中文"),
			want:    []uint32{3, 3, 2},
		},
		{
			name:    "TrailingCRLF",
			content: []byte("abc\r\n"),
			want:    []uint32{3, 0},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, semanticTokenLineLengths(tt.content))
		})
	}
}

func TestSemanticTokenFallbackLength(t *testing.T) {
	for _, tt := range []struct {
		name        string
		content     []byte
		startOffset int
		endOffset   int
		want        uint32
	}{
		{
			name:        "UTF16SourceSpan",
			content:     []byte("中文"),
			startOffset: 0,
			endOffset:   len("中文"),
			want:        2,
		},
		{
			name:        "SyntheticSpanOutsideSource",
			content:     nil,
			startOffset: 0,
			endOffset:   1,
			want:        1,
		},
		{
			name:        "InvalidRange",
			content:     []byte("abc"),
			startOffset: 2,
			endOffset:   2,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, semanticTokenFallbackLength(tt.content, tt.startOffset, tt.endOffset))
		})
	}
}

type decodedSemanticToken struct {
	line      uint32
	character uint32
	length    uint32
	tokenType SemanticTokenTypes
}

func decodeSemanticTokens(data []uint32) []decodedSemanticToken {
	if len(data)%5 != 0 {
		return nil
	}

	var (
		line      uint32
		character uint32
		tokens    []decodedSemanticToken
	)
	for tokenData := range slices.Chunk(data, 5) {
		line += tokenData[0]
		if tokenData[0] == 0 {
			character += tokenData[1]
		} else {
			character = tokenData[1]
		}

		tokenTypeIndex := tokenData[3]
		if int(tokenTypeIndex) >= len(semanticTokenTypesLegend) {
			continue
		}
		tokens = append(tokens, decodedSemanticToken{
			line:      line,
			character: character,
			length:    tokenData[2],
			tokenType: semanticTokenTypesLegend[tokenTypeIndex],
		})
	}
	return tokens
}

func assertSemanticTokenModifierMask(
	t *testing.T,
	data []uint32,
	want decodedSemanticToken,
	wantMask uint32,
) {
	t.Helper()

	var line, character uint32
	for tokenData := range slices.Chunk(data, 5) {
		line += tokenData[0]
		if tokenData[0] == 0 {
			character += tokenData[1]
		} else {
			character = tokenData[1]
		}
		if line != want.line ||
			character != want.character ||
			tokenData[2] != want.length ||
			semanticTokenTypesLegend[tokenData[3]] != want.tokenType {
			continue
		}
		assert.Equal(t, wantMask, tokenData[4])
		return
	}
	assert.Failf(t, "semantic token not found", "%#v", want)
}

func assertNoOverlappingSemanticToken(t *testing.T, tokens []decodedSemanticToken, forbidden decodedSemanticToken) {
	t.Helper()

	forbiddenEnd := forbidden.character + forbidden.length
	for _, token := range tokens {
		if token.line != forbidden.line || token.tokenType != forbidden.tokenType {
			continue
		}
		tokenEnd := token.character + token.length
		assert.Falsef(t, token.character < forbiddenEnd && forbidden.character < tokenEnd, "unexpected overlapping token: %#v", token)
	}
}
