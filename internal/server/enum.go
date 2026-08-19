package server

import (
	"go/constant"
	gotoken "go/token"
	gotypes "go/types"
	"maps"
	"slices"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// enumInfo describes source-level enum types and members.
type enumInfo struct {
	typesByObject   map[*gotypes.TypeName]*enumTypeInfo
	typesByIdent    map[*ast.Ident]*enumTypeInfo
	members         []*enumMemberInfo
	membersByIdent  map[*ast.Ident]*enumMemberInfo
	membersByObject map[gotypes.Object][]*enumMemberInfo

	regularConstsByIdent  map[*ast.Ident]*gotypes.Const
	regularConstsByObject map[*gotypes.Const]struct{}
}

// enumTypeInfo describes one source-level enum type.
type enumTypeInfo struct {
	name    string
	object  *gotypes.TypeName
	named   *gotypes.Named
	members []*enumMemberInfo
}

// enumMemberInfo describes one source-level enum member.
type enumMemberInfo struct {
	owner  *enumTypeInfo
	ident  *ast.Ident
	object *gotypes.Const
	doc    string
}

type enumContextStatus uint8

const (
	enumContextUnknown enumContextStatus = iota
	enumContextAllowed
	enumContextDisallowed
)

type enumTypePathPartKind uint8

const (
	enumTypePathElement enumTypePathPartKind = iota
	enumTypePathMapKey
	enumTypePathMapValue
	enumTypePathFunctionResult
)

type enumTypePathPart struct {
	kind  enumTypePathPartKind
	index int
}

// enumIdentContext describes how an identifier can be used as an enum member
// in its surrounding expression.
type enumIdentContext struct {
	status               enumContextStatus
	expectedTypes        []gotypes.Type
	basicTypeConstraints []gotypes.BasicInfo
	allowConversion      bool
}

// newEnumInfo builds source-level enum information from an AST package.
func newEnumInfo(astPkg *ast.Package, typeInfo *types.Info) *enumInfo {
	info := &enumInfo{
		typesByObject:   make(map[*gotypes.TypeName]*enumTypeInfo),
		typesByIdent:    make(map[*ast.Ident]*enumTypeInfo),
		membersByIdent:  make(map[*ast.Ident]*enumMemberInfo),
		membersByObject: make(map[gotypes.Object][]*enumMemberInfo),

		regularConstsByIdent:  make(map[*ast.Ident]*gotypes.Const),
		regularConstsByObject: make(map[*gotypes.Const]struct{}),
	}
	if astPkg == nil || typeInfo == nil {
		return info
	}

	for _, filename := range slices.Sorted(maps.Keys(astPkg.Files)) {
		astFile := astPkg.Files[filename]
		ast.Inspect(astFile, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.GenDecl:
				if node.Tok == token.CONST {
					info.addRegularConsts(node.Specs, typeInfo)
				}
			case *ast.TypeSpec:
				if enumType, ok := node.Type.(*ast.EnumType); ok {
					info.addEnumType(node, enumType, typeInfo)
				}
			}
			return true
		})
	}

	return info
}

// addRegularConsts indexes ordinary const declarations.
func (i *enumInfo) addRegularConsts(specs []ast.Spec, typeInfo *types.Info) {
	for _, spec := range specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, ident := range valueSpec.Names {
			if ident.Name == "_" {
				continue
			}
			constant := constObjectForIdent(typeInfo, ident)
			i.regularConstsByIdent[ident] = constant
			if constant != nil {
				i.regularConstsByObject[constant] = struct{}{}
			}
		}
	}
}

// addEnumType indexes one source-level enum type and its members.
func (i *enumInfo) addEnumType(typeSpec *ast.TypeSpec, enumType *ast.EnumType, typeInfo *types.Info) {
	typeName, _ := typeInfo.ObjectOf(typeSpec.Name).(*gotypes.TypeName)
	owner := &enumTypeInfo{
		name:   typeSpec.Name.Name,
		object: typeName,
	}
	i.typesByIdent[typeSpec.Name] = owner
	if typeName != nil {
		owner.named = resolvedNamedType(typeName.Type())
		i.typesByObject[typeName] = owner
	}

	for _, spec := range enumType.Specs {
		valueSpec := spec.(*ast.ValueSpec)
		var doc string
		if valueSpec.Doc != nil {
			doc = valueSpec.Doc.Text()
		}
		for _, ident := range valueSpec.Names {
			if ident.Name == "_" {
				continue
			}
			constant := constObjectForIdent(typeInfo, ident)
			member := &enumMemberInfo{
				owner:  owner,
				ident:  ident,
				object: constant,
				doc:    doc,
			}
			owner.members = append(owner.members, member)
			i.members = append(i.members, member)
			i.membersByIdent[ident] = member
			if constant == nil {
				continue
			}
			if owner.named == nil {
				if named, ok := gotypes.Unalias(constant.Type()).(*gotypes.Named); ok {
					owner.object = named.Obj()
					owner.named = named
					i.typesByObject[named.Obj()] = owner
				}
			}
			i.membersByObject[constant] = append(i.membersByObject[constant], member)
		}
	}
}

// constObjectForIdent returns the constant visible under ident's source name.
func constObjectForIdent(typeInfo *types.Info, ident *ast.Ident) *gotypes.Const {
	if constant, ok := sourceObjectForIdent(typeInfo, ident).(*gotypes.Const); ok {
		return constant
	}
	constant, _ := typeInfo.ObjectOf(ident).(*gotypes.Const)
	return constant
}

// sourceObjectForIdent returns the object visible under ident's source name.
func sourceObjectForIdent(typeInfo *types.Info, ident *ast.Ident) gotypes.Object {
	if typeInfo.Pkg != nil {
		scope := typeInfo.Pkg.Scope()
		if innermost := scope.Innermost(ident.Pos()); innermost != nil {
			scope = innermost
		}
		_, obj := scope.LookupParent(ident.Name, ident.Pos())
		if obj != nil {
			return obj
		}
	}
	if obj := typeInfo.ObjectOf(ident); obj != nil {
		return obj
	}
	for node := range typeInfo.Scopes {
		astFile, ok := node.(*ast.File)
		if !ok || ident.Pos() < astFile.Pos() || ident.End() > astFile.End() || astFile.ClassFields == nil {
			continue
		}
		for _, spec := range astFile.ClassFields.Specs {
			valueSpec := spec.(*ast.ValueSpec)
			for _, name := range valueSpec.Names {
				if name.Name == ident.Name {
					return typeInfo.ObjectOf(name)
				}
			}
		}
	}
	return nil
}

// sourceTypeForExpr returns the recorded type of expr, falling back to the
// source-visible object for identifiers omitted by the XGo recorder.
func sourceTypeForExpr(typeInfo *types.Info, expr ast.Expr) gotypes.Type {
	if typ := typeInfo.TypeOf(expr); xgoutil.IsValidType(typ) {
		return typ
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if obj := sourceObjectForIdent(typeInfo, ident); obj != nil {
			return obj.Type()
		}
	}
	return nil
}

// typeFor returns source-level enum information for typ.
func (i *enumInfo) typeFor(typ gotypes.Type) *enumTypeInfo {
	named, ok := gotypes.Unalias(typ).(*gotypes.Named)
	if !ok {
		return nil
	}
	return i.typesByObject[named.Obj()]
}

// declarationType returns the enum type declared by ident.
func (i *enumInfo) declarationType(ident *ast.Ident) *enumTypeInfo {
	return i.typesByIdent[ident]
}

// declarationMember returns the enum member declared by ident.
func (i *enumInfo) declarationMember(ident *ast.Ident) *enumMemberInfo {
	return i.membersByIdent[ident]
}

// declarationMemberAt returns the enum member declaration covering pos.
func (i *enumInfo) declarationMemberAt(pos token.Pos) *enumMemberInfo {
	for _, member := range i.members {
		if member.ident.Pos() <= pos && pos < member.ident.End() {
			return member
		}
	}
	return nil
}

// membersForObject returns source-level enum members represented by obj.
func (i *enumInfo) membersForObject(obj gotypes.Object) []*enumMemberInfo {
	return i.membersByObject[obj]
}

// isRegularConstDeclaration reports whether ident declares an ordinary constant.
func (i *enumInfo) isRegularConstDeclaration(ident *ast.Ident) bool {
	_, ok := i.regularConstsByIdent[ident]
	return ok
}

// objectForIdent returns the object denoted by ident, including indexed
// ordinary const declarations.
func (i *enumInfo) objectForIdent(typeInfo *types.Info, ident *ast.Ident) gotypes.Object {
	if obj := typeInfo.ObjectOf(ident); obj != nil {
		return obj
	}
	if constant := i.regularConstsByIdent[ident]; constant != nil {
		return constant
	}
	return nil
}

// regularConstDeclarationAt returns the ordinary const declaration covering pos.
func (i *enumInfo) regularConstDeclarationAt(pos token.Pos) (*ast.Ident, *gotypes.Const) {
	for ident, obj := range i.regularConstsByIdent {
		if obj != nil && ident.Pos() <= pos && pos < ident.End() {
			return ident, obj
		}
	}
	return nil, nil
}

// isRegularConstObject reports whether obj has an ordinary const declaration.
func (i *enumInfo) isRegularConstObject(obj gotypes.Object) bool {
	constant, ok := obj.(*gotypes.Const)
	if !ok {
		return false
	}
	_, ok = i.regularConstsByObject[constant]
	return ok
}

// isSyntheticObject reports whether obj is an XGo-generated enum constant.
func (i *enumInfo) isSyntheticObject(obj gotypes.Object) bool {
	constant, ok := obj.(*gotypes.Const)
	return ok && !constant.Pos().IsValid() && i.typeFor(constant.Type()) != nil
}

// membersForExpectedTypes selects members whose enum types are expected at a use.
func (i *enumInfo) membersForExpectedTypes(members []*enumMemberInfo, expectedTypes []gotypes.Type) []*enumMemberInfo {
	expectedOwners := make(map[*enumTypeInfo]struct{})
	for _, typ := range expectedTypes {
		if owner := i.typeFor(typ); owner != nil {
			expectedOwners[owner] = struct{}{}
		}
	}
	if len(expectedOwners) == 0 {
		return nil
	}
	selected := make([]*enumMemberInfo, 0, len(members))
	for _, member := range members {
		if _, ok := expectedOwners[member.owner]; ok {
			selected = append(selected, member)
		}
	}
	return selected
}

// enumMembersForIdent resolves the source-level enum members represented by ident.
func (r *compileResult) enumMembersForIdent(typeInfo *types.Info, ident *ast.Ident) []*enumMemberInfo {
	if member := r.enumInfo.declarationMember(ident); member != nil {
		return []*enumMemberInfo{member}
	}
	if r.enumInfo.isRegularConstDeclaration(ident) {
		return nil
	}
	obj := typeInfo.ObjectOf(ident)
	members := r.enumInfo.membersForObject(obj)
	if len(members) == 0 {
		return nil
	}
	context := r.enumContextAtIdent(typeInfo, ident)
	if selected := r.enumInfo.membersForExpectedTypes(members, context.expectedTypes); len(selected) > 0 {
		return selected
	}
	if r.enumInfo.isRegularConstObject(obj) {
		return nil
	}
	return members
}

// enumContextAtIdent returns the enum context provided by ident's surrounding
// expression.
func (r *compileResult) enumContextAtIdent(typeInfo *types.Info, ident *ast.Ident) enumIdentContext {
	astPkg, _ := r.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(r.proj.Fset, astPkg, ident)
	if astFile == nil {
		return enumIdentContext{}
	}
	path, _ := xgoutil.PathEnclosingInterval(astFile, ident.Pos(), ident.End())
	var (
		target                   ast.Expr = ident
		typePath                 []enumTypePathPart
		basicTypeConstraints     []gotypes.BasicInfo
		pendingLambdaResultIndex = -1
	)
	contextForTypes := func(expectedTypes []gotypes.Type) enumIdentContext {
		context := enumIdentContext{
			status:               enumContextAllowed,
			basicTypeConstraints: slices.Clone(basicTypeConstraints),
		}
		for _, typ := range expectedTypes {
			if typ := enumTypePathTargetType(typ, typePath); typ != nil {
				context.expectedTypes = append(context.expectedTypes, typ)
			}
		}
		context.expectedTypes = deduplicateTypes(context.expectedTypes)
		return context
	}
	contextForType := func(typ gotypes.Type) enumIdentContext {
		return contextForTypes([]gotypes.Type{typ})
	}
	addBasicTypeConstraint := func(info gotypes.BasicInfo) {
		if !slices.Contains(basicTypeConstraints, info) {
			basicTypeConstraints = append(basicTypeConstraints, info)
		}
	}
	for pathIndex, node := range path {
		switch node := node.(type) {
		case *ast.ParenExpr:
			if node.X == target {
				target = node
			}
			continue
		case *ast.UnaryExpr:
			if node.X != target {
				continue
			}
			switch node.Op {
			case token.ADD, token.SUB:
				addBasicTypeConstraint(gotypes.IsNumeric)
			case token.XOR:
				addBasicTypeConstraint(gotypes.IsInteger)
			case token.NOT:
				addBasicTypeConstraint(gotypes.IsBoolean)
			default:
				return enumIdentContext{status: enumContextDisallowed}
			}
			target = node
			continue
		case *ast.StarExpr:
			if node.X == target {
				return enumIdentContext{status: enumContextDisallowed}
			}
			continue
		case *ast.ArrowExpr:
			if resultIndex := slices.Index(node.Rhs, target); resultIndex >= 0 {
				typePath = append(typePath, enumTypePathPart{
					kind:  enumTypePathFunctionResult,
					index: resultIndex,
				})
				target = node
			}
			continue
		case *ast.LambdaExpr:
			if pendingLambdaResultIndex >= 0 {
				typePath = append(typePath, enumTypePathPart{
					kind:  enumTypePathFunctionResult,
					index: pendingLambdaResultIndex,
				})
				target = node
				pendingLambdaResultIndex = -1
			}
			continue
		case *ast.SliceLit:
			if slices.Contains(node.Elts, target) {
				typePath = append(typePath, enumTypePathPart{kind: enumTypePathElement})
				target = node
			}
			continue
		case *ast.MatrixLit:
			if slices.ContainsFunc(node.Elts, func(row []ast.Expr) bool {
				return slices.Contains(row, target)
			}) {
				typePath = append(typePath,
					enumTypePathPart{kind: enumTypePathElement},
					enumTypePathPart{kind: enumTypePathElement},
				)
				target = node
			}
			continue
		case *ast.ComprehensionExpr:
			switch {
			case node.Tok == token.LBRACK && node.Elt == target:
				typePath = append(typePath, enumTypePathPart{kind: enumTypePathElement})
			case node.Tok == token.LBRACE && node.Elt == target:
			case node.Tok == token.LBRACE:
				keyValue, ok := node.Elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				switch target {
				case keyValue.Key:
					typePath = append(typePath, enumTypePathPart{kind: enumTypePathMapKey})
				case keyValue.Value:
					typePath = append(typePath, enumTypePathPart{kind: enumTypePathMapValue})
				default:
					continue
				}
			default:
				continue
			}
			target = node
			continue
		case *ast.ErrWrapExpr:
			if node.Default == target {
				if typ := enumErrWrapDefaultType(typeInfo, node); typ != nil {
					return contextForType(typ)
				}
				target = node
			}
			continue
		case *ast.ElemEllipsis:
			if node.Elt == target {
				return enumIdentContext{status: enumContextDisallowed}
			}
			continue
		}

		if call := callExprFromNode(node); call != nil {
			if builtinContext, ok := enumBuiltinCallContext(typeInfo, call, target, len(typePath) > 0); ok {
				if builtinContext.status == enumContextDisallowed {
					return builtinContext
				}
				for _, required := range builtinContext.basicTypeConstraints {
					addBasicTypeConstraint(required)
				}
				return contextForTypes(builtinContext.expectedTypes)
			}
			expected, allowConversion := enumExpectedTypesForCallArg(r.proj, typeInfo, call, target)
			if len(expected) > 0 {
				context := contextForTypes(expected)
				context.allowConversion = allowConversion && len(typePath) == 0
				return context
			}
			if slices.Contains(call.Args, target) || slices.ContainsFunc(call.Kwargs, func(kwarg *ast.KwargExpr) bool {
				return kwarg.Value == target
			}) {
				return contextForTypes(nil)
			}
		}

		switch node := node.(type) {
		case *ast.ValueSpec:
			valueIndex := slices.Index(node.Values, target)
			if valueIndex < 0 {
				continue
			}
			if len(node.Names) == 0 {
				return contextForTypes(nil)
			}
			nameIndex := min(valueIndex, len(node.Names)-1)
			if obj := typeInfo.ObjectOf(node.Names[nameIndex]); obj != nil {
				return contextForType(obj.Type())
			}
			if node.Type != nil {
				return contextForType(typeInfo.TypeOf(node.Type))
			}
			return contextForTypes(nil)
		case *ast.AssignStmt:
			valueIndex := slices.Index(node.Rhs, target)
			if valueIndex >= 0 && valueIndex < len(node.Lhs) {
				if node.Tok == token.SHL_ASSIGN || node.Tok == token.SHR_ASSIGN {
					addBasicTypeConstraint(gotypes.IsInteger)
					return contextForTypes(nil)
				}
				return contextForType(typeInfo.TypeOf(node.Lhs[valueIndex]))
			}
			if slices.Contains(node.Lhs, target) {
				return enumIdentContext{status: enumContextDisallowed}
			}
		case *ast.IncDecStmt:
			if node.X == target {
				return enumIdentContext{status: enumContextDisallowed}
			}
		case *ast.ReturnStmt:
			resultIndex := slices.Index(node.Results, target)
			if resultIndex < 0 {
				continue
			}
			sig, inLambda := enumEnclosingFunctionContext(typeInfo, path[pathIndex+1:])
			if sig != nil {
				if resultIndex < sig.Results().Len() {
					return contextForType(sig.Results().At(resultIndex).Type())
				}
				return contextForTypes(nil)
			}
			if inLambda {
				pendingLambdaResultIndex = resultIndex
				continue
			}
			return contextForTypes(nil)
		case *ast.CaseClause:
			if !slices.Contains(node.List, target) {
				continue
			}
			for _, outer := range path[pathIndex+1:] {
				switchStmt, ok := outer.(*ast.SwitchStmt)
				if ok && switchStmt.Tag != nil {
					return contextForType(typeInfo.TypeOf(switchStmt.Tag))
				}
			}
			addBasicTypeConstraint(gotypes.IsBoolean)
			return contextForTypes(nil)
		case *ast.CompositeLit:
			context := enumCompositeElementContext(typeInfo, node, target)
			if context.status == enumContextUnknown {
				continue
			}
			if context.status == enumContextDisallowed {
				return context
			}
			for _, required := range context.basicTypeConstraints {
				addBasicTypeConstraint(required)
			}
			return contextForTypes(context.expectedTypes)
		case *ast.IfStmt:
			if node.Cond == target {
				addBasicTypeConstraint(gotypes.IsBoolean)
				return contextForTypes(nil)
			}
		case *ast.ForStmt:
			if node.Cond == target {
				addBasicTypeConstraint(gotypes.IsBoolean)
				return contextForTypes(nil)
			}
		case *ast.ForPhrase:
			if node.Cond == target {
				addBasicTypeConstraint(gotypes.IsBoolean)
				return contextForTypes(nil)
			}
			if node.X == target {
				addBasicTypeConstraint(gotypes.IsInteger | gotypes.IsString)
				return contextForTypes(nil)
			}
		case *ast.RangeStmt:
			if node.X == target {
				addBasicTypeConstraint(gotypes.IsInteger | gotypes.IsString)
				return contextForTypes(nil)
			}
		case *ast.RangeExpr:
			switch target {
			case node.First:
				if typ := typeInfo.TypeOf(node.Last); xgoutil.IsValidType(typ) && !isUntypedType(typ) {
					return contextForType(typ)
				}
				addBasicTypeConstraint(gotypes.IsInteger | gotypes.IsFloat)
				return contextForTypes(nil)
			case node.Last, node.Expr3:
				if node.First == nil {
					return enumIdentContext{status: enumContextDisallowed}
				}
				if typ := typeInfo.TypeOf(node.First); xgoutil.IsValidType(typ) && !isUntypedType(typ) {
					return contextForType(typ)
				}
				return enumIdentContext{status: enumContextDisallowed}
			}
		case *ast.BinaryExpr:
			var (
				other        ast.Expr
				targetIsLeft bool
			)
			switch target {
			case node.X:
				other = node.Y
				targetIsLeft = true
			case node.Y:
				other = node.X
			default:
				continue
			}
			if node.Op == token.SHL || node.Op == token.SHR {
				addBasicTypeConstraint(gotypes.IsInteger)
				if targetIsLeft {
					target = node
					continue
				}
				return contextForTypes(nil)
			}
			var required gotypes.BasicInfo
			switch node.Op {
			case token.LAND, token.LOR:
				required = gotypes.IsBoolean
			case token.ADD:
				required = gotypes.IsNumeric | gotypes.IsString
			case token.SUB, token.MUL, token.QUO:
				required = gotypes.IsNumeric
			case token.REM, token.AND, token.OR, token.XOR, token.AND_NOT:
				required = gotypes.IsInteger
			case token.LSS, token.LEQ, token.GTR, token.GEQ:
				required = gotypes.IsOrdered
			}
			if required != 0 {
				addBasicTypeConstraint(required)
			}
			if typ := typeInfo.TypeOf(other); xgoutil.IsValidType(typ) && !isUntypedType(typ) {
				return contextForType(typ)
			}
			if required := enumBasicTypeConstraintForExpr(typeInfo, other); required != 0 {
				addBasicTypeConstraint(required)
			}
			if ident, ok := other.(*ast.Ident); ok && ident.Name == "nil" {
				return enumIdentContext{status: enumContextDisallowed}
			}
			switch node.Op {
			case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
				token.AND, token.OR, token.XOR, token.AND_NOT, token.LAND, token.LOR:
				target = node
				continue
			default:
				return contextForTypes(nil)
			}
		case *ast.IndexExpr:
			if node.X == target {
				addBasicTypeConstraint(gotypes.IsString)
				return contextForTypes(nil)
			}
			if node.Index == target {
				if typ := typeInfo.TypeOf(node.X); xgoutil.IsValidType(typ) {
					if mapType, ok := typ.Underlying().(*gotypes.Map); ok {
						return contextForType(mapType.Key())
					}
				}
				addBasicTypeConstraint(gotypes.IsInteger)
				return contextForTypes(nil)
			}
		case *ast.SliceExpr:
			if node.X == target {
				addBasicTypeConstraint(gotypes.IsString)
				target = node
				continue
			}
			if node.Low == target || node.High == target || node.Max == target {
				addBasicTypeConstraint(gotypes.IsInteger)
				return contextForTypes(nil)
			}
		case *ast.ArrayType:
			if node.Len == target {
				addBasicTypeConstraint(gotypes.IsInteger)
				return contextForTypes(nil)
			}
		case *ast.SendStmt:
			if node.Chan == target {
				return enumIdentContext{status: enumContextDisallowed}
			}
			if slices.Contains(node.Values, target) {
				if typ := typeInfo.TypeOf(node.Chan); xgoutil.IsValidType(typ) {
					switch underlying := typ.Underlying().(type) {
					case *gotypes.Chan:
						if len(node.Values) != 1 || node.Ellipsis.IsValid() {
							return enumIdentContext{status: enumContextDisallowed}
						}
						return contextForType(underlying.Elem())
					case *gotypes.Slice:
						if node.Ellipsis.IsValid() {
							if enumIsByteSlice(typ) {
								addBasicTypeConstraint(gotypes.IsString)
								return contextForTypes(nil)
							}
							return enumIdentContext{status: enumContextDisallowed}
						}
						return contextForType(underlying.Elem())
					}
				}
				return contextForTypes(nil)
			}
		}
	}
	return enumIdentContext{}
}

// enumErrWrapDefaultType returns the non-error result type replaced by the
// default expression.
func enumErrWrapDefaultType(typeInfo *types.Info, expr *ast.ErrWrapExpr) gotypes.Type {
	tuple, ok := typeInfo.TypeOf(expr.X).(*gotypes.Tuple)
	if !ok || tuple.Len() != 2 {
		return nil
	}
	return tuple.At(0).Type()
}

// enumBuiltinCallContext returns the enum context for a Go built-in argument.
// wrapped reports whether the direct argument wraps the enum identifier in a
// type-carrying expression.
func enumBuiltinCallContext(
	typeInfo *types.Info,
	call *ast.CallExpr,
	target ast.Expr,
	wrapped bool,
) (enumIdentContext, bool) {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return enumIdentContext{}, false
	}
	obj := typeInfo.ObjectOf(ident)
	if _, ok := obj.(*gotypes.Builtin); !ok && !xgoutil.IsInBuiltinPkg(obj) {
		return enumIdentContext{}, false
	}
	argIndex := slices.Index(call.Args, target)
	if argIndex < 0 {
		return enumIdentContext{}, false
	}
	name := obj.Name()
	if wrapped && name != "append" && name != "copy" {
		return enumIdentContext{}, false
	}

	context := enumIdentContext{status: enumContextAllowed}
	switch name {
	case "len":
		context.basicTypeConstraints = []gotypes.BasicInfo{gotypes.IsString}
	case "cap", "clear", "close", "new":
		context.status = enumContextDisallowed
	case "make":
		if argIndex == 0 {
			context.status = enumContextDisallowed
		} else {
			context.basicTypeConstraints = []gotypes.BasicInfo{gotypes.IsInteger}
		}
	case "complex":
		context.basicTypeConstraints = []gotypes.BasicInfo{gotypes.IsFloat}
		for index, arg := range call.Args {
			if index == argIndex {
				continue
			}
			if typ := typeInfo.TypeOf(arg); xgoutil.IsValidType(typ) && !isUntypedType(typ) {
				context.expectedTypes = []gotypes.Type{typ}
				break
			}
		}
	case "real", "imag":
		context.basicTypeConstraints = []gotypes.BasicInfo{gotypes.IsComplex}
	case "append":
		return enumAppendBuiltinCallContext(typeInfo, call, argIndex, wrapped)
	case "delete":
		if argIndex == 0 {
			context.status = enumContextDisallowed
			break
		}
		if typ := typeInfo.TypeOf(call.Args[0]); xgoutil.IsValidType(typ) {
			if mapType, ok := typ.Underlying().(*gotypes.Map); ok {
				context.expectedTypes = []gotypes.Type{mapType.Key()}
			}
		}
	case "copy":
		if wrapped {
			if len(call.Args) != 2 {
				return enumIdentContext{}, false
			}
			otherType := sourceTypeForExpr(typeInfo, call.Args[1-argIndex])
			if !xgoutil.IsValidType(otherType) {
				return enumIdentContext{}, false
			}
			if _, ok := otherType.Underlying().(*gotypes.Slice); !ok {
				return enumIdentContext{}, false
			}
			context.expectedTypes = []gotypes.Type{otherType}
		} else if argIndex == 0 {
			context.status = enumContextDisallowed
		} else if enumIsByteSlice(typeInfo.TypeOf(call.Args[0])) {
			context.basicTypeConstraints = []gotypes.BasicInfo{gotypes.IsString}
		} else {
			context.status = enumContextDisallowed
		}
	default:
		return enumIdentContext{}, false
	}
	return context, true
}

// enumAppendBuiltinCallContext returns the enum context for an append argument.
func enumAppendBuiltinCallContext(
	typeInfo *types.Info,
	call *ast.CallExpr,
	argIndex int,
	wrapped bool,
) (enumIdentContext, bool) {
	context := enumIdentContext{status: enumContextAllowed}
	if argIndex != 0 {
		containerType := typeInfo.TypeOf(call.Args[0])
		if call.Ellipsis.IsValid() {
			switch {
			case wrapped && xgoutil.IsValidType(containerType):
				context.expectedTypes = []gotypes.Type{containerType}
			case enumIsByteSlice(containerType):
				context.basicTypeConstraints = []gotypes.BasicInfo{gotypes.IsString}
			default:
				context.status = enumContextDisallowed
			}
			return context, true
		}
		if xgoutil.IsValidType(containerType) {
			if sliceType, ok := containerType.Underlying().(*gotypes.Slice); ok {
				context.expectedTypes = []gotypes.Type{sliceType.Elem()}
			}
		}
		return context, true
	}

	if !wrapped {
		context.status = enumContextDisallowed
		return context, true
	}
	if len(call.Args) == 1 {
		return enumIdentContext{}, false
	}

	firstValue := call.Args[1]
	firstValueType := sourceTypeForExpr(typeInfo, firstValue)
	if call.Ellipsis.IsValid() && len(call.Args) == 2 {
		if !xgoutil.IsValidType(firstValueType) {
			if enumBasicTypeConstraintForExpr(typeInfo, firstValue) != gotypes.IsString {
				return enumIdentContext{}, false
			}
			context.expectedTypes = []gotypes.Type{gotypes.NewSlice(gotypes.Typ[gotypes.Byte])}
			return context, true
		}
		switch underlying := firstValueType.Underlying().(type) {
		case *gotypes.Basic:
			if underlying.Info()&gotypes.IsString == 0 {
				return enumIdentContext{}, false
			}
			context.expectedTypes = []gotypes.Type{gotypes.NewSlice(gotypes.Typ[gotypes.Byte])}
		case *gotypes.Slice:
			context.expectedTypes = []gotypes.Type{firstValueType}
		default:
			return enumIdentContext{}, false
		}
		return context, true
	}
	if required := enumBasicTypeConstraintForExpr(typeInfo, firstValue); required != 0 {
		context.basicTypeConstraints = []gotypes.BasicInfo{required}
		return context, true
	}
	if !xgoutil.IsValidType(firstValueType) {
		return enumIdentContext{}, false
	}
	context.expectedTypes = []gotypes.Type{gotypes.NewSlice(firstValueType)}
	return context, true
}

// enumIsByteSlice reports whether typ is the []byte type accepted by the
// append and copy string special cases.
func enumIsByteSlice(typ gotypes.Type) bool {
	if !xgoutil.IsValidType(typ) {
		return false
	}
	sliceType, ok := typ.Underlying().(*gotypes.Slice)
	return ok && gotypes.Identical(gotypes.Unalias(sliceType.Elem()), gotypes.Typ[gotypes.Uint8])
}

// enumTypePathTargetType returns the type selected by typePath.
func enumTypePathTargetType(typ gotypes.Type, typePath []enumTypePathPart) gotypes.Type {
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	for _, part := range slices.Backward(typePath) {
		switch part.kind {
		case enumTypePathElement:
			switch underlying := typ.Underlying().(type) {
			case *gotypes.Array:
				typ = underlying.Elem()
			case *gotypes.Slice:
				typ = underlying.Elem()
			default:
				return nil
			}
		case enumTypePathMapKey:
			mapType, ok := typ.Underlying().(*gotypes.Map)
			if !ok {
				return nil
			}
			typ = mapType.Key()
		case enumTypePathMapValue:
			mapType, ok := typ.Underlying().(*gotypes.Map)
			if !ok {
				return nil
			}
			typ = mapType.Elem()
		case enumTypePathFunctionResult:
			sig, ok := typ.Underlying().(*gotypes.Signature)
			if !ok || part.index >= sig.Results().Len() {
				return nil
			}
			typ = sig.Results().At(part.index).Type()
		}
	}
	return typ
}

// enumExpectedTypesForCallArg returns expected types when target is a direct
// call argument.
func enumExpectedTypesForCallArg(
	proj *xgo.Project,
	typeInfo *types.Info,
	call *ast.CallExpr,
	target ast.Expr,
) ([]gotypes.Type, bool) {
	var expected []gotypes.Type
	expected = append(expected, enumExpectedTypesForTupleElement(proj, typeInfo, call, target)...)

	for resolvedArg := range resolvedCallExprArgs(proj, typeInfo, call) {
		if resolvedArg.Arg == target && xgoutil.IsValidType(resolvedArg.ExpectedType) {
			expected = append(expected, resolvedArg.ExpectedType)
		}
	}
	allowConversion := false
	if len(call.Args) == 1 && call.Args[0] == target {
		if tv, ok := typeInfo.Types[call.Fun]; ok && tv.IsType() && xgoutil.IsValidType(tv.Type) {
			expected = append(expected, gotypes.Unalias(tv.Type))
			allowConversion = true
		}
	}
	return deduplicateTypes(expected), allowConversion
}

// enumExpectedTypesForTupleElement returns expected types when target is an
// element of an expanded tuple argument.
func enumExpectedTypesForTupleElement(
	proj *xgo.Project,
	typeInfo *types.Info,
	call *ast.CallExpr,
	target ast.Expr,
) []gotypes.Type {
	if len(call.Args) != 1 || len(call.Kwargs) > 0 || call.Ellipsis.IsValid() {
		return nil
	}
	tuple, ok := call.Args[0].(*ast.TupleLit)
	if !ok {
		return nil
	}
	elementIndex := slices.Index(tuple.Elts, target)
	if elementIndex < 0 {
		return nil
	}

	funcs := callExprFuncOverloads(proj, typeInfo, call)
	if len(funcs) == 0 {
		if fun := xgoutil.FuncFromCallExpr(typeInfo, call); fun != nil {
			funcs = []*gotypes.Func{fun}
		}
	}
	var expected []gotypes.Type
	for _, fun := range funcs {
		sig, params := xgoutil.ResolveFuncSignatureForCall(typeInfo, call, fun)
		if sig == nil || params == nil || params.Len() == 1 && enumIsTupleType(params.At(0).Type()) {
			continue
		}
		if typ := callExprArgType(sig, params, elementIndex); xgoutil.IsValidType(typ) {
			expected = append(expected, typ)
		}
	}
	return deduplicateTypes(expected)
}

// enumIsTupleType reports whether typ uses XGo's generated tuple structure.
func enumIsTupleType(typ gotypes.Type) bool {
	strct, ok := typ.Underlying().(*gotypes.Struct)
	return ok && strct.NumFields() > 0 && strct.Field(0).Name() == "X_0"
}

// enumEnclosingFunctionContext returns the nearest enclosing ordinary
// function signature, or reports that the nearest function is a lambda.
func enumEnclosingFunctionContext(typeInfo *types.Info, path []ast.Node) (*gotypes.Signature, bool) {
	for _, node := range path {
		switch node := node.(type) {
		case *ast.LambdaExpr:
			return nil, true
		case *ast.FuncLit:
			if sig, _ := typeInfo.TypeOf(node).(*gotypes.Signature); sig != nil {
				return sig, false
			}
		case *ast.FuncDecl:
			if fun, _ := typeInfo.ObjectOf(node.Name).(*gotypes.Func); fun != nil {
				return fun.Signature(), false
			}
		}
	}
	return nil, false
}

// enumCompositeElementContext returns the enum context for target in literal.
func enumCompositeElementContext(
	typeInfo *types.Info,
	literal *ast.CompositeLit,
	target ast.Expr,
) enumIdentContext {
	typ := typeInfo.TypeOf(literal)
	if !xgoutil.IsValidType(typ) && literal.Type != nil {
		typ = typeInfo.TypeOf(literal.Type)
	}
	if !xgoutil.IsValidType(typ) {
		return enumIdentContext{}
	}
	underlyingType := xgoutil.DerefType(typ).Underlying()
	contextForType := func(typ gotypes.Type) enumIdentContext {
		return enumIdentContext{status: enumContextAllowed, expectedTypes: []gotypes.Type{typ}}
	}
	for index, element := range literal.Elts {
		if element == target {
			switch compositeType := underlyingType.(type) {
			case *gotypes.Array:
				return contextForType(compositeType.Elem())
			case *gotypes.Slice:
				return contextForType(compositeType.Elem())
			case *gotypes.Struct:
				if index < compositeType.NumFields() {
					return contextForType(compositeType.Field(index).Type())
				}
			}
			return enumIdentContext{status: enumContextDisallowed}
		}

		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if keyValue.Key == target {
			switch compositeType := underlyingType.(type) {
			case *gotypes.Map:
				return contextForType(compositeType.Key())
			case *gotypes.Array, *gotypes.Slice:
				return enumIdentContext{
					status:               enumContextAllowed,
					basicTypeConstraints: []gotypes.BasicInfo{gotypes.IsInteger},
				}
			default:
				return enumIdentContext{status: enumContextDisallowed}
			}
		}
		if keyValue.Value != target {
			continue
		}
		switch compositeType := underlyingType.(type) {
		case *gotypes.Map:
			return contextForType(compositeType.Elem())
		case *gotypes.Struct:
			key, ok := keyValue.Key.(*ast.Ident)
			if !ok {
				return enumIdentContext{status: enumContextDisallowed}
			}
			for fieldIndex := range compositeType.NumFields() {
				field := compositeType.Field(fieldIndex)
				if field.Name() == key.Name {
					return contextForType(field.Type())
				}
			}
		}
		return enumIdentContext{status: enumContextDisallowed}
	}
	return enumIdentContext{}
}

// isUntypedType reports whether typ is an untyped basic type.
func isUntypedType(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	return ok && basic.Info()&gotypes.IsUntyped != 0
}

// enumBasicTypeConstraintForExpr returns the basic type category imposed by
// expr when it denotes an untyped value.
func enumBasicTypeConstraintForExpr(typeInfo *types.Info, expr ast.Expr) gotypes.BasicInfo {
	if typeAndValue, ok := typeInfo.Types[expr]; ok && xgoutil.IsValidType(typeAndValue.Type) {
		if basic, ok := typeAndValue.Type.Underlying().(*gotypes.Basic); ok && basic.Info()&gotypes.IsUntyped != 0 {
			return enumUntypedBasicConstraint(basic, typeAndValue.Value)
		}
	}
	switch expr := expr.(type) {
	case *ast.BasicLit:
		switch expr.Kind {
		case token.STRING, token.CSTRING:
			return gotypes.IsString
		case token.INT, token.FLOAT, token.IMAG, token.CHAR:
			value := constant.MakeFromLiteral(expr.Value, gotoken.Token(expr.Kind), 0)
			return enumNumericConstantConstraint(value)
		case token.RAT:
			return gotypes.IsNumeric
		}
	case *ast.Ident:
		if expr.Name == "true" || expr.Name == "false" {
			return gotypes.IsBoolean
		}
	}
	return 0
}

// enumUntypedBasicConstraint returns the enum basic types compatible with an
// untyped basic value.
func enumUntypedBasicConstraint(basic *gotypes.Basic, value constant.Value) gotypes.BasicInfo {
	info := basic.Info()
	switch {
	case info&gotypes.IsBoolean != 0:
		return gotypes.IsBoolean
	case info&gotypes.IsString != 0:
		return gotypes.IsString
	case info&gotypes.IsNumeric != 0:
		if value != nil {
			return enumNumericConstantConstraint(value)
		}
		return gotypes.IsNumeric
	default:
		return 0
	}
}

// enumNumericConstantConstraint returns the numeric enum basic types to which
// value can be assigned without changing its exact numeric category.
func enumNumericConstantConstraint(value constant.Value) gotypes.BasicInfo {
	if value == nil || value.Kind() == constant.Unknown {
		return gotypes.IsNumeric
	}
	if constant.ToInt(value).Kind() != constant.Unknown {
		return gotypes.IsNumeric
	}
	if constant.ToFloat(value).Kind() != constant.Unknown {
		return gotypes.IsFloat | gotypes.IsComplex
	}
	return gotypes.IsComplex
}

// spxDefinitionForEnumMembers returns one definition for a non-empty set of
// source-level members.
func (r *compileResult) spxDefinitionForEnumMembers(members ...*enumMemberInfo) SpxDefinition {
	first := members[0]
	var def SpxDefinition
	for _, member := range members {
		if member.object != nil {
			def = GetSpxDefinitionForConst(member.object, nil)
			break
		}
	}
	if def.CompletionItemLabel == "" {
		var pkgPath string
		if first.owner.object != nil {
			pkgPath = xgoutil.PkgPath(first.owner.object.Pkg())
		}
		def = SpxDefinition{
			ID: SpxDefinitionIdentifier{
				Package: ToPtr(pkgPath),
				Name:    ToPtr(first.ident.Name),
			},
			Overview: "const " + first.ident.Name,

			CompletionItemLabel:            first.ident.Name,
			CompletionItemInsertText:       first.ident.Name,
			CompletionItemInsertTextFormat: PlainTextTextFormat,
		}
	}
	def.CompletionItemKind = EnumMemberCompletion
	if len(members) == 1 {
		if first.owner.named != nil {
			def.TypeHint = first.owner.named
		}
		def.Detail = first.doc
		return def
	}

	var detail strings.Builder
	for memberIndex, member := range members {
		if memberIndex > 0 {
			detail.WriteString("\n\n")
		}
		detail.WriteString(member.owner.name)
		detail.WriteByte('.')
		detail.WriteString(member.ident.Name)
		detail.WriteByte(':')
		if doc := strings.TrimSpace(member.doc); doc != "" {
			detail.WriteByte('\n')
			detail.WriteString(doc)
		}
	}
	def.Detail = detail.String()
	return def
}

// spxDefinitionsForEnumTypes returns definitions for members of the given
// types. Members with the same source name are represented by one definition.
func (r *compileResult) spxDefinitionsForEnumTypes(expectedTypes ...gotypes.Type) []SpxDefinition {
	if len(expectedTypes) == 0 {
		return nil
	}
	membersByName := make(map[string][]*enumMemberInfo)
	var names []string
	seenOwners := make(map[*enumTypeInfo]struct{})
	for _, typ := range expectedTypes {
		owner := r.enumInfo.typeFor(typ)
		if owner == nil {
			continue
		}
		if _, ok := seenOwners[owner]; ok {
			continue
		}
		seenOwners[owner] = struct{}{}
		for _, member := range owner.members {
			name := member.ident.Name
			if _, ok := membersByName[name]; !ok {
				names = append(names, name)
			}
			membersByName[name] = append(membersByName[name], member)
		}
	}

	defs := make([]SpxDefinition, 0, len(names))
	for _, name := range names {
		defs = append(defs, r.spxDefinitionForEnumMembers(membersByName[name]...))
	}
	return defs
}
