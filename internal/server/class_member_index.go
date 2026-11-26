package server

import (
	gotypes "go/types"

	"github.com/goplus/gogen"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// indexedClassMember records a method and its declaring type. The zero value
// denotes a field or conflicting declarations for a name in the index.
type indexedClassMember struct {
	method *gotypes.Func
	owner  *gotypes.Named
}

// classMemberIndex records declared names across a class's embedding graph.
type classMemberIndex map[string]indexedClassMember

// buildClassMemberIndex indexes nongeneric named structs and their embeddings.
// An empty index disables reuse for other embedded types, whose selectors may
// depend on compiler rules beyond declared methods and fields.
func buildClassMemberIndex(receiver gotypes.Type) classMemberIndex {
	structures := make(map[*gotypes.Named]*gotypes.Struct)
	var memberCount int
	var walk func(gotypes.Type) bool
	walk = func(typ gotypes.Type) bool {
		// Gogen unwraps aliases at the outer receiver and pointer elements.
		if ptr, ok := typ.(*gotypes.Pointer); ok {
			typ = gotypes.Unalias(ptr.Elem())
		}
		named, ok := typ.(*gotypes.Named)
		if !ok || named.TypeParams().Len() != 0 {
			return false
		}
		if structures[named] != nil {
			return true
		}
		structure, ok := named.Underlying().(*gotypes.Struct)
		if !ok {
			return false
		}
		structures[named] = structure
		memberCount += named.NumMethods() + structure.NumFields()
		for field := range structure.Fields() {
			if field.Embedded() && !walk(field.Type()) {
				return false
			}
		}
		return true
	}
	if !walk(gotypes.Unalias(receiver)) {
		return classMemberIndex{}
	}

	index := make(classMemberIndex, memberCount)
	add := func(name string, member indexedClassMember) {
		if _, exists := index[name]; exists {
			member = indexedClassMember{}
		}
		index[name] = member
	}
	for named, structure := range structures {
		for method := range named.Methods() {
			add(method.Name(), indexedClassMember{method: method, owner: named})
		}
		for field := range structure.Fields() {
			add(field.Name(), indexedClassMember{})
		}
	}
	return index
}

// ordinaryImportedMethod identifies an imported method whose selector has no
// competing declaration under either its exact spelling or its alias.
func (index classMemberIndex) ordinaryImportedMethod(name, projectPath string) indexedClassMember {
	member, exact := index[name]
	if alias := xgoutil.MemberNameForAlias(name); alias != name {
		if candidate, ok := index[alias]; ok {
			if exact {
				return indexedClassMember{}
			}
			member = candidate
		}
	}
	method := member.method
	if method == nil || !method.Exported() || method.Pkg() == nil || method.Pkg().Path() == projectPath || method.Origin() != method {
		return indexedClassMember{}
	}
	signature := method.Signature()
	if _, extended := gogen.CheckFuncEx(signature); extended || signature.TypeParams().Len() != 0 || signature.RecvTypeParams().Len() != 0 {
		return indexedClassMember{}
	}
	return member
}

// importedMethodKey identifies a selector by package path, declaring type name,
// and spelling. A later lookup can replace an older result for the same key
// if the imported method object changes.
type importedMethodKey struct {
	pkgPath  string
	receiver string
	name     string
}

// evaluateClassMember reuses Gogen's result when the current receiver can
// only resolve the selector to the same imported method.
func (r *autoPropertyResolver) evaluateClassMember(key autoPropertyKey) autoProperty {
	if r.classMembers == nil {
		r.classMembers = buildClassMemberIndex(r.receiver)
	}
	member := r.classMembers.ordinaryImportedMethod(key.name, r.proj.PkgPath)
	method := member.method
	if method == nil {
		return r.evaluate(key)
	}
	sharedKey := importedMethodKey{pkgPath: method.Pkg().Path(), receiver: member.owner.Obj().Name(), name: key.name}
	if cached, ok := r.classCache.importedResults.Load(sharedKey); ok {
		result := cached.(autoProperty)
		if result.selectedMember == method {
			return result
		}
	}
	result := r.evaluate(key)
	if result.selectedMember == method && (result.object == nil || result.object == method) &&
		(result.implementation == nil || result.implementation == method) {
		r.classCache.importedResults.Store(sharedKey, result)
	}
	return result
}
