package pkgdoc

import (
	goast "go/ast"
	goparser "go/parser"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGoWithXGoDirectives(t *testing.T) {
	goSource := `package sample
//xgols:notrecommended
var GoValue int
type hidden struct { Field int }
type Public hidden
const XGoo_Choose = "Helper"
func Helper() {}
`
	for _, tt := range []struct {
		name   string
		xgo    string
		marked []string
	}{
		{
			name: "Merge",
			xgo: `package sample
type hidden struct {
	//xgols:notrecommended
	Field int
}
type Public hidden
func Helper() {}
//xgols:notrecommended
func Choose = (Helper)
`,
			marked: []string{"GoValue", "Choose", "hidden.Field", "Public.Field"},
		},
		{name: "UnannotatedXGo", xgo: "package sample\nfunc Helper() {}\nfunc Choose = (Helper)\n", marked: []string{"GoValue"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			goFile, err := goparser.ParseFile(token.NewFileSet(), "sample.go", goSource, goparser.ParseComments)
			require.NoError(t, err)
			xgoFile, err := parser.ParseFile(token.NewFileSet(), "sample.xgo", tt.xgo, parser.ParseComments)
			require.NoError(t, err)
			goPkg := &goast.Package{Name: "sample", Files: map[string]*goast.File{"sample.go": goFile}}
			xgoPkg := &ast.Package{Name: "sample", Files: map[string]*ast.File{"sample.xgo": xgoFile}}
			doc := NewGoWithXGoDirectives("sample", goPkg, xgoPkg, nil)
			assert.Equal(t, markedDeclarations(tt.marked...), doc.Declarations)
		})
	}
}

func TestNewXGoDirectives(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.xgo", `
//xgols:notrecommended
var (
	first, second int
	third int
)
var (
	visible int
	//xgols:notrecommended
	marked int
)
//xgols:notrecommended
const (
	one = 1
	two = 2
)
//xgols:notrecommended
type (
	Record struct {
		//xgols:notrecommended
		field, other int
		visible int
		trailing int //xgols:notrecommended
		//xgols:notrecommended
		*Embedded
	}
	Alias = int
)
type Embedded struct{}
type Reader interface {
	//xgols:notrecommended
	Read() int
	Visible()
}
//xgols:notrecommended
func (r *Record) Turn__0() {}
//xgols:notrecommended
func helper() {}
//xgols:notrecommended
func turn = (helper)
//xgols:notrecommended
func (Record).action = (helper)
func trailing() {} //xgols:notrecommended

//xgols:notrecommended

func detached() {}
`, parser.ParseComments)
	require.NoError(t, err)
	doc := NewXGo("main", &ast.Package{Name: "main", Files: map[string]*ast.File{"main.xgo": file}}, nil)
	assert.Equal(t, markedDeclarations(
		"first", "second", "third", "marked", "one", "two", "Record", "Alias",
		"Record.field", "Record.other", "Record.Embedded", "Reader.Read", "Record.Turn__0",
		"helper", "turn", "Record.action",
	), doc.Declarations)
	assert.Empty(t, doc.Funcs["helper"])
	assert.Empty(t, doc.Types["Record"].Doc)
}

func TestNewXGoClassDirectives(t *testing.T) {
	t.Run("NormalClass", func(t *testing.T) {
		file, err := parser.ParseEntry(token.NewFileSet(), "Record.gox", `
//xgols:notrecommended
var (
	first, second int
	*Embedded
)
//xgols:notrecommended
func internal() {}
//xgols:notrecommended
func action = (internal)
//xgols:notrecommended
const limit = 1
`, parser.Config{Mode: parser.ParseComments})
		require.NoError(t, err)
		doc := NewXGo("main", &ast.Package{Name: "main", Files: map[string]*ast.File{"Record.gox": file}}, nil)
		assert.Equal(t, markedDeclarations("Record.first", "Record.second", "Record.Embedded", "Record.internal", "Record.action", "limit"), doc.Declarations)
	})
	t.Run("UnclassifiedClass", func(t *testing.T) {
		file, err := parser.ParseFile(token.NewFileSet(), "Worker.gox", `
//xgols:notrecommended
var first int
//xgols:notrecommended
func internal() {}
//xgols:notrecommended
func action = (internal)
//xgols:notrecommended
func (r *Record) method() {}
//xgols:notrecommended
type Record struct{}
`, parser.ParseComments|parser.ParseXGoClass)
		require.NoError(t, err)
		doc := NewXGo("main", &ast.Package{Name: "main", Files: map[string]*ast.File{"Worker.gox": file}}, nil)
		assert.Equal(t, markedDeclarations("Record.method", "Record"), doc.Declarations)
	})
}

func TestXGoDirectivesIncompleteReceivers(t *testing.T) {
	group := &ast.CommentGroup{List: []*ast.Comment{{Text: "//xgols:notrecommended"}}}
	for _, recv := range []*ast.FieldList{
		{},
		{List: []*ast.Field{{Type: &ast.BadExpr{}}}},
	} {
		doc := &PkgDoc{}
		doc.recordXGoFunctionDirectives(&ast.File{}, "", "Incomplete", recv, group)
		assert.Nil(t, doc.Declarations)
	}
}
