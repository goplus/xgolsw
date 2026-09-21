package server

import (
	gotypes "go/types"
	"maps"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// sourceIdent describes an identifier as written in a project source file.
type sourceIdent struct {
	ident  *ast.Ident
	file   *ast.File
	kind   protocol.DocumentHighlightKind
	called bool
}

// sourceInfo indexes source references separately from compiler-generated uses.
// Keyword names are kept separate because their rename spelling can differ.
// Its maps and slices are shared between requests and must not be modified
// after construction.
type sourceInfo struct {
	references map[gotypes.Object][]sourceIdent
	kwargs     map[gotypes.Object][]sourceIdent
	highlights map[gotypes.Object][]sourceIdent
}

// sourceInfoCacheKind identifies source references for one project revision.
type sourceInfoCacheKind struct{}

// buildSourceInfoCache indexes completed syntax and types from a stable snapshot.
func buildSourceInfoCache(proj *xgo.Project) (any, error) {
	proj.TypeInfo()
	proj = proj.Snapshot()
	astPkg, _ := proj.ASTPackage()
	info, _ := proj.TypeInfo()
	result := &sourceInfo{
		references: make(map[gotypes.Object][]sourceIdent),
		kwargs:     make(map[gotypes.Object][]sourceIdent),
		highlights: make(map[gotypes.Object][]sourceIdent),
	}
	if astPkg == nil || info == nil {
		return result, nil
	}
	seen := make(map[*ast.Ident]bool)
	for _, filename := range slices.Sorted(maps.Keys(astPkg.Files)) {
		astFile := astPkg.Files[filename]
		file := xgoutil.NodeTokenFile(proj.Fset, astFile)
		if file == nil {
			continue
		}
		indexIdent := func(ref sourceIdent) {
			ident := ref.ident
			if seen[ident] || !xgoutil.IsSourceIdent(file, astFile.Code, ident) {
				return
			}
			seen[ident] = true
			// An embedded field has both a field definition and a type
			// use. References follow Uses, while highlights follow the
			// object selected at the source identifier.
			if used, ok := info.Uses[ident]; ok {
				if overload := info.Overloads[ident]; overload != nil {
					used = overload
				}
				if obj := info.ObjectDeclaration(used); obj != nil && ident != info.ObjToDef[obj] {
					result.references[obj] = append(result.references[obj], ref)
				}
			}
			if obj := info.ObjectDeclaration(info.SourceObjectOf(ident)); obj != nil {
				result.highlights[obj] = append(result.highlights[obj], ref)
			}
		}
		var stack []ast.Node
		ast.Inspect(astFile, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if call, ok := node.(*ast.CallExpr); ok {
				for _, kwarg := range call.Kwargs {
					targets := make(map[gotypes.Object]bool)
					for _, target := range lookupCallExprKwargTargets(info, call, kwarg.Name.Name) {
						obj := types.ObjectOrigin(kwargTargetObject(target.target))
						if obj != nil && !targets[obj] {
							targets[obj] = true
							result.kwargs[obj] = append(result.kwargs[obj], sourceIdent{ident: kwarg.Name, file: astFile, kind: Read})
						}
					}
				}
			}
			if ident, ok := node.(*ast.Ident); ok {
				indexIdent(sourceIdent{ident: ident, file: astFile, kind: sourceHighlightKind(ident, stack), called: sourceIdentCalled(ident, stack)})
			}
			if branch, ok := node.(*ast.BranchStmt); ok {
				if call := callExprFromNode(info, branch); call != nil {
					indexIdent(sourceIdent{ident: xgoutil.CallExprFunIdent(call), file: astFile, kind: Read, called: true})
				}
			}
			stack = append(stack, node)
			return true
		})
	}
	return result, nil
}

// sourceInfoForProject returns the source index for the project's current state.
func sourceInfoForProject(proj *xgo.Project) (*sourceInfo, error) {
	data, err := proj.Cache(sourceInfoCacheKind{})
	if err != nil {
		return nil, err
	}
	return data.(*sourceInfo), nil
}

// sourceIdentCalled reports whether an identifier is the function of an explicit
// call, allowing selectors and parentheses around the function expression.
func sourceIdentCalled(ident *ast.Ident, parents []ast.Node) bool {
	var expr ast.Node = ident
	for _, parent := range slices.Backward(parents) {
		switch parent := parent.(type) {
		case *ast.SelectorExpr:
			if parent.Sel != expr {
				return false
			}
			expr = parent
		case *ast.ParenExpr:
			expr = parent
		case *ast.CallExpr:
			return parent.Fun == expr
		case *ast.FuncDecorator:
			return parent.Fun == expr
		default:
			return false
		}
	}
	return false
}

// sourceHighlightKind classifies a source identifier by its enclosing syntax.
func sourceHighlightKind(ident *ast.Ident, parents []ast.Node) protocol.DocumentHighlightKind {
	for _, parent := range slices.Backward(parents) {
		switch p := parent.(type) {
		case *ast.KwargExpr:
			if p.Name == ident {
				return Read
			}
		case *ast.ValueSpec:
			if slices.Contains(p.Names, ident) {
				return Write
			}
			if slices.Contains(p.Values, ast.Expr(ident)) {
				return Read
			}
		case *ast.Field:
			if slices.Contains(p.Names, ident) {
				return Write
			}
		case *ast.FuncDecl:
			if p.Name == ident {
				return Write
			}
		case *ast.OverloadFuncDecl:
			if p.Name == ident {
				return Write
			}
			return Read
		case *ast.TypeSpec:
			if p.Name == ident {
				return Write
			}
		case *ast.LabeledStmt:
			if p.Label == ident {
				return Write
			}
		case *ast.AssignStmt:
			if slices.Contains(p.Lhs, ast.Expr(ident)) {
				return Write
			}
			if slices.Contains(p.Rhs, ast.Expr(ident)) {
				return Read
			}
		case *ast.IncDecStmt:
			if p.X == ident {
				return Write
			}
		case *ast.RangeStmt:
			if p.X == ident {
				return Read
			}
			if p.Key == ident || p.Value == ident {
				return Write
			}
		case *ast.ForPhrase:
			if p.Key == ident || p.Value == ident {
				return Write
			}
			if p.X == ident || p.Cond == ident {
				return Read
			}
		case *ast.BinaryExpr,
			*ast.UnaryExpr,
			*ast.CallExpr,
			*ast.FuncDecorator,
			*ast.CompositeLit,
			*ast.IndexExpr,
			*ast.RangeExpr,
			*ast.ComprehensionExpr,
			*ast.ReturnStmt,
			*ast.SendStmt:
			return Read
		case *ast.KeyValueExpr:
			if p.Key == ident || p.Value == ident {
				return Read
			}
		case *ast.SelectorExpr:
			if p.X == ident {
				return Read
			}
		}
	}
	return Text
}
