package server

import (
	gotypes "go/types"
	"maps"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// sourceIdent describes an identifier as written in a project source file.
type sourceIdent struct {
	ident        *ast.Ident
	file         *ast.File
	kind         protocol.DocumentHighlightKind
	called       bool
	typeSelector bool
}

// structKwargUse identifies a field binding at a keyword name.
type structKwargUse struct {
	ident *ast.Ident
	obj   gotypes.Object
}

// hasSourceSpelling reports whether ident matches the text in the current file.
func hasSourceSpelling(proj *xgo.Project, ident *ast.Ident) bool {
	if ident == nil {
		return false
	}
	file := sourceASTFile(proj, ident.Pos())
	return file != nil && xgoutil.IsSourceIdent(proj.Fset.File(ident.Pos()), file.Code, ident)
}

// sourceInfo indexes source references separately from compiler-generated uses.
// Keyword names are kept separate because their rename spelling can differ.
// Its maps and slices are shared between requests and must not be modified
// after construction.
type sourceInfo struct {
	references         map[gotypes.Object][]sourceIdent
	kwargs             map[gotypes.Object][]sourceIdent
	highlights         map[gotypes.Object][]sourceIdent
	kwargSpellingTypes map[structKwargUse]gotypes.Type
	overloadSelections map[token.Pos]gotypes.Object
}

// sourceInfoCacheKind identifies source references for one project revision.
type sourceInfoCacheKind struct{}

// buildSourceInfoCache indexes completed syntax and expression types from a stable snapshot.
func buildSourceInfoCache(proj *xgo.Project) (any, error) {
	expressionTypeInfo(proj)
	proj = proj.Snapshot()
	astPkg, _ := proj.ASTPackage()
	info, _ := expressionTypeInfo(proj)
	result := &sourceInfo{
		references:         make(map[gotypes.Object][]sourceIdent),
		kwargs:             make(map[gotypes.Object][]sourceIdent),
		highlights:         make(map[gotypes.Object][]sourceIdent),
		kwargSpellingTypes: make(map[structKwargUse]gotypes.Type),
		overloadSelections: make(map[token.Pos]gotypes.Object),
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
					selected := info.SelectedOverloads[ident].Implementation
					if selected == nil && used != overload {
						selected = xgoutil.ConcreteOverloadSelection(used).Implementation
					}
					if selected != nil {
						result.overloadSelections[ident.Pos()] = selected
					}
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
				for kwarg, kwargTargets := range callExprKwargTargets(info, call, 0) {
					// The compiler can record a field use during a failed lowering.
					seen[kwarg.Name] = true
					targets := make(map[gotypes.Object]bool)
					for _, target := range kwargTargets {
						obj := types.ObjectOrigin(kwargTargetObject(target.target))
						if obj != nil && !targets[obj] {
							targets[obj] = true
							result.kwargs[obj] = append(result.kwargs[obj], sourceIdent{ident: kwarg.Name, file: astFile, kind: Read})
							if target.target.Field != nil {
								result.kwargSpellingTypes[structKwargUse{ident: kwarg.Name, obj: obj}] = target.binding.StructSpellingType()
							}
						}
					}
				}
			}
			if ident, ok := node.(*ast.Ident); ok {
				var typeSelector bool
				if selector, ok := stack[len(stack)-1].(*ast.SelectorExpr); ok && selector.Sel == ident {
					typeSelector, _ = xgoutil.IsTypeExpr(info, selector.X)
				}
				indexIdent(sourceIdent{
					ident:        ident,
					file:         astFile,
					kind:         sourceHighlightKind(ident, stack),
					called:       sourceIdentCalled(ident, stack),
					typeSelector: typeSelector,
				})
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

// sourceIdentCalled reports whether an identifier names an explicit callee.
func sourceIdentCalled(ident *ast.Ident, parents []ast.Node) bool {
	for _, parent := range slices.Backward(parents) {
		switch parent := parent.(type) {
		case *ast.SelectorExpr, *ast.ParenExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.ErrWrapExpr:
		case *ast.CallExpr:
			return xgoutil.CallExprFunIdent(parent) == ident
		case *ast.FuncDecorator:
			return xgoutil.CallExprFunIdent(&parent.CallExpr) == ident
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
