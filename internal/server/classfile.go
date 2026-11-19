package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
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

// classReceiverTypeAt resolves the enclosing receiver's named type. Syntax
// outside a receiver body, such as decorators, uses the classfile owner.
func classReceiverTypeAt(info *types.Info, file *ast.File, pos token.Pos) *gotypes.Named {
	if recv := xgoutil.ClassReceiverAt(info, file, pos); recv != nil {
		return resolvedNamedType(recv)
	}
	return info.ClassTypes[file]
}
