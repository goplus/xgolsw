package testframework

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"
)

// fmtSource provides the compiler's required builtin declarations without a host toolchain.
//
//go:embed testdata/fmt.go
var fmtSource string

// NewPkgDataZip builds an archive containing framework exports and documentation
// together with the minimal fmt exports required by XGo builtins.
func NewPkgDataZip(t testing.TB) []byte {
	t.Helper()

	fset := token.NewFileSet()
	pkg, err := NewImporter(t, fset).Import(PkgPath)
	require.NoError(t, err)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(PkgPath + ".pkgexport")
	require.NoError(t, err)
	require.NoError(t, gcexportdata.Write(w, fset, pkg))
	file, err := goparser.ParseFile(fset, "fmt.go", fmtSource, 0)
	require.NoError(t, err)
	fmtPkg, err := new(gotypes.Config).Check("fmt", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	w, err = zw.Create("fmt.pkgexport")
	require.NoError(t, err)
	require.NoError(t, gcexportdata.Write(w, fset, fmtPkg))
	w, err = zw.Create(PkgPath + ".pkgdoc")
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(w).Encode(NewPkgDoc(t)))
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
