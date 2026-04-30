package server

import (
	"fmt"
	"go/constant"
	gotypes "go/types"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// timeDurationUnitDecl is the built-in unit declaration for time.Duration.
const timeDurationUnitDecl = "ns=1,us=1000,\u00b5s=1000,ms=1000000,s=1000000000,m=60000000000,h=3600000000000,d=86400000000000"

// xgoUnitSpec describes one unit suffix available for an XGo unit literal.
type xgoUnitSpec struct {
	Name       string
	Factor     string
	SourceType gotypes.Type
}

// xgoUnitSpecsForType returns the unit suffixes available for typ.
func xgoUnitSpecsForType(typ gotypes.Type) []xgoUnitSpec {
	obj, ok := xgoUnitTypeName(typ)
	if !ok {
		return nil
	}

	decl, ok := xgoUnitDeclForTypeName(obj)
	if !ok {
		return nil
	}
	return parseXGoUnitDecl(decl, typ)
}

// xgoUnitTypeName returns the type name object that owns typ's unit declaration.
func xgoUnitTypeName(typ gotypes.Type) (*gotypes.TypeName, bool) {
	if typ == nil {
		return nil, false
	}
	switch typ := typ.(type) {
	case *gotypes.Named:
		return typ.Obj(), typ.Obj() != nil
	case *gotypes.Alias:
		return typ.Obj(), typ.Obj() != nil
	default:
		return nil, false
	}
}

// xgoUnitSpecForType returns the unit suffix named name for typ.
func xgoUnitSpecForType(typ gotypes.Type, name string) (xgoUnitSpec, bool) {
	for _, spec := range xgoUnitSpecsForType(typ) {
		if spec.Name == name {
			return spec, true
		}
	}
	return xgoUnitSpec{}, false
}

// xgoUnitDeclForTypeName returns the raw XGo unit declaration string for obj.
func xgoUnitDeclForTypeName(obj *gotypes.TypeName) (string, bool) {
	if obj == nil {
		return "", false
	}
	pkg := obj.Pkg()
	if pkg == nil {
		return "", false
	}

	if pkg.Path() == "time" && obj.Name() == "Duration" {
		return timeDurationUnitDecl, true
	}

	// gogen.ValWithUnit imports non-time unit declarations by package path.
	// Main-package unit declarations are not accepted by the compiler.
	if pkg.Path() == "" || xgoutil.IsMainPkg(pkg) {
		return "", false
	}

	scope := pkg.Scope()
	if scope == nil {
		return "", false
	}
	unitObj, ok := scope.Lookup("XGou_" + obj.Name()).(*gotypes.Const)
	if !ok {
		return "", false
	}
	val := unitObj.Val()
	if val == nil || val.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(val), true
}

// parseXGoUnitDecl parses an XGo unit declaration into ordered unit specs.
func parseXGoUnitDecl(decl string, typ gotypes.Type) []xgoUnitSpec {
	parts := strings.Split(decl, ",")
	specs := make([]xgoUnitSpec, 0, len(parts))
	for _, part := range parts {
		name, factor, ok := strings.Cut(part, "=")
		if !ok || name == "" || factor == "" {
			continue
		}
		specs = append(specs, xgoUnitSpec{
			Name:       name,
			Factor:     factor,
			SourceType: typ,
		})
	}
	return specs
}

// xgoUnitStart returns the source position where lit's unit suffix starts.
func xgoUnitStart(lit *ast.NumberUnitLit) token.Pos {
	return lit.ValuePos + token.Pos(len(lit.Value))
}

// isXGoUnitNumberKind reports whether kind can carry an XGo unit suffix.
func isXGoUnitNumberKind(kind token.Token) bool {
	return kind == token.INT || kind == token.FLOAT
}

// xgoUnitExpectedTypesAtPosition returns expected types for the unit literal at pos.
func xgoUnitExpectedTypesAtPosition(proj *xgo.Project, typeInfo *types.Info, astFile *ast.File, pos token.Pos) []gotypes.Type {
	path, _ := xgoutil.PathEnclosingInterval(astFile, pos-1, pos)
	lit := xgoUnitLiteralAtPath(path, pos)
	if lit == nil {
		return nil
	}
	return xgoUnitLiteralExpectedTypes(proj, typeInfo, path, lit)
}

// xgoUnitLiteralAtPath returns the unit-capable literal at path and pos.
func xgoUnitLiteralAtPath(path []ast.Node, pos token.Pos) ast.Expr {
	if lit := xgoutil.EnclosingNode[*ast.NumberUnitLit](path); lit != nil {
		if isXGoUnitNumberKind(lit.Kind) {
			return lit
		}
		return nil
	}
	lit := xgoutil.EnclosingNode[*ast.BasicLit](path)
	if lit == nil || !isXGoUnitNumberKind(lit.Kind) || pos != lit.End() {
		return nil
	}
	return lit
}

// xgoUnitLiteralExpectedTypes returns expected types for lit from its syntax context.
func xgoUnitLiteralExpectedTypes(proj *xgo.Project, typeInfo *types.Info, path []ast.Node, lit ast.Expr) []gotypes.Type {
	if typeInfo == nil || lit == nil {
		return nil
	}

	var expectedTypes []gotypes.Type
	appendType := func(typ gotypes.Type) {
		if !xgoutil.IsValidType(typ) {
			return
		}
		for _, existing := range expectedTypes {
			if sameXGoUnitExpectedType(existing, typ) {
				return
			}
		}
		expectedTypes = append(expectedTypes, typ)
	}

	for _, node := range path {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			continue
		}
		for resolvedArg := range resolvedCallExprArgs(proj, typeInfo, call) {
			if resolvedArg.Arg == lit {
				appendType(xgoUnitExpectedTypeForResolvedArg(resolvedArg))
			}
		}
		for _, overload := range callExprFuncOverloads(proj, typeInfo, call) {
			if !overloadMatchesCallExpr(typeInfo, call, overload, -1) {
				continue
			}
			for resolvedArg := range resolvedOverloadCallExprArgs(typeInfo, call, overload) {
				if resolvedArg.Arg == lit {
					appendType(xgoUnitExpectedTypeForResolvedArg(resolvedArg))
				}
			}
		}
	}
	return expectedTypes
}

// sameXGoUnitExpectedType reports whether a and b identify the same expected
// unit type.
func sameXGoUnitExpectedType(a, b gotypes.Type) bool {
	aObj, aOK := xgoUnitTypeName(a)
	bObj, bOK := xgoUnitTypeName(b)
	if aOK || bOK {
		return aOK && bOK && sameXGoUnitTypeName(aObj, bObj)
	}
	return gotypes.Identical(a, b)
}

// sameXGoUnitTypeName reports whether a and b name the same type.
func sameXGoUnitTypeName(a, b *gotypes.TypeName) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil || a.Name() != b.Name() {
		return false
	}
	aPkg := a.Pkg()
	bPkg := b.Pkg()
	if aPkg == nil || bPkg == nil {
		return aPkg == bPkg
	}
	return aPkg.Path() == bPkg.Path()
}

// xgoUnitExpectedTypeForResolvedArg returns the expected type only for source
// contexts that the XGo compiler accepts for unit literals.
func xgoUnitExpectedTypeForResolvedArg(resolvedArg xgoutil.ResolvedCallExprArg) gotypes.Type {
	switch resolvedArg.Kind {
	case xgoutil.ResolvedCallExprArgPositional:
		return resolvedArg.ExpectedType
	case xgoutil.ResolvedCallExprArgKeyword:
		if resolvedArg.KwargTarget != nil && resolvedArg.KwargTarget.Kind == xgoutil.ResolvedCallExprKwargTargetInterfaceMethod {
			return resolvedArg.ExpectedType
		}
	}
	return nil
}

// hoverForXGoUnit returns hover content for the unit suffix at position.
func hoverForXGoUnit(proj *xgo.Project, typeInfo *types.Info, astFile *ast.File, position token.Position) *Hover {
	tokenFile := xgoutil.NodeTokenFile(proj.Fset, astFile)
	pos := tokenFile.Pos(position.Offset)
	path, _ := xgoutil.PathEnclosingInterval(astFile, pos, pos)
	lit := xgoutil.EnclosingNode[*ast.NumberUnitLit](path)
	if lit == nil {
		return nil
	}
	unitStart := xgoUnitStart(lit)
	if pos < unitStart || pos > lit.End() {
		return nil
	}

	for _, expectedType := range xgoUnitLiteralExpectedTypes(proj, typeInfo, path, lit) {
		spec, ok := xgoUnitSpecForType(expectedType, lit.Unit)
		if !ok {
			continue
		}
		return &Hover{
			Contents: MarkupContent{
				Kind: Markdown,
				Value: fmt.Sprintf(
					"unit `%s` for `%s`\n\nMultiplier: `%s`",
					spec.Name,
					GetSimplifiedTypeString(spec.SourceType),
					spec.Factor,
				),
			},
			Range: RangeForPosEnd(proj, unitStart, lit.End()),
		}
	}
	return nil
}
