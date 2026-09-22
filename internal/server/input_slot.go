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

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
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
	proj := s.requestProject()
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return nil, nil
	}
	astFile := astPkg.Files[filename]
	if astFile == nil || !astFile.Pos().IsValid() {
		return nil, nil
	}
	ctx := newInputSlotContext(proj, astFile)
	ctx.frameworkResult, err = analyzeFramework(proj)
	if err != nil {
		return nil, err
	}
	return findInputSlots(ctx), nil
}

// inputSlotContext caches file-level data shared by all input slots in one
// request.
type inputSlotContext struct {
	proj                     *xgo.Project
	frameworkResult          *frameworkAnalysis
	predefinedScopes         []*gotypes.Scope
	imports                  *fileImports
	autoProperties           *autoPropertyResolver
	packageProperties        *autoPropertyResolver
	astFile                  *ast.File
	astPkg                   *ast.Package
	typeInfo                 *types.Info
	parents                  map[ast.Node]ast.Node
	scopeObjects             map[*gotypes.Scope][]gotypes.Object
	scopeBoundaries          map[*gotypes.Scope][]token.Pos
	predefinedNames          map[predefinedNamesCacheKey][]string
	utf16ColumnsByByteOffset []uint32
}

// predefinedNamesCacheKey identifies expressions with the same visible names.
type predefinedNamesCacheKey struct {
	kind             XGoInputSlotKind
	scope            *gotypes.Scope
	declaredType     gotypes.Type
	visibilityRegion int
}

// newInputSlotContext creates a context for finding input slots in astFile.
func newInputSlotContext(proj *xgo.Project, astFile *ast.File) *inputSlotContext {
	typeInfo, _ := expressionTypeInfo(proj)
	astPkg, _ := proj.ASTPackage()
	var predefinedScopes []*gotypes.Scope
	imports := importsForFile(proj, astFile)
	for _, pkg := range imports.members {
		predefinedScopes = append(predefinedScopes, pkg.Scope())
	}
	return &inputSlotContext{
		proj:                     proj,
		predefinedScopes:         append(predefinedScopes, gotypes.Universe),
		imports:                  imports,
		astFile:                  astFile,
		astPkg:                   astPkg,
		typeInfo:                 typeInfo,
		parents:                  nodeParents(astFile),
		scopeObjects:             make(map[*gotypes.Scope][]gotypes.Object),
		scopeBoundaries:          make(map[*gotypes.Scope][]token.Pos),
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
	filePosition := c.proj.Fset.PositionFor(pos, false)
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
	pos := node.Pos()
	for node != nil {
		if scope := xgoutil.ScopeAtNode(c.typeInfo, node, pos); scope != nil {
			return scope
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

// objectUnavailableRange returns the source interval where obj is not yet
// visible within its recorded scope. Most declarations become visible after
// their initializer. A comprehension filter variable is also visible before
// its declaration in source because the result expression precedes the clauses.
func objectUnavailableRange(info *types.Info, file *ast.File, obj gotypes.Object, parents map[ast.Node]ast.Node) (start, end token.Pos) {
	scope := obj.Parent()
	if scope == info.Scopes[file] || info.Pkg != nil && scope == info.Pkg.Scope() {
		return token.NoPos, token.NoPos
	}
	switch node := parents[info.ObjToDef[obj]].(type) {
	case *ast.ValueSpec:
		return token.NoPos, node.End()
	case *ast.AssignStmt:
		if clause, ok := parents[node].(*ast.ForPhrase); ok && clause.Init == node {
			return node.Pos(), node.End()
		}
		return token.NoPos, node.End()
	case *ast.RangeStmt:
		return token.NoPos, node.Body.Pos()
	case *ast.TypeSpec:
		return token.NoPos, node.Name.Pos()
	default:
		return token.NoPos, obj.Pos()
	}
}

// visibilityRegion partitions the candidate cache at every source boundary
// where an object in scope or its parents changes visibility.
func (c *inputSlotContext) visibilityRegion(scope *gotypes.Scope, pos token.Pos) int {
	positions, ok := c.scopeBoundaries[scope]
	if !ok {
		for current := scope; current != nil && current != gotypes.Universe; current = current.Parent() {
			for _, obj := range c.objectsInScope(current) {
				start, end := objectUnavailableRange(c.typeInfo, c.astFile, obj, c.parents)
				if start.IsValid() {
					positions = append(positions, start)
				}
				if end.IsValid() {
					positions = append(positions, end)
				}
			}
		}
		slices.Sort(positions)
		c.scopeBoundaries[scope] = positions
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
			if callExpr := callExprFromNode(typeInfo, node); callExpr != nil {
				inputSlots = append(inputSlots, findInputSlotsFromCallExpr(ctx, callExpr)...)
			}
		case *ast.CallExpr, *ast.FuncDecorator:
			inputSlots = append(inputSlots, findInputSlotsFromCallExpr(ctx, callExprFromNode(typeInfo, node))...)
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				addInputSlot(checkAddressInputSlot(ctx, lhs))
			}
		case *ast.ForStmt:
			if expr, ok := node.Init.(*ast.ExprStmt); ok {
				addInputSlot(checkValueInputSlot(ctx, expr.X, nil))
			}

			if expr, ok := node.Post.(*ast.ExprStmt); ok {
				addInputSlot(checkValueInputSlot(ctx, expr.X, nil))
			}
		case *ast.RangeStmt:
			if node.Key != nil && !isBlank(node.Key) {
				addInputSlot(checkAddressInputSlot(ctx, node.Key))
			}

			if node.Value != nil && !isBlank(node.Value) {
				addInputSlot(checkAddressInputSlot(ctx, node.Value))
			}
		case *ast.IncDecStmt:
			addInputSlot(checkAddressInputSlot(ctx, node.X))
		}

		switch node.(type) {
		case *ast.CompositeLit, *ast.TupleLit, *ast.SliceLit, *ast.MatrixLit:
		default:
			if len(valueOperands(typeInfo, node)) == 0 {
				return true
			}
		}
		var path []ast.Node
		for parent := node; parent != nil; parent = ctx.parents[parent] {
			path = append(path, parent)
		}
		for expr, typ := range contextualValueTypes(typeInfo, path) {
			if _, multipleResults := typ.(*gotypes.Tuple); multipleResults {
				continue
			}
			if !xgoutil.IsValidType(typ) {
				typ = nil
			}
			addInputSlot(checkValueInputSlot(ctx, expr, typ))
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
	// Constant string conversions have no callable signature. Keep their
	// literal editable without replacing the surrounding conversion.
	if literal, _ := resourceStringLiteral(callExpr, ctx.typeInfo); literal != nil {
		if slot := createValueInputSlotFromBasicLit(ctx, literal, ctx.typeInfo.TypeOf(callExpr)); slot != nil {
			return []XGoInputSlot{*slot}
		}
		return nil
	}

	var inputSlots []XGoInputSlot
	for expr, typ := range builtinArgValueTypes(ctx.typeInfo, callExpr) {
		if slot := checkValueInputSlot(ctx, expr, typ); slot != nil {
			inputSlots = append(inputSlots, *slot)
		}
	}
	for resolvedArg := range resolvedCallExprArgs(ctx.typeInfo, callExpr) {
		if resolvedArg.ExpectedType == nil || resolvedArg.IsTypeArg() {
			continue
		}

		expectedType := resolvedArg.ExpectedType
		if _, ok := expectedType.(*gotypes.TypeParam); ok {
			if actualType := ctx.typeInfo.TypeOf(resolvedArg.Arg); xgoutil.IsValidType(actualType) {
				expectedType = actualType
			}
		}
		for expr, declaredType := range valueElementTypes(ctx.typeInfo, resolvedArg.Arg, expectedType) {
			var slot *XGoInputSlot
			if lit, ok := astutil.Unparen(expr).(*ast.NumberUnitLit); ok {
				unitExpectedType := xgoUnitExpectedTypeForResolvedArg(resolvedArg)
				if astutil.Unparen(expr) != astutil.Unparen(resolvedArg.Arg) {
					unitExpectedType = declaredType
				}
				if len(xgoUnitSpecsForType(unitExpectedType)) == 0 {
					continue
				}
				declaredType = xgoutil.DerefType(unitExpectedType)
				slot = createValueInputSlotFromNumberUnitLit(ctx, lit, declaredType)
			} else {
				slot = checkValueInputSlot(ctx, expr, declaredType)
			}
			if slot != nil {
				inputSlots = append(inputSlots, *slot)
			}
		}
	}
	return inputSlots
}

// collectPredefinedNames collects all predefined names for the given expression.
func collectPredefinedNames(ctx *inputSlotContext, kind XGoInputSlotKind, expr ast.Expr, declaredType gotypes.Type) []string {
	innermostScope := ctx.innermostScope(expr)
	key := predefinedNamesCacheKey{scope: innermostScope, kind: kind, declaredType: declaredType}
	if innermostScope != nil {
		key.visibilityRegion = ctx.visibilityRegion(innermostScope, expr.Pos())
	}
	if names, ok := ctx.predefinedNames[key]; ok {
		return names
	}

	var names []string
	seenNames := make(map[string]struct{})
	addObjectName := func(obj gotypes.Object) {
		if variable, ok := obj.(*gotypes.Var); ok && isGeneratedVariable(ctx.proj, variable) {
			return
		}
		name := obj.Name()
		if _, ok := seenNames[name]; ok {
			return
		}
		// A visible declaration shadows outer names even when its type
		// cannot be used in this slot.
		seenNames[name] = struct{}{}
		if _, variable := obj.(*gotypes.Var); kind == XGoInputSlotKindAddress && !variable {
			return
		}
		if typ := obj.Type(); typ != nil && declaredType != nil && !gotypes.AssignableTo(typ, declaredType) {
			return
		}
		if name == "this" || xgoutil.IsXGoInternalName(name) {
			return
		}
		names = append(names, name)
	}

	addPackageAlias := func(obj gotypes.Object) {
		if !isAliasCallable(obj) || xgoutil.IsXGoInternalName(obj.Name()) {
			return
		}
		name := functionAliasName(obj.Name())
		if name == obj.Name() {
			return
		}
		if _, seen := seenNames[name]; seen {
			return
		}
		if ctx.packageProperties == nil {
			ctx.packageProperties = &autoPropertyResolver{proj: ctx.proj}
		}
		property := ctx.packageProperties.resolvePackageObject(obj)
		if !property.exists {
			return
		}
		seenNames[name] = struct{}{}
		_, tuple := property.typ.(*gotypes.Tuple)
		if kind == XGoInputSlotKindValue && xgoutil.IsValidType(property.typ) && !tuple && (declaredType == nil || gotypes.AssignableTo(property.typ, declaredType)) {
			names = append(names, name)
		}
	}

	var classType *gotypes.Named
	for scope := innermostScope; scope != nil && scope != gotypes.Universe; scope = scope.Parent() {
		// All locals, including parameters ordered after "this", hide members.
		if scope == ctx.typeInfo.Pkg.Scope() && xgoutil.IsNamedStructType(classType) {
			if ctx.autoProperties == nil {
				ctx.autoProperties = &autoPropertyResolver{proj: ctx.proj, receiver: gotypes.NewPointer(classType)}
			}
			for structMember := range xgoutil.StructMembers(classType, nil) {
				switch member := structMember.Member.(type) {
				case *gotypes.Var:
					if !member.Origin().Embedded() {
						addObjectName(member)
					} else {
						seenNames[member.Name()] = struct{}{}
					}
				case *gotypes.Func:
					// The declared method name hides imported values even when
					// its alias cannot supply a value for this slot.
					seenNames[member.Name()] = struct{}{}
				}
			}
			for name, property := range ctx.autoProperties.candidates() {
				if _, seen := seenNames[name]; seen || !property.exists {
					continue
				}
				seenNames[name] = struct{}{}
				_, tuple := property.typ.(*gotypes.Tuple)
				if kind == XGoInputSlotKindValue && xgoutil.IsValidType(property.typ) && !tuple && (declaredType == nil || gotypes.AssignableTo(property.typ, declaredType)) {
					names = append(names, name)
				}
			}
		}
		objects := ctx.objectsInScope(scope)
		names = slices.Grow(names, len(objects))
		for _, obj := range objects {
			if _, ok := obj.(*gotypes.PkgName); ok {
				continue
			}
			start, end := objectUnavailableRange(ctx.typeInfo, ctx.astFile, obj, ctx.parents)
			if expr.Pos() < start || expr.Pos() >= end {
				switch obj.(type) {
				case *gotypes.Var, *gotypes.Const:
					addObjectName(obj)
				default:
					seenNames[obj.Name()] = struct{}{}
				}
			}

			// Registered classfile methods can omit the receiver's Defs entry.
			if ctx.astFile.IsClass && obj.Name() == "this" && obj.Pos() == ctx.astFile.Pos() {
				classType, _ = xgoutil.DerefType(obj.Type()).(*gotypes.Named)
			}
		}
	}

	for _, obj := range ctx.objectsInScope(ctx.typeInfo.Pkg.Scope()) {
		addPackageAlias(obj)
	}
	for name := range ctx.imports.ambiguous {
		seenNames[name] = struct{}{}
	}
	for _, scope := range ctx.predefinedScopes {
		objects := ctx.objectsInScope(scope)
		names = slices.Grow(names, len(objects))
		for _, obj := range objects {
			if scope != gotypes.Universe && !obj.Exported() {
				continue
			}
			if _, ok := obj.(*gotypes.Var); ok {
				addObjectName(obj)
			} else {
				seenNames[obj.Name()] = struct{}{}
			}
			addPackageAlias(obj)
		}
	}

	ctx.predefinedNames[key] = names
	return names
}

// checkValueInputSlot checks if the expression is a value input slot.
func checkValueInputSlot(ctx *inputSlotContext, expr ast.Expr, declaredType gotypes.Type) *XGoInputSlot {
	switch expr := astutil.Unparen(expr).(type) {
	case *ast.BasicLit:
		return createValueInputSlotFromBasicLit(ctx, expr, declaredType)
	case *ast.Ident:
		return createValueInputSlotFromIdent(ctx, expr, declaredType)
	case *ast.UnaryExpr:
		return createValueInputSlotFromUnaryExpr(ctx, expr, declaredType)
	case *ast.CallExpr:
		return ctx.adaptInputSlot(expr, declaredType, nil)
	}
	return nil
}

// checkAddressInputSlot checks if the expression is an address input slot.
func checkAddressInputSlot(ctx *inputSlotContext, expr ast.Expr) *XGoInputSlot {
	ident, ok := expr.(*ast.Ident)
	if !ok || !xgoutil.IsSourceIdent(xgoutil.NodeTokenFile(ctx.proj.Fset, ctx.astFile), ctx.astFile.Code, ident) {
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
		PredefinedNames: collectPredefinedNames(ctx, XGoInputSlotKindAddress, expr, nil),
		Range:           ctx.rangeForNode(ident),
	}
}

// createValueInputSlotFromBasicLit creates a value input slot from a basic literal.
func createValueInputSlotFromBasicLit(ctx *inputSlotContext, lit *ast.BasicLit, declaredType gotypes.Type) *XGoInputSlot {
	input := XGoInput{Kind: XGoInputKindInPlace}
	switch lit.Kind {
	case token.STRING:
		input.Type = XGoInputTypeString
		v, ok := xgoutil.StringLitOrConstValue(lit, ctx.typeInfo.Types[lit])
		if !ok {
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

	return ctx.adaptInputSlot(lit, declaredType, &XGoInputSlot{
		Kind:            XGoInputSlotKindValue,
		Accept:          accept,
		Input:           input,
		PredefinedNames: collectPredefinedNames(ctx, XGoInputSlotKindValue, lit, declaredType),
		Range:           ctx.rangeForPosEnd(lit.Pos(), basicLitEnd(ctx.proj.Fset, ctx.astFile, lit)),
	})
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
		PredefinedNames: collectPredefinedNames(ctx, XGoInputSlotKindValue, lit, declaredType),
		Range:           ctx.rangeForPosEnd(lit.ValuePos, xgoUnitStart(lit)),
	}
}

// createValueInputSlotFromIdent creates a value input slot from an identifier.
func createValueInputSlotFromIdent(ctx *inputSlotContext, ident *ast.Ident, declaredType gotypes.Type) *XGoInputSlot {
	if ctx.typeInfo == nil || !xgoutil.IsSourceIdent(xgoutil.NodeTokenFile(ctx.proj.Fset, ctx.astFile), ctx.astFile.Code, ident) {
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
	}

	accept := XGoInputSlotAccept{Type: input.Type}
	if declaredType != nil {
		accept.Type = ctx.inferInputType(declaredType)
	}

	return ctx.adaptInputSlot(ident, declaredType, &XGoInputSlot{
		Kind:            XGoInputSlotKindValue,
		Accept:          accept,
		Input:           input,
		PredefinedNames: collectPredefinedNames(ctx, XGoInputSlotKindValue, ident, declaredType),
		Range:           ctx.rangeForNode(ident),
	})
}

// inferInputType classifies basic types and optional framework types.
func (ctx *inputSlotContext) inferInputType(typ gotypes.Type) XGoInputType {
	if ctx.frameworkResult != nil && ctx.frameworkResult.inputType != nil {
		return ctx.frameworkResult.inputType(typ)
	}
	return inferBasicInputType(typ)
}

// adaptInputSlot applies framework semantics to an otherwise ordinary input.
func (ctx *inputSlotContext) adaptInputSlot(expr ast.Expr, typ gotypes.Type, slot *XGoInputSlot) *XGoInputSlot {
	if ctx.frameworkResult != nil && ctx.frameworkResult.adaptInputSlot != nil {
		return ctx.frameworkResult.adaptInputSlot(ctx, expr, typ, slot)
	}
	return slot
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
