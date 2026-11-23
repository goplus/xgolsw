package server

import (
	goast "go/ast"
	"go/constant"
	gotoken "go/token"
	gotypes "go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil/typeparams"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// indexedCompletionTypeArguments resolves written type arguments of the
// identifier or selector at the head of path. Value indices keep the
// ordinary completion context.
func (ctx *completionContext) indexedCompletionTypeArguments(path []ast.Node) []gotypes.Type {
	if len(path) < 2 {
		return nil
	}
	switch path[0].(type) {
	case *ast.Ident, *ast.SelectorExpr:
	default:
		return nil
	}
	for i, node := range path[1:] {
		if _, ok := node.(*ast.ParenExpr); ok {
			continue
		}
		indexed := typeparams.UnpackIndexExpr(node)
		if indexed == nil || astutil.Unparen(indexed.X) != path[0] {
			break
		}
		args := ctx.resolveTypeArguments(indexed.Indices, ctx.typeExpression)
		if args == nil {
			return nil
		}
		if !ctx.typeExpression && !ctx.itemSet.callResult {
			expected := expectedExprTypes(ctx.typeInfo, path[i+1:])
			if slices.ContainsFunc(expected, func(typ gotypes.Type) bool { return signatureType(typ) != nil }) {
				ctx.expectedTypes = expected
				ctx.itemSet.setExpectedTypes(expected)
			}
		}
		return args
	}
	return nil
}

// resolveTypeArguments resolves a complete list of type arguments.
func (ctx *completionContext) resolveTypeArguments(indices []ast.Expr, typeSyntax bool) []gotypes.Type {
	args := make([]gotypes.Type, 0, len(indices))
	for _, index := range indices {
		arg := ctx.resolveTypeArgument(index, typeSyntax)
		if !xgoutil.IsValidType(arg) {
			return nil
		}
		args = append(args, arg)
	}
	return args
}

// resolveTypeArgument resolves a type argument even when an incomplete
// selector prevents the checker from reaching them.
func (ctx *completionContext) resolveTypeArgument(expr ast.Expr, typeSyntax bool) gotypes.Type {
	expr = astutil.Unparen(expr)
	if isType, known := xgoutil.IsTypeExpr(ctx.typeInfo, expr); known {
		if !isType {
			return nil
		}
		if typ := ctx.typeInfo.TypeOf(expr); xgoutil.IsValidType(typ) {
			return typ
		}
	}
	switch expr := expr.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		if obj, ok := ctx.typeArgumentObject(expr, typeSyntax).(*gotypes.TypeName); ok {
			return obj.Type()
		}
	case *ast.IndexExpr, *ast.IndexListExpr:
		indexed := typeparams.UnpackIndexExpr(expr)
		if obj, ok := ctx.typeArgumentObject(indexed.X, typeSyntax).(*gotypes.TypeName); ok {
			if args := ctx.resolveTypeArguments(indexed.Indices, typeSyntax); args != nil {
				return instantiateIndexedType(obj.Type(), args, typeSyntax)
			}
		}
	case *ast.StarExpr:
		if elem := ctx.resolveTypeArgument(expr.X, typeSyntax); xgoutil.IsValidType(elem) {
			return gotypes.NewPointer(elem)
		}
	case *ast.ArrayType:
		if elem := ctx.resolveTypeArgument(expr.Elt, true); xgoutil.IsValidType(elem) {
			if expr.Len == nil {
				return gotypes.NewSlice(elem)
			}
			if length, ok := ctx.arrayTypeLength(expr.Len); ok {
				return gotypes.NewArray(elem, length)
			}
		}
	case *ast.MapType:
		key := ctx.resolveTypeArgument(expr.Key, true)
		value := ctx.resolveTypeArgument(expr.Value, true)
		if xgoutil.IsValidType(key) && xgoutil.IsValidType(value) {
			return gotypes.NewMap(key, value)
		}
	case *ast.ChanType:
		if elem := ctx.resolveTypeArgument(expr.Value, true); xgoutil.IsValidType(elem) {
			dir := gotypes.SendRecv
			switch expr.Dir {
			case ast.SEND:
				dir = gotypes.SendOnly
			case ast.RECV:
				dir = gotypes.RecvOnly
			}
			return gotypes.NewChan(dir, elem)
		}
	case *ast.FuncType:
		return ctx.resolveFunctionTypeArgument(expr)
	case *ast.StructType, *ast.InterfaceType:
		if tv := ctx.evalWrittenArgument(expr, true); tv.IsType() {
			return tv.Type
		}
	}
	return nil
}

// arrayTypeLength resolves a constant bound in the enclosing value scope.
func (ctx *completionContext) arrayTypeLength(expr ast.Expr) (int64, bool) {
	resolver := autoPropertyResolver{proj: ctx.proj}
	value := ctx.writtenValue(&resolver, astutil.Unparen(expr)).Value
	if value == nil {
		return 0, false
	}
	length, ok := constant.Int64Val(constant.ToInt(value))
	return length, ok && length >= 0
}

// evalWrittenArgument checks Go-compatible syntax left unchecked after an
// incomplete selector. Its file set and scope belong to this evaluation only.
func (ctx *completionContext) evalWrittenArgument(expr ast.Expr, typeSyntax bool) gotypes.TypeAndValue {
	start, end := ctx.tokenFile.Offset(expr.Pos()), ctx.tokenFile.Offset(expr.End())
	if start >= end || end > len(ctx.astFile.Code) {
		return gotypes.TypeAndValue{}
	}
	pkg := gotypes.NewPackage(ctx.typeInfo.Pkg.Path(), ctx.typeInfo.Pkg.Name())
	source := ctx.writtenArgumentSource(pkg, expr, typeSyntax, start, end)
	tv, err := gotypes.Eval(token.NewFileSet(), pkg, token.NoPos, source)
	if err != nil {
		return gotypes.TypeAndValue{}
	}
	return tv
}

// writtenValue recovers and caches the type and value of an expression that
// the checker may have skipped after an incomplete name.
func (ctx *completionContext) writtenValue(resolver *autoPropertyResolver, expr ast.Expr) gotypes.TypeAndValue {
	if value, ok := ctx.writtenValues[expr]; ok {
		return value
	}
	value := ctx.typeInfo.Types[expr]
	value.Type = ctx.typeInfo.TypeOf(expr)
	if !xgoutil.IsValidType(value.Type) {
		if value = ctx.writtenOperandValue(resolver, expr); value.Type == nil {
			value = ctx.evalWrittenArgument(expr, false)
		}
	}
	if ctx.writtenValues == nil {
		ctx.writtenValues = make(map[ast.Expr]gotypes.TypeAndValue)
	}
	ctx.writtenValues[expr] = value
	return value
}

// writtenOperandValue resolves XGo selectors, implicit calls, and operators
// that the Go evaluator cannot recover from an unchecked operand.
func (ctx *completionContext) writtenOperandValue(resolver *autoPropertyResolver, expr ast.Expr) gotypes.TypeAndValue {
	packageAlias := func(obj gotypes.Object, name string) autoProperty {
		if isAliasCallable(obj) && obj.Name() != name && xgoutil.FunctionAliasName(obj.Name()) == name {
			return resolver.resolvePackageObject(obj)
		}
		return autoProperty{}
	}
	switch expr := astutil.Unparen(expr).(type) {
	case *ast.Ident:
		name := expr.Name
		if visible, typ := ctx.typeDisplay.sourceLookup(name); visible != nil && visible.Parent() != gotypes.Universe {
			if _, variable := visible.(*gotypes.Var); variable && typ != nil {
				return writtenVariableValue(typ)
			}
			return gotypes.TypeAndValue{Type: typ}
		}
		upper := xgoutil.UpperFirstASCII(name)
		if upper != name {
			if alias := packageAlias(ctx.typeInfo.Pkg.Scope().Lookup(upper), name); alias.exists {
				return gotypes.TypeAndValue{Type: alias.typ}
			}
			return gotypes.TypeAndValue{Type: packageAlias(ctx.unqualifiedTypeArgumentObject(name, false), name).typ}
		}
	case *ast.SelectorExpr:
		name := expr.Sel.Name
		if ident, ok := astutil.Unparen(expr.X).(*ast.Ident); ok {
			if pkg := ctx.selectorPackage(ident, false); pkg != nil {
				obj := pkg.Imported().Scope().Lookup(xgoutil.UpperFirstASCII(name))
				return gotypes.TypeAndValue{Type: packageAlias(obj, name).typ}
			}
		}
		member, typeSelector := ctx.writtenSelectorMember(resolver, expr)
		if variable, ok := member.object.(*gotypes.Var); ok {
			if typeSelector {
				return writtenVariableValue(member.typ)
			}
			if variable.IsField() {
				if value := ctx.evalWrittenAccess(resolver, expr); xgoutil.IsValidType(value.Type) {
					return value
				}
			}
		}
		value := gotypes.TypeAndValue{Type: member.typ}
		if c, ok := member.object.(*gotypes.Const); ok {
			value.Value = c.Val()
		}
		return value
	case *ast.BinaryExpr:
		left, right := ctx.writtenValue(resolver, expr.X), ctx.writtenValue(resolver, expr.Y)
		return resolver.operatorValue(expr.Op, left, right)
	case *ast.CallExpr:
		return ctx.writtenConversionValue(resolver, expr)
	case *ast.CompositeLit:
		if expr.Type == nil {
			return ctx.writtenCompositeValue(resolver, expr, nil)
		}
	case *ast.TupleLit, *ast.SliceLit:
		return ctx.writtenLiteralValue(resolver, expr, nil)
	case *ast.UnaryExpr:
		if expr.Op != token.AND {
			return resolver.operatorValue(expr.Op, ctx.writtenValue(resolver, expr.X))
		}
		// The Go checker needs the original literal to allow its address.
		if _, literal := astutil.Unparen(expr.X).(*ast.CompositeLit); !literal {
			return ctx.evalWrittenAccess(resolver, expr)
		}
	case *ast.IndexExpr, *ast.SliceExpr, *ast.StarExpr:
		return ctx.evalWrittenAccess(resolver, expr)
	}
	return gotypes.TypeAndValue{}
}

// writtenConversionValue recovers a conversion with written arguments. XGo decides
// whether the call uses a cast protocol. Go checks native casts and computes
// their exact constant values.
func (ctx *completionContext) writtenConversionValue(resolver *autoPropertyResolver, call *ast.CallExpr) gotypes.TypeAndValue {
	typ := ctx.resolveTypeArgument(call.Fun, false)
	if !xgoutil.IsValidType(typ) {
		return gotypes.TypeAndValue{}
	}
	// The compiler models a conversion as one parameter of the target type
	// when it decides whether to expand a tuple literal.
	params := gotypes.NewTuple(gotypes.NewParam(token.NoPos, nil, "", typ))
	args, ellipsis := xgoutil.CallExprArgs(ctx.typeInfo, call, params)
	if len(args) == 1 && len(call.Kwargs) == 0 && !ellipsis {
		switch literal := args[0].(type) {
		case *ast.TupleLit:
			// The retained tuple is constructed with the target type.
			return ctx.writtenLiteralValue(resolver, literal, typ)
		case *ast.SliceLit:
			if target, ok := writtenSliceType(typ); ok {
				// The compiler constructs the slice directly and skips the cast.
				return ctx.writtenLiteralValue(resolver, literal, target)
			}
		}
	}
	_, overloaded := gogen.CheckOverloadNamed(typ)
	operands := make([]*gogen.Element, 0, len(args))
	var operand gotypes.TypeAndValue
	for _, arg := range args {
		if overloaded {
			if argType := ctx.resolveTypeArgument(arg, false); xgoutil.IsValidType(argType) {
				operands = append(operands, &gogen.Element{Val: goast.NewIdent("_"), Type: gogen.NewTypeType(argType)})
				continue
			}
		}
		operand = ctx.writtenValue(resolver, arg)
		if operand.Type == gotypes.Typ[gotypes.Invalid] {
			return operand
		}
		if !isWrittenValueOperand(operand) {
			return gotypes.TypeAndValue{}
		}
		operands = append(operands, &gogen.Element{Val: goast.NewIdent("_"), Type: operand.Type, CVal: operand.Value})
	}
	if len(call.Kwargs) != 0 {
		var expected gotypes.Type
		if len(args) == 0 {
			expected = typ
		}
		operand = ctx.writtenConversionKeywords(resolver, call.Kwargs, expected)
		if !xgoutil.IsValidType(operand.Type) {
			return operand
		}
		operands = append(operands, &gogen.Element{Val: goast.NewIdent("_"), Type: operand.Type})
	}
	if value, native := resolver.conversionValue(typ, operands, ellipsis); !native {
		return value
	}
	if len(operands) != 1 || ellipsis {
		return gotypes.TypeAndValue{Type: gotypes.Typ[gotypes.Invalid]}
	}
	pkg := gotypes.NewPackage(ctx.typeInfo.Pkg.Path(), ctx.typeInfo.Pkg.Name())
	arg := bindWrittenValue(pkg, operand)
	const name = "conversion"
	pkg.Scope().Insert(gotypes.NewTypeName(token.NoPos, pkg, name, typ))
	return checkWrittenValueExpr(pkg, &goast.CallExpr{Fun: goast.NewIdent(name), Args: []goast.Expr{arg}})
}

// writtenConversionKeywords constructs the literal argument that a conversion
// receives from keyword lowering.
func (ctx *completionContext) writtenConversionKeywords(resolver *autoPropertyResolver, kwargs []*ast.KwargExpr, expected gotypes.Type) gotypes.TypeAndValue {
	var mapType, elem gotypes.Type
	if expected != nil {
		if _, ok := xgoutil.DerefType(expected).Underlying().(*gotypes.Struct); ok {
			return ctx.writtenStructKeywords(resolver, kwargs, expected)
		}
		switch underlying := expected.Underlying().(type) {
		case *gotypes.Map:
			mapType, elem = expected, underlying.Elem()
		case *gotypes.Interface:
			if _, named := expected.(*gotypes.Named); named {
				// The interface keyword protocol needs a receiver factory.
				return gotypes.TypeAndValue{}
			}
		}
	}
	typ := xgoutil.KeywordMapType(resolver.codeBuilder(), kwargs, mapType, func(expr ast.Expr) gotypes.Type {
		value := ctx.writtenContextualValue(resolver, expr, elem)
		if value.IsType() {
			return nil
		}
		return value.Type
	})
	return gotypes.TypeAndValue{Type: typ}
}

// writtenStructKeywords lowers keyword spellings to their struct field names.
func (ctx *completionContext) writtenStructKeywords(resolver *autoPropertyResolver, kwargs []*ast.KwargExpr, expected gotypes.Type) gotypes.TypeAndValue {
	elements := make([]ast.Expr, 0, len(kwargs))
	for _, arg := range kwargs {
		field := xgoutil.StructKeywordField(expected, ctx.typeInfo.Pkg, arg.Name.Name)
		if field == nil {
			return gotypes.TypeAndValue{Type: gotypes.Typ[gotypes.Invalid]}
		}
		elements = append(elements, &ast.KeyValueExpr{Key: ast.NewIdent(field.Name()), Value: arg.Value})
	}
	return ctx.writtenCompositeValue(resolver, &ast.CompositeLit{Elts: elements}, expected)
}

// writtenContextualValue applies the literal context supplied by a composite
// element or keyword argument.
func (ctx *completionContext) writtenContextualValue(resolver *autoPropertyResolver, expr ast.Expr, expected gotypes.Type) gotypes.TypeAndValue {
	if expected != nil {
		switch literal := astutil.Unparen(expr).(type) {
		case *ast.CompositeLit:
			if value := ctx.writtenCompositeValue(resolver, literal, expected); value.Type != nil {
				return value
			}
		case *ast.TupleLit:
			return ctx.writtenLiteralValue(resolver, literal, expected)
		case *ast.SliceLit:
			if target, ok := writtenSliceType(expected); ok {
				return ctx.writtenLiteralValue(resolver, literal, target)
			}
		}
	}
	return ctx.writtenValue(resolver, expr)
}

// writtenCompositeValue checks a composite literal using the type supplied
// by its syntax or enclosing element, or infers a map from its entries.
func (ctx *completionContext) writtenCompositeValue(resolver *autoPropertyResolver, literal *ast.CompositeLit, expected gotypes.Type) (value gotypes.TypeAndValue) {
	typ := expected
	ptr, _ := expected.(*gotypes.Pointer)
	if literal.Type != nil {
		typ = ctx.resolveTypeArgument(literal.Type, true)
		if !xgoutil.IsValidType(typ) {
			return value
		}
	} else if ptr != nil {
		typ = nil
		if _, ok := ptr.Elem().Underlying().(*gotypes.Struct); ok {
			typ = ptr.Elem()
		}
	}

	var underlying gotypes.Type
	if typ != nil {
		underlying = typ.Underlying()
	}
	var structure *gotypes.Struct
	var elem, keyType gotypes.Type
	switch shape := underlying.(type) {
	case *gotypes.Struct:
		structure = shape
	case *gotypes.Array:
		elem = shape.Elem()
	case *gotypes.Slice:
		elem = shape.Elem()
	case *gotypes.Map:
		keyType, elem = shape.Key(), shape.Elem()
	default:
		if literal.Type != nil {
			return value
		}
		typ, underlying = nil, nil
	}

	cb := resolver.codeBuilder()
	stack := cb.InternalStack()
	depth := stack.Len()
	value.Type = gotypes.Typ[gotypes.Invalid]
	defer func() {
		stack.SetLen(depth)
		repanicUnexpectedGogenError(recover(), false)
	}()
	keyed := slices.ContainsFunc(literal.Elts, func(expr ast.Expr) bool {
		_, ok := expr.(*ast.KeyValueExpr)
		return ok
	})
	placeholder := goast.NewIdent("_")
	for i, expr := range literal.Elts {
		pair, _ := expr.(*ast.KeyValueExpr)
		if structure != nil {
			index := i
			if keyed {
				if pair == nil {
					return value
				}
				name, ok := pair.Key.(*ast.Ident)
				if !ok {
					return value
				}
				index = cb.LookupField(structure, name.Name)
				if index < 0 {
					return value
				}
				cb.Val(index)
			}
			if index >= structure.NumFields() {
				return value
			}
			elem = structure.Field(index).Type()
		} else if pair != nil {
			key := ctx.writtenContextualValue(resolver, pair.Key, keyType)
			if !isWrittenValueOperand(key) {
				return value
			}
			cb.Val(&gogen.Element{Val: placeholder, Type: key.Type, CVal: key.Value})
		} else if typ == nil || keyType != nil {
			return value
		} else if keyed {
			cb.None()
		}
		if pair != nil {
			expr = pair.Value
		}
		element := ctx.writtenContextualValue(resolver, expr, elem)
		if !isWrittenValueOperand(element) {
			return value
		}
		cb.Val(&gogen.Element{Val: placeholder, Type: element.Type, CVal: element.Value})
	}
	arity := stack.Len() - depth
	switch underlying.(type) {
	case *gotypes.Struct:
		cb.StructLit(typ, arity, keyed)
	case *gotypes.Array:
		cb.ArrayLitEx(typ, arity, keyed)
	case *gotypes.Slice:
		cb.SliceLitEx(typ, arity, keyed)
	case *gotypes.Map, nil:
		if cb.MapLitEx(typ, arity) != nil {
			return value
		}
	}
	if ptr != nil && typ != nil && gotypes.Identical(typ, ptr.Elem()) {
		cb.UnaryOp(gotoken.AND)
	}
	return gotypes.TypeAndValue{Type: cb.Get(-1).Type}
}

// writtenSliceType returns the contextual type for a slice literal. The
// compiler recognizes direct and named slices, and infers generic elements.
func writtenSliceType(typ gotypes.Type) (gotypes.Type, bool) {
	_, alias := typ.(*gotypes.Alias)
	slice, ok := typ.Underlying().(*gotypes.Slice)
	if !ok || alias {
		return nil, false
	}
	if _, parameter := slice.Elem().(*gotypes.TypeParam); parameter {
		return nil, true
	}
	return typ, true
}

// writtenLiteralValue checks a tuple or slice literal using its contextual
// type, or lets the compiler infer the type when none is available.
func (ctx *completionContext) writtenLiteralValue(resolver *autoPropertyResolver, expr ast.Expr, typ gotypes.Type) (value gotypes.TypeAndValue) {
	var elements []ast.Expr
	var tuple bool
	switch literal := expr.(type) {
	case *ast.TupleLit:
		elements, tuple = literal.Elts, true
	case *ast.SliceLit:
		elements = literal.Elts
	}
	operands := make([]*gogen.Element, 0, len(elements))
	for _, element := range elements {
		operand := ctx.writtenValue(resolver, element)
		if operand.Type == gotypes.Typ[gotypes.Invalid] {
			return operand
		}
		if !isWrittenValueOperand(operand) {
			return value
		}
		operands = append(operands, &gogen.Element{Val: goast.NewIdent("_"), Type: operand.Type, CVal: operand.Value})
	}
	cb := resolver.codeBuilder()
	stack := cb.InternalStack()
	depth := stack.Len()
	value.Type = gotypes.Typ[gotypes.Invalid]
	defer func() {
		stack.SetLen(depth)
		repanicUnexpectedGogenError(recover(), false)
	}()
	for _, operand := range operands {
		cb.Val(operand)
	}
	if tuple {
		cb.TupleLit(typ, len(operands))
	} else {
		cb.SliceLitEx(typ, len(operands), false)
	}
	return gotypes.TypeAndValue{Type: cb.Get(-1).Type}
}

// evalWrittenAccess checks an access expression after resolving its operands
// with XGo semantics. An unresolved operand permits checking the original
// source. A rejected access must not fall back to Go's operand lookup.
func (ctx *completionContext) evalWrittenAccess(resolver *autoPropertyResolver, expr ast.Expr) gotypes.TypeAndValue {
	pkg := gotypes.NewPackage(ctx.typeInfo.Pkg.Path(), ctx.typeInfo.Pkg.Name())
	var unknown, rejected bool
	bind := func(expr ast.Expr) goast.Expr {
		if expr == nil {
			return nil
		}
		value := ctx.writtenValue(resolver, expr)
		if value.Type == gotypes.Typ[gotypes.Invalid] {
			rejected = true
			return nil
		}
		bound := bindWrittenValue(pkg, value)
		if bound == nil {
			unknown = true
		}
		return bound
	}

	var access goast.Expr
	switch expr := expr.(type) {
	case *ast.SelectorExpr:
		access = &goast.SelectorExpr{X: bind(expr.X), Sel: goast.NewIdent(expr.Sel.Name)}
	case *ast.UnaryExpr:
		access = &goast.UnaryExpr{Op: gotoken.AND, X: bind(expr.X)}
	case *ast.StarExpr:
		access = &goast.StarExpr{X: bind(expr.X)}
	case *ast.IndexExpr:
		access = &goast.IndexExpr{X: bind(expr.X), Index: bind(expr.Index)}
	case *ast.SliceExpr:
		access = &goast.SliceExpr{X: bind(expr.X), Low: bind(expr.Low), High: bind(expr.High), Max: bind(expr.Max), Slice3: expr.Slice3}
	}
	if rejected {
		return gotypes.TypeAndValue{Type: gotypes.Typ[gotypes.Invalid]}
	}
	if unknown {
		return gotypes.TypeAndValue{}
	}
	return checkWrittenValueExpr(pkg, access)
}

// isWrittenValueOperand reports whether the checker recovered a single value.
func isWrittenValueOperand(value gotypes.TypeAndValue) bool {
	_, tuple := value.Type.(*gotypes.Tuple)
	return xgoutil.IsValidType(value.Type) && !value.IsType() && !tuple && value.Type.Underlying() != nil
}

// bindWrittenValue binds an operand in a temporary Go scope, preserving its
// constant value and addressability. An unresolved value has no binding.
func bindWrittenValue(pkg *gotypes.Package, value gotypes.TypeAndValue) goast.Expr {
	if !isWrittenValueOperand(value) {
		return nil
	}
	if value.Type == gotypes.Typ[gotypes.UntypedNil] {
		return goast.NewIdent("nil")
	}
	scope := pkg.Scope()
	name := "operand" + strconv.Itoa(scope.Len())
	ident := goast.NewIdent(name)
	switch {
	case value.Value != nil:
		scope.Insert(gotypes.NewConst(token.NoPos, pkg, name, value.Type, value.Value))
	case value.Addressable():
		scope.Insert(gotypes.NewVar(token.NoPos, pkg, name, value.Type))
	default:
		// A call result preserves the non-addressability of the operand.
		result := gotypes.NewVar(token.NoPos, pkg, "", value.Type)
		signature := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(result), false)
		scope.Insert(gotypes.NewFunc(token.NoPos, pkg, name, signature))
		return &goast.CallExpr{Fun: ident}
	}
	return ident
}

// writtenVariableValue preserves the addressability of a variable resolved
// by XGo member lookup.
func writtenVariableValue(typ gotypes.Type) gotypes.TypeAndValue {
	pkg := gotypes.NewPackage("", "")
	pkg.Scope().Insert(gotypes.NewVar(token.NoPos, pkg, "value", typ))
	return checkWrittenValueExpr(pkg, goast.NewIdent("value"))
}

// checkWrittenValueExpr checks a synthetic Go expression and returns the
// invalid type when the checker rejects it.
func checkWrittenValueExpr(pkg *gotypes.Package, expr goast.Expr) gotypes.TypeAndValue {
	info := gotypes.Info{Types: make(map[goast.Expr]gotypes.TypeAndValue)}
	if err := gotypes.CheckExpr(token.NewFileSet(), pkg, token.NoPos, expr, &info); err == nil {
		if value := info.Types[expr]; value.Type != nil {
			return value
		}
	}
	return gotypes.TypeAndValue{Type: gotypes.Typ[gotypes.Invalid]}
}

// writtenSelectorMember resolves a selector on a type or value in an
// unchecked argument. The second result reports a selector on a type.
func (ctx *completionContext) writtenSelectorMember(resolver *autoPropertyResolver, expr *ast.SelectorExpr) (autoProperty, bool) {
	receiver := ctx.writtenValue(resolver, expr.X)
	if !xgoutil.IsValidType(receiver.Type) {
		return autoProperty{}, false
	}
	member := autoPropertyResolver{proj: ctx.proj, receiver: receiver.Type}
	if receiver.IsType() {
		return member.resolveTypeMember(expr.Sel.Name), true
	}
	return member.resolve(expr.Sel.Name), false
}

// writtenArgumentSource exposes source bindings to the isolated evaluator.
// Resolved array bounds are literals in the temporary source so an XGo value
// and an imported type with the same name do not share a Go scope binding.
func (ctx *completionContext) writtenArgumentSource(pkg *gotypes.Package, expr ast.Expr, typeSyntax bool, start, end int) string {
	scope := pkg.Scope()
	code := ctx.astFile.Code
	var source strings.Builder
	next := start
	replace := func(expr ast.Expr, text string) {
		offset := ctx.tokenFile.Offset(expr.Pos())
		source.Write(code[next:offset])
		source.WriteString(text)
		next = ctx.tokenFile.Offset(expr.End())
	}
	writeArrayBound := func(expr ast.Expr) bool {
		// When evaluating a value, Go also resolves declarations inside it.
		// Leave that evaluator responsible for any nested array bounds.
		if !typeSyntax {
			return false
		}
		length, ok := ctx.arrayTypeLength(expr)
		if !ok {
			return false
		}
		replace(expr, strconv.FormatInt(length, 10))
		return true
	}
	var callAliases map[*ast.Ident]string
	var bind func(ast.Expr, bool)
	bind = func(expr ast.Expr, typeSyntax bool) {
		ast.Inspect(expr, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.Field:
				bind(node.Type, true)
				return false
			case *ast.ArrayType:
				if node.Len != nil && !writeArrayBound(node.Len) {
					bind(node.Len, false)
				}
				bind(node.Elt, true)
				return false
			case *ast.CallExpr:
				if ident := xgoutil.CallExprFunIdent(node); ident != nil {
					if declared := xgoutil.UpperFirstASCII(ident.Name); declared != ident.Name {
						if callAliases == nil {
							callAliases = make(map[*ast.Ident]string)
						}
						callAliases[ident] = declared
					}
				}
			case *ast.SelectorExpr:
				if ident, ok := node.X.(*ast.Ident); ok {
					if name := ctx.selectorPackage(ident, typeSyntax); name != nil {
						scope.Insert(gotypes.NewPkgName(token.NoPos, pkg, ident.Name, name.Imported()))
						if declared := callAliases[node.Sel]; declared != "" &&
							xgoutil.IsSourceIdent(ctx.tokenFile, code, node.Sel) &&
							canCheckPackageCallInGo(name.Imported().Scope().Lookup(declared)) {
							replace(node.Sel, declared)
						}
						return false
					}
				}
			case *ast.Ident:
				if scope.Lookup(node.Name) != nil {
					return true
				}
				obj := ctx.typeArgumentObject(node, typeSyntax)
				if obj != nil && obj.Name() == node.Name && obj.Parent() != nil && obj.Type() != nil {
					// Scope.Insert leaves the original parent of a bound object untouched.
					scope.Insert(obj)
				}
			}
			return true
		})
	}
	bind(expr, typeSyntax)
	if next == start {
		return string(code[start:end])
	}
	source.Write(code[next:end])
	return source.String()
}

// canCheckPackageCallInGo reports whether obj has an ordinary signature
// whose arguments Go can check in the temporary expression.
func canCheckPackageCallInGo(obj gotypes.Object) bool {
	if obj == nil {
		return false
	}
	sig, ok := obj.Type().(*gotypes.Signature)
	if !ok {
		return false
	}
	if _, extended := gogen.CheckFuncEx(sig); extended {
		return false
	}
	for param := range sig.Params().Variables() {
		if xgoutil.SourceParamName(param) != param.Name() {
			return false
		}
	}
	return true
}

// resolveFunctionTypeArgument rebuilds the signature of a written function type.
func (ctx *completionContext) resolveFunctionTypeArgument(expr *ast.FuncType) gotypes.Type {
	params, ok := ctx.resolveFunctionTypeFields(expr.Params)
	if !ok {
		return nil
	}
	results, ok := ctx.resolveFunctionTypeFields(expr.Results)
	if !ok {
		return nil
	}
	var variadic bool
	if fields := expr.Params.List; len(fields) != 0 {
		_, variadic = fields[len(fields)-1].Type.(*ast.Ellipsis)
	}
	return gotypes.NewSignatureType(nil, nil, nil, params, results, variadic)
}

// resolveFunctionTypeFields expands each parameter or result name into a
// tuple entry, with a slice representing a variadic parameter.
func (ctx *completionContext) resolveFunctionTypeFields(fields *ast.FieldList) (*gotypes.Tuple, bool) {
	if fields == nil {
		return nil, true
	}
	var vars []*gotypes.Var
	for _, field := range fields.List {
		expr := field.Type
		ellipsis, variadic := expr.(*ast.Ellipsis)
		if variadic {
			expr = ellipsis.Elt
		}
		typ := ctx.resolveTypeArgument(expr, true)
		if !xgoutil.IsValidType(typ) {
			return nil, false
		}
		if variadic {
			typ = gotypes.NewSlice(typ)
		}
		if len(field.Names) == 0 {
			vars = append(vars, gotypes.NewParam(field.Pos(), ctx.typeInfo.Pkg, "", typ))
			continue
		}
		for _, name := range field.Names {
			vars = append(vars, gotypes.NewParam(name.Pos(), ctx.typeInfo.Pkg, name.Name, typ))
		}
	}
	return gotypes.NewTuple(vars...), true
}

// typeArgumentObject resolves the declaration rather than the recorded type of
// a name, which may already hold an instantiated type.
func (ctx *completionContext) typeArgumentObject(expr ast.Expr, typeSyntax bool) gotypes.Object {
	var obj gotypes.Object
	switch expr := astutil.Unparen(expr).(type) {
	case *ast.Ident:
		obj = ctx.typeInfo.ObjectOf(expr)
		if obj == nil {
			obj = ctx.unqualifiedTypeArgumentObject(expr.Name, typeSyntax)
		}
	case *ast.SelectorExpr:
		obj = ctx.typeInfo.ObjectOf(expr.Sel)
		if obj == nil && expr.Sel.IsExported() {
			if ident, ok := astutil.Unparen(expr.X).(*ast.Ident); ok {
				if pkg := ctx.selectorPackage(ident, typeSyntax); pkg != nil {
					obj = pkg.Imported().Scope().Lookup(expr.Sel.Name)
				}
			}
		}
	}
	return obj
}

// selectorPackage resolves an import even when the checker did not record its
// use. In value expressions, a visible local or class member can shadow it.
func (ctx *completionContext) selectorPackage(ident *ast.Ident, typeSyntax bool) *gotypes.PkgName {
	if obj := ctx.typeInfo.ObjectOf(ident); obj != nil {
		pkg, _ := obj.(*gotypes.PkgName)
		return pkg
	}
	pkg := importsForFile(ctx.proj, ctx.astFile).named[ident.Name]
	if pkg != nil && !typeSyntax {
		if visible, _ := ctx.typeDisplay.sourceLookup(ident.Name); visible != nil && visible.Parent() != gotypes.Universe {
			return nil
		}
	}
	return pkg
}

// unqualifiedTypeArgumentObject follows XGo's type lookup inside type syntax
// and expression lookup in a bracket operand. A lowercase function alias can
// supply a value index by invoking an auto-property.
func (ctx *completionContext) unqualifiedTypeArgumentObject(name string, typeSyntax bool) gotypes.Object {
	var visible gotypes.Object
	exported := name
	if typeSyntax {
		_, obj := ctx.innermostScope.LookupParent(name, ctx.pos)
		if _, ok := obj.(*gotypes.TypeName); ok {
			return obj
		}
	} else {
		visible, _ = ctx.typeDisplay.sourceLookup(name)
		if visible != nil && visible.Parent() != gotypes.Universe {
			return visible
		}
		exported = xgoutil.UpperFirstASCII(name)
	}
	if !token.IsExported(exported) {
		return visible
	}
	imports := importsForFile(ctx.proj, ctx.astFile)
	if imports.ambiguous[name] {
		return nil
	}
	for _, pkg := range imports.members {
		if obj := pkg.Scope().Lookup(exported); obj != nil && (name == exported || gogen.HasAutoProperty(obj.Type())) {
			return obj
		}
	}
	return visible
}

// indexedTypeDefinition describes a type after applying its written arguments.
func (ctx *completionContext) indexedTypeDefinition(def symbolDefinition, obj *gotypes.TypeName, args []gotypes.Type) (symbolDefinition, bool) {
	instance := instantiateIndexedType(obj.Type(), args, ctx.typeExpression)
	if instance == nil {
		return def, false
	}
	if _, overloaded := gogen.CheckOverloadNamed(obj.Type()); overloaded {
		variant := instance.(*gotypes.Named).Obj()
		def = ctx.typeDisplay.definitionForType(variant, obj.Name(), ctx.pkgDocForObject(variant))
		def.SourceObject = obj
	}
	def.TypeHint = instance
	return def, true
}

// instantiateIndexedType applies a complete type argument list to a named
// type or alias. Only type syntax tries overloaded named types in index order.
func instantiateIndexedType(typ gotypes.Type, args []gotypes.Type, typeSyntax bool) gotypes.Type {
	if group, overloaded := gogen.CheckOverloadNamed(typ); overloaded {
		if !typeSyntax {
			return nil
		}
		for _, variant := range group.Types {
			if instance := instantiateIndexedType(variant, args, typeSyntax); instance != nil {
				return instance
			}
		}
		return nil
	}
	generic, ok := typ.(interface{ TypeParams() *gotypes.TypeParamList })
	if !ok || generic.TypeParams().Len() != len(args) {
		return nil
	}
	instance, err := gotypes.Instantiate(nil, typ, args, true)
	if err != nil {
		return nil
	}
	return instance
}
