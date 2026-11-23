package pkgdoc

import (
	"encoding/json"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"testing"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDirectives(t *testing.T) {
	for _, tt := range []struct {
		name    string
		comment string
		want    bool
	}{
		{name: "Exact", comment: "//xgols:notRecommended", want: true},
		{name: "TrailingWhitespace", comment: "//xgols:notRecommended \t\r\n", want: true},
		{name: "SpaceAfterSlashes", comment: "// xgols:notRecommended"},
		{name: "BlockComment", comment: "/*xgols:notRecommended*/"},
		{name: "OldNamespace", comment: "//xgolsw:hidden"},
		{name: "DashedName", comment: "//xgols:not-recommended"},
		{name: "Arguments", comment: "//xgols:notRecommended reason"},
		{name: "Suffix", comment: "//xgols:notRecommendedOther"},
		{name: "OldSpelling", comment: "//xgols:notrecommended"},
		{name: "DifferentCase", comment: "//xgols:NotRecommended"},
		{name: "Prose", comment: "// Use //xgols:notRecommended here."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			group := &ast.CommentGroup{List: []*ast.Comment{{Text: tt.comment}}}
			assert.Equal(t, tt.want, ParseDirectives(nil, group).NotRecommended)
		})
	}
	assert.Equal(t, DeclarationMetadata{}, ParseDirectives())
	assert.True(t, ParseDirectives(&ast.CommentGroup{List: []*ast.Comment{
		{Text: "// Example is documented."}, {Text: "//xgols:notRecommended"},
	}}).NotRecommended)
}

func TestNewGoDirectives(t *testing.T) {
	doc := newGoTestDoc(t, `package sample

//xgols:notRecommended
var (
	First, Second int
	Third int
)
var (
	Visible int
	//xgols:notRecommended
	Marked int
)
//xgols:notRecommended
const (
	One = 1
	Two = 2
)
//xgols:notRecommended
type (
	Record[T any] struct {
		//xgols:notRecommended
		Field, Other T
		Visible T
		Trailing T //xgols:notRecommended
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
func (r *Record[T]) Turn__0() {}
//xgols:notRecommended
func helper() {}
func trailing() {} //xgols:notRecommended

//xgols:notRecommended

func detached() {}
`)
	assert.Equal(t, markedDeclarations(
		"First", "Second", "Third", "Marked", "One", "Two", "Record", "Alias",
		"Record.Field", "Record.Other", "Record.Embedded", "Reader.Read", "Record.Turn__0", "helper",
	), doc.Declarations)
	assert.NotContains(t, doc.Funcs["helper"], "notrecommended")
	assert.Empty(t, doc.Types["Record"].Doc)
}

func TestDeclarationMetadataJSON(t *testing.T) {
	t.Run("UnannotatedArchive", func(t *testing.T) {
		doc := newGoTestDoc(t, "package sample\nvar Visible int\n")
		assert.Nil(t, doc.Declarations)
		data, err := json.Marshal(doc)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "Declarations")
	})
	t.Run("RoundTrip", func(t *testing.T) {
		doc := newGoTestDoc(t, "package sample\n//xgols:notRecommended\nfunc Internal() {}\n")
		data, err := json.Marshal(doc)
		require.NoError(t, err)
		var decoded PkgDoc
		require.NoError(t, json.Unmarshal(data, &decoded))
		assert.Equal(t, doc, &decoded)
	})
}

func TestObjectMetadata(t *testing.T) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "sample.go", `package sample
//xgols:notRecommended
var Internal int
//xgols:notRecommended
const Limit = 1
//xgols:notRecommended
type Record[T any] struct {
	//xgols:notRecommended
	Field T
	Visible T
}
type Other struct { Field int }
type Alias = Record[int]
type Reader interface {
	//xgols:notRecommended
	Read() int
}
//xgols:notRecommended
func (r (*Record[T])) Turn__0() {}
//xgols:notRecommended
func Helper() {}
`, goparser.ParseComments)
	require.NoError(t, err)
	info := &gotypes.Info{Defs: make(map[*goast.Ident]gotypes.Object)}
	pkg, err := new(gotypes.Config).Check("example.com/sample", fset, []*goast.File{file}, info)
	require.NoError(t, err)
	doc := NewGo(pkg.Path(), &goast.Package{Name: "sample", Files: map[string]*goast.File{"sample.go": file}})
	for ident, obj := range info.Defs {
		if obj == nil {
			continue
		}
		switch ident.Name {
		case "Internal", "Limit", "Record", "Read", "Turn__0", "Helper":
			assert.True(t, doc.ObjectMetadata(obj).NotRecommended, ident.Name)
		case "Other", "Alias", "Reader", "Visible", "T", "r":
			assert.False(t, doc.ObjectMetadata(obj).NotRecommended, ident.Name)
		}
	}
	recordName, ok := pkg.Scope().Lookup("Record").(*gotypes.TypeName)
	require.True(t, ok)
	record, ok := recordName.Type().(*gotypes.Named)
	require.True(t, ok)
	instance, err := gotypes.Instantiate(nil, record, []gotypes.Type{gotypes.Typ[gotypes.Int]}, true)
	require.NoError(t, err)
	named, ok := instance.(*gotypes.Named)
	require.True(t, ok)
	st, ok := named.Underlying().(*gotypes.Struct)
	require.True(t, ok)
	assert.True(t, doc.ObjectMetadata(st.Field(0)).NotRecommended)
	assert.False(t, doc.ObjectMetadata(st.Field(1)).NotRecommended)
	assert.True(t, doc.ObjectMetadata(named.Method(0)).NotRecommended)
	otherName, ok := pkg.Scope().Lookup("Other").(*gotypes.TypeName)
	require.True(t, ok)
	other, ok := otherName.Type().Underlying().(*gotypes.Struct)
	require.True(t, ok)
	assert.False(t, doc.ObjectMetadata(other.Field(0)).NotRecommended)
	assert.Equal(t, DeclarationMetadata{}, doc.ObjectMetadata(nil))

	t.Run("UnrelatedDeclarations", func(t *testing.T) {
		local := gotypes.NewVar(token.NoPos, pkg, "Internal", gotypes.Typ[gotypes.Int])
		assert.False(t, doc.ObjectMetadata(local).NotRecommended)
		otherPkg := gotypes.NewPackage("example.com/other", "other")
		assert.False(t, doc.ObjectMetadata(gotypes.NewVar(token.NoPos, otherPkg, "Internal", gotypes.Typ[gotypes.Int])).NotRecommended)
		assert.False(t, doc.ObjectMetadata(gotypes.Universe.Lookup("int")).NotRecommended)
		iface := gotypes.NewInterfaceType(nil, nil)
		recv := gotypes.NewVar(token.NoPos, pkg, "", iface)
		method := gotypes.NewFunc(token.NoPos, pkg, "Read", gotypes.NewSignatureType(recv, nil, nil, nil, nil, false))
		assert.False(t, doc.ObjectMetadata(method).NotRecommended)
	})
	t.Run("StaleArchiveKeys", func(t *testing.T) {
		stale := &PkgDoc{Path: pkg.Path(), Declarations: markedDeclarations("Missing.Field", "Limit.Field", "Reader.Field")}
		assert.False(t, stale.ObjectMetadata(st.Field(0)).NotRecommended)
	})

	t.Run("ReconstructedOverload", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := goparser.ParseFile(fset, "sample.go", `package sample
const XGoPackage = true
func ChooseString(value string) string { return value }
//xgols:notRecommended
const XGoo_Choose = "ChooseString"
`, goparser.ParseComments)
		require.NoError(t, err)
		pkg, err := new(gotypes.Config).Check("example.com/sample", fset, []*goast.File{file}, nil)
		require.NoError(t, err)
		gogen.InitXGoPackage(pkg)

		doc := NewGo(pkg.Path(), &goast.Package{Name: "sample", Files: map[string]*goast.File{"sample.go": file}})
		group := pkg.Scope().Lookup("Choose")
		require.NotNil(t, group)
		assert.True(t, doc.ObjectMetadata(group).NotRecommended)
		assert.False(t, doc.ObjectMetadata(pkg.Scope().Lookup("ChooseString")).NotRecommended)
		assert.False(t, (&PkgDoc{Path: pkg.Path()}).ObjectMetadata(group).NotRecommended)
	})

	t.Run("ReconstructedStaticMembers", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := goparser.ParseFile(fset, "sample.go", `package sample
const XGoPackage = true
type Record struct{}
//xgols:notRecommended
func XGos_Record_Choose(value int) int { return value }
//xgols:notRecommended
var XGos_Record_Current int
//xgols:notRecommended
const XGos_Record_Default = 1
var XGos_Record_Visible int
`, goparser.ParseComments)
		require.NoError(t, err)
		pkg, err := new(gotypes.Config).Check("example.com/sample", fset, []*goast.File{file}, nil)
		require.NoError(t, err)
		gogen.InitXGoPackage(pkg)

		doc := NewGo(pkg.Path(), &goast.Package{Name: "sample", Files: map[string]*goast.File{"sample.go": file}})
		record := pkg.Scope().Lookup("Record")
		require.NotNil(t, record)
		for _, name := range []string{"Choose", "Current", "Default", "Visible"} {
			member, _, _ := gotypes.LookupFieldOrMethod(record.Type(), false, pkg, name)
			require.NotNil(t, member, name)
			assert.Equal(t, name != "Visible", doc.ObjectMetadata(member).NotRecommended, name)
		}
	})
}

func TestGoDirectivesForDerivedMembers(t *testing.T) {
	const source = `package sample
type Embedded struct{}
type hidden[T any, U any] (struct {
	//xgols:notRecommended
	Embedded
	//xgols:notRecommended
	Value U
})
//xgols:notRecommended
func (hidden[T, U]) Read() {}
type Public (hidden[int, string])
func (Public) Read() {}

type reader (interface {
	//xgols:notRecommended
	Get() int
})
type Reader reader

type Base struct{}
type pointer = *(Base)
//xgols:notRecommended
func (pointer) Method() {}

type API = interface {
	//xgols:notRecommended
	Load()
}
type Other = interface { Load() }
`
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "sample.go", source, goparser.ParseComments)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/sample", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	doc := NewGo(pkg.Path(), &goast.Package{Name: pkg.Name(), Files: map[string]*goast.File{"sample.go": file}})
	assert.Equal(t, markedDeclarations("hidden.Embedded", "hidden.Value", "hidden.Read", "Public.Embedded", "Public.Value",
		"reader.Get", "Reader.Get", "Base.Method", "API.Load"), doc.Declarations)
	for _, tt := range []struct {
		owner, member string
		marked        bool
	}{
		{owner: "Public", member: "Read"},
		{owner: "API", member: "Load", marked: true},
		{owner: "Other", member: "Load"},
	} {
		owner := pkg.Scope().Lookup(tt.owner)
		require.NotNil(t, owner)
		member, _, _ := gotypes.LookupFieldOrMethod(owner.Type(), false, pkg, tt.member)
		require.NotNil(t, member)
		assert.Equal(t, tt.marked, doc.ObjectMetadata(member).NotRecommended, tt.owner)
	}
}

func TestGoDirectivesIncompleteReceivers(t *testing.T) {
	group := &ast.CommentGroup{List: []*ast.Comment{{Text: "//xgols:notRecommended"}}}
	for _, recv := range []*goast.FieldList{
		{},
		{List: []*goast.Field{{Type: &goast.BadExpr{}}}},
	} {
		doc := &PkgDoc{}
		doc.collectGoDirectives(&goast.Package{Files: map[string]*goast.File{"sample.go": {
			Decls: []goast.Decl{&goast.FuncDecl{Name: goast.NewIdent("Incomplete"), Recv: recv, Doc: group}},
		}}}, nil)
		assert.Nil(t, doc.Declarations)
	}
}

func markedDeclarations(names ...string) map[string]DeclarationMetadata {
	declarations := make(map[string]DeclarationMetadata, len(names))
	for _, name := range names {
		declarations[name] = DeclarationMetadata{NotRecommended: true}
	}
	return declarations
}
