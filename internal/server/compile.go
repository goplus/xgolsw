package server

import (
	"fmt"
	gotypes "go/types"
	"iter"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/qiniu/x/errors"
)

// errNoMainSpxFile is the error returned when no valid main.spx file is found
// in the main package while compiling.
var errNoMainSpxFile = errors.New("no valid main.spx file found in main package")

// compileResult contains the compile results and additional information from
// the compile process.
type compileResult struct {
	proj *xgo.Project

	// mainSpxFile is the main.spx file path.
	mainSpxFile string

	// spxSpriteTypes stores the spx sprite types.
	spxSpriteTypes map[gotypes.Type]struct{}

	// spxResourceSet is the set of spx resources.
	spxResourceSet SpxResourceSet

	// spxResourceRefs stores spx resource references.
	spxResourceRefs []SpxResourceRef

	// seenSpxResourceRefs stores already seen spx resource references to avoid
	// duplicates.
	seenSpxResourceRefs map[SpxResourceRef]struct{}

	// spxSpriteResourceAutoBindings stores spx sprite resource auto-bindings.
	spxSpriteResourceAutoBindings map[gotypes.Object]struct{}

	// diagnostics stores diagnostic messages for each document.
	diagnostics map[DocumentURI][]Diagnostic

	// seenDiagnostics stores already reported diagnostics to avoid duplicates.
	seenDiagnostics map[DocumentURI]map[string]struct{}

	// hasErrorSeverityDiagnostic is true if the compile result has any
	// diagnostics with error severity.
	hasErrorSeverityDiagnostic bool
}

// newCompileResult creates a new [compileResult].
func newCompileResult(proj *xgo.Project) *compileResult {
	return &compileResult{
		proj:                          proj,
		spxSpriteTypes:                make(map[gotypes.Type]struct{}),
		spxSpriteResourceAutoBindings: make(map[gotypes.Object]struct{}),
		diagnostics:                   make(map[DocumentURI][]Diagnostic),
	}
}

// spxDefinitionsFor returns all spx definitions for the given object. It
// returns multiple definitions only if the object is an XGo overloadable
// function.
func (r *compileResult) spxDefinitionsFor(obj gotypes.Object, selectorTypeName string) []SpxDefinition {
	if obj == nil {
		return nil
	}
	if xgoutil.IsInBuiltinPkg(obj) {
		return []SpxDefinition{GetSpxDefinitionForBuiltinObj(obj)}
	}

	var pkgDoc *pkgdoc.PkgDoc
	if xgoutil.IsInMainPkg(obj) {
		pkgDoc, _ = r.proj.PkgDoc()
	} else {
		pkgPath := xgoutil.PkgPath(obj.Pkg())
		pkgDoc, _ = pkgdata.GetPkgDoc(pkgPath)
	}

	typeInfo, _ := r.proj.TypeInfo()
	switch obj := obj.(type) {
	case *gotypes.Var:
		astPkg, _ := r.proj.ASTPackage()
		forceVar := xgoutil.IsDefinedInClassFieldsDecl(r.proj.Fset, typeInfo, astPkg, obj)
		return []SpxDefinition{GetSpxDefinitionForVar(obj, selectorTypeName, forceVar, pkgDoc)}
	case *gotypes.Const:
		return []SpxDefinition{GetSpxDefinitionForConst(obj, pkgDoc)}
	case *gotypes.TypeName:
		return []SpxDefinition{GetSpxDefinitionForType(obj, pkgDoc)}
	case *gotypes.Func:
		if typeInfo != nil {
			if defIdent := typeInfo.ObjToDef[obj]; defIdent != nil && defIdent.Implicit() {
				return nil
			}
		}
		if xgoutil.IsUnexpandableXGoOverloadableFunc(obj) {
			return nil
		}
		if funcOverloads := xgoutil.ExpandXGoOverloadableFunc(obj); funcOverloads != nil {
			defs := make([]SpxDefinition, 0, len(funcOverloads))
			for _, funcOverload := range funcOverloads {
				defs = append(defs, GetSpxDefinitionForFunc(funcOverload, selectorTypeName, pkgDoc))
			}
			return defs
		}
		return []SpxDefinition{GetSpxDefinitionForFunc(obj, selectorTypeName, pkgDoc)}
	case *gotypes.PkgName:
		return []SpxDefinition{GetSpxDefinitionForPkg(obj, pkgDoc)}
	}
	return nil
}

// spxDefinitionsForIdent returns all spx definitions for the given identifier.
// It returns multiple definitions only if the identifier is an XGo
// overloadable function.
func (r *compileResult) spxDefinitionsForIdent(ident *ast.Ident) []SpxDefinition {
	if ident.Name == "_" {
		return nil
	}
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	return r.spxDefinitionsFor(typeInfo.ObjectOf(ident), SelectorTypeNameForIdent(r.proj, ident))
}

// spxDefinitionsForNamedStruct returns all spx definitions for the given named
// struct type.
func (r *compileResult) spxDefinitionsForNamedStruct(named *gotypes.Named) []SpxDefinition {
	var defs []SpxDefinition
	for structMember := range xgoutil.StructMembers(named) {
		defs = append(defs, r.spxDefinitionsFor(structMember.Member, structMember.Selector.Obj().Name())...)
	}
	return defs
}

// spxDefinitionForField returns the spx definition for the given field and
// optional selector type name.
func (r *compileResult) spxDefinitionForField(field *gotypes.Var, selectorTypeName string) SpxDefinition {
	var (
		forceVar bool
		pkgDoc   *pkgdoc.PkgDoc
	)
	if typeInfo, _ := r.proj.TypeInfo(); typeInfo != nil {
		if defIdent := typeInfo.ObjToDef[field]; defIdent != nil {
			if selectorTypeName == "" {
				selectorTypeName = SelectorTypeNameForIdent(r.proj, defIdent)
			}
			astPkg, _ := r.proj.ASTPackage()
			forceVar = xgoutil.IsDefinedInClassFieldsDecl(r.proj.Fset, typeInfo, astPkg, field)
			pkgDoc, _ = r.proj.PkgDoc()
		}
	} else {
		pkg := field.Pkg()
		pkgPath := xgoutil.PkgPath(pkg)
		pkgDoc, _ = pkgdata.GetPkgDoc(pkgPath)
	}
	return GetSpxDefinitionForVar(field, selectorTypeName, forceVar, pkgDoc)
}

// spxDefinitionForMethod returns the spx definition for the given method and
// optional selector type name.
func (r *compileResult) spxDefinitionForMethod(method *gotypes.Func, selectorTypeName string) SpxDefinition {
	var pkgDoc *pkgdoc.PkgDoc
	if typeInfo, _ := r.proj.TypeInfo(); typeInfo != nil {
		if defIdent := typeInfo.ObjToDef[method]; defIdent != nil {
			if selectorTypeName == "" {
				selectorTypeName = SelectorTypeNameForIdent(r.proj, defIdent)
			}
			pkgDoc, _ = r.proj.PkgDoc()
		}
	} else {
		if idx := strings.LastIndex(selectorTypeName, "."); idx >= 0 {
			selectorTypeName = selectorTypeName[idx+1:]
		}
		pkg := method.Pkg()
		pkgPath := xgoutil.PkgPath(pkg)
		pkgDoc, _ = pkgdata.GetPkgDoc(pkgPath)
	}
	return GetSpxDefinitionForFunc(method, selectorTypeName, pkgDoc)
}

// isInSpxEventHandler checks if the given position is inside an spx event
// handler callback.
func (r *compileResult) isInSpxEventHandler(pos token.Pos) bool {
	astPkg, _ := r.proj.ASTPackage()
	astFile := xgoutil.PosASTFile(r.proj.Fset, astPkg, pos)
	if astFile == nil {
		return false
	}
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil {
		return false
	}

	var isIn bool
	for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos-1, pos, false) {
		callExpr, ok := node.(*ast.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			continue
		}
		funcIdent, ok := callExpr.Fun.(*ast.Ident)
		if !ok {
			continue
		}
		funcObj := typeInfo.ObjectOf(funcIdent)
		if !IsInSpxPkg(funcObj) {
			continue
		}
		isIn = IsSpxEventHandlerFuncName(funcIdent.Name)
		if isIn {
			break
		}
	}
	return isIn
}

// spxResourceRefAtPosition returns the spx resource reference at the given position.
func (r *compileResult) spxResourceRefAtPosition(position token.Position) *SpxResourceRef {
	var (
		bestRef      *SpxResourceRef
		bestNodeSpan int
	)
	fset := r.proj.Fset
	for _, ref := range r.spxResourceRefs {
		nodePos := fset.Position(ref.Node.Pos())
		nodeEnd := fset.Position(ref.Node.End())
		if nodePos.Filename != position.Filename ||
			position.Line != nodePos.Line ||
			position.Column < nodePos.Column ||
			position.Column > nodeEnd.Column {
			continue
		}

		nodeSpan := nodeEnd.Column - nodePos.Column
		if bestRef == nil || nodeSpan < bestNodeSpan {
			bestRef = &ref
			bestNodeSpan = nodeSpan
		}
	}
	return bestRef
}

// spxImportsAtASTFilePosition returns the import at the given position in the given AST file.
func (r *compileResult) spxImportsAtASTFilePosition(astFile *ast.File, position token.Position) *SpxReferencePkg {
	fset := r.proj.Fset
	for _, imp := range astFile.Imports {
		nodePos := fset.Position(imp.Pos())
		nodeEnd := fset.Position(imp.End())
		if nodePos.Filename != position.Filename ||
			position.Line != nodePos.Line ||
			position.Column < nodePos.Column ||
			position.Column > nodeEnd.Column {
			continue
		}

		pkg, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		pkgDoc, err := pkgdata.GetPkgDoc(pkg)
		if err != nil {
			continue
		}
		return &SpxReferencePkg{
			Pkg:     pkgDoc,
			PkgPath: pkg,
			Node:    imp,
		}
	}
	return nil
}

// hasSpxSpriteType reports whether the given type is an spx sprite type.
func (r *compileResult) hasSpxSpriteType(typ gotypes.Type) bool {
	_, ok := r.spxSpriteTypes[typ]
	return ok
}

// hasSpxSpriteResourceAutoBinding reports whether the given object is an spx
// resource auto-binding.
func (r *compileResult) hasSpxSpriteResourceAutoBinding(obj gotypes.Object) bool {
	_, ok := r.spxSpriteResourceAutoBindings[obj]
	return ok
}

// addSpxResourceRef adds an spx resource reference to the compile result.
func (r *compileResult) addSpxResourceRef(ref SpxResourceRef) {
	if r.seenSpxResourceRefs == nil {
		r.seenSpxResourceRefs = make(map[SpxResourceRef]struct{})
	}

	if _, ok := r.seenSpxResourceRefs[ref]; ok {
		return
	}
	r.seenSpxResourceRefs[ref] = struct{}{}

	r.spxResourceRefs = append(r.spxResourceRefs, ref)
}

// addDiagnostics adds diagnostics to the compile result.
func (r *compileResult) addDiagnostics(documentURI DocumentURI, diags ...Diagnostic) {
	if r.seenDiagnostics == nil {
		r.seenDiagnostics = make(map[DocumentURI]map[string]struct{})
	}
	seenDiagnostics := r.seenDiagnostics[documentURI]
	if seenDiagnostics == nil {
		seenDiagnostics = make(map[string]struct{})
		r.seenDiagnostics[documentURI] = seenDiagnostics
	}

	r.diagnostics[documentURI] = slices.Grow(r.diagnostics[documentURI], len(diags))
	for _, diag := range diags {
		fingerprint := fmt.Sprintf("%d\n%v\n%s", diag.Severity, diag.Range, diag.Message)
		if _, ok := seenDiagnostics[fingerprint]; ok {
			continue
		}
		seenDiagnostics[fingerprint] = struct{}{}

		r.diagnostics[documentURI] = append(r.diagnostics[documentURI], diag)
		if diag.Severity == SeverityError {
			r.hasErrorSeverityDiagnostic = true
		}
	}
}

// compile compiles spx source files and returns compile result. It uses cached
// result if available.
func (s *Server) compile() (*compileResult, error) {
	// NOTE(xsw): don't create a snapshot
	snapshot := s.workspaceRootFS // .Snapshot()

	// TODO(wyvern): remove this once we have a better way to update files.
	snapshot.UpdateFiles(s.fileMapGetter())
	return s.compileAt(snapshot)
}

// compileAt compiles spx source files at the given snapshot and returns the
// compile result.
func (s *Server) compileAt(snapshot *xgo.Project) (*compileResult, error) {
	var spxFiles []string
	for file := range snapshot.Files() {
		if path.Ext(file) == ".spx" {
			spxFiles = append(spxFiles, file)
		}
	}
	if len(spxFiles) == 0 {
		return nil, errNoMainSpxFile
	}

	result := newCompileResult(snapshot)
	for _, spxFile := range spxFiles {
		documentURI := s.toDocumentURI(spxFile)
		result.diagnostics[documentURI] = []Diagnostic{}

		astFile, err := snapshot.ASTFile(spxFile)
		if err != nil {
			var (
				errorList scanner.ErrorList
				codeError *gogen.CodeError
			)
			if errors.As(err, &errorList) && astFile.Pos().IsValid() {
				// Handle parse errors.
				for _, e := range errorList {
					result.addDiagnostics(documentURI, Diagnostic{
						Severity: SeverityError,
						Range:    RangeForASTFilePosition(result.proj, astFile, e.Pos),
						Message:  s.translate(e.Msg),
					})
				}
			} else if errors.As(err, &codeError) {
				// Handle code generation errors.
				result.addDiagnostics(documentURI, Diagnostic{
					Severity: SeverityError,
					Range:    RangeForPosEnd(result.proj, codeError.Pos, codeError.End),
					Message:  codeError.Error(),
				})
			} else {
				// Handle unknown errors (including recovered panics).
				result.addDiagnostics(documentURI, Diagnostic{
					Severity: SeverityError,
					Message:  s.translate(fmt.Sprintf("failed to parse spx file: %v", err)),
				})
			}
		}
		if astFile == nil {
			continue
		}
		if astFile.Name.Name != "main" && astFile.Pos().IsValid() {
			result.addDiagnostics(documentURI, Diagnostic{
				Severity: SeverityError,
				Range:    RangeForASTFileNode(result.proj, astFile, astFile.Name),
				Message:  s.translate("package name must be main"),
			})
			continue
		}

		if spxFileBaseName := path.Base(spxFile); spxFileBaseName == "main.spx" {
			result.mainSpxFile = spxFile
		}
	}
	if result.mainSpxFile == "" {
		if len(result.diagnostics) == 0 {
			return nil, errNoMainSpxFile
		}
		return result, nil
	}

	handleErr := func(err error) {
		if typeErr, ok := err.(typesutil.Error); ok {
			if !typeErr.Pos.IsValid() {
				panic(fmt.Sprintf("unexpected nopos error: %s", typeErr.Msg))
			}
			position := typeErr.Fset.Position(typeErr.Pos)
			documentURI := s.toDocumentURI(position.Filename)
			result.addDiagnostics(documentURI, Diagnostic{
				Severity: SeverityError,
				Range:    RangeForPosEnd(result.proj, typeErr.Pos, typeErr.End),
				Message:  typeErr.Msg,
			})
		}
	}

	typeInfo, err := snapshot.TypeInfo()
	if err != nil {
		switch err := err.(type) {
		case errors.List:
			for _, e := range err {
				handleErr(e)
			}
		default:
			handleErr(err)
		}
	}
	pkg := typeInfo.Pkg

	for file := range snapshot.Files() {
		if file == "main.spx" {
			// Skip the main.spx file, as it is not a sprite file.
			continue
		}
		if path.Ext(file) != ".spx" {
			continue
		}

		spriteName := strings.TrimSuffix(path.Base(file), ".spx")
		obj := pkg.Scope().Lookup(spriteName)
		if obj != nil {
			named, ok := xgoutil.DerefType(obj.Type()).(*gotypes.Named)
			if ok {
				result.spxSpriteTypes[named] = struct{}{}
			}
		}
	}

	s.inspectForSpxResourceSet(snapshot, result)
	s.inspectForSpxResourceRefs(result)
	s.inspectDiagnosticsAnalyzers(result)

	return result, nil
}

// compileAndGetASTFileForDocumentURI handles common compilation and file
// retrieval logic for a given document URI. The returned astFile is probably
// nil even if the compilation succeeded.
func (s *Server) compileAndGetASTFileForDocumentURI(uri DocumentURI) (result *compileResult, spxFile string, astFile *ast.File, err error) {
	spxFile, err = s.fromDocumentURI(uri)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get file path from document URI %q: %w", uri, err)
	}
	if path.Ext(spxFile) != ".spx" {
		return nil, "", nil, fmt.Errorf("file %q does not have .spx extension", spxFile)
	}
	result, err = s.compile()
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to compile: %w", err)
	}
	if astPkg, _ := result.proj.ASTPackage(); astPkg != nil {
		astFile = astPkg.Files[spxFile]
	}
	return
}

// inspectForSpxResourceSet inspects for spx resource set in main.spx.
func (s *Server) inspectForSpxResourceSet(snapshot *xgo.Project, result *compileResult) {
	spxResourceSet, err := NewSpxResourceSet(snapshot)
	if err != nil {
		documentURI := s.toDocumentURI(result.mainSpxFile)
		result.addDiagnostics(documentURI, Diagnostic{
			Severity: SeverityError,
			Message:  s.translate(fmt.Sprintf("failed to create spx resource set: %v", err)),
		})
		return
	}
	result.spxResourceSet = *spxResourceSet
}

// inspectDiagnosticsAnalyzers runs registered analyzers on each spx source file
// and collects diagnostics.
//
// For each spx file in the main package, it:
//  1. Creates an analysis pass with file-specific information
//  2. Runs all registered analyzers on the file
//  3. Collects diagnostics from analyzers
//  4. Reports any analyzer errors as diagnostics
//
// Parameters:
//   - result: The compilation result containing AST and type information
//
// The function updates result.diagnostics with any issues found by analyzers.
// Diagnostic severity levels include:
//   - Error: For analyzer failures or serious code issues
//   - Warning: For potential problems that don't prevent compilation
func (s *Server) inspectDiagnosticsAnalyzers(result *compileResult) {
	proj := result.proj
	fset := proj.Fset
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return
	}
	pkgDoc, _ := proj.PkgDoc()
	for spxFile, astFile := range astPkg.Files {
		var diagnostics []Diagnostic
		// propertyNamesCached / propertyNamesCache together memoize
		// GetPropertyNamesForCall results per CallExpr. Two maps are needed
		// because a cached nil result for an unknown target must be
		// distinguished from a missing entry.
		propertyNamesCached := make(map[*ast.CallExpr]struct{})
		propertyNamesCache := make(map[*ast.CallExpr][]string)
		pass := &protocol.Pass{
			Fset:      fset,
			Files:     []*ast.File{astFile},
			Pkg:       typeInfo.Pkg,
			TypesInfo: typeInfo,
			Report: func(d protocol.Diagnostic) {
				diagnostics = append(diagnostics, Diagnostic{
					Range:    RangeForPosEnd(proj, d.Pos, d.End),
					Severity: SeverityError,
					Message:  s.translate(d.Message),
				})
			},
			ResultOf: map[*protocol.Analyzer]any{
				inspect.Analyzer: inspector.New([]*ast.File{astFile}),
			},
			IsPropertyNameType: IsSpxPropertyNameType,
			GetPropertyNamesForCall: func(call *ast.CallExpr) []string {
				if _, ok := propertyNamesCached[call]; ok {
					return propertyNamesCache[call]
				}
				propertyNamesCached[call] = struct{}{}
				named := PropertyTargetNamedTypeForCall(typeInfo, call, spxFile, result.mainSpxFile)
				if named == nil {
					return nil
				}
				names := make([]string, 0)
				for m := range propertyMembers(named, makePkgDocFor(pkgDoc)) {
					names = append(names, m.Name)
				}
				propertyNamesCache[call] = names
				return names
			},
			ResolvedCallExprArgs: func(call *ast.CallExpr) iter.Seq[xgoutil.ResolvedCallExprArg] {
				return resolvedCallExprArgs(result.proj, typeInfo, call)
			},
		}

		for _, analyzer := range s.analyzers {
			an := analyzer.Analyzer()
			if _, err := an.Run(pass); err != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Severity: SeverityError,
					Message:  s.translate(fmt.Sprintf("analyzer %q failed: %v", an.Name, err)),
				})
			}
		}

		if len(diagnostics) > 0 {
			documentURI := s.toDocumentURI(spxFile)
			result.addDiagnostics(documentURI, diagnostics...)
		}
	}
}

// inspectForSpxResourceRefs inspects for spx resource references in the code.
func (s *Server) inspectForSpxResourceRefs(result *compileResult) {
	s.inspectForAutoBindingSpxResources(result)

	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return
	}

	// Check all identifier definitions.
	for ident, obj := range typeInfo.Defs {
		if ident == nil || !ident.Pos().IsValid() || ident.Implicit() || obj == nil {
			continue
		}

		switch obj.(type) {
		case *gotypes.Const, *gotypes.Var:
			if ident.Obj == nil {
				break
			}
			valueSpec, ok := ident.Obj.Decl.(*ast.ValueSpec)
			if !ok {
				break
			}
			idx := slices.Index(valueSpec.Names, ident)
			if idx < 0 || idx >= len(valueSpec.Values) {
				break
			}
			expr := valueSpec.Values[idx]

			s.inspectSpxResourceRefForTypeAtExpr(result, expr, xgoutil.DerefType(obj.Type()), nil)
		}
	}

	// Check all type-checked expressions.
	for expr, tv := range typeInfo.Types {
		if expr == nil || !expr.Pos().IsValid() || tv.IsType() || tv.Type == nil {
			continue
		}

		switch expr := expr.(type) {
		case *ast.BasicLit:
			if expr.Kind == token.STRING {
				if returnType := s.resolveReturnTypeForExpr(result, expr); returnType != nil {
					getSpriteContext := sync.OnceValue(func() *SpxSpriteResource {
						spxFileBaseName := path.Base(xgoutil.NodeFilename(result.proj.Fset, expr))
						if spxFileBaseName == "main.spx" {
							return nil
						}
						spriteName := strings.TrimSuffix(spxFileBaseName, ".spx")
						return result.spxResourceSet.Sprite(spriteName)
					})
					s.inspectSpxResourceRefForTypeAtExpr(result, expr, returnType, getSpriteContext)
				} else {
					s.inspectSpxResourceRefForTypeAtExpr(result, expr, xgoutil.DerefType(tv.Type), nil)
				}
			}
		case *ast.Ident:
			typ := xgoutil.DerefType(tv.Type)
			switch typ {
			case GetSpxBackdropNameType(),
				GetSpxSpriteNameType(),
				GetSpxSoundNameType(),
				GetSpxWidgetNameType():
				s.inspectSpxResourceRefForTypeAtExpr(result, s.resolveIdentifierToAssignedExpr(result, expr), typ, nil)
			}
		case *ast.CallExpr:
			fun := xgoutil.FuncFromCallExpr(typeInfo, expr)
			funcOverloads := callExprFuncOverloads(result.proj, typeInfo, expr)
			if fun == nil || (!HasSpxResourceNameTypeParams(fun) && len(expr.Kwargs) == 0 && len(funcOverloads) == 0) {
				continue
			}

			getSpriteContext := sync.OnceValue(func() *SpxSpriteResource {
				return s.resolveSpxSpriteContextFromCallExpr(result, expr)
			})
			for resolvedArg := range resolvedCallExprArgs(result.proj, typeInfo, expr) {
				if resolvedArg.ExpectedType == nil {
					continue
				}
				paramType := xgoutil.DerefType(resolvedArg.ExpectedType)

				if sliceLit, ok := resolvedArg.Arg.(*ast.SliceLit); ok {
					paramType = spxResourceNameValueType(resolvedArg.ExpectedType)
					for _, elt := range sliceLit.Elts {
						s.inspectSpxResourceRefForTypeAtExpr(result, elt, paramType, getSpriteContext)
					}
				} else {
					s.inspectSpxResourceRefForTypeAtExpr(result, resolvedArg.Arg, paramType, getSpriteContext)
				}
			}
		}
	}
}

// inspectForAutoBindingSpxResources inspects for auto-binding spx resources and
// their references.
func (s *Server) inspectForAutoBindingSpxResources(result *compileResult) {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return
	}

	gameObj := typeInfo.Pkg.Scope().Lookup("Game")
	if gameObj == nil {
		return
	}
	gameType, ok := gameObj.Type().(*gotypes.Named)
	if !ok || !xgoutil.IsNamedStructType(gameType) {
		return
	}
	for structMember := range xgoutil.StructMembers(gameType) {
		field, ok := structMember.Member.(*gotypes.Var)
		if !ok {
			continue
		}
		fieldType, ok := xgoutil.DerefType(field.Type()).(*gotypes.Named)
		if !ok {
			continue
		}
		if fieldType == GetSpxSpriteType() || result.hasSpxSpriteType(fieldType) {
			result.spxSpriteResourceAutoBindings[structMember.Member] = struct{}{}
		}
	}
	for ident, obj := range typeInfo.Uses {
		if result.hasSpxSpriteResourceAutoBinding(obj) && !ident.Implicit() {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSpriteResourceID{SpriteName: obj.Name()},
				Kind: SpxResourceRefKindAutoBindingReference,
				Node: ident,
			})
		}
	}
}

// resolveIdentifierToAssignedExpr resolves an identifier to its assigned
// expression by looking for assignment statements in the AST.
func (s *Server) resolveIdentifierToAssignedExpr(result *compileResult, ident *ast.Ident) ast.Expr {
	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, ident)
	if astFile == nil {
		return ident
	}

	var resolvedExpr ast.Expr = ident
	for node := range xgoutil.PathEnclosingIntervalNodes(astFile, ident.Pos(), ident.End(), false) {
		assignStmt, ok := node.(*ast.AssignStmt)
		if !ok {
			continue
		}

		idx := slices.IndexFunc(assignStmt.Lhs, func(lhs ast.Expr) bool {
			return lhs == ident
		})
		if idx < 0 || idx >= len(assignStmt.Rhs) {
			continue
		}
		resolvedExpr = assignStmt.Rhs[idx]
		break
	}
	return resolvedExpr
}

// resolveReturnTypeForExpr resolves the function return type for an expression
// when it appears in a return statement.
func (s *Server) resolveReturnTypeForExpr(result *compileResult, expr ast.Expr) gotypes.Type {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, expr)
	if astFile == nil {
		return nil
	}

	path, _ := xgoutil.PathEnclosingInterval(astFile, expr.Pos(), expr.End())
	stmt := xgoutil.EnclosingReturnStmt(path)
	if stmt == nil {
		return nil
	}

	idx := xgoutil.ReturnValueIndex(stmt, expr)
	if idx < 0 {
		return nil
	}

	sig := xgoutil.EnclosingFuncSignature(typeInfo, path)
	if sig == nil || idx >= sig.Results().Len() {
		return nil
	}

	typ := xgoutil.DerefType(sig.Results().At(idx).Type())
	if IsSpxResourceNameType(typ) {
		return typ
	}
	return nil
}

// resolveSpxSpriteContextFromCallExpr resolves the sprite context from a call expression.
func (s *Server) resolveSpxSpriteContextFromCallExpr(result *compileResult, callExpr *ast.CallExpr) *SpxSpriteResource {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	funcType := typeInfo.TypeOf(callExpr.Fun)
	if !xgoutil.IsValidType(funcType) {
		return nil
	}
	funcSig, ok := funcType.(*gotypes.Signature)
	if !ok {
		return nil
	}
	funcSigRecv := funcSig.Recv()
	if funcSigRecv == nil {
		return nil
	}
	switch xgoutil.DerefType(funcSigRecv.Type()) {
	case GetSpxSpriteType(), GetSpxSpriteImplType():
	default:
		return nil
	}

	switch fun := callExpr.Fun.(type) {
	case *ast.Ident:
		spxSpriteName := strings.TrimSuffix(path.Base(xgoutil.NodeFilename(result.proj.Fset, callExpr)), ".spx")
		return result.spxResourceSet.Sprite(spxSpriteName)
	case *ast.SelectorExpr:
		ident, ok := fun.X.(*ast.Ident)
		if !ok {
			return nil
		}
		obj := typeInfo.ObjectOf(ident)
		if obj == nil {
			return nil
		}
		if !result.hasSpxSpriteResourceAutoBinding(obj) {
			return nil
		}

		spxSpriteName := obj.Name()
		return result.spxResourceSet.Sprite(spxSpriteName)
	default:
		return nil
	}
}

// inspectSpxResourceRefForTypeAtExpr inspects an spx resource reference for a
// given type at an expression.
func (s *Server) inspectSpxResourceRefForTypeAtExpr(result *compileResult, expr ast.Expr, typ gotypes.Type, getSpriteContext func() *SpxSpriteResource) {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	exprTV := typeInfo.Types[expr]

	spxResourceName, ok := xgoutil.StringLitOrConstValue(expr, exprTV)
	if !ok {
		return
	}
	spxResourceRefKind := SpxResourceRefKindStringLiteral
	if _, ok := expr.(*ast.Ident); ok {
		spxResourceRefKind = SpxResourceRefKindConstantReference
	}

	switch canonicalSpxResourceNameType(typ) {
	case GetSpxBackdropNameType():
		const resourceType = "backdrop"

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, resourceType)
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxBackdropResourceID{BackdropName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if result.spxResourceSet.Backdrop(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, resourceType, spxResourceName, "")
			}
		}
	case GetSpxSpriteNameType():
		const resourceType = "sprite"

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, resourceType)
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSpriteResourceID{SpriteName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if result.spxResourceSet.Sprite(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, resourceType, spxResourceName, "")
			}
		}
	case GetSpxSpriteCostumeNameType():
		spriteContext := getSpriteContext()
		if spriteContext == nil {
			break
		}

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, "sprite costume")
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSpriteCostumeResourceID{SpriteName: spriteContext.Name, CostumeName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if spriteContext.Costume(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, "costume", spxResourceName, spriteContext.Name)
			}
		}
	case GetSpxSpriteAnimationNameType():
		spriteContext := getSpriteContext()
		if spriteContext == nil {
			break
		}

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, "sprite animation")
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSpriteAnimationResourceID{SpriteName: spriteContext.Name, AnimationName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if spriteContext.Animation(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, "animation", spxResourceName, spriteContext.Name)
			}
		}
	case GetSpxSoundNameType():
		const resourceType = "sound"

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, resourceType)
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxSoundResourceID{SoundName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if result.spxResourceSet.Sound(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, resourceType, spxResourceName, "")
			}
		}
	case GetSpxWidgetNameType():
		const resourceType = "widget"

		if spxResourceName == "" {
			s.addEmptySpxResourceNameDiagnostic(result, expr, resourceType)
		} else {
			result.addSpxResourceRef(SpxResourceRef{
				ID:   SpxWidgetResourceID{WidgetName: spxResourceName},
				Kind: spxResourceRefKind,
				Node: expr,
			})
			if result.spxResourceSet.Widget(spxResourceName) == nil {
				s.addSpxResourceNotFoundDiagnostic(result, expr, resourceType, spxResourceName, "")
			}
		}
	}
}

// addEmptySpxResourceNameDiagnostic adds a diagnostic for empty spx resource name.
func (s *Server) addEmptySpxResourceNameDiagnostic(result *compileResult, expr ast.Expr, resourceType string) {
	result.addDiagnostics(s.nodeDocumentURI(result.proj, expr), Diagnostic{
		Severity: SeverityError,
		Range:    RangeForNode(result.proj, expr),
		Message:  s.translate(fmt.Sprintf("%s resource name cannot be empty", resourceType)),
	})
}

// addSpxResourceNotFoundDiagnostic adds a diagnostic for spx resource not found.
func (s *Server) addSpxResourceNotFoundDiagnostic(result *compileResult, expr ast.Expr, resourceType, resourceName, contextSpriteName string) {
	message := fmt.Sprintf("%s resource %q not found", resourceType, resourceName)
	if contextSpriteName != "" {
		message = fmt.Sprintf("%s in sprite %q", message, contextSpriteName)
	}
	result.addDiagnostics(s.nodeDocumentURI(result.proj, expr), Diagnostic{
		Severity: SeverityError,
		Range:    RangeForNode(result.proj, expr),
		Message:  s.translate(message),
	})
}
