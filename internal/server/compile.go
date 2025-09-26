package server

import (
	"fmt"
	"go/types"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/goplus/gogen"
	xgoast "github.com/goplus/xgo/ast"
	xgoscanner "github.com/goplus/xgo/scanner"
	xgotoken "github.com/goplus/xgo/token"
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
	spxSpriteTypes map[types.Type]struct{}

	// spxResourceSet is the set of spx resources.
	spxResourceSet SpxResourceSet

	// spxResourceRefs stores spx resource references.
	spxResourceRefs []SpxResourceRef

	// seenSpxResourceRefs stores already seen spx resource references to avoid
	// duplicates.
	seenSpxResourceRefs map[SpxResourceRef]struct{}

	// spxSpriteResourceAutoBindings stores spx sprite resource auto-bindings.
	spxSpriteResourceAutoBindings map[types.Object]struct{}

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
		spxSpriteTypes:                make(map[types.Type]struct{}),
		spxSpriteResourceAutoBindings: make(map[types.Object]struct{}),
		diagnostics:                   make(map[DocumentURI][]Diagnostic),
	}
}

// spxDefinitionsFor returns all spx definitions for the given object. It
// returns multiple definitions only if the object is an XGo overloadable
// function.
func (r *compileResult) spxDefinitionsFor(obj types.Object, selectorTypeName string) []SpxDefinition {
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
	case *types.Var:
		astPkg, _ := r.proj.ASTPackage()
		forceVar := xgoutil.IsDefinedInClassFieldsDecl(r.proj.Fset, typeInfo, astPkg, obj)
		return []SpxDefinition{GetSpxDefinitionForVar(obj, selectorTypeName, forceVar, pkgDoc)}
	case *types.Const:
		return []SpxDefinition{GetSpxDefinitionForConst(obj, pkgDoc)}
	case *types.TypeName:
		return []SpxDefinition{GetSpxDefinitionForType(obj, pkgDoc)}
	case *types.Func:
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
	case *types.PkgName:
		return []SpxDefinition{GetSpxDefinitionForPkg(obj, pkgDoc)}
	}
	return nil
}

// spxDefinitionsForIdent returns all spx definitions for the given identifier.
// It returns multiple definitions only if the identifier is an XGo
// overloadable function.
func (r *compileResult) spxDefinitionsForIdent(ident *xgoast.Ident) []SpxDefinition {
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
func (r *compileResult) spxDefinitionsForNamedStruct(named *types.Named) []SpxDefinition {
	var defs []SpxDefinition
	xgoutil.WalkStruct(named, func(member types.Object, selector *types.Named) bool {
		defs = append(defs, r.spxDefinitionsFor(member, selector.Obj().Name())...)
		return true
	})
	return defs
}

// spxDefinitionForField returns the spx definition for the given field and
// optional selector type name.
func (r *compileResult) spxDefinitionForField(field *types.Var, selectorTypeName string) SpxDefinition {
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
func (r *compileResult) spxDefinitionForMethod(method *types.Func, selectorTypeName string) SpxDefinition {
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
func (r *compileResult) isInSpxEventHandler(pos xgotoken.Pos) bool {
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
	xgoutil.WalkPathEnclosingInterval(astFile, pos-1, pos, false, func(node xgoast.Node) bool {
		callExpr, ok := node.(*xgoast.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			return true
		}
		funcIdent, ok := callExpr.Fun.(*xgoast.Ident)
		if !ok {
			return true
		}
		funcObj := typeInfo.ObjectOf(funcIdent)
		if !IsInSpxPkg(funcObj) {
			return true
		}
		isIn = IsSpxEventHandlerFuncName(funcIdent.Name)
		return !isIn // Stop walking if we found a match.
	})
	return isIn
}

// spxResourceRefAtPosition returns the spx resource reference at the given position.
func (r *compileResult) spxResourceRefAtPosition(position xgotoken.Position) *SpxResourceRef {
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
func (r *compileResult) spxImportsAtASTFilePosition(astFile *xgoast.File, position xgotoken.Position) *SpxReferencePkg {
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
func (r *compileResult) hasSpxSpriteType(typ types.Type) bool {
	_, ok := r.spxSpriteTypes[typ]
	return ok
}

// hasSpxSpriteResourceAutoBinding reports whether the given object is an spx
// resource auto-binding.
func (r *compileResult) hasSpxSpriteResourceAutoBinding(obj types.Object) bool {
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
				errorList xgoscanner.ErrorList
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
			named, ok := xgoutil.DerefType(obj.Type()).(*types.Named)
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
func (s *Server) compileAndGetASTFileForDocumentURI(uri DocumentURI) (result *compileResult, spxFile string, astFile *xgoast.File, err error) {
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
	mainASTFile, _ := result.proj.ASTFile(result.mainSpxFile)
	typeInfo, _ := snapshot.TypeInfo()
	if typeInfo == nil {
		return
	}

	var spxResourceRootDir string
	for expr, tv := range typeInfo.Types {
		if expr == nil || !expr.Pos().IsValid() || expr.Pos() < mainASTFile.Pos() || expr.End() > mainASTFile.End() {
			continue
		}

		callExpr, ok := expr.(*xgoast.CallExpr)
		if !ok || len(callExpr.Args) == 0 || tv.Type != GetSpxGoptGameRunFunc().Type() {
			continue
		}
		firstArg := callExpr.Args[0]
		firstArgTV, ok := typeInfo.Types[firstArg]
		if !ok {
			continue
		}

		if types.AssignableTo(firstArgTV.Type, types.Typ[types.String]) {
			spxResourceRootDir, _ = xgoutil.StringLitOrConstValue(firstArg, firstArgTV)
		} else {
			documentURI := s.toDocumentURI(result.mainSpxFile)
			result.addDiagnostics(documentURI, Diagnostic{
				Severity: SeverityError,
				Range:    RangeForNode(result.proj, firstArg),
				Message:  s.translate("first argument of run must be a string literal or constant"),
			})
		}
		break
	}
	if spxResourceRootDir == "" {
		spxResourceRootDir = "assets"
	}
	spxResourceSet, err := NewSpxResourceSet(snapshot, spxResourceRootDir)
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
	for spxFile, astFile := range astPkg.Files {
		var diagnostics []Diagnostic
		pass := &protocol.Pass{
			Fset:      fset,
			Files:     []*xgoast.File{astFile},
			TypesInfo: typeInfo,
			Report: func(d protocol.Diagnostic) {
				diagnostics = append(diagnostics, Diagnostic{
					Range:    RangeForPosEnd(proj, d.Pos, d.End),
					Severity: SeverityError,
					Message:  s.translate(d.Message),
				})
			},
			ResultOf: map[*protocol.Analyzer]any{
				inspect.Analyzer: inspector.New([]*xgoast.File{astFile}),
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

		documentURI := s.toDocumentURI(spxFile)
		result.addDiagnostics(documentURI, diagnostics...)
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
		case *types.Const, *types.Var:
			if ident.Obj == nil {
				break
			}
			valueSpec, ok := ident.Obj.Decl.(*xgoast.ValueSpec)
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
		case *xgoast.BasicLit:
			if expr.Kind == xgotoken.STRING {
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
		case *xgoast.Ident:
			typ := xgoutil.DerefType(tv.Type)
			switch typ {
			case GetSpxBackdropNameType(),
				GetSpxSpriteNameType(),
				GetSpxSoundNameType(),
				GetSpxWidgetNameType():
				s.inspectSpxResourceRefForTypeAtExpr(result, s.resolveIdentifierToAssignedExpr(result, expr), typ, nil)
			}
		case *xgoast.CallExpr:
			fun := xgoutil.FuncFromCallExpr(typeInfo, expr)
			if fun == nil || !HasSpxResourceNameTypeParams(fun) {
				continue
			}

			getSpriteContext := sync.OnceValue(func() *SpxSpriteResource {
				return s.resolveSpxSpriteContextFromCallExpr(result, expr)
			})
			xgoutil.WalkCallExprArgs(typeInfo, expr, func(fun *types.Func, params *types.Tuple, paramIndex int, arg xgoast.Expr, argIndex int) bool {
				param := params.At(paramIndex)
				paramType := xgoutil.DerefType(param.Type())

				if sliceLit, ok := arg.(*xgoast.SliceLit); ok {
					for _, elt := range sliceLit.Elts {
						s.inspectSpxResourceRefForTypeAtExpr(result, elt, paramType, getSpriteContext)
					}
				} else {
					s.inspectSpxResourceRefForTypeAtExpr(result, arg, paramType, getSpriteContext)
				}
				return true
			})
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
	gameType, ok := gameObj.Type().(*types.Named)
	if !ok || !xgoutil.IsNamedStructType(gameType) {
		return
	}
	xgoutil.WalkStruct(gameType, func(member types.Object, selector *types.Named) bool {
		field, ok := member.(*types.Var)
		if !ok {
			return true
		}
		fieldType, ok := xgoutil.DerefType(field.Type()).(*types.Named)
		if !ok {
			return true
		}
		if fieldType == GetSpxSpriteType() || result.hasSpxSpriteType(fieldType) {
			result.spxSpriteResourceAutoBindings[member] = struct{}{}
		}
		return true
	})
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
func (s *Server) resolveIdentifierToAssignedExpr(result *compileResult, ident *xgoast.Ident) xgoast.Expr {
	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, ident)
	if astFile == nil {
		return ident
	}

	var resolvedExpr xgoast.Expr = ident
	xgoutil.WalkPathEnclosingInterval(astFile, ident.Pos(), ident.End(), false, func(node xgoast.Node) bool {
		assignStmt, ok := node.(*xgoast.AssignStmt)
		if !ok {
			return true
		}

		idx := slices.IndexFunc(assignStmt.Lhs, func(lhs xgoast.Expr) bool {
			return lhs == ident
		})
		if idx < 0 || idx >= len(assignStmt.Rhs) {
			return true
		}
		resolvedExpr = assignStmt.Rhs[idx]
		return false
	})
	return resolvedExpr
}

// resolveReturnTypeForExpr resolves the function return type for an expression
// when it appears in a return statement.
func (s *Server) resolveReturnTypeForExpr(result *compileResult, expr xgoast.Expr) types.Type {
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
func (s *Server) resolveSpxSpriteContextFromCallExpr(result *compileResult, callExpr *xgoast.CallExpr) *SpxSpriteResource {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	funcTV, ok := typeInfo.Types[callExpr.Fun]
	if !ok {
		return nil
	}
	funcSig, ok := funcTV.Type.(*types.Signature)
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
	case *xgoast.Ident:
		spxSpriteName := strings.TrimSuffix(path.Base(xgoutil.NodeFilename(result.proj.Fset, callExpr)), ".spx")
		return result.spxResourceSet.Sprite(spxSpriteName)
	case *xgoast.SelectorExpr:
		ident, ok := fun.X.(*xgoast.Ident)
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
func (s *Server) inspectSpxResourceRefForTypeAtExpr(result *compileResult, expr xgoast.Expr, typ types.Type, getSpriteContext func() *SpxSpriteResource) {
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
	if _, ok := expr.(*xgoast.Ident); ok {
		spxResourceRefKind = SpxResourceRefKindConstantReference
	}

	switch typ {
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
func (s *Server) addEmptySpxResourceNameDiagnostic(result *compileResult, expr xgoast.Expr, resourceType string) {
	result.addDiagnostics(s.nodeDocumentURI(result.proj, expr), Diagnostic{
		Severity: SeverityError,
		Range:    RangeForNode(result.proj, expr),
		Message:  s.translate(fmt.Sprintf("%s resource name cannot be empty", resourceType)),
	})
}

// addSpxResourceNotFoundDiagnostic adds a diagnostic for spx resource not found.
func (s *Server) addSpxResourceNotFoundDiagnostic(result *compileResult, expr xgoast.Expr, resourceType, resourceName, contextSpriteName string) {
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
