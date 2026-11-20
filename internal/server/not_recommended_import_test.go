package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNotRecommendedImportServer(t *testing.T, source string) *Server {
	t.Helper()

	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	proj := s.getProj()
	file, err := goparser.ParseFile(proj.Fset, "dependency.go", `package dependency
const XGoPackage = true
//xgols:notRecommended
var HiddenValue string
var VisibleValue string
//xgols:notRecommended
const HiddenLimit = "hidden"
const VisibleLimit = "visible"
//xgols:notRecommended
type HiddenType struct{}
type Record struct {
    //xgols:notRecommended
    HiddenField string
    VisibleField string
}
//xgols:notRecommended
func HiddenText() string { return "hidden" }
func VisibleText() string { return "visible" }
//xgols:notRecommended
func (Record) HiddenMethod() string { return "hidden" }
func (Record) VisibleMethod() string { return "visible" }
`, goparser.ParseComments)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/dependency", proj.Fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	doc := pkgdoc.NewGo(pkg.Path(), &goast.Package{Name: pkg.Name(), Files: map[string]*goast.File{"dependency.go": file}})
	baseImporter, baseLookup := proj.Importer, s.lookupPkgDoc
	proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if path == pkg.Path() {
			return pkg, nil
		}
		return baseImporter.Import(path)
	})
	s.lookupPkgDoc = func(path string) (*pkgdoc.PkgDoc, error) {
		if path == pkg.Path() {
			return doc, nil
		}
		return baseLookup(path)
	}
	return s
}

func TestServerNotRecommendedImportedDeclarations(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		want, absent []string
	}{
		{
			name:   "PackageMembers",
			source: "import dep \"example.com/dependency\"\nfunc run() { dep.|VisibleText() }\n",
			want:   []string{"VisibleValue", "VisibleLimit", "Record", "visibleText"},
			absent: []string{"HiddenValue", "HiddenLimit", "HiddenType", "hiddenText", "HiddenText"},
		},
		{
			name:   "DotImport",
			source: "import . \"example.com/dependency\"\nfunc run() {\n|\n}\n",
			want:   []string{"VisibleValue", "VisibleLimit", "Record", "visibleText"},
			absent: []string{"HiddenValue", "HiddenLimit", "HiddenType", "hiddenText", "HiddenText"},
		},
		{
			name:   "Members",
			source: "import dep \"example.com/dependency\"\nvar record dep.Record\nfunc run() { println record.|visibleMethod }\n",
			want:   []string{"VisibleField", "visibleMethod"},
			absent: []string{"HiddenField", "hiddenMethod", "HiddenMethod"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newNotRecommendedImportServer(t, source)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			for _, name := range tt.want {
				assert.Contains(t, labels, name)
			}
			for _, name := range tt.absent {
				assert.NotContains(t, labels, name)
			}
		})
	}
}

func TestServerNotRecommendedImportedInputNames(t *testing.T) {
	s := newNotRecommendedImportServer(t, `import . "example.com/dependency"
func Use(value string) {}
use "value"
`)
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	names := slots[len(slots)-1].PredefinedNames
	for _, name := range []string{"HiddenValue", "hiddenText"} {
		assert.NotContains(t, names, name)
	}
	for _, name := range []string{"VisibleValue", "visibleText"} {
		assert.Contains(t, names, name)
	}
}

func TestServerNotRecommendedDeclarationAttachment(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `//xgols:notRecommended
var (
    groupFirst, groupSecond int
    groupThird int
)
var (
    //xgols:notRecommended
    specFirst, specSecond int
    visibleValue int
)
//xgols:notRecommended
const (
    groupLimit = 1
    groupOtherLimit = 2
)
//xgols:notRecommended
type (
    GroupType struct{}
    OtherGroupType struct{}
)
//xgols:notRecommended

var detached int
var trailing int //xgols:notRecommended
/*xgols:notRecommended*/
var block int
// xgols:notRecommended
var spaced int
//xgols:not-recommended
var dashed int
//xgols:notRecommended because internal
var arguments int
//xgols:notRecommended
func HiddenOuter() {
    var nested int
    println nested
}
func run() {
|
}
`)
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	for _, name := range []string{"groupFirst", "groupSecond", "groupThird", "specFirst", "specSecond", "groupLimit", "groupOtherLimit", "GroupType", "OtherGroupType", "hiddenOuter", "HiddenOuter"} {
		assert.NotContains(t, labels, name)
	}
	for _, name := range []string{"visibleValue", "detached", "trailing", "block", "spaced", "dashed", "arguments"} {
		assert.Contains(t, labels, name)
	}
}

func TestServerNotRecommendedEmbeddedField(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `type Embedded struct { Value int }
//xgols:notRecommended
type Record struct {
    //xgols:notRecommended
    *Embedded
    Visible int
}
var record Record
func run() {

println record.|Visible
}
`)
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	assert.NotContains(t, labels, "Embedded")
	assert.Contains(t, labels, "Visible")
	assert.Contains(t, labels, "Value")
	_, typePos := typeDisplayTestSource(t, `type Embedded struct { Value int }
//xgols:notRecommended
type Record struct {
    //xgols:notRecommended
    *Embedded
    Visible int
}
var record Record
func run() {
|
println record.Visible
}
`)
	labels = completionItemLabels(completionItemsAt(t, s, "main.xgo", typePos))
	assert.Contains(t, labels, "Embedded")
	assert.NotContains(t, labels, "Record")
}
