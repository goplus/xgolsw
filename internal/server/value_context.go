package server

import (
	gotypes "go/types"
	"iter"
	"slices"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// tupleElementType returns an element of an XGo tuple value. Multiple function
// results use go/types.Tuple instead and are not tuple values.
func tupleElementType(typ gotypes.Type, index int) gotypes.Type {
	if !xgoutil.IsValidType(typ) || !new(gogen.CodeBuilder).IsTupleType(typ) {
		return nil
	}
	tuple := typ.Underlying().(*gotypes.Struct)
	if index < 0 || index >= tuple.NumFields() {
		return nil
	}
	return tuple.Field(index).Type()
}

// valueElementTypes yields values with their contextual types, descending into
// literals without changing the surrounding arity.
func valueElementTypes(info *types.Info, expr ast.Expr, typ gotypes.Type) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		if expr == nil {
			return
		}
		if literal, ok := astutil.Unparen(expr).(*ast.CompositeLit); ok {
			if explicit := info.TypeOf(literal.Type); xgoutil.IsValidType(explicit) {
				typ = explicit
			}
		}
		if !xgoutil.IsValidType(typ) {
			return
		}
		typ = xgoutil.DerefType(typ)
		switch astutil.Unparen(expr).(type) {
		case *ast.TupleLit, *ast.SliceLit, *ast.MatrixLit, *ast.CompositeLit:
			for element, elementType := range literalElementTypes(expr, typ) {
				for value, valueType := range valueElementTypes(info, element, elementType) {
					if !yield(value, valueType) {
						return
					}
				}
			}
		default:
			yield(expr, typ)
		}
	}
}

// literalElementTypes yields direct literal elements and their expected types.
// Struct field names are not values. Map keys and array indices are values.
func literalElementTypes(expr ast.Expr, typ gotypes.Type) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		if !xgoutil.IsValidType(typ) {
			return
		}
		typ = xgoutil.DerefType(typ)
		switch literal := astutil.Unparen(expr).(type) {
		case *ast.TupleLit:
			for i, element := range literal.Elts {
				if !yield(element, tupleElementType(typ, i)) {
					return
				}
			}
		case *ast.SliceLit:
			for _, element := range literal.Elts {
				if !yield(element, collectionElementType(typ)) {
					return
				}
			}
		case *ast.MatrixLit:
			for _, row := range literal.Elts {
				for _, element := range row {
					if !yield(element, collectionElementType(collectionElementType(typ))) {
						return
					}
				}
			}
		case *ast.CompositeLit:
			for index, element := range literal.Elts {
				keyType, valueType := compositeElementTypes(typ, index, element)
				if kv, ok := element.(*ast.KeyValueExpr); ok {
					if keyType != nil && !yield(kv.Key, keyType) {
						return
					}
					element = kv.Value
				}
				if !yield(element, valueType) {
					return
				}
			}
		}
	}
}

// compositeElementTypes resolves the key and value types of one composite
// element. Struct field names are not values.
func compositeElementTypes(typ gotypes.Type, index int, element ast.Expr) (keyType, valueType gotypes.Type) {
	var key ast.Expr
	if kv, ok := element.(*ast.KeyValueExpr); ok {
		key = kv.Key
	}
	switch typ := xgoutil.DerefType(typ).Underlying().(type) {
	case *gotypes.Struct:
		if key == nil && index < typ.NumFields() {
			valueType = typ.Field(index).Type()
		} else if name, ok := key.(*ast.Ident); ok {
			for field := range typ.Fields() {
				if field.Name() == name.Name {
					valueType = field.Type()
					break
				}
			}
		}
	case *gotypes.Map:
		keyType, valueType = typ.Key(), typ.Elem()
	case *gotypes.Array, *gotypes.Slice:
		keyType, valueType = gotypes.Typ[gotypes.Int], collectionElementType(typ)
	}
	return
}

// literalTargetType resolves a literal operand without enumerating siblings.
// parent identifies a keyed element when target is its key or value.
func literalTargetType(literal ast.Node, typ gotypes.Type, target ast.Expr, parent ast.Node) gotypes.Type {
	typ = xgoutil.DerefType(typ)
	switch literal := literal.(type) {
	case *ast.SliceLit:
		return collectionElementType(typ)
	case *ast.MatrixLit:
		return collectionElementType(collectionElementType(typ))
	case *ast.TupleLit:
		return tupleElementType(typ, xgoutil.SourceExprIndex(literal.Elts, target))
	case *ast.CompositeLit:
		element := target
		if kv, ok := parent.(*ast.KeyValueExpr); ok {
			element = kv
		}
		index := xgoutil.SourceExprIndex(literal.Elts, element)
		if index < 0 {
			return nil
		}
		keyType, valueType := compositeElementTypes(typ, index, element)
		if kv, ok := element.(*ast.KeyValueExpr); ok && kv.Key == target {
			return keyType
		}
		return valueType
	}
	return nil
}

// literalTypes resolves a literal's element container. An explicit composite
// type takes precedence over an outer interface or assignment type.
func literalTypes(info *types.Info, path []ast.Node) []gotypes.Type {
	expr := path[0].(ast.Expr)
	if literal, ok := expr.(*ast.CompositeLit); ok {
		if typ := info.TypeOf(literal.Type); xgoutil.IsValidType(typ) {
			return []gotypes.Type{typ}
		}
	}
	if types := expectedExprTypes(info, path); len(types) > 0 {
		return types
	}
	if typ := info.TypeOf(expr); xgoutil.IsValidType(typ) {
		return []gotypes.Type{typ}
	}
	return nil
}

// valueListType selects a value's expected type. A sole expression supplying
// multiple destinations expects all their types as a result tuple.
func valueListType(count, index int, targets []gotypes.Type) gotypes.Type {
	if count == 1 && len(targets) > 1 {
		vars := make([]*gotypes.Var, len(targets))
		for i, typ := range targets {
			if !xgoutil.IsValidType(typ) {
				return nil
			}
			vars[i] = gotypes.NewVar(token.NoPos, nil, "", typ)
		}
		return gotypes.NewTuple(vars...)
	}
	if index >= 0 && index < len(targets) {
		return targets[index]
	}
	return nil
}

// resultTypes returns the result types of a resolved function signature.
func resultTypes(sig *gotypes.Signature) []gotypes.Type {
	if sig == nil {
		return nil
	}
	result := make([]gotypes.Type, sig.Results().Len())
	for i := range result {
		result[i] = sig.Results().At(i).Type()
	}
	return result
}

// collectionElementType returns the element type of an array or slice.
func collectionElementType(typ gotypes.Type) gotypes.Type {
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	switch typ := typ.Underlying().(type) {
	case *gotypes.Array:
		return typ.Elem()
	case *gotypes.Slice:
		return typ.Elem()
	}
	return nil
}

// indexKeyType returns the key type for indexing a map, array, slice, or string.
// Type instantiation has no runtime key.
func indexKeyType(info *types.Info, expr *ast.IndexExpr) gotypes.Type {
	if info.Types[expr.Index].IsType() {
		return nil
	}
	typ := info.TypeOf(expr.X)
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	switch typ := xgoutil.DerefType(gotypes.Unalias(typ)).Underlying().(type) {
	case *gotypes.Map:
		return typ.Key()
	case *gotypes.Array, *gotypes.Slice:
		return gotypes.Typ[gotypes.Int]
	case *gotypes.Basic:
		if typ.Info()&gotypes.IsString != 0 {
			return gotypes.Typ[gotypes.Int]
		}
	}
	return nil
}

// sendValueTypes resolves a channel value or an XGo slice append operand.
func sendValueTypes(info *types.Info, stmt *ast.SendStmt) []gotypes.Type {
	typ := info.TypeOf(stmt.Chan)
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	if channel, ok := typ.Underlying().(*gotypes.Chan); ok {
		if len(stmt.Values) == 1 && !stmt.Ellipsis.IsValid() {
			return []gotypes.Type{channel.Elem()}
		}
		return nil
	}
	return appendedValueTypes(typ, stmt.Ellipsis.IsValid())
}

// binaryOperandTypes keeps comparisons and shift counts separate from the
// result type while propagating an arithmetic expression's expected type.
func binaryOperandTypes(info *types.Info, expr *ast.BinaryExpr, target ast.Expr, outer []ast.Node) []gotypes.Type {
	other := expr.X
	if target == expr.X {
		other = expr.Y
	}
	switch expr.Op {
	case token.SHL, token.SHR:
		if target == expr.Y {
			return []gotypes.Type{gotypes.Typ[gotypes.Int]}
		}
	case token.LAND, token.LOR:
		return []gotypes.Type{gotypes.Typ[gotypes.Bool]}
	default:
		if typ := info.TypeOf(other); xgoutil.IsValidType(typ) && !isUntypedType(typ) {
			return []gotypes.Type{typ}
		}
	}
	switch expr.Op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		if typ := info.TypeOf(other); xgoutil.IsValidType(typ) && typ != gotypes.Typ[gotypes.UntypedNil] {
			return []gotypes.Type{gotypes.Default(typ)}
		}
		return nil
	}
	if expected := expectedExprTypes(info, outer); len(expected) > 0 {
		return expected
	}
	return validExpectedType(info.TypeOf(expr))
}

// switchCaseTypes returns the value types accepted by the nearest expression
// switch. Type switch cases contain types rather than values.
func switchCaseTypes(info *types.Info, path []ast.Node) []gotypes.Type {
	for _, node := range path {
		switch node := node.(type) {
		case *ast.SwitchStmt:
			if node.Tag == nil {
				return []gotypes.Type{gotypes.Typ[gotypes.Bool]}
			}
			return validExpectedType(info.TypeOf(node.Tag))
		case *ast.TypeSwitchStmt:
			return nil
		}
	}
	return nil
}

// errorWrapResultType returns the single result replaced by an error default.
func errorWrapResultType(info *types.Info, expr *ast.ErrWrapExpr) gotypes.Type {
	if results, ok := info.TypeOf(expr.X).(*gotypes.Tuple); ok && results.Len() == 2 {
		return results.At(0).Type()
	}
	return nil
}

// errorWrapOperandTypes restores the trailing error consumed by ! or ?.
func errorWrapOperandTypes(info *types.Info, expr *ast.ErrWrapExpr, path []ast.Node) []gotypes.Type {
	expected := expectedExprTypes(info, path)
	if len(expected) == 0 {
		return validExpectedType(info.TypeOf(expr.X))
	}
	for i, typ := range expected {
		var results []*gotypes.Var
		if tuple, ok := typ.(*gotypes.Tuple); ok {
			results = slices.Collect(tuple.Variables())
		} else {
			results = []*gotypes.Var{gotypes.NewVar(token.NoPos, nil, "", typ)}
		}
		results = append(results, gotypes.NewVar(token.NoPos, nil, "", gotypes.Universe.Lookup("error").Type()))
		expected[i] = gotypes.NewTuple(results...)
	}
	return expected
}

// comprehensionValueTypes resolves a comprehension's element, key, or value
// separately from its collection result and iteration clauses.
func comprehensionValueTypes(info *types.Info, expr *ast.ComprehensionExpr, target ast.Expr, path []ast.Node) []gotypes.Type {
	containers := expectedExprTypes(info, path)
	containers = append(containers, validExpectedType(info.TypeOf(expr))...)
	var result []gotypes.Type
	for _, typ := range containers {
		var element gotypes.Type
		switch {
		case expr.Tok == token.LBRACK && target == expr.Elt:
			element = collectionElementType(typ)
		case expr.Tok == token.LBRACE && target == expr.Elt:
			element = typ
			if tuple, ok := typ.(*gotypes.Tuple); ok && tuple.Len() > 0 {
				element = tuple.At(0).Type()
			}
		case expr.Tok == token.LBRACE:
			kv, ok := expr.Elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			m, ok := typ.Underlying().(*gotypes.Map)
			if !ok {
				continue
			}
			if target == kv.Key {
				element = m.Key()
			} else if target == kv.Value {
				element = m.Elem()
			}
		}
		if xgoutil.IsValidType(element) {
			result = append(result, element)
		}
	}
	return deduplicateTypes(result)
}

// expectedExprTypes resolves the contextual types of path[0]. The path runs
// from the expression toward the file, as in PathEnclosingInterval.
func expectedExprTypes(info *types.Info, path []ast.Node) []gotypes.Type {
	if len(path) == 0 {
		return nil
	}
	target, ok := path[0].(ast.Expr)
	if !ok {
		return nil
	}
	for index, node := range path[1:] {
		outer := path[index+1:]
		if call := callExprFromNode(info, node); call != nil {
			if target == call.Fun {
				return validExpectedType(info.TypeOf(target))
			}
			types, _ := expectedTypesForCallArg(info, call, target)
			return types
		}
		switch node := node.(type) {
		case *ast.ParenExpr:
			if node.X == target {
				target = node
			}
		case *ast.KeyValueExpr, *ast.KwargExpr:
			// The surrounding literal or call resolves the value.
		case *ast.CondExpr:
			if target == node.Cond {
				if _, selector := node.Cond.(*ast.Ident); !selector {
					return []gotypes.Type{gotypes.Typ[gotypes.Bool]}
				}
			}
			return nil
		case *ast.ComprehensionExpr:
			return comprehensionValueTypes(info, node, target, outer)
		case *ast.ErrWrapExpr:
			if target == node.X {
				return errorWrapOperandTypes(info, node, outer)
			}
			if target == node.Default {
				if typ := errorWrapResultType(info, node); typ != nil {
					return []gotypes.Type{typ}
				}
				return expectedExprTypes(info, outer)
			}
			return nil
		case *ast.TupleLit, *ast.SliceLit, *ast.MatrixLit, *ast.CompositeLit:
			var result []gotypes.Type
			for _, typ := range literalTypes(info, outer) {
				if typ := literalTargetType(node, typ, target, path[index]); xgoutil.IsValidType(typ) {
					result = append(result, typ)
				}
			}
			// A tuple expanded into call arguments has no single target type.
			if len(result) == 0 {
				for _, parent := range outer[1:] {
					if _, ok := parent.(*ast.ParenExpr); ok {
						continue
					}
					if call := callExprFromNode(info, parent); call != nil {
						result, _ = expectedTypesForCallArg(info, call, target)
					}
					break
				}
			}
			return deduplicateTypes(result)
		case *ast.BinaryExpr:
			if target == node.X || target == node.Y {
				return binaryOperandTypes(info, node, target, outer)
			}
			return nil
		case *ast.UnaryExpr:
			if target != node.X {
				return nil
			}
			expected := expectedExprTypes(info, outer)
			if len(expected) == 0 {
				expected = validExpectedType(info.TypeOf(node))
			}
			switch node.Op {
			case token.NOT:
				return []gotypes.Type{gotypes.Typ[gotypes.Bool]}
			case token.ADD, token.SUB, token.XOR:
				return expected
			case token.AND:
				var result []gotypes.Type
				for _, typ := range expected {
					if pointer, ok := typ.Underlying().(*gotypes.Pointer); ok {
						result = append(result, pointer.Elem())
					}
				}
				return result
			case token.ARROW:
				var channels []gotypes.Type
				for _, typ := range expected {
					if results, ok := typ.(*gotypes.Tuple); ok {
						if results.Len() == 0 {
							continue
						}
						typ = results.At(0).Type()
					}
					channels = append(channels, gotypes.NewChan(gotypes.RecvOnly, typ))
				}
				return channels
			}
			return nil
		case *ast.StarExpr:
			if info.Types[node].IsType() || target != node.X {
				return nil
			}
			expected := expectedExprTypes(info, outer)
			if len(expected) == 0 {
				expected = validExpectedType(info.TypeOf(node))
			}
			for i, typ := range expected {
				expected[i] = gotypes.NewPointer(typ)
			}
			return expected
		case *ast.IndexExpr:
			if target == node.Index {
				return validExpectedType(indexKeyType(info, node))
			}
			return nil
		case *ast.SliceExpr:
			if target == node.Low || target == node.High || target == node.Max {
				return []gotypes.Type{gotypes.Typ[gotypes.Int]}
			}
			return nil
		case *ast.SendStmt:
			if slices.Contains(node.Values, target) {
				return sendValueTypes(info, node)
			}
			return nil
		case *ast.ArrayType:
			if target == node.Len {
				return []gotypes.Type{gotypes.Typ[gotypes.Int]}
			}
			return nil
		case *ast.IfStmt:
			if target == node.Cond {
				return []gotypes.Type{gotypes.Typ[gotypes.Bool]}
			}
			return nil
		case *ast.ForStmt:
			if target == node.Cond {
				return []gotypes.Type{gotypes.Typ[gotypes.Bool]}
			}
			return nil
		case *ast.ForPhrase:
			if target == node.Cond {
				return []gotypes.Type{gotypes.Typ[gotypes.Bool]}
			}
			return nil
		case *ast.CaseClause:
			if slices.Contains(node.List, target) {
				return switchCaseTypes(info, outer[1:])
			}
			return nil
		case *ast.RangeExpr:
			other := node.First
			if target == node.First || other == nil {
				other = node.Last
			}
			if typ := info.TypeOf(other); xgoutil.IsValidType(typ) {
				return []gotypes.Type{gotypes.Default(typ)}
			}
			return nil
		case *ast.ValueSpec:
			if index := slices.Index(node.Values, target); index >= 0 {
				targets := make([]gotypes.Type, len(node.Names))
				for i, name := range node.Names {
					targets[i] = info.TypeOf(name)
					if !xgoutil.IsValidType(targets[i]) {
						targets[i] = info.TypeOf(node.Type)
					}
				}
				return validExpectedType(valueListType(len(node.Values), index, targets))
			}
			return nil
		case *ast.AssignStmt:
			if index := slices.Index(node.Rhs, target); index >= 0 {
				if node.Tok == token.SHL_ASSIGN || node.Tok == token.SHR_ASSIGN {
					return []gotypes.Type{gotypes.Typ[gotypes.Int]}
				}
				targets := make([]gotypes.Type, len(node.Lhs))
				for i, lhs := range node.Lhs {
					targets[i] = info.TypeOf(lhs)
				}
				return validExpectedType(valueListType(len(node.Rhs), index, targets))
			}
			return nil
		case *ast.ReturnStmt:
			if index := slices.Index(node.Results, target); index >= 0 {
				return validExpectedType(valueListType(len(node.Results), index, resultTypes(enclosingFunctionSignature(info, outer[1:]))))
			}
			return nil
		case *ast.ArrowExpr:
			if index := slices.Index(node.Rhs, target); index >= 0 {
				return validExpectedType(valueListType(len(node.Rhs), index, resultTypes(contextualFunctionSignature(info, outer))))
			}
			return nil
		case *ast.FuncDecl, ast.Stmt, ast.Expr:
			return nil
		}
	}
	return nil
}

// valueOperands returns the direct value expressions of a node. Call arguments
// and literal elements have separate parameter and element mappings.
func valueOperands(info *types.Info, node ast.Node) []ast.Expr {
	switch node := node.(type) {
	case *ast.ValueSpec:
		return node.Values
	case *ast.AssignStmt:
		return node.Rhs
	case *ast.ReturnStmt:
		return node.Results
	case *ast.ArrowExpr:
		return node.Rhs
	case *ast.SendStmt:
		return node.Values
	case *ast.BinaryExpr:
		return []ast.Expr{node.X, node.Y}
	case *ast.UnaryExpr:
		return []ast.Expr{node.X}
	case *ast.StarExpr:
		if !info.Types[node].IsType() {
			return []ast.Expr{node.X}
		}
	case *ast.IndexExpr:
		if indexKeyType(info, node) != nil {
			return []ast.Expr{node.X, node.Index}
		}
	case *ast.SliceExpr:
		return []ast.Expr{node.X, node.Low, node.High, node.Max}
	case *ast.TypeAssertExpr:
		return []ast.Expr{node.X}
	case *ast.AnySelectorExpr:
		return []ast.Expr{node.X}
	case *ast.CondExpr:
		// A bare identifier after @ names a selector, not a boolean value.
		if _, selector := node.Cond.(*ast.Ident); selector {
			return []ast.Expr{node.X}
		}
		return []ast.Expr{node.X, node.Cond}
	case *ast.ErrWrapExpr:
		return []ast.Expr{node.X, node.Default}
	case *ast.ComprehensionExpr:
		if kv, ok := node.Elt.(*ast.KeyValueExpr); ok {
			return []ast.Expr{kv.Key, kv.Value}
		}
		return []ast.Expr{node.Elt}
	case *ast.ForPhrase:
		return []ast.Expr{node.X, node.Cond}
	case *ast.RangeStmt:
		return []ast.Expr{node.X}
	case *ast.RangeExpr:
		return []ast.Expr{node.First, node.Last, node.Expr3}
	case *ast.ArrayType:
		return []ast.Expr{node.Len}
	case *ast.IfStmt:
		return []ast.Expr{node.Cond}
	case *ast.ForStmt:
		return []ast.Expr{node.Cond}
	case *ast.SwitchStmt:
		return []ast.Expr{node.Tag}
	case *ast.CaseClause:
		return node.List
	}
	return nil
}

// expectedValueType selects an actual value's context when syntax permits
// alternatives, such as a slice or string appended to a byte slice.
func expectedValueType(info *types.Info, expr ast.Expr, expected []gotypes.Type) gotypes.Type {
	actual := info.TypeOf(expr)
	if xgoutil.IsValidType(actual) {
		for _, typ := range expected {
			if gotypes.AssignableTo(actual, typ) {
				return typ
			}
		}
	}
	if len(expected) > 0 {
		return expected[0]
	}
	return nil
}

// validExpectedType discards unavailable types from incomplete source.
func validExpectedType(typ gotypes.Type) []gotypes.Type {
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	return []gotypes.Type{typ}
}

// contextualFunctionSignature includes lambda signatures supplied by the
// surrounding expression, which the compiler does not record in Types.
func contextualFunctionSignature(info *types.Info, path []ast.Node) *gotypes.Signature {
	expr := path[0].(ast.Expr)
	if sig := signatureType(info.TypeOf(expr)); sig != nil {
		return sig
	}
	for _, typ := range expectedExprTypes(info, path) {
		if sig := signatureType(typ); sig != nil {
			return sig
		}
	}
	return nil
}

// enclosingFunctionSignature resolves the nearest function boundary, including
// a lambda with a contextually inferred signature.
func enclosingFunctionSignature(info *types.Info, path []ast.Node) *gotypes.Signature {
	for index, node := range path {
		switch node := node.(type) {
		case *ast.LambdaExpr:
			return contextualFunctionSignature(info, path[index:])
		case *ast.FuncLit:
			sig, _ := info.TypeOf(node).(*gotypes.Signature)
			return sig
		case *ast.FuncDecl:
			if fun, _ := info.ObjectOf(node.Name).(*gotypes.Func); fun != nil {
				return fun.Signature()
			}
			return nil
		}
	}
	return nil
}

// contextualValueTypes yields operands and literal elements with their expected
// types. Index and slice receivers have no contextual type from their keys or
// bounds. The caller decides how to handle other unavailable types.
func contextualValueTypes(info *types.Info, path []ast.Node) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		switch node := path[0].(type) {
		case *ast.IndexExpr:
			if typ := indexKeyType(info, node); typ != nil {
				yield(node.Index, typ)
			}
			return
		case *ast.SliceExpr:
			for _, bound := range []ast.Expr{node.Low, node.High, node.Max} {
				if bound != nil && !yield(bound, gotypes.Typ[gotypes.Int]) {
					return
				}
			}
			return
		case *ast.CompositeLit, *ast.TupleLit, *ast.SliceLit, *ast.MatrixLit:
			for _, typ := range literalTypes(info, path) {
				for expr, elementType := range literalElementTypes(node.(ast.Expr), typ) {
					if !yield(expr, elementType) {
						return
					}
				}
			}
			return
		}
		values := valueOperands(info, path[0])
		if len(values) == 0 {
			return
		}
		valuePath := make([]ast.Node, len(path)+1)
		copy(valuePath[1:], path)
		for _, expr := range values {
			if expr == nil {
				continue
			}
			valuePath[0] = expr
			typ := expectedValueType(info, expr, expectedExprTypes(info, valuePath))
			if !yield(expr, typ) {
				return
			}
		}
	}
}
