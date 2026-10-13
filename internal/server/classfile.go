package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
	"github.com/goplus/xgolsw/xgo"
)

// classTypeForFile resolves the type generated for a classfile using the
// compiler's naming rules and the project's framework registrations.
func classTypeForFile(proj *xgo.Project, file *ast.File) *gotypes.Named {
	if file == nil || !file.IsClass {
		return nil
	}
	info, _ := proj.TypeInfo()
	if info == nil {
		return nil
	}
	name, _ := cl.GetFileClassType(file, proj.Fset.PositionFor(file.Pos(), false).Filename, proj.Module().LookupClass)
	obj, ok := info.Pkg.Scope().Lookup(name).(*gotypes.TypeName)
	if !ok {
		return nil
	}
	return resolvedNamedType(obj.Type())
}
