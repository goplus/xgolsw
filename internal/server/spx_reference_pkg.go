package server

import (
	gopast "github.com/goplus/gop/ast"
	"github.com/goplus/goxlsw/internal/pkgdoc"
)

// SpxReferencePkg is a reference to an imported package.
type SpxReferencePkg struct {
	PkgPath string
	Pkg     *pkgdoc.PkgDoc
	Node    *gopast.ImportSpec
}
