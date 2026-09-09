package server

import (
	"bytes"
	gotypes "go/types"
	"io/fs"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/format"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentFormatting(t *testing.T) {
	t.Run("SourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			want     string
		}{
			{
				name: "XGo", filename: "main.xgo",
				source: "var first=1\nvar second=2\ntype Count int\necho first,second\n",
				want:   "var first = 1\nvar second = 2\n\ntype Count int\n\necho first, second\n",
			},
			{
				name: "LegacyXGo", filename: "main.gop",
				source: "var first=1\nvar second=2\ntype Count int\necho first,second\n",
				want:   "var first = 1\nvar second = 2\n\ntype Count int\n\necho first, second\n",
			},
			{
				name: "StandaloneClass", filename: "Record.gox",
				source: "var count Count\ntype Count int\n",
				want:   "type Count int\n\nvar (\n\tcount Count\n)\n",
			},
			{
				name: "ProjectClass", filename: "main_fixture.gox",
				source: "var count Count\ntype Count int\n",
				want:   "type Count int\n\nvar (\n\tcount Count\n)\n",
			},
			{
				name: "WorkClass", filename: "Worker_fixture.gox",
				source: "var count Count\ntype Count int\n",
				want:   "type Count int\n\nvar (\n\tcount Count\n)\n",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{tt.filename: []byte(tt.source)})
				params := &DocumentFormattingParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				}
				edits, err := s.textDocumentFormatting(params)
				require.NoError(t, err)
				require.Len(t, edits, 1)
				assert.Equal(t, tt.want, edits[0].NewText)

				s.ModifyFiles([]FileChange{{Path: tt.filename, Content: []byte(edits[0].NewText), Version: 1}})
				edits, err = s.textDocumentFormatting(params)
				require.NoError(t, err)
				assert.Empty(t, edits)
			})
		}
	})

	t.Run("ClassfileRegistration", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.unit": []byte("var count Count\ntype Count int\n")})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.unit"},
		}
		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		assert.Nil(t, edits)

		mod := s.workspaceRootFS.Mod
		class, ok := mod.LookupClass("_fixture.gox")
		require.True(t, ok)
		class.Ext = ".unit"
		class.FullExt = "main.unit"
		class.Works[0].Ext = ".unit"
		require.NoError(t, mod.ImportClasses())
		edits, err = s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, "type Count int\n\nvar (\n\tcount Count\n)\n", edits[0].NewText)
	})

	t.Run("AutoLambda", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte("onStart {echo \"ready\"}\nonEvent \"value\", (value)=>{echo \"value\"}\n"),
		})
		class, ok := s.workspaceRootFS.Mod.LookupClass("_fixture.gox")
		require.True(t, ok)
		class.AutoLambdas = map[string]int{"onStart": 0}
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}
		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, "onStart {\n\techo \"ready\"\n}\nonEvent \"value\", () => {\n\techo \"value\"\n}\n", edits[0].NewText)

		s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(edits[0].NewText), Version: 1}})
		edits, err = s.textDocumentFormatting(params)
		require.NoError(t, err)
		assert.Empty(t, edits)
	})

	t.Run("FileUpdates", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte("onStart => {\n\techo \"old\"\n}\n"),
		})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		s.ModifyFiles([]FileChange{
			{Path: "main_fixture.gox", Content: []byte("var limit int = 2\n"), Version: 1},
			{Path: "Worker_fixture.gox", Content: []byte("onEvent \"value\", (value)=>{echo limit}\nonValue (value)=>{echo \"ready\"}\n"), Version: 1},
		})
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"},
		}
		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, "onEvent \"value\", () => {\n\techo limit\n}\nonValue (value) => {\n\techo \"ready\"\n}\n", edits[0].NewText)
	})

	t.Run("FileSetIsolation", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			want   string
		}{
			{
				name:   "NoChangesNeeded",
				source: "onStart => {\n\techo \"ready\"\n}\n",
				want:   "onStart => {\n\techo \"ready\"\n}\n",
			},
			{
				name:   "WithChanges",
				source: "var count int\nonEvent \"value\", (value)=>{echo count}\n",
				want:   "var (\n\tcount int\n)\n\nonEvent \"value\", () => {\n\techo count\n}\n",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main_fixture.gox":   []byte(tt.source),
					"Worker_fixture.gox": []byte("var count int\n"),
				})
				proj := s.workspaceRootFS
				// Populate the importer before measuring the project's file set.
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				countFiles := func() int {
					count := 0
					proj.Fset.Iterate(func(*token.File) bool {
						count++
						return true
					})
					return count
				}
				wantCount := countFiles()
				wantBase := proj.Fset.Base()
				params := &DocumentFormattingParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				}

				for range 3 {
					edits, err := s.textDocumentFormatting(params)
					require.NoError(t, err)
					if tt.want == tt.source {
						assert.Empty(t, edits)
					} else {
						require.Len(t, edits, 1)
						assert.Equal(t, tt.want, edits[0].NewText)
					}
					assert.Equal(t, wantCount, countFiles())
					assert.Equal(t, wantBase, proj.Fset.Base())
				}
			})
		}
	})

	t.Run("OtherFileASTIsolation", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox":   []byte("onEvent \"value\", (value) => {\n\techo \"ready\"\n}\n"),
			"Worker_fixture.gox": []byte("var count int\n"),
		})
		proj := s.workspaceRootFS
		workerAST, err := proj.ASTFile("Worker_fixture.gox")
		require.NoError(t, err)
		declCount := len(workerAST.Decls)
		edits, err := s.textDocumentFormatting(&DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		})
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, "onEvent \"value\", () => {\n\techo \"ready\"\n}\n", edits[0].NewText)
		assert.Len(t, workerAST.Decls, declCount)
		cachedAST, err := proj.ASTFile("Worker_fixture.gox")
		require.NoError(t, err)
		assert.Same(t, workerAST, cachedAST)
	})

	t.Run("PlainXGoLambda", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("import \"example.com/framework\"\nvar app framework.App\napp.onEvent \"value\", (value)=>{echo \"ready\"}\n"),
		})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		edits, err := s.textDocumentFormatting(&DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, "import \"example.com/framework\"\n\nvar app framework.App\n\napp.onEvent \"value\", () => {\n\techo \"ready\"\n}\n", edits[0].NewText)
	})

	t.Run("LineEndingsAndUTF16", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			want   string
			end    Position
		}{
			{
				name: "UTF16LastLine", source: "echo   \"\U0001f680\"", want: "echo \"\U0001f680\"\n",
				end: Position{Line: 0, Character: 11},
			},
			{
				name: "CRLFOnly", source: "echo \"ready\"\r\n", want: "echo \"ready\"\n",
				end: Position{Line: 1, Character: 0},
			},
			{
				name: "CRLFWithUTF16LastLine", source: "echo \"ready\"\r\necho   \"\U0001f680\"", want: "echo \"ready\"\necho \"\U0001f680\"\n",
				end: Position{Line: 1, Character: 11},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				edits, err := s.textDocumentFormatting(&DocumentFormattingParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				})
				require.NoError(t, err)
				assert.Equal(t, []TextEdit{{Range: Range{End: tt.end}, NewText: tt.want}}, edits)
			})
		}
	})

	t.Run("InvalidSyntax", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("func {\n")})
		edits, err := s.textDocumentFormatting(&DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		assert.ErrorContains(t, err, "failed to format source file")
		assert.Nil(t, edits)
	})

	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`
// A classfile project.

var (
  title string
  count int
)
type Score int
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 8, Character: 0},
			},
			NewText: `// A classfile project.

type Score int

var (
	title string
	count int
)
`,
		}, edits[0])
	})

	t.Run("EnumAndFuncDecorator", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`import "time"

@retry(time.Second)
func run() {
}

type Color const (
	Red = iota
)

func retry(delay time.Duration, fn func()) {
	fn()
}
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, `import "time"

type Color const (
	Red = iota
)

@retry(time.Second)
func run() {
}

func retry(delay time.Duration, fn func()) {
	fn()
}
`, edits[0].NewText)
	})

	t.Run("StaticValues", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`const .kind, Rect.name = "shape", "rect"

var (
.count int=1
Rect.total, .extra int=2,3
width int
)

type Rect int
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, `type Rect int

const .kind, Rect.name = "shape", "rect"

var (
	.count             int = 1
	Rect.total, .extra int = 2, 3
	width              int
)
`, edits[0].NewText)
	})

	t.Run("UnsupportedFile", func(t *testing.T) {
		m := map[string][]byte{
			"notes.txt": []byte(`echo "Hello, XGo!"`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///notes.txt"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("FileNotFound", func(t *testing.T) {
		s := newTestServer(t, nil)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///notexist_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.Nil(t, edits)
	})

	t.Run("NoChangesNeeded", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte("onStart => {\n\techo \"ready\"\n}\n"),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("AcceptableFormatError", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.

var title string
!InvalidSyntax
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 4, Character: 0},
			},
			NewText: `// A classfile project.

var (
	title string
)

!InvalidSyntax
`,
		}, edits[0])
	})

	t.Run("ClassFieldsDeclarationWithComments", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.

// Class fields.
var (
    // The title.
    title string // The project title.

    count int // The first counter.
    // The second counter.
    count2 int
    // The third counter.
    count3 int
    count4 int // The fourth counter.
    // The fifth counter.
    count5 int

    count6 int // The sixth counter.
)
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, `// A classfile project.

// Class fields.
var (
	// The title.
	title string // The project title.

	count int // The first counter.
	// The second counter.
	count2 int
	// The third counter.
	count3 int
	count4 int // The fourth counter.
	// The fifth counter.
	count5 int

	count6 int // The sixth counter.
)
`, edits[0].NewText)
	})

	t.Run("ClassFieldsDeclarationWithClosingComment", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`var (
    score int
) // Class fields.
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, `var (
	score int
) // Class fields.
`, edits[0].NewText)
	})

	t.Run("ClassFieldsDeclarationWithoutDoc", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.

	var (
		// The title.
		title string // The project title.
		count int
	)
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, `// A classfile project.

var (
	// The title.
	title string // The project title.
	count int
)
`, edits[0].NewText)
	})

	t.Run("EmbeddedClassField", func(t *testing.T) {
		m := map[string][]byte{
			"Worker_fixture.gox": {},
			"main_fixture.gox": []byte(`// A classfile project.

var (
	Worker
)
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("WithImportStmt", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.
import "math"

onStart => {
	println math.floor(2.5)
}
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("WithUnusedLambdaParams", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.
onEvent "value", (value) => {
	println "value"
}

onEvent "value", (value) => {
	println value
}
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		proj := s.workspaceRootFS
		astFile, err := proj.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		typeInfo, err := proj.TypeInfo()
		require.NoError(t, err)
		var before bytes.Buffer
		require.NoError(t, format.Node(&before, proj.Fset, astFile))
		require.Equal(t, string(m["main_fixture.gox"]), before.String())

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 8, Character: 0},
			},
			NewText: `// A classfile project.
onEvent "value", () => {
	println "value"
}

onEvent "value", (value) => {
	println value
}
`,
		}, edits[0])

		// Formatting must not change the cached AST or the pending document.
		var after bytes.Buffer
		require.NoError(t, format.Node(&after, proj.Fset, astFile))
		assert.Equal(t, before.String(), after.String())
		cachedAST, err := proj.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		assert.Same(t, astFile, cachedAST)
		cachedTypeInfo, err := proj.TypeInfo()
		require.NoError(t, err)
		assert.Same(t, typeInfo, cachedTypeInfo)
		file, ok := proj.File("main_fixture.gox")
		require.True(t, ok)
		assert.Equal(t, m["main_fixture.gox"], file.Content)

		repeatedEdits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		assert.Equal(t, edits, repeatedEdits)

		s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(edits[0].NewText), Version: 1}})
		edits, err = s.textDocumentFormatting(params)
		require.NoError(t, err)
		assert.Empty(t, edits)
	})

	t.Run("WithUnusedLambdaParamsInKwarg", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`type Worker struct{}

type Options0 struct {
	Handler func()
}

type Options1 struct {
	Handler func(int)
}

var worker Worker

func (w *Worker) handle0(opts Options0?) {}
func (w *Worker) handle1(opts Options1?) {}

func (Worker).handle = (
	(Worker).handle0
	(Worker).handle1
)

onStart => {
	worker.handle handler = (n) => {
		echo "hi"
	}
	worker.handle handler = (n) => {
		echo n
	}
}
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 28, Character: 0},
			},
			NewText: `type Worker struct{}

type Options0 struct {
	Handler func()
}

type Options1 struct {
	Handler func(int)
}

func (w *Worker) handle0(opts Options0?) {}

func (w *Worker) handle1(opts Options1?) {}

func (Worker).handle = (
	(Worker).handle0
	(Worker).handle1
)

var (
	worker Worker
)

onStart => {
	worker.handle handler = () => {
		echo "hi"
	}
	worker.handle handler = (n) => {
		echo n
	}
}
`,
		}, edits[0])
	})

	t.Run("EmptyFile", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(``),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("WhitespaceOnlyFile", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(` `),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 1},
			},
			NewText: ``,
		}, edits[0])
	})

	t.Run("WithFloatingComments", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`import "fmt"

// floating comment1

// comment for var a
var a int

// floating comment2

// comment for func test
func test() {
	// comment inside func test
}

// floating comment3

// comment for const b
const b = "123"

// floating comment4


// floating comment5
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 23, Character: 0},
			},
			NewText: `import "fmt"

// floating comment1

// floating comment2

// floating comment3

// comment for const b
const b = "123"

var (
	// comment for var a
	a int
)

// comment for func test
func test() {
	// comment inside func test
}

// floating comment4

// floating comment5
`,
		}, edits[0])
	})

	t.Run("WithTrailingComments", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`import "fmt" // trailing comment for import "fmt"

const foo = "bar" // trailing comment for const foo

var a int // trailing comment for var a

func test() {} // trailing comment for func test
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 7, Character: 0},
			},
			NewText: `import "fmt" // trailing comment for import "fmt"

const foo = "bar" // trailing comment for const foo

var (
	a int // trailing comment for var a
)

func test() {} // trailing comment for func test
`,
		}, edits[0])
	})

	t.Run("WithMethods", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.

type Foo struct{}

var (
	flag bool
)

func (Foo) Bar() {}

func Bar() {}
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 11, Character: 0},
			},
			NewText: `// A classfile project.

type Foo struct{}

func (Foo) Bar() {}

var (
	flag bool
)

func Bar() {}
`,
		}, edits[0])
	})

	t.Run("ClassFieldsDeclarationWithAndWithoutInit", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.

var (
	dir int
	values []int

	moveStep int = 20
)

`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, `// A classfile project.

var (
	dir    int
	values []int

	moveStep int = 20
)
`, edits[0].NewText)
	})

	t.Run("EmptyClassFieldsDeclaration", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte("var ( )\n"),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, "var ()\n", edits[0].NewText)
	})

	t.Run("DuplicateClassFieldsDeclaration", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.

var (
	score int
)

var (
	playerName string = "Player1"
)

`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		assert.ErrorContains(t, err, "multiple top-level var declarations in classfile")
		assert.Nil(t, edits)
	})

	t.Run("ClassFieldsDeclarationWithMixedComments", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.

var (
	// Score for the player
	playerScore int
	// Lives remaining
	livesRemaining int

	// Variables with initialization
	// Game speed setting
	speed int = 10
	// Player name
	name string = "DefaultPlayer"
)

`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, `// A classfile project.

var (
	// Score for the player
	playerScore int
	// Lives remaining
	livesRemaining int

	// Variables with initialization
	// Game speed setting
	speed int = 10
	// Player name
	name string = "DefaultPlayer"
)
`, edits[0].NewText)
	})

	t.Run("DuplicateClassFieldsDeclarationWithoutParens", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.

var x int
var y int = 10
var z string
var name string = "Player"

`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		assert.ErrorContains(t, err, "multiple top-level var declarations in classfile")
		assert.Nil(t, edits)
	})

	t.Run("WithShadowEntryComments", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`// A classfile project.
var (
	count int
)

// onStart comment
onStart => {
	count++
	echo count
}
`),
		}
		s := newTestServer(t, m)
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Empty(t, edits)
	})
}

func TestOverloadResolvedCallExprArgType(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")
	handlerType := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
	handlerField := gotypes.NewField(token.NoPos, pkg, "Handler", handlerType, false)
	optionsType := gotypes.NewNamed(
		gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
		gotypes.NewStruct([]*gotypes.Var{handlerField}, nil),
		nil,
	)
	overload := gotypes.NewFunc(token.NoPos, pkg, "handle", gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		gotypes.NewTuple(
			gotypes.NewParam(token.NoPos, pkg, "name", gotypes.Typ[gotypes.String]),
			gotypes.NewParam(token.NoPos, pkg, "opts", optionsType),
		),
		nil,
		false,
	))
	kwarg := &ast.KwargExpr{
		Name:  &ast.Ident{Name: "handler"},
		Value: &ast.Ident{Name: "callback"},
	}

	t.Run("Keyword", func(t *testing.T) {
		callExpr := &ast.CallExpr{
			Args:   []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"first"`}},
			Kwargs: []*ast.KwargExpr{kwarg},
		}

		got := overloadResolvedCallExprArgType(nil, callExpr, overload, xgoutil.ResolvedCallExprArg{
			Kind:       xgoutil.ResolvedCallExprArgKeyword,
			Kwarg:      kwarg,
			ParamIndex: 0,
		})
		assert.True(t, gotypes.Identical(handlerType, got))
	})

	t.Run("PositionalAfterVariadicKwargParam", func(t *testing.T) {
		variadicOverload := gotypes.NewFunc(token.NoPos, pkg, "handle", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "opts", optionsType),
				gotypes.NewParam(token.NoPos, pkg, "values", gotypes.NewSlice(gotypes.Typ[gotypes.Int])),
			),
			nil,
			true,
		))
		callExpr := &ast.CallExpr{
			Args:   []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "1"}},
			Kwargs: []*ast.KwargExpr{kwarg},
		}

		got := overloadResolvedCallExprArgType(nil, callExpr, variadicOverload, xgoutil.ResolvedCallExprArg{
			Kind:       xgoutil.ResolvedCallExprArgPositional,
			ArgIndex:   0,
			ParamIndex: 0,
		})
		assert.True(t, gotypes.Identical(gotypes.Typ[gotypes.Int], got))
	})
}

func TestOverloadMatchesCallExpr(t *testing.T) {
	t.Run("LambdaArity", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		handlerType := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		overload := gotypes.NewFunc(token.NoPos, pkg, "handle", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "handler", handlerType),
				gotypes.NewParam(token.NoPos, pkg, "values", gotypes.NewSlice(gotypes.Typ[gotypes.Int])),
			),
			nil,
			true,
		))
		callExpr := &ast.CallExpr{
			Args: []ast.Expr{&ast.LambdaExpr{}},
		}

		assert.True(t, overloadMatchesCallExpr(nil, callExpr, overload, -1))
	})

	t.Run("SkippedUnknownKwarg", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		countField := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		optionsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{countField}, nil),
			nil,
		)
		overload := gotypes.NewFunc(token.NoPos, pkg, "handle", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "opts", optionsType)),
			nil,
			false,
		))
		callExpr := &ast.CallExpr{
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "unknown"},
				Value: &ast.Ident{Name: "value"},
			}},
		}

		assert.True(t, overloadMatchesCallExpr(nil, callExpr, overload, 0))
		assert.False(t, overloadMatchesCallExpr(nil, callExpr, overload, -1))
	})
}

func TestCallExprArgType(t *testing.T) {
	t.Run("Variadic", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		handlerType := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		sig := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "name", gotypes.Typ[gotypes.String]),
				gotypes.NewParam(token.NoPos, pkg, "handlers", gotypes.NewSlice(handlerType)),
			),
			nil,
			true,
		)
		params := sig.Params()

		assert.Equal(t, gotypes.Typ[gotypes.String], callExprArgType(sig, params, 0))
		assert.True(t, gotypes.Identical(handlerType, callExprArgType(sig, params, 1)))
		assert.True(t, gotypes.Identical(handlerType, callExprArgType(sig, params, 2)))
		assert.Nil(t, callExprArgType(sig, params, -1))
	})

	t.Run("Autoclosure", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		autoclosureType := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.Bool])),
			false,
		)
		sig := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(
				token.NoPos,
				pkg,
				"__xgo_autoclosure_condition",
				autoclosureType,
			)),
			nil,
			false,
		)

		assert.Equal(t, gotypes.Typ[gotypes.Bool], callExprArgType(sig, sig.Params(), 0))
	})
}
