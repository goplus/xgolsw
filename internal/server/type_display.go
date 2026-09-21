package server

import (
	gotypes "go/types"
	"iter"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// typeDisplay formats types in a source context. Its zero value uses package
// names when no source context is available.
type typeDisplay struct {
	qualifier   gotypes.Qualifier
	unqualified func(*gotypes.TypeName) bool
	sourceName  func(*gotypes.TypeName) (string, bool)
}

// newTypeDisplay resolves package qualifiers at pos in file. The result belongs
// to this source snapshot and must not be shared across files or edits. The
// caller must have obtained non-nil project type information.
func newTypeDisplay(proj *xgo.Project, file *ast.File, pos token.Pos) typeDisplay {
	info, _ := proj.TypeInfo()
	scope := info.Pkg.Scope()
	bindings := importsForFile(proj, file)
	lookups := bindings.members
	imports := make(map[*gotypes.Package][]*gotypes.PkgName)
	for _, name := range bindings.names {
		imports[name.Imported()] = append(imports[name.Imported()], name)
	}
	if file != nil {
		astPkg, _ := proj.ASTPackage()
		if inner := xgoutil.InnermostScopeAt(proj.Fset, info, astPkg, pos); inner != nil {
			scope = inner
		}
	}

	// Named imports only resolve package qualifiers. They do not hide bare
	// names from classfile lookup packages or dot imports.
	lookup := func(name string) gotypes.Object {
		for current := scope; current != nil; {
			at, obj := current.LookupParent(name, pos)
			if _, imported := obj.(*gotypes.PkgName); !imported {
				return obj
			}
			current = at.Parent()
		}
		return nil
	}

	unqualified := func(obj *gotypes.TypeName) bool {
		if obj.Pkg() == info.Pkg {
			visible := lookup(obj.Name())
			return visible == nil || visible == obj
		}
		if visible := lookup(obj.Name()); visible != nil {
			return visible == obj
		}
		var found gotypes.Object
		for _, pkg := range lookups {
			if candidate := pkg.Scope().Lookup(obj.Name()); candidate != nil && candidate.Exported() {
				if found != nil {
					return false
				}
				found = candidate
			}
		}
		return found == obj
	}
	// A class member can shadow a type in expression context, including the
	// function position of a conversion. Type annotations have no such conflict.
	class := classTypeForFile(proj, file)
	sourceLookup := func(name string) gotypes.Object {
		at, obj := scope.LookupParent(name, pos)
		if obj != nil && at != info.Pkg.Scope() && at != gotypes.Universe && at != info.Scopes[file] {
			return obj
		}
		if class != nil {
			if member, _, _ := gotypes.LookupFieldOrMethod(class, true, info.Pkg, name); member != nil {
				return member
			}
			var alias string
			if name[0] >= 'a' && name[0] <= 'z' {
				alias = string(name[0]-'a'+'A') + name[1:]
			} else if name[0] == '_' {
				alias = "XGo" + name
			}
			if alias != "" {
				member, _, _ := gotypes.LookupFieldOrMethod(class, true, info.Pkg, alias)
				if fun, ok := member.(*gotypes.Func); ok && methodHasAutoProperty(fun.Type(), 0) {
					return fun
				}
			}
		}
		return lookup(name)
	}
	return typeDisplay{qualifier: func(pkg *gotypes.Package) string {
		for _, name := range imports[pkg] {
			if obj := lookup(name.Name()); obj == nil || obj.Parent() == gotypes.Universe {
				return name.Name()
			}
		}
		// Keep inaccessible packages identifiable, including shadowed import
		// names and types from packages absent from this file's imports.
		if lookup(pkg.Name()) != nil || bindings.named[pkg.Name()] != nil {
			return pkg.Path()
		}
		for imported := range imports {
			if imported != pkg && imported.Name() == pkg.Name() {
				return pkg.Path()
			}
		}
		for _, implicit := range lookups {
			if implicit != pkg && implicit.Name() == pkg.Name() {
				return pkg.Path()
			}
		}
		return pkg.Name()
	}, unqualified: unqualified, sourceName: func(obj *gotypes.TypeName) (string, bool) {
		if unqualified(obj) {
			if visible := sourceLookup(obj.Name()); visible == nil || visible == obj {
				return obj.Name(), true
			}
		}
		if !obj.Exported() {
			return "", false
		}
		for _, name := range imports[obj.Pkg()] {
			if visible := sourceLookup(name.Name()); visible == nil || visible.Parent() == gotypes.Universe {
				return name.Name() + "." + obj.Name(), true
			}
		}
		return "", false
	}}
}

// sourceTypeString formats a type only when every name resolves in the source
// context. Display-only package qualifiers cannot be used in generated edits.
func (d typeDisplay) sourceTypeString(typ gotypes.Type) (string, bool) {
	unqualified := make(map[*gotypes.TypeName]bool)
	qualifiers := make(map[*gotypes.Package]string)
	for obj := range displayedTypeNames(typ) {
		name, ok := d.sourceName(obj)
		if !ok {
			return "", false
		}
		if qualifier, _, qualified := strings.Cut(name, "."); qualified {
			qualifiers[obj.Pkg()] = qualifier
		} else {
			unqualified[obj] = true
		}
	}
	d.unqualified = func(obj *gotypes.TypeName) bool { return unqualified[obj] }
	d.qualifier = func(pkg *gotypes.Package) string { return qualifiers[pkg] }
	return d.typeString(typ), true
}

// typeString formats a type using the source context's package qualifiers.
func (d typeDisplay) typeString(typ gotypes.Type) string {
	// Basic types and type parameters have no package qualifier, except
	// unsafe.Pointer. They need no scope lookup or composite traversal.
	switch typ := typ.(type) {
	case *gotypes.Basic:
		if typ.Kind() != gotypes.UnsafePointer {
			return typ.String()
		}
	case *gotypes.TypeParam:
		return typ.String()
	}
	qualifier := d.qualifier
	if qualifier == nil {
		qualifier = func(pkg *gotypes.Package) string { return pkg.Name() }
	}
	if d.unqualified == nil {
		return gotypes.TypeString(typ, qualifier)
	}
	// TypeString qualifies packages rather than individual names. If a
	// composite type contains a shadowed name, keep its package qualified
	// throughout that type to avoid confusing it with the visible name.
	short := make(map[*gotypes.Package]bool)
	for obj := range displayedTypeNames(typ) {
		if _, parameter := obj.Type().(*gotypes.TypeParam); parameter {
			continue
		}
		pkg := obj.Pkg()
		if value, seen := short[pkg]; !seen || value {
			short[pkg] = d.unqualified(obj)
		}
	}
	qualifiers := make(map[*gotypes.Package]string, len(short))
	owners := make(map[string]*gotypes.Package)
	for pkg, omit := range short {
		if pkg == nil || omit {
			continue
		}
		name := qualifier(pkg)
		qualifiers[pkg] = name
		if previous := owners[name]; previous != nil && previous != pkg {
			qualifiers[previous] = previous.Path()
			qualifiers[pkg] = pkg.Path()
		} else {
			owners[name] = pkg
		}
	}
	return gotypes.TypeString(typ, func(pkg *gotypes.Package) string { return qualifiers[pkg] })
}

// displayedTypeNames visits names printed by TypeString, including type
// arguments but not the underlying types of named types or aliases.
func displayedTypeNames(typ gotypes.Type) iter.Seq[*gotypes.TypeName] {
	return func(yield func(*gotypes.TypeName) bool) {
		seen := make(map[gotypes.Type]bool)
		pending := []gotypes.Type{typ}
		for len(pending) > 0 {
			typ := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if typ == nil || seen[typ] {
				continue
			}
			seen[typ] = true
			switch typ := typ.(type) {
			case interface {
				Obj() *gotypes.TypeName
				TypeArgs() *gotypes.TypeList
				TypeParams() *gotypes.TypeParamList
			}:
				if !yield(typ.Obj()) {
					return
				}
				for arg := range typ.TypeArgs().Types() {
					pending = append(pending, arg)
				}
				if typ.TypeArgs().Len() == 0 {
					for param := range typ.TypeParams().TypeParams() {
						pending = append(pending, param.Constraint())
					}
				}
			case *gotypes.Basic:
				if typ.Kind() == gotypes.UnsafePointer {
					if !yield(gotypes.Unsafe.Scope().Lookup("Pointer").(*gotypes.TypeName)) {
						return
					}
				} else if obj, ok := gotypes.Universe.Lookup(typ.Name()).(*gotypes.TypeName); ok {
					if !yield(obj) {
						return
					}
				}
			case *gotypes.TypeParam:
				if !yield(typ.Obj()) {
					return
				}
			case *gotypes.Map:
				pending = append(pending, typ.Key(), typ.Elem())
			case interface{ Elem() gotypes.Type }:
				pending = append(pending, typ.Elem())
			case *gotypes.Struct:
				for field := range typ.Fields() {
					pending = append(pending, field.Type())
				}
			case *gotypes.Tuple:
				for v := range typ.Variables() {
					pending = append(pending, v.Type())
				}
			case *gotypes.Signature:
				for param := range typ.TypeParams().TypeParams() {
					pending = append(pending, param.Constraint())
				}
				pending = append(pending, typ.Params(), typ.Results())
			case *gotypes.Interface:
				for method := range typ.ExplicitMethods() {
					pending = append(pending, method.Type())
				}
				for embedded := range typ.EmbeddedTypes() {
					pending = append(pending, embedded)
				}
			case *gotypes.Union:
				for term := range typ.Terms() {
					pending = append(pending, term.Type())
				}
			}
		}
	}
}

// sourceParamLabel formats a source-facing function parameter label.
func (d typeDisplay) sourceParamLabel(sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int) string {
	param := params.At(paramIndex)
	paramType := xgoutil.SourceParamType(param)
	typeName := d.typeString(paramType)
	if sig.Variadic() && paramIndex == params.Len()-1 {
		if slice, ok := paramType.(*gotypes.Slice); ok {
			typeName = "..." + d.typeString(slice.Elem())
		}
	}
	if name := xgoutil.SourceParamName(param); name != "" {
		return name + " " + typeName
	}
	return typeName
}

// displayedFuncName resolves the source-facing function display name used by
// source UI surfaces.
func displayedFuncName(fun *gotypes.Func) (parsedRecvTypeName, parsedName string, overloadID *string, isXGotMethod bool) {
	isXGoPkg := xgoutil.IsMarkedAsXGoPackage(fun.Pkg())
	name := fun.Name()
	sig := fun.Signature()

	if recv := sig.Recv(); recv != nil {
		recvType := xgoutil.DerefType(recv.Type())
		if named, ok := recvType.(*gotypes.Named); ok {
			parsedRecvTypeName = named.Obj().Name()
		}
	} else if isXGoPkg && strings.HasPrefix(name, xgoutil.XGotPrefix) {
		recvTypeName, methodName, ok := xgoutil.SplitXGotMethodName(name, true)
		if ok {
			parsedRecvTypeName = recvTypeName
			name = methodName
			isXGotMethod = true
		}
	} else if isXGoPkg {
		if funcName, ok := xgoutil.SplitXGoxFuncName(name); ok {
			name = funcName
		}
	}

	parsedName = name
	if isXGoPkg {
		parsedName, overloadID = xgoutil.ParseXGoFuncName(parsedName)
	} else if !xgoutil.IsInMainPkg(fun) {
		parsedName = xgoutil.ToLowerCamelCase(parsedName)
	}
	return
}

// displayedFuncResults formats the source-facing result list for function
// signatures shown in source UI surfaces.
func (d typeDisplay) displayedFuncResults(results *gotypes.Tuple) string {
	if results.Len() == 0 {
		return ""
	}
	if results.Len() == 1 && results.At(0).Name() == "" {
		return " " + d.typeString(results.At(0).Type())
	}

	var sb strings.Builder
	sb.WriteString(" (")
	for i := range results.Len() {
		if i > 0 {
			sb.WriteString(", ")
		}
		result := results.At(i)
		if name := result.Name(); name != "" {
			sb.WriteString(name)
			sb.WriteString(" ")
		}
		sb.WriteString(d.typeString(result.Type()))
	}
	sb.WriteString(")")
	return sb.String()
}

// displayedFuncParamLabels formats the source-facing parameter list for
// function signatures shown in source UI surfaces.
func (d typeDisplay) displayedFuncParamLabels(sig *gotypes.Signature, isXGotMethod bool) []string {
	labels := make([]string, 0, sig.TypeParams().Len()+sig.Params().Len())
	for typeParam := range sig.TypeParams().TypeParams() {
		labels = append(labels, typeParam.Obj().Name()+" Type")
	}
	params := sig.Params()
	for i := range params.Len() {
		if isXGotMethod && i == 0 {
			continue
		}
		labels = append(labels, d.sourceParamLabel(sig, params, i))
	}
	return labels
}

// funcOverview formats the source-facing declaration of a function.
func (d typeDisplay) funcOverview(fun *gotypes.Func) (overview, parsedRecvTypeName, parsedName string, overloadID *string) {
	sig := fun.Signature()
	parsedRecvTypeName, parsedName, overloadID, isXGotMethod := displayedFuncName(fun)

	var sb strings.Builder
	sb.WriteString("func ")
	sb.WriteString(parsedName)
	sb.WriteString("(")
	sb.WriteString(strings.Join(d.displayedFuncParamLabels(sig, isXGotMethod), ", "))
	sb.WriteString(")")
	sb.WriteString(d.displayedFuncResults(sig.Results()))

	overview = sb.String()
	return
}

// typeOverview describes a type declaration, including its parameters and
// alias target. Named types use compact descriptions.
func (d typeDisplay) typeOverview(obj *gotypes.TypeName) string {
	var overview strings.Builder
	overview.WriteString("type ")
	overview.WriteString(obj.Name())
	if typ, ok := obj.Type().(interface{ TypeParams() *gotypes.TypeParamList }); ok {
		params := typ.TypeParams()
		if params.Len() > 0 {
			overview.WriteByte('[')
			for i := range params.Len() {
				param := params.At(i)
				if i > 0 {
					overview.WriteString(", ")
				}
				overview.WriteString(param.Obj().Name())
				overview.WriteByte(' ')
				overview.WriteString(d.typeString(param.Constraint()))
			}
			overview.WriteByte(']')
		}
	}
	if obj.IsAlias() {
		rhs := obj.Type()
		if alias, ok := rhs.(*gotypes.Alias); ok {
			rhs = alias.Rhs()
		}
		overview.WriteString(" = ")
		overview.WriteString(d.typeString(rhs))
	}
	return overview.String()
}
