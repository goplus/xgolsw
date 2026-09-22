package server

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"iter"
	"slices"

	"github.com/goplus/xgo/cl"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// xgoPropertyKindPriority defines the presentation order for XGo properties.
var xgoPropertyKindPriority = map[XGoPropertyKind]int{
	XGoPropertyKindField:  0,
	XGoPropertyKindMethod: 1,
}

// xgoGetProperties returns accessible fields and single-value auto-properties.
// Framework adapters may supply properties exposed by their runtime instead.
func (s *Server) xgoGetProperties(params XGoGetPropertiesParams) ([]XGoProperty, error) {
	proj := s.requestProject()
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

	typ := gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(typeName.Type())))
	namedType, ok := typ.(*gotypes.Named)
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

// propertyObject identifies a readable field or implicit call and its result.
// Object is the source member. Function is the implementation selected for an
// implicit call and may differ from an overload or template declaration.
type propertyObject struct {
	Name     string
	Object   gotypes.Object
	Type     gotypes.Type
	Function *gotypes.Func
}

// propertyObjects resolves properties against the actual receiver. Framework
// policies apply only to the receiver types owned by that framework.
func (r *definitionContext) propertyObjects(named *gotypes.Named) iter.Seq[propertyObject] {
	if adapter := r.frameworkAdapter(); adapter != nil {
		if properties := adapter.properties(named); properties != nil {
			return properties
		}
	}
	return func(yield func(propertyObject) bool) {
		resolver := autoPropertyResolver{proj: r.proj, receiver: gotypes.NewPointer(named)}
		for name, resolved := range resolver.candidates() {
			if !xgoutil.IsValidType(resolved.typ) || resolved.object == nil {
				continue
			}
			if _, tuple := resolved.typ.(*gotypes.Tuple); tuple {
				continue
			}
			if field, ok := resolved.object.(*gotypes.Var); ok && field.Embedded() {
				continue
			}
			if !yield(propertyObject{Name: name, Object: resolved.object, Type: resolved.typ, Function: resolved.function}) {
				return
			}
		}
	}
}

// propertyCandidates yields possible names without deciding shadowing. An
// outer field named Size hides the method spelling Size, but need not hide
// an inherited XGo alias size. The compiler resolves each candidate name.
func propertyCandidates(typ gotypes.Type) iter.Seq[gotypes.Object] {
	return func(yield func(gotypes.Object) bool) {
		seen := make(map[gotypes.Type]bool)
		var walk func(gotypes.Type) bool
		walk = func(typ gotypes.Type) bool {
			typ = gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(typ)))
			if seen[typ] {
				return true
			}
			seen[typ] = true
			if named, ok := typ.(*gotypes.Named); ok {
				for method := range named.Methods() {
					if !yield(method) {
						return false
					}
				}
			}
			switch underlying := typ.Underlying().(type) {
			case *gotypes.Struct:
				for field := range underlying.Fields() {
					if !yield(field) {
						return false
					}
				}
				for field := range underlying.Fields() {
					if field.Embedded() && !walk(field.Type()) {
						return false
					}
				}
			case *gotypes.Interface:
				for method := range underlying.Methods() {
					if !yield(method) {
						return false
					}
				}
			}
			return true
		}
		walk(typ)
	}
}

// propertyMember describes a resolved property for client presentation.
type propertyMember struct {
	propertyObject
	Kind       XGoPropertyKind
	Definition symbolDefinition
}

// propertyMembers attaches documentation and definition identifiers to properties.
func (r *definitionContext) propertyMembers(named *gotypes.Named) iter.Seq[propertyMember] {
	return func(yield func(propertyMember) bool) {
		for property := range r.propertyObjects(named) {
			kind := XGoPropertyKindField
			if _, method := property.Object.(*gotypes.Func); method {
				kind = XGoPropertyKindMethod
			}
			obj := property.Object
			if property.Function != nil {
				obj = property.Function
			}
			for _, def := range r.definitionsForSelection(obj, named) {
				if !yield(propertyMember{property, kind, def}) {
					return
				}
			}
		}
	}
}

// collectPropertiesFromNamedType describes the receiver's readable properties.
func (r *definitionContext) collectPropertiesFromNamedType(named *gotypes.Named) []XGoProperty {
	var properties []XGoProperty
	for m := range r.propertyMembers(named) {
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
