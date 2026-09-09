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
	for _, tt := range []struct {
		name     string
		filename string
	}{
		{name: "XGo", filename: "main.xgo"},
		{name: "Gop", filename: "main.gop"},
		{name: "NormalClass", filename: "Record.gox"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"functions.xgo": []byte("func consume(value int) {}\n"),
				tt.filename:     []byte("consume 1\n"),
			})
			tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
			})
			require.NoError(t, err)
			require.NotNil(t, tokens)
			assert.Equal(t, []uint32{
				0, 0, 1, 13, 0, // {
				0, 0, 7, 7, 0, // consume
				0, 8, 1, 12, 0, // 1
				0, 1, 1, 13, 0, // }
			}, tokens.Data)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	for _, tt := range []struct {
		name     string
		filename string
		want     []uint32
	}{
		{
			name:     "ProjectMethod",
			filename: "main_fixture.gox",
			want: []uint32{
				1, 0, 1, 13, 0, // {
				0, 0, 6, 6, 0, // Worker
				0, 6, 1, 13, 0, // .
				0, 1, 5, 8, 0, // apply
				0, 6, 3, 5, 6, // Low
				0, 3, 1, 13, 0, // }
			},
		},
		{
			name:     "WorkCallback",
			filename: "Worker_fixture.gox",
			want: []uint32{
				1, 0, 1, 13, 0, // {
				0, 0, 7, 8, 0, // onValue
				0, 8, 6, 4, 1, // amount
				0, 7, 2, 13, 0, // =>
				0, 3, 1, 13, 0, // {
				1, 1, 6, 6, 0, // Worker
				0, 6, 1, 13, 0, // .
				0, 1, 5, 8, 0, // apply
				0, 6, 6, 5, 0, // amount
				1, 0, 1, 13, 0, // }
				0, 1, 1, 13, 0, // }
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main_fixture.gox":   []byte("\nWorker.apply Low\n"),
				"Worker_fixture.gox": []byte("\nonValue amount => {\n\tWorker.apply amount\n}\n"),
			})
			tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
			})
			require.NoError(t, err)
			require.NotNil(t, tokens)
			assert.Equal(t, tt.want, tokens.Data)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	for _, tt := range []struct {
		name     string
		filename string
		source   string
	}{
		{
			name:     "ProjectField",
			filename: "main_fixture.gox",
			source:   "var count int\nonStart => {\n\tcount = 1\n}\n",
		},
		{
			name:     "WorkField",
			filename: "Worker_fixture.gox",
			source:   "var count int\nonValue amount => {\n\tcount = amount\n}\n",
		},
		{
			name:     "NormalClassField",
			filename: "Record.gox",
			source:   "var count int\nfunc run() {\n\tcount = 1\n}\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{"main_fixture.gox": []byte("\n")}
			files[tt.filename] = []byte(tt.source)
			s := newTestServer(t, files)
			tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
			})
			require.NoError(t, err)
			require.NotNil(t, tokens)
			declaration := decodedSemanticToken{line: 0, character: 4, length: 5, tokenType: VariableType}
			reference := decodedSemanticToken{line: 2, character: 1, length: 5, tokenType: VariableType}
			assertSemanticTokenModifierMask(t, tokens.Data, declaration, getSemanticTokenModifiersMask([]SemanticTokenModifiers{ModDeclaration}))
			assertSemanticTokenModifierMask(t, tokens.Data, reference, 0)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("VariableKinds", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("var count int\nfunc use(value int) {\n\tlocal := value\n\tcount = local\n}\nuse count\n"),
		})
		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		declarationMask := getSemanticTokenModifiersMask([]SemanticTokenModifiers{ModDeclaration})
		for _, tt := range []struct {
			token decodedSemanticToken
			mask  uint32
		}{
			{decodedSemanticToken{line: 0, character: 4, length: 5, tokenType: VariableType}, declarationMask},
			{decodedSemanticToken{line: 1, character: 9, length: 5, tokenType: ParameterType}, declarationMask},
			{decodedSemanticToken{line: 2, character: 1, length: 5, tokenType: VariableType}, declarationMask},
			{decodedSemanticToken{line: 2, character: 10, length: 5, tokenType: VariableType}, 0},
			{decodedSemanticToken{line: 3, character: 1, length: 5, tokenType: VariableType}, 0},
			{decodedSemanticToken{line: 3, character: 9, length: 5, tokenType: VariableType}, 0},
			{decodedSemanticToken{line: 5, character: 4, length: 5, tokenType: VariableType}, 0},
		} {
			assertSemanticTokenModifierMask(t, tokens.Data, tt.token, tt.mask)
		}
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"enums.xgo": []byte("type Color const (\n\tRed = iota\n)\n"),
			"main.xgo":  []byte("var color Color = Red\n"),
		})
		params := &SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}
		tokens, err := s.textDocumentSemanticTokensFull(params)
		require.NoError(t, err)
		require.NotNil(t, tokens)
		assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
			line: 0, character: 10, length: 5, tokenType: EnumType,
		})
		member := decodedSemanticToken{line: 0, character: 18, length: 3, tokenType: EnumMemberType}
		mask := getSemanticTokenModifiersMask([]SemanticTokenModifiers{ModStatic, ModReadonly})
		assertSemanticTokenModifierMask(t, tokens.Data, member, mask)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		s.ModifyFiles([]FileChange{
			{Path: "enums.xgo", Content: []byte("type Color int\nconst Red Color = 1\nfunc broken() { missing() }\n"), Version: 1},
			{Path: "main.xgo", Content: []byte("\nvar color Color = Red\n"), Version: 1},
		})
		tokens, err = s.textDocumentSemanticTokensFull(params)
		require.NoError(t, err)
		require.NotNil(t, tokens)
		decoded := decodeSemanticTokens(tokens.Data)
		assert.Contains(t, decoded, decodedSemanticToken{
			line: 1, character: 10, length: 5, tokenType: TypeType,
		})
		constant := decodedSemanticToken{line: 1, character: 18, length: 3, tokenType: VariableType}
		assertSemanticTokenModifierMask(t, tokens.Data, constant, mask)
		for _, token := range decoded {
			assert.Equal(t, uint32(1), token.line)
			assert.NotEqual(t, EnumType, token.tokenType)
			assert.NotEqual(t, EnumMemberType, token.tokenType)
		}
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	t.Run("IncompleteSource", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("var count int\ncount = 1\nmissing(\n"),
		})
		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		decoded := decodeSemanticTokens(tokens.Data)
		for _, want := range []decodedSemanticToken{
			{line: 0, character: 4, length: 5, tokenType: VariableType},
			{line: 1, character: 0, length: 5, tokenType: VariableType},
			{line: 1, character: 8, length: 1, tokenType: NumberType},
		} {
			assert.Contains(t, decoded, want)
		}
		_, err = s.workspaceRootFS.ASTPackage()
		assert.Error(t, err)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	for _, tt := range []struct {
		name         string
		uri          DocumentURI
		source       string
		wantError    bool
		wantASTError bool
		want         *SemanticTokens
	}{
		{name: "EmptyFile", uri: "file:///main.xgo", want: &SemanticTokens{Data: []uint32{}}},
		{name: "MissingFile", uri: "file:///missing.xgo"},
		{name: "NonSourceFile", uri: "file:///notes.txt"},
		{name: "InvalidURI", uri: "https://example.com/main.xgo", wantError: true},
		{name: "StartWithInvalidChar", uri: "file:///main.xgo", source: "\n\u201c\u201dvar (\n    maps []int\n)\n", wantASTError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo":  []byte(tt.source),
				"notes.txt": []byte("var count int\n"),
			})
			tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
				TextDocument: TextDocumentIdentifier{URI: tt.uri},
			})
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, tokens)
			_, err = s.workspaceRootFS.ASTPackage()
			if tt.wantASTError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("KwargField", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func main() {
	configure count = 1
}
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
			line:      8,
			character: 11,
			length:    5,
			tokenType: PropertyType,
		})
	})

	t.Run("XGoUnit", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`import "time"

func wait(d time.Duration) {}

func main() {
	wait 1m
}
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
			line:      5,
			character: 6,
			length:    1,
			tokenType: NumberType,
		})
	})

	t.Run("UTF16Positions", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("\nfunc main() {\n\tvar \u4e2d\u6587 []int\n\t\u4e2d\u6587 = append(\u4e2d\u6587, 1)\n\tprintln \"\u975e\u82f1\u6587\", \u4e2d\u6587\n}\n"),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

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

	t.Run("UTF16Encoding", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("var caf\u00e9 = \"\U0001f600\"\n"),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		decoded := decodeSemanticTokens(tokens.Data)
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      0,
			character: 4,
			length:    4,
			tokenType: VariableType,
		})
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      0,
			character: 11,
			length:    4,
			tokenType: StringType,
		})
	})

	t.Run("UTF16FollowingArgument", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("println \"\U0001F600\", 1\n"),
		})
		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		decoded := decodeSemanticTokens(tokens.Data)
		assert.Contains(t, decoded, decodedSemanticToken{
			line: 0, character: 8, length: 4, tokenType: StringType,
		})
		assert.Contains(t, decoded, decodedSemanticToken{
			line: 0, character: 14, length: 1, tokenType: NumberType,
		})
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("MultilineInterpolatedString", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func main() {
	name := "world"
	println ` + "`" + `hello
${name}
done` + "`" + `
}
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

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

	t.Run("EnumAndFuncDecorator", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func retry(fn func()) {
	fn()
}

@retry
func run() {}

type Color const (
	Red = iota
)
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		decoded := decodeSemanticTokens(tokens.Data)
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      4,
			character: 0,
			length:    1,
			tokenType: OperatorType,
		})
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      7,
			character: 11,
			length:    5,
			tokenType: KeywordType,
		})
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      7,
			character: 5,
			length:    5,
			tokenType: EnumType,
		})
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      8,
			character: 1,
			length:    3,
			tokenType: EnumMemberType,
		})
	})

	t.Run("EnumBlankIdentifier", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Color const (
	_ = iota
	Red
)
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		decoded := decodeSemanticTokens(tokens.Data)
		assert.NotContains(t, decoded, decodedSemanticToken{
			line:      1,
			character: 1,
			length:    1,
			tokenType: EnumMemberType,
		})
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      2,
			character: 1,
			length:    3,
			tokenType: EnumMemberType,
		})
	})

	t.Run("EnumMemberSharedWithRegularConstant", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`const (
	Shared = 1
)

type Color const (
	Shared = 1
)

func run() {
	println(Shared)
	var color Color = (Shared)
}
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		decoded := decodeSemanticTokens(tokens.Data)
		declarationMask := getSemanticTokenModifiersMask([]SemanticTokenModifiers{
			ModDeclaration,
			ModStatic,
			ModReadonly,
		})
		referenceMask := getSemanticTokenModifiersMask([]SemanticTokenModifiers{ModStatic, ModReadonly})
		for _, want := range []struct {
			token decodedSemanticToken
			mask  uint32
		}{
			{decodedSemanticToken{line: 1, character: 1, length: 6, tokenType: VariableType}, declarationMask},
			{decodedSemanticToken{line: 5, character: 1, length: 6, tokenType: EnumMemberType}, declarationMask},
			{decodedSemanticToken{line: 9, character: 9, length: 6, tokenType: VariableType}, referenceMask},
			{decodedSemanticToken{line: 10, character: 20, length: 6, tokenType: EnumMemberType}, referenceMask},
		} {
			assert.Contains(t, decoded, want.token)
			assertSemanticTokenModifierMask(t, tokens.Data, want.token, want.mask)
		}
	})

	t.Run("CrossFileEnumReference", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
		}{
			{name: "XGo", filename: "main.xgo"},
			{name: "Project", filename: "main_fixture.gox"},
			{name: "Work", filename: "Worker_fixture.gox"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main_fixture.gox": []byte("\n"),
					"enums.xgo": []byte(`type Color const (
	Red = iota
)
`),
				}
				files[tt.filename] = []byte("var color Color = Red\n")
				s := newTestServer(t, files)

				tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				})
				require.NoError(t, err)
				require.NotNil(t, tokens)
				_, err = s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)

				decoded := decodeSemanticTokens(tokens.Data)
				assert.Contains(t, decoded, decodedSemanticToken{
					line:      0,
					character: 10,
					length:    5,
					tokenType: EnumType,
				})
				assert.Contains(t, decoded, decodedSemanticToken{
					line:      0,
					character: 18,
					length:    3,
					tokenType: EnumMemberType,
				})
			})
		}
	})

	t.Run("EnumAlias", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Shade = Color

var color Shade = Red
`),
			"enums.xgo": []byte(`type Color const (
	Red = iota
)
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		decoded := decodeSemanticTokens(tokens.Data)
		for _, want := range []decodedSemanticToken{
			{line: 0, character: 5, length: 5, tokenType: EnumType},
			{line: 0, character: 13, length: 5, tokenType: EnumType},
			{line: 2, character: 10, length: 5, tokenType: EnumType},
			{line: 2, character: 18, length: 3, tokenType: EnumMemberType},
		} {
			assert.Contains(t, decoded, want)
		}
	})

	t.Run("EnumPointerAlias", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Color const (
	Red = iota
)

type ColorPtr = *Color
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		decoded := decodeSemanticTokens(tokens.Data)
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      4,
			character: 5,
			length:    8,
			tokenType: TypeType,
		})
		assert.NotContains(t, decoded, decodedSemanticToken{
			line:      4,
			character: 5,
			length:    8,
			tokenType: EnumType,
		})
		assert.Contains(t, decoded, decodedSemanticToken{
			line:      4,
			character: 17,
			length:    5,
			tokenType: EnumType,
		})
	})

	t.Run("LocalEnum", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func run() {
	type Color const (
		Red = iota
	)
	var color Color = Red
}
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		decoded := decodeSemanticTokens(tokens.Data)
		for _, want := range []decodedSemanticToken{
			{line: 1, character: 6, length: 5, tokenType: EnumType},
			{line: 2, character: 2, length: 3, tokenType: EnumMemberType},
			{line: 4, character: 11, length: 5, tokenType: EnumType},
			{line: 4, character: 19, length: 3, tokenType: EnumMemberType},
		} {
			assert.Contains(t, decoded, want)
		}
		for _, unwanted := range []decodedSemanticToken{
			{line: 1, character: 6, length: 5, tokenType: TypeType},
			{line: 2, character: 2, length: 3, tokenType: VariableType},
			{line: 4, character: 11, length: 5, tokenType: TypeType},
			{line: 4, character: 19, length: 3, tokenType: VariableType},
		} {
			assert.NotContains(t, decoded, unwanted)
		}
	})

	t.Run("DuplicateEnumMembers", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type First const (
	Unknown = iota
)

type Second const (
	Unknown = iota
)

var (
	first First = Unknown
	second Second = Unknown
)
`),
		})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		decoded := decodeSemanticTokens(tokens.Data)
		declarationMask := getSemanticTokenModifiersMask([]SemanticTokenModifiers{
			ModDeclaration,
			ModStatic,
			ModReadonly,
		})
		referenceMask := getSemanticTokenModifiersMask([]SemanticTokenModifiers{ModStatic, ModReadonly})
		for _, want := range []struct {
			token decodedSemanticToken
			mask  uint32
		}{
			{decodedSemanticToken{line: 1, character: 1, length: 7, tokenType: EnumMemberType}, declarationMask},
			{decodedSemanticToken{line: 5, character: 1, length: 7, tokenType: EnumMemberType}, declarationMask},
			{decodedSemanticToken{line: 9, character: 15, length: 7, tokenType: EnumMemberType}, referenceMask},
			{decodedSemanticToken{line: 10, character: 17, length: 7, tokenType: EnumMemberType}, referenceMask},
		} {
			assert.Contains(t, decoded, want.token)
			assertSemanticTokenModifierMask(t, tokens.Data, want.token, want.mask)
		}
	})

	t.Run("ImportedPositionCollision", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`import "example.com/dep"

var (
	x = dep.Value
)
`),
		})
		proj := s.workspaceRootFS
		astPkg, err := proj.ASTPackage()
		require.NoError(t, err)

		var referencePos token.Pos
		ast.Inspect(astPkg.Files["main.xgo"], func(node ast.Node) bool {
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

		s.workspaceRootFS.Importer = semanticTokenPositionImporter{
			fallback: s.workspaceRootFS.Importer,
			pos:      foreignPos,
		}
		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

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
			content: []byte("abc\ndef\n\u4e2d\u6587"),
			want:    []uint32{3, 3, 2},
		},
		{
			name:    "CRLF",
			content: []byte("abc\r\ndef\r\n\u4e2d\u6587"),
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
			content:     []byte("\u4e2d\u6587"),
			startOffset: 0,
			endOffset:   len("\u4e2d\u6587"),
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
