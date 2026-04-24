package server

import (
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/pkgdoc"
)

// SpxReferencePkg is a reference to an imported package.
type SpxReferencePkg struct {
	PkgPath string
	Pkg     *pkgdoc.PkgDoc
	Node    *ast.ImportSpec
}
