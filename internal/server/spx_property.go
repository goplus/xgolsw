package server

import (
	gotypes "go/types"
	"iter"

	"github.com/goplus/gogen"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// properties supplies the monitor property surface of an spx receiver. Other
// receivers, including ordinary types in an spx project, use XGo semantics.
func (r *spxSymbols) properties(named *gotypes.Named) iter.Seq[propertyObject] {
	if !r.isPropertyTarget(named, make(map[*gotypes.Named]bool)) {
		return nil
	}
	return r.monitorProperties(named)
}

// isPropertyTarget recognizes the SDK's base types and types embedding them by
// identity. A coincidentally named type or a second importer cannot opt in.
func (r *spxSymbols) isPropertyTarget(named *gotypes.Named, seen map[*gotypes.Named]bool) bool {
	if seen[named] {
		return false
	}
	seen[named] = true
	switch r.spxTypeName(named) {
	case "Game", "Sprite", "SpriteImpl":
		return true
	}
	if structure, ok := named.Underlying().(*gotypes.Struct); ok {
		for field := range structure.Fields() {
			if embedded := resolvedNamedType(field.Type()); field.Embedded() && embedded != nil && r.isPropertyTarget(embedded, seen) {
				return true
			}
		}
	}
	return false
}

// monitorProperties follows spx's runtime field lookup and reflected method
// set. Fields are searched breadth first and precede methods. Only exported
// embedded fields expose their contents. Equal-depth fields use source order.
func (r *spxSymbols) monitorProperties(named *gotypes.Named) iter.Seq[propertyObject] {
	return func(yield func(propertyObject) bool) {
		seenTypes := make(map[*gotypes.Named]bool)
		seenNames := make(map[string]bool)
		for queue := []*gotypes.Named{named}; len(queue) != 0; {
			var next []*gotypes.Named
			for _, current := range queue {
				if seenTypes[current] {
					continue
				}
				seenTypes[current] = true
				structure, ok := current.Underlying().(*gotypes.Struct)
				if !ok {
					continue
				}
				for field := range structure.Fields() {
					if field.Embedded() && field.Exported() {
						if embedded := resolvedNamedType(field.Type()); embedded != nil {
							next = append(next, embedded)
						}
					}
					if seenNames[field.Name()] {
						continue
					}
					seenNames[field.Name()] = true
					if field.Embedded() || !r.isMonitorField(field) {
						continue
					}
					if !yield(propertyObject{Name: field.Name(), Object: field, Type: field.Type()}) {
						return
					}
				}
			}
			queue = next
		}
		methods := gotypes.NewMethodSet(gotypes.NewPointer(named))
		for i := range methods.Len() {
			method := methods.At(i).Obj().(*gotypes.Func)
			if !method.Exported() || xgoutil.IsXGoInternalName(method.Name()) {
				continue
			}
			if xgoutil.IsMarkedAsXGoPackage(method.Pkg()) && xgoutil.IsXGoOverloadedFuncName(method.Name()) {
				continue
			}
			sig := method.Signature()
			if _, extended := gogen.CheckFuncEx(sig); extended || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
				continue
			}
			name := xgoutil.ToLowerCamelCase(method.Name())
			if name == method.Name() || seenNames[name] || !r.isMonitorValueType(sig.Results().At(0).Type()) {
				continue
			}
			if !yield(propertyObject{Name: name, Object: method, Type: sig.Results().At(0).Type()}) {
				return
			}
		}
	}
}

// isMonitorField restricts monitor fields to project scalars and SDK containers.
func (r *spxSymbols) isMonitorField(field *gotypes.Var) bool {
	typ := gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(field.Type())))
	if _, basic := typ.(*gotypes.Basic); basic {
		return xgoutil.IsInMainPkg(field)
	}
	return r.isPropertyTypeName(typ)
}

// isMonitorValueType reports the values offered by the monitor UI.
func (r *spxSymbols) isMonitorValueType(typ gotypes.Type) bool {
	typ = gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(typ)))
	if _, basic := typ.(*gotypes.Basic); basic {
		return true
	}
	return r.isPropertyTypeName(typ)
}

// isPropertyTypeName recognizes the SDK's monitor containers.
func (r *spxSymbols) isPropertyTypeName(typ gotypes.Type) bool {
	switch r.spxTypeName(typ) {
	case "Value", "List":
		return true
	}
	return false
}
