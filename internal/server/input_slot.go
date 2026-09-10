package server

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"iter"
	"slices"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// xgoGetInputSlots gets input slots in source order. Returned slots have
// pairwise non-overlapping ranges.
func (s *Server) xgoGetInputSlots(params []XGoGetInputSlotsParams) ([]XGoInputSlot, error) {
	if l := len(params); l == 0 {
		return nil, nil
	} else if l > 1 {
		return nil, fmt.Errorf("%s only supports one document at a time", CommandXGoGetInputSlots)
	}
	param := params[0]

	filename, err := s.fromDocumentURI(param.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", param.TextDocument.URI, err)
	}
	proj := s.getProjWithFile()
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return nil, nil
	}
	astFile := astPkg.Files[filename]
	if astFile == nil || !astFile.Pos().IsValid() {
		return nil, nil
	}
	ctx := newInputSlotContext(proj, astFile)
	ctx.spxResult, err = s.compileForSpxInputSlots(proj, filename)
	if err != nil {
		return nil, err
	}
	return findInputSlots(ctx), nil
}

// inputSlotContext caches file-level data shared by all input slots in one
// request.
type inputSlotContext struct {
	proj                     *xgo.Project
	spxResult                *compileResult
	predefinedScopes         []*gotypes.Scope
	astFile                  *ast.File
	astPkg                   *ast.Package
	typeInfo                 *types.Info
	parents                  map[ast.Node]ast.Node
	scopeObjects             map[*gotypes.Scope][]gotypes.Object
	scopeStartPositions      map[*gotypes.Scope][]token.Pos
	predefinedNames          map[predefinedNamesCacheKey][]string
	utf16ColumnsByByteOffset []uint32
}

// predefinedNamesCacheKey identifies expressions with the same visible names.
type predefinedNamesCacheKey struct {
	scope              *gotypes.Scope
	declaredType       gotypes.Type
	visibleObjectCount int
}

// newInputSlotContext creates a context for finding input slots in astFile.
func newInputSlotContext(proj *xgo.Project, astFile *ast.File) *inputSlotContext {
	typeInfo, _ := proj.TypeInfo()
	astPkg, _ := proj.ASTPackage()
	var predefinedScopes []*gotypes.Scope
	if astFile.IsClass {
		filename := xgoutil.NodeFilename(proj.Fset, astFile)
		if class, ok := proj.Mod.LookupClass(modfile.ClassExt(filename)); ok {
			for _, pkgPath := range class.PkgPaths {
				pkg, err := proj.Importer.Import(pkgPath)
				if err == nil {
					predefinedScopes = append(predefinedScopes, pkg.Scope())
				}
			}
		}
	}
	return &inputSlotContext{
		proj:                     proj,
		predefinedScopes:         append(predefinedScopes, gotypes.Universe),
		astFile:                  astFile,
		astPkg:                   astPkg,
		typeInfo:                 typeInfo,
		parents:                  nodeParents(astFile),
		scopeObjects:             make(map[*gotypes.Scope][]gotypes.Object),
		scopeStartPositions:      make(map[*gotypes.Scope][]token.Pos),
		predefinedNames:          make(map[predefinedNamesCacheKey][]string),
		utf16ColumnsByByteOffset: buildUTF16ColumnIndex(astFile.Code),
	}
}

// nodeParents returns the parent of each descendant of root.
func nodeParents(root ast.Node) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

// buildUTF16ColumnIndex returns the UTF-16 column at every byte offset in code.
func buildUTF16ColumnIndex(code []byte) []uint32 {
	columns := make([]uint32, len(code)+1)
	var column uint32
	for offset := 0; offset < len(code); {
		columns[offset] = column
		switch code[offset] {
		case '\n':
			offset++
			column = 0
			columns[offset] = column
			continue
		case '\r':
			if offset+1 < len(code) && code[offset+1] == '\n' {
				offset++
				columns[offset] = column
				continue
			}
		}

		r, size := utf8.DecodeRune(code[offset:])
		for i := 1; i < size; i++ {
			columns[offset+i] = column
		}
		column += uint32(utf16.RuneLen(r))
		offset += size
		columns[offset] = column
	}
	return columns
}

// position converts pos to an LSP position using the precomputed UTF-16
// column index.
func (c *inputSlotContext) position(pos token.Pos) Position {
	filePosition := c.proj.Fset.Position(pos)
	offset := min(max(filePosition.Offset, 0), len(c.astFile.Code))
	return Position{
		Line:      uint32(max(filePosition.Line, 1) - 1),
		Character: c.utf16ColumnsByByteOffset[offset],
	}
}

// rangeForNode returns the LSP range for node.
func (c *inputSlotContext) rangeForNode(node ast.Node) Range {
	return Range{Start: c.position(node.Pos()), End: c.position(node.End())}
}

// rangeForPosEnd returns the LSP range for pos and end.
func (c *inputSlotContext) rangeForPosEnd(pos, end token.Pos) Range {
	return Range{Start: c.position(pos), End: c.position(end)}
}

// innermostScope returns the innermost type-checker scope containing node.
func (c *inputSlotContext) innermostScope(node ast.Node) *gotypes.Scope {
	for node != nil {
		if scope := c.typeInfo.Scopes[node]; scope != nil {
			return scope
		}
		switch node := node.(type) {
		case *ast.FuncDecl:
			if scope := c.typeInfo.Scopes[node.Type]; scope != nil {
				return scope
			}
		case *ast.FuncLit:
			if scope := c.typeInfo.Scopes[node.Type]; scope != nil {
				return scope
			}
			if scope := c.typeInfo.Scopes[node.Body]; scope != nil {
				return scope
			}
		}
		node = c.parents[node]
	}
	return nil
}

// objectsInScope returns the objects in scope in scope-name order.
func (c *inputSlotContext) objectsInScope(scope *gotypes.Scope) []gotypes.Object {
	if objects, ok := c.scopeObjects[scope]; ok {
		return objects
	}
	names := scope.Names()
	objects := make([]gotypes.Object, 0, len(names))
	for _, name := range names {
		if obj := scope.Lookup(name); obj != nil {
			objects = append(objects, obj)
		}
	}
	c.scopeObjects[scope] = objects
	return objects
}

// objectScopeStart returns the position where obj becomes visible. Package
// and file objects are visible throughout their scopes. Local declarations
// become visible after their initializers, not at their identifier positions.
func (c *inputSlotContext) objectScopeStart(obj gotypes.Object) token.Pos {
	scope := obj.Parent()
	if scope == c.typeInfo.Scopes[c.astFile] || c.typeInfo.Pkg != nil && scope == c.typeInfo.Pkg.Scope() {
		return token.NoPos
	}
	switch node := c.parents[c.typeInfo.ObjToDef[obj]].(type) {
	case *ast.ValueSpec, *ast.AssignStmt:
		return node.End()
	case *ast.RangeStmt:
		return node.Body.Pos()
	default:
		return obj.Pos()
	}
}

// visibleObjectCount counts variables and constants whose scopes have started
// at pos in scope and its parents. It partitions the candidate cache at every
// declaration that can change name visibility.
func (c *inputSlotContext) visibleObjectCount(scope *gotypes.Scope, pos token.Pos) int {
	positions, ok := c.scopeStartPositions[scope]
	if !ok {
		for current := scope; current != nil && current != gotypes.Universe; current = current.Parent() {
			for _, obj := range c.objectsInScope(current) {
				switch obj.(type) {
				case *gotypes.Var, *gotypes.Const:
					positions = append(positions, c.objectScopeStart(obj))
				}
			}
		}
		slices.Sort(positions)
		c.scopeStartPositions[scope] = positions
	}
	count, _ := slices.BinarySearch(positions, pos+1)
	return count
}

// compareInputSlotPriority compares input slots by conflict priority. Slots
// with earlier starts win, and longer slots win when their starts are equal.
func compareInputSlotPriority(a, b XGoInputSlot) int {
	if start := comparePositions(a.Range.Start, b.Range.Start); start != 0 {
		return start
	}
	return comparePositions(b.Range.End, a.Range.End)
}

// normalizeInputSlots orders input slots by source range, drops degenerate
// ranges, and removes overlaps. Discovery order breaks ties between slots with
// identical ranges. It takes ownership of inputSlots and may overwrite it.
func normalizeInputSlots(inputSlots []XGoInputSlot) []XGoInputSlot {
	// Reuse the input storage when candidates are already in priority order.
	if slices.IsSortedFunc(inputSlots, compareInputSlotPriority) {
		normalizedSlots := appendNonOverlappingInputSlots(inputSlots[:0], slices.Values(inputSlots))
		clear(inputSlots[len(normalizedSlots):])
		return normalizedSlots
	}

	// Sorting indices preserves discovery order for exact ties without moving
	// the comparatively large slot values during every sort operation.
	indices := make([]int, len(inputSlots))
	for i := range indices {
		indices[i] = i
	}
	slices.SortFunc(indices, func(i, j int) int {
		if priority := compareInputSlotPriority(inputSlots[i], inputSlots[j]); priority != 0 {
			return priority
		}
		return cmp.Compare(i, j)
	})
	orderedSlots := func(yield func(XGoInputSlot) bool) {
		for _, index := range indices {
			if !yield(inputSlots[index]) {
				return
			}
		}
	}
	return appendNonOverlappingInputSlots(make([]XGoInputSlot, 0, len(inputSlots)), orderedSlots)
}

// appendNonOverlappingInputSlots appends valid slots from a sequence ordered by
// [compareInputSlotPriority] unless they overlap the last accepted slot. The
// ordering and disjointness of accepted slots mean only the last can overlap
// the next slot. The destination must be empty and may alias storage read by
// slots. Appending at most once per input keeps aliased writes behind unread
// inputs.
func appendNonOverlappingInputSlots(dst []XGoInputSlot, slots iter.Seq[XGoInputSlot]) []XGoInputSlot {
	for slot := range slots {
		if comparePositions(slot.Range.Start, slot.Range.End) >= 0 {
			continue
		}
		if len(dst) > 0 && IsRangesOverlap(dst[len(dst)-1].Range, slot.Range) {
			continue
		}
		dst = append(dst, slot)
	}
	return dst
}

// findInputSlots finds all input slots in the AST file in source order. The
// returned slots have pairwise non-overlapping ranges.
func findInputSlots(ctx *inputSlotContext) []XGoInputSlot {
	typeInfo := ctx.typeInfo
	if typeInfo == nil {
		return nil
	}
	astFile := ctx.astFile

	var inputSlots []XGoInputSlot
	addInputSlot := func(slot *XGoInputSlot) {
		if slot != nil {
			inputSlots = append(inputSlots, *slot)
		}
	}

	ast.Inspect(astFile, func(node ast.Node) bool {
		if node == nil {
			return true
		}

		switch node := node.(type) {
		case *ast.BranchStmt:
			if callExpr := xgoutil.CreateCallExprFromBranchStmt(typeInfo, node); callExpr != nil {
				inputSlots = append(inputSlots, findInputSlotsFromCallExpr(ctx, callExpr)...)
			}
		case *ast.CallExpr, *ast.FuncDecorator:
			inputSlots = append(inputSlots, findInputSlotsFromCallExpr(ctx, callExprFromNode(node))...)
		case *ast.BinaryExpr:
			addInputSlot(checkValueInputSlot(ctx, node.X, nil))
			addInputSlot(checkValueInputSlot(ctx, node.Y, nil))
		case *ast.UnaryExpr:
			addInputSlot(checkValueInputSlot(ctx, node.X, nil))
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				addInputSlot(checkAddressInputSlot(ctx, lhs))
			}

			for i, rhs := range node.Rhs {
				var declaredType gotypes.Type
				if len(node.Lhs) == len(node.Rhs) {
					declaredType = typeInfo.TypeOf(node.Lhs[i])
				}

				addInputSlot(checkValueInputSlot(ctx, rhs, declaredType))
			}
		case *ast.ForStmt:
			if expr, ok := node.Init.(*ast.ExprStmt); ok {
				addInputSlot(checkValueInputSlot(ctx, expr.X, nil))
			}

			if node.Cond != nil {
				addInputSlot(checkValueInputSlot(ctx, node.Cond, gotypes.Typ[gotypes.Bool]))
			}

			if expr, ok := node.Post.(*ast.ExprStmt); ok {
				addInputSlot(checkValueInputSlot(ctx, expr.X, nil))
			}
		case *ast.ValueSpec:
			for i, value := range node.Values {
				var declaredType gotypes.Type
				if len(node.Names) == len(node.Values) {
					nameIdent := node.Names[i]
					if nameIdent != nil && nameIdent.Name != "_" {
						obj := typeInfo.ObjectOf(nameIdent)
						if obj != nil {
							declaredType = obj.Type()
						}
					}
				}

				addInputSlot(checkValueInputSlot(ctx, value, declaredType))
			}
		case *ast.ReturnStmt:
			for _, res := range node.Results {
				addInputSlot(checkValueInputSlot(ctx, res, nil))
			}
		case *ast.IfStmt:
			addInputSlot(checkValueInputSlot(ctx, node.Cond, gotypes.Typ[gotypes.Bool]))
		case *ast.SwitchStmt:
			if node.Tag != nil {
				addInputSlot(checkValueInputSlot(ctx, node.Tag, nil))
			}
		case *ast.CaseClause:
			for _, expr := range node.List {
				addInputSlot(checkValueInputSlot(ctx, expr, nil))
			}
		case *ast.RangeStmt:
			if node.Key != nil && !isBlank(node.Key) {
				addInputSlot(checkAddressInputSlot(ctx, node.Key))
			}

			if node.Value != nil && !isBlank(node.Value) {
				addInputSlot(checkAddressInputSlot(ctx, node.Value))
			}

			addInputSlot(checkValueInputSlot(ctx, node.X, nil))
		case *ast.IncDecStmt:
			addInputSlot(checkAddressInputSlot(ctx, node.X))
		}
		return true
	})
	return normalizeInputSlots(inputSlots)
}

// findInputSlotsFromCallExpr finds input slots from a call expression.
func findInputSlotsFromCallExpr(ctx *inputSlotContext, callExpr *ast.CallExpr) []XGoInputSlot {
	if ctx.typeInfo == nil {
		return nil
	}

	var inputSlots []XGoInputSlot
	for resolvedArg := range resolvedCallExprArgs(ctx.proj, ctx.typeInfo, callExpr) {
		if resolvedArg.ExpectedType == nil || resolvedArg.IsTypeArg() {
			continue
		}

		expectedType := resolvedArg.ExpectedType
		if _, ok := expectedType.(*gotypes.TypeParam); ok {
			if actualType := ctx.typeInfo.TypeOf(resolvedArg.Arg); xgoutil.IsValidType(actualType) {
				expectedType = actualType
			}
		}
		declaredType := xgoutil.DerefType(expectedType)
		if sliceType, ok := declaredType.(*gotypes.Slice); ok {
			declaredType = xgoutil.DerefType(sliceType.Elem())
		}

		var slot *XGoInputSlot
		if lit, ok := resolvedArg.Arg.(*ast.NumberUnitLit); ok {
			unitExpectedType := xgoUnitExpectedTypeForResolvedArg(resolvedArg)
			if len(xgoUnitSpecsForType(unitExpectedType)) == 0 {
				continue
			}
			declaredType = xgoutil.DerefType(unitExpectedType)
			slot = createValueInputSlotFromNumberUnitLit(ctx, lit, declaredType)
		} else {
			slot = checkValueInputSlot(ctx, resolvedArg.Arg, declaredType)
		}
		if slot != nil {
			inputSlots = append(inputSlots, *slot)
		}
	}
	return inputSlots
}

// collectPredefinedNames collects all predefined names for the given expression.
func collectPredefinedNames(ctx *inputSlotContext, expr ast.Expr, declaredType gotypes.Type) []string {
	innermostScope := ctx.innermostScope(expr)
	key := predefinedNamesCacheKey{scope: innermostScope, declaredType: declaredType}
	if innermostScope != nil {
		key.visibleObjectCount = ctx.visibleObjectCount(innermostScope, expr.Pos())
	}
	if names, ok := ctx.predefinedNames[key]; ok {
		return names
	}

	var names []string
	seenNames := make(map[string]struct{})
	addObjectName := func(obj gotypes.Object) {
		name := obj.Name()
		if _, ok := obj.(*gotypes.Func); ok {
			name = xgoutil.ToLowerCamelCase(name)
		}
		if _, ok := seenNames[name]; ok {
			return
		}
		// A visible declaration shadows outer names even when its type
		// cannot be used in this slot.
		seenNames[name] = struct{}{}
		switch obj := obj.(type) {
		case *gotypes.Var, *gotypes.Const:
			if typ := obj.Type(); typ != nil && declaredType != nil && !gotypes.AssignableTo(typ, declaredType) {
				return
			}

			if name == "this" || xgoutil.IsXGoInternalName(name) {
				return
			}
		case *gotypes.Func:
			if declaredType != nil {
				// For functions with no parameters and exactly one return value,
				// check if the return type is assignable to the declared type.
				funcSig := obj.Signature()
				if funcSig.Params().Len() != 0 || funcSig.Results().Len() != 1 {
					return
				}
				funcReturnType := funcSig.Results().At(0).Type()
				if !gotypes.AssignableTo(funcReturnType, declaredType) {
					return
				}
			}
		default:
			return
		}
		names = append(names, name)
	}

	for scope := innermostScope; scope != nil && scope != gotypes.Universe; scope = scope.Parent() {
		objects := ctx.objectsInScope(scope)
		names = slices.Grow(names, len(objects))
		for _, obj := range objects {
			if ctx.objectScopeStart(obj) <= expr.Pos() {
				switch obj.(type) {
				case *gotypes.Var, *gotypes.Const:
					addObjectName(obj)
				}
			}

			if !ctx.astFile.IsClass || !xgoutil.IsSyntheticThisIdent(
				ctx.proj.Fset,
				ctx.typeInfo,
				ctx.astPkg,
				ctx.typeInfo.ObjToDef[obj],
			) {
				continue
			}
			objType := xgoutil.DerefType(obj.Type())
			named, ok := objType.(*gotypes.Named)
			if !ok || !xgoutil.IsNamedStructType(named) {
				continue
			}

			for structMember := range xgoutil.StructMembers(named) {
				switch member := structMember.Member.(type) {
				case *gotypes.Var:
					if !member.Origin().Embedded() {
						addObjectName(member)
					}
				case *gotypes.Func:
					// Add methods with no parameters and exactly one return value.
					// These methods can be used as property expressions in XGo.
					funcSig := member.Signature()
					if funcSig.Params().Len() == 0 && funcSig.Results().Len() == 1 {
						addObjectName(member)
					}
				}
			}
		}
	}

	for _, scope := range ctx.predefinedScopes {
		objects := ctx.objectsInScope(scope)
		names = slices.Grow(names, len(objects))
		for _, obj := range objects {
			if _, ok := obj.(*gotypes.Var); ok && (scope == gotypes.Universe || obj.Exported()) {
				addObjectName(obj)
			}
		}
	}

	ctx.predefinedNames[key] = names
	return names
}

// checkValueInputSlot checks if the expression is a value input slot.
func checkValueInputSlot(ctx *inputSlotContext, expr ast.Expr, declaredType gotypes.Type) *XGoInputSlot {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		return createValueInputSlotFromBasicLit(ctx, expr, declaredType)
	case *ast.Ident:
		return createValueInputSlotFromIdent(ctx, expr, declaredType)
	case *ast.UnaryExpr:
		return createValueInputSlotFromUnaryExpr(ctx, expr, declaredType)
	case *ast.CallExpr:
		return createValueInputSlotFromColorFuncCall(ctx, expr, declaredType)
	}
	return nil
}

// checkAddressInputSlot checks if the expression is an address input slot.
func checkAddressInputSlot(ctx *inputSlotContext, expr ast.Expr) *XGoInputSlot {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return nil
	}
	return &XGoInputSlot{
		Kind:   XGoInputSlotKindAddress,
		Accept: XGoInputSlotAccept{Type: XGoInputTypeUnknown},
		Input: XGoInput{
			Kind: XGoInputKindPredefined,
			Type: XGoInputTypeUnknown,
			Name: ident.Name,
		},
		PredefinedNames: collectPredefinedNames(ctx, expr, nil),
		Range:           ctx.rangeForNode(ident),
	}
}

// createValueInputSlotFromBasicLit creates a value input slot from a basic literal.
func createValueInputSlotFromBasicLit(ctx *inputSlotContext, lit *ast.BasicLit, declaredType gotypes.Type) *XGoInputSlot {
	input := XGoInput{Kind: XGoInputKindInPlace}
	switch lit.Kind {
	case token.STRING:
		input.Type = XGoInputTypeString
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil
		}
		input.Value = v
	case token.INT:
		input.Type = XGoInputTypeInteger
		v, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil {
			return nil
		}
		input.Value = v
	case token.FLOAT:
		input.Type = XGoInputTypeDecimal
		v, err := strconv.ParseFloat(lit.Value, 64)
		if err != nil {
			return nil
		}
		input.Value = v
	default:
		return nil
	}

	accept := XGoInputSlotAccept{Type: input.Type}
	if declaredType != nil {
		accept.Type = ctx.inferInputType(declaredType)
	}
	if accept.Type == SpxInputTypeResourceName {
		for _, spxResourceRef := range ctx.spxResult.spxResourceRefs {
			if spxResourceRef.Node == lit {
				input.Type = SpxInputTypeResourceName
				input.Value = spxResourceRef.ID.URI()
				accept.ResourceContext = ToPtr(spxResourceRef.ID.ContextURI())
				break
			}
		}
		if accept.ResourceContext == nil {
			return nil
		}
	}

	return &XGoInputSlot{
		Kind:            XGoInputSlotKindValue,
		Accept:          accept,
		Input:           input,
		PredefinedNames: collectPredefinedNames(ctx, lit, declaredType),
		Range:           ctx.rangeForNode(lit),
	}
}

// createValueInputSlotFromNumberUnitLit creates a value input slot from a
// number-with-unit literal.
func createValueInputSlotFromNumberUnitLit(ctx *inputSlotContext, lit *ast.NumberUnitLit, declaredType gotypes.Type) *XGoInputSlot {
	input := XGoInput{Kind: XGoInputKindInPlace}
	switch lit.Kind {
	case token.INT:
		input.Type = XGoInputTypeInteger
		v, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil {
			return nil
		}
		input.Value = v
	case token.FLOAT:
		input.Type = XGoInputTypeDecimal
		v, err := strconv.ParseFloat(lit.Value, 64)
		if err != nil {
			return nil
		}
		input.Value = v
	default:
		return nil
	}

	accept := XGoInputSlotAccept{Type: input.Type}
	if declaredType != nil {
		if acceptType := ctx.inferInputType(declaredType); acceptType != XGoInputTypeUnknown {
			accept.Type = acceptType
		}
	}

	return &XGoInputSlot{
		Kind:            XGoInputSlotKindValue,
		Accept:          accept,
		Input:           input,
		PredefinedNames: collectPredefinedNames(ctx, lit, declaredType),
		Range:           ctx.rangeForPosEnd(lit.ValuePos, xgoUnitStart(lit)),
	}
}

// createValueInputSlotFromIdent creates a value input slot from an identifier.
func createValueInputSlotFromIdent(ctx *inputSlotContext, ident *ast.Ident, declaredType gotypes.Type) *XGoInputSlot {
	if ctx.typeInfo == nil {
		return nil
	}
	typ := ctx.typeInfo.TypeOf(ident)
	if typ == nil {
		return nil
	}
	typ = xgoutil.DerefType(typ)

	input := XGoInput{
		Kind: XGoInputKindPredefined,
		Type: ctx.inferInputType(typ),
		Name: ident.Name,
	}
	switch input.Type {
	case XGoInputTypeBoolean:
		if basicType, ok := typ.(*gotypes.Basic); ok && basicType.Kind() == gotypes.UntypedBool {
			input.Kind = XGoInputKindInPlace
			input.Value = ident.Name == "true"
			input.Name = ""
		}
	case SpxInputTypeDirection,
		SpxInputTypeEffectKind,
		SpxInputTypeLayerAction,
		SpxInputTypeDirAction,
		SpxInputTypeKey,
		SpxInputTypeSpecialObj,
		SpxInputTypeRotationStyle:
		obj := ctx.typeInfo.ObjectOf(ident)
		if obj != nil && !IsInSpxPkg(obj) {
			break
		}
		cnst, ok := obj.(*gotypes.Const)
		if !ok {
			break
		}
		input.Kind = XGoInputKindInPlace
		switch input.Type {
		case SpxInputTypeDirection:
			input.Value, _ = strconv.ParseFloat(cnst.Val().ExactString(), 64)
		default:
			input.Value = cnst.Name()
		}
		input.Name = ""
	}

	accept := XGoInputSlotAccept{Type: input.Type}
	if declaredType != nil {
		accept.Type = ctx.inferInputType(declaredType)
	}
	switch accept.Type {
	case SpxInputTypeResourceName:
		switch canonicalSpxResourceNameType(declaredType) {
		case GetSpxBackdropNameType():
			accept.ResourceContext = ToPtr(SpxBackdropResourceContextURI)
		case GetSpxSoundNameType():
			accept.ResourceContext = ToPtr(SpxSoundResourceContextURI)
		case GetSpxSpriteNameType():
			accept.ResourceContext = ToPtr(SpxSpriteResourceContextURI)
		case GetSpxSpriteCostumeNameType():
			spxSpriteResource := inferSpxSpriteResourceEnclosingNode(ctx.spxResult, ident)
			if spxSpriteResource == nil {
				return nil
			}
			accept.ResourceContext = ToPtr(FormatSpxSpriteCostumeResourceContextURI(spxSpriteResource.Name))
		case GetSpxSpriteAnimationNameType():
			spxSpriteResource := inferSpxSpriteResourceEnclosingNode(ctx.spxResult, ident)
			if spxSpriteResource == nil {
				return nil
			}
			accept.ResourceContext = ToPtr(FormatSpxSpriteAnimationResourceContextURI(spxSpriteResource.Name))
		case GetSpxWidgetNameType():
			accept.ResourceContext = ToPtr(SpxWidgetResourceContextURI)
		default:
			return nil
		}
	case SpxInputTypeSpriteInstance:
		accept.ResourceContext = ToPtr(SpxSpriteResourceContextURI)
		if spxSpriteResource := spxSpriteResourceForObject(ctx.spxResult, ctx.typeInfo.ObjectOf(ident)); spxSpriteResource != nil {
			input.Kind = XGoInputKindInPlace
			input.Value = spxSpriteResource.ID.URI()
			input.Name = ""
		}
	}

	return &XGoInputSlot{
		Kind:            XGoInputSlotKindValue,
		Accept:          accept,
		Input:           input,
		PredefinedNames: collectPredefinedNames(ctx, ident, declaredType),
		Range:           ctx.rangeForNode(ident),
	}
}

// inferInputType classifies a type using spx metadata only for spx documents.
func (ctx *inputSlotContext) inferInputType(typ gotypes.Type) XGoInputType {
	if ctx.spxResult != nil {
		return inferSpxInputTypeFromTypeInProject(ctx.spxResult, typ)
	}
	return inferBasicInputType(typ)
}

// inferBasicInputType classifies basic types and aliases without loading framework packages.
func inferBasicInputType(typ gotypes.Type) XGoInputType {
	if alias, ok := typ.(*gotypes.Alias); ok {
		return inferBasicInputType(alias.Rhs())
	}
	basic, ok := typ.(*gotypes.Basic)
	if !ok {
		return XGoInputTypeUnknown
	}
	switch basic.Kind() {
	case gotypes.String, gotypes.UntypedString:
		return XGoInputTypeString
	case gotypes.Int, gotypes.Int8, gotypes.Int16, gotypes.Int32, gotypes.Int64,
		gotypes.Uint, gotypes.Uint8, gotypes.Uint16, gotypes.Uint32, gotypes.Uint64,
		gotypes.UntypedInt:
		return XGoInputTypeInteger
	case gotypes.Float32, gotypes.Float64, gotypes.UntypedFloat:
		return XGoInputTypeDecimal
	case gotypes.Bool, gotypes.UntypedBool:
		return XGoInputTypeBoolean
	}
	return XGoInputTypeUnknown
}

// createValueInputSlotFromUnaryExpr creates a value input slot from a unary expression.
func createValueInputSlotFromUnaryExpr(ctx *inputSlotContext, expr *ast.UnaryExpr, declaredType gotypes.Type) *XGoInputSlot {
	var inputSlot *XGoInputSlot
	switch x := expr.X.(type) {
	case *ast.BasicLit:
		inputSlot = createValueInputSlotFromBasicLit(ctx, x, declaredType)
		if inputSlot == nil {
			return nil
		}

		switch expr.Op {
		case token.ADD:
			// Nothing to do for unary plus.
		case token.SUB:
			switch v := inputSlot.Input.Value.(type) {
			case int64:
				inputSlot.Input.Value = -v
			case float64:
				inputSlot.Input.Value = -v
			default:
				return nil
			}
		case token.XOR:
			if x.Kind != token.INT {
				return nil
			}
			v, ok := inputSlot.Input.Value.(int64)
			if !ok {
				return nil
			}
			inputSlot.Input.Value = ^v
		}
	case *ast.Ident:
		inputSlot = createValueInputSlotFromIdent(ctx, x, declaredType)
		if inputSlot == nil {
			return nil
		}

		if expr.Op == token.NOT {
			v, ok := inputSlot.Input.Value.(bool)
			if !ok {
				return nil
			}
			inputSlot.Input.Value = !v
		}
	default:
		return nil
	}
	inputSlot.Range = ctx.rangeForNode(expr) // Update the range to include the entire unary expression.
	return inputSlot
}

// isBlank checks if an expression is a blank identifier (_).
func isBlank(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "_"
}
