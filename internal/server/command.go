package server

import (
	"cmp"
	"encoding/json"
	"fmt"
	gotypes "go/types"
	"iter"
	"slices"
	"unicode"

	"github.com/goplus/xgo/cl"
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
		return s.renameResources(cmdParams)
	case CommandXGoGetInputSlots, CommandSpxGetInputSlots:
		var cmdParams []XGoGetInputSlotsParams
		for _, arg := range params.Arguments {
			var cmdParam XGoGetInputSlotsParams
			if err := json.Unmarshal(arg, &cmdParam); err != nil {
				return nil, fmt.Errorf("failed to unmarshal command argument as XGoGetInputSlotsParams: %w", err)
			}
			cmdParams = append(cmdParams, cmdParam)
		}
		return s.xgoGetInputSlots(cmdParams)
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

// xgoGetProperties gets properties for a project or class type.
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

	typeName, ok := obj.(*gotypes.TypeName)
	if !ok {
		return nil, fmt.Errorf("target %q is not a type", params.Target)
	}

	typ := gotypes.Unalias(typeName.Type())
	namedType, ok := xgoutil.DerefType(typ).(*gotypes.Named)
	if !ok {
		return nil, fmt.Errorf("target %q is not a named type", params.Target)
	}
	if _, ok := namedType.Underlying().(*gotypes.Struct); !ok {
		return nil, fmt.Errorf("target %q is not a struct type", params.Target)
	}

	pos := typeName.Pos()
	file := sourceASTFile(proj, pos)
	if file == nil {
		// Generated class types can have no declaration position. Resolve
		// their source file through the same registration as the compiler.
		astPkg, _ := proj.ASTPackage()
		if astPkg != nil {
			for filename, candidate := range astPkg.Files {
				if name, _ := cl.GetFileClassType(candidate, filename, proj.Module().LookupClass); name == typeName.Name() {
					file, pos = candidate, candidate.Pos()
					break
				}
			}
		}
	}
	ctx := &definitionContext{
		typeDisplay:  newTypeDisplay(proj, file, pos),
		proj:         proj,
		lookupPkgDoc: s.lookupPkgDoc,
	}
	properties := ctx.collectPropertiesFromNamedType(namedType)

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
	// Definition describes the member for documentation and completion.
	Definition symbolDefinition
}

// propertyObject holds the source object for a property discovered during a
// type traversal.
type propertyObject struct {
	Name   string
	Object gotypes.Object
}

// propertyObjects returns an iterator over property source objects in
// depth-first, outer-scope-first order. Accessible outer members shadow
// embedded properties with the same source name, even if the outer member
// is not a property.
func (r *definitionContext) propertyObjects(namedType *gotypes.Named) iter.Seq[propertyObject] {
	return func(yield func(propertyObject) bool) {
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

			yieldProperty := func(property propertyObject) bool {
				if !xgoutil.IsExportedOrInMainPkg(property.Object) || seenNames[property.Name] {
					return true
				}
				seenNames[property.Name] = true
				if !r.isPropertyOfEnclosingType(property.Object) {
					return true
				}
				return yield(property)
			}

			var embeddedTypes []*gotypes.Named
			for field := range structType.Fields() {
				if field.Embedded() {
					embeddedType := gotypes.Unalias(xgoutil.DerefType(field.Type()))
					if embeddedNamed, ok := embeddedType.(*gotypes.Named); ok {
						embeddedTypes = append(embeddedTypes, embeddedNamed)
					}
				}
				if !yieldProperty(propertyObject{
					Name:   field.Name(),
					Object: field,
				}) {
					return false
				}
			}

			// Exact local method names take precedence over property aliases.
			for method := range namedType.Methods() {
				if !method.Exported() && xgoutil.IsInMainPkg(method) {
					seenNames[method.Name()] = true
				}
			}
			for method := range namedType.Methods() {
				if !yieldProperty(propertyObject{
					Name:   xgoutil.ToLowerCamelCase(method.Name()),
					Object: method,
				}) {
					return false
				}
			}

			for _, embeddedType := range embeddedTypes {
				if !walk(embeddedType) {
					return false
				}
			}
			return true
		}
		walk(namedType)
	}
}

// propertyMembers returns an iterator over property fields and property methods
// in depth-first, outer-scope-first order. Outer members shadow embedded ones
// with the same name.
func (r *definitionContext) propertyMembers(namedType *gotypes.Named) iter.Seq[propertyMember] {
	return func(yield func(propertyMember) bool) {
		for property := range r.propertyObjects(namedType) {
			var member propertyMember
			switch object := property.Object.(type) {
			case *gotypes.Var:
				member = propertyMember{
					Name: property.Name,
					Type: object.Type(),
					Kind: XGoPropertyKindField,
				}
			case *gotypes.Func:
				member = propertyMember{
					Name: property.Name,
					Type: object.Signature().Results().At(0).Type(),
					Kind: XGoPropertyKindMethod,
				}
			default:
				continue
			}
			for _, def := range r.definitionsForSelection(property.Object, namedType) {
				member.Definition = def
				if !yield(member) {
					return
				}
			}
		}
	}
}

// collectPropertiesFromNamedType recursively collects properties from a named type.
func (r *definitionContext) collectPropertiesFromNamedType(namedType *gotypes.Named) []XGoProperty {
	var properties []XGoProperty
	for m := range r.propertyMembers(namedType) {
		properties = append(properties, XGoProperty{
			Name:       m.Name,
			Type:       r.typeString(m.Type),
			Kind:       m.Kind,
			Doc:        m.Definition.Detail,
			Definition: m.Definition.ID,
		})
	}
	return properties
}

// isPropertyField includes unembedded basic fields in the main package and
// fields whose type is exposed by the framework.
func (r *definitionContext) isPropertyField(field *gotypes.Var) bool {
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

	// Include property types supplied by the framework.
	if named, ok := fieldType.(*gotypes.Named); ok && r.isFrameworkPropertyType(named) {
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
//   - The return type is a basic type (int, float64, string, etc.), or a framework property type
func (r *definitionContext) isPropertyMethod(method *gotypes.Func) bool {
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

	// The return type must be a basic type or a framework property type.
	retType := gotypes.Unalias(xgoutil.DerefType(sig.Results().At(0).Type()))
	if _, ok := retType.(*gotypes.Basic); ok {
		return true
	}
	if named, ok := retType.(*gotypes.Named); ok && r.isFrameworkPropertyType(named) {
		return true
	}
	return false
}

// isPropertyOfEnclosingType checks if the given object is a property of its enclosing type.
// This is useful for determining if a rename operation affects a property that may be
// monitored by the IDE. Returns true if the object is a field or method that qualifies
// as a property according to the same criteria used by xgoGetProperties.
func (r *definitionContext) isPropertyOfEnclosingType(obj gotypes.Object) bool {
	if obj == nil {
		return false
	}

	// Check if the current object is a property (field or method)
	switch obj := obj.(type) {
	case *gotypes.Var:
		return obj.IsField() && r.isPropertyField(obj)
	case *gotypes.Func:
		return r.isPropertyMethod(obj)
	}

	return false
}
