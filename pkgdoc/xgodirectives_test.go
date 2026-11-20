package pkgdoc

import (
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewXGoDirectives(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.xgo", `
//xgols:notRecommended
var (
	first, second int
	third int
)
var (
	visible int
	//xgols:notRecommended
	marked int
)
//xgols:notRecommended
const (
	one = 1
	two = 2
)
//xgols:notRecommended
type (
	Record struct {
		//xgols:notRecommended
		field, other int
		visible int
		trailing int //xgols:notRecommended
		//xgols:notRecommended
		*Embedded
	}
	Alias = int
)
type Embedded struct{}
type Reader interface {
	//xgols:notRecommended
	Read() int
	Visible()
}
//xgols:notRecommended
func (r *Record) Turn__0() {}
//xgols:notRecommended
func helper() {}
//xgols:notRecommended
func turn = (helper)
//xgols:notRecommended
func (Record).action = (helper)
func trailing() {} //xgols:notRecommended

//xgols:notRecommended

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
//xgols:notRecommended
var (
	first, second int
	*Embedded
)
//xgols:notRecommended
func internal() {}
//xgols:notRecommended
func action = (internal)
//xgols:notRecommended
const limit = 1
`, parser.Config{Mode: parser.ParseComments})
		require.NoError(t, err)
		doc := NewXGo("main", &ast.Package{Name: "main", Files: map[string]*ast.File{"Record.gox": file}}, nil)
		assert.Equal(t, markedDeclarations("Record.first", "Record.second", "Record.Embedded", "Record.internal", "Record.action", "limit"), doc.Declarations)
	})
	t.Run("UnclassifiedClass", func(t *testing.T) {
		file, err := parser.ParseFile(token.NewFileSet(), "Worker.gox", `
//xgols:notRecommended
var first int
//xgols:notRecommended
func internal() {}
//xgols:notRecommended
func action = (internal)
//xgols:notRecommended
func (r *Record) method() {}
//xgols:notRecommended
type Record struct{}
`, parser.ParseComments|parser.ParseXGoClass)
		require.NoError(t, err)
		doc := NewXGo("main", &ast.Package{Name: "main", Files: map[string]*ast.File{"Worker.gox": file}}, nil)
		assert.Equal(t, markedDeclarations("Record.method", "Record"), doc.Declarations)
	})
}

func TestXGoDirectivesIncompleteReceivers(t *testing.T) {
	group := &ast.CommentGroup{List: []*ast.Comment{{Text: "//xgols:notRecommended"}}}
	for _, recv := range []*ast.FieldList{
		{},
		{List: []*ast.Field{{Type: &ast.BadExpr{}}}},
	} {
		doc := &PkgDoc{}
		doc.recordXGoFunctionDirectives(&ast.File{}, "", "Incomplete", recv, group)
		assert.Nil(t, doc.Declarations)
	}
}
