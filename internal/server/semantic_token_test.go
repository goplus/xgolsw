package server

import (
	gotypes "go/types"
	"slices"
	"strings"
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
	t.Run("IncompleteImport", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
		}{
			{name: "Keyword", source: "import"},
			{name: "Newline", source: "import\n"},
			{name: "Alias", source: "import alias\n"},
			{name: "InvalidPath", source: "import 123\n"},
			{name: "Group", source: "import (\nalias\n)\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				params := &SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}
				for version, source := range []string{tt.source, "import _ \"fmt\"\n", tt.source} {
					s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(source), Version: version + 1}})
					_, parseErr := s.requestProject().ASTFile("main.xgo")
					if version == 1 {
						require.NoError(t, parseErr)
					} else {
						require.Error(t, parseErr)
					}
					tokens, err := s.textDocumentSemanticTokensFull(params)
					require.NoError(t, err)
					require.NotNil(t, tokens)
					decoded := decodeSemanticTokens(tokens.Data)
					assert.Contains(t, decoded, decodedSemanticToken{length: 6, tokenType: KeywordType})
					var literals []decodedSemanticToken
					for _, token := range decoded {
						assert.Positive(t, token.length)
						if token.tokenType == StringType {
							literals = append(literals, token)
						}
					}
					if version == 1 {
						assert.Equal(t, []decodedSemanticToken{{character: 9, length: 5, tokenType: StringType}}, literals)
					} else {
						assert.Empty(t, literals)
					}
				}
			})
		}
	})

	t.Run("SourceSpans", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			source       string
			want         []decodedSemanticToken
			declarations []decodedSemanticToken
		}{
			{
				name: "LocalTypeDeclaration", source: "func run() {\ntype Item int\nvar value Item\necho value\n}\n",
				declarations: []decodedSemanticToken{{line: 1, character: 5, length: 4, tokenType: TypeType}},
			},
			{
				name: "FloatingComments", source: "func run() {\n// Note.\nprintln 1 // Tail.\n}\n",
				want: []decodedSemanticToken{{line: 1, length: 8, tokenType: CommentType}, {line: 2, character: 10, length: 8, tokenType: CommentType}},
			},
			{
				name: "CRLFComment", source: "/* First\r\nSecond */\r\npackage main\r\nvar value = 1\r\n",
				want: []decodedSemanticToken{{length: 8, tokenType: CommentType}, {line: 1, length: 9, tokenType: CommentType}},
			},
			{
				name: "HashComment", source: "println 1 # Note.\nprintln 2\n",
				want: []decodedSemanticToken{{character: 10, length: 7, tokenType: CommentType}, {line: 1, character: 8, length: 1, tokenType: NumberType}},
			},
			{
				name: "RawSelector", source: "println `a\r\nb`.len\n",
				want: []decodedSemanticToken{{line: 1, character: 2, length: 1, tokenType: OperatorType}},
			},
			{
				name: "ErrorDefault", source: "func read() (int, error) { return 1, nil }\nprintln read()? /* : */ : 0\n",
				want: []decodedSemanticToken{{line: 1, character: 24, length: 1, tokenType: OperatorType}},
			},
			{
				name: "RangeAssignment", source: "var values [1]int\nfor values[func() int {\nfor range 1 {}\nreturn 0\n}()] = range []int{1} {}\n",
				want: []decodedSemanticToken{{line: 4, character: 7, length: 5, tokenType: KeywordType}},
			},
			{
				name: "Interpolation", source: "println \"${1 + 2} ${len(make(chan int))}\"\n",
				want: []decodedSemanticToken{{character: 13, length: 1, tokenType: OperatorType}, {character: 29, length: 4, tokenType: KeywordType}},
			},
			{
				name: "InterpolationComment", source: "println `${len(\"x\") /* + */ + 1}`\n",
				want: []decodedSemanticToken{{character: 20, length: 7, tokenType: CommentType}, {character: 28, length: 1, tokenType: OperatorType}},
			},
			{
				name: "LineDirective", source: "//line virtual.xgo:100:20\nvar value = 1\n",
				want: []decodedSemanticToken{{line: 1, length: 3, tokenType: KeywordType}, {line: 1, character: 10, length: 1, tokenType: OperatorType}},
			},
			{
				name: "OperatorDeclaration", source: "type Item int\nfunc (value Item) + (other Item) Item { return value }\n",
				want:         []decodedSemanticToken{{line: 1, character: 18, length: 1, tokenType: OperatorType}},
				declarations: []decodedSemanticToken{{line: 1, character: 18, length: 1, tokenType: OperatorType}},
			},
			{
				name: "Import", source: "import _ \"fmt\"\n",
				want: []decodedSemanticToken{{character: 9, length: 5, tokenType: StringType}},
			},
			{
				name: "RawStringCRLF", source: "var text = `\U0001f600\r\nb`\n",
				want: []decodedSemanticToken{{character: 11, length: 3, tokenType: StringType}, {line: 1, length: 2, tokenType: StringType}},
			},
			{
				name: "ShadowEntry", source: "println 1\n",
				want: []decodedSemanticToken{{character: 8, length: 1, tokenType: NumberType}},
			},
			{
				name: "RangeVariables", source: "for _, value := range []int{1} {\nprintln value\n}\n",
				want: []decodedSemanticToken{{character: 16, length: 5, tokenType: KeywordType}},
			},
			{
				name: "RangeSpaces", source: "for     range []int{1} {}\n",
				want: []decodedSemanticToken{{character: 8, length: 5, tokenType: KeywordType}},
			},
			{
				name: "RangeComment", source: "for /* range */ range []int{1} {}\n",
				want: []decodedSemanticToken{{character: 16, length: 5, tokenType: KeywordType}},
			},
			{
				name: "LambdaSpaces", source: "func run(callback func(int)) {}\nrun (value)   =>   { println value }\n",
				want: []decodedSemanticToken{{line: 1, character: 10, length: 1, tokenType: OperatorType}},
			},
			{
				name: "ArrowComments", source: "func run(callback func(int) int) {}\nrun (value) /* ) */ => /* ( */ (value + 1)\n",
				want: []decodedSemanticToken{{line: 1, character: 10, length: 1, tokenType: OperatorType}, {line: 1, character: 31, length: 1, tokenType: OperatorType}},
			},
			{
				name: "SelectorSpaces", source: "type Item struct { Value int }\nvar item Item\nprintln item. /* . */ Value\n",
				want: []decodedSemanticToken{{line: 2, character: 12, length: 1, tokenType: OperatorType}},
			},
			{
				name: "FunctionLiteral", source: "var run = func() {}\n",
				want: []decodedSemanticToken{{character: 10, length: 4, tokenType: KeywordType}},
			},
			{
				name: "ForPhrase", source: "for value <- [1, 2] { println value }\n",
				want: []decodedSemanticToken{{length: 3, tokenType: KeywordType}},
			},
			{
				name: "TypeAssertionSpaces", source: "var value any = 1\nprintln value. /* . */ ( int )\n",
				want: []decodedSemanticToken{{line: 1, character: 13, length: 1, tokenType: OperatorType}},
			},
			{
				name: "TypeSwitchSpaces", source: "var value any = 1\nswitch value.( /* type */ type ) { case int: }\n",
				want: []decodedSemanticToken{{line: 1, character: 26, length: 4, tokenType: KeywordType}},
			},
			{
				name: "ReceiveChannel", source: "var values <-chan int\n",
				want: []decodedSemanticToken{{character: 13, length: 4, tokenType: KeywordType}},
			},
			{
				name: "SliceSpaces", source: "var values [ /* ] */ ]int\n",
				want: []decodedSemanticToken{{character: 21, length: 1, tokenType: OperatorType}},
			},
			{
				name: "ExplicitReceiver", source: "type Item struct{}\nfunc (item *Item) run() {}\n",
				want: []decodedSemanticToken{{line: 1, character: 5, length: 1, tokenType: OperatorType}},
			},
			{
				name: "Label", source: "start:\nfor {\nbreak start\n}\n",
				want: []decodedSemanticToken{{length: 5, tokenType: LabelType}},
			},
			{
				name: "Comprehension", source: "var values = {value: value for value <- [1, 2]}\n",
				want: []decodedSemanticToken{{character: 19, length: 1, tokenType: OperatorType}},
			},
			{
				name: "Overload", source: "func consumeInt(int) {}\nfunc consumeString(string) {}\nfunc consume = (consumeInt, consumeString)\n",
				want: []decodedSemanticToken{{line: 2, character: 5, length: 7, tokenType: FunctionType}},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				params := &SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}
				tokens, err := s.textDocumentSemanticTokensFull(params)
				require.NoError(t, err)
				require.NotNil(t, tokens)
				_, err = s.requestProject().TypeInfo()
				require.NoError(t, err)
				decoded := decodeSemanticTokens(tokens.Data)
				for _, want := range tt.want {
					assert.Contains(t, decoded, want)
				}
				for _, declaration := range tt.declarations {
					assertSemanticTokenModifierMask(t, tokens.Data, declaration, getSemanticTokenModifiersMask([]SemanticTokenModifiers{ModDeclaration}))
				}
				lines := strings.Split(tt.source, "\n")
				for i, current := range decoded {
					require.Less(t, int(current.line), len(lines))
					assert.LessOrEqual(t, current.character+current.length, uint32(UTF16Len(strings.TrimSuffix(lines[current.line], "\r"))))
					if i > 0 && decoded[i-1].line == current.line {
						assert.GreaterOrEqual(t, current.character, decoded[i-1].character+decoded[i-1].length, "overlapping tokens: %v", decoded)
					}
				}
				cached, err := s.textDocumentSemanticTokensFull(params)
				require.NoError(t, err)
				assert.Equal(t, tokens, cached)
			})
		}
	})

	t.Run("UnterminatedComment", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("var value = 1\n/* unfinished\r\ncomment"),
		})
		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		decoded := decodeSemanticTokens(tokens.Data)
		assert.Contains(t, decoded, decodedSemanticToken{line: 1, length: 13, tokenType: CommentType})
		assert.Contains(t, decoded, decodedSemanticToken{line: 2, length: 7, tokenType: CommentType})
		_, err = s.requestProject().ASTPackage()
		assert.ErrorContains(t, err, "comment not terminated")
	})

	t.Run("ImplicitReceiver", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			filename  string
			newServer testServerFactory
		}{
			{"NormalClass", "Record.gox", newTestServer},
			{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
			{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte("func run() {\n\t_ = this\n}\n")}
				if tt.filename == "Worker_fixture.gox" {
					files["main_fixture.gox"] = nil
				}
				s := tt.newServer(t, files)
				tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				})
				require.NoError(t, err)
				require.NotNil(t, tokens)
				_, err = s.getProj().TypeInfo()
				require.NoError(t, err)
				decoded := decodeSemanticTokens(tokens.Data)
				assert.Contains(t, decoded, decodedSemanticToken{line: 1, character: 5, length: 4, tokenType: VariableType})
				for _, tokenType := range []SemanticTokenTypes{ParameterType, StructType, OperatorType} {
					assertNoOverlappingSemanticToken(t, decoded, decodedSemanticToken{line: 0, length: 4, tokenType: tokenType})
				}
			})
		}
	})

	t.Run("ExplicitReceiver", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("type Item struct{}\nfunc (item *Item) run() {\n\t_ = item\n}\n"),
		})
		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)
		_, err = s.getProj().TypeInfo()
		require.NoError(t, err)
		decoded := decodeSemanticTokens(tokens.Data)
		for _, want := range []decodedSemanticToken{
			{line: 1, character: 6, length: 4, tokenType: ParameterType},
			{line: 1, character: 11, length: 1, tokenType: OperatorType},
			{line: 1, character: 12, length: 4, tokenType: StructType},
			{line: 2, character: 5, length: 4, tokenType: VariableType},
		} {
			assert.Contains(t, decoded, want)
		}
	})

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
				0, 0, 7, 7, 0, // consume
				0, 8, 1, 12, 0, // 1
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
				1, 0, 6, 6, 0, // Worker
				0, 6, 1, 13, 0, // .
				0, 1, 5, 8, 0, // apply
				0, 6, 3, 5, 6, // Low
			},
		},
		{
			name:     "WorkCallback",
			filename: "Worker_fixture.gox",
			want: []uint32{
				1, 0, 7, 8, 0, // onValue
				0, 8, 6, 4, 1, // amount
				0, 7, 2, 13, 0, // =>
				0, 3, 1, 13, 0, // {
				1, 1, 6, 6, 0, // Worker
				0, 6, 1, 13, 0, // .
				0, 1, 5, 8, 0, // apply
				0, 6, 6, 5, 0, // amount
				1, 0, 1, 13, 0, // }
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newFrameworkTestServer(t, map[string][]byte{
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
		name        string
		filename    string
		source      string
		projectFile string
		newServer   testServerFactory
	}{
		{
			name:      "ProjectField",
			filename:  "main_fixture.gox",
			source:    "var count int\nonStart => {\n\tcount = 1\n}\n",
			newServer: newFrameworkTestServer,
		},
		{
			name:        "WorkField",
			filename:    "Worker_fixture.gox",
			source:      "var count int\nonValue amount => {\n\tcount = amount\n}\n",
			projectFile: "main_fixture.gox",
			newServer:   newFrameworkTestServer,
		},
		{
			name:      "NormalClassField",
			filename:  "Record.gox",
			source:    "var count int\nfunc run() {\n\tcount = 1\n}\n",
			newServer: newTestServer,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{tt.filename: []byte(tt.source)}
			if tt.projectFile != "" {
				files[tt.projectFile] = nil
			}
			s := tt.newServer(t, files)
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
			name        string
			filename    string
			projectFile string
			newServer   testServerFactory
		}{
			{name: "XGo", filename: "main.xgo", newServer: newTestServer},
			{name: "Project", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
			{name: "Work", filename: "Worker_fixture.gox", projectFile: "main_fixture.gox", newServer: newFrameworkTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"enums.xgo": []byte(`type Color const (
	Red = iota
)
`),
				}
				files[tt.filename] = []byte("var color Color = Red\n")
				if tt.projectFile != "" {
					files[tt.projectFile] = nil
				}
				s := tt.newServer(t, files)

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
		name        string
		start       Position
		end         Position
		lineLengths []uint32
		want        []semanticTokenSegment
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
			name:        "EmptySpan",
			start:       Position{Line: 0, Character: 5},
			end:         Position{Line: 0, Character: 5},
			lineLengths: []uint32{10},
		},
		{
			name:        "AllSegmentsEmpty",
			start:       Position{Line: 1, Character: 10},
			end:         Position{Line: 2, Character: 0},
			lineLengths: []uint32{0, 10, 0},
			want:        []semanticTokenSegment{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, semanticTokenSegments(tt.start, tt.end, tt.lineLengths))
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
