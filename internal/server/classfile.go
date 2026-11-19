package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
)

// classTypeForFile resolves the compiler-generated owner of a classfile.
func classTypeForFile(proj *xgo.Project, file *ast.File) *gotypes.Named {
	if file == nil || !file.IsClass {
		return nil
	}
	info, _ := proj.TypeInfo()
	if info == nil {
		return nil
	}
	return info.ClassTypes[file]
}
