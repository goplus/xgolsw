package server

import (
	"cmp"
	"encoding/json"
	"fmt"
	gotypes "go/types"
	"iter"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

const (
	CommandXGoRenameResources = "xgo.renameResources"
	CommandSpxRenameResources = "spx.renameResources"
	CommandXGoGetInputSlots   = "xgo.getInputSlots"
	CommandSpxGetInputSlots   = "spx.getInputSlots"
	CommandXGoGetProperties   = "xgo.getProperties"
)

// xgoPropertyKindPriority defines the presentation order for XGo properties.
var xgoPropertyKindPriority = map[XGoPropertyKind]int{
	XGoPropertyKindField:  0,
	XGoPropertyKindMethod: 1,
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspace_executeCommand
func (s *Server) workspaceExecuteCommand(params *ExecuteCommandParams) (any, error) {
	switch params.Command {
	case CommandXGoRenameResources, CommandSpxRenameResources:
		var cmdParams []XGoRenameResourceParams
		for _, arg := range params.Arguments {
			var cmdParam XGoRenameResourceParams
			if err := json.Unmarshal(arg, &cmdParam); err != nil {
				return nil, fmt.Errorf("failed to unmarshal command argument as XGoRenameResourceParams: %w", err)
			}
			cmdParams = append(cmdParams, cmdParam)
		}
		return s.spxRenameResources(cmdParams)
	case CommandXGoGetInputSlots, CommandSpxGetInputSlots:
		var cmdParams []XGoGetInputSlotsParams
		for _, arg := range params.Arguments {
			var cmdParam XGoGetInputSlotsParams
			if err := json.Unmarshal(arg, &cmdParam); err != nil {
				return nil, fmt.Errorf("failed to unmarshal command argument as XGoGetInputSlotsParams: %w", err)
			}
			cmdParams = append(cmdParams, cmdParam)
		}
		return s.spxGetInputSlots(cmdParams)
	case CommandXGoGetProperties:
		var cmdParams XGoGetPropertiesParams
		if len(params.Arguments) != 1 {
			return nil, fmt.Errorf("expected exactly one argument for command %s", CommandXGoGetProperties)
		}
		if err := json.Unmarshal(params.Arguments[0], &cmdParams); err != nil {
			return nil, fmt.Errorf("failed to unmarshal command argument as XGoGetPropertiesParams: %w", err)
		}
		return s.xgoGetProperties(cmdParams)
	}
	return nil, fmt.Errorf("unknown command: %s", params.Command)
}

// spxRenameResources renames spx resources in the workspace.
func (s *Server) spxRenameResources(params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
	result, err := s.compile()
	if err != nil {
		return nil, err
	}
	return s.spxRenameResourcesWithCompileResult(result, params)
}

// spxRenameResourcesWithCompileResult renames spx resources in the workspace with the given compile result.
func (s *Server) spxRenameResourcesWithCompileResult(result *compileResult, params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
	workspaceEdit := WorkspaceEdit{
		Changes: make(map[DocumentURI][]TextEdit),
	}
	seenTextEdits := make(map[DocumentURI]map[TextEdit]struct{})
	for _, param := range params {
		id, err := ParseSpxResourceURI(param.Resource.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse spx resource URI: %w", err)
		}
		var changes map[DocumentURI][]TextEdit
		switch id := id.(type) {
		case SpxBackdropResourceID:
			changes, err = s.spxRenameBackdropResource(result, id, param.NewName)
		case SpxSoundResourceID:
			changes, err = s.spxRenameSoundResource(result, id, param.NewName)
		case SpxSpriteResourceID:
			changes, err = s.spxRenameSpriteResource(result, id, param.NewName)
		case SpxSpriteCostumeResourceID:
			changes, err = s.spxRenameSpriteCostumeResource(result, id, param.NewName)
		case SpxSpriteAnimationResourceID:
			changes, err = s.spxRenameSpriteAnimationResource(result, id, param.NewName)
		case SpxWidgetResourceID:
			changes, err = s.spxRenameWidgetResource(result, id, param.NewName)
		default:
			return nil, fmt.Errorf("unsupported spx resource type: %T", id)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to rename spx resource %q: %w", param.Resource.URI, err)
		}
		for documentURI, textEdits := range changes {
			if _, ok := seenTextEdits[documentURI]; !ok {
				seenTextEdits[documentURI] = make(map[TextEdit]struct{})
			}
			for _, textEdit := range textEdits {
				if _, ok := seenTextEdits[documentURI][textEdit]; ok {
					continue
				}
				seenTextEdits[documentURI][textEdit] = struct{}{}

				workspaceEdit.Changes[documentURI] = append(workspaceEdit.Changes[documentURI], textEdit)
			}
		}
	}
	return &workspaceEdit, nil
}

// spxGetInputSlots gets input slots in a document.
func (s *Server) spxGetInputSlots(params []XGoGetInputSlotsParams) ([]XGoInputSlot, error) {
	if l := len(params); l == 0 {
		return nil, nil
	} else if l > 1 {
		return nil, fmt.Errorf("%s only supports one document at a time", CommandXGoGetInputSlots)
	}
	param := params[0]

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(param.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}

	return findInputSlots(result, astFile), nil
}

// xgoGetProperties gets properties for a specific target (e.g., "Game" or a sprite name).
// Returns a list of properties including:
//  1. Direct fields (non-embedded) of the target type, including unexported fields
//  2. Methods with no parameters (excluding receiver) and exactly one output parameter,
//     including unexported methods
func (s *Server) xgoGetProperties(params XGoGetPropertiesParams) ([]XGoProperty, error) {
	proj := s.getProj()
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, fmt.Errorf("no type information available")
	}

	pkg := typeInfo.Pkg
	if pkg == nil {
		return nil, fmt.Errorf("no package information available")
	}

	// Lookup the target object in the package scope
	obj := pkg.Scope().Lookup(params.Target)
	if obj == nil {
		return nil, fmt.Errorf("target %q not found", params.Target)
	}

	// Get the type of the object
	var namedType *gotypes.Named
	switch obj := obj.(type) {
	case *gotypes.TypeName:
		// If it's a type name (e.g., "Game"), get its underlying type
		// Unalias to handle type aliases (e.g., type MyGame = Game)
		typ := gotypes.Unalias(obj.Type())
		typ = xgoutil.DerefType(typ)
		if named, ok := typ.(*gotypes.Named); ok {
			namedType = named
		} else {
			return nil, fmt.Errorf("target %q is not a named type", params.Target)
		}
	default:
		return nil, fmt.Errorf("target %q is not a type", params.Target)
	}

	// Get underlying struct type
	if _, ok := namedType.Underlying().(*gotypes.Struct); !ok {
		return nil, fmt.Errorf("target %q is not a struct type", params.Target)
	}

	mainPkgDoc, _ := proj.PkgDoc()

	properties := collectPropertiesFromNamedType(namedType, mainPkgDoc)

	slices.SortStableFunc(properties, func(a, b XGoProperty) int {
		if p1, p2 := xgoPropertyKindPriority[a.Kind], xgoPropertyKindPriority[b.Kind]; p1 != p2 {
			return p1 - p2
		}
		return cmp.Compare(a.Name, b.Name)
	})

	return properties, nil
}

// propertyMember holds the resolved information for a single property member
// (field or method) discovered during a type traversal.
type propertyMember struct {
	// Name is the property name (lowerCamelCase for methods, original for fields).
	Name string
	// Type is the property's value type.
	Type gotypes.Type
	// Kind indicates whether the property comes from a field or a method.
	Kind XGoPropertyKind
	// SpxDef is the full spx definition for the member.
	SpxDef SpxDefinition
}

// propertyMembers returns an iterator over property fields and property methods
// in depth-first, outer-scope-first order. Outer members shadow embedded ones
// with the same name.
func propertyMembers(namedType *gotypes.Named, pkgDocFor func(*gotypes.Package) *pkgdoc.PkgDoc) iter.Seq[propertyMember] {
	return func(yield func(propertyMember) bool) {
		visited := make(map[*gotypes.Named]bool)
		seenNames := make(map[string]bool)
		var walk func(namedType *gotypes.Named) bool
		walk = func(namedType *gotypes.Named) bool {
			if visited[namedType] {
				return true
			}
			visited[namedType] = true

			structType, ok := namedType.Underlying().(*gotypes.Struct)
			if !ok {
				return true
			}

			selectorTypeName := namedType.Obj().Name()
			yieldProperty := func(member propertyMember) bool {
				if seenNames[member.Name] {
					return true
				}
				seenNames[member.Name] = true
				return yield(member)
			}

			// Single pass over fields: yield direct property fields and collect
			// embedded types for later recursion, so each field is visited only once.
			var embeddedTypes []*gotypes.Named
			for field := range structType.Fields() {
				if field.Embedded() {
					embeddedType := gotypes.Unalias(xgoutil.DerefType(field.Type()))
					if embNamed, ok := embeddedType.(*gotypes.Named); ok {
						embeddedTypes = append(embeddedTypes, embNamed)
					}
					continue
				}
				if !isPropertyField(field) {
					continue
				}
				name := field.Name()
				if !yieldProperty(propertyMember{
					Name:   name,
					Type:   field.Type(),
					Kind:   XGoPropertyKindField,
					SpxDef: GetSpxDefinitionForVar(field, selectorTypeName, false, pkgDocFor(field.Pkg())),
				}) {
					return false
				}
			}

			// Collect methods defined directly on this type.
			for method := range namedType.Methods() {
				if !isPropertyMethod(method) {
					continue
				}
				name := xgoutil.ToLowerCamelCase(method.Name())
				sig := method.Signature()
				if !yieldProperty(propertyMember{
					Name:   name,
					Type:   sig.Results().At(0).Type(),
					Kind:   XGoPropertyKindMethod,
					SpxDef: GetSpxDefinitionForFunc(method, selectorTypeName, pkgDocFor(method.Pkg())),
				}) {
					return false
				}
			}

			// Recurse into embedded types collected during the field pass.
			for _, embNamed := range embeddedTypes {
				if !walk(embNamed) {
					return false
				}
			}
			return true
		}
		walk(namedType)
	}
}

// makePkgDocFor returns a function that resolves the [pkgdoc.PkgDoc] for a
// given package, using mainPkgDoc for the main package and pre-built package
// data for all others.
func makePkgDocFor(mainPkgDoc *pkgdoc.PkgDoc) func(*gotypes.Package) *pkgdoc.PkgDoc {
	return func(pkg *gotypes.Package) *pkgdoc.PkgDoc {
		if xgoutil.IsMainPkg(pkg) {
			return mainPkgDoc
		}
		doc, _ := pkgdata.GetPkgDoc(xgoutil.PkgPath(pkg))
		return doc
	}
}

// collectPropertiesFromNamedType recursively collects properties from a named type.
func collectPropertiesFromNamedType(namedType *gotypes.Named, mainPkgDoc *pkgdoc.PkgDoc) []XGoProperty {
	var properties []XGoProperty
	for m := range propertyMembers(namedType, makePkgDocFor(mainPkgDoc)) {
		properties = append(properties, XGoProperty{
			Name:       m.Name,
			Type:       GetSimplifiedTypeString(m.Type),
			Kind:       m.Kind,
			Doc:        m.SpxDef.Detail,
			Definition: m.SpxDef.ID,
		})
	}
	return properties
}

// isPropertyField checks if a field should be included as a property.
// Returns true if:
// - The field is not embedded
// - The field type is a basic type (int, float64, string, etc.), spx.Value, or spx.List
func isPropertyField(field *gotypes.Var) bool {
	if field.Embedded() {
		return false
	}

	fieldType := gotypes.Unalias(xgoutil.DerefType(field.Type()))

	// Allow basic types (int, float64, string, bool, etc.)
	if _, ok := fieldType.(*gotypes.Basic); ok {
		if pkg := field.Pkg(); pkg != nil && pkg.Name() == "main" {
			return true
		}
	}

	// Allow spx.Value and spx.List
	if named, ok := fieldType.(*gotypes.Named); ok && isSpxValueOrListType(named) {
		return true
	}

	return false
}

// isPropertyMethod checks if a method should be included as a property.
// Returns true if:
//   - The method name is not reserved for XGo-generated internals
//   - The method name starts with an uppercase letter
//   - The method has no parameters
//   - The method has exactly one return value
//   - The return type is a basic type (int, float64, string, etc.), or a named
//     type from github.com/goplus/spx/v2 named "Value" or "List"
func isPropertyMethod(method *gotypes.Func) bool {
	if xgoutil.IsXGoInternalName(method.Name()) {
		return false
	}
	// Check if the method name starts with a lowercase letter
	if method.Name() != "" && unicode.IsLower(rune(method.Name()[0])) {
		return false
	}
	sig := method.Signature()
	// Only include methods with no parameters and exactly one return value
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}

	// The return type must be a basic type, spx.Value, or spx.List
	retType := gotypes.Unalias(xgoutil.DerefType(sig.Results().At(0).Type()))
	if _, ok := retType.(*gotypes.Basic); ok {
		return true
	}
	if named, ok := retType.(*gotypes.Named); ok && isSpxValueOrListType(named) {
		return true
	}
	return false
}

// isSpxValueOrListType reports whether named is spx.Value or spx.List.
func isSpxValueOrListType(named *gotypes.Named) bool {
	obj := named.Obj()
	pkg := obj.Pkg()
	if pkg == nil || pkg.Path() != SpxPkgPath {
		return false
	}

	switch obj.Name() {
	case "Value", "List":
		return true
	}
	return false
}

// isPropertyOfEnclosingType checks if the given object is a property of its enclosing type.
// This is useful for determining if a rename operation affects a property that may be
// monitored by the IDE. Returns true if the object is a field or method that qualifies
// as a property according to the same criteria used by xgoGetProperties.
func isPropertyOfEnclosingType(obj gotypes.Object) bool {
	if obj == nil {
		return false
	}

	// Check if the current object is a property (field or method)
	switch obj := obj.(type) {
	case *gotypes.Var:
		return obj.IsField() && isPropertyField(obj)
	case *gotypes.Func:
		return isPropertyMethod(obj)
	}

	return false
}

// findEnclosingTypeForField finds the exact enclosing type for a given field.
// This accurately identifies which struct type contains the field, avoiding ambiguity
// when multiple types have fields with the same name.
// Returns the enclosing *types.Named if found, nil otherwise.
//
// Performance note: This function has O(N_types × N_fields_per_type) complexity.
// For better performance in hot paths, consider caching results or using alternative approaches.
func findEnclosingTypeForField(field *gotypes.Var) *gotypes.Named {
	if field == nil || !field.IsField() {
		return nil
	}

	// Find the enclosing type by looking through all types in the package
	pkg := field.Pkg()
	if pkg == nil {
		return nil
	}

	// Search for the type that contains this field by checking all named types in the package
	// Note: types.Var for fields don't have a direct back-reference to their containing struct,
	// so we need to search through package scope.
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		typeObj := scope.Lookup(name)
		if typeObj == nil {
			continue
		}

		typeName, ok := typeObj.(*gotypes.TypeName)
		if !ok {
			continue
		}

		namedType, ok := typeName.Type().(*gotypes.Named)
		if !ok {
			continue
		}

		structType, ok := namedType.Underlying().(*gotypes.Struct)
		if !ok {
			continue
		}

		for structField := range structType.Fields() {
			if structField == field {
				return namedType
			}
		}
	}

	return nil
}

// findEnclosingTypeForMethod finds the exact enclosing type for a given method.
// This returns the receiver type of the method.
// Returns the enclosing *types.Named if found, nil otherwise.
func findEnclosingTypeForMethod(method *gotypes.Func) *gotypes.Named {
	if method == nil {
		return nil
	}

	recv := method.Signature().Recv()
	if recv == nil {
		return nil
	}

	// Dereference pointer receiver if needed
	recvType := xgoutil.DerefType(recv.Type())
	namedType, ok := recvType.(*gotypes.Named)
	if !ok {
		return nil
	}

	return namedType
}

// findEnclosingType finds the exact enclosing type for a given object.
// Supports both fields and methods.
// Returns the enclosing *types.Named if found, nil otherwise.
func findEnclosingType(obj gotypes.Object) *gotypes.Named {
	if obj == nil {
		return nil
	}

	switch obj := obj.(type) {
	case *gotypes.Var:
		if obj.IsField() {
			return findEnclosingTypeForField(obj)
		}
	case *gotypes.Func:
		return findEnclosingTypeForMethod(obj)
	}

	return nil
}

// findInputSlots finds all input slots in the AST file.
func findInputSlots(result *compileResult, astFile *ast.File) []XGoInputSlot {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	var inputSlots []XGoInputSlot
	addInputSlots := func(slots ...XGoInputSlot) {
		for _, slot := range slots {
			if slices.ContainsFunc(inputSlots, func(existing XGoInputSlot) bool {
				return IsRangesOverlap(existing.Range, slot.Range)
			}) {
				continue
			}
			inputSlots = append(inputSlots, slot)
		}
	}
	addInputSlot := func(slot *SpxInputSlot) {
		if slot != nil {
			addInputSlots(*slot)
		}
	}

	ast.Inspect(astFile, func(node ast.Node) bool {
		if node == nil {
			return true
		}

		switch node := node.(type) {
		case *ast.BranchStmt:
			if callExpr := xgoutil.CreateCallExprFromBranchStmt(typeInfo, node); callExpr != nil {
				slots := findInputSlotsFromCallExpr(result, callExpr)
				addInputSlots(slots...)
			}
		case *ast.CallExpr:
			slots := findInputSlotsFromCallExpr(result, node)
			addInputSlots(slots...)
		case *ast.BinaryExpr:
			addInputSlot(checkValueInputSlot(result, node.X, nil))
			addInputSlot(checkValueInputSlot(result, node.Y, nil))
		case *ast.UnaryExpr:
			addInputSlot(checkValueInputSlot(result, node.X, nil))
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				addInputSlot(checkAddressInputSlot(result, lhs))
			}

			for i, rhs := range node.Rhs {
				var declaredType gotypes.Type
				if len(node.Lhs) == len(node.Rhs) {
					declaredType = typeInfo.TypeOf(node.Lhs[i])
				}

				addInputSlot(checkValueInputSlot(result, rhs, declaredType))
			}
		case *ast.ForStmt:
			if node.Init != nil {
				if expr, ok := node.Init.(*ast.ExprStmt); ok {
					addInputSlot(checkValueInputSlot(result, expr.X, nil))
				}
			}

			if node.Cond != nil {
				addInputSlot(checkValueInputSlot(result, node.Cond, gotypes.Typ[gotypes.Bool]))
			}

			if node.Post != nil {
				if expr, ok := node.Post.(*ast.ExprStmt); ok {
					addInputSlot(checkValueInputSlot(result, expr.X, nil))
				}
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

				addInputSlot(checkValueInputSlot(result, value, declaredType))
			}
		case *ast.ReturnStmt:
			for _, res := range node.Results {
				addInputSlot(checkValueInputSlot(result, res, nil))
			}
		case *ast.IfStmt:
			addInputSlot(checkValueInputSlot(result, node.Cond, gotypes.Typ[gotypes.Bool]))
		case *ast.SwitchStmt:
			if node.Tag != nil {
				addInputSlot(checkValueInputSlot(result, node.Tag, nil))
			}
		case *ast.CaseClause:
			for _, expr := range node.List {
				addInputSlot(checkValueInputSlot(result, expr, nil))
			}
		case *ast.RangeStmt:
			if node.Key != nil && !isBlank(node.Key) {
				addInputSlot(checkAddressInputSlot(result, node.Key))
			}

			if node.Value != nil && !isBlank(node.Value) {
				addInputSlot(checkAddressInputSlot(result, node.Value))
			}

			addInputSlot(checkValueInputSlot(result, node.X, nil))
		case *ast.IncDecStmt:
			addInputSlot(checkAddressInputSlot(result, node.X))
		}
		return true
	})
	sortSpxInputSlots(inputSlots)
	return inputSlots
}

// findInputSlotsFromCallExpr finds input slots from a call expression.
func findInputSlotsFromCallExpr(result *compileResult, callExpr *ast.CallExpr) []SpxInputSlot {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	var inputSlots []SpxInputSlot
	for resolvedArg := range resolvedCallExprArgs(result.proj, typeInfo, callExpr) {
		if resolvedArg.ExpectedType == nil || resolvedArg.IsTypeArg() {
			continue
		}

		expectedType := resolvedArg.ExpectedType
		declaredType := xgoutil.DerefType(expectedType)
		if sliceType, ok := declaredType.(*gotypes.Slice); ok {
			declaredType = xgoutil.DerefType(sliceType.Elem())
		}

		var slot *SpxInputSlot
		if lit, ok := resolvedArg.Arg.(*ast.NumberUnitLit); ok {
			unitExpectedType := xgoUnitExpectedTypeForResolvedArg(resolvedArg)
			if len(xgoUnitSpecsForType(unitExpectedType)) == 0 {
				continue
			}
			declaredType = xgoutil.DerefType(unitExpectedType)
			slot = createValueInputSlotFromNumberUnitLit(result, lit, declaredType)
		} else {
			slot = checkValueInputSlot(result, resolvedArg.Arg, declaredType)
		}
		if slot != nil {
			inputSlots = append(inputSlots, *slot)
		}
	}
	return inputSlots
}

// collectPredefinedNames collects all predefined names for the given expression.
func collectPredefinedNames(result *compileResult, expr ast.Expr, declaredType gotypes.Type) []string {
	typeInfo, _ := result.proj.TypeInfo()
	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, expr)
	innermostScope := xgoutil.InnermostScopeAt(result.proj.Fset, typeInfo, astPkg, expr.Pos())

	var names []string
	growNames := func(n int) {
		names = slices.Grow(names, n)
	}
	seenNames := make(map[string]struct{})
	addNameOf := func(obj gotypes.Object) {
		name := obj.Name()
		switch obj := obj.(type) {
		case *gotypes.Var, *gotypes.Const:
			if typ := obj.Type(); typ != nil && declaredType != nil && !gotypes.AssignableTo(typ, declaredType) {
				return
			}

			switch {
			case name == "this",
				xgoutil.IsXGoInternalName(name):
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

			name = xgoutil.ToLowerCamelCase(name)
		default:
			return
		}
		if _, ok := seenNames[name]; ok {
			return
		}
		seenNames[name] = struct{}{}
		names = append(names, name)
	}

	for scope := innermostScope; scope != nil && scope != gotypes.Universe; scope = scope.Parent() {
		growNames(len(scope.Names()))
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if obj == nil {
				continue
			}
			defIdent := typeInfo.ObjToDef[obj]

			if scope != innermostScope || obj.Pos() < expr.Pos() {
				switch obj.(type) {
				case *gotypes.Var, *gotypes.Const:
					addNameOf(obj)
				}
			}

			if astFile.IsClass && xgoutil.IsSyntheticThisIdent(result.proj.Fset, typeInfo, astPkg, defIdent) {
				objType := xgoutil.DerefType(obj.Type())
				named, ok := objType.(*gotypes.Named)
				if !ok || !xgoutil.IsNamedStructType(named) {
					continue
				}

				for structMember := range xgoutil.StructMembers(named) {
					switch member := structMember.Member.(type) {
					case *gotypes.Var:
						if !member.Origin().Embedded() {
							addNameOf(member)
						}
					case *gotypes.Func:
						// Add methods with no parameters and exactly one return value.
						// For example, the method `Game.BackdropName` can be used in `echo backdropname`.
						funcSig := member.Signature()
						if funcSig.Params().Len() == 0 && funcSig.Results().Len() == 1 {
							addNameOf(member)
						}
					}
				}
			}
		}
	}

	for _, scope := range []*gotypes.Scope{
		GetSpxPkg().Scope(),
		GetMathPkg().Scope(),
		gotypes.Universe,
	} {
		growNames(len(scope.Names()))
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if obj == nil {
				continue
			}
			if _, ok := obj.(*gotypes.Var); ok {
				addNameOf(obj)
			}
		}
	}

	return names
}

// checkValueInputSlot checks if the expression is a value input slot.
func checkValueInputSlot(result *compileResult, expr ast.Expr, declaredType gotypes.Type) *SpxInputSlot {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		return createValueInputSlotFromBasicLit(result, expr, declaredType)
	case *ast.Ident:
		return createValueInputSlotFromIdent(result, expr, declaredType)
	case *ast.UnaryExpr:
		return createValueInputSlotFromUnaryExpr(result, expr, declaredType)
	case *ast.CallExpr:
		return createValueInputSlotFromColorFuncCall(result, expr, declaredType)
	}
	return nil
}

// checkAddressInputSlot checks if the expression is an address input slot.
func checkAddressInputSlot(result *compileResult, expr ast.Expr) *SpxInputSlot {
	if ident, ok := expr.(*ast.Ident); ok {
		return &SpxInputSlot{
			Kind:   SpxInputSlotKindAddress,
			Accept: SpxInputSlotAccept{Type: SpxInputTypeUnknown},
			Input: SpxInput{
				Kind: SpxInputKindPredefined,
				Type: SpxInputTypeUnknown,
				Name: ident.Name,
			},
			PredefinedNames: collectPredefinedNames(result, expr, nil),
			Range:           RangeForNode(result.proj, ident),
		}
	}
	return nil
}

// createValueInputSlotFromBasicLit creates a value input slot from a basic literal.
func createValueInputSlotFromBasicLit(result *compileResult, lit *ast.BasicLit, declaredType gotypes.Type) *SpxInputSlot {
	input := SpxInput{Kind: SpxInputKindInPlace}
	switch lit.Kind {
	case token.STRING:
		input.Type = SpxInputTypeString
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil
		}
		input.Value = v
	case token.INT:
		input.Type = SpxInputTypeInteger
		v, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil {
			return nil
		}
		input.Value = v
	case token.FLOAT:
		input.Type = SpxInputTypeDecimal
		v, err := strconv.ParseFloat(lit.Value, 64)
		if err != nil {
			return nil
		}
		input.Value = v
	default:
		return nil
	}

	accept := SpxInputSlotAccept{Type: input.Type}
	if declaredType != nil {
		accept.Type = inferSpxInputTypeFromType(declaredType)
	}
	if accept.Type == SpxInputTypeResourceName {
		for _, spxResourceRef := range result.spxResourceRefs {
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

	return &SpxInputSlot{
		Kind:            SpxInputSlotKindValue,
		Accept:          accept,
		Input:           input,
		PredefinedNames: collectPredefinedNames(result, lit, declaredType),
		Range:           RangeForNode(result.proj, lit),
	}
}

// createValueInputSlotFromNumberUnitLit creates a value input slot from a
// number-with-unit literal.
func createValueInputSlotFromNumberUnitLit(result *compileResult, lit *ast.NumberUnitLit, declaredType gotypes.Type) *SpxInputSlot {
	input := SpxInput{Kind: SpxInputKindInPlace}
	switch lit.Kind {
	case token.INT:
		input.Type = SpxInputTypeInteger
		v, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil {
			return nil
		}
		input.Value = v
	case token.FLOAT:
		input.Type = SpxInputTypeDecimal
		v, err := strconv.ParseFloat(lit.Value, 64)
		if err != nil {
			return nil
		}
		input.Value = v
	default:
		return nil
	}

	accept := SpxInputSlotAccept{Type: input.Type}
	if declaredType != nil {
		if acceptType := inferSpxInputTypeFromType(declaredType); acceptType != SpxInputTypeUnknown {
			accept.Type = acceptType
		}
	}

	return &SpxInputSlot{
		Kind:            SpxInputSlotKindValue,
		Accept:          accept,
		Input:           input,
		PredefinedNames: collectPredefinedNames(result, lit, declaredType),
		Range:           RangeForPosEnd(result.proj, lit.ValuePos, xgoUnitStart(lit)),
	}
}

// createValueInputSlotFromIdent creates a value input slot from an identifier.
func createValueInputSlotFromIdent(result *compileResult, ident *ast.Ident, declaredType gotypes.Type) *SpxInputSlot {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	typ := typeInfo.TypeOf(ident)
	if typ == nil {
		return nil
	}
	typ = xgoutil.DerefType(typ)

	input := SpxInput{
		Kind: SpxInputKindPredefined,
		Type: inferSpxInputTypeFromType(typ),
		Name: ident.Name,
	}
	switch input.Type {
	case SpxInputTypeBoolean:
		if basicType, ok := typ.(*gotypes.Basic); ok && basicType.Kind() == gotypes.UntypedBool {
			input.Kind = SpxInputKindInPlace
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
		obj := typeInfo.ObjectOf(ident)
		if obj != nil && !IsInSpxPkg(obj) {
			break
		}
		cnst, ok := obj.(*gotypes.Const)
		if !ok {
			break
		}
		input.Kind = SpxInputKindInPlace
		switch input.Type {
		case SpxInputTypeDirection:
			input.Value, _ = strconv.ParseFloat(cnst.Val().ExactString(), 64)
		default:
			input.Value = cnst.Name()
		}
		input.Name = ""
	}

	accept := SpxInputSlotAccept{Type: input.Type}
	if declaredType != nil {
		accept.Type = inferSpxInputTypeFromType(declaredType)
	}
	if accept.Type == SpxInputTypeResourceName {
		switch canonicalSpxResourceNameType(declaredType) {
		case GetSpxBackdropNameType():
			accept.ResourceContext = ToPtr(SpxBackdropResourceContextURI)
		case GetSpxSoundNameType():
			accept.ResourceContext = ToPtr(SpxSoundResourceContextURI)
		case GetSpxSpriteNameType():
			accept.ResourceContext = ToPtr(SpxSpriteResourceContextURI)
		case GetSpxSpriteCostumeNameType():
			spxSpriteResource := inferSpxSpriteResourceEnclosingNode(result, ident)
			if spxSpriteResource == nil {
				return nil
			}
			accept.ResourceContext = ToPtr(FormatSpxSpriteCostumeResourceContextURI(spxSpriteResource.Name))
		case GetSpxSpriteAnimationNameType():
			spxSpriteResource := inferSpxSpriteResourceEnclosingNode(result, ident)
			if spxSpriteResource == nil {
				return nil
			}
			accept.ResourceContext = ToPtr(FormatSpxSpriteAnimationResourceContextURI(spxSpriteResource.Name))
		case GetSpxWidgetNameType():
			accept.ResourceContext = ToPtr(SpxWidgetResourceContextURI)
		default:
			return nil
		}
	}

	return &SpxInputSlot{
		Kind:            SpxInputSlotKindValue,
		Accept:          accept,
		Input:           input,
		PredefinedNames: collectPredefinedNames(result, ident, declaredType),
		Range:           RangeForNode(result.proj, ident),
	}
}

// createValueInputSlotFromUnaryExpr creates a value input slot from a unary expression.
func createValueInputSlotFromUnaryExpr(result *compileResult, expr *ast.UnaryExpr, declaredType gotypes.Type) *SpxInputSlot {
	var inputSlot *SpxInputSlot
	switch x := expr.X.(type) {
	case *ast.BasicLit:
		inputSlot = createValueInputSlotFromBasicLit(result, x, declaredType)
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
			switch x.Kind {
			case token.INT:
				switch v := inputSlot.Input.Value.(type) {
				case int64:
					inputSlot.Input.Value = ^v
				default:
					return nil
				}
			default:
				return nil
			}
		}
	case *ast.Ident:
		inputSlot = createValueInputSlotFromIdent(result, x, declaredType)
		if inputSlot == nil {
			return nil
		}

		switch expr.Op {
		case token.NOT:
			switch v := inputSlot.Input.Value.(type) {
			case bool:
				inputSlot.Input.Value = !v
			default:
				return nil
			}
		}
	default:
		return nil
	}
	inputSlot.Range = RangeForNode(result.proj, expr) // Update the range to include the entire unary expression.
	return inputSlot
}

// createValueInputSlotFromColorFuncCall creates a value input slot from an spx
// color function call.
func createValueInputSlotFromColorFuncCall(result *compileResult, callExpr *ast.CallExpr, declaredType gotypes.Type) *SpxInputSlot {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	fun := xgoutil.FuncFromCallExpr(typeInfo, callExpr)
	if fun == nil || !IsInSpxPkg(fun) || !isSpxColorFunc(fun) {
		return nil
	}

	constructor := SpxInputTypeSpxColorConstructor(fun.Name())
	maxArgs := 3
	switch constructor {
	case SpxInputTypeSpxColorConstructorHSB:
	case SpxInputTypeSpxColorConstructorHSBA:
		maxArgs = 4
	default:
		return nil // This should never happen, but just in case.
	}

	var args []float64
	for i, argExpr := range callExpr.Args {
		if i >= maxArgs {
			break
		}
		lit, ok := argExpr.(*ast.BasicLit)
		if !ok {
			return nil
		}

		var val float64
		switch lit.Kind {
		case token.FLOAT:
			floatVal, err := strconv.ParseFloat(lit.Value, 64)
			if err != nil {
				return nil
			}
			val = floatVal
		case token.INT:
			intVal, err := strconv.ParseInt(lit.Value, 0, 64)
			if err != nil {
				return nil
			}
			val = float64(intVal)
		default:
			return nil
		}
		args = append(args, val)
	}
	if len(args) < maxArgs {
		return nil
	}

	return &SpxInputSlot{
		Kind:   SpxInputSlotKindValue,
		Accept: SpxInputSlotAccept{Type: SpxInputTypeColor},
		Input: SpxInput{
			Kind: SpxInputKindInPlace,
			Type: SpxInputTypeColor,
			Value: SpxColorInputValue{
				Constructor: constructor,
				Args:        args,
			},
		},
		PredefinedNames: collectPredefinedNames(result, callExpr, declaredType),
		Range:           RangeForNode(result.proj, callExpr),
	}
}

// isSpxColorFunc checks if the fun is an spx color function.
func isSpxColorFunc(fun *gotypes.Func) bool {
	switch fun {
	case GetSpxHSBFunc(), GetSpxHSBAFunc():
		return true
	}
	return false
}

// inferSpxInputTypeFromType attempts to infer the input type from the given type.
func inferSpxInputTypeFromType(typ gotypes.Type) SpxInputType {
	if basicType, ok := typ.(*gotypes.Basic); ok {
		switch basicType.Kind() {
		case gotypes.String, gotypes.UntypedString:
			return SpxInputTypeString
		case gotypes.Int, gotypes.Int8, gotypes.Int16, gotypes.Int32, gotypes.Int64,
			gotypes.Uint, gotypes.Uint8, gotypes.Uint16, gotypes.Uint32, gotypes.Uint64,
			gotypes.UntypedInt:
			return SpxInputTypeInteger
		case gotypes.Float32, gotypes.Float64, gotypes.UntypedFloat:
			return SpxInputTypeDecimal
		case gotypes.Bool, gotypes.UntypedBool:
			return SpxInputTypeBoolean
		}
		return SpxInputTypeUnknown
	}

	if IsSpxResourceNameType(typ) {
		return SpxInputTypeResourceName
	}

	switch typ {
	case GetSpxDirectionType():
		return SpxInputTypeDirection
	case GetSpxLayerActionType():
		return SpxInputTypeLayerAction
	case GetSpxDirActionType():
		return SpxInputTypeDirAction
	case GetSpxEffectKindType():
		return SpxInputTypeEffectKind
	case GetSpxKeyType():
		return SpxInputTypeKey
	case GetSpxSpecialObjType():
		return SpxInputTypeSpecialObj
	case GetSpxRotationStyleType():
		return SpxInputTypeRotationStyle
	case GetSpxPropertyNameType():
		return SpxInputTypePropertyName
	}

	// Fall back to the alias RHS when no direct basic or spx type match is found.
	if alias, ok := typ.(*gotypes.Alias); ok {
		rhs := alias.Rhs()
		if rhs != nil && rhs != typ {
			return inferSpxInputTypeFromType(rhs)
		}
	}
	return SpxInputTypeUnknown
}

// inferSpxSpriteResourceEnclosingNode infers the enclosing [SpxSpriteResource]
// for the given node. It returns nil if no [SpxSpriteResource] can be inferred.
func inferSpxSpriteResourceEnclosingNode(result *compileResult, node ast.Node) *SpxSpriteResource {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	spxFile := xgoutil.NodeFilename(result.proj.Fset, node)
	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, node)

	var spxSpriteResource *SpxSpriteResource
	for pathNode := range xgoutil.PathEnclosingIntervalNodes(astFile, node.Pos(), node.End(), false) {
		if pathNode == nil {
			continue
		}

		callExpr, ok := pathNode.(*ast.CallExpr)
		if !ok {
			continue
		}

		var spxSpriteName string
		if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				break
			}
			obj := typeInfo.ObjectOf(ident)
			if obj == nil {
				break
			}
			named, ok := xgoutil.DerefType(obj.Type()).(*gotypes.Named)
			if !ok {
				break
			}

			if named == GetSpxSpriteType() {
				spxSpriteName = ident.Name
			} else if result.hasSpxSpriteType(named) {
				spxSpriteName = obj.Name()
			}
		} else if spxFile != "main.spx" {
			spxSpriteName = strings.TrimSuffix(spxFile, ".spx")
		}
		spxSpriteResource = result.spxResourceSet.sprites[spxSpriteName]
		break
	}
	return spxSpriteResource
}

// isBlank checks if an expression is a blank identifier (_).
func isBlank(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "_"
}

// sortSpxInputSlots sorts the given spx input slots in a stable manner.
func sortSpxInputSlots(slots []SpxInputSlot) {
	slices.SortFunc(slots, func(a, b SpxInputSlot) int {
		// First sort by line number.
		if a.Range.Start.Line != b.Range.Start.Line {
			return cmp.Compare(a.Range.Start.Line, b.Range.Start.Line)
		}
		// If same line, sort by character position.
		if a.Range.Start.Character != b.Range.Start.Character {
			return cmp.Compare(a.Range.Start.Character, b.Range.Start.Character)
		}
		// If same position (unlikely), sort by input kind for stability.
		return cmp.Compare(a.Kind, b.Kind)
	})
}
