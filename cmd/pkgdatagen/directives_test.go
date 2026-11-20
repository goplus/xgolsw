package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	gotypes "go/types"
	"os"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"
)

func TestGenerateDirectiveMemberIdentity(t *testing.T) {
	const pkgPath = "example.com/fixture"
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("go.mod", []byte("module "+pkgPath+"\n\ngo 1.26.0\n"), 0o644))
	require.NoError(t, os.WriteFile("dep.go", []byte(`package fixture

type Base struct{}
type alias = Base
type Alias = alias
//xgols:notRecommended
func (*alias) Hidden() {}
func (*Base) Visible() {}
type pointerAlias = *Base
//xgols:notRecommended
func (pointerAlias) HiddenThroughPointer() {}

type API = interface {
    //xgols:notRecommended
    Read()
}
type OtherAPI interface { Read() }

type hidden struct {
    //xgols:notRecommended
    Key int
    //xgols:notRecommended
    Base
}
type intermediate = hidden
type Public intermediate
type Other struct { Key int; Base }

type internal[T any] struct {
    //xgols:notRecommended
    Item T
}
type Generic internal[int]

type internalPair[A, B any] struct {
    //xgols:notRecommended
    Second B
}
type Pair (internalPair[int, string])

type hiddenReader interface {
    //xgols:notRecommended
    Text() string
}
type Reader hiddenReader
`), 0o644))
	pkg, doc := generateDirectiveTestPackage(t, pkgPath)
	assert.Nil(t, pkg.Scope().Lookup("hidden"))
	for _, tt := range []struct {
		owner, member string
		marked        bool
	}{
		{owner: "Base", member: "Hidden", marked: true},
		{owner: "Alias", member: "Hidden", marked: true},
		{owner: "Base", member: "HiddenThroughPointer", marked: true},
		{owner: "Base", member: "Visible"},
		{owner: "API", member: "Read", marked: true},
		{owner: "OtherAPI", member: "Read"},
		{owner: "Public", member: "Key", marked: true},
		{owner: "Public", member: "Base", marked: true},
		{owner: "Other", member: "Key"},
		{owner: "Other", member: "Base"},
		{owner: "Generic", member: "Item", marked: true},
		{owner: "Pair", member: "Second", marked: true},
		{owner: "Reader", member: "Text", marked: true},
	} {
		owner := pkg.Scope().Lookup(tt.owner)
		require.NotNil(t, owner, tt.owner)
		member, _, _ := gotypes.LookupFieldOrMethod(owner.Type(), true, pkg, tt.member)
		require.NotNil(t, member, "%s.%s", tt.owner, tt.member)
		assert.Equal(t, tt.marked, doc.ObjectMetadata(member).NotRecommended, "%s.%s", tt.owner, tt.member)
	}
}

func TestGenerateDirectivesFromSelectedGoFiles(t *testing.T) {
	const pkgPath = "example.com/fixture"
	t.Chdir(t.TempDir())
	for name, source := range map[string]string{
		"go.mod": "module " + pkgPath + "\n\ngo 1.26.0\n",
		"dep.xgo": `package fixture
//xgols:notRecommended
const XGoValue = 1
`,
		"xgo_autogen.go": `package fixture
const XGoValue = 1
`,
		"native.go": `package fixture
// GoValue holds the value.
//xgols:notRecommended
const GoValue = 2
`,
	} {
		require.NoError(t, os.WriteFile(name, []byte(source), 0o644))
	}

	pkg, doc := generateDirectiveTestPackage(t, pkgPath)
	xgoValue := pkg.Scope().Lookup("XGoValue")
	require.NotNil(t, xgoValue)
	assert.False(t, doc.ObjectMetadata(xgoValue).NotRecommended)
	goValue := pkg.Scope().Lookup("GoValue")
	require.NotNil(t, goValue)
	assert.True(t, doc.ObjectMetadata(goValue).NotRecommended)
	assert.Equal(t, "GoValue holds the value.\n", doc.Consts["GoValue"])
}

func generateDirectiveTestPackage(t *testing.T, pkgPath string) (*gotypes.Package, *pkgdoc.PkgDoc) {
	t.Helper()

	require.NoError(t, generate([]string{pkgPath}, "pkgdata.zip"))
	archive, err := zip.OpenReader("pkgdata.zip")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, archive.Close()) })
	pkg, err := gcexportdata.Read(bytes.NewReader(readGeneratedFile(t, archive, pkgPath+".pkgexport")), token.NewFileSet(), make(map[string]*gotypes.Package), pkgPath)
	require.NoError(t, err)
	var doc pkgdoc.PkgDoc
	require.NoError(t, json.Unmarshal(readGeneratedFile(t, archive, pkgPath+".pkgdoc"), &doc))
	return pkg, &doc
}
