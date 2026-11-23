package server

import (
	"fmt"
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// xgoUnitTypeName returns the type name object that owns typ's unit declaration.
func xgoUnitTypeName(typ gotypes.Type) (*gotypes.TypeName, bool) {
	if typ == nil {
		return nil, false
	}
	switch typ := typ.(type) {
	case *gotypes.Named:
		return typ.Obj(), true
	case *gotypes.Alias:
		return typ.Obj(), true
	default:
		return nil, false
	}
}

// xgoUnitStart returns the source position where lit's unit suffix starts.
func xgoUnitStart(lit *ast.NumberUnitLit) token.Pos {
	return lit.ValuePos + token.Pos(len(lit.Value))
}

// isXGoUnitNumberKind reports whether kind can carry an XGo unit suffix.
func isXGoUnitNumberKind(kind token.Token) bool {
	return kind == token.INT || kind == token.FLOAT
}

// xgoUnitSuffixFitsNumber reports whether appending suffix preserves the
// numeric token and produces a separate unit token.
func xgoUnitSuffixFitsNumber(number, suffix string) bool {
	source := []byte(number + suffix)
	file := token.NewFileSet().AddFile("", -1, len(source))
	var lexer scanner.Scanner
	lexer.Init(file, source, nil, 0)
	_, kind, value := lexer.Scan()
	if !isXGoUnitNumberKind(kind) || value != number {
		return false
	}
	_, kind, value = lexer.Scan()
	return lexer.ErrorCount == 0 && kind == token.UNIT && value == suffix
}

// xgoUnitExpectedTypesAtPosition returns expected types for the unit literal at pos.
func xgoUnitExpectedTypesAtPosition(typeInfo *types.Info, astFile *ast.File, pos token.Pos) []gotypes.Type {
	path, _ := xgoutil.PathEnclosingInterval(astFile, pos-1, pos)
	lit := xgoUnitLiteralAtPath(path, pos)
	if lit == nil {
		return nil
	}
	return xgoUnitLiteralExpectedTypes(typeInfo, path, lit)
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
func xgoUnitLiteralExpectedTypes(typeInfo *types.Info, path []ast.Node, lit ast.Expr) []gotypes.Type {
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
		call := callExprFromNode(typeInfo, node)
		if call == nil {
			continue
		}
		for resolvedArg := range xgoutil.ResolvedCallExprArgs(typeInfo, call) {
			if resolvedArg.Arg == lit {
				appendType(xgoUnitArgumentType(resolvedArg.ExpectedType, resolvedArg.KwargTarget))
			}
		}
		for _, overload := range callExprOverloads(typeInfo, call) {
			// An alternate overload can supply a replacement for the written unit.
			bound, _, matches := overload.matchArgs(typeInfo, call, lit)
			if !matches {
				continue
			}
			for resolvedArg := range bound.args(typeInfo, call) {
				if resolvedArg.Arg == lit {
					appendType(xgoUnitArgumentType(resolvedArg.ExpectedType, resolvedArg.KwargTarget))
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

// xgoUnitArgumentType returns the type used to lower a unit literal in a
// positional argument or a keyword that becomes a builder method argument.
func xgoUnitArgumentType(typ gotypes.Type, keyword *xgoutil.ResolvedCallExprKwargTarget) gotypes.Type {
	if keyword == nil {
		return typ
	}
	switch keyword.Kind {
	case xgoutil.ResolvedCallExprKwargTargetInterfaceMethod, xgoutil.ResolvedCallExprKwargTargetInterfaceSet:
		return typ
	}
	return nil
}

// hoverForXGoUnit returns hover content for the unit suffix at position.
func hoverForXGoUnit(
	proj *xgo.Project, typeInfo *types.Info, astFile *ast.File, position token.Position, markupKind MarkupKind,
) *Hover {
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

	for _, expectedType := range xgoUnitLiteralExpectedTypes(typeInfo, path, lit) {
		spec, ok := xgoutil.UnitSpecForType(expectedType, lit.Unit)
		if !ok {
			continue
		}
		unitType := newTypeDisplay(proj, astFile, pos).typeString(spec.SourceType)
		markdownValue := fmt.Sprintf(
			"unit `%s` for `%s`\n\nMultiplier: `%s`",
			spec.Name,
			unitType,
			spec.Factor,
		)
		return &Hover{
			Contents: markupContent(
				markupKind,
				markdownValue,
				fmt.Sprintf("unit %s for %s\n\nMultiplier: %s", spec.Name, unitType, spec.Factor),
			),
			Range: RangeForPosEnd(proj, unitStart, lit.End()),
		}
	}
	return nil
}
