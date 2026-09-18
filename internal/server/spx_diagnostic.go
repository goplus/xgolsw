package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
)

// spxDiagnosticPass supplies property information to analyzers for an spx project.
func spxDiagnosticPass(result *spxAnalysis) func(string, *protocol.Pass) {
	ctx := &definitionContext{proj: result.proj, framework: result.spxSymbols, frameworkResolved: true}
	propertyNamesCache := make(map[*gotypes.Named]map[string]struct{})
	return func(filename string, pass *protocol.Pass) {
		file, _ := result.proj.ASTFile(filename)
		pass.IsPropertyNameType = result.isSpxPropertyNameType
		pass.GetPropertyNamesForCall = func(call *ast.CallExpr) map[string]struct{} {
			named := propertyTargetForCall(result.proj, file, call)
			if named == nil {
				return nil
			}
			if names, ok := propertyNamesCache[named]; ok {
				return names
			}
			names := make(map[string]struct{})
			for property := range ctx.propertyObjects(named) {
				names[property.Name] = struct{}{}
			}
			propertyNamesCache[named] = names
			return names
		}
	}
}
