package server

import (
	"errors"
	"fmt"
	"go/types"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/goplus/gogen"
	xgoast "github.com/goplus/xgo/ast"
	xgoscanner "github.com/goplus/xgo/scanner"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/internal/classfile"
	classfilespx "github.com/goplus/xgolsw/internal/classfile/spx"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
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
	spxResourceSet *SpxResourceSet

	// spxResourceRefs stores spx resource references.
	spxResourceRefs []SpxResourceRef

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

// compile compiles spx source files and returns compile result.
func (s *Server) compile() (*compileResult, error) {
	classProj := s.getClassProject()
	if classProj == nil {
		return nil, fmt.Errorf("class project not initialized")
	}

	proj := classProj.Underlying()
	proj.UpdateFiles(s.fileMapGetter())

	snapshot, err := classProj.Snapshot(classfilespx.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed to build classfile snapshot: %w", err)
	}
	return s.compileWithSnapshot(classProj, snapshot)
}

// compileWithSnapshot compiles spx source files using the provided classfile snapshot.
func (s *Server) compileWithSnapshot(classProj *classfile.Project, snapshot *classfile.Snapshot) (*compileResult, error) {
	proj := classProj.Underlying()
	var spxFiles []string
	for file := range proj.Files() {
		if path.Ext(file) == ".spx" {
			spxFiles = append(spxFiles, file)
		}
	}
	if len(spxFiles) == 0 {
		return nil, errNoMainSpxFile
	}

	result := newCompileResult(proj)
	for _, spxFile := range spxFiles {
		documentURI := s.toDocumentURI(spxFile)
		result.diagnostics[documentURI] = []Diagnostic{}

		astFile, err := proj.ASTFile(spxFile)
		if err != nil {
			var (
				errorList xgoscanner.ErrorList
				codeError *gogen.CodeError
			)
			if errors.As(err, &errorList) && astFile != nil && astFile.Pos().IsValid() {
				for _, e := range errorList {
					result.addDiagnostics(documentURI, Diagnostic{
						Severity: SeverityError,
						Range:    RangeForASTFilePosition(result.proj, astFile, e.Pos),
						Message:  s.translate(e.Msg),
					})
				}
			} else if errors.As(err, &codeError) {
				result.addDiagnostics(documentURI, Diagnostic{
					Severity: SeverityError,
					Range:    RangeForPosEnd(result.proj, codeError.Pos, codeError.End),
					Message:  codeError.Error(),
				})
			} else {
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
		if path.Base(spxFile) == "main.spx" {
			result.mainSpxFile = spxFile
		}
	}
	if mainAny, ok := snapshot.Resources.Get(classfilespx.MainFileKey); ok {
		if mainFile, ok := mainAny.(string); ok {
			result.mainSpxFile = mainFile
		}
	}

	fallbackFile := result.mainSpxFile
	if fallbackFile == "" && len(spxFiles) > 0 {
		fallbackFile = spxFiles[0]
	}
	for _, diag := range snapshot.Diagnostics {
		if !diag.Pos.IsValid() {
			continue
		}
		filename := fallbackFile
		var (
			rangeSet            bool
			startLine, startCol int
			endLine, endCol     int
		)
		if diag.Fset != nil {
			startPos := diag.Fset.Position(diag.Pos)
			if startPos.Filename != "" {
				filename = startPos.Filename
			}
			endPos := startPos
			if diag.End.IsValid() {
				endPos = diag.Fset.Position(diag.End)
			}
			startLine = startPos.Line
			if startLine < 1 {
				startLine = 1
			}
			startCol = startPos.Column
			if startCol < 1 {
				startCol = 1
			}
			endLine = endPos.Line
			if endLine < 1 {
				endLine = startLine
			}
			endCol = endPos.Column
			if endCol < 1 {
				endCol = startCol
			}
			rangeSet = true
		}
		if !rangeSet {
			continue
		}
		if filename == "" {
			continue
		}
		documentURI := s.toDocumentURI(filename)
		if existing, ok := result.diagnostics[documentURI]; ok {
			skip := false
			for _, existingDiag := range existing {
				if existingDiag.Message == diag.Msg {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		diagnostic := Diagnostic{
			Severity: SeverityError,
			Message:  diag.Msg,
			Range: Range{
				Start: Position{Line: uint32(startLine - 1), Character: uint32(startCol - 1)},
				End:   Position{Line: uint32(endLine - 1), Character: uint32(endCol - 1)},
			},
		}
		result.addDiagnostics(documentURI, diagnostic)
	}

	if result.mainSpxFile == "" {
		if len(result.diagnostics) == 0 {
			return nil, errNoMainSpxFile
		}
		return result, nil
	}

	if setAny, ok := snapshot.Resources.Get(classfilespx.ResourceSetKey); ok {
		if set, ok := setAny.(*classfilespx.ResourceSet); ok && set != nil {
			result.spxResourceSet = set
		}
	}
	if refsAny, ok := snapshot.Resources.Get(classfilespx.ResourceRefsKey); ok {
		if refs, ok := refsAny.([]*classfilespx.ResourceRef); ok {
			result.spxResourceRefs = result.spxResourceRefs[:0]
			for _, ref := range refs {
				if ref == nil {
					continue
				}
				result.spxResourceRefs = append(result.spxResourceRefs, *ref)
			}
		}
	}

	typeInfo, _ := proj.TypeInfo()
	if typeInfo != nil {
		if pkg := typeInfo.Pkg; pkg != nil {
			for file := range proj.Files() {
				if file == "main.spx" || path.Ext(file) != ".spx" {
					continue
				}
				spriteName := strings.TrimSuffix(path.Base(file), ".spx")
				if obj := pkg.Scope().Lookup(spriteName); obj != nil {
					if named, ok := xgoutil.DerefType(obj.Type()).(*types.Named); ok {
						result.spxSpriteTypes[named] = struct{}{}
					}
				}
			}
		}
	}

	s.inspectForAutoBindingSpxResources(result)
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

// inspectForAutoBindingSpxResources inspects for auto-binding spx resources.
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
}
